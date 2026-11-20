#pragma once
// Two show-offs for a board with a motion sensor (the 1.8" AMOLED's QMI8658):
//
//   magic   armed from the board's panel: the screen goes dark under the
//           header and a sphere of coloured dots turns there, leaning with
//           the board. A knock on the board (or a tap on the glass) and the
//           dots burst outwards, uncovering the agents. One knock, then it
//           disarms itself.
//   sick    shaken on an agent's card: its critter grows from its place to
//           the middle of the screen, goes green and throws up coloured
//           tokens that fall the way the board is held, then shrinks back.
//
// The firmware feeds accelerometer samples (in g) to funMotion(); the layout
// draws with funDraw*(). Pure C++, no allocation: the tests drive it.
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "panel_layout.h"

constexpr uint32_t kRevealMs = 650;          // the dots flying out
constexpr uint32_t kSickGrowMs = 450, kSickHoldMs = 500, kSickSpewMs = 2600, kSickShrinkMs = 450;
constexpr uint32_t kSickMs = kSickGrowMs + kSickHoldMs + kSickSpewMs + kSickShrinkMs;
constexpr int kTokens = 90;
// How the motion sensor's x and y lie on the screen (1 or -1 each): checked on the board.
constexpr float kImuSx = 1.f, kImuSy = -1.f;

// ---------------------------------------------------------------- detection

// A knock: the change in how hard the board is pulled (its acceleration's
// length, gravity filtered out) jumps past kKnockG, away from any shaking.
// A shake: kShakePeaks swings past kShakeG within kShakeWindowMs.
constexpr float kKnockG = 1.1f, kShakeG = 0.7f;
constexpr int kShakePeaks = 6;
constexpr uint32_t kShakeWindowMs = 1200, kKnockQuietMs = 450;

enum class Motion : uint8_t { None, Knock, Shake };

static inline Motion funDetect(FunUi& f, float ax, float ay, float az, uint32_t ms) {
  const float mag = sqrtf(ax * ax + ay * ay + az * az);
  if (!f.seeded) { f.base = mag; f.seeded = true; }
  f.base += (mag - f.base) * 0.05f;           // slow: gravity and how the board is held
  const float hp = mag - f.base;
  // Gravity's direction on the glass, smoothed: the tokens fall that way, the sphere leans.
  f.gx += (ax - f.gx) * 0.2f;
  f.gy += (ay - f.gy) * 0.2f;
  f.gz += (az - f.gz) * 0.2f;
  // Swings for the shake: a peak counts when |hp| crosses kShakeG.
  const bool high = hp > kShakeG || hp < -kShakeG;
  if (high && !f.wasHigh) {
    f.peaks[f.np % 8] = ms;
    f.np++;
  }
  f.wasHigh = high;
  int recent = 0;
  for (int i = 0; i < 8 && i < f.np; i++)
    if (ms - f.peaks[i] < kShakeWindowMs) recent++;
  if (recent >= kShakePeaks) {
    f.np = 0;                                  // one shake per burst of swings
    f.lastShake = ms ? ms : 1;
    return Motion::Shake;
  }
  if ((hp > kKnockG || hp < -kKnockG) && ms - f.lastKnock > kKnockQuietMs && (!f.lastShake || ms - f.lastShake > kShakeWindowMs) &&
      recent <= 2) {
    f.lastKnock = ms ? ms : 1;
    return Motion::Knock;
  }
  return Motion::None;
}

static inline void funReveal(FunUi& f, uint32_t ms) {
  f.magic = false;
  f.revealAt = ms ? ms : 1;
}

static inline bool funRevealing(const FunUi& f, uint32_t ms) { return f.revealAt && ms - f.revealAt < kRevealMs; }
static inline bool funSick(const FunUi& f, uint32_t ms) { return f.sickAt && ms - f.sickAt < kSickMs; }
static inline bool funBusy(const FunUi& f, uint32_t ms) { return f.magic || funRevealing(f, ms) || funSick(f, ms); }

// ---------------------------------------------------------------- the sphere

namespace fun_detail {

// A colour wheel for the dots and the tokens: the deck's hat colours and the accent.
static inline uint16_t wheel(int i) {
  static const uint16_t c[] = {rgb565(0xFF, 0x4D, 0x00), rgb565(0x4F, 0xB3, 0xA6), rgb565(0x6F, 0x8F, 0xD8),
                               rgb565(0xE8, 0xB9, 0x4A), rgb565(0x6D, 0xBE, 0x6A), rgb565(0xA6, 0x8B, 0xD8),
                               rgb565(0xE5, 0x8B, 0xB0), rgb565(0x5E, 0xC4, 0xD6)};
  return c[(unsigned)i % 8];
}

static inline uint16_t dim(uint16_t col, uint16_t bg, int a15) { return blend565(bg, col, (uint8_t)(a15 < 0 ? 0 : a15 > 15 ? 15 : a15)); }

constexpr int kDots = 260;

// A point of the sphere (a Fibonacci spiral: evenly spread), unit length.
static inline void dot(int i, float& x, float& y, float& z) {
  const float k = (i + 0.5f) / kDots;
  y = 1.f - 2.f * k;
  const float r = sqrtf(1.f - y * y);
  const float a = i * 2.39996323f;             // the golden angle
  x = cosf(a) * r;
  z = sinf(a) * r;
}

}  // namespace fun_detail

// The magic screen: dark from `top` down, the sphere turning in the middle,
// leaning as the board does. `burst` (0..1): the dots flying out (the reveal).
static inline void funSphere(Canvas& c, const FunUi& f, int top, uint16_t bg, uint32_t ms, float burst, bool clear) {
  using namespace fun_detail;
  const int w = c.w, h = c.h;
  if (clear) c.rect(0, top, w, h - top, bg);
  const float cx = w / 2.f, cy = top + (h - top) / 2.f, R = (w < h - top ? w : h - top) * 0.36f;
  const float spin = ms * 0.0011f;
  // Lean: the board's tilt turns the sphere a little (gx, gy in g).
  const float tx = f.gy * 0.6f, ty = -f.gx * 0.6f;
  const float cs = cosf(spin + ty), sn = sinf(spin + ty), ct = cosf(0.35f + tx), st = sinf(0.35f + tx);
  for (int i = 0; i < kDots; i++) {
    float x, y, z;
    dot(i, x, y, z);
    // Turn about the vertical, then tip towards the viewer.
    const float x1 = x * cs + z * sn, z1 = -x * sn + z * cs;
    const float y2 = y * ct - z1 * st, z2 = y * st + z1 * ct;
    float px = cx + x1 * R, py = cy + y2 * R;
    int a = (int)(6 + (z2 + 1.f) * 4.5f);      // nearer: brighter
    int s = z2 > 0.4f ? 6 : z2 > -0.3f ? 5 : 3;
    if (burst > 0) {                           // out from the middle, fading
      const float k = 1.f + burst * burst * 3.2f;
      px = cx + (px - cx) * k;
      py = cy + (py - cy) * k;
      a = (int)(a * (1.f - burst));
      s += (int)(burst * 3);
    }
    if (a <= 0) continue;
    c.rect((int)px - s / 2, (int)py - s / 2, s, s, dim(wheel(i / 7 + (int)(ms / 400)), bg, a));
  }
}

// ---------------------------------------------------------------- sick

// Where the sick critter is, its scale, and whether it is green and spewing.
struct SickPose {
  float x, y;     // top-left of its 18 x 13 box
  int scale;
  bool green, spew;
  float t;        // 0..1 within the current phase
};

// from: the critter's box on the card (x, y, scale); to: the middle, big.
static inline SickPose funSickPose(const FunUi& f, int w, int h, int fromX, int fromY, int fromScale, uint32_t ms) {
  const uint32_t t = ms - f.sickAt;
  const int big = w / 22;                      // about 18 cells across most of the glass
  const float toX = (w - 14 * big) / 2.f, toY = h * 0.30f;
  auto lerp = [](float a, float b, float k) { return a + (b - a) * k; };
  auto ease = [](float k) { return k < 0 ? 0.f : k > 1 ? 1.f : 1.f - (1.f - k) * (1.f - k) * (1.f - k); };
  SickPose p{toX, toY, big, true, false, 0};
  if (t < kSickGrowMs) {
    const float k = ease((float)t / kSickGrowMs);
    p = SickPose{lerp(fromX, toX, k), lerp(fromY, toY, k), (int)lroundf(lerp(fromScale, big, k)), false, false, k};
  } else if (t < kSickGrowMs + kSickHoldMs) {
    p.t = (float)(t - kSickGrowMs) / kSickHoldMs;
    p.x += sinf(t * 0.045f) * big * 0.6f;      // swaying, going green
  } else if (t < kSickGrowMs + kSickHoldMs + kSickSpewMs) {
    p.spew = true;
    p.t = (float)(t - kSickGrowMs - kSickHoldMs) / kSickSpewMs;
    p.y += sinf(t * 0.03f) * big * 0.3f;       // heaving
  } else {
    const float k = ease((float)(t - kSickGrowMs - kSickHoldMs - kSickSpewMs) / kSickShrinkMs);
    p = SickPose{lerp(toX, fromX, k), lerp(toY, fromY, k), (int)lroundf(lerp(big, fromScale, k)), k < 0.5f, false, k};
  }
  if (p.scale < 1) p.scale = 1;
  return p;
}

// The tokens: thrown from the mouth while it spews, falling the way the board
// is held (gravity from the accelerometer), bouncing off the edges. Stepped
// once a frame with the time since the last step.
static inline void funTokensStep(FunUi& f, const SickPose& p, int w, int h, uint32_t ms) {
  const float dt = f.lastStep ? (ms - f.lastStep) / 1000.f : 0.016f;
  f.lastStep = ms;
  if (dt <= 0 || dt > 0.1f) return;
  // Gravity on the glass: always half a g towards the bottom of the screen
  // (so they fall with the board lying flat), plus the board's tilt. The
  // sensor reads the push up the slope, so the tokens go the other way;
  // kImuSx and kImuSy map its axes onto the screen's.
  const float g = 1400.f;                      // px/s² for 1 g
  const float gx = -kImuSx * f.gx * g, gy = (0.5f - kImuSy * f.gy) * g;
  if (p.spew) {                                // the mouth: under the eyes, a little right of centre
    const float mx = p.x + 7.5f * p.scale, my = p.y + 5.f * p.scale;
    for (int k = 0; k < 3; k++) {
      Token& t = f.tok[f.nextTok++ % kTokens];
      const float a = 1.1f + (float)((f.nextTok * 37) % 100) / 100.f * 0.9f;   // a fan, down and out
      const float sp = 260.f + (float)((f.nextTok * 53) % 100) * 3.f;
      t.x = mx;
      t.y = my;
      t.vx = cosf(a) * sp * ((f.nextTok & 1) ? 1 : -1);
      t.vy = sinf(a) * sp;
      t.color = (uint8_t)(f.nextTok % 8);
      t.life = 255;
    }
  }
  for (Token& t : f.tok) {
    if (!t.life) continue;
    t.vx += gx * dt;
    t.vy += gy * dt;
    t.x += t.vx * dt;
    t.y += t.vy * dt;
    if (t.x < 4) { t.x = 4; t.vx = -t.vx * 0.55f; }
    if (t.x > w - 10) { t.x = w - 10; t.vx = -t.vx * 0.55f; }
    if (t.y < 4) { t.y = 4; t.vy = -t.vy * 0.55f; }
    if (t.y > h - 10) { t.y = h - 10; t.vy = -t.vy * 0.55f; t.vx *= 0.85f; }
    t.life = t.life > 3 ? t.life - 3 : 0;
  }
}

static inline void funTokensDraw(Canvas& c, const FunUi& f, uint16_t bg) {
  using namespace fun_detail;
  for (const Token& t : f.tok) {
    if (!t.life) continue;
    const int a = t.life > 120 ? 15 : t.life / 8;
    const uint16_t col = dim(wheel(t.color), bg, a);
    // A token: a little coin, a light pixel on top.
    c.rect((int)t.x, (int)t.y + 1, 7, 5, col);
    c.rect((int)t.x + 1, (int)t.y, 5, 7, col);
    c.rect((int)t.x + 2, (int)t.y + 2, 2, 2, dim(rgb565(0xF7, 0xF4, 0xEC), bg, a));
  }
}

static inline void funSickStart(FunUi& f, const char* agentId, uint32_t ms) {
  f.sickAt = ms ? ms : 1;
  f.lastStep = 0;
  snprintf(f.sickId, sizeof f.sickId, "%s", agentId);
  for (Token& t : f.tok) t.life = 0;
}
