#pragma once
// The spring carousel: the round home where the agents sit on a helix and
// the screen shows one turn of it. The selected agent is at 12 o'clock, the
// seam; the next K - 1 agents follow it clockwise round the ring. Turning by
// one step moves every agent one slot: at the seam one agent rises from
// under the selected one and grows into its place, and one dives under it,
// shrinking and fading. Up to K agents the ring is a closed loop instead,
// spread evenly, and nothing dives.
//
// This file is the geometry (where an agent at a slot coordinate draws) and
// the motion (where the ring is at a moment: at rest, easing to a slot, or
// following a finger). Drawing is layout_spring.h's; touch is round_view.h's.
// Numbers are from the design (spring carousel, notes board), in px on the
// 480 x 480 panel, angles clockwise from 12 o'clock.
//
// Pure C++: runs on the host (tests, sim/render) as on the board.
#include <math.h>
#include <stdint.h>

struct SpringGeom {
  int K;                  // agents on a turn
  int sel, ring, peek;    // critter scales: the selected one, the ring, under the seam
  float rSel;             // the selected one's radius (at 12 o'clock)
  float rOut, rIn;        // the turn's spiral: just right of the seam, just left of it
  float rUnder, peekAng;  // where dives and rises pass under the seam
  float loopR;            // up to K agents: the closed loop's radius
  int nameBase, phraseBase, metaBase, barY, barW;   // the middle
  float dotsR, dotsSpread;                          // past K: the dots round 6 o'clock
};

// The Orbit's (2.8″): nine to a turn at x3, the selected one x5; the middle
// sits lower, between the ring's 80° and 120° slots, where it is widest.
static inline SpringGeom springOrbit28() {
  SpringGeom g{};
  g.K = 9; g.sel = 5; g.ring = 3; g.peek = 1;
  g.rSel = 132; g.rOut = 188; g.rIn = 178; g.rUnder = 104; g.peekAng = 14; g.loopR = 180;
  g.nameBase = 268; g.phraseBase = 304; g.metaBase = 336; g.barY = 360; g.barW = 140;
  g.dotsR = 233; g.dotsSpread = 30;
  return g;
}

// The Dial's (2.1″): seven to a turn at x4, the selected one x6 (the same 7 mm critters).
static inline SpringGeom springDial21() {
  SpringGeom g{};
  g.K = 7; g.sel = 6; g.ring = 4; g.peek = 2;
  g.rSel = 140; g.rOut = 184; g.rIn = 174; g.rUnder = 108; g.peekAng = 14; g.loopR = 178;
  g.nameBase = 216; g.phraseBase = 254; g.metaBase = 290; g.barY = 314; g.barW = 120;
  g.dotsR = 233; g.dotsSpread = 26;
  return g;
}

namespace spring_detail {

static inline float mixf(float a, float b, float u) { return a + (b - a) * u; }
static inline int iround(float x) { return (int)floorf(x + 0.5f); }
// Opacity in four steps (25, 50, 75, 100 %), as a 4-bit alpha (0..15).
static inline int q4alpha(float x) {
  int q = iround(x * 4);
  if (q < 0) q = 0;
  if (q > 4) q = 4;
  static const int kA[5] = {0, 4, 8, 11, 15};
  return kA[q];
}
static inline float fmodPos(float a, float n) {
  float r = fmodf(a, n);
  return r < 0 ? r + n : r;
}

}  // namespace spring_detail

// Where an agent draws: its body centre, scale, opacity (0..15) and depth
// (higher draws on top).
struct SpringPlace {
  bool shown = false;
  float x = 0, y = 0;
  int scale = 0;
  int alpha = 0;
  int z = 0;
};

static inline void springPolar(float r, float deg, float& x, float& y) {
  const float t = (deg - 90.f) * 3.14159265358979f / 180.f;
  x = 240.f + r * cosf(t);
  y = 240.f + r * sinf(t);
}

// The visible turn is a gentle spiral: widest just right of the seam, narrowest just left of it.
static inline float springSpiral(const SpringGeom& g, float s) {
  return spring_detail::mixf(g.rOut, g.rIn, (s - 1) / (g.K - 2));
}

// Positions round a closed loop of n agents (n <= K): at least three, two face each other.
static inline float springLoopAngle(int n) { return n >= 3 ? 360.f / n : n == 2 ? 180.f : 0.f; }

// Where an agent at slot coordinate s draws, with n agents in all.
// s = 0 is the seam (selected); 1..K-1 the ring clockwise; (K-1, K) diving
// under the seam; (-1, 0) rising from under it. Past those it is hidden.
static inline SpringPlace springPlace(const SpringGeom& g, float s, int n) {
  using namespace spring_detail;
  SpringPlace p;
  const int K = g.K;
  if (n <= K) {                     // a closed loop: no seam, spread evenly
    const int m = n < 1 ? 1 : n;
    s = fmodPos(s, (float)m);
    const float d = s < m - s ? s : m - s;
    const float u = d < 1 ? d : 1;
    const float ang = s * springLoopAngle(n);
    springPolar(mixf(g.rSel, g.loopR, u), ang, p.x, p.y);
    p.scale = iround(mixf((float)g.sel, (float)g.ring, u));
    p.alpha = 15;
    p.z = d < 0.5f ? 3 : 2;
    p.shown = true;
    return p;
  }
  if (s <= -1 || s >= K) return p;
  const float pa = g.peekAng;
  if (s < 0) {                      // rising from under the seam
    const float u = s + 1;
    springPolar(mixf(g.rUnder, g.rSel, u), mixf(pa, 0, u), p.x, p.y);
    p.scale = iround(mixf((float)g.peek, (float)g.sel, u));
    p.alpha = q4alpha(u);
    p.z = u > 0.6f ? 4 : 1;
  } else if (s <= 1) {              // the seam to the first ring slot
    springPolar(mixf(g.rSel, springSpiral(g, 1), s), s * 360.f / K, p.x, p.y);
    p.scale = iround(mixf((float)g.sel, (float)g.ring, s));
    p.alpha = 15;
    p.z = 3;
  } else if (s <= K - 1) {          // on the ring
    springPolar(springSpiral(g, s), s * 360.f / K, p.x, p.y);
    p.scale = g.ring;
    p.alpha = 15;
    p.z = 2;
  } else {                          // diving under the seam
    const float u = s - (K - 1);
    springPolar(mixf(springSpiral(g, (float)(K - 1)), g.rUnder, u), mixf((K - 1) * 360.f / K, 360 - pa, u), p.x, p.y);
    p.scale = iround(mixf((float)g.ring, (float)g.peek, u));
    p.alpha = q4alpha(1 - u);
    p.z = 1;
  }
  p.shown = p.alpha > 0;
  return p;
}

// The slot coordinate of agent j when the ring stands at `pos` (a selection
// that may be fractional and runs past n: the spring wraps).
static inline float springSlot(const SpringGeom& g, int j, float pos, int n) {
  using namespace spring_detail;
  if (n <= 0) return 0;
  float d = fmodPos(j - pos, (float)n);
  if (n > g.K && d > n / 2.f + 2) d -= n;
  return d;
}

// The agent index at a ring position.
static inline int springIndex(float pos, int n) {
  if (n <= 0) return -1;
  const int i = spring_detail::iround(pos) % n;
  return i < 0 ? i + n : i;
}

// Agents on one turn: K, or all of them on a closed loop.
static inline int springTurn(const SpringGeom& g, int n) { return n > g.K ? g.K : n; }

// Degrees round the rim for one step.
static inline float springStepDeg(const SpringGeom& g, int n) {
  return n > g.K ? 360.f / g.K : springLoopAngle(n);
}

// ---------------------------------------------------------------- motion

constexpr uint32_t kSpringStepMs = 300;      // one step, ease in-out
constexpr uint32_t kSpringFastMs = 120;      // a step of a fling or a jump to a far agent
constexpr uint32_t kSpringWaiterMs = 10000;  // untouched this long: turn to a waiter by itself

// Where the ring is. `pos` is the selected agent's index (it runs past the
// fleet's size, both ways: read it modulo the count).
struct SpringMotion {
  bool placed = false;      // the ring has been put on an agent
  float from = 0, to = 0;   // an animation from `from` to `to`, or at rest on `to`
  uint32_t t0 = 0, dur = 0; // dur 0: at rest
  bool dragging = false;    // a finger turns it: at `drag`
  float dragBase = 0, drag = 0;
  float vel = 0;            // the finger's speed round the rim, degrees per ms (clockwise positive)
  uint32_t lastMoveMs = 0;
  char pendingId[12] = "";  // a new waiter to turn to once the board is left alone
};

static inline float springEase(float u) {
  if (u <= 0) return 0;
  if (u >= 1) return 1;
  if (u < 0.5f) return 4 * u * u * u;
  const float v = -2 * u + 2;
  return 1 - v * v * v / 2;
}

static inline float springPos(const SpringMotion& m, uint32_t ms) {
  if (m.dragging) return m.drag;
  if (!m.dur) return m.to;
  const uint32_t dt = ms - m.t0;
  if (dt >= m.dur) return m.to;
  return m.from + (m.to - m.from) * springEase((float)dt / m.dur);
}

static inline bool springMoving(const SpringMotion& m, uint32_t ms) {
  return m.dragging || (m.dur && ms - m.t0 < m.dur);
}

// How long a turn of `steps` takes: 300 ms for one, 120 ms a step beyond.
static inline uint32_t springDuration(float steps) {
  if (steps < 0) steps = -steps;
  const uint32_t fast = (uint32_t)(steps * kSpringFastMs + 0.5f);
  return fast > kSpringStepMs ? fast : kSpringStepMs;
}

// Turns from wherever it is now to `to`.
static inline void springGo(SpringMotion& m, float to, uint32_t ms, uint32_t dur) {
  m.from = springPos(m, ms);
  m.dragging = false;
  m.to = to;
  m.t0 = ms;
  m.dur = m.from == to ? 0 : dur;
}

// Puts the ring on agent i at once.
static inline void springSet(SpringMotion& m, float i) {
  m.from = m.to = i;
  m.dur = 0;
  m.dragging = false;
  m.placed = true;
}

// The shortest way from agent `from` to agent `to` on a ring of n, in steps (negative: backwards).
static inline int springShortest(int from, int to, int n) {
  if (n <= 0) return 0;
  int d = ((to - from) % n + n) % n;
  if (d > n / 2) d -= n;
  return d;
}
