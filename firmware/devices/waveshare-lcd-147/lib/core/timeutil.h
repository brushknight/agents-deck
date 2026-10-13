#pragma once
// Time helpers. The board has no clock of its own: "now" is the daemon's
// server.time plus the milliseconds since that state arrived.
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

// Days since 1970-01-01 for a civil date (Howard Hinnant's days_from_civil).
static inline int64_t daysFromCivil(int64_t y, unsigned m, unsigned d) {
  y -= m <= 2;
  const int64_t era = (y >= 0 ? y : y - 399) / 400;
  const unsigned yoe = (unsigned)(y - era * 400);
  const unsigned doy = (153 * (m + (m > 2 ? -3 : 9)) + 2) / 5 + d - 1;
  const unsigned doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  return era * 146097 + (int64_t)doe - 719468;
}

// RFC 3339 as Go writes it: "2026-10-08T01:02:03Z", with optional fractional
// seconds and either "Z" or a "+02:00" offset. Returns 0 for anything else.
static inline int64_t parseIso8601(const char* s) {
  if (!s) return 0;
  int y, mo, d, h, mi, se, used = 0;
  if (sscanf(s, "%4d-%2d-%2dT%2d:%2d:%2d%n", &y, &mo, &d, &h, &mi, &se, &used) != 6) return 0;
  if (mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || se > 60) return 0;
  int64_t t = daysFromCivil(y, (unsigned)mo, (unsigned)d) * 86400 + h * 3600 + mi * 60 + se;
  const char* p = s + used;
  if (*p == '.') {
    p++;
    while (*p >= '0' && *p <= '9') p++;
  }
  if (*p == '+' || *p == '-') {
    int oh, om;
    if (sscanf(p + 1, "%2d:%2d", &oh, &om) != 2) return 0;
    const int64_t off = oh * 3600 + om * 60;
    t += (*p == '+') ? -off : off;
  }
  return t;
}

// Same breakpoints as `rel` in the agents-deck web UI: 45s, 12m, 3h, 2d.
static inline void fmtAge(int64_t seconds, char* out, size_t cap) {
  if (seconds < 0) seconds = 0;
  if (seconds < 60) { snprintf(out, cap, "%ds", (int)seconds); return; }
  const int64_t m = seconds / 60;
  if (m < 60) { snprintf(out, cap, "%dm", (int)m); return; }
  const int64_t h = m / 60;
  if (h < 48) { snprintf(out, cap, "%dh", (int)h); return; }
  snprintf(out, cap, "%dd", (int)(h / 24));
}
