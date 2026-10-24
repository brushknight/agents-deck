#pragma once
// The round boards' UI state: which screen is up (home, an agent's detail,
// a prompt), which agent is featured, the ring's page, and what a touch does.
// Shared by the Orbit (layout_orbit.h) and the Dial (layout_dial.h); each
// layout draws the screens and says what lies under a point (RoundLayout).
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
#include "view.h"

constexpr uint32_t kRoundIdleMs = 30000;   // untouched this long: back home
constexpr uint32_t kRoundHoldMs = kStopHoldMs; // stop: press and hold this long
constexpr uint32_t kRoundSentMs = 1000;    // "sent" after an answer, then back
constexpr float kRimTurnMin = 25.f;        // degrees round the rim that make a drag
constexpr int kRoundSlop = 14, kRoundSwipeMin = 40;
constexpr uint32_t kRoundTapMaxMs = 800;
constexpr int kRoundWaitIds = 16;          // waiting agents remembered, to spot new ones

enum class RoundScreen : uint8_t { Home, Detail, Prompt };

// What the board may do (Welcome.scope): deck_cmd.h's, shared with the square boards.
using RoundScope = DeckScope;

// What lies under a point.
enum class HitKind : uint8_t {
  None,
  Agent,       // a ring critter or a dial dot: `agent`
  Middle,      // the featured agent in the middle (home), or the detail's middle
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
};

// A round layout: draws frames and says what is under a point. The state
// machine below does the rest. Implementations are pure (no I/O).
class RoundLayout {
 public:
  enum class Kind : uint8_t { Orbit, Dial };
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
  if (fresh >= 0) {
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
  (void)ms;
}

// Time passes: the "sent" toast ends, the screen times out home.
static inline void roundTick(RoundUi& ui, const RoundLayout& L, const Fleet& f, uint32_t ms) {
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) roundBack(ui, f);
  if (!ui.down && (uint32_t)(ms - ui.lastTouchMs) >= kRoundIdleMs) {
    if (ui.screen != RoundScreen::Home) roundHome(ui);
    // Home shows what matters again (a fresh waiter keeps its place on the orbit).
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
        if (L.kind() == RoundLayout::Kind::Dial) featureAgent(ui, L, f, h.agent);   // a dot: jump to it
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

static inline void roundSwipe(RoundUi& ui, const RoundLayout& L, const Fleet& f, Swipe s) {
  switch (ui.screen) {
    case RoundScreen::Home:
      if (L.kind() == RoundLayout::Kind::Dial) {
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

// One touch sample: `touching` with the finger at (x, y), or lifted. Returns
// what to send, if anything.
static inline RoundCommand roundTouch(RoundUi& ui, const RoundLayout& L, const Fleet& f, bool touching, int x, int y,
                                      RoundScope scope, int64_t nowEpoch, uint32_t ms) {
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
      return out;
    }
    ui.x = x;
    ui.y = y;
    rimMove(ui.rim, cx, cy, x, y);
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > kRoundSlop * kRoundSlop) ui.pressed = RoundHit();   // it became a drag
    // Stop: held long enough while still on the button.
    if (ui.pressed.kind == HitKind::Stop && !ui.holdFired && (uint32_t)(ms - ui.downMs) >= kRoundHoldMs) {
      ui.holdFired = true;
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
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (ui.rim.active && (ui.rim.turned >= kRimTurnMin || ui.rim.turned <= -kRimTurnMin)) {
    roundRim(ui, L, f, ui.rim.turned);
  } else if (adx <= kRoundSlop && ady <= kRoundSlop) {
    if (pressed.kind == HitKind::Stop) {
      if (!ui.holdFired) roundToast(ui, "hold to stop", ms);
    } else if ((uint32_t)(ms - ui.downMs) <= kRoundTapMaxMs || pressed.kind != HitKind::None) {
      out = roundTap(ui, L, f, L.hit(ui, f, ui.x0, ui.y0, scope, nowEpoch), scope, ms);
    }
  } else if (adx >= kRoundSwipeMin || ady >= kRoundSwipeMin) {
    roundSwipe(ui, L, f, adx >= ady ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down));
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
