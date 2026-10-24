#pragma once
// The round icon buttons of an agent's card (focus, interrupt, stop, close), shared
// by the round and the square layouts: pixel icons in 4 px cells, an
// outline ring at rest, filled when pressed, and the 1 s hold ring on stop.
#include <stdint.h>
#include <string.h>

#include "canvas.h"
#include "round_geom.h"

enum class IconKind : uint8_t { Focus, Interrupt, Stop, Close };

namespace ui_buttons {

static const char* const kIconFocus[] = {"#########", "#.......#", "#.#.....#", "#..#....#", "#...#...#",
                                         "#..#....#", "#.#.###.#", "#.......#", "#########"};
static const char* const kIconPause[] = {"##.##", "##.##", "##.##", "##.##", "##.##", "##.##", "##.##"};
static const char* const kIconStop[] = {"#####", "#####", "#####", "#####", "#####"};
static const char* const kIconClose[] = {"#.....#", ".#...#.", "..#.#..", "...#...", "..#.#..", ".#...#.", "#.....#"};

static inline void icon(Canvas& c, const char* const* rows, int n, int cx, int cy, uint16_t col) {
  const int w = (int)strlen(rows[0]) * 4, h = n * 4;
  for (int r = 0; r < n; r++)
    for (int k = 0; rows[r][k]; k++)
      if (rows[r][k] == '#') c.rect(cx - w / 2 + k * 4, cy - h / 2 + r * 4, 4, 4, col);
}

}  // namespace ui_buttons

// One button of radius r centred on (cx, cy): idle (outlined), pressed
// (filled ink), stop held (filled accent with the hold ring filling over 1 s;
// `track` is the ring's unfilled colour).
static inline void iconButton(Canvas& c, int cx, int cy, int r, IconKind kind, bool pressed, int holdPermille,
                              uint16_t bg, uint16_t ink, uint16_t accent, uint16_t track) {
  using namespace ui_buttons;
  const bool stop = kind == IconKind::Stop;
  const uint16_t line = stop ? accent : ink;
  uint16_t fill = bg, ico = line;
  if (pressed) { fill = stop ? accent : ink; ico = bg; }
  fillDisc(c, cx, cy, r, fill);
  if (!pressed) strokeCircle(c, cx, cy, r - 1.5f, 3, line);
  if (kind == IconKind::Focus) icon(c, kIconFocus, 9, cx, cy, ico);
  else if (kind == IconKind::Interrupt) icon(c, kIconPause, 7, cx, cy, ico);
  else if (kind == IconKind::Close) icon(c, kIconClose, 7, cx, cy, ico);
  else icon(c, kIconStop, 5, cx, cy, ico);
  if (stop && pressed) {
    strokeCircle(c, cx, cy, r + 7, 5, track);
    if (holdPermille > 0) strokeArc(c, cx, cy, r + 7, 5, 0, 360.f * holdPermille / 1000, accent);
  }
}
