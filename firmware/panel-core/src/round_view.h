#pragma once
// The round boards' UI state: which screen is up (home, an agent's detail,
// a prompt), which agent is featured, the ring's page, and what a touch does.
// Shared by the Orbit (layout_orbit.h), the Dial (layout_dial.h) and the
// spring carousel (layout_spring.h); each layout draws the screens and says
// what lies under a point (RoundLayout).
//
//   home ──tap a critter / dot──▶ detail ──tap the middle (waits)──▶ prompt
//   home ──tap the middle──▶ the featured agent's prompt when it waits, else its detail
//   detail, prompt ──swipe down──▶ back (the prompt goes back where it came from)
//   anywhere ──30 s untouched──▶ home, featuring by priority again
//   a newly waiting agent ──▶ home with it featured (never straight into the prompt)
//
// Touch comes in as raw samples (finger down at x, y, or up). roundTouch()
// turns them into taps, swipes and drags round the rim, tracks what the
// finger presses (drawn pressed, and the 1 s hold on stop), and returns a
// command for the board to send: an Answer, Focus, Interrupt or Dismiss.
//
// Pure C++: runs on the host (tests, sim/render) as on the board.
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "deck_cmd.h"
#include "deck_ui.h"
#include "model.h"
#include "panel_layout.h"
#include "round_geom.h"
#include "spring.h"
#include "touch_pinch.h"
#include "view.h"

constexpr uint32_t kRoundIdleMs = 30000;   // untouched this long: back home
constexpr uint32_t kRoundHoldMs = kStopHoldMs; // stop: press and hold this long
constexpr uint32_t kRoundSentMs = 1000;    // "sent" after an answer, then back
constexpr float kRimTurnMin = 25.f;        // degrees round the rim that make a drag
constexpr int kRoundSlop = 14, kRoundSwipeMin = 40;
constexpr uint32_t kRoundTapMaxMs = 800;
constexpr int kRoundWaitIds = 16;          // waiting agents remembered, to spot new ones

enum class RoundScreen : uint8_t { Home, Detail, Prompt };

// What the last touch was taken for (for the debug log and the tests).
enum class RoundGesture : uint8_t { None, Tap, SwipeLeft, SwipeRight, SwipeUp, SwipeDown, Rim, Hold, Pinch };

// What the board may do (Welcome.scope): deck_cmd.h's, shared with the square boards.
using RoundScope = DeckScope;

// What lies under a point.
enum class HitKind : uint8_t {
  None,
  Agent,       // a ring critter or a dial dot: `agent`
  Middle,      // the featured agent in the middle (home), or the detail's middle
  Back,        // the detail's and the prompt's back button, or the detail's critter
  More,        // the orbit's "+N" chip
  Option,      // a prompt band: `option` (index into Agent.opts)
  Focus, Interrupt, Stop,   // the detail's buttons
};

struct RoundHit {
  HitKind kind = HitKind::None;
  int agent = -1;
  int option = -1;
  bool operator==(const RoundHit& o) const { return kind == o.kind && agent == o.agent && option == o.option; }
};

// What the board should send (deck_cmd.h).
using RoundCmd = DeckCmd;
using RoundCommand = DeckCommand;

struct RoundUi {
  RoundScreen screen = RoundScreen::Home;
  RoundScreen promptFrom = RoundScreen::Home;   // where swipe down leaves the prompt for
  char openId[12] = "";        // the detail's or the prompt's agent
  char featureId[12] = "";     // home: the featured agent when chosen (a new waiter, a dial turn); "" = by priority
  int page = 0;                // the orbit's ring page
  int optFirst = 0;            // the prompt: first option shown (more than 3 scroll)
  uint32_t lastTouchMs = 0;

  // The finger.
  bool down = false;
  int x0 = 0, y0 = 0, x = 0, y = 0;
  uint32_t downMs = 0;
  RoundHit pressed;            // what it went down on (drawn pressed until it moves or lifts)
  bool holdFired = false;      // the stop hold already sent its Dismiss
  RimDrag rim;
  RoundGesture lastGesture = RoundGesture::None;
  bool sliding = false;        // the dial: a finger on the rim slider
  PinchTracker pinch;          // two fingers (multi-touch boards)
  SpringMotion spring;         // the spring carousel's ring (spring.h)

  // The detail's button row, animated when it changes (round_ui.h):
  // bit masks of the buttons shown, and when the change began.
  struct {
    uint8_t from = 0, to = 0;
    uint32_t t0 = 0;
    char agent[12] = "";
  } row;

  // Feedback.
  char toast[40] = "";
  uint32_t toastUntil = 0;
  bool backAfterToast = false; // the prompt was answered: go back when "sent" ends
  uint32_t cmdSeq = 0;         // the command waiting for its Result
  bool wake = false;           // a new waiting agent: the board should light up (it clears this)

  // Waiting agents seen so far, to tell a new one.
  char waitIds[kRoundWaitIds][12] = {};
  uint8_t waitCount = 0;
};

// Everything a round layout draws from.
struct RoundInput {
  const Fleet* fleet;          // nullptr before the first snapshot
  LinkState link;
  const char* device;          // the board's name on the deck
  RoundScope scope;
  uint32_t ms;                 // animation clock (millis)
  int64_t nowEpoch;            // the deck's time
  const RoundUi* ui;
  uint32_t silentSec;          // seconds since the deck last said anything ("last seen")
  // The board keeps the last frame: a layout may redraw only what changed
  // (the spring carousel does), and says what with damage().
  bool partial = false;
};

// What a frame changed, for a board that copies only that to the panel.
struct RoundRect {
  int16_t x, y, w, h;
};
constexpr int kRoundMaxDamage = 48;
struct RoundDamage {
  bool full = true;            // the whole frame (rects then unused)
  int n = 0;
  RoundRect r[kRoundMaxDamage];
};

// The parts of row y that the rects cover, merged and in order: up to `max`
// spans [x0, x1). Returns how many. (Rects overlap; a pixel is in one span.)
static inline int roundRowSpans(const RoundRect* r, int n, int y, int16_t (*span)[2], int max) {
  int k = 0;
  for (int i = 0; i < n; i++) {
    if (y < r[i].y || y >= r[i].y + r[i].h || r[i].w <= 0) continue;
    int16_t x0 = r[i].x, x1 = (int16_t)(r[i].x + r[i].w);
    // Insert in order of x0, merging as we go.
    int at = 0;
    while (at < k && span[at][0] <= x0) at++;
    if (k < max) {
      for (int m = k; m > at; m--) { span[m][0] = span[m - 1][0]; span[m][1] = span[m - 1][1]; }
      span[at][0] = x0;
      span[at][1] = x1;
      k++;
    } else {   // out of room: widen the nearest span (covers more, never less)
      const int m = at > 0 ? at - 1 : 0;
      if (x0 < span[m][0]) span[m][0] = x0;
      if (x1 > span[m][1]) span[m][1] = x1;
    }
    int w = 0;
    for (int m = 1; m < k; m++) {
      if (span[m][0] <= span[w][1]) { if (span[m][1] > span[w][1]) span[w][1] = span[m][1]; }
      else { w++; span[w][0] = span[m][0]; span[w][1] = span[m][1]; }
    }
    k = k ? w + 1 : 0;
  }
  return k;
}

// A round layout: draws frames and says what is under a point. The state
// machine below does the rest. Implementations are pure (no I/O).
class RoundLayout {
 public:
  enum class Kind : uint8_t { Orbit, Dial, Spring };
  virtual ~RoundLayout() = default;
  virtual Kind kind() const = 0;
  virtual int width() const { return 480; }
  virtual int height() const { return 480; }
  // The whole frame.
  virtual void draw(Canvas& c, const RoundInput& in) const = 0;
  // What a touch at (x, y) lands on, on the screen `ui` shows.
  virtual RoundHit hit(const RoundUi& ui, const Fleet& f, int x, int y, RoundScope scope, int64_t nowEpoch) const = 0;
  // Pages of the ring (the orbit pages past 16 agents).
  virtual int pages(const Fleet& f) const = 0;
  virtual int pageOf(const Fleet& f, int agent) const = 0;
  // Where a drag round the rim may start: radii from the centre.
  virtual float rimInner() const = 0;
  // A rim that works as a slider (the dial): the agent at an angle, -1 for none.
  virtual int sliderAt(const Fleet&, float /*ang*/) const { return -1; }
  // The spring carousel's geometry (Kind::Spring), else nullptr.
  virtual const SpringGeom* spring() const { return nullptr; }
  // What the last draw() changed (only meaningful with RoundInput.partial).
  virtual void damage(RoundDamage& d) const { d.full = true; d.n = 0; }
};

// The prompt shows up to three answers, or four at a time of a longer list,
// which scrolls: the highest first answer it can start at.
constexpr int kRoundOptShown = 4;
static inline int roundOptLast(int optCount) { return optCount > 3 ? optCount - kRoundOptShown : 0; }

// ---------------------------------------------------------------- featured

// "Busiest": what makes a running agent stand out from other running ones.
static inline int busyness(const Agent& a) { return a.subagents * 1000 + ctxPct(a); }

// The agent the middle shows when nobody chose: the longest waiting, then
// hungry, then error, then the busiest running. -1 for an empty fleet.
static inline int roundAutoFeatured(const Fleet& f) {
  int best = -1, bestRank = -1;
  for (int i = 0; i < f.count; i++) {
    const Agent& a = f.agents[i];
    int rank = 0;
    if (a.status == Status::Waiting) rank = 5;
    else if (hungry(a)) rank = 4;
    else if (a.status == Status::Error) rank = 3;
    else if (a.status == Status::Running) rank = 2;
    else if (a.status == Status::Starting) rank = 1;
    if (best < 0 || rank > bestRank) { best = i; bestRank = rank; continue; }
    if (rank < bestRank) continue;
    const Agent& b = f.agents[best];
    const int64_t as = a.statusSince ? a.statusSince : INT64_MAX, bs = b.statusSince ? b.statusSince : INT64_MAX;
    switch (rank) {
      case 5: case 4: if (as < bs) best = i; break;                  // waiting / hungry longest
      case 3: if (a.statusSince > b.statusSince) best = i; break;    // the newest error
      case 2: if (busyness(a) > busyness(b) || (busyness(a) == busyness(b) && as < bs)) best = i; break;
      default: if (a.statusSince > b.statusSince) best = i; break;
    }
  }
  return best;
}

// The featured agent: the one chosen, while it is there, else by priority.
static inline int roundFeatured(const RoundUi& ui, const Fleet& f) {
  if (ui.featureId[0]) {
    const int i = findAgent(f, ui.featureId);
    if (i >= 0) return i;
  }
  return roundAutoFeatured(f);
}

// How many other agents wait besides `i` ("tap to answer · +N").
static inline int othersWaiting(const Fleet& f, int i) {
  int n = 0;
  for (int k = 0; k < f.count; k++)
    if (k != i && f.agents[k].status == Status::Waiting) n++;
  return n;
}

// ---------------------------------------------------------------- transitions

static inline void roundToast(RoundUi& ui, const char* text, uint32_t ms, uint32_t forMs = kToastMs) {
  snprintf(ui.toast, sizeof ui.toast, "%s", text);
  ui.toastUntil = ms + forMs;
}

static inline void roundHome(RoundUi& ui) {
  ui.screen = RoundScreen::Home;
  ui.openId[0] = '\0';
  ui.pressed = RoundHit();
  ui.backAfterToast = false;
}

static inline void roundOpen(RoundUi& ui, const Fleet& f, int i, RoundScreen s) {
  if (i < 0 || i >= f.count) return;
  if (s == RoundScreen::Prompt) {
    if (f.agents[i].status != Status::Waiting) s = RoundScreen::Detail;   // nothing to answer: its detail
    else ui.promptFrom = ui.screen == RoundScreen::Prompt ? ui.promptFrom : ui.screen;
  }
  ui.screen = s;
  ui.optFirst = 0;
  snprintf(ui.openId, sizeof ui.openId, "%s", f.agents[i].id);
}

// Back from a prompt or a detail.
static inline void roundBack(RoundUi& ui, const Fleet& f) {
  if (ui.screen == RoundScreen::Prompt && ui.promptFrom == RoundScreen::Detail && findAgent(f, ui.openId) >= 0)
    ui.screen = RoundScreen::Detail;
  else
    roundHome(ui);
  ui.backAfterToast = false;
}

static inline void featureAgent(RoundUi& ui, const RoundLayout& L, const Fleet& f, int i) {
  if (i < 0 || i >= f.count) return;
  snprintf(ui.featureId, sizeof ui.featureId, "%s", f.agents[i].id);
  ui.page = L.pageOf(f, i);
}

// Steps the shown agent by `step` in slot order (the detail, the dial).
static inline int stepAgent(const Fleet& f, int i, int step) {
  if (f.count == 0) return -1;
  if (i < 0) i = 0;
  return ((i + step) % f.count + f.count) % f.count;
}

// ---------------------------------------------------------------- the spring carousel

static inline bool isSpring(const RoundLayout& L) { return L.kind() == RoundLayout::Kind::Spring && L.spring(); }

// The featured agent follows where the ring is going.
static inline void springFeature(RoundUi& ui, const Fleet& f) {
  const int i = springIndex(ui.spring.to, f.count);
  if (i >= 0) snprintf(ui.featureId, sizeof ui.featureId, "%s", f.agents[i].id);
  else ui.featureId[0] = '\0';
}

// Turns the ring `steps` agents on from where it is going (positive: the next
// agents come to the seam, the ring turns anticlockwise).
static inline void springStep(RoundUi& ui, const Fleet& f, int steps, uint32_t ms) {
  if (f.count < 2 || !steps) return;
  SpringMotion& m = ui.spring;
  const float to = (float)spring_detail::iround(m.to) + steps;
  springGo(m, to, ms, springDuration(to - springPos(m, ms)));
  springFeature(ui, f);
}

// Turns agent i to the seam the short way.
static inline void springTurnTo(RoundUi& ui, const Fleet& f, int i, uint32_t ms) {
  springStep(ui, f, springShortest(springIndex(ui.spring.to, f.count), i, f.count), ms);
}

// Keeps the ring on its agent while the fleet changes, and puts it on the
// one that matters most the first time.
static inline void springSync(RoundUi& ui, const Fleet& f, uint32_t ms) {
  SpringMotion& m = ui.spring;
  if (f.count == 0) { m.placed = false; m.dragging = false; return; }
  if (!m.placed) {
    springSet(m, (float)roundFeatured(ui, f));
    springFeature(ui, f);
    return;
  }
  if (m.dragging) return;
  const int want = ui.featureId[0] ? findAgent(f, ui.featureId) : -1;
  if (want < 0) springFeature(ui, f);                              // it left: whoever is there now
  else if (want != springIndex(m.to, f.count)) springSet(m, (float)want);   // agents came or went before it
  if (!springMoving(m, ms) && (m.to < 0 || m.to >= f.count)) springSet(m, (float)springIndex(m.to, f.count));
}

// Left alone for 10 s: the ring turns to a new waiter, or to a waiter off the turn.
static inline void springTick(RoundUi& ui, const RoundLayout& L, const Fleet& f, uint32_t ms) {
  SpringMotion& m = ui.spring;
  if (!f.count || ui.down || springMoving(m, ms) || (int32_t)(ms - ui.lastTouchMs) < (int32_t)kSpringWaiterMs) return;
  const int cur = springIndex(m.to, f.count);
  int target = -1;
  if (m.pendingId[0]) {
    const int i = findAgent(f, m.pendingId);
    if (i >= 0 && f.agents[i].status == Status::Waiting) target = i;
    else m.pendingId[0] = '\0';
  }
  const int K = L.spring()->K;
  if (target < 0 && cur >= 0 && f.agents[cur].status != Status::Waiting && f.count > K) {
    int best = 0;
    for (int j = 0; j < f.count; j++) {
      if (f.agents[j].status != Status::Waiting || ((j - cur) % f.count + f.count) % f.count < K) continue;
      const int d = springShortest(cur, j, f.count);
      if (target < 0 || (d < 0 ? -d : d) < (best < 0 ? -best : best)) { target = j; best = d; }
    }
  }
  if (target < 0) return;
  m.pendingId[0] = '\0';
  if (ui.screen != RoundScreen::Home) roundHome(ui);
  if (target != cur) springTurnTo(ui, f, target, ms);
}

// Keeps the UI valid for a changed fleet, and wakes the screen to home when
// an agent starts waiting. Call after every change and every frame.
static inline void roundSync(RoundUi& ui, const RoundLayout& L, const Fleet& f, uint32_t ms) {
  // New waiting agents: the newest one is featured on home.
  int fresh = -1;
  for (int i = 0; i < f.count; i++) {
    if (f.agents[i].status != Status::Waiting) continue;
    bool seen = false;
    for (int k = 0; k < ui.waitCount; k++)
      if (!strcmp(ui.waitIds[k], f.agents[i].id)) { seen = true; break; }
    if (!seen && (fresh < 0 || f.agents[i].statusSince >= f.agents[fresh].statusSince)) fresh = i;
  }
  ui.waitCount = 0;
  for (int i = 0; i < f.count && ui.waitCount < kRoundWaitIds; i++)
    if (f.agents[i].status == Status::Waiting) snprintf(ui.waitIds[ui.waitCount++], 12, "%s", f.agents[i].id);
  if (fresh >= 0 && isSpring(L)) {
    // The spring turns to it once the board is left alone (springTick).
    snprintf(ui.spring.pendingId, sizeof ui.spring.pendingId, "%s", f.agents[fresh].id);
    ui.wake = true;
  } else if (fresh >= 0) {
    // Not while the finger is answering this very agent's prompt.
    const bool answering = ui.screen == RoundScreen::Prompt && !strcmp(ui.openId, f.agents[fresh].id);
    if (!answering) {
      roundHome(ui);
      featureAgent(ui, L, f, fresh);
    }
    ui.wake = true;
  }

  // A featured waiter that was answered goes back to the priority order on
  // the orbit; the dial keeps showing it until the timeout.
  if (ui.featureId[0]) {
    const int i = findAgent(f, ui.featureId);
    if (i < 0) ui.featureId[0] = '\0';
    else if (L.kind() == RoundLayout::Kind::Orbit && f.agents[i].status != Status::Waiting) ui.featureId[0] = '\0';
  }

  if (isSpring(L)) springSync(ui, f, ms);

  const int pages = L.pages(f);
  if (ui.page >= pages) ui.page = pages - 1;
  if (ui.page < 0) ui.page = 0;

  if (ui.screen != RoundScreen::Home) {
    const int i = findAgent(f, ui.openId);
    if (i < 0) roundHome(ui);
    else if (ui.screen == RoundScreen::Prompt && f.agents[i].status != Status::Waiting && !ui.backAfterToast)
      roundBack(ui, f);   // answered elsewhere, or it moved on
    else if (ui.screen == RoundScreen::Prompt) {
      const int last = roundOptLast(f.agents[i].optCount);
      if (ui.optFirst > last) ui.optFirst = last;
    }
  }
}

// Time passes: the "sent" toast ends, the screen times out home.
static inline void roundTick(RoundUi& ui, const RoundLayout& L, const Fleet& f, uint32_t ms) {
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) roundBack(ui, f);
  if (isSpring(L)) springTick(ui, L, f, ms);
  if (!ui.down && (int32_t)(ms - ui.lastTouchMs) >= (int32_t)kRoundIdleMs) {   // signed: safe across a touch stamped just ahead
    if (ui.screen != RoundScreen::Home) roundHome(ui);
    // Home shows what matters again (a fresh waiter keeps its place on the orbit).
    // (The spring stays where it was turned: springTick turns it to waiters.)
    if (L.kind() == RoundLayout::Kind::Dial) ui.featureId[0] = '\0';
    const int fi = roundFeatured(ui, f);
    ui.page = fi >= 0 ? L.pageOf(f, fi) : 0;
    ui.lastTouchMs = ms - kRoundIdleMs;   // don't redo this every tick
  }
}

// A tap on `h`.
static inline RoundCommand roundTap(RoundUi& ui, const RoundLayout& L, const Fleet& f, const RoundHit& h,
                                    RoundScope scope, uint32_t ms) {
  RoundCommand out;
  switch (ui.screen) {
    case RoundScreen::Home:
      if (h.kind == HitKind::Agent) {
        if (isSpring(L)) springTurnTo(ui, f, h.agent, ms);                         // a ring agent: turn it to the seam
        else if (L.kind() == RoundLayout::Kind::Dial) featureAgent(ui, L, f, h.agent);   // a dot: jump to it
        else roundOpen(ui, f, h.agent, RoundScreen::Detail);
      } else if (h.kind == HitKind::Middle) {
        const int i = roundFeatured(ui, f);
        roundOpen(ui, f, i, RoundScreen::Prompt);   // the prompt when it waits, else the detail
      } else if (h.kind == HitKind::More) {
        const int pages = L.pages(f);
        ui.page = pages > 1 ? (ui.page + 1) % pages : 0;
      }
      return out;
    case RoundScreen::Detail: {
      const int i = findAgent(f, ui.openId);
      if (i < 0) return out;
      const Agent& a = f.agents[i];
      if (h.kind == HitKind::Back) { roundHome(ui); return out; }
      if (h.kind == HitKind::Middle && a.status == Status::Waiting) roundOpen(ui, f, i, RoundScreen::Prompt);
      else if (h.kind == HitKind::Agent) snprintf(ui.openId, sizeof ui.openId, "%s", f.agents[h.agent].id);
      else if (h.kind == HitKind::Focus && canAct(scope)) {
        out = commandFor(RoundCmd::Focus, a);
        roundToast(ui, "focus", ms);
      } else if (h.kind == HitKind::Interrupt && canAct(scope)) {
        out = commandFor(RoundCmd::Interrupt, a);
        roundToast(ui, "interrupted", ms);
      } else if (h.kind == HitKind::Stop && !ui.holdFired) {
        roundToast(ui, "hold to stop", ms);
      }
      return out;
    }
    case RoundScreen::Prompt: {
      const int i = findAgent(f, ui.openId);
      if (h.kind == HitKind::Back) { roundBack(ui, f); return out; }
      if (i < 0 || h.kind != HitKind::Option || !canAct(scope) || ui.backAfterToast) return out;
      const Agent& a = f.agents[i];
      if (h.option < 0 || h.option >= a.optCount) return out;
      out = commandFor(RoundCmd::Answer, a);
      snprintf(out.prompt, sizeof out.prompt, "%s", a.promptId);
      snprintf(out.key, sizeof out.key, "%s", a.opts[h.option].key);
      roundToast(ui, "sent", ms, kRoundSentMs);
      ui.backAfterToast = true;
      return out;
    }
  }
  return out;
}

static inline void roundSwipe(RoundUi& ui, const RoundLayout& L, const Fleet& f, Swipe s, uint32_t ms = 0) {
  switch (ui.screen) {
    case RoundScreen::Home:
      if (isSpring(L)) {
        if (s == Swipe::Left || s == Swipe::Right) springStep(ui, f, s == Swipe::Left ? 1 : -1, ms);
      } else if (L.kind() == RoundLayout::Kind::Dial) {
        if (s == Swipe::Left || s == Swipe::Right)
          featureAgent(ui, L, f, stepAgent(f, roundFeatured(ui, f), s == Swipe::Left ? 1 : -1));
      } else if (L.pages(f) > 1 && (s == Swipe::Left || s == Swipe::Right)) {
        const int pages = L.pages(f);
        ui.page = (ui.page + (s == Swipe::Left ? 1 : -1) + pages) % pages;
      }
      return;
    case RoundScreen::Detail:
      if (s == Swipe::Down) roundHome(ui);
      else if (s == Swipe::Left || s == Swipe::Right) {
        const int i = stepAgent(f, findAgent(f, ui.openId), s == Swipe::Left ? 1 : -1);
        if (i >= 0) snprintf(ui.openId, sizeof ui.openId, "%s", f.agents[i].id);
      }
      return;
    case RoundScreen::Prompt: {
      const int i = findAgent(f, ui.openId);
      const int n = i >= 0 ? f.agents[i].optCount : 0;
      if (s == Swipe::Up && ui.optFirst < roundOptLast(n)) ui.optFirst++;
      else if (s == Swipe::Down && ui.optFirst > 0) ui.optFirst--;
      else if (s == Swipe::Down) roundBack(ui, f);
      return;
    }
  }
}

// A drag round the rim that turned `deg` degrees (clockwise positive).
static inline void roundRim(RoundUi& ui, const RoundLayout& L, const Fleet& f, float deg) {
  if (ui.screen != RoundScreen::Home || f.count == 0) return;
  if (L.kind() == RoundLayout::Kind::Dial) {
    // One agent per dot passed, at least one per kRimTurnMin.
    float per = 360.f / f.count;
    if (per < kRimTurnMin) per = kRimTurnMin;
    int steps = (int)(deg / per + (deg < 0 ? -0.5f : 0.5f));
    if (!steps) steps = deg < 0 ? -1 : 1;
    featureAgent(ui, L, f, stepAgent(f, roundFeatured(ui, f), steps));
    return;
  }
  const int pages = L.pages(f);
  if (pages > 1) ui.page = (ui.page + (deg > 0 ? 1 : -1) + pages) % pages;
}

constexpr float kSpringFlingDegPerMs = 0.5f;   // a release faster than this round the rim keeps turning
constexpr uint32_t kSpringFlingMs = 200;       // ... as far as the finger would go in this long

// A touch on the spring's home: a drag round the rim turns the ring under
// the finger and snaps to a slot on release (a fling keeps turning, at most
// one turn); a swipe across the middle turns one step; a tap on a ring agent
// turns it to the seam, a tap on the selected one or the middle opens it.
static inline RoundCommand springHomeTouch(RoundUi& ui, const RoundLayout& L, const Fleet& f, bool touching, int x,
                                           int y, RoundScope scope, int64_t nowEpoch, uint32_t ms) {
  SpringMotion& m = ui.spring;
  const SpringGeom& g = *L.spring();
  const float c = L.width() / 2.f;
  const float per = springStepDeg(g, f.count);
  if (touching) {
    ui.lastTouchMs = ms;
    if (!ui.down) {
      ui.down = true;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ms;
      ui.holdFired = false;
      ui.pressed = RoundHit();
      rimBegin(ui.rim, c, c, L.rimInner(), c + 8, x, y);
      if (per <= 0) ui.rim.active = false;
      ui.sliding = false;
      m.vel = 0;
      m.lastMoveMs = ms;
      return RoundCommand();
    }
    const float step = rimMove(ui.rim, c, c, x, y);
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (ui.rim.active && !m.dragging && dx * dx + dy * dy > kRoundSlop * kRoundSlop) {
      m.dragBase = springPos(m, ms);   // catches the ring mid-turn too
      m.dragging = true;
      ui.sliding = true;
    }
    if (m.dragging) {
      m.drag = m.dragBase - ui.rim.turned / per;   // clockwise turns back to earlier agents
      const uint32_t dt = ms - m.lastMoveMs;
      if (dt) {
        const float v = step / dt;
        m.vel = dt > 60 ? v : 0.5f * (v + m.vel);
      }
      m.lastMoveMs = ms;
    }
    return RoundCommand();
  }
  ui.down = false;
  ui.lastTouchMs = ms;
  ui.sliding = false;
  if (m.dragging) {
    ui.lastGesture = RoundGesture::Rim;
    ui.rim.active = false;
    float to = m.drag;
    if (ms - m.lastMoveMs <= 60 && (m.vel >= kSpringFlingDegPerMs || m.vel <= -kSpringFlingDegPerMs))
      to -= m.vel * kSpringFlingMs / per;
    const float turn = (float)springTurn(g, f.count);   // at most one turn a gesture
    if (to > m.dragBase + turn) to = m.dragBase + turn;
    if (to < m.dragBase - turn) to = m.dragBase - turn;
    const float slot = (float)spring_detail::iround(to);
    springGo(m, slot, ms, springDuration(slot - m.drag));
    springFeature(ui, f);
    return RoundCommand();
  }
  RoundCommand out;
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (adx <= kRoundSlop && ady <= kRoundSlop) {
    if ((uint32_t)(ms - ui.downMs) <= kRoundTapMaxMs) {
      ui.lastGesture = RoundGesture::Tap;
      out = roundTap(ui, L, f, L.hit(ui, f, ui.x0, ui.y0, scope, nowEpoch), scope, ms);
    }
  } else if (adx >= kRoundSwipeMin || ady >= kRoundSwipeMin) {
    const Swipe sw = adx >= ady ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down);
    ui.lastGesture = sw == Swipe::Left ? RoundGesture::SwipeLeft : sw == Swipe::Right ? RoundGesture::SwipeRight
                     : sw == Swipe::Up ? RoundGesture::SwipeUp : RoundGesture::SwipeDown;
    roundSwipe(ui, L, f, sw, ms);
  }
  ui.rim.active = false;
  return out;
}

// One touch sample: `touching` with the finger at (x, y), or lifted. Returns
// what to send, if anything.
static inline RoundCommand roundTouch(RoundUi& ui, const RoundLayout& L, const Fleet& f, bool touching, int x, int y,
                                      RoundScope scope, int64_t nowEpoch, uint32_t ms) {
  if (isSpring(L) && ui.screen == RoundScreen::Home && (ui.down || touching))
    return springHomeTouch(ui, L, f, touching, x, y, scope, nowEpoch, ms);
  RoundCommand out;
  const float cx = L.width() / 2.f, cy = L.height() / 2.f;
  if (touching) {
    ui.lastTouchMs = ms;
    if (!ui.down) {
      ui.down = true;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ms;
      ui.holdFired = false;
      ui.pressed = L.hit(ui, f, x, y, scope, nowEpoch);
      if (ui.pressed.kind == HitKind::Stop && scope != RoundScope::Admin) ui.pressed = RoundHit();
      if (ui.pressed.kind == HitKind::Option && !canAct(scope)) ui.pressed = RoundHit();
      rimBegin(ui.rim, cx, cy, L.rimInner(), cx + 8, x, y);
      if (ui.screen != RoundScreen::Home) ui.rim.active = false;
      ui.sliding = false;
      if (ui.rim.active) {
        const int i = L.sliderAt(f, ui.rim.last);
        if (i >= 0) {                     // the slider: the dot under the finger, at once
          ui.sliding = true;
          ui.pressed = RoundHit();
          featureAgent(ui, L, f, i);
        }
      }
      return out;
    }
    ui.x = x;
    ui.y = y;
    rimMove(ui.rim, cx, cy, x, y);
    if (ui.sliding) {                     // follow the finger round the ring
      const int i = L.sliderAt(f, ui.rim.last);
      if (i >= 0 && strcmp(f.agents[i].id, ui.featureId)) featureAgent(ui, L, f, i);
      return out;
    }
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > kRoundSlop * kRoundSlop) ui.pressed = RoundHit();   // it became a drag
    // Stop: held long enough while still on the button.
    if (ui.pressed.kind == HitKind::Stop && !ui.holdFired && (uint32_t)(ms - ui.downMs) >= kRoundHoldMs) {
      ui.holdFired = true;
      ui.lastGesture = RoundGesture::Hold;
      const int i = findAgent(f, ui.openId);
      if (i >= 0) {
        out = commandFor(RoundCmd::Dismiss, f.agents[i]);
        roundToast(ui, "stopping\xE2\x80\xA6", ms);
      }
    }
    return out;
  }
  if (!ui.down) return out;
  ui.down = false;
  ui.lastTouchMs = ms;
  const RoundHit pressed = ui.pressed;
  ui.pressed = RoundHit();
  if (ui.sliding) {                       // let go of the slider: the agent stays
    ui.sliding = false;
    ui.rim.active = false;
    ui.lastGesture = RoundGesture::Rim;
    return out;
  }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (ui.rim.active && (ui.rim.turned >= kRimTurnMin || ui.rim.turned <= -kRimTurnMin)) {
    ui.lastGesture = RoundGesture::Rim;
    roundRim(ui, L, f, ui.rim.turned);
  } else if (adx <= kRoundSlop && ady <= kRoundSlop) {
    if (pressed.kind == HitKind::Stop) {
      if (!ui.holdFired) roundToast(ui, "hold to stop", ms);
    } else if ((uint32_t)(ms - ui.downMs) <= kRoundTapMaxMs || pressed.kind != HitKind::None) {
      ui.lastGesture = RoundGesture::Tap;
      out = roundTap(ui, L, f, L.hit(ui, f, ui.x0, ui.y0, scope, nowEpoch), scope, ms);
    }
  } else if (adx >= kRoundSwipeMin || ady >= kRoundSwipeMin) {
    const Swipe sw = adx >= ady ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down);
    ui.lastGesture = sw == Swipe::Left ? RoundGesture::SwipeLeft : sw == Swipe::Right ? RoundGesture::SwipeRight
                     : sw == Swipe::Up ? RoundGesture::SwipeUp : RoundGesture::SwipeDown;
    roundSwipe(ui, L, f, sw, ms);
  }
  ui.rim.active = false;
  return out;
}

// How far the stop hold has filled, 0..1000 (0 when stop isn't held).
static inline int roundHoldPermille(const RoundUi& ui, uint32_t ms) {
  if (!ui.down || ui.pressed.kind != HitKind::Stop) return 0;
  const uint32_t t = ms - ui.downMs;
  return t >= kRoundHoldMs ? 1000 : (int)(t * 1000 / kRoundHoldMs);
}

// The deck's Result for the command we sent.
static inline void roundResult(RoundUi& ui, uint32_t seq, uint32_t code, uint32_t ms) {
  if (!seq || seq != ui.cmdSeq) return;
  ui.cmdSeq = 0;
  if (code == 204 || code == 200) return;
  if (code == 403) roundToast(ui, "not allowed: scope", ms);
  else if (code == 409) roundToast(ui, "the prompt changed", ms);
  else {
    char b[24];
    snprintf(b, sizeof b, "error %u", (unsigned)code);
    roundToast(ui, b, ms);
  }
  ui.backAfterToast = false;   // stay: show what went wrong
}

// A sample from a multi-touch board (touch_pinch.h): a pinch in on the
// detail or the prompt goes home. While two fingers are down, and until all
// lift, nothing else happens: no tap, no swipe, no answer. One finger alone
// goes to roundTouch() as usual.
static inline RoundCommand roundTouchPoints(RoundUi& ui, const RoundLayout& L, const Fleet& f, const TouchPoints& t,
                                            RoundScope scope, int64_t nowEpoch, uint32_t ms) {
  switch (pinchFeed(ui.pinch, t)) {
    case PinchEvent::Begin:   // drop the one-finger touch in progress
      ui.down = false;
      ui.pressed = RoundHit();
      ui.sliding = false;
      ui.rim.active = false;
      ui.lastTouchMs = ms;
      return RoundCommand();
    case PinchEvent::In:
      ui.lastGesture = RoundGesture::Pinch;
      ui.lastTouchMs = ms;
      if (ui.screen != RoundScreen::Home) roundHome(ui);
      return RoundCommand();
    case PinchEvent::End:
      ui.lastTouchMs = ms;
      return RoundCommand();
    default: break;
  }
  if (ui.pinch.active) return RoundCommand();
  return roundTouch(ui, L, f, t.n > 0, t.x[0], t.y[0], scope, nowEpoch, ms);
}
