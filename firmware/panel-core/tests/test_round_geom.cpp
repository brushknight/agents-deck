// round_geom.h: angles, polar placement, hit tests, the rim drag, and that
// discs, arcs, pills and the mask draw where they should and nowhere else.
#include <assert.h>
#include <math.h>
#include <stdio.h>

#include <vector>

#include "round_geom.h"

static bool near(float a, float b, float eps = 0.01f) { return fabsf(a - b) <= eps; }

static void testAngles() {
  // 0 is 12 o'clock, clockwise.
  assert(near(angleAt(240, 240, 240, 100), 0));
  assert(near(angleAt(240, 240, 380, 240), 90));
  assert(near(angleAt(240, 240, 240, 380), 180));
  assert(near(angleAt(240, 240, 100, 240), 270));
  assert(near(angleAt(240, 240, 340, 140), 45));
  assert(near(angleAt(240, 240, 240, 240), 0));   // the centre itself

  RPoint p = polar(240, 240, 100, 0);
  assert(p.x == 240 && p.y == 140);
  p = polar(240, 240, 100, 90);
  assert(p.x == 340 && p.y == 240);
  p = polar(240, 240, 202, 148);   // the spec's focus button (V4)
  assert(abs(p.x - 347) <= 1 && abs(p.y - 411) <= 1);
  p = polar(240, 240, 168, 144);   // V1's
  assert(abs(p.x - 339) <= 1 && abs(p.y - 376) <= 1);
  // polar and angleAt agree all the way round.
  for (int a = 0; a < 360; a += 7) {
    const RPoint q = polar(240, 240, 200, (float)a);
    assert(fabsf(angleDelta((float)a, angleAt(240, 240, q.x, q.y))) < 0.5f);
  }

  assert(near(normDeg(-30), 330));
  assert(near(normDeg(725), 5));
  assert(near(angleDelta(350, 10), 20));
  assert(near(angleDelta(10, 350), -20));
  assert(near(angleDelta(0, 180), 180));
  assert(near(angleDelta(90, 90), 0));

  assert(angleOnArc(10, 350, 30));      // across 12 o'clock
  assert(!angleOnArc(30, 350, 30));
  assert(angleOnArc(200, 0, 360));
  assert(!angleOnArc(0, 10, 0));
  assert(angleOnArc(240, 240, 240) && angleOnArc(0, 240, 240) && angleOnArc(120, 240, 240));
  assert(!angleOnArc(180, 240, 240));   // the detail's free bottom
}

static void testHits() {
  assert(inCircle(240, 240, 10, 245, 245));
  assert(!inCircle(240, 240, 10, 250, 250));
  assert(inCircle(240, 240, 10, 250, 240));   // on the edge counts
  assert(inAnnulus(240, 240, 150, 240, 240, 30));
  assert(!inAnnulus(240, 240, 150, 240, 240, 200));
  assert(!inAnnulus(240, 240, 150, 240, 0, 0));
  assert(near(chordHalf(240, 240, 240), 240));
  assert(near(chordHalf(240, 100, 400), 0));
  assert(near(chordHalf(240, 5, 243), 4));
  // A box from y 342 to 414 inside r 214: limited by its bottom edge.
  assert(near(chordHalfSpan(240, 214, 342, 414), chordHalf(240, 214, 414)));
  assert(near(chordHalfSpan(240, 214, 100, 380), chordHalf(240, 214, 100)));
}

static void testRimDrag() {
  RimDrag d;
  rimBegin(d, 240, 240, 150, 248, 240, 20);   // 12 o'clock, on the rim
  assert(d.active);
  // Clockwise past 12 o'clock and on to 3 o'clock, in small steps.
  for (int a = 0; a <= 90; a += 5) {
    const RPoint p = polar(240, 240, 220, (float)a);
    rimMove(d, 240, 240, p.x, p.y);
  }
  assert(fabsf(d.turned - 90) < 1);
  // Back anticlockwise across 12 o'clock to 11.
  for (int a = 85; a >= -30; a -= 5) {
    const RPoint p = polar(240, 240, 220, (float)a);
    rimMove(d, 240, 240, p.x, p.y);
  }
  assert(fabsf(d.turned + 30) < 1);
  // Samples near the centre are ignored.
  const float before = d.turned;
  assert(rimMove(d, 240, 240, 245, 238) == 0 && d.turned == before);
  // A touch in the middle never starts a drag.
  rimBegin(d, 240, 240, 150, 248, 240, 240);
  assert(!d.active);
  assert(rimMove(d, 240, 240, 400, 240) == 0 && d.turned == 0);
}

// A canvas with a guard band, to catch drawing outside it.
struct Guarded {
  static constexpr int W = 480, H = 480, PAD = 2 * 480;
  static constexpr uint16_t GUARD = 0xDEAD;
  std::vector<uint16_t> mem;
  Canvas c;
  Guarded() : mem(W * H + 2 * PAD, GUARD), c{mem.data() + PAD, W, H} { c.fill(0); }
  bool guardsIntact() const {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[mem.size() - 1 - i] != GUARD) return false;
    return true;
  }
  uint16_t at(int x, int y) const { return c.px[y * W + x]; }
};

static void testDrawing() {
  const uint16_t W = 0xFFFF;
  {
    Guarded g;
    fillDisc(g.c, 240, 240, 100, W);
    assert(g.at(240, 240) == W && g.at(240, 145) == W && g.at(335, 240) == W);
    assert(g.at(240, 135) == 0 && g.at(345, 240) == 0 && g.at(320, 320) == 0);   // (320,320) is 113 away
    // Symmetric: mirrored pixels match.
    for (int y = 130; y < 350; y += 3)
      for (int x = 130; x < 350; x += 3) assert(g.at(x, y) == g.at(479 - x, y) && g.at(x, y) == g.at(x, 479 - y));
    assert(g.guardsIntact());
  }
  {
    // A ring: a hole in the middle.
    Guarded g;
    strokeCircle(g.c, 240, 240, 229, 5, W);
    assert(g.at(240, 240) == 0 && g.at(240, 11) == W && g.at(240, 468) == W && g.at(240, 20) == 0);
    assert(g.guardsIntact());
  }
  {
    // An arc over the top only: 300° → 60°.
    Guarded g;
    strokeArc(g.c, 240, 240, 220, 14, 300, 120, W);
    const RPoint top = polar(240, 240, 220, 0), right = polar(240, 240, 220, 90), left = polar(240, 240, 220, 270);
    assert(g.at(top.x, top.y) == W && g.at(right.x, right.y) == 0 && g.at(left.x, left.y) == 0);
    const RPoint in1 = polar(240, 240, 220, 55), out1 = polar(240, 240, 220, 65);
    assert(g.at(in1.x, in1.y) == W && g.at(out1.x, out1.y) == 0);
    assert(g.guardsIntact());
  }
  {
    // Partly and fully off the canvas: clipped, nothing written outside.
    Guarded g;
    fillDisc(g.c, 0, 0, 60, W);
    fillDisc(g.c, 479, 479, 60, W);
    fillDisc(g.c, -500, 900, 60, W);
    strokeCircle(g.c, 240, 240, 400, 30, W);
    strokeArc(g.c, 470, 10, 50, 9, 10, 300, W);
    fillPill(g.c, -30, 460, 600, 60, W);
    strokePill(g.c, 400, -20, 200, 60, 3, W);
    fillDiscRows(g.c, 240, 240, 300, -50, 600, W);
    dashedCircle(g.c, 240, 240, 239, 6, 40, 3, W);
    assert(g.guardsIntact());
  }
  {
    // A band of a disc: its ends follow the circle.
    Guarded g;
    fillDiscRows(g.c, 240, 240, 216, 178, 252, W);
    assert(g.at(240, 178) == W && g.at(240, 251) == W && g.at(240, 177) == 0 && g.at(240, 252) == 0);
    const int half = (int)chordHalf(240, 216, 178.5f);
    assert(g.at(240 - half + 2, 178) == W && g.at(240 - half - 2, 178) == 0);
    assert(g.at(240 + 210, 240) == W && g.at(240 + 218, 240) == 0 && g.at(240, 300) == 0);
  }
  {
    // A pill: inside the box, round ends.
    Guarded g;
    fillPill(g.c, 70, 178, 340, 72, W);
    assert(g.at(240, 200) == W && g.at(72, 214) == W && g.at(408, 214) == W);
    assert(g.at(71, 179) == 0 && g.at(409, 249) == 0);   // the corners are cut
    assert(g.at(69, 214) == 0 && g.at(411, 214) == 0);
    Guarded h;
    strokePill(h.c, 70, 178, 340, 72, 3, W);
    assert(h.at(240, 178) == W && h.at(240, 214) == 0 && h.at(71, 214) == W);
  }
  {
    // The mask: corners black, the disc untouched.
    Guarded g;
    g.c.fill(W);
    maskOutside(g.c, 240, 240, 240, 0);
    assert(g.at(0, 0) == 0 && g.at(479, 479) == 0 && g.at(10, 10) == 0);
    assert(g.at(240, 240) == W && g.at(240, 2) == W && g.at(2, 240) == W && g.at(400, 400) == W);
    assert(g.guardsIntact());
  }
}

int main() {
  testAngles();
  testHits();
  testRimDrag();
  testDrawing();
  printf("round geometry: ok\n");
  return 0;
}
