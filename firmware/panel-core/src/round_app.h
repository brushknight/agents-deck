#pragma once
// The firmware of a round board, minus the board: Deck Link on the native
// USB port, the fleet, the round UI (round_view.h) and the frame loop. A
// device's main.cpp fills a RoundBoard with its display, touch and backlight
// drivers and a layout (an OrbitPanel, a DialPanel or a SpringPanel), and
// calls roundAppSetup() and roundAppLoop(). Arduino only (the rest of
// panel-core runs on the host too). grid_app.h is the same for the square
// boards.
//
// Frames: every 100 ms (the critters animate), at once after a touch or a
// change, and back to back while the spring turns. The frame stays in PSRAM
// between frames, so a layout may redraw only what changed; with
// RoundBoard.pushDamage the board then copies only that to the panel.
//
// Built with -DROUND_BENCH it runs without Deck Link: a made-up fleet of 14
// on the spring, turned by itself every second or so, with the frame times
// on the USB port.
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
  // Optional: copies only the parts of the frame the layout says changed.
  void (*pushDamage)(const uint16_t* frame, const RoundDamage& d);
  // Optional: copies the changed parts of rows y0..y1 from a strip (row y0
  // first, kRoundW wide) straight to the panel. With it, a layout that draws
  // in strips (the spring) draws in internal RAM instead of the PSRAM frame.
  void (*pushStrip)(const uint16_t* strip, int y0, int y1, const RoundSpans& s);
  bool (*touch)(int& x, int& y);           // true while a finger is down, at (x, y) in frame pixels
  // Multi-touch boards (optional): the fingers down, up to two, in frame
  // pixels. When set it is used instead of touch(), and a pinch in goes home.
  void (*touchPoints)(TouchPoints& t);
  void (*brightness)(uint8_t level);       // 0..255
  int bootPin;                             // the BOOT button (active low): back home; -1 for none
  uint8_t brightActive, brightIdle;
};

#ifdef SPRING_PROFILE
uint32_t springProfUs[12], springProfPx;
uint32_t springNowUs() { return micros(); }
#endif

namespace round_app {

constexpr uint32_t kCalmMs = 120000;       // dim after 2 min with nothing going on
constexpr size_t kRxBuffer = 32768;        // see roundAppSetup()

static RoundBoard board;
static DeckLink deck;
static DeckFleet* fleet = nullptr;         // in PSRAM: up to PANEL_MAX_AGENTS agents, twice
static uint16_t* frame = nullptr;          // in PSRAM
constexpr int kStripRows = 40;
static uint16_t* strip = nullptr;          // kStripRows rows, in internal RAM (pushStrip boards)
static Canvas canvas{nullptr, 0, 0};
static RoundUi ui;
static char deviceName[40] = "";
static bool dirty = true;
static uint32_t lastActiveMs = 0;          // a touch, or a new waiting agent
static uint32_t lastHeardMs = 0;           // the last frame from the deck

#ifdef ROUND_BENCH
static LinkState linkState() { return LinkState::Live; }
#else
static LinkState linkState() { return linkStateOf(deck.state(), deck.approved()); }
#endif

// Frame timing (PANEL_DEBUG and ROUND_BENCH): draw and push, in microseconds.
struct FrameStats {
  uint32_t frames = 0, drawUs = 0, pushUs = 0, maxUs = 0, partial = 0;
  void add(uint32_t d, uint32_t p, bool part) {
    frames++;
    drawUs += d;
    pushUs += p;
    if (d + p > maxUs) maxUs = d + p;
    partial += part;
  }
};
static FrameStats statsAll, statsTurn;   // every frame; frames while the spring turns
static uint32_t turnMs = 0;              // time spent turning since the last report
static uint32_t lastDrawUs = 0, lastPushUs = 0;

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
  in.partial = true;   // the frame keeps the last one
  const uint32_t t0 = micros();
  if (strip && board.layout->drawStrips(in, strip, kStripRows, board.pushStrip)) {
    // Drawn and pushed in strips: the PSRAM frame no longer holds the picture.
    lastDrawUs = micros() - t0;
    lastPushUs = 0;
    statsAll.add(lastDrawUs, 0, true);
    if (springMoving(ui.spring, ms)) statsTurn.add(lastDrawUs, 0, true);
    return;
  }
  board.layout->draw(canvas, in);
  const uint32_t t1 = micros();
  RoundDamage d;
  board.layout->damage(d);
  if (!d.full && board.pushDamage) board.pushDamage(frame, d);
  else board.push(frame);
  lastDrawUs = t1 - t0;
  lastPushUs = micros() - t1;
  const bool turning = springMoving(ui.spring, ms);
  statsAll.add(lastDrawUs, lastPushUs, !d.full);
  if (turning) statsTurn.add(lastDrawUs, lastPushUs, !d.full);
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

#ifdef ROUND_BENCH
// A made-up fleet of 14: every state, both species, hats, some context used.
static void benchFleet() {
  static const char* const kNames[] = {"checkout-api", "infra-network", "docs-site", "api-tests", "infra-tf",
                                       "k8s-nodes", "auth-flow", "search-index", "mobile-app", "billing-jobs",
                                       "ledger", "scratch", "pdf-export", "rate-limit"};
  Fleet& f = fleet->fleet;
  memset(&f, 0, sizeof f);
  for (int i = 0; i < 14; i++) {
    Agent& a = f.agents[i];
    snprintf(a.id, sizeof a.id, "b%02d", i);
    snprintf(a.title, sizeof a.title, "%s", kNames[i]);
    a.slot = (uint8_t)i;
    a.species = i % 4 == 3 ? Species::Codex : Species::Claude;
    a.hat = (uint8_t)(i % 6);
    a.hatColor = (uint8_t)(i % 5);
    a.ctxWindow = 200000;
    a.ctxUsed = 20000 + i * 11000;
    a.status = i % 5 == 4 ? Status::Idle : Status::Running;
    if (i == 1 || i == 6) {
      a.status = Status::Waiting;
      snprintf(a.waitTitle, sizeof a.waitTitle, "allow bash?");
    }
    if (i == 2) { a.status = Status::Idle; a.unseen = true; }
    if (i == 4) a.status = Status::Error;
    if (i % 3 == 0 && a.status == Status::Running) snprintf(a.actTool, sizeof a.actTool, "Edit");
  }
  f.count = f.total = 14;
  fleet->have = true;
}

// Turns the ring by itself: one step either way, then three at once, with pauses.
static void benchScript() {
  static const int kSteps[] = {-1, -1, 1, 1, 3, -3, 1, -1};
  static int k = 0;
  static uint32_t next = 3000;
  const uint32_t ms = millis();
  if (springMoving(ui.spring, ms) || (int32_t)(ms - next) < 0) return;
  springStep(ui, fleet->fleet, kSteps[k++ % 8], ms);
  ui.lastTouchMs = ms;   // keep it from turning to the waiters by itself
  next = ms + 1200;
  dirty = true;
}
#endif

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
  if (b.pushStrip)
    strip = (uint16_t*)heap_caps_malloc((size_t)W * kStripRows * 2, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);

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
#ifdef ROUND_BENCH
  (void)cfg;
  benchFleet();
#else
  deck.begin(Serial, cfg);
#endif
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
#ifdef ROUND_BENCH
  benchScript();
#else
  // Drain what arrived while the last frame was drawn (each loop() reads up to 4 KB).
  for (int i = 0; i < 16; i++) {
    deck.loop();
    if (Serial.available() <= 0) break;
  }
#endif
  touchPump();
  buttonPump();
  static uint32_t lastFrame = 0, turnFrom = 0;
#if defined(PANEL_DEBUG) || defined(ROUND_BENCH)
  static uint32_t lastNote = 0;
  if (millis() - lastNote > 5000) {
    lastNote = millis();
    auto avg = [](uint32_t us, uint32_t n) { return n ? (unsigned)(us / n) : 0u; };
    const FrameStats& a = statsAll;
    const FrameStats& t = statsTurn;
    Serial.printf("frames: %u in 5 s, draw %u us push %u us (avg), max %u us, partial %u; turning: %u frames in %u ms "
                  "= %u fps, draw %u us push %u us\n",
                  (unsigned)a.frames, avg(a.drawUs, a.frames), avg(a.pushUs, a.frames), (unsigned)a.maxUs,
                  (unsigned)a.partial, (unsigned)t.frames, (unsigned)turnMs,
                  turnMs ? (unsigned)(t.frames * 1000 / turnMs) : 0u, avg(t.drawUs, t.frames), avg(t.pushUs, t.frames));
#ifdef SPRING_PROFILE
    {
      static const char* const kNames[12] = {"notch", "critter", "name", "phrase", "meta", "bar",
                                             "dots", "arc", "-", "clear", "mask", "build"};
      Serial.printf("  per frame (us):");
      for (int k = 0; k < 12; k++)
        if (springProfUs[k]) Serial.printf(" %s %u", kNames[k], a.frames ? (unsigned)(springProfUs[k] / a.frames) : 0u);
      Serial.printf("; damage %u px\n", a.frames ? (unsigned)(springProfPx / a.frames) : 0u);
      memset(springProfUs, 0, sizeof springProfUs);
      springProfPx = 0;
    }
#endif
#ifndef ROUND_BENCH
    const deck_link_stats_t* st = deck_link_stats(deck.core());
    ROUND_LOG("status: link %d approved %d agents %u screen %d rx %u bad %u overflow %u heap %u psram %u",
              (int)deck.state(), (int)deck.approved(), fleet->have ? (unsigned)fleet->fleet.count : 0u,
              (int)ui.screen, (unsigned)st->rx_frames, (unsigned)st->rx_bad, (unsigned)st->rx_overflow,
              (unsigned)ESP.getFreeHeap(), (unsigned)ESP.getFreePsram());
#endif
    statsAll = statsTurn = FrameStats();
    turnMs = 0;
  }
#endif
  // Critters animate: a frame every 100 ms, at once after a touch or new state,
  // and back to back while the spring turns.
  const uint32_t now = millis();
  const bool turning = springMoving(ui.spring, now);
  if (frame && (dirty || turning || now - lastFrame >= 100)) {
    dirty = false;
    if (turning && turnFrom) turnMs += now - turnFrom;
    turnFrom = turning ? now : 0;
    lastFrame = now;
    updateBrightness();
    render();
  }
  delay(turning ? 1 : 2);
}

#endif  // ARDUINO
