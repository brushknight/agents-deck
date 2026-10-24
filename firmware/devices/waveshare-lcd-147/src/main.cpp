// agents deck panel for the Waveshare ESP32-S3-LCD-1.47B.
//
// The board is a read-only Deck Link device: it talks to the agents deck
// over the chip's native USB (docs/link/protocol.md) and draws the fleet. It
// sends Hello, Subscribe and Pong (the kit does all three) and never a
// command, so the default view scope is enough.
//
// Everything that can run without hardware lives in firmware/panel-core and is tested on
// the host. This file is the I/O: the link on USB serial, the display, the
// LED and the button. Nothing else is printed on the serial port: it carries
// Deck Link frames only.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <DeckLink.h>

#include "deck_model.h"
#include "layout_147.h"

#define FW_VERSION "0.5.0"

// ---------------------------------------------------------------- hardware
constexpr int PIN_LCD_DC = 41, PIN_LCD_CS = 42, PIN_LCD_SCK = 40, PIN_LCD_MOSI = 45;
constexpr int PIN_LCD_RST = 39, PIN_LCD_BL = 46, PIN_RGB = 38, PIN_BOOT = 0;
constexpr uint8_t BL_ACTIVE = 220, BL_IDLE = 45;

Arduino_DataBus* bus = new Arduino_ESP32SPI(PIN_LCD_DC, PIN_LCD_CS, PIN_LCD_SCK, PIN_LCD_MOSI, GFX_NOT_DEFINED);
Arduino_GFX* lcd = new Arduino_ST7789(bus, PIN_LCD_RST, 1 /* landscape */, true /* IPS */, 172, 320, 34, 0, 34, 0);

static uint16_t frame[kScreenW * kScreenH];
static Canvas canvas{frame, kScreenW, kScreenH};

// ---------------------------------------------------------------- state
static DeckLink deck;
static DeckFleet deckFleet;           // the fleet, kept current from Deck Link frames
static ViewState view;
static char deviceName[40] = "";      // the board's name on the deck, from Welcome

static LinkState linkState() { return linkStateOf(deck.state(), deck.approved()); }

static const Fleet* shownFleet() { return deckFleet.have ? &deckFleet.fleet : nullptr; }

// What is known to be current: nothing while the link is down.
static Counts liveCounts() {
  if (!deckFleet.have || linkState() != LinkState::Live) return Counts{0, 0, 0, 0};
  return countFleet(deckFleet.fleet);
}

// ---------------------------------------------------------------- deck events
static void onFrame(deck_link_t*, const deck_frame_t* f) {
  deckApply(deckFleet, *f, millis());
  if (deckFleet.newWaiting) {
    deckFleet.newWaiting = false;
    view.hold = false;                // never hide a new prompt behind a hold
  }
}

static void onWelcome(deck_link_t*, const char* name, agentsdeck_link_v1_Scope, bool) {
  snprintf(deviceName, sizeof deviceName, "%s", name);
}

// ---------------------------------------------------------------- drawing
static void renderNow() {
  const uint32_t ms = millis();
  const Fleet* f = shownFleet();
  const LinkState ls = linkState();
  // Approved but no snapshot yet: still connecting as far as the screen goes.
  const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
  const int hero = f ? resolveHero(*f, view, ms) : -1;
  drawScreen(canvas, {f, hero, shown, deviceName, ms, deckNowEpoch(deckFleet, ms), view.hold});
  lcd->draw16bitRGBBitmap(0, 0, frame, kScreenW, kScreenH);
}

static void setLed(uint8_t r, uint8_t g, uint8_t b) {
  static uint32_t last = 0xFFFFFFFF;
  const uint32_t v = (uint32_t)r << 16 | (uint32_t)g << 8 | b;
  if (v == last) return;
  last = v;
#if ESP_ARDUINO_VERSION_MAJOR >= 3
  rgbLedWrite(PIN_RGB, r, g, b);
#else
  neopixelWrite(PIN_RGB, r, g, b);
#endif
}

// Orange flashes when somebody needs the user, a slow blue breath while
// agents work, dark otherwise.
static void updateLed() {
  const uint32_t ms = millis();
  const Counts n = liveCounts();
  if (n.waiting || n.error) {
    if ((ms / 125) % 2) setLed(0, 0, 0); else setLed(60, 18, 0);
  } else if (n.running) {
    const uint32_t t = ms % 3000;
    const uint32_t tri = t < 1500 ? t : 3000 - t;          // 0..1500..0
    setLed(0, (uint8_t)(1 + tri * 5 / 1500), (uint8_t)(4 + tri * 36 / 1500));
  } else {
    setLed(0, 0, 0);
  }
}

static void updateBacklight() {
  // Notices are always bright; a quiet or empty board is dim.
  const LinkState ls = linkState();
  bool active = ls == LinkState::Unapproved || (!deckFleet.have && ls != LinkState::Live);
  if (!active) {
    const Counts n = liveCounts();
    active = n.running || n.waiting || n.error || n.hungry;
  }
  static int last = -1;
  const int level = active ? BL_ACTIVE : BL_IDLE;
  if (level != last) { analogWrite(PIN_LCD_BL, level); last = level; }
}

// ---------------------------------------------------------------- button
// BOOT only changes which agent is shown; the board never sends a command.
static void buttonPump() {
  static bool down = false;
  static uint32_t changedAt = 0;
  const bool level = digitalRead(PIN_BOOT) == LOW;
  const uint32_t now = millis();
  if (level == down || now - changedAt < 30) return;      // debounce
  down = level;
  changedAt = now;
  if (down && deckFleet.have) {
    pressNext(deckFleet.fleet, view, now);
    renderNow();
  }
}

// ---------------------------------------------------------------- arduino
void setup() {
  Serial.begin(115200);                                   // native USB: the rate is ignored
  pinMode(PIN_BOOT, INPUT_PULLUP);
  pinMode(PIN_LCD_BL, OUTPUT);
  analogWrite(PIN_LCD_BL, BL_ACTIVE);
  setLed(0, 0, 0);
  if (!lcd->begin()) {
    for (;;) { setLed(60, 0, 0); delay(500); setLed(0, 0, 0); delay(500); }   // no display: blink red
  }

  DeckLinkConfig cfg = {};
  panelConfig(cfg, DeckLink::chipId(), FW_VERSION);      // Hello and Subscribe: see deck_model.h
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  deck.begin(Serial, cfg);

  renderNow();
}

void loop() {
  deck.loop();
  buttonPump();
  updateLed();

  static uint32_t lastFrame = 0;
  if (millis() - lastFrame >= 200) {
    lastFrame = millis();
    updateBacklight();
    renderNow();
  }
  delay(2);
}
