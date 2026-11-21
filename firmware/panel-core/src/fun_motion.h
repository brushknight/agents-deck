#pragma once
// Two show-offs for a board with a motion sensor (the 1.8" AMOLED's QMI8658):
//
//   magic   switched on in the board's panel, it stays on until it is switched
//           off there. On, the screen shows the header over an empty grid.
//           A knock on the board (it hitting a table, not being carried)
//           takes the agents: they fly in one after another from the top-right
//           corner, growing to their places, and from there it is the deck
//           as usual. The next knock gives them back: they fly out the same
//           way and the grid is empty again.
//   sick    shaken on an agent's card: its critter grows from its place into
//           the top third of the screen, over the card, dizzy (crossed
//           eyes), mouth open, and pours colour for 3 s: four columns of
//           squares as wide as its mouth falling straight down off the glass,
//           each column changing colour now and then (only the squares that
//           come out after the change take it); then it goes back.
//
// The firmware feeds accelerometer samples (in g) to funMotion(); the layout
// draws with funDraw*(). Pure C++, no allocation: the tests drive it.
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "panel_layout.h"

// The reveal: each tile flies kRevealTravelMs, the next starting kRevealStaggerMs later.
constexpr uint32_t kRevealTravelMs = 350, kRevealStaggerMs = 15;
constexpr uint32_t kRevealMs = kRevealTravelMs + 8 * kRevealStaggerMs;   // room for nine tiles
constexpr uint32_t kSickGrowMs = 450, kSickHoldMs = 250, kSickSpewMs = 3000, kSickShrinkMs = 450;
constexpr uint32_t kSickMs = kSickGrowMs + kSickHoldMs + kSickSpewMs + kSickShrinkMs;

// ---------------------------------------------------------------- detection

// A knock: the board hitting something stops it in a few milliseconds, so
// its acceleration's length jumps by more than kKnockJerkG between two
// samples (5 ms apart); picking it up or turning it changes far slower.
// A shake: kShakePeaks swings past kShakeG within kShakeWindowMs.
// A knock: the size of the acceleration changes by kKnockG within
// kKnockWindowMs (the board hitting the table, or a knuckle on it; carrying it
// changes it far slower). Needs the sensor read often (every 2 ms on the 1.8
// inch), or the jolt falls between two readings.
// And the board was still for kKnockStillMs before it (not shaken, not
// handled), and no finger touched the glass for kKnockTouchMs.
constexpr float kKnockG = 0.8f, kShakeG = 0.5f, kStillG = 0.25f;
constexpr uint32_t kKnockWindowMs = 10, kKnockStillMs = 300, kKnockTouchMs = 500;
constexpr int kShakePeaks = 4;   // two shakes back and forth
constexpr uint32_t kShakeWindowMs = 1000, kKnockQuietMs = 450;

enum class Motion : uint8_t { None, Knock, Shake };

static inline Motion funDetect(FunUi& f, float ax, float ay, float az, uint32_t ms) {
  const float mag = sqrtf(ax * ax + ay * ay + az * az);
  if (!f.seeded) {
    f.base = mag;
    for (int i = 0; i < 8; i++) { f.hist[i] = mag; f.histMs[i] = ms; }
    f.lastMs = ms;
    f.seeded = true;
  }
  // Smoothing by time, so it is the same at any sample rate.
  const float dt = (float)(ms - f.lastMs > 100 ? 100 : ms - f.lastMs);
  f.lastMs = ms;
  f.base += (mag - f.base) * (dt / 400.f);    // slow: gravity and how the board is held
  const float hp = mag - f.base;
  const float kg = dt / 40.f > 1.f ? 1.f : dt / 40.f;   // gravity's direction
  f.gx += (ax - f.gx) * kg;
  f.gy += (ay - f.gy) * kg;
  f.gz += (az - f.gz) * kg;
  // The biggest change from any sample in the last kKnockWindowMs.
  float jerk = 0;
  for (int i = 0; i < 8; i++) {
    if (ms - f.histMs[i] > kKnockWindowMs) continue;
    const float d = mag - f.hist[i];
    if (d > jerk || -d > jerk) jerk = d > 0 ? d : -d;
  }
  f.hist[f.hn % 8] = mag;
  f.histMs[f.hn % 8] = ms;
  f.hn++;
  // Swings for the shake: a peak counts when |hp| goes over kShakeG, and the
  // next one only after it fell back under 60% of it.
  const float ahp = hp < 0 ? -hp : hp;
  if (!f.wasHigh && ahp > kShakeG) {
    f.peaks[f.np % 8] = ms;
    f.np++;
    f.wasHigh = true;
  } else if (f.wasHigh && ahp < kShakeG * 0.6f) {
    f.wasHigh = false;
  }
  // Movement in bursts: when this one began, and when the one before ended.
  if (ahp > kStillG) {
    if (!f.lastMove || ms - f.lastMove > kKnockWindowMs) {
      f.prevMoveEnd = f.lastMove;
      f.burstAt = ms;
    }
    f.lastMove = ms;
  }
  const bool stillBefore = f.burstAt && ms - f.burstAt <= kKnockWindowMs && (!f.prevMoveEnd || f.burstAt - f.prevMoveEnd > kKnockStillMs);
  const bool untouched = !f.lastTouch || ms - f.lastTouch > kKnockTouchMs;
  int recent = 0;
  for (int i = 0; i < 8 && i < f.np; i++)
    if (ms - f.peaks[i] < kShakeWindowMs) recent++;
  if (recent >= kShakePeaks) {
    f.np = 0;                                  // one shake per burst of swings
    f.lastShake = ms ? ms : 1;
    return Motion::Shake;
  }
  (void)stillBefore;                          // (moving it onto the table is movement too: not asked)
  if (jerk > kKnockG && untouched && ms - f.lastKnock > kKnockQuietMs && (!f.lastShake || ms - f.lastShake > kShakeWindowMs) &&
      recent <= 2) {
    f.lastKnock = ms ? ms : 1;
    return Motion::Knock;
  }
  return Motion::None;
}

// A knock while the trick is on: take the agents, or give them back.
static inline void funKnock(FunUi& f, uint32_t ms) {
  f.taken = !f.taken;
  if (f.taken) { f.revealAt = ms ? ms : 1; f.returnAt = 0; }
  else { f.returnAt = ms ? ms : 1; f.revealAt = 0; f.spent = true; }   // one take, one give-back per switching on
}

static inline void funMagicOff(FunUi& f) { f.magic = f.taken = false; f.revealAt = f.returnAt = 0; }

static inline bool funRevealing(const FunUi& f, uint32_t ms) { return f.revealAt && ms - f.revealAt < kRevealMs; }
static inline bool funReturning(const FunUi& f, uint32_t ms) { return f.returnAt && ms - f.returnAt < kRevealMs; }

// How far tile k (in the order they come in) is on its way: 0 not yet, 1 landed.
static inline float funRevealT(const FunUi& f, int k, uint32_t ms) {
  const uint32_t at = f.revealAt ? f.revealAt : f.returnAt;
  const int32_t t = (int32_t)(ms - at) - (int32_t)(k * kRevealStaggerMs);
  if (t <= 0) return 0;
  if (t >= (int32_t)kRevealTravelMs) return 1;
  const float x = (float)t / kRevealTravelMs;
  return 1.f - (1.f - x) * (1.f - x) * (1.f - x);   // fast out of the corner, settling in
}
static inline bool funSick(const FunUi& f, uint32_t ms) { return f.sickAt && ms - f.sickAt < kSickMs; }
static inline bool funGiving(const FunUi& f, uint32_t ms) { return f.giveAt && ms - f.giveAt < kRevealMs; }
static inline bool funBack(const FunUi& f, uint32_t ms) { return f.backAt && ms - f.backAt < kRevealMs; }
static inline bool funBusy(const FunUi& f, uint32_t ms) {
  return funRevealing(f, ms) || funReturning(f, ms) || funSick(f, ms) || funGiving(f, ms) || funBack(f, ms);
}

// A tile's flight for tile k of a flight that started at 'at': 0 to 1, eased.
static inline float funFlightT(uint32_t at, int k, uint32_t ms) {
  const int32_t t = (int32_t)(ms - at) - (int32_t)(k * kRevealStaggerMs);
  if (t <= 0) return 0;
  if (t >= (int32_t)kRevealTravelMs) return 1;
  const float x = (float)t / kRevealTravelMs;
  return 1.f - (1.f - x) * (1.f - x) * (1.f - x);
}

// ---------------------------------------------------------------- sick

// Where the sick critter is and how big; whether it pours, and how far down.
struct SickPose {
  float x, y;     // top-left of its 18 x 13 box
  int scale;
  bool dizzy;     // crossed eyes and the open mouth
  float pour;     // 0: none; the stream's progress through the 3 s (0..1)
};

// from: the critter's box on the card (x, y, scale); to: the top third, big.
static inline SickPose funSickPose(const FunUi& f, int w, int h, int fromX, int fromY, int fromScale, uint32_t ms) {
  const uint32_t t = ms - f.sickAt;
  int big = w / 20 < h / 40 ? w / 20 : h / 40;   // the critter (and its hat) in the top third
  if (big < fromScale) big = fromScale;
  const float toX = (w - 14 * big) / 2.f, toY = 6.f;   // drawSprite's y is the top of its box, hat included
  auto lerp = [](float a, float b, float k) { return a + (b - a) * k; };
  auto ease = [](float k) { return k < 0 ? 0.f : k > 1 ? 1.f : 1.f - (1.f - k) * (1.f - k) * (1.f - k); };
  SickPose p{toX, toY, big, true, 0};
  if (t < kSickGrowMs) {
    const float k = ease((float)t / kSickGrowMs);
    p = SickPose{lerp(fromX, toX, k), lerp(fromY, toY, k), (int)lroundf(lerp(fromScale, big, k)), k > 0.5f, 0};
  } else if (t < kSickGrowMs + kSickHoldMs) {
    // grown, eyes crossed, about to go
  } else if (t < kSickGrowMs + kSickHoldMs + kSickSpewMs) {
    p.pour = (float)(t - kSickGrowMs - kSickHoldMs) / kSickSpewMs;
  } else {
    const float k = ease((float)(t - kSickGrowMs - kSickHoldMs - kSickSpewMs) / kSickShrinkMs);
    p = SickPose{lerp(toX, fromX, k), lerp(toY, fromY, k), (int)lroundf(lerp(big, fromScale, k)), k < 0.5f, 0};
  }
  if (p.scale < 1) p.scale = 1;
  return p;
}

// The mouth, in pixels: under the eyes, in the middle of the body (the
// critter's cells: x 5.5 to 8.5, y 3.7 to 5.3 under its box's top at -4),
// four stream columns wide.
struct Mouth {
  int x, y, w, h, cell;
};

static inline Mouth funMouth(const SickPose& p) {
  const int s = p.scale;
  const int cell = (3 * s + 2) / 4 < 2 ? 2 : (3 * s + 2) / 4;
  Mouth m;
  m.cell = cell;
  m.w = 4 * cell;
  m.x = (int)(p.x + 7.f * s) - m.w / 2;
  m.y = (int)(p.y + 7.7f * s);
  m.h = (int)(1.6f * s + 0.5f);
  return m;
}

namespace fun_detail {

static const uint16_t kPour[] = {rgb565(0xFF, 0x3B, 0x30), rgb565(0xFF, 0x95, 0x00), rgb565(0xFF, 0xD6, 0x0A),
                                 rgb565(0x34, 0xC7, 0x59), rgb565(0x0A, 0x84, 0xFF), rgb565(0x5E, 0x5C, 0xE6),
                                 rgb565(0xBF, 0x5A, 0xF2)};

static inline uint32_t rnd(uint32_t& x) {   // xorshift: the same pour every time for one shake
  x ^= x << 13;
  x ^= x >> 17;
  x ^= x << 5;
  return x;
}

// The colour column k pours at time t ms after it started: runs of 120 to
// 520 ms, each a colour unlike the last.
static inline uint16_t pourColor(uint32_t seed, int k, uint32_t t) {
  uint32_t x = seed * 2654435761u + (uint32_t)k * 40503u + 1u;
  int col = k * 2 % 7;
  uint32_t at = 0;
  for (int guard = 0; guard < 64; guard++) {
    at += 120 + rnd(x) % 400;
    if (at > t) break;
    col = (col + 1 + (int)(rnd(x) % 6)) % 7;
  }
  return kPour[col];
}

}  // namespace fun_detail

// The open mouth and the pour. A square leaves the mouth every cell / v ms
// and falls at v px/ms; one's colour is its column's at the moment it left.
static inline void funPour(Canvas& c, const SickPose& p, const FunUi& f, uint16_t mouth, uint32_t ms) {
  if (!p.dizzy) return;
  const Mouth m = funMouth(p);
  c.rect(m.x, m.y, m.w, m.h, mouth);
  const uint32_t start = f.sickAt + kSickGrowMs + kSickHoldMs, end = start + kSickSpewMs;
  if ((int32_t)(ms - start) < 0) return;
  constexpr float v = 0.5f;
  const uint32_t step = (uint32_t)(m.cell / v);
  const uint32_t last = (int32_t)(ms - end) < 0 ? ms : end;
  const int top = m.y + m.h / 2;
  for (uint32_t te = start + (last - start) / step * step;; te -= step) {
    const int y = top + (int)((ms - te) * v);
    if (y >= c.h) break;
    for (int k = 0; k < 4; k++) c.rect(m.x + k * m.cell, y, m.cell, m.cell, fun_detail::pourColor(f.sickAt, k, te - start));
    if (te < start + step) break;
  }
}

static inline void funSickStart(FunUi& f, const char* agentId, uint32_t ms) {
  f.sickAt = ms ? ms : 1;
  snprintf(f.sickId, sizeof f.sickId, "%s", agentId);
}
