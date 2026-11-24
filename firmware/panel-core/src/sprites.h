#pragma once
// The mascots of agents-deck, ported from spriteSVG() and hatSVG() in
// backend/internal/web/static/deck-core.js: Claude's flat block with arms, the
// Codex onigiri, and the pixel hat of the agent's working folder.
//
// All geometry is in the SVG's own units: an 18 x 13 box whose top-left is
// (0, -4). The body sits at x 0..14, y 0..8; the "bubble" (what the agent is
// doing) sits at x 14..18. A still hat can rise to y -5 and a cap's brim
// reaches x -3; a working hat's frames can rise to y -6. Leave that much
// room. One unit is `scale` pixels. Animation timing follows settings.css.
#include <initializer_list>
#include <math.h>
#include <stdint.h>

#include "canvas.h"
#include "view.h"

namespace sprite_detail {

static inline int phase(uint32_t ms, uint32_t periodMs, int steps) {
  return (int)((uint64_t)(ms % periodMs) * steps / periodMs);
}

struct Pen {
  Canvas& c;
  int ox, oy, s;
  uint16_t body, cut;
  // Transform of the body group: scale about (7, 7), then shift down by gdy units.
  float gsx = 1, gsy = 1, gdy = 0;

  int px(float ux) const { return ox + (int)lroundf(ux * s); }
  int py(float uy) const { return oy + (int)lroundf((uy + 4) * s); }

  // Rectangle in units. Never thinner than one pixel.
  void r(float x, float y, float w, float h, uint16_t col) {
    const int x0 = px(x), y0 = py(y);
    int x1 = px(x + w), y1 = py(y + h);
    if (x1 <= x0) x1 = x0 + 1;
    if (y1 <= y0) y1 = y0 + 1;
    c.rect(x0, y0, x1 - x0, y1 - y0, col);
  }

  float gx(float x) const { return (x - 7) * gsx + 7; }
  float gy(float y) const { return (y - 7) * gsy + 7 + gdy; }

  // Rectangle that moves with the body group.
  void g(float x, float y, float w, float h, uint16_t col) {
    r(gx(x), gy(y), gx(x + w) - gx(x), gy(y + h) - gy(y), col);
  }

  // Stroke of the SVG's 0.55-unit width, inside the body group.
  void stroke(float x0, float y0, float x1, float y1, uint16_t col) {
    int t = (int)lroundf(0.55f * s);
    if (t < 1) t = 1;
    c.line(px(gx(x0)) - t / 2, py(gy(y0)) - t / 2, px(gx(x1)) - t / 2, py(gy(y1)) - t / 2, col, t);
  }

  // Small bitmap: '#' cells of w x h units, top-left at (x0, y0).
  void pixels(const char* const* rows, int n, float x0, float w, float h, float y0, uint16_t col, float dy = 0) {
    for (int ry = 0; ry < n; ry++)
      for (int cx = 0; rows[ry][cx]; cx++)
        if (rows[ry][cx] == '#') r(x0 + cx * w, y0 + ry * h + dy, w, h, col);
  }
};

static inline void onigiriBody(Pen& p, bool nori) {
  p.g(6, -3, 2, 1, p.body);
  p.g(5, -2, 4, 1, p.body);
  p.g(4, -1, 6, 1, p.body);
  p.g(3, 0, 8, 1, p.body);
  p.g(2, 1, 10, 2, p.body);
  p.g(1, 3, 12, 2, p.body);
  p.g(0, 5, 14, 1, p.body);
  p.g(1, 6, 12, 1, p.body);
  if (nori) p.g(4, 5.4f, 6, 1.6f, p.cut);
}

// Arms are 1 x 2, beside the body. l and r are the top row of each arm:
// 2 hangs down, 1 is half raised, 0 is up.
static inline void arms(Pen& p, bool bot, float l, float r) {
  if (bot) { p.g(1, l - 1, 1, 2, p.body); p.g(12, r - 1, 1, 2, p.body); }
  else     { p.g(0, l, 1, 2, p.body);     p.g(13, r, 1, 2, p.body); }
}

// Leg set 0 = standing (LEGS_A), 1 = stepping (LEGS_B). `dy` lifts them (hop).
static inline void legs(Pen& p, bool bot, int set, float dy = 0) {
  if (bot) {
    p.r(3.5f, (set ? 6.4f : 7) + dy, 2, 1.4f, p.body);
    p.r(8.5f, 7 + dy, 2, 1.4f, p.body);
    return;
  }
  static const float kA[] = {2, 4, 9, 11}, kB[] = {3, 5, 8, 10};
  for (float x : (set ? kB : kA)) p.r(x, 6 + dy, 1, 1.6f, p.body);
}

static inline void dots(Pen& p, int shown) {
  static const float kX[] = {14.6f, 15.9f, 17.2f};
  for (int i = 0; i < shown && i < 3; i++) p.r(kX[i], -2.6f, i == 2 ? 0.8f : 0.9f, 0.9f, p.body);
}

// ---------------------------------------------------------------- hats
// Bitmap rows placed at (x, top) in units; a = the hat's colour, b = its
// shade, c = highlight, d = near-black. Extras are single cells (x, y, key).
// Every hat has a still version and the frames it cycles through while its
// agent works, each shown for `ms`. The onigiri wears a narrower version that
// sits down over its tip. The bandana's knot and tails sit on the left.
// Order as kHatShapes in model.h. Mirrors HAT_ART in deck-core.js and hats.dart in
// the Flutter deck: edit all three together.
constexpr int kHatRowsMax = 5, kHatExtrasMax = 5, kHatFramesMax = 4;
struct HatCell { int8_t x, y; char key; };                 // key 0 ends the list
struct HatFrame {
  int8_t x, top;
  const char* rows[kHatRowsMax];
  HatCell extras[kHatExtrasMax];
};
struct HatArt {
  uint16_t ms;                                             // how long each working frame shows
  uint8_t frames;
  HatFrame still;
  HatFrame work[kHatFramesMax];
};
static const HatArt kHats[kHatShapeCount] = {
    {400, 2,  // hard hat
     {0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaaaaaaa...", ".bbbbbbbbbbbb."}, {}},
     {{0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaaccaaa...", ".bbbbbbbbbbbb."}, {}},
      {0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaabbaaa...", ".bbbbbbbbbbbb."}, {}}}},
    {300, 4,  // cap
     {-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {}},
     {{-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {}},
      {-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {}},
      {-3, -3, {"........aaaaaa", ".......aaaaacaa", "...bbbbbbbbbbbb"}, {{-3, 0, 'b'}, {-2, 0, 'b'}}},
      {-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {}}}},
    {350, 2,  // beanie
     {0, -4, {".....cc.......", "....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {}},
     {{0, -4, {".....cc.......", "....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {}},
      {0, -3, {"....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {{5, -5, 'c'}, {6, -5, 'c'}}}}},
    {280, 4,  // helmet
     {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaacaaaaa..", "..bbbbbbbbbb.."}, {}},
     {{0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..acaaaaaaaa..", "..bbbbbbbbbb.."}, {}},
      {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaacaaaa..", "..bbbbbbbbbb.."}, {}},
      {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaaaaaca..", "..bbbbbbbbbb.."}, {}},
      {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaaaaaaa..", "..bbbbbbbbbb.."}, {}}}},
    {300, 4,  // wizard
     {0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {}},
     {{0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {}},
      {0, -5, {".........a....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{11, -6, 'c'}}},
      {0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {}},
      {0, -5, {".......a......", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {}}}},
    {300, 4,  // beret
     {0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {}},
     {{0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {}},
      {0, -4, {".......b......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {}},
      {0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {}},
      {0, -4, {".....b........", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {}}}},
    {450, 2,  // chef
     {0, -4, {"...cc.cc.cc...", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {}},
     {{0, -4, {"...cc.cc.cc...", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {}},
      {0, -5, {"....cccccc....", "..cccccccccc..", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {}}}},
    {280, 4,  // crown
     {0, -4, {"...a..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {}},
     {{0, -4, {"...c..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {}},
      {0, -4, {"...a..ca..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {}},
      {0, -4, {"...a..ac..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {}},
      {0, -4, {"...a..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {}}}},
    {320, 4,  // headphones
     {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {}},
     {{0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -1, 'c'}, {-1, -2, 'c'}}},
      {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -2, 'c'}, {-1, -3, 'c'}}},
      {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-3, -3, 'c'}, {-2, -4, 'c'}, {-1, -1, 'c'}}},
      {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -2, 'c'}}}}},
    {120, 4,  // propeller
     {0, -4, {"..cccc.aaaa...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {}},
     {{0, -4, {"..cccc.aaaa...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {}},
      {0, -4, {"....cc.aa.....", ".......d......", "....ababab....", "...aaaaaaaa..."}, {}},
      {0, -4, {"..aaaa.cccc...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {}},
      {0, -4, {"....aa.cc.....", ".......d......", "....ababab....", "...aaaaaaaa..."}, {}}}},
    {450, 4,  // top hat
     {0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {}},
     {{0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {}},
      {0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {}},
      {0, -6, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {}},
      {0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {}}}},
    {350, 4,  // cowboy
     {0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {}},
     {{0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {}},
      {0, -4, {".....aaaa.....", "b...aabbaa....", "b...aaaaaa....", ".bbbbbbbbbbbbb"}, {}},
      {0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {}},
      {0, -4, {".....aaaa.....", "....aabbaa...b", "....aaaaaa...b", "bbbbbbbbbbbbb."}, {}}}},
    {200, 3,  // bandana
     {0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}},
     {{0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}},
      {0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -1, 'b'}, {-1, -2, 'b'}, {0, 0, 'b'}, {-1, 1, 'b'}}},
      {0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {-1, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}}}},
};
static const HatArt kOniHats[kHatShapeCount] = {
    {400, 2,  // hard hat
     {0, -3, {".....aaaa.....", "....acaaaa....", "...bbbbbbbb..."}, {}},
     {{0, -3, {".....aaaa.....", "....acccaa....", "...bbbbbbbb..."}, {}},
      {0, -3, {".....aaaa.....", "....acbbaa....", "...bbbbbbbb..."}, {}}}},
    {300, 4,  // cap
     {-1, -3, {"......aaaa", ".....aaacaa", "bbbbbbbbbbbb"}, {}},
     {{-1, -3, {"......aaaa", ".....aaacaa", "bbbbbbbbbbbb"}, {}},
      {-1, -3, {"......aaaa", ".....aaacaa", "bbbbbbbbbbbb"}, {}},
      {-1, -3, {"......aaaa", ".....aaacaa", "..bbbbbbbbbb"}, {{-1, 0, 'b'}}},
      {-1, -3, {"......aaaa", ".....aaacaa", "bbbbbbbbbbbb"}, {}}}},
    {350, 2,  // beanie
     {0, -4, {"......cc......", ".....aaaa.....", "....aaaaaa....", "...babababa..."}, {}},
     {{0, -4, {"......cc......", ".....aaaa.....", "....aaaaaa....", "...babababa..."}, {}},
      {0, -3, {".....aaaa.....", "....aaaaaa....", "...babababa..."}, {{6, -5, 'c'}, {7, -5, 'c'}}}}},
    {280, 4,  // helmet
     {0, -3, {".....aaaa.....", "....aacaaa....", "...bbbbbbbb..."}, {}},
     {{0, -3, {".....aaaa.....", "....acaaaa....", "...bbbbbbbb..."}, {}},
      {0, -3, {".....aaaa.....", "....aaacaa....", "...bbbbbbbb..."}, {}},
      {0, -3, {".....aaaa.....", "....aaaaca....", "...bbbbbbbb..."}, {}},
      {0, -3, {".....aaaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {}}}},
    {300, 4,  // wizard
     {0, -5, {"......aa......", "......aa......", ".....acaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {}},
     {{0, -5, {"......a.......", "......aa......", ".....acaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {}},
      {0, -5, {".......a......", "......aa......", ".....acaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {{9, -6, 'c'}}},
      {0, -5, {"......a.......", "......aa......", ".....acaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {}},
      {0, -5, {".....a........", "......aa......", ".....acaa.....", "....aaaaaa....", "...bbbbbbbb..."}, {}}}},
    {300, 4,  // beret
     {0, -4, {"......bb......", "....aaaaaa....", "..aaaaaaaaaa..", "....bbbbbb...."}, {}},
     {{0, -4, {"......b.......", "....aaaaaa....", "..aaaaaaaaaa..", "....bbbbbb...."}, {}},
      {0, -4, {".......b......", "....aaaaaa....", "..aaaaaaaaaa..", "....bbbbbb...."}, {}},
      {0, -4, {"......b.......", "....aaaaaa....", "..aaaaaaaaaa..", "....bbbbbb...."}, {}},
      {0, -4, {".....b........", "....aaaaaa....", "..aaaaaaaaaa..", "....bbbbbb...."}, {}}}},
    {450, 2,  // chef
     {0, -4, {"....cccccc....", "....cccccc....", ".....cccc.....", "....aaaaaa...."}, {}},
     {{0, -4, {"....cccccc....", "....cccccc....", ".....cccc.....", "....aaaaaa...."}, {}},
      {0, -5, {".....cccc.....", "....cccccc....", "....cccccc....", ".....cccc.....", "....aaaaaa...."}, {}}}},
    {280, 4,  // crown
     {0, -3, {"....a.aa.a....", "....aaaaaa....", "....acaaca...."}, {}},
     {{0, -3, {"....c.aa.a....", "....aaaaaa....", "....acaaca...."}, {}},
      {0, -3, {"....a.ca.a....", "....aaaaaa....", "....acaaca...."}, {}},
      {0, -3, {"....a.ac.a....", "....aaaaaa....", "....acaaca...."}, {}},
      {0, -3, {"....a.aa.a....", "....aaaaaa....", "....acaaca...."}, {}}}},
    {320, 4,  // headphones
     {0, -3, {".....aaaa.....", "....a....a....", "...a......a..."}, {}},
     {{0, -3, {".....aaaa.....", "....a....a....", "...a......a..."}, {{-1, -1, 'c'}, {0, -2, 'c'}}},
      {0, -3, {".....aaaa.....", "....a....a....", "...a......a..."}, {{-1, -2, 'c'}, {0, -3, 'c'}}},
      {0, -3, {".....aaaa.....", "....a....a....", "...a......a..."}, {{-2, -3, 'c'}, {-1, -4, 'c'}, {0, -1, 'c'}}},
      {0, -3, {".....aaaa.....", "....a....a....", "...a......a..."}, {{-1, -2, 'c'}}}}},
    {120, 4,  // propeller
     {0, -4, {"...ccc..aaa...", "......dd......", ".....abab.....", "....aaaaaa...."}, {}},
     {{0, -4, {"...ccc..aaa...", "......dd......", ".....abab.....", "....aaaaaa...."}, {}},
      {0, -4, {".....cc.aa....", "......dd......", ".....abab.....", "....aaaaaa...."}, {}},
      {0, -4, {"...aaa..ccc...", "......dd......", ".....abab.....", "....aaaaaa...."}, {}},
      {0, -4, {".....aa.cc....", "......dd......", ".....abab.....", "....aaaaaa...."}, {}}}},
    {450, 4,  // top hat
     {0, -4, {".....aaaa.....", ".....aaaa.....", ".....bbbb.....", "...aaaaaaaa..."}, {}},
     {{0, -4, {".....aaaa.....", ".....aaaa.....", ".....bbbb.....", "...aaaaaaaa..."}, {}},
      {0, -4, {".....aaaa.....", ".....aaaa.....", ".....bbbb.....", "...aaaaaaaa..."}, {}},
      {0, -5, {".....aaaa.....", ".....aaaa.....", ".....bbbb.....", "...aaaaaaaa..."}, {}},
      {0, -4, {".....aaaa.....", ".....aaaa.....", ".....bbbb.....", "...aaaaaaaa..."}, {}}}},
    {350, 4,  // cowboy
     {0, -4, {"......aa......", ".....abba.....", ".b..aaaaaa..b.", ".bbbbbbbbbbbb."}, {}},
     {{0, -4, {"......aa......", ".....abba.....", ".b..aaaaaa..b.", ".bbbbbbbbbbbb."}, {}},
      {0, -4, {"......aa......", ".b...abba.....", ".b..aaaaaa....", "..bbbbbbbbbbb."}, {}},
      {0, -4, {"......aa......", ".....abba.....", ".b..aaaaaa..b.", ".bbbbbbbbbbbb."}, {}},
      {0, -4, {"......aa......", ".....abba...b.", "....aaaaaa..b.", ".bbbbbbbbbbb.."}, {}}}},
    {200, 3,  // bandana
     {0, 0, {"...aaaaaaaa...", "..acacacacac.."}, {{2, 0, 'b'}, {1, -1, 'b'}, {1, 1, 'b'}, {0, 1, 'b'}}},
     {{0, 0, {"...aaaaaaaa...", "..acacacacac.."}, {{2, 0, 'b'}, {1, -1, 'b'}, {1, 1, 'b'}, {0, 1, 'b'}}},
      {0, 0, {"...aaaaaaaa...", "..acacacacac.."}, {{2, 0, 'b'}, {1, 0, 'b'}, {0, -1, 'b'}, {1, 1, 'b'}, {0, 2, 'b'}}},
      {0, 0, {"...aaaaaaaa...", "..acacacacac.."}, {{2, 0, 'b'}, {1, -1, 'b'}, {0, -1, 'b'}, {1, 1, 'b'}, {0, 1, 'b'}}}}},
};
constexpr int kHatHeadphones = 8;                          // index into kHatShapes

// {main, shade} per colour, order as kHatColors in model.h.
static const uint16_t kHatPalette[kHatColorCount][2] = {
    {rgb565(0x4F, 0xB3, 0xA6), rgb565(0x2F, 0x7F, 0x75)}, {rgb565(0x6F, 0x8F, 0xD8), rgb565(0x4A, 0x63, 0xA3)},
    {rgb565(0xE8, 0xB9, 0x4A), rgb565(0xB0, 0x84, 0x29)}, {rgb565(0x6D, 0xBE, 0x6A), rgb565(0x46, 0x8A, 0x44)},
    {rgb565(0xA6, 0x8B, 0xD8), rgb565(0x76, 0x60, 0xA6)}, {rgb565(0xE5, 0x8B, 0xB0), rgb565(0xB0, 0x5E, 0x80)},
    {rgb565(0x5E, 0xC4, 0xD6), rgb565(0x3A, 0x8E, 0x9C)}, {rgb565(0xE8, 0xE4, 0xDC), rgb565(0xA8, 0xA3, 0x9A)},
};

// Poses of a working agent: its hat animates; every other pose keeps it still
// (WORKING_POSES in deck-core.js).
static inline bool poseWorking(Pose pose) {
  switch (pose) {
    case Pose::Run: case Pose::Think: case Pose::Read: case Pose::Write: case Pose::Bash:
    case Pose::Web: case Pose::Plan: case Pose::Delegate: case Pose::Compact: return true;
    default: return false;
  }
}

// `hat` is 1 + the shape index. While `working`, the hat shows the frame that
// `ms` falls in; otherwise its still version. A faded hat (an exited agent)
// is mixed with the background.
static inline void drawHat(Pen& p, bool bot, uint8_t hat, uint8_t color, bool faded, bool working, uint32_t ms) {
  if (hat < 1 || hat > kHatShapeCount) return;
  const int shape = hat - 1;
  const HatArt& art = (bot ? kOniHats : kHats)[shape];
  const HatFrame& f = working ? art.work[ms / art.ms % art.frames] : art.still;
  const uint16_t* pal = kHatPalette[color < kHatColorCount ? color : kHatInk];
  auto cell = [&](float x, float y, char key) {
    uint16_t col;
    switch (key) {
      case 'a': col = pal[0]; break;
      case 'b': col = pal[1]; break;
      case 'c': col = rgb565(0xF7, 0xF4, 0xEC); break;
      case 'd': col = rgb565(0x0A, 0x0A, 0x0A); break;
      default: return;
    }
    p.g(x, y, 1, 1, faded ? blend565(p.cut, col, 7) : col);
  };
  for (int ry = 0; ry < kHatRowsMax && f.rows[ry]; ry++)
    for (int cx = 0; f.rows[ry][cx]; cx++) cell(f.x + cx, f.top + ry, f.rows[ry][cx]);
  for (int i = 0; i < kHatExtrasMax && f.extras[i].key; i++) cell(f.extras[i].x, f.extras[i].y, f.extras[i].key);
  if (shape == kHatHeadphones) {                           // ear cups
    const uint16_t cup = faded ? blend565(p.cut, pal[1], 7) : pal[1];
    if (bot) { p.g(2, -0.5f, 1, 2, cup); p.g(11, -0.5f, 1, 2, cup); }
    else     { p.g(0, 0, 1, 2, cup);     p.g(13, 0, 1, 2, cup); }
  }
}

}  // namespace sprite_detail

// Draws one sprite. (x, y) is the pixel of the box's top-left corner; the box
// is 18*scale wide and 13*scale high. `body` is the sprite colour and `cut`
// the colour behind it (eyes are holes). With bubble = false only the body,
// the face, the limbs and the hat are drawn, which fits a 14*scale wide cell.
// `hat` is Agent::hat (0 = none) and `hatColor` is Agent::hatColor. The hat
// steps through its frames while the pose is a working one, on the same `ms`.
static inline void drawSprite(Canvas& c, int x, int y, int scale, Species sp, Pose pose,
                              uint16_t body, uint16_t cut, uint32_t ms, bool bubble,
                              uint8_t hat = 0, uint8_t hatColor = 0) {
  using namespace sprite_detail;
  // Nothing of it in these rows (a strip of the frame): units -6..9 (hats, a jump, strokes: a margin).
  if (y + 16 * scale <= c.top || y - 4 * scale >= c.h) return;
  Pen p{c, x, y, scale, body, cut};
  const bool bot = sp == Species::Codex;
  const int half = phase(ms, 500, 2);          // .la / .lb / .bob: two halves of 0.5 s
  const bool working = poseWorking(pose);
  if (pose == Pose::Write) pose = Pose::Bash;  // "writing looks like bash for now" (deck-core.js)

  // Group motion comes first: the body, the arms, the face and the hat share it.
  int legSet = 0;
  float legDy = 0, armL = 2, armR = 2;
  bool showArms = true, showLegs = true;
  switch (pose) {
    case Pose::Bash:      p.gdy = phase(ms, 250, 2) ? -0.5f : 0; legSet = phase(ms, 250, 2); break;
    case Pose::Delegate:
    case Pose::Run:       p.gdy = half ? -0.5f : 0; legSet = half; armL = half ? 1 : 2; armR = half ? 2 : 1; break;
    case Pose::Think:     armR = 1; break;
    case Pose::Celebrate: p.gdy = legDy = phase(ms, 400, 2) ? -1.5f : 0; armL = armR = 0; break;
    case Pose::Err:       armL = armR = 0; break;        // both up in alarm
    case Pose::Exit:      showArms = false; showLegs = bot; break;
    case Pose::Compact: {
      static const float kSx[] = {1, 1.1f, 1.2f, 1.1f}, kSy[] = {1, 0.8f, 0.62f, 0.8f};
      const int q = phase(ms, 1200, 4);
      p.gsx = kSx[q];
      p.gsy = kSy[q];
      break;
    }
    default: break;
  }

  if (bot) onigiriBody(p, pose != Pose::Exit); else p.g(1, 0, 12, 6, body);
  if (showArms) arms(p, bot, armL, armR);

  // The block's face sits half a row higher than the onigiri's.
  const float f = bot ? 0 : -0.5f;
  auto eyes = [&](float ey, float eh = 2, float dx = 0) {
    p.g(4 + dx, ey + f, 1, eh, cut);
    p.g(9 + dx, ey + f, 1, eh, cut);
  };

  switch (pose) {
    case Pose::Think: {
      eyes(1, 1.5f, phase(ms, 1200, 2) ? 1 : -1);
      if (bubble) {
        const int q = phase(ms, 1200, 4);
        p.r(14.3f, -0.9f, 0.6f, 0.6f, body);
        if (q >= 1) p.r(15.3f, -2.1f, 0.9f, 0.9f, body);
        if (q >= 2) p.r(16.5f, -3.8f, 1.3f, 1.3f, body);
      }
      break;
    }
    case Pose::Read: {
      static const float kDx[] = {0, -1, 0, 1};
      eyes(2, 2, kDx[phase(ms, 1200, 4)]);
      static const char* const kLens[] = {".##..", "#..#.", "#..#.", ".##..", "....#"};
      if (bubble) p.pixels(kLens, 5, 14.6f, 0.75f, 0.75f, -4, body);
      break;
    }
    case Pose::Bash: {
      eyes(2);
      static const char* const kPrompt[] = {"#..", ".#.", "#.."};
      if (bubble) {
        p.pixels(kPrompt, 3, 14.5f, 0.7f, 0.8f, -3.7f, body);
        if (phase(ms, 1000, 2) == 0) p.r(16.5f, -1.7f, 1.2f, 0.5f, body);
      }
      break;
    }
    case Pose::Web: {
      eyes(1, 2, 0.4f);
      static const char* const kGlobeA[] = {".###.", "#.#.#", "#####", "#.#.#", ".###."};
      static const char* const kGlobeB[] = {".###.", "##.##", "#####", "##.##", ".###."};
      if (bubble) p.pixels(phase(ms, 1000, 2) ? kGlobeB : kGlobeA, 5, 14.4f, 0.72f, 0.72f, -4.2f, body);
      break;
    }
    case Pose::Plan: {
      eyes(2);
      if (bubble) {
        const int done = phase(ms, 2000, 4);             // boxes fill one by one
        for (int i = 0; i < 3; i++) {
          const float yy = -3.9f + i * 1.25f;
          p.r(14.4f, yy, 0.9f, 0.9f, body);
          if (done <= i) p.r(14.6f, yy + 0.2f, 0.5f, 0.5f, cut);
          p.r(15.6f, yy + 0.25f, 2.2f, 0.45f, body);
        }
      }
      break;
    }
    case Pose::Delegate: {
      eyes(2);
      if (bubble) {                                      // a small block of its own: MINI(14.4, 3.2, 0.26)
        const int mx = p.px(14.4f), my = p.py(3.2f);
        const float k = 0.26f * scale;
        auto mr = [&](float ux, float uy, float uw, float uh, uint16_t col) {
          const int x0 = mx + (int)lroundf(ux * k), y0 = my + (int)lroundf(uy * k);
          int x1 = mx + (int)lroundf((ux + uw) * k), y1 = my + (int)lroundf((uy + uh) * k);
          if (x1 <= x0) x1 = x0 + 1;
          if (y1 <= y0) y1 = y0 + 1;
          c.rect(x0, y0, x1 - x0, y1 - y0, col);
        };
        mr(1, 0, 12, 6, body);
        mr(0, 2, 1, 2, body); mr(13, 2, 1, 2, body);
        mr(4, 1.5f, 1, 2, cut); mr(9, 1.5f, 1, 2, cut);
        static const float kA[] = {2, 4, 9, 11}, kB[] = {3, 5, 8, 10};
        for (float lx : (half ? kB : kA)) mr(lx, 6, 1, 1.6f, body);
      }
      break;
    }
    case Pose::Hungry: {
      eyes(1, 2, 0.6f);
      const int chomp = phase(ms, 1000, 2);              // mouth open, mouth shut
      if (bot) { if (chomp) p.g(6.2f, 4, 1.6f, 0.5f, cut); else p.g(6.2f, 3.4f, 1.6f, 1.4f, cut); }
      else     { if (chomp) p.g(6.2f, 4.8f + f, 2.2f, 0.5f, cut); else p.g(6.2f, 4.2f + f, 2.2f, 1.6f, cut); }
      static const char* const kCookie[] = {".###.", "##.##", "#####", "#.###", ".###."};
      if (bubble) p.pixels(kCookie, 5, 14.3f, 0.75f, 0.75f, -4.2f, body, phase(ms, 600, 2) ? 0.6f : 0);
      break;
    }
    case Pose::Celebrate: {
      for (float ex : {4.5f, 9.5f}) {                    // ^ ^
        p.stroke(ex - 1, 3.3f + f, ex, 2.2f + f, cut);
        p.stroke(ex, 2.2f + f, ex + 1, 3.3f + f, cut);
      }
      if (bubble) {                                      // the finish flag in the raised hand
        const uint16_t flag = rgb565(0xE8, 0xE4, 0xDC);
        p.g(14, -4, 1, 6, flag);
        for (int cx = 0; cx < 3; cx++)
          for (int cy = 0; cy < 2; cy++) p.g(15 + cx, -4 + cy, 1, 1, (cx + cy + half) % 2 ? cut : flag);
      }
      break;
    }
    case Pose::Run:
      eyes(2);
      if (bubble) dots(p, 1 + phase(ms, 1200, 3));
      break;
    case Pose::Wait: {
      eyes(1);
      static const char* const kQuestion[] = {"###", "..#", ".#.", "...", ".#."};
      if (bubble && phase(ms, 1000, 5) < 3) p.pixels(kQuestion, 5, 14.5f, 1.1f, 0.9f, -4, body);
      break;
    }
    case Pose::Err: {
      for (float ex : {3.8f, 8.8f}) {                    // x x
        p.stroke(ex - 0.4f, 1.8f + f, ex + 1.4f, 4.0f + f, cut);
        p.stroke(ex + 1.4f, 1.8f + f, ex - 0.4f, 4.0f + f, cut);
      }
      static const char* const kBang[] = {"#", "#", "#", ".", "#"};
      if (bubble && phase(ms, 1000, 5) < 3) p.pixels(kBang, 5, 15.6f, 1.2f, 0.9f, -4, body);
      break;
    }
    case Pose::Start:
      eyes(2);
      if (bubble) dots(p, 1 + phase(ms, 1200, 3));
      break;
    case Pose::Compact: {
      p.stroke(3.6f, 1.9f + f, 4.8f, 2.7f + f, cut);     // > <
      p.stroke(4.8f, 2.7f + f, 3.6f, 3.5f + f, cut);
      p.stroke(10.4f, 1.9f + f, 9.2f, 2.7f + f, cut);
      p.stroke(9.2f, 2.7f + f, 10.4f, 3.5f + f, cut);
      if (bubble) {                                      // a stack of pages pressed into one
        static const float kP1[] = {0, 0.4f, 1.1f, 0.4f}, kP2[] = {0, 0.8f, 2.2f, 0.8f};
        const int q = phase(ms, 1200, 4);
        p.r(14.6f, -3.8f + kP2[q], 3.2f, 0.6f, body);
        p.r(14.6f, -2.7f + kP1[q], 3.2f, 0.6f, body);
        p.r(14.6f, -1.6f, 3.2f, 0.6f, body);
      }
      break;
    }
    case Pose::Exit:                                     // hollow shell, two dots for eyes
      if (bot) {
        p.g(6, -1, 2, 1, cut); p.g(5, 0, 4, 1, cut); p.g(4, 1, 6, 2, cut); p.g(3, 3, 8, 2, cut); p.g(2, 5, 10, 1, cut);
      } else {
        p.g(2, 1, 10, 4, cut);
      }
      p.g(4, 3 + f, 1, 1, body);
      p.g(9, 3 + f, 1, 1, body);
      break;
    case Pose::Idle:
    default: {                                           // asleep
      p.g(3.5f, 3.2f + f, 2, 0.55f, cut);
      p.g(8.5f, 3.2f + f, 2, 0.55f, cut);
      static const char* const kZ[] = {"####", "..#.", ".#..", "####"};
      if (bubble) {
        const float lift = phase(ms, 2400, 4) >= 2 ? -0.6f : 0;
        p.pixels(kZ, 4, 14.5f, 0.5f, 0.5f, -2.6f, body, lift);
        if (phase(ms, 2400, 4) != 3) p.pixels(kZ, 4, 16.6f, 0.34f, 0.34f, -3.9f, body, lift);
      }
      break;
    }
  }

  drawHat(p, bot, hat, hatColor, pose == Pose::Exit, working, ms);
  if (showLegs) legs(p, bot, legSet, legDy);
}

// ---------------------------------------------------------------- agent pets
// While Agent pets is on, each agent's pet (Agent::pet, the kit's deck_pet.h)
// stands where its critter would, at the same scale, and does the critter's
// pose with its bubble: one pet cell is one sprite unit. Cell (0, 0) sits at
// unit (-1, -5), so the pet's middle is the body's and a grown-up's feet land
// on the critter's. With its bubbles, flag, flames and stroke it reaches
// cells DECK_PET_REACH_* (units -6..23 across, -17..10 down).

namespace sprite_detail {

// The kit's colours (0xRRGGBB and an alpha) into the RGB565 canvas, clipped.
static inline void petFill(void* user, int x, int y, int w, int h, uint32_t rgb, uint8_t alpha) {
  Canvas& c = *(Canvas*)user;
  const uint16_t col = rgb565((uint8_t)(rgb >> 16), (uint8_t)(rgb >> 8), (uint8_t)rgb);
  const uint8_t a = (uint8_t)((alpha * 15 + 127) / 255);
  if (a >= 15) { c.rect(x, y, w, h, col); return; }
  if (a == 0) return;
  const int x0 = x < 0 ? 0 : x, x1 = x + w > c.w ? c.w : x + w;
  const int y0 = y < c.top ? c.top : y, y1 = y + h > c.h ? c.h : y + h;
  for (int yy = y0; yy < y1; yy++)
    for (int xx = x0; xx < x1; xx++) {
      uint16_t& d = c.px[(size_t)yy * c.w + xx];
      d = c.enc(blend565(c.dec(d), col, a));
    }
}

}  // namespace sprite_detail

// The pet's pose for the critter's (the kit's poses follow Pose, after NONE).
static inline uint8_t petPose(Pose pose) { return (uint8_t)((int)pose + 1); }

// Draws the agent's pet with its cell (0, 0) at pixel (x, y), in `pose` at
// `ms`, with its bubble or not. The look is kept in static memory: pets are
// drawn from one task.
static inline void drawPetCells(Canvas& c, int x, int y, int scale, const Agent& a, Pose pose, uint32_t ms, bool bubble) {
  using namespace sprite_detail;
  // nothing of it in these rows (a strip of the frame)
  if (y + (DECK_PET_REACH_Y1 + 1) * scale <= c.top || y + (DECK_PET_REACH_Y0 - 1) * scale >= c.h) return;
  deck_pet_opts_t o{};
  o.pose = petPose(pose);
  o.ms = ms;
  o.bubble = bubble;
  o.hat = a.hat;
  o.hat_color = a.hatColor;
  static deck_pet_look_t look;
  deck_pet_look(&look, &a.pet, &o);
  int dx, dy;
  deck_pet_motion(&look, ms, scale, &dx, &dy);
  deck_pet_draw(&look, x + dx, y + dy, scale, petFill, &c);
}

// The pet with the sprite box's top-left at (x, y), as drawSprite would its critter.
static inline void drawPet(Canvas& c, int x, int y, int scale, const Agent& a, Pose pose, uint32_t ms, bool bubble) {
  drawPetCells(c, x - scale, y - scale, scale, a, pose, ms, bubble);
}

// The row under the pet's still art (its feet, the egg's foot, the weak one's
// ground): stand it on a line there.
static inline int petBottom(const Agent& a) {
  static const uint8_t kBottom[] = {14, 14, 13, 13, 12, 13};   // per deck_pet_art_t
  static deck_pet_look_t look;
  deck_pet_look(&look, &a.pet, nullptr);
  return look.art < sizeof kBottom ? kBottom[look.art] : 14;
}

// The agent as the deck shows it: its pet while it has one, else its critter
// (drawSprite, with its hat); both in `pose`.
static inline void drawCritter(Canvas& c, int x, int y, int scale, const Agent& a, Pose pose, uint16_t body, uint16_t cut,
                               uint32_t ms, bool bubble) {
  if (a.pet.has) drawPet(c, x, y, scale, a, pose, ms, bubble);
  else drawSprite(c, x, y, scale, a.species, pose, body, cut, ms, bubble, a.hat, a.hatColor);
}

// "Pip · Kid · level 7", or "Egg": the pet's name, stage and level, for a
// screen that shows one agent in detail. Empty without a pet.
static inline void petLine(const Agent& a, char* out, size_t cap) {
  if (!cap) return;
  out[0] = '\0';
  if (!a.pet.has) return;
  const char* stage = deck_pet_stage_name(a.pet.stage);
  if (a.pet.stage == DECK_PET_EGG || !a.pet.name[0]) snprintf(out, cap, "%s", stage);
  else snprintf(out, cap, "%s" DECK_DOT "%s" DECK_DOT "level %u", a.pet.name, stage, (unsigned)a.pet.level);
}
