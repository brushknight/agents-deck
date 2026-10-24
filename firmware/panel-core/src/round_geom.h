#pragma once
// Circle geometry for the round boards: polar placement, angles from touch
// points (for dragging round the rim), hit tests for discs and rings, and
// anti-aliased drawing of discs, rings, arcs and pills on a Canvas.
//
// Angles are in degrees, clockwise from 12 o'clock, as on a clock face:
// 0 is up, 90 right, 180 down, 270 left. Screen y grows downwards.
//
// Drawing walks one span per row (no per-pixel trigonometry outside the
// thin band of an arc's ends), so a full ring costs a few thousand pixels.
// Pure C++: runs on the host (tests, sim/render) as on the board.
#include <math.h>
#include <stdint.h>

#include "canvas.h"

constexpr float kRoundPi = 3.14159265358979f;

struct RPoint {
  int x, y;
};

// ---------------------------------------------------------------- angles

// Folds any angle into [0, 360).
static inline float normDeg(float a) {
  a = fmodf(a, 360.f);
  return a < 0 ? a + 360.f : a;
}

// The point at `deg` on a circle of radius r round (cx, cy), rounded to pixels.
static inline RPoint polar(float cx, float cy, float r, float deg) {
  const float rad = deg * kRoundPi / 180.f;
  return RPoint{(int)lroundf(cx + r * sinf(rad)), (int)lroundf(cy - r * cosf(rad))};
}

// The angle of (x, y) seen from (cx, cy); 0 for the centre itself.
static inline float angleAt(float cx, float cy, float x, float y) {
  const float dx = x - cx, dy = y - cy;
  if (dx == 0 && dy == 0) return 0;
  return normDeg(atan2f(dx, -dy) * 180.f / kRoundPi);
}

// The shortest turn from `from` to `to`, in (-180, 180]: positive is clockwise.
static inline float angleDelta(float from, float to) {
  float d = normDeg(to - from);
  return d > 180.f ? d - 360.f : d;
}

// True when `a` lies on the arc that starts at `start` and runs `sweep`
// degrees clockwise (a sweep of 360 or more is the whole circle).
static inline bool angleOnArc(float a, float start, float sweep) {
  if (sweep >= 360.f) return true;
  if (sweep <= 0) return false;
  return normDeg(a - start) <= sweep;
}

// ---------------------------------------------------------------- hit tests

static inline bool inCircle(float cx, float cy, float r, float x, float y) {
  const float dx = x - cx, dy = y - cy;
  return dx * dx + dy * dy <= r * r;
}

// Between the radii r0 <= d <= r1.
static inline bool inAnnulus(float cx, float cy, float r0, float r1, float x, float y) {
  const float dx = x - cx, dy = y - cy, d2 = dx * dx + dy * dy;
  return d2 >= r0 * r0 && d2 <= r1 * r1;
}

// Half the width of the circle's chord at row y (0 outside the circle).
static inline float chordHalf(float cy, float r, float y) {
  const float dy = y - cy;
  return dy * dy >= r * r ? 0.f : sqrtf(r * r - dy * dy);
}

// The widest box from y0 to y1 that stays inside the circle: its half width
// (the chord at whichever edge lies further from the centre).
static inline float chordHalfSpan(float cy, float r, float y0, float y1) {
  const float a = chordHalf(cy, r, y0), b = chordHalf(cy, r, y1);
  return a < b ? a : b;
}

// ---------------------------------------------------------------- drawing

namespace round_detail {

static inline void blendPx(Canvas& c, int x, int y, uint16_t col, int a15) {
  if (x < 0 || y < c.top || x >= c.w || y >= c.h || a15 <= 0) return;
  uint16_t& p = c.px[y * c.w + x];
  p = blend565(p, col, (uint8_t)(a15 > 15 ? 15 : a15));
}

// Coverage (0..15) of a pixel whose centre is `d` from the centre, for a
// ring between r0 and r1 (r0 may be 0 for a disc).
static inline int ringCover(float d, float r0, float r1) {
  float cov = 1.f;
  if (d > r1 - 0.5f) cov = r1 + 0.5f - d;
  if (r0 > 0 && d < r0 + 0.5f) {
    const float inner = d - (r0 - 0.5f);
    if (inner < cov) cov = inner;
  }
  if (cov <= 0) return 0;
  if (cov >= 1) return 15;
  return (int)(cov * 15.f + 0.5f);
}

}  // namespace round_detail

// A ring between radii r0 and r1 (a disc when r0 <= 0), restricted to the
// arc from `start` clockwise over `sweep` degrees, anti-aliased on the
// circles. The arc's ends are cut square along the radius.
static inline void fillArc(Canvas& c, float cx, float cy, float r0, float r1, float start, float sweep,
                           uint16_t col) {
  using namespace round_detail;
  if (r1 <= 0 || sweep <= 0) return;
  if (r0 < 0) r0 = 0;
  const bool full = sweep >= 360.f;
  int yTop = (int)floorf(cy - r1 - 1), yBot = (int)ceilf(cy + r1 + 1);
  if (yTop < c.top) yTop = c.top;
  if (yBot > c.h - 1) yBot = c.h - 1;
  const float ro2 = (r1 + 0.5f) * (r1 + 0.5f);
  const float ri2 = r0 > 0.5f ? (r0 - 0.5f) * (r0 - 0.5f) : -1.f;
  for (int y = yTop; y <= yBot; y++) {
    const float dy = y + 0.5f - cy;
    const float dy2 = dy * dy;
    if (dy2 > ro2) continue;
    const float xo = sqrtf(ro2 - dy2);
    // The inner hole on this row, if any.
    float xi = -1.f;
    if (ri2 > 0 && dy2 < ri2) xi = sqrtf(ri2 - dy2);
    int x0 = (int)floorf(cx - xo), x1 = (int)ceilf(cx + xo);
    if (x0 < 0) x0 = 0;
    if (x1 > c.w - 1) x1 = c.w - 1;
    for (int x = x0; x <= x1; x++) {
      const float dx = x + 0.5f - cx;
      if (xi > 1.5f && dx > -xi + 1.f && dx < xi - 1.f) {   // skip the hole in one go
        x = (int)floorf(cx + xi - 1.f);
        continue;
      }
      const float d = sqrtf(dx * dx + dy2);
      const int a = ringCover(d, r0, r1);
      if (!a) continue;
      if (!full && !angleOnArc(angleAt(0, 0, dx, dy), start, sweep)) continue;
      blendPx(c, x, y, col, a);
    }
  }
}

// The part of a disc between rows y0 and y1 (y1 exclusive): a band whose
// ends follow the circle, anti-aliased along the curve.
static inline void fillDiscRows(Canvas& c, float cx, float cy, float r, int y0, int y1, uint16_t col) {
  using namespace round_detail;
  if (y0 < c.top) y0 = c.top;
  if (y1 > c.h) y1 = c.h;
  for (int y = y0; y < y1; y++) {
    const float dy = y + 0.5f - cy;
    const float h = chordHalf(0, r + 0.5f, dy);
    if (h <= 0) continue;
    int x0 = (int)floorf(cx - h), x1 = (int)ceilf(cx + h);
    if (x0 < 0) x0 = 0;
    if (x1 > c.w - 1) x1 = c.w - 1;
    for (int x = x0; x <= x1; x++) {
      const float dx = x + 0.5f - cx;
      const int a = ringCover(sqrtf(dx * dx + dy * dy), 0, r);
      if (a == 15) {                                 // the solid middle in one go
        const int xr = (int)floorf(cx + chordHalf(0, r - 0.5f, dy) - 0.5f);
        if (xr > x) { c.rect(x, y, xr - x, 1, col); x = xr - 1; continue; }
      }
      blendPx(c, x, y, col, a);
    }
  }
}

// A filled disc.
static inline void fillDisc(Canvas& c, float cx, float cy, float r, uint16_t col) {
  fillArc(c, cx, cy, 0, r, 0, 360, col);
}

// A circle outline `t` pixels thick, centred on radius r.
static inline void strokeCircle(Canvas& c, float cx, float cy, float r, float t, uint16_t col) {
  fillArc(c, cx, cy, r - t / 2, r + t / 2, 0, 360, col);
}

// An arc `t` pixels thick centred on radius r, from `start` over `sweep`.
static inline void strokeArc(Canvas& c, float cx, float cy, float r, float t, float start, float sweep, uint16_t col) {
  fillArc(c, cx, cy, r - t / 2, r + t / 2, start, sweep, col);
}

// A dashed circle: `count` dashes of `dashDeg` degrees, evenly spaced.
static inline void dashedCircle(Canvas& c, float cx, float cy, float r, float t, int count, float dashDeg,
                                uint16_t col) {
  for (int i = 0; i < count; i++) strokeArc(c, cx, cy, r, t, 360.f * i / count, dashDeg, col);
}

// Paints everything outside the circle with `col`, anti-aliased on the edge:
// the round panel's mask, so a frame looks on the host as it does on the glass.
static inline void maskOutside(Canvas& c, float cx, float cy, float r, uint16_t col) {
  for (int y = c.top; y < c.h; y++) {
    const float dy = y + 0.5f - cy;
    for (int x = 0; x < c.w; x++) {
      const float dx = x + 0.5f - cx;
      const float d = sqrtf(dx * dx + dy * dy);
      if (d < r - 0.5f) {                           // inside: jump to the far edge of this row
        const float h = chordHalf(0, r - 0.5f, dy);
        const int xr = (int)floorf(cx + h) - 1;
        if (xr > x) x = xr;
        continue;
      }
      const float out = d - (r - 0.5f);              // 0..1 on the edge pixel
      const int a = out >= 1 ? 15 : (int)(out * 15.f + 0.5f);
      round_detail::blendPx(c, x, y, col, a);
    }
  }
}

// A pill: a rectangle with fully rounded ends (radius h / 2), filled.
static inline void fillPill(Canvas& c, int x, int y, int w, int h, uint16_t col) {
  const float r = h / 2.f;
  if (w < h) w = h;
  fillDisc(c, x + r, y + r, r, col);
  fillDisc(c, x + w - r, y + r, r, col);
  c.rect((int)lroundf(x + r), y, (int)lroundf(w - 2 * r), h, col);
}

// A pill's outline, `t` pixels thick, inside the box.
static inline void strokePill(Canvas& c, int x, int y, int w, int h, int t, uint16_t col) {
  const float r = h / 2.f;
  if (w < h) w = h;
  strokeArc(c, x + r, y + r, r - t / 2.f, (float)t, 180, 180, col);       // the left end
  strokeArc(c, x + w - r, y + r, r - t / 2.f, (float)t, 0, 180, col);     // the right end
  c.rect((int)lroundf(x + r), y, (int)lroundf(w - 2 * r), t, col);
  c.rect((int)lroundf(x + r), y + h - t, (int)lroundf(w - 2 * r), t, col);
}

// ---------------------------------------------------------------- rim drag

// Follows a finger round the rim: feed every touch sample, read the turn so
// far. A drag counts only when it starts in the rim band (r0..r1); the
// angle is measured from the screen's centre, so it works anywhere round.
struct RimDrag {
  bool active = false;
  float last = 0;     // the finger's angle at the previous sample
  float turned = 0;   // degrees turned since the drag began, clockwise positive
};

// Start (or not) a drag at a touch-down.
static inline void rimBegin(RimDrag& d, float cx, float cy, float r0, float r1, int x, int y) {
  d.active = inAnnulus(cx, cy, r0, r1, (float)x, (float)y);
  d.turned = 0;
  d.last = angleAt(cx, cy, (float)x, (float)y);
}

// A finger move. Returns the degrees turned by this move.
static inline float rimMove(RimDrag& d, float cx, float cy, int x, int y) {
  if (!d.active) return 0;
  // Too near the centre the angle jumps about: ignore those samples.
  if (inCircle(cx, cy, 24, (float)x, (float)y)) return 0;
  const float a = angleAt(cx, cy, (float)x, (float)y);
  const float step = angleDelta(d.last, a);
  d.last = a;
  d.turned += step;
  return step;
}
