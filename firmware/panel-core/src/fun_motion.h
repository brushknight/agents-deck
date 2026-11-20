#pragma once
// Two show-offs for a board with a motion sensor (the 1.8" AMOLED's QMI8658):
//
//   magic   armed from the board's panel: the screen shows an empty grid, no
//           agents, no header. A knock on the board (it hitting something,
//           even lightly), or a tap on the glass, and the agents fly in one
//           after another from the top-right corner, growing to their places,
//           and from there it is the deck as usual. One knock, then it
//           disarms itself.
//   sick    shaken on an agent's card: its critter grows from its place into
//           the top third of the screen, over the card, dizzy (crossed
//           eyes), and a rainbow of pixels pours from its mouth straight down
//           to the bottom of the glass for 3 s; then it goes back.
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
constexpr uint32_t kRevealTravelMs = 520, kRevealStaggerMs = 90;
constexpr uint32_t kRevealMs = kRevealTravelMs + 8 * kRevealStaggerMs;   // room for nine tiles
constexpr uint32_t kSickGrowMs = 450, kSickHoldMs = 250, kSickSpewMs = 3000, kSickShrinkMs = 450;
constexpr uint32_t kSickMs = kSickGrowMs + kSickHoldMs + kSickSpewMs + kSickShrinkMs;

// ---------------------------------------------------------------- detection

// A knock: the board hitting something stops it in a few milliseconds, so
// its acceleration's length jumps by more than kKnockJerkG between two
// samples (5 ms apart); picking it up or turning it changes far slower.
// A shake: kShakePeaks swings past kShakeG within kShakeWindowMs.
constexpr float kKnockJerkG = 0.35f, kShakeG = 0.7f;
constexpr int kShakePeaks = 6;
constexpr uint32_t kShakeWindowMs = 1200, kKnockQuietMs = 450;

enum class Motion : uint8_t { None, Knock, Shake };

static inline Motion funDetect(FunUi& f, float ax, float ay, float az, uint32_t ms) {
  const float mag = sqrtf(ax * ax + ay * ay + az * az);
  if (!f.seeded) { f.base = f.prevMag = mag; f.seeded = true; }
  const float jerk = mag - f.prevMag;
  f.prevMag = mag;
  f.base += (mag - f.base) * 0.05f;           // slow: gravity and how the board is held
  const float hp = mag - f.base;
  // Gravity's direction, smoothed.
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
  if ((jerk > kKnockJerkG || jerk < -kKnockJerkG) && ms - f.lastKnock > kKnockQuietMs && (!f.lastShake || ms - f.lastShake > kShakeWindowMs) &&
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

// How far tile k (in the order they come in) is on its way: 0 not yet, 1 landed.
static inline float funRevealT(const FunUi& f, int k, uint32_t ms) {
  const int32_t t = (int32_t)(ms - f.revealAt) - (int32_t)(k * kRevealStaggerMs);
  if (t <= 0) return 0;
  if (t >= (int32_t)kRevealTravelMs) return 1;
  const float x = (float)t / kRevealTravelMs;
  return 1.f - (1.f - x) * (1.f - x) * (1.f - x);   // fast out of the corner, settling in
}
static inline bool funSick(const FunUi& f, uint32_t ms) { return f.sickAt && ms - f.sickAt < kSickMs; }
static inline bool funBusy(const FunUi& f, uint32_t ms) { return f.magic || funRevealing(f, ms) || funSick(f, ms); }

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
  const float toX = (w - 14 * big) / 2.f, toY = 6.f * big + 6.f;   // its hat reaches 6 cells above its box
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

// The open mouth (in the critter's cells: under the eyes, between them) and,
// while it pours, the rainbow: bands of colour a cell high, falling from the
// mouth to the bottom of the glass. It reaches the bottom in its first tenth,
// runs, and in its last tenth the end leaves the mouth and drops away.
static inline void funPour(Canvas& c, const SickPose& p, uint16_t mouth, uint32_t ms) {
  const int s = p.scale;
  const int mx = (int)(p.x + 5.5f * s), my = (int)(p.y + 3.f * s), mw = 3 * s, mh = 2 * s;
  if (!p.dizzy) return;
  c.rect(mx, my, mw, mh, mouth);
  if (p.pour <= 0) return;
  static const uint16_t kRainbow[] = {rgb565(0xFF, 0x3B, 0x30), rgb565(0xFF, 0x95, 0x00), rgb565(0xFF, 0xD6, 0x0A),
                                      rgb565(0x34, 0xC7, 0x59), rgb565(0x0A, 0x84, 0xFF), rgb565(0x5E, 0x5C, 0xE6),
                                      rgb565(0xBF, 0x5A, 0xF2)};
  const int top0 = my + mh - s / 2, bottom = c.h;
  const float run = p.pour < 0.1f ? p.pour / 0.1f : 1.f;              // reaching down
  const float gone = p.pour > 0.9f ? (p.pour - 0.9f) / 0.1f : 0.f;   // the end leaving
  const int y1 = top0 + (int)((bottom - top0) * run);
  const int y0 = top0 + (int)((bottom - top0) * gone);
  const int band = s < 4 ? 4 : s;                                     // one critter cell
  const int shift = (int)(ms * 0.45f) % (band * 7);                   // the colours run down
  for (int y = y0 - ((y0 - top0 + band * 7 - shift) % band); y < y1; y += band) {
    const int k = ((y - top0 - shift) / band % 7 + 7) % 7;
    // A pixel-art edge: every other band one cell wider on alternate sides.
    const int jog = ((y - top0 - shift) / band) & 1 ? s / 2 : -s / 2;
    const int yy = y < y0 ? y0 : y;
    c.rect(mx + jog, yy, mw, (y + band > y1 ? y1 : y + band) - yy, kRainbow[k]);
  }
}

static inline void funSickStart(FunUi& f, const char* agentId, uint32_t ms) {
  f.sickAt = ms ? ms : 1;
  snprintf(f.sickId, sizeof f.sickId, "%s", agentId);
}
