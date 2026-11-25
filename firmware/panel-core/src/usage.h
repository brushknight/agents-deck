#pragma once
// Usage limits on the panels (usage_limits.dart): the deck's corner readout
// and an agent's usage row. Pure C++.
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "deck_ui.h"
#include "model.h"
#include "sprites.h"

// The week of a set: the window labelled "week", else its first.
static inline const LimitWin* usageWeek(const LimitSet& s) {
  for (int i = 0; i < s.count; i++)
    if (!strcmp(s.win[i].label, "week")) return &s.win[i];
  return s.count ? &s.win[0] : nullptr;
}

// "work", "claude" (one account), "codex".
static inline const char* usageName(const LimitSet& s) {
  if (s.tool == ToolKind::Codex) return "codex";
  return s.account[0] ? s.account : "claude";
}

// The colour that marks a set: its account's hat colour (from an agent that
// runs in it), else its tool's colour.
static inline uint16_t usageColor(const Theme& t, const Fleet& f, const LimitSet& s) {
  if (s.tool == ToolKind::Claude && s.account[0])
    for (int i = 0; i < f.count; i++)
      if (!strcmp(f.agents[i].account, s.account) && f.agents[i].accountColor < kHatColorCount)
        return sprite_detail::kHatPalette[f.agents[i].accountColor][0];
  return toolColor(t, s.tool);
}

// The two sets the deck's corner shows: the Claude account in use (the one
// most agents run in, busy ones first), then the other set closest to its
// limit. Pending Grigorii's answer on which two to show: change it here (and
// cornerSources in usage_limits.dart) only. Returns how many (0 to 2).
static inline int usageCorner(const Fleet& f, int out[2]) {
  if (!f.limitCount) return 0;
  auto score = [&](const LimitSet& s) {
    int n = 0;
    for (int i = 0; i < f.count; i++) {
      const Agent& a = f.agents[i];
      if (a.tool != s.tool || strcmp(a.account, s.account)) continue;
      n += a.status == Status::Running || a.status == Status::Waiting ? 1000 : 1;
    }
    return n;
  };
  bool anyClaude = false;
  for (int i = 0; i < f.limitCount; i++) anyClaude |= f.limits[i].tool == ToolKind::Claude;
  int inUse = -1;
  for (int i = 0; i < f.limitCount; i++) {
    if (anyClaude && f.limits[i].tool != ToolKind::Claude) continue;
    if (inUse < 0 || score(f.limits[i]) > score(f.limits[inUse])) inUse = i;
  }
  auto nearness = [&](const LimitSet& s) {
    const LimitWin* w = usageWeek(s);
    if (!w || w->stale) return -1;
    return (int)w->level * 1000 + w->used;
  };
  int next = -1;
  for (int i = 0; i < f.limitCount; i++) {
    if (i == inUse) continue;
    if (next < 0 || nearness(f.limits[i]) > nearness(f.limits[next])) next = i;
  }
  out[0] = inUse;
  if (next < 0) return 1;
  out[1] = next;
  return 2;
}

// When a limit comes back, short, in the deck's local time: "16:40" today,
// "thu 09:00" within the week, "26 nov" later (resetText).
static inline void usageWhen(int64_t at, int64_t now, int32_t utcOffset, char* out, size_t cap) {
  static const char* const days[] = {"sun", "mon", "tue", "wed", "thu", "fri", "sat"};
  static const char* const months[] = {"jan", "feb", "mar", "apr", "may", "jun",
                                       "jul", "aug", "sep", "oct", "nov", "dec"};
  const time_t lt = (time_t)(at + utcOffset), ln = (time_t)(now + utcOffset);
  struct tm t, n;
  gmtime_r(&lt, &t);
  gmtime_r(&ln, &n);
  if (t.tm_year == n.tm_year && t.tm_yday == n.tm_yday) snprintf(out, cap, "%02d:%02d", t.tm_hour, t.tm_min);
  else if (at - now < 156 * 3600) snprintf(out, cap, "%s %02d:%02d", days[t.tm_wday], t.tm_hour, t.tm_min);
  else snprintf(out, cap, "%d %s", t.tm_mday, months[t.tm_mon]);
}

// A window's value: "86%", "out", "out · thu 06:34" (withWhen), "-" (reset
// since the last report).
static inline void usageValue(const LimitWin& w, bool withWhen, int64_t now, int32_t utcOffset, char* out,
                              size_t cap) {
  if (w.stale) { snprintf(out, cap, "-"); return; }
  if (w.out()) {
    if (withWhen && w.resetsAt) {
      char when[16];
      usageWhen(w.resetsAt, now, utcOffset, when, sizeof when);
      snprintf(out, cap, "out" DECK_DOT "%s", when);
    } else snprintf(out, cap, "out");
    return;
  }
  snprintf(out, cap, "%u%%", (unsigned)w.used);
}
