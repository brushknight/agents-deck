#pragma once
// The firmware of a round board, minus the board: Deck Link on the native
// USB port, the fleet, the round UI (round_view.h) and the frame loop. A
// device's main.cpp fills a RoundBoard with its display, touch and backlight
// drivers and a layout (an OrbitPanel or a DialPanel), and calls
// roundAppSetup() and roundAppLoop(). Arduino only (the rest of panel-core
// runs on the host too). grid_app.h is the same for the square boards.
#if defined(ARDUINO)
#include <Arduino.h>
#include <DeckLink.h>
#include <esp_heap_caps.h>

#include <new>

#include "deck_model.h"
#include "round_ui.h"
#include "round_view.h"

#ifdef PANEL_DEBUG
#define NET_LOG(...) do { Serial.printf(__VA_ARGS__); Serial.print("\n"); } while (0)
#endif
#include "net_link.h"

#ifdef PANEL_DEBUG
#define ROUND_LOG(...) do { Serial.printf(__VA_ARGS__); Serial.print("\n"); } while (0)
#else
#define ROUND_LOG(...) do {} while (0)
#endif

struct RoundBoard {
  const char* type;                        // Hello.type, e.g. "waveshare-touch-lcd-28c"
  const RoundLayout* layout;               // e.g. new OrbitPanel(layoutOrbit28())
  bool (*begin)();                         // display, touch, backlight; false: the display didn't start
  void (*push)(const uint16_t* frame);     // a whole RGB565 frame of 480 x 480
  bool (*touch)(int& x, int& y);           // true while a finger is down, at (x, y) in frame pixels
  // Multi-touch boards (optional): the fingers down, up to two, in frame
  // pixels. When set it is used instead of touch(), and a pinch in goes home.
  void (*touchPoints)(TouchPoints& t);
  void (*brightness)(uint8_t level);       // 0..255
  int bootPin;                             // the BOOT button (active low): back home; -1 for none
  uint8_t brightActive, brightIdle;
};

namespace round_app {

constexpr uint32_t kCalmMs = 120000;       // dim after 2 min with nothing going on
constexpr size_t kRxBuffer = 32768;        // see roundAppSetup()

static RoundBoard board;
static DeckLink deck;
static DeckFleet* fleet = nullptr;         // in PSRAM: up to PANEL_MAX_AGENTS agents, twice
static uint16_t* frame = nullptr;          // in PSRAM
static Canvas canvas{nullptr, 0, 0};
static RoundUi ui;
static char deviceName[40] = "";
static bool dirty = true;
static uint32_t lastActiveMs = 0;          // a touch, or a new waiting agent
static uint32_t lastHeardMs = 0;           // the last frame from the deck

// The link the screen follows: USB, or Wi-Fi once that is up (net_link.h).
static deck_link_t* link() { return net_link::active(); }

static LinkState linkState() {
  if (net_link::wifiPending()) return LinkState::Connecting;   // the notice says where Wi-Fi is
  return linkStateOf(deck_link_state(link()), deck_link_approved(link()));
}

static RoundScope scope() {
  switch (deck_link_scope(link())) {
    case agentsdeck_link_v1_Scope_SCOPE_ADMIN: return RoundScope::Admin;
    case agentsdeck_link_v1_Scope_SCOPE_ACT:   return RoundScope::Act;
    default:                                   return RoundScope::View;
  }
}

static void onFrame(deck_link_t* ctx, const deck_frame_t* f) {
  if (ctx != link()) return;               // the other link (USB while on Wi-Fi): not what the screen shows
  lastHeardMs = millis();
  if (deckApply(*fleet, *f, millis())) dirty = true;
  fleet->newWaiting = false;               // round_view.h spots new waiters itself
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
  roundResult(ui, seq, code, millis());
  dirty = true;
}

static void onCertIssued(deck_link_t*, uint32_t seq, const agentsdeck_link_v1_CertIssued* c) {
  net_link::certIssued(seq, c);
}

static void onLink(deck_link_t*) { dirty = true; }

// The screen's link changed (USB ⇄ Wi-Fi): start the fleet over; the new link sends a snapshot.
static void onSwitched(NetVia via) {
  fleet->~DeckFleet();
  new (fleet) DeckFleet();
  roundHome(ui);
  dirty = true;
  ROUND_LOG("round: now on %s", via == NetVia::Wifi ? "Wi-Fi" : "USB");
}

static void onToast(const char* text) {
  snprintf(ui.toast, sizeof ui.toast, "%s", text);
  ui.toastUntil = millis() + 2500;
  dirty = true;
}

static void send(const RoundCommand& c) {
  switch (c.cmd) {
    case RoundCmd::Answer:    ui.cmdSeq = deck_answer(link(), c.agent, c.prompt, c.key); break;
    case RoundCmd::Focus:     ui.cmdSeq = deck_focus(link(), c.agent); break;
    case RoundCmd::Interrupt: ui.cmdSeq = deck_interrupt(link(), c.agent); break;
    case RoundCmd::Dismiss:   ui.cmdSeq = deck_dismiss(link(), c.agent); break;
    default: return;
  }
  ROUND_LOG("round: command %d for %s, seq %u", (int)c.cmd, c.agent, (unsigned)ui.cmdSeq);
}

static void render() {
  const uint32_t ms = millis();
  const LinkState ls = linkState();
  const Fleet* f = fleet->have ? &fleet->fleet : nullptr;
  if (f) {
    roundSync(ui, *board.layout, *f, ms);
    roundTick(ui, *board.layout, *f, ms);
    if (ui.wake) { ui.wake = false; lastActiveMs = ms; }
    roundAnimate(ui, *f, scope(), ms);
  }
  // Approved but no snapshot yet: still connecting as far as the screen goes.
  const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
  const uint32_t silent = (ms - lastHeardMs) / 1000;
  static NetView net;
  net = net_link::view();
  RoundInput in{f, shown, deviceName, scope(), ms, deckNowEpoch(*fleet, ms), &ui, silent, &net};
  board.layout->draw(canvas, in);
  board.push(frame);
}

// Bright while something happens or someone touched it in the last 2 min.
static void updateBrightness() {
  const uint32_t ms = millis();
  bool active = ms - lastActiveMs < kCalmMs || linkState() != LinkState::Live || !fleet->have;
  if (!active) {
    const Counts n = countFleet(fleet->fleet);
    active = n.running || n.waiting || n.error || n.hungry;
  }
  static int last = -1;
  const int level = active ? board.brightActive : board.brightIdle;
  if (level != last && board.brightness) { board.brightness((uint8_t)level); last = level; }
}

// Touch is sampled in its own task every 10 ms, not once per frame: a frame
// takes ~90 ms to draw and push, and a swipe sampled that rarely is one or
// two points, often read as a tap. The task queues the samples (every move
// while down, and the lift); the loop feeds them all to roundTouch().
struct TouchSample {
  TouchPoints t;
  uint32_t ms;
};
static QueueHandle_t touchQueue = nullptr;

static void touchTask(void*) {
  TouchPoints last;
  for (;;) {
    TouchPoints t;
    if (board.touchPoints) {
      board.touchPoints(t);
    } else {
      int x = 0, y = 0;
      t.n = board.touch(x, y) ? 1 : 0;
      t.x[0] = (int16_t)x;
      t.y[0] = (int16_t)y;
    }
    const bool moved = t.n != last.n || (t.n && (t.x[0] != last.x[0] || t.y[0] != last.y[0])) ||
                       (t.n > 1 && (t.x[1] != last.x[1] || t.y[1] != last.y[1]));
    if (moved) {
      const TouchSample s{t, millis()};
      xQueueSend(touchQueue, &s, 0);   // a full queue drops a sample, never blocks
      last = t;
    }
    vTaskDelay(pdMS_TO_TICKS(10));
  }
}

#ifdef PANEL_DEBUG
static const char* gestureName(RoundGesture g) {
  switch (g) {
    case RoundGesture::Tap:        return "tap";
    case RoundGesture::SwipeLeft:  return "swipe left";
    case RoundGesture::SwipeRight: return "swipe right";
    case RoundGesture::SwipeUp:    return "swipe up";
    case RoundGesture::SwipeDown:  return "swipe down";
    case RoundGesture::Rim:        return "rim drag";
    case RoundGesture::Hold:       return "hold";
    case RoundGesture::Pinch:      return "pinch in";
    default:                       return "none";
  }
}
#endif

static void touchPump() {
  if (!touchQueue) return;
  TouchSample s;
  while (xQueueReceive(touchQueue, &s, 0) == pdTRUE) {
    lastActiveMs = millis();
    if (!fleet->have || linkState() != LinkState::Live) { ui.down = false; ui.pinch = PinchTracker(); continue; }
    const bool was = ui.down;
    ui.lastGesture = RoundGesture::None;
    const RoundCommand c =
        roundTouchPoints(ui, *board.layout, fleet->fleet, s.t, scope(), deckNowEpoch(*fleet, s.ms), s.ms);
    if (!was && ui.down) ROUND_LOG("touch: down at %d,%d", s.t.x[0], s.t.y[0]);
    if (ui.lastGesture != RoundGesture::None)
      ROUND_LOG("touch: %s from %d,%d to %d,%d, screen %d", gestureName(ui.lastGesture), ui.x0, ui.y0, ui.x, ui.y,
                (int)ui.screen);
    send(c);
    dirty = true;
  }
}

static void buttonPump() {
  if (board.bootPin < 0) return;
  static bool down = false;
  static uint32_t changedAt = 0;
  const bool level = digitalRead(board.bootPin) == LOW;
  const uint32_t now = millis();
  if (level == down || now - changedAt < 30) return;      // debounce
  down = level;
  changedAt = now;
  if (down) {
    roundHome(ui);
    lastActiveMs = now;
    dirty = true;
  }
}

}  // namespace round_app

static inline void roundAppSetup(const RoundBoard& b, const char* firmware) {
  using namespace round_app;
  board = b;
  // The USB serial driver drops what doesn't fit in its RX queue (256 bytes
  // by default) while a frame is drawn: give a snapshot or a busy deck room.
  Serial.setRxBufferSize(kRxBuffer);
  Serial.begin(115200);                                     // native USB: the rate is ignored
  if (board.bootPin >= 0) pinMode(board.bootPin, INPUT_PULLUP);

  void* fm = heap_caps_calloc(1, sizeof(DeckFleet), MALLOC_CAP_SPIRAM);
  if (!fm) fm = calloc(1, sizeof(DeckFleet));
  fleet = new (fm) DeckFleet();
  const int W = b.layout->width(), H = b.layout->height();
  frame = (uint16_t*)heap_caps_calloc((size_t)W * H, 2, MALLOC_CAP_SPIRAM);
  canvas = Canvas{frame, W, H};

  const bool shown = frame && board.begin();
  ROUND_LOG("round: %s, display %s, fleet %u B, frame %s, psram free %u", b.type, shown ? "up" : "FAILED",
            (unsigned)sizeof(DeckFleet), frame ? "ok" : "none", (unsigned)ESP.getFreePsram());
  if (board.brightness) board.brightness(board.brightActive);

  // Hello: a 480 x 480 colour touch screen; every agent with TILE | CARD | PROMPT | NUMBERS.
  DeckLinkConfig cfg = {};
  panelConfig(cfg, DeckLink::chipId(), firmware, PanelSpec{b.type, (uint32_t)W, (uint32_t)H, true});
  cfg.input_count = 1;                                      // touch only: the BOOT button is not a deck input
  cfg.inputs[0] = agentsdeck_link_v1_Input_INPUT_TOUCH;
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  cfg.on_online = onLink;
  cfg.on_offline = onLink;
  cfg.on_cert_issued = onCertIssued;
  cfg.on_wifi_config = net_link::wifiConfigHook();   // nullptr without Wi-Fi: the core answers 501
  deck.begin(Serial, cfg);
  net_link::begin(deck.core(), cfg, net_link::Hooks{onSwitched, onToast});
  lastActiveMs = lastHeardMs = millis();
  ui.lastTouchMs = millis();
  if (shown && (board.touch || board.touchPoints)) {
    touchQueue = xQueueCreate(64, sizeof(TouchSample));
    xTaskCreatePinnedToCore(touchTask, "touch", 3072, nullptr, 2, nullptr, 0);
  }
  if (shown) render();
}

static inline void roundAppLoop() {
  using namespace round_app;
  // Drain what arrived while the last frame was drawn (each loop() reads up to 4 KB).
  for (int i = 0; i < 16; i++) {
    deck.loop();
    if (Serial.available() <= 0) break;
  }
  net_link::loop();                        // Wi-Fi: the plan, the TLS link and its frames
  touchPump();
  buttonPump();
  static uint32_t lastFrame = 0;
#ifdef PANEL_DEBUG
  static uint32_t lastNote = 0, frames = 0, drawMs = 0;
  if (millis() - lastNote > 5000) {
    lastNote = millis();
    const deck_link_stats_t* st = deck_link_stats(deck.core());
    ROUND_LOG("status: via %s phase %d link %d approved %d agents %u screen %d frames %u avg %u ms rx %u bad %u "
              "overflow %u heap %u psram %u",
              net_link::view().via == NetVia::Wifi ? "wifi" : "usb", (int)net_link::view().phase,
              (int)deck_link_state(link()), (int)deck_link_approved(link()), fleet->have ? (unsigned)fleet->fleet.count : 0u,
              (int)ui.screen, (unsigned)frames, frames ? (unsigned)(drawMs / frames) : 0u, (unsigned)st->rx_frames,
              (unsigned)st->rx_bad, (unsigned)st->rx_overflow, (unsigned)ESP.getFreeHeap(),
              (unsigned)ESP.getFreePsram());
    frames = drawMs = 0;
  }
#endif
  // Critters animate: a frame every 100 ms, and at once after a touch or new state.
  if (frame && (dirty || millis() - lastFrame >= 100)) {
    dirty = false;
    lastFrame = millis();
    updateBrightness();
#ifdef PANEL_DEBUG
    const uint32_t t0 = millis();
    render();
    drawMs += millis() - t0;
    frames++;
#else
    render();
#endif
  }
  delay(2);
}

#endif  // ARDUINO
