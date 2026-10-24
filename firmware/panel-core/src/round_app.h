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

static LinkState linkState() { return linkStateOf(deck.state(), deck.approved()); }

static RoundScope scope() {
  switch (deck.scope()) {
    case agentsdeck_link_v1_Scope_SCOPE_ADMIN: return RoundScope::Admin;
    case agentsdeck_link_v1_Scope_SCOPE_ACT:   return RoundScope::Act;
    default:                                   return RoundScope::View;
  }
}

static void onFrame(deck_link_t*, const deck_frame_t* f) {
  lastHeardMs = millis();
  if (deckApply(*fleet, *f, millis())) dirty = true;
  fleet->newWaiting = false;               // round_view.h spots new waiters itself
}

static void onWelcome(deck_link_t*, const char* name, agentsdeck_link_v1_Scope, bool) {
  snprintf(deviceName, sizeof deviceName, "%s", name);
  dirty = true;
}

static void onResult(deck_link_t*, uint32_t seq, uint32_t code, const char*) {
  roundResult(ui, seq, code, millis());
  dirty = true;
}

static void onLink(deck_link_t*) { dirty = true; }

static void send(const RoundCommand& c) {
  switch (c.cmd) {
    case RoundCmd::Answer:    ui.cmdSeq = deck.answer(c.agent, c.prompt, c.key); break;
    case RoundCmd::Focus:     ui.cmdSeq = deck.focus(c.agent); break;
    case RoundCmd::Interrupt: ui.cmdSeq = deck.interrupt(c.agent); break;
    case RoundCmd::Dismiss:   ui.cmdSeq = deck.dismiss(c.agent); break;
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
  RoundInput in{f, shown, deviceName, scope(), ms, deckNowEpoch(*fleet, ms), &ui, silent};
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
  int16_t x, y;
  bool down;
  uint32_t ms;
};
static QueueHandle_t touchQueue = nullptr;

static void touchTask(void*) {
  bool was = false;
  int lx = -1, ly = -1;
  for (;;) {
    int x = 0, y = 0;
    const bool down = board.touch(x, y);
    if (down != was || (down && (x != lx || y != ly))) {
      const TouchSample t{(int16_t)x, (int16_t)y, down, millis()};
      xQueueSend(touchQueue, &t, 0);   // a full queue drops a sample, never blocks
      was = down;
      lx = x;
      ly = y;
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
    default:                       return "none";
  }
}
#endif

static void touchPump() {
  if (!touchQueue) return;
  TouchSample t;
  while (xQueueReceive(touchQueue, &t, 0) == pdTRUE) {
    lastActiveMs = millis();
    if (!fleet->have || linkState() != LinkState::Live) { ui.down = false; continue; }
    const bool was = ui.down;
    ui.lastGesture = RoundGesture::None;
    const RoundCommand c =
        roundTouch(ui, *board.layout, fleet->fleet, t.down, t.x, t.y, scope(), deckNowEpoch(*fleet, t.ms), t.ms);
    if (!was && t.down) ROUND_LOG("touch: down at %d,%d", t.x, t.y);
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
  deck.begin(Serial, cfg);
  lastActiveMs = lastHeardMs = millis();
  ui.lastTouchMs = millis();
  if (shown && board.touch) {
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
  touchPump();
  buttonPump();
  static uint32_t lastFrame = 0;
#ifdef PANEL_DEBUG
  static uint32_t lastNote = 0, frames = 0, drawMs = 0;
  if (millis() - lastNote > 5000) {
    lastNote = millis();
    const deck_link_stats_t* st = deck_link_stats(deck.core());
    ROUND_LOG("status: link %d approved %d agents %u screen %d frames %u avg %u ms rx %u bad %u overflow %u "
              "heap %u psram %u",
              (int)deck.state(), (int)deck.approved(), fleet->have ? (unsigned)fleet->fleet.count : 0u,
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
