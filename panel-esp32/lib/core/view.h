#pragma once
// What to show for a fleet: which agent is the hero, which pose it has, and
// the text lines. No drawing here. The pose and wording tables follow the
// agents-deck web UI (backend/internal/web/static/app.js).
#include <ctype.h>
#include <stdio.h>
#include <string.h>

#include "model.h"

// ---------------------------------------------------------------- hero choice

// Higher rank wins the hero position.
static inline int rankOf(const Agent& a) {
  switch (a.status) {
    case Status::Waiting:  return 5;
    case Status::Error:    return 4;
    case Status::Idle:     return a.unseen ? 3 : 0;   // finished, not reviewed yet ("hungry")
    case Status::Running:  return 2;
    case Status::Starting: return 1;
    default:               return 0;
  }
}

// Index of the agent that deserves attention most, -1 for an empty fleet.
// Equal rank: the most recent status change wins.
static inline int autoHero(const Fleet& f) {
  int best = -1;
  for (int i = 0; i < f.count; i++) {
    if (best < 0) { best = i; continue; }
    const int r = rankOf(f.agents[i]), rb = rankOf(f.agents[best]);
    if (r > rb || (r == rb && f.agents[i].statusSince > f.agents[best].statusSince)) best = i;
  }
  return best;
}

static inline int findAgent(const Fleet& f, const char* id) {
  for (int i = 0; i < f.count; i++)
    if (!strcmp(f.agents[i].id, id)) return i;
  return -1;
}

constexpr uint32_t kHoldMs = 30000;

// The button pins one agent as hero for kHoldMs.
struct ViewState {
  char heldId[8] = "";
  bool hold = false;
  uint32_t holdUntilMs = 0;
};

static inline int resolveHero(const Fleet& f, ViewState& v, uint32_t nowMs) {
  if (v.hold) {
    const int i = findAgent(f, v.heldId);
    if (i >= 0 && (int32_t)(v.holdUntilMs - nowMs) > 0) return i;   // safe across millis() wrap
    v.hold = false;
  }
  return autoHero(f);
}

static inline void pressNext(const Fleet& f, ViewState& v, uint32_t nowMs) {
  if (f.count == 0) return;
  const int cur = resolveHero(f, v, nowMs);
  const int next = (cur + 1) % f.count;
  snprintf(v.heldId, sizeof v.heldId, "%s", f.agents[next].id);
  v.hold = true;
  v.holdUntilMs = nowMs + kHoldMs;
}

// True when `next` has a waiting agent that was not waiting in `prev`. The
// caller drops the button hold then, so a new prompt is never hidden.
static inline bool hasNewWaiting(const Fleet& prev, const Fleet& next) {
  for (int i = 0; i < next.count; i++) {
    if (next.agents[i].status != Status::Waiting) continue;
    const int j = findAgent(prev, next.agents[i].id);
    if (j < 0 || prev.agents[j].status != Status::Waiting) return true;
  }
  return false;
}

// ---------------------------------------------------------------- pose

enum class Pose : uint8_t {
  Think, Read, Write, Bash, Web, Plan, Delegate, Compact, Run,
  Wait, Err, Idle, Start, Exit, Celebrate, Hungry
};

constexpr int kCelebrateSec = 6;

static inline Pose poseOfTool(const char* tool) {
  char t[16];
  size_t i = 0;
  for (; tool[i] && i < sizeof t - 1; i++) t[i] = (char)tolower((unsigned char)tool[i]);
  t[i] = '\0';
  static const struct { const char* name; Pose pose; } kTable[] = {
      {"read", Pose::Read}, {"grep", Pose::Read}, {"glob", Pose::Read}, {"ls", Pose::Read},
      {"edit", Pose::Write}, {"write", Pose::Write}, {"multiedit", Pose::Write}, {"notebookedit", Pose::Write},
      {"bash", Pose::Bash}, {"bashoutput", Pose::Bash}, {"killshell", Pose::Bash}, {"killbash", Pose::Bash},
      {"webfetch", Pose::Web}, {"websearch", Pose::Web}, {"todowrite", Pose::Plan},
      {"task", Pose::Delegate}, {"agent", Pose::Delegate}, {"compact", Pose::Compact},
  };
  for (const auto& e : kTable)
    if (!strcmp(t, e.name)) return e.pose;
  return Pose::Run;
}

static inline Pose poseOf(const Agent& a, int64_t nowEpoch) {
  switch (a.status) {
    case Status::Running:  return a.actTool[0] ? poseOfTool(a.actTool) : Pose::Think;
    case Status::Waiting:  return Pose::Wait;
    case Status::Error:    return Pose::Err;
    case Status::Starting: return Pose::Start;
    case Status::Exited:   return Pose::Exit;
    case Status::Idle: {
      const int64_t age = nowEpoch - a.statusSince;
      if (a.statusSince && age >= 0 && age < kCelebrateSec) return Pose::Celebrate;
      return a.unseen ? Pose::Hungry : Pose::Idle;
    }
    default: return Pose::Idle;
  }
}

// ---------------------------------------------------------------- text

static inline int ctxPct(const Agent& a) {
  if (!a.ctxWindow) return 0;
  const uint64_t p = ((uint64_t)a.ctxUsed * 100 + a.ctxWindow / 2) / a.ctxWindow;
  return p > 100 ? 100 : (int)p;
}

#define DECK_DOT " \xC2\xB7 "   // " · "

// "running · 12m", "needs you · 3m", "hungry · 2m" …
static inline void statusLine(const Agent& a, int64_t nowEpoch, char* out, size_t cap) {
  const char* word = "";
  switch (a.status) {
    case Status::Running:  word = "running"; break;
    case Status::Waiting:  word = "needs you"; break;
    case Status::Error:    word = "error"; break;
    case Status::Idle:     word = a.unseen ? "hungry" : "idle"; break;
    case Status::Exited:   word = "exited"; break;
    case Status::Starting: word = "starting"; break;
    default: break;
  }
  if (!a.statusSince || a.status == Status::Starting || a.status == Status::Unknown) {
    snprintf(out, cap, "%s", word);
    return;
  }
  char age[8];
  fmtAge(nowEpoch - a.statusSince, age, sizeof age);
  snprintf(out, cap, "%s" DECK_DOT "%s", word, age);
}

// What the agent is doing or asking. Empty when there is nothing to say.
static inline void activityLine(const Agent& a, char* out, size_t cap) {
  out[0] = '\0';
  switch (a.status) {
    case Status::Running:
      if (!a.actTool[0]) snprintf(out, cap, "thinking");
      else if (a.actDetail[0]) snprintf(out, cap, "%s %s", a.actTool, a.actDetail);
      else snprintf(out, cap, "%s", a.actTool);
      break;
    case Status::Waiting:
      snprintf(out, cap, "%s", a.waitDetail[0] ? a.waitDetail : a.waitTitle[0] ? a.waitTitle : "needs input");
      break;
    case Status::Error:
      snprintf(out, cap, "%s", a.errMsg[0] ? a.errMsg : "error");
      break;
    default: break;
  }
}

struct Counts { uint8_t running, waiting, error, hungry; };

static inline Counts countFleet(const Fleet& f) {
  Counts c{0, 0, 0, 0};
  for (int i = 0; i < f.count; i++) {
    const Agent& a = f.agents[i];
    if (a.status == Status::Running) c.running++;
    else if (a.status == Status::Waiting) c.waiting++;
    else if (a.status == Status::Error) c.error++;
    else if (a.status == Status::Idle && a.unseen) c.hungry++;
  }
  return c;
}
