// The spring carousel (spring.h, layout_spring.h, its touch in round_view.h):
// where agents sit on the turn, how it turns (swipes, a drag round the rim,
// a fling, a tap on a ring agent), turning to waiters by itself, and that a
// frame redrawn in part is the same, pixel for pixel, as one drawn whole.
#define PANEL_MAX_AGENTS 64
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_spring.h"

static Agent mk(const char* id, uint8_t slot, Status st, int64_t since = 1000) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.status = st;
  a.statusSince = since;
  a.ctxWindow = 200000;
  a.ctxUsed = 50000 + slot * 9000;
  return a;
}

static void wait(Agent& a) {
  a.status = Status::Waiting;
  snprintf(a.promptId, sizeof a.promptId, "p-%s", a.id);
  snprintf(a.waitTitle, sizeof a.waitTitle, "allow bash?");
  a.optCount = 2;
  snprintf(a.opts[0].key, sizeof a.opts[0].key, "1");
  snprintf(a.opts[0].label, sizeof a.opts[0].label, "yes");
  snprintf(a.opts[1].key, sizeof a.opts[1].key, "2");
  snprintf(a.opts[1].label, sizeof a.opts[1].label, "no");
}

static Fleet crowd(int n) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (int i = 0; i < n; i++) {
    char id[16];
    snprintf(id, sizeof id, "a%02d", i);
    f.agents[f.count++] = mk(id, (uint8_t)i, i % 3 ? Status::Running : Status::Idle);
  }
  f.total = f.count;
  return f;
}

static const SpringPanel kSpring(layoutSpring21());
static const SpringGeom& G = kSpring.L.G;

static int sel(const RoundUi& ui, const Fleet& f) { return roundFeatured(ui, f); }

static void settle(RoundUi& ui, const Fleet& f, uint32_t& ms) {
  ms += 2000;
  roundSync(ui, kSpring, f, ms);
  assert(!springMoving(ui.spring, ms));
}

static void touch(RoundUi& ui, const Fleet& f, bool down, int x, int y, uint32_t ms) {
  roundTouch(ui, kSpring, f, down, x, y, RoundScope::Act, 0, ms);
}

static void swipe(RoundUi& ui, const Fleet& f, int x0, int x1, uint32_t& ms) {
  for (int k = 0; k <= 6; k++) { touch(ui, f, true, x0 + (x1 - x0) * k / 6, 240, ms); ms += 16; }
  touch(ui, f, false, 0, 0, ms);
}

// A finger round the rim from a0 to a1 degrees, `stepMs` between samples, then held `holdMs` and lifted.
static void rim(RoundUi& ui, const Fleet& f, float a0, float a1, uint32_t& ms, uint32_t stepMs = 16, uint32_t holdMs = 0) {
  for (int k = 0; k <= 20; k++) {
    float x, y;
    springPolar(205, a0 + (a1 - a0) * k / 20, x, y);
    touch(ui, f, true, (int)x, (int)y, ms);
    ms += stepMs;
  }
  ms += holdMs;
  touch(ui, f, false, 0, 0, ms);
}

static void testGeometry() {
  // The selected one at 12 o'clock, x6, 140 px up; the turn a spiral from 184 to 174 px; seven to a turn.
  SpringPlace p = springPlace(G, 0, 20);
  assert(p.shown && p.scale == 6 && p.alpha == 15 && fabsf(p.x - 240) < 0.01f && fabsf(p.y - 100) < 0.01f);
  p = springPlace(G, 1, 20);
  assert(p.scale == 4 && fabsf(hypotf(p.x - 240, p.y - 240) - 184) < 0.01f);
  p = springPlace(G, 6, 20);
  assert(p.scale == 4 && fabsf(hypotf(p.x - 240, p.y - 240) - 174) < 0.01f && p.x < 240);
  // Diving and rising: smaller, fading in four steps; gone past the turn.
  p = springPlace(G, 6.5f, 20);
  assert(p.shown && p.scale == 3 && p.alpha == 8 && p.z == 1);
  p = springPlace(G, -0.5f, 20);
  assert(p.shown && p.scale == 4 && p.alpha == 8);
  assert(!springPlace(G, 7, 20).shown && !springPlace(G, -1, 20).shown);
  // Up to K: a closed loop spread evenly, nothing hidden.
  for (int n = 1; n <= 7; n++)
    for (float pos = 0; pos < n; pos += 0.25f)
      for (int j = 0; j < n; j++) assert(springPlace(G, springSlot(G, j, pos, n), n).shown);
  p = springPlace(G, 1, 3);
  assert(fabsf(p.x - (240 + 178 * sinf(120 * kRoundPi / 180))) < 0.01f);
  // The wrap: past the last agent comes the first.
  assert(springSlot(G, 0, 19, 20) == 1 && springSlot(G, 19, 0, 20) == -1);
  assert(springIndex(-1, 20) == 19 && springIndex(20, 20) == 0);
  assert(springShortest(18, 2, 20) == 4 && springShortest(2, 18, 20) == -4);
  // The 2.8″: nine to a turn at x3, the selected one x5 132 px up, a step per 40° round the rim.
  const SpringGeom O = springOrbit28();
  p = springPlace(O, 0, 20);
  assert(p.scale == 5 && fabsf(p.y - 108) < 0.01f);
  p = springPlace(O, 1, 20);
  assert(p.scale == 3 && fabsf(hypotf(p.x - 240, p.y - 240) - 188) < 0.01f);
  assert(springPlace(O, 8, 20).shown && !springPlace(O, 9, 20).shown);
  assert(fabsf(springStepDeg(O, 20) - 40) < 0.01f && fabsf(springStepDeg(G, 20) - 51.43f) < 0.01f);
}

static void testTurning() {
  Fleet f = crowd(12);
  RoundUi ui;
  uint32_t ms = 1000;
  ui.lastTouchMs = ms;
  roundSync(ui, kSpring, f, ms);
  const int first = sel(ui, f);
  // Swipe left: the next agent; right: back; the turn takes 300 ms.
  swipe(ui, f, 330, 150, ms);
  assert(sel(ui, f) == (first + 1) % 12);
  assert(springMoving(ui.spring, ms) && ui.spring.dur == kSpringStepMs);
  settle(ui, f, ms);
  swipe(ui, f, 150, 330, ms);
  swipe(ui, f, 150, 330, ms);
  assert(sel(ui, f) == (first + 11) % 12);   // wraps
  settle(ui, f, ms);
  assert(ui.spring.to >= 0 && ui.spring.to < 12);   // normalised at rest

  // A drag round the rim: the ring follows the finger, a step per 51°, and snaps on release.
  const int at = sel(ui, f);
  rim(ui, f, 120, 120 + 2 * 51.4f, ms, 40, 120);   // slowly clockwise: two earlier agents come to the seam
  assert(sel(ui, f) == (at + 10) % 12);
  settle(ui, f, ms);
  rim(ui, f, 200, 200 - 51.4f * 1.2f, ms, 40, 120);   // a little more than a step anticlockwise: one
  assert(sel(ui, f) == (at + 11) % 12);
  settle(ui, f, ms);
  // Mid-drag the ring sits between slots, under the finger.
  {
    const int s0 = sel(ui, f);
    float x, y;
    springPolar(205, 90, x, y);
    touch(ui, f, true, (int)x, (int)y, ms);
    for (int k = 1; k <= 10; k++) {
      springPolar(205, 90 + k * 2.57f, x, y);
      touch(ui, f, true, (int)x, (int)y, ms += 20);
    }
    const float pos = springPos(ui.spring, ms);
    assert(ui.spring.dragging && fabsf(pos - (s0 - 0.5f)) < 0.05f);
    touch(ui, f, false, 0, 0, ms += 300);
    settle(ui, f, ms);
  }
  // A fling: fast, released while moving: it keeps turning, at most one turn.
  const int before = sel(ui, f);
  rim(ui, f, 100, 100 + 60, ms, 5);   // 60° in 100 ms
  const int d = springShortest(before, sel(ui, f), 12);
  assert(d <= -2 && d >= -7);
  settle(ui, f, ms);
  rim(ui, f, 100, 100 + 300, ms, 2);   // very fast: still one turn at most
  assert(fabsf(ui.spring.to - ui.spring.from) <= 7.5f);
  settle(ui, f, ms);

  // A tap on a ring agent turns it to the seam the short way; a tap on the selected one opens it.
  const int s0 = sel(ui, f);
  const SpringPlace p = springPlace(G, 2, 12);
  touch(ui, f, true, (int)p.x, (int)p.y, ms);
  touch(ui, f, false, 0, 0, ms += 60);
  assert(sel(ui, f) == (s0 + 2) % 12 && ui.screen == RoundScreen::Home);
  assert(ui.spring.dur == 300);
  settle(ui, f, ms);
  touch(ui, f, true, 240, 100, ms);
  touch(ui, f, false, 0, 0, ms += 60);
  assert(ui.screen == RoundScreen::Detail && !strcmp(ui.openId, f.agents[(s0 + 2) % 12].id));
  // Back with a swipe down; then a tap in the middle of a waiter opens its prompt.
  for (int k = 0; k <= 6; k++) roundTouch(ui, kSpring, f, true, 240, 150 + k * 30, RoundScope::Act, 0, ms += 16);
  roundTouch(ui, kSpring, f, false, 0, 0, RoundScope::Act, 0, ms += 16);
  assert(ui.screen == RoundScreen::Home);
  wait(f.agents[sel(ui, f)]);
  roundSync(ui, kSpring, f, ms);
  touch(ui, f, true, 240, 250, ms += 100);
  touch(ui, f, false, 0, 0, ms += 60);
  assert(ui.screen == RoundScreen::Prompt);
  roundHome(ui);

  // Agents come and go: the ring stays on its agent.
  const int keep = sel(ui, f);
  char id[12];
  snprintf(id, sizeof id, "%s", f.agents[keep].id);
  for (int i = 0; i + 1 < f.count; i++) f.agents[i] = f.agents[i + 1];   // the first one leaves
  f.count--;
  roundSync(ui, kSpring, f, ms);
  assert(!strcmp(f.agents[sel(ui, f)].id, id) || keep == 0);
}

static void testWaiters() {
  Fleet f = crowd(20);
  RoundUi ui;
  uint32_t ms = 5000;
  ui.lastTouchMs = ms;
  roundSync(ui, kSpring, f, ms);
  springSet(ui.spring, 0);
  springFeature(ui, f);
  // A waiter off the turn (9 behind): nothing at once, then after 10 s alone it turns to it the short way.
  wait(f.agents[11]);
  roundSync(ui, kSpring, f, ms);
  assert(ui.wake && sel(ui, f) == 0);
  roundTick(ui, kSpring, f, ms + 5000);
  assert(sel(ui, f) == 0);
  ui.lastTouchMs = ms;
  roundTick(ui, kSpring, f, ms + kSpringWaiterMs);
  assert(sel(ui, f) == 11 && ui.spring.to == -9);
  assert(ui.spring.dur == 9 * kSpringFastMs);
  // An old waiter in the window is left alone; one off it draws the arc on its side.
  ms += 20000;
  roundSync(ui, kSpring, f, ms);
  springSet(ui.spring, 8);
  springFeature(ui, f);
  roundTick(ui, kSpring, f, ms + 1);
  assert(sel(ui, f) == 8 && !springMoving(ui.spring, ms + 1));   // 11 is on the turn
  assert(spring_draw::arrowSide(G, f, 8) == 0);
  assert(spring_draw::arrowSide(G, f, 15) == -1 && spring_draw::arrowSide(G, f, 3) == 1);
}

// The panel of a board drawn to in strips: the strips' changed spans land here.
static uint16_t* gPanel = nullptr;
static int gStrips = 0;
static void putStrip(const uint16_t* strip, int y0, int y1, const RoundSpans& sp) {
  for (int y = y0; y < y1; y++)
    for (int k = 0; k < sp.n[y]; k++)
      memcpy(gPanel + y * 480 + sp.x[y][k][0], strip + (y - y0) * 480 + sp.x[y][k][0],
             (size_t)(sp.x[y][k][1] - sp.x[y][k][0]) * 2);
  gStrips++;
}

// Draws the same sequence three ways: whole every frame; in part over the
// last frame; in strips onto a panel that keeps the last frame (falling back
// to whole frames off home, as the board does). They must match.
static void testPartial(const SpringLayout& lay) {
  const SpringPanel whole(lay), part(lay), strips(lay);
  std::vector<uint16_t> a(480 * 480), b(480 * 480), panel(480 * 480), canvas(480 * 480), strip(480 * 40, 0x1234);
  Canvas ca{a.data(), 480, 480}, cb{b.data(), 480, 480}, cc{canvas.data(), 480, 480};
  gPanel = panel.data();
  Fleet f = crowd(14);
  f.agents[3].unseen = true;
  wait(f.agents[9]);
  RoundUi ui;
  uint32_t ms = 1000;
  int64_t now = 2000;
  ui.lastTouchMs = ms;
  int partials = 0;
  NetView net;   // USB; Wi-Fi below (the mark under the middle)
  auto frame = [&]() {
    roundSync(ui, whole, f, ms);
    roundTick(ui, whole, f, ms);
    RoundInput in{&f, LinkState::Live, "dev", RoundScope::Act, ms, now, &ui, 0, &net};
    whole.draw(ca, in);
    in.partial = true;
    part.draw(cb, in);
    RoundDamage d;
    part.damage(d);
    if (!d.full) partials++;
    if (!strips.drawStrips(in, strip.data(), 40, putStrip)) {
      strips.draw(cc, in);
      memcpy(panel.data(), canvas.data(), panel.size() * 2);
    }
    for (int i = 0; i < 480 * 480; i++)
      if (a[i] != b[i] || a[i] != panel[i]) {
        fprintf(stderr, "%s frame differs at %d,%d (ms %u)\n", a[i] != b[i] ? "partial" : "strip", i % 480, i / 480, ms);
        assert(false);
      }
  };
  for (int k = 0; k < 6; k++) { frame(); ms += 100; }
  springStep(ui, f, -1, ms);
  for (int k = 0; k < 12; k++) { frame(); ms += 33; }
  springStep(ui, f, 3, ms);
  for (int k = 0; k < 16; k++) { frame(); ms += 33; }
  now += 70;   // the ages tick
  frame();
  // A drag, with frames between samples.
  for (int k = 0; k <= 12; k++) {
    float x, y;
    springPolar(205, 60 + k * 9.f, x, y);
    roundTouch(ui, whole, f, true, (int)x, (int)y, RoundScope::Act, 0, ms);
    frame();
    ms += 30;
  }
  roundTouch(ui, whole, f, false, 0, 0, RoundScope::Act, 0, ms);
  for (int k = 0; k < 12; k++) { frame(); ms += 33; }
  // The fleet changes under it; a toast; a detail and back.
  f.agents[2].status = Status::Error;
  f.count--;
  frame();
  roundToast(ui, "sent", ms);
  frame();
  ms += 2000;
  frame();
  roundOpen(ui, f, 1, RoundScreen::Detail);
  frame();
  roundHome(ui);
  frame();
  ms += 50;
  frame();
  // Few agents: the closed loop.
  f.count = 4;
  springStep(ui, f, 1, ms);
  for (int k = 0; k < 12; k++) { frame(); ms += 33; }
  // On Wi-Fi: the mark comes, a critter turns past it, a long toast over it, back to USB.
  ms += 3000;   // the toast above is gone
  frame();
  const int before = partials;
  net.via = NetVia::Wifi;
  net.phase = NetPhase::Online;
  frame();
  ms += 100;
  frame();
  springStep(ui, f, 2, ms);
  for (int k = 0; k < 14; k++) { frame(); ms += 33; }
  roundToast(ui, "Wi-Fi: network not found", ms);
  frame();
  ms += 3000;
  frame();
  frame();
  net.via = NetVia::Usb;
  frame();
  ms += 100;
  frame();
  assert(partials > before + 10);
  assert(partials > 40 && gStrips > 100);
}

int main() {
  testGeometry();
  testTurning();
  testWaiters();
  testPartial(layoutSpring21());
  testPartial(layoutSpring28());
  return 0;
}
