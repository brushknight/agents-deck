// agents deck panel for the LilyGO T-Display-S3 (and the T-Display-S3 Touch).
//
// The board talks to the agents deck over the chip's native USB, or over
// Wi-Fi once paired (the idf5 build; net_link.h, docs/link/protocol.md), and
// draws the fleet: one agent in detail and a mini-board. With scope act (a
// board on a registered USB port gets it) it answers prompts, focuses
// terminals and interrupts agents, from the touch screen and the two
// buttons; with view it only looks.
//
// What the screen shows and what touch and buttons do is in
// panel-core/src/layout_tds3.h, tested on the host. This file is the I/O: the
// link (USB serial; Wi-Fi through net_link.h), the display on its 8-bit parallel bus, the backlight,
// the CST816 touch controller and the buttons. Nothing else is printed on the
// serial port: it carries Deck Link frames only.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <DeckLink.h>
#include <Wire.h>
#include <esp_heap_caps.h>
#include <time.h>

#include <new>

#include "deck_model.h"
#include "layout_tds3.h"
// The idf5-debug build prints net_link's one-line notes on the USB port,
// between Deck Link frames (the deck ignores text). Others print nothing.
#ifdef PANEL_DEBUG
#define NET_LOG(...) do { Serial.printf(__VA_ARGS__); Serial.print("\n"); } while (0)
#endif
#include "net_link.h"

#define FW_VERSION "0.1.0"

// How often the screen is drawn: the critters, the marching frame and the
// slides move in steps this long.
constexpr uint32_t kFrameMs = 40;
constexpr uint32_t kIdleFrameMs = 200;   // when nothing moves fast (tds3::busy): idle agents doze

// The clock on the empty screen: the deck sends UTC, the zone is the board's.
// TDS3_TZ is a POSIX TZ string, e.g. -DTDS3_TZ='"CET-1CEST,M3.5.0,M10.5.0/3"' for
// Central Europe; without it the clock shows UTC and says so.

// TDS3_SHOW_FPS=1 (the fps env) prints how long the last frame took (draw + push) in a corner.
#ifndef TDS3_SHOW_FPS
#define TDS3_SHOW_FPS 0
#endif

// Which way up: 1 has the USB port on the right, 3 on the left.
#ifndef TDS3_ROTATION
#define TDS3_ROTATION 1
#endif

// ---------------------------------------------------------------- hardware
// Pins from LilyGO's examples/factory/pin_config.h.
constexpr int PIN_LCD_D0 = 39, PIN_LCD_D1 = 40, PIN_LCD_D2 = 41, PIN_LCD_D3 = 42;
constexpr int PIN_LCD_D4 = 45, PIN_LCD_D5 = 46, PIN_LCD_D6 = 47, PIN_LCD_D7 = 48;
constexpr int PIN_LCD_WR = 8, PIN_LCD_RD = 9, PIN_LCD_DC = 7, PIN_LCD_CS = 6, PIN_LCD_RST = 5;
constexpr int PIN_LCD_BL = 38;
constexpr int PIN_POWER_ON = 15;      // powers the LCD (and the rest) when running on a battery
constexpr int PIN_BUTTON_1 = 0;       // BOOT
constexpr int PIN_BUTTON_2 = 14;
constexpr int PIN_TOUCH_SDA = 18, PIN_TOUCH_SCL = 17, PIN_TOUCH_INT = 16, PIN_TOUCH_RST = 21;
constexpr uint8_t CST816_ADDR = 0x15;
// The board is powered (USB or a charger): the backlight stays up, empty screen included.
constexpr uint8_t BL_LEVEL = 220;

constexpr int kWidth = tds3::kW, kHeight = tds3::kH;

#if ESP_ARDUINO_VERSION_MAJOR >= 3
// The S3's LCD peripheral sends the frame by DMA: about 10 ms, the CPU free for Wi-Fi and touch.
Arduino_DataBus* bus = new Arduino_ESP32LCD8(PIN_LCD_DC, PIN_LCD_CS, PIN_LCD_WR, PIN_LCD_RD, PIN_LCD_D0, PIN_LCD_D1,
                                             PIN_LCD_D2, PIN_LCD_D3, PIN_LCD_D4, PIN_LCD_D5, PIN_LCD_D6, PIN_LCD_D7);
#else
// GFX 1.5.0's LCD bus garbles this panel on arduino-esp32 2.0.17: the CPU drives the pins instead.
Arduino_DataBus* bus = new Arduino_ESP32PAR8Q(PIN_LCD_DC, PIN_LCD_CS, PIN_LCD_WR, PIN_LCD_RD, PIN_LCD_D0, PIN_LCD_D1,
                                              PIN_LCD_D2, PIN_LCD_D3, PIN_LCD_D4, PIN_LCD_D5, PIN_LCD_D6, PIN_LCD_D7);
#endif
Arduino_GFX* lcd = new Arduino_ST7789(bus, PIN_LCD_RST, TDS3_ROTATION, true /* IPS */, 170, 320, 35, 0, 35, 0);

// The frame in internal RAM (drawing and pushing it from PSRAM costs a frame
// time the board doesn't have on Wi-Fi); PSRAM only if internal RAM is short.
static uint16_t* frame = nullptr;
static Canvas canvas{nullptr, kWidth, kHeight};

// ---------------------------------------------------------------- state
static DeckLink deck;                 // the USB link; net_link.h adds Wi-Fi
static DeckFleet* fleet = nullptr;    // the fleet, kept current from Deck Link frames
static ViewState view;
static tds3::Ui ui;
static char deviceName[40] = "";      // the board's name on the deck, from Welcome
static bool dirty = true;             // draw at the next loop
static bool busyScreen = true;        // the last frame had fast motion: draw every kFrameMs
static uint32_t renderMs = 0;         // how long the last frame took
static uint32_t touchErrors = 0;      // failed CST816 reads (idf5-debug reports them)
static uint32_t touchDown = 0, touchPresses = 0;   // samples with a finger, and finger-down edges

// The link the screen follows: USB, or Wi-Fi once paired and away from USB.
static deck_link_t* link() { return net_link::active(); }

static LinkState linkState() {
  if (net_link::wifiPending()) return LinkState::Connecting;   // the notice says where Wi-Fi is
  return linkStateOf(deck_link_state(link()), deck_link_approved(link()));
}

static const Fleet* shownFleet() { return fleet->have ? &fleet->fleet : nullptr; }

static DeckScope scope() { return deckScope(deck_link_scope(link())); }

// Minutes past local midnight for the deck's time, -1 before the deck has sent one.
static int clockMinutes(int64_t epoch) {
  if (epoch <= 0) return -1;
  const time_t t = (time_t)epoch;
  struct tm tm;
#ifdef TDS3_TZ
  localtime_r(&t, &tm);
#else
  gmtime_r(&t, &tm);
#endif
  return tm.tm_hour * 60 + tm.tm_min;
}

// ---------------------------------------------------------------- deck events
static void onFrame(deck_link_t* ctx, const deck_frame_t* f) {
  if (ctx != link()) return;          // the other link (USB while on Wi-Fi): not what the screen shows
  deckApply(*fleet, *f, millis());
  if (fleet->newWaiting) {
    fleet->newWaiting = false;
    view.hold = false;                // never hide a new prompt behind a hold
  }
  dirty = true;
}

static void onWelcome(deck_link_t* ctx, const char* name, agentsdeck_link_v1_Scope, bool approved) {
  // A deck on USB takes over at once (and pairs the board for Wi-Fi when it needs to).
  if (ctx == deck.core()) net_link::usbWelcome(approved, name);
  else net_link::wifiWelcome();
  if (ctx == link()) snprintf(deviceName, sizeof deviceName, "%s", name);
  dirty = true;
}

static void onResult(deck_link_t* ctx, uint32_t seq, uint32_t code, const char*) {
  net_link::result(seq, code);
  if (ctx != link()) return;
  tds3::result(ui, seq, code, millis());
  dirty = true;
}

static void onCertIssued(deck_link_t*, uint32_t seq, const agentsdeck_link_v1_CertIssued* c) {
  net_link::certIssued(seq, c);
}

// The screen's link changed (USB <-> Wi-Fi): the fleet starts over (the new
// link sends a snapshot), and so does what the screen remembers. A toast stays.
static void onSwitched(NetVia) {
  fleet->~DeckFleet();
  new (fleet) DeckFleet();
  tds3::Ui fresh;
  memcpy(fresh.toast, ui.toast, sizeof fresh.toast);
  fresh.toastStart = ui.toastStart;
  fresh.toastUntil = ui.toastUntil;
  ui = fresh;
  view = ViewState();
  dirty = true;
}

static void onToast(const char* text) {
  tds3::toast(ui, text, millis(), 2500);
  dirty = true;
}

static void onLink(deck_link_t*) { dirty = true; }

static void send(const DeckCommand& c) {
  uint32_t seq = 0;
  switch (c.cmd) {
    case DeckCmd::Answer:    seq = deck_answer(link(), c.agent, c.prompt, c.key); break;
    case DeckCmd::Focus:     seq = deck_focus(link(), c.agent); break;
    case DeckCmd::Interrupt: seq = deck_interrupt(link(), c.agent); break;
    default: return;
  }
  ui.cmdSeq = seq;
}

// ---------------------------------------------------------------- drawing
static void renderNow() {
  const uint32_t ms = millis();
  const Fleet* f = shownFleet();
  const LinkState ls = linkState();
  // Approved but no snapshot yet: still connecting as far as the screen goes.
  const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
  const int hero = f ? resolveHero(*f, view, ms) : -1;
#if TDS3_SHOW_FPS
  static uint32_t lastUs = 0;
  const uint32_t t0 = micros();
#endif
  tds3::noteHero(ui, shown == LinkState::Live || shown == LinkState::Lost ? f : nullptr, hero, ms);
  tds3::noteLink(ui, shown, ms);
  const int64_t now = deckNowEpoch(*fleet, ms);
  static NetView net;
  net = net_link::view();
  tds3::Input in{f, hero, shown, deviceName, ms, now, view.hold, scope(), &ui};
  in.net = &net;
  in.clockMin = clockMinutes(now);
#ifndef TDS3_TZ
  in.clockUtc = true;
#endif
  tds3::draw(canvas, in);
  busyScreen = tds3::busy(in);
#if TDS3_SHOW_FPS
  char t[16];
  snprintf(t, sizeof t, "%ums", (unsigned)(lastUs / 1000));
  canvas.rect(kWidth - 46, kHeight - 18, 46, 18, col::bg);
  canvas.text(FONT_S, kWidth - 44, kHeight - 18, t, col::ink);
#endif
  lcd->draw16bitRGBBitmap(0, 0, frame, kWidth, kHeight);
#if TDS3_SHOW_FPS
  lastUs = micros() - t0;
#endif
  dirty = false;
}

// ---------------------------------------------------------------- touch (CST816)
static bool touchOk = false;

static bool touchBegin() {
  pinMode(PIN_TOUCH_RST, OUTPUT);
  digitalWrite(PIN_TOUCH_RST, LOW);
  delay(10);
  digitalWrite(PIN_TOUCH_RST, HIGH);
  delay(60);
  Wire.begin(PIN_TOUCH_SDA, PIN_TOUCH_SCL, 400000);
  Wire.beginTransmission(CST816_ADDR);
  if (Wire.endTransmission() != 0) return false;           // the plain T-Display-S3: no touch
  // Keep it awake: asleep it doesn't answer on I2C until touched.
  Wire.beginTransmission(CST816_ADDR);
  Wire.write(0xFE);                                        // DisAutoSleep
  Wire.write(0x01);
  Wire.endTransmission();
  return true;
}

// One sample: 1 with screen coordinates while a finger is down, 0 when none
// is, -1 when the read failed (not the same as a finger lifting).
static int touchRead(int& x, int& y) {
  Wire.beginTransmission(CST816_ADDR);
  Wire.write(0x02);                                        // FingerNum, then XposH, XposL, YposH, YposL
  if (Wire.endTransmission(false) != 0) return -1;
  if (Wire.requestFrom((int)CST816_ADDR, 5) != 5) return -1;
  uint8_t b[5];
  for (uint8_t& v : b) v = Wire.read();
  if ((b[0] & 0x0F) == 0) return 0;
  // The controller reports the panel's own portrait axes: 170 wide, 320 tall.
  const int px = ((b[1] & 0x0F) << 8) | b[2];
  const int py = ((b[3] & 0x0F) << 8) | b[4];
#if TDS3_ROTATION == 3
  x = kWidth - 1 - py;
  y = px;
#else
  x = py;
  y = kHeight - 1 - px;
#endif
  x = constrain(x, 0, kWidth - 1);
  y = constrain(y, 0, kHeight - 1);
  return 1;
}

static void touchPump() {
  if (!touchOk) return;
  static uint32_t lastPoll = 0;
  const uint32_t ms = millis();
  if (ms - lastPoll < 15) return;
  lastPoll = ms;
  int x = 0, y = 0;
  const int r = touchRead(x, y);
  if (r < 0) touchErrors++;
  if (r > 0) { touchDown++; if (!ui.down) touchPresses++; }
  if (r < 0 && !ui.down) return;
  if (r < 0) { x = ui.x; y = ui.y; }                       // a glitch mid-press: the finger is still there
  const bool down = r != 0;
  if (!down && !ui.down) return;
  if (!fleet->have || linkState() != LinkState::Live) { ui.down = false; return; }
  send(tds3::touch(ui, fleet->fleet, view, down, x, y, scope(), ms));
  dirty = true;
}

// ---------------------------------------------------------------- buttons
// A press shows the next (GPIO 14) or the previous (BOOT) agent on release;
// held for kStopHoldMs it acts instead, once (layout_tds3.h).
struct Button {
  int pin;
  tds3::Key key;
  bool down = false;
  bool held = false;          // the hold already fired
  uint32_t changedAt = 0;

  void pump(uint32_t now) {
    const bool level = digitalRead(pin) == LOW;
    if (level != down && now - changedAt >= 30) {          // debounced edge
      down = level;
      changedAt = now;
      if (!down && !held && fleet->have) {              // released before the hold: a press
        tds3::key(ui, fleet->fleet, view, key, false, scope(), now);
        dirty = true;
      }
      held = false;
      return;
    }
    if (down && !held && now - changedAt >= kStopHoldMs) {
      held = true;
      if (fleet->have && linkState() == LinkState::Live) {
        send(tds3::key(ui, fleet->fleet, view, key, true, scope(), now));
        dirty = true;
      }
    }
  }
};

static Button nextButton{PIN_BUTTON_2, tds3::Key::Next}, prevButton{PIN_BUTTON_1, tds3::Key::Prev};

// ---------------------------------------------------------------- arduino
void setup() {
#ifdef TDS3_TZ
  setenv("TZ", TDS3_TZ, 1);
  tzset();
#endif
  pinMode(PIN_POWER_ON, OUTPUT);
  digitalWrite(PIN_POWER_ON, HIGH);
  // The USB serial driver drops what doesn't fit in its RX queue (256 bytes by
  // default) while a frame is drawn: give a snapshot room.
  Serial.setRxBufferSize(16384);
  Serial.begin(115200);                                   // native USB: the rate is ignored
  pinMode(PIN_BUTTON_1, INPUT_PULLUP);
  pinMode(PIN_BUTTON_2, INPUT_PULLUP);
  pinMode(PIN_LCD_BL, OUTPUT);
  analogWrite(PIN_LCD_BL, BL_LEVEL);

  void* fm = heap_caps_calloc(1, sizeof(DeckFleet), MALLOC_CAP_SPIRAM);
  if (!fm) fm = calloc(1, sizeof(DeckFleet));
  fleet = new (fm) DeckFleet();
  frame = (uint16_t*)heap_caps_calloc((size_t)kWidth * kHeight, 2, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
  if (!frame) frame = (uint16_t*)heap_caps_calloc((size_t)kWidth * kHeight, 2, MALLOC_CAP_SPIRAM);
  canvas.px = frame;

  if (!frame || !lcd->begin()) {
    for (;;) { analogWrite(PIN_LCD_BL, BL_LEVEL); delay(500); analogWrite(PIN_LCD_BL, 0); delay(500); }   // no display: blink
  }
  touchOk = touchBegin();

  DeckLinkConfig cfg = {};
  // Hello and Subscribe: see deck_model.h. Touch only when the CST816 answered.
  panelConfig(cfg, DeckLink::chipId(), FW_VERSION, PanelSpec{"lilygo-t-display-s3", kWidth, kHeight, touchOk, 2});
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  cfg.on_online = onLink;
  cfg.on_offline = onLink;
  cfg.on_cert_issued = onCertIssued;
  cfg.on_wifi_config = net_link::wifiConfigHook();   // nullptr without Wi-Fi: the deck hears 501
  deck.begin(Serial, cfg);
  net_link::begin(deck.core(), cfg, net_link::Hooks{onSwitched, onToast});

  renderNow();
}

void loop() {
#ifdef PANEL_DEBUG
  // Every 2 s: loops, the slowest one, and where the time went.
  static uint32_t loops = 0, worst = 0, tNet = 0, tTouch = 0, tDraw = 0, frames = 0, lastNote = 0;
  const uint32_t t0 = millis();
#endif
  // Drain what arrived while the last frame was drawn (each loop() reads up to 4 KB).
  for (int i = 0; i < 16; i++) {
    deck.loop();
    if (Serial.available() <= 0) break;
  }
  net_link::loop();                   // Wi-Fi: the plan, the TLS link and its frames
#ifdef PANEL_DEBUG
  const uint32_t t1 = millis();
#endif
  touchPump();
#ifdef PANEL_DEBUG
  const uint32_t t2 = millis();
#endif
  const uint32_t now = millis();
  nextButton.pump(now);
  prevButton.pump(now);

  // At once after a change, then every kFrameMs while something moves, kIdleFrameMs when not.
  // A frame never follows the last sooner than that one took: touch, the
  // buttons and the link keep at least half the time (on Wi-Fi a frame takes longer).
  static uint32_t lastFrame = 0;
  const uint32_t gap = now - lastFrame;
  const uint32_t every = busyScreen ? kFrameMs : kIdleFrameMs;
  if ((dirty && gap >= renderMs) || gap >= (every > 2 * renderMs ? every : 2 * renderMs)) {
    lastFrame = now;
    renderNow();
    renderMs = millis() - now;
#ifdef PANEL_DEBUG
    tDraw += renderMs;
    frames++;
#endif
  }
#ifdef PANEL_DEBUG
  loops++;
  tNet += t1 - t0;
  tTouch += t2 - t1;
  const uint32_t took = millis() - t0;
  if (took > worst) worst = took;
  if (millis() - lastNote >= 2000) {
    lastNote = millis();
    NET_LOG("loop: %u loops, worst %u ms; net %u ms, touch %u ms, draw %u ms in %u frames; touch %u presses, %u down, "
            "%u errors; heap %u",
            (unsigned)loops, (unsigned)worst, (unsigned)tNet, (unsigned)tTouch, (unsigned)tDraw, (unsigned)frames,
            (unsigned)touchPresses, (unsigned)touchDown, (unsigned)touchErrors,
            (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL));
    touchPresses = touchDown = touchErrors = 0;
    loops = worst = tNet = tTouch = tDraw = frames = 0;
  }
#endif
  delay(2);
}
