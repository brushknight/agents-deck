#pragma once
// The firmware of a grid board, minus the board: Deck Link on the native USB
// port, the fleet, the touch UI and the frame loop. A device's main.cpp
// fills a GridBoard with its display, touch and backlight drivers and calls
// gridAppSetup() and gridAppLoop(). Arduino only (the rest of panel-core
// runs on the host too).
#if defined(ARDUINO)
#include <Arduino.h>
#include <DeckLink.h>
#include <esp_heap_caps.h>

#include <new>

#include "deck_model.h"
#include "panel_layout.h"

// Bring-up builds (-DPANEL_DEBUG) print one-line notes on the USB port, between
// Deck Link frames (the deck ignores text). Release builds print nothing.
#ifdef PANEL_DEBUG
#define PANEL_LOG(...) do { Serial.printf(__VA_ARGS__); Serial.print("\n"); } while (0)
#else
#define PANEL_LOG(...) do {} while (0)
#endif

struct GridBoard {
  const char* type;                        // Hello.type, e.g. "waveshare-touch-lcd-4"
  const PanelLayout* layout;               // e.g. new GridPanel(layoutLcd4())
  bool (*begin)();                         // display, touch, backlight; false: the display didn't start
  void (*push)(const uint16_t* frame);     // a whole RGB565 frame of the layout's width x height
  bool (*touch)(int& x, int& y);           // true while a finger is down, at (x, y) in frame pixels
  void (*brightness)(uint8_t level);       // 0..255
  int bootPin;                             // the BOOT button (active low), -1 for none
  uint8_t brightActive, brightIdle;
};

namespace grid_app {

constexpr size_t kRxBuffer = 32768;

static GridBoard board;
static DeckLink deck;
static DeckFleet* fleet = nullptr;         // in PSRAM: up to PANEL_MAX_AGENTS agents, twice
static uint16_t* frame = nullptr;          // in PSRAM
static Canvas canvas{nullptr, 0, 0};
static PanelUi ui;
static Gesture gesture;
static char deviceName[40] = "";
static bool dirty = true;
static uint32_t lastActiveMs = 0;

static LinkState linkState() { return linkStateOf(deck.state(), deck.approved()); }

static bool canAct() {
  const agentsdeck_link_v1_Scope s = deck.scope();
  return s == agentsdeck_link_v1_Scope_SCOPE_ACT || s == agentsdeck_link_v1_Scope_SCOPE_ADMIN;
}

static void onFrame(deck_link_t*, const deck_frame_t* f) {
  if (deckApply(*fleet, *f, millis())) dirty = true;
  if (fleet->newWaiting) fleet->newWaiting = false;
}

static void onWelcome(deck_link_t*, const char* name, agentsdeck_link_v1_Scope, bool) {
  snprintf(deviceName, sizeof deviceName, "%s", name);
  dirty = true;
}

static void onResult(deck_link_t*, uint32_t seq, uint32_t code, const char*) {
  uiResult(ui, seq, code, millis());
  dirty = true;
}

static void onLink(deck_link_t*) { dirty = true; }

static void render() {
  const uint32_t ms = millis();
  const LinkState ls = linkState();
  const Fleet* f = fleet->have ? &fleet->fleet : nullptr;
  if (f) board.layout->fix(ui, *f);
  // Approved but no snapshot yet: still connecting as far as the screen goes.
  const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
  PanelInput in{f, shown, deviceName, canAct(), ms, deckNowEpoch(*fleet, ms), &ui};
  board.layout->draw(canvas, in);
  board.push(frame);
}

// Bright while something happens or someone touched it in the last 30 s;
// dim when every agent is idle.
static void updateBrightness() {
  const uint32_t ms = millis();
  bool active = ms - lastActiveMs < 30000 || linkState() != LinkState::Live || !fleet->have;
  if (!active) {
    const Counts n = countFleet(fleet->fleet);
    active = n.running || n.waiting || n.error || n.hungry;
  }
  static int last = -1;
  const int level = active ? board.brightActive : board.brightIdle;
  if (level != last && board.brightness) { board.brightness((uint8_t)level); last = level; }
}

static void touchPump() {
  int x = 0, y = 0;
  const bool down = board.touch && board.touch(x, y);
  const uint32_t ms = millis();
  const GestureEvent e = gestureFeed(gesture, down, x, y, ms);
  if (down) lastActiveMs = ms;
  if (e.kind != GestureKind::None)
    PANEL_LOG("touch: %s at %d,%d", e.kind == GestureKind::Tap ? "tap" : "swipe", e.x, e.y);
  if (e.kind == GestureKind::None || !fleet->have) return;
  const Fleet& f = fleet->fleet;
  if (e.kind == GestureKind::Tap) {
    const UiAnswer a = board.layout->tap(ui, f, e.x, e.y, canAct(), ms);
    if (a.send) ui.answerSeq = deck.answer(a.agent, a.prompt, a.key);
  } else {
    board.layout->swipe(ui, f, e.swipe);
  }
  dirty = true;
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
  if (down && fleet->have) {
    board.layout->button(ui, fleet->fleet);
    lastActiveMs = now;
    dirty = true;
  }
}

}  // namespace grid_app

static inline void gridAppSetup(const GridBoard& b, const char* firmware) {
  using namespace grid_app;
  board = b;
  const int W = b.layout->width(), H = b.layout->height();
  // The USB serial driver drops what doesn't fit in its RX queue (256 bytes
  // by default), and a frame takes ~50 ms to draw: a snapshot or a busy deck
  // would lose bytes. Room for well over a frame time of traffic.
  Serial.setRxBufferSize(kRxBuffer);
  Serial.begin(115200);                                     // native USB: the rate is ignored
  if (board.bootPin >= 0) pinMode(board.bootPin, INPUT_PULLUP);

  void* fm = heap_caps_calloc(1, sizeof(DeckFleet), MALLOC_CAP_SPIRAM);
  if (!fm) fm = calloc(1, sizeof(DeckFleet));
  fleet = new (fm) DeckFleet();
  frame = (uint16_t*)heap_caps_calloc((size_t)W * H, 2, MALLOC_CAP_SPIRAM);
  canvas = Canvas{frame, W, H};

  const bool shown = frame && board.begin();
  PANEL_LOG("panel: %s, display %s, fleet %u B, frame %s, psram free %u", b.type, shown ? "up" : "FAILED",
            (unsigned)sizeof(DeckFleet), frame ? "ok" : "none", (unsigned)ESP.getFreePsram());
  if (board.brightness) board.brightness(board.brightActive);

  DeckLinkConfig cfg = {};
  panelConfig(cfg, DeckLink::chipId(), firmware, PanelSpec{b.type, (uint32_t)W, (uint32_t)H, true});
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  cfg.on_online = onLink;
  cfg.on_offline = onLink;
  deck.begin(Serial, cfg);
  lastActiveMs = millis();
  if (shown) render();
}

static inline void gridAppLoop() {
  using namespace grid_app;
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
    PANEL_LOG("status: link %d approved %d agents %u frames %u avg %u ms rx %u bad %u overflow %u heap %u psram %u",
              (int)deck.state(), (int)deck.approved(), fleet->have ? (unsigned)fleet->fleet.count : 0u,
              (unsigned)frames, frames ? (unsigned)(drawMs / frames) : 0u, (unsigned)st->rx_frames,
              (unsigned)st->rx_bad, (unsigned)st->rx_overflow, (unsigned)ESP.getFreeHeap(), (unsigned)ESP.getFreePsram());
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
