#include <assert.h>
#include <string.h>
#include "timeutil.h"

static void age(int64_t s, const char* want) {
  char b[8];
  fmtAge(s, b, sizeof b);
  assert(strcmp(b, want) == 0);
}

int main() {
  assert(parseIso8601("1970-01-01T00:00:00Z") == 0);
  assert(parseIso8601("2026-10-08T01:02:03Z") == 1791421323);
  assert(parseIso8601("2026-10-08T01:02:03.134875Z") == 1791421323);
  // Go writes a numeric offset when the time is not UTC.
  assert(parseIso8601("2026-10-08T03:02:03+02:00") == 1791421323);
  assert(parseIso8601("2026-10-07T20:02:03.5-05:00") == 1791421323);
  assert(parseIso8601("2024-02-29T12:00:00Z") == 1709208000);   // leap day
  assert(parseIso8601("garbage") == 0);
  assert(parseIso8601("") == 0);
  assert(parseIso8601(nullptr) == 0);
  assert(parseIso8601("2026-13-08T01:02:03Z") == 0);

  age(-5, "0s");
  age(0, "0s");
  age(59, "59s");
  age(60, "1m");
  age(3599, "59m");
  age(3600, "1h");
  age(172799, "47h");
  age(172800, "2d");
  return 0;
}
