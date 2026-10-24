// The round boards' state machine (round_view.h) driven through the Orbit
// and the Dial: what is featured, taps, swipes, the rim, the stop hold, the
// timeout, a new waiting agent, answering; and that every screen of both
// layouts stays inside the frame.
#define PANEL_MAX_AGENTS 64
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_dial.h"
#include "layout_orbit.h"

static Agent mk(const char* id, uint8_t slot, Status st, int64_t since = 1000) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.status = st;
  a.statusSince = since;
  return a;
}

static Agent waiter(const char* id, uint8_t slot, int64_t since = 1000) {
  Agent a = mk(id, slot, Status::Waiting, since);
  snprintf(a.promptId, sizeof a.promptId, "p-%s", id);
  snprintf(a.waitTitle, sizeof a.waitTitle, "allow bash?");
  a.optCount = 3;
  const char* labels[] = {"yes", "yes, always", "no"};
  for (int k = 0; k < 3; k++) {
    snprintf(a.opts[k].key, sizeof a.opts[k].key, "%d", k + 1);
    snprintf(a.opts[k].label, sizeof a.opts[k].label, "%s", labels[k]);
  }
  a.opts[0].primary = true;
  return a;
}

static Fleet fleetOf(std::initializer_list<Agent> as) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (const Agent& a : as) f.agents[f.count++] = a;
  f.total = f.count;
  return f;
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

static const OrbitPanel kOrbit(layoutOrbit28());
static const DialPanel kDial(layoutDial21());

// A tap: down and up at once.
static RoundCommand tap(RoundUi& ui, const RoundLayout& L, const Fleet& f, int x, int y, uint32_t& ms,
                        RoundScope s = RoundScope::Admin) {
  roundTouch(ui, L, f, true, x, y, s, 0, ms);
  ms += 60;
  RoundCommand c = roundTouch(ui, L, f, false, 0, 0, s, 0, ms);
  ms += 200;
  return c;
}

static void swipe(RoundUi& ui, const RoundLayout& L, const Fleet& f, int x0, int y0, int x1, int y1, uint32_t& ms) {
  for (int k = 0; k <= 6; k++) {
    roundTouch(ui, L, f, true, x0 + (x1 - x0) * k / 6, y0 + (y1 - y0) * k / 6, RoundScope::Admin, 0, ms);
    ms += 16;
  }
  roundTouch(ui, L, f, false, 0, 0, RoundScope::Admin, 0, ms);
  ms += 200;
}

// A finger round the rim from angle a0 to a1 (degrees, clockwise positive).
static void rimDrag(RoundUi& ui, const RoundLayout& L, const Fleet& f, float a0, float a1, uint32_t& ms) {
  const int steps = 20;
  for (int k = 0; k <= steps; k++) {
    const RPoint p = polar(240, 240, 215, a0 + (a1 - a0) * k / steps);
    roundTouch(ui, L, f, true, p.x, p.y, RoundScope::Admin, 0, ms);
    ms += 16;
  }
  roundTouch(ui, L, f, false, 0, 0, RoundScope::Admin, 0, ms);
  ms += 200;
}

static void testFeatured() {
  // Longest waiting > hungry > error > busiest running.
  Agent run1 = mk("r1", 0, Status::Running, 500), run2 = mk("r2", 1, Status::Running, 900);
  run2.subagents = 2;
  Agent err = mk("er", 2, Status::Error, 700);
  Agent hun = mk("hu", 3, Status::Idle, 600);
  hun.unseen = true;
  Agent w1 = waiter("w1", 4, 800), w2 = waiter("w2", 5, 400);
  Fleet f = fleetOf({run1, run2, err, hun, w1, w2});
  assert(!strcmp(f.agents[roundAutoFeatured(f)].id, "w2"));   // waiting the longest
  f = fleetOf({run1, run2, err, hun});
  assert(!strcmp(f.agents[roundAutoFeatured(f)].id, "hu"));
  f = fleetOf({run1, run2, err});
  assert(!strcmp(f.agents[roundAutoFeatured(f)].id, "er"));
  f = fleetOf({run1, run2});
  assert(!strcmp(f.agents[roundAutoFeatured(f)].id, "r2"));   // more subagents
  f = fleetOf({mk("i1", 0, Status::Idle)});
  assert(roundAutoFeatured(f) == 0);
  f = fleetOf({});
  assert(roundAutoFeatured(f) == -1);
  f = fleetOf({w1, waiter("w3", 6), w2});
  assert(othersWaiting(f, 0) == 2);
}

static void testOrbitNavigation() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle), waiter("c", 2)});
  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  assert(ui.wake && ui.screen == RoundScreen::Home && !strcmp(ui.featureId, "c"));   // c is news
  ui.wake = false;

  // A ring critter: its detail. Three agents: slots at 0°, 120°, 240° on r 178.
  RPoint p = polar(240, 240, 178, 120);
  RoundCommand cmd = tap(ui, kOrbit, f, p.x, p.y, ms);
  assert(cmd.cmd == RoundCmd::None && ui.screen == RoundScreen::Detail && !strcmp(ui.openId, "b"));
  // Swipe left / right: the neighbours, in slot order.
  swipe(ui, kOrbit, f, 300, 240, 150, 240, ms);
  assert(!strcmp(ui.openId, "c"));
  swipe(ui, kOrbit, f, 150, 240, 300, 240, ms);
  swipe(ui, kOrbit, f, 150, 240, 300, 240, ms);
  assert(!strcmp(ui.openId, "a"));
  // Swipe down: home.
  swipe(ui, kOrbit, f, 240, 150, 240, 330, ms);
  assert(ui.screen == RoundScreen::Home);

  // The middle: the featured waiter's prompt.
  tap(ui, kOrbit, f, 240, 240, ms);
  assert(ui.screen == RoundScreen::Prompt && !strcmp(ui.openId, "c") && ui.promptFrom == RoundScreen::Home);
  swipe(ui, kOrbit, f, 240, 150, 240, 330, ms);
  assert(ui.screen == RoundScreen::Home);

  // From a detail to the prompt, and back to the detail.
  p = polar(240, 240, 178, 240);
  tap(ui, kOrbit, f, p.x, p.y, ms);
  assert(ui.screen == RoundScreen::Detail && !strcmp(ui.openId, "c"));
  tap(ui, kOrbit, f, 240, 200, ms);
  assert(ui.screen == RoundScreen::Prompt && ui.promptFrom == RoundScreen::Detail);
  swipe(ui, kOrbit, f, 240, 150, 240, 330, ms);
  assert(ui.screen == RoundScreen::Detail);

  // 30 s untouched: home.
  roundTick(ui, kOrbit, f, ms + kRoundIdleMs - 300);
  assert(ui.screen == RoundScreen::Detail);
  roundTick(ui, kOrbit, f, ms + kRoundIdleMs);
  assert(ui.screen == RoundScreen::Home);

  // The middle of a calm fleet: the featured agent's detail.
  Fleet calm = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle)});
  RoundUi u2;
  roundSync(u2, kOrbit, calm, ms);
  tap(u2, kOrbit, calm, 240, 240, ms);
  assert(u2.screen == RoundScreen::Detail && !strcmp(u2.openId, "a"));
  // A touch stamped a little ahead of the clock (a sample read during the frame) is not 49 days old.
  u2.lastTouchMs = ms + 50;
  roundTick(u2, kOrbit, calm, ms);
  assert(u2.screen == RoundScreen::Detail);
}

static void testAnswer() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), waiter("c", 1)});
  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  tap(ui, kOrbit, f, 240, 240, ms);
  assert(ui.screen == RoundScreen::Prompt);
  // Scope view: the pills don't answer.
  Pill pills[4];
  const int n = promptPills(f.agents[1], 0, pills);
  assert(n == 3);
  RoundCommand cmd = tap(ui, kOrbit, f, 240, pills[1].y + 30, ms, RoundScope::View);
  assert(cmd.cmd == RoundCmd::None && ui.screen == RoundScreen::Prompt);
  // Scope act: the second pill sends Answer(c, p-c, "2"), says "sent", then goes back.
  roundTouch(ui, kOrbit, f, true, 240, pills[1].y + 30, RoundScope::Act, 0, ms);
  assert(ui.pressed.kind == HitKind::Option && ui.pressed.option == 1);   // drawn pressed
  cmd = roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Act, 0, ms + 80);
  assert(cmd.cmd == RoundCmd::Answer && !strcmp(cmd.agent, "c") && !strcmp(cmd.prompt, "p-c") && !strcmp(cmd.key, "2"));
  assert(!strcmp(ui.toast, "sent") && ui.backAfterToast);
  // A second tap while "sent" shows sends nothing.
  ms += 100;
  cmd = tap(ui, kOrbit, f, 240, pills[0].y + 30, ms, RoundScope::Act);
  assert(cmd.cmd == RoundCmd::None);
  roundTick(ui, kOrbit, f, ms + kRoundSentMs);
  assert(ui.screen == RoundScreen::Home);

  // 409: the prompt changed; the toast says so.
  ui.cmdSeq = 7;
  roundResult(ui, 7, 409, ms);
  assert(!strcmp(ui.toast, "the prompt changed") && ui.cmdSeq == 0);
  roundResult(ui, 8, 500, ms);   // not ours: ignored
  assert(!strcmp(ui.toast, "the prompt changed"));

  // Answered elsewhere while the prompt is open: back.
  tap(ui, kOrbit, f, 240, 240, ms);
  assert(ui.screen == RoundScreen::Prompt);
  Fleet done = fleetOf({mk("a", 0, Status::Running), mk("c", 1, Status::Running)});
  roundSync(ui, kOrbit, done, ms);
  assert(ui.screen == RoundScreen::Home);

  // More than three options scroll with swipes up; swipe down scrolls back, then leaves.
  Agent many = waiter("m", 0);
  many.optCount = 5;
  for (int k = 3; k < 5; k++) snprintf(many.opts[k].key, sizeof many.opts[k].key, "%d", k + 1);
  Fleet fm = fleetOf({many});
  RoundUi um;
  roundSync(um, kOrbit, fm, ms);
  tap(um, kOrbit, fm, 240, 240, ms);
  assert(um.screen == RoundScreen::Prompt && promptPills(many, 0, pills) == 4);
  swipe(um, kOrbit, fm, 240, 400, 240, 200, ms);
  assert(um.optFirst == 1);
  swipe(um, kOrbit, fm, 240, 400, 240, 200, ms);
  assert(um.optFirst == 1);   // the last four are showing
  assert(promptPills(many, um.optFirst, pills) == 4 && pills[3].option == 4);
  swipe(um, kOrbit, fm, 240, 200, 240, 400, ms);
  assert(um.optFirst == 0 && um.screen == RoundScreen::Prompt);
  swipe(um, kOrbit, fm, 240, 200, 240, 400, ms);
  assert(um.screen == RoundScreen::Home);
}

// The detail's row as it settles: kind and centre of each button, left to right.
struct RowB { HitKind kind; int x, y; };
static int rowOf(const DetailStyle& S, const Agent& a, RoundScope scope, RowB out[4]) {
  const uint8_t m = detailRowMask(a, scope);
  int n = 0;
  for (int i = 0; i < 4; i++) {
    const float ang = rowAngle(S, m, (uint8_t)(1 << i));
    if (ang < 0) continue;
    const RPoint p = polar(240, 240, S.btnArcR, ang);
    out[n++] = RowB{kRowKinds[i], p.x, p.y};
  }
  return n;
}

static void testButtons() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle)});
  const DetailStyle S = detailOrbit();
  RowB b[4];
  // Close on the left, then focus, interrupt (running), stop (admin).
  assert(rowOf(S, f.agents[0], RoundScope::Admin, b) == 4);
  assert(b[0].kind == HitKind::Back && b[1].kind == HitKind::Focus && b[2].kind == HitKind::Interrupt &&
         b[3].kind == HitKind::Stop);
  assert(b[0].x < b[1].x && b[1].x < b[2].x && b[2].x < b[3].x);
  assert(rowOf(S, f.agents[1], RoundScope::Admin, b) == 3);   // no interrupt when idle
  assert(rowOf(S, f.agents[0], RoundScope::Act, b) == 3 && b[2].kind == HitKind::Interrupt);   // no stop
  assert(rowOf(S, f.agents[0], RoundScope::View, b) == 1 && b[0].kind == HitKind::Back);       // close only
  assert(rowOf(S, f.agents[0], RoundScope::View, b) == 1 && b[0].x == 240);                   // centred

  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  roundOpen(ui, f, 0, RoundScreen::Detail);
  rowOf(S, f.agents[0], RoundScope::Admin, b);
  RoundCommand cmd = tap(ui, kOrbit, f, b[1].x, b[1].y, ms);
  assert(cmd.cmd == RoundCmd::Focus && !strcmp(cmd.agent, "a"));
  cmd = tap(ui, kOrbit, f, b[2].x + 10, b[2].y - 10, ms);
  assert(cmd.cmd == RoundCmd::Interrupt);
  cmd = tap(ui, kOrbit, f, b[1].x, b[1].y, ms, RoundScope::View);   // with view, focus isn't there
  assert(cmd.cmd == RoundCmd::None);

  // Stop: a tap only says "hold to stop".
  roundOpen(ui, f, 0, RoundScreen::Detail);
  cmd = tap(ui, kOrbit, f, b[3].x, b[3].y, ms);
  assert(cmd.cmd == RoundCmd::None && !strcmp(ui.toast, "hold to stop"));
  // Held 1 s: Dismiss, once.
  int fired = 0;
  roundTouch(ui, kOrbit, f, true, b[3].x, b[3].y, RoundScope::Admin, 0, ms);
  assert(roundHoldPermille(ui, ms) == 0);
  for (uint32_t t = 16; t <= 1500; t += 16) {
    if (t > 480 && t < 520) assert(roundHoldPermille(ui, ms + t) > 450 && roundHoldPermille(ui, ms + t) < 550);
    if (roundTouch(ui, kOrbit, f, true, b[3].x, b[3].y, RoundScope::Admin, 0, ms + t).cmd == RoundCmd::Dismiss) {
      fired++;
      assert(t >= kRoundHoldMs);
    }
  }
  assert(roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 1600).cmd == RoundCmd::None);
  assert(fired == 1);
  ms += 2000;
  // Let go early: nothing.
  roundTouch(ui, kOrbit, f, true, b[3].x, b[3].y, RoundScope::Admin, 0, ms);
  assert(roundTouch(ui, kOrbit, f, true, b[3].x, b[3].y, RoundScope::Admin, 0, ms + 900).cmd == RoundCmd::None);
  assert(roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 950).cmd == RoundCmd::None);
  // Slide off the button: cancelled.
  ms += 2000;
  roundTouch(ui, kOrbit, f, true, b[3].x, b[3].y, RoundScope::Admin, 0, ms);
  roundTouch(ui, kOrbit, f, true, b[3].x - 40, b[3].y - 40, RoundScope::Admin, 0, ms + 100);
  assert(roundTouch(ui, kOrbit, f, true, b[3].x - 40, b[3].y - 40, RoundScope::Admin, 0, ms + 1200).cmd == RoundCmd::None);
  roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 1300);

  // The row for 1 to 4 buttons, on both boards: inside the circle, apart, under
  // the text, and the bar's ends clear of it (the bar grows with fewer buttons).
  for (const DetailStyle& st : {detailOrbit(), detailDial()}) {
    float lastExtent = 0;
    for (uint8_t m : {(uint8_t)1, (uint8_t)3, (uint8_t)7, (uint8_t)15}) {
      const int n = maskCount(m);
      const float extent = barExtent(st, n);
      assert(extent > lastExtent);
      lastExtent = extent;
      for (int i = 0; i < 4; i++) {
        const float ang = rowAngle(st, m, (uint8_t)(1 << i));
        if (ang < 0) continue;
        const RPoint p = polar(240, 240, st.btnArcR, ang);
        assert(st.btnArcR + st.btnR + 7 + 3 <= 240);              // with the hold ring
        assert(p.y - st.btnR > st.modelBase + 10);                // under the text
        const float halfDeg = asinf(st.btnR / st.btnArcR) * 180 / kRoundPi;
        assert(fabsf(ang - 180) + halfDeg < extent);              // the bar stops short of it
        for (int k = i + 1; k < 4; k++) {
          const float ang2 = rowAngle(st, m, (uint8_t)(1 << k));
          if (ang2 < 0) continue;
          const RPoint q = polar(240, 240, st.btnArcR, ang2);
          const float d = sqrtf((float)(p.x - q.x) * (p.x - q.x) + (float)(p.y - q.y) * (p.y - q.y));
          assert(d >= 2 * st.btnR + 8);                           // apart
          assert(d >= 2 * st.btnHit - 6);                         // hit circles barely touch
        }
      }
    }
  }
}

// The row and the bar ease to a new set of buttons over kRowAnimMs.
static void testRowAnimation() {
  const DetailStyle S = detailOrbit();
  Agent run = mk("a", 0, Status::Running), idle = mk("a", 0, Status::Idle);
  RoundUi ui;
  const uint8_t m4 = detailRowMask(run, RoundScope::Admin), m3 = detailRowMask(idle, RoundScope::Admin);
  assert(maskCount(m4) == 4 && maskCount(m3) == 3);
  roundRowSync(ui, "a", m4, 1000);
  RowFrame f = rowFrame(S, ui, "a", m4, 1000);
  assert(f.n == 4 && fabsf(f.arcSweep - (360 - 2 * barExtent(S, 4))) < 0.01f);   // a new agent: settled at once
  // It goes idle: interrupt leaves, the others close up, the bar grows.
  roundRowSync(ui, "a", m3, 2000);
  f = rowFrame(S, ui, "a", m3, 2000);
  assert(f.n == 4 && fabsf(f.arcSweep - (360 - 2 * barExtent(S, 4))) < 0.01f);   // the start
  float lastSweep = f.arcSweep, lastR = 1e9f;
  for (uint32_t t = 2016; t <= 2000 + kRowAnimMs; t += 16) {
    f = rowFrame(S, ui, "a", m3, t);
    assert(f.arcSweep >= lastSweep - 0.001f);   // grows steadily
    lastSweep = f.arcSweep;
    for (int i = 0; i < f.n; i++)
      if (f.btn[i].kind == HitKind::Interrupt) { assert(f.btn[i].r <= lastR + 0.001f); lastR = f.btn[i].r; }
  }
  f = rowFrame(S, ui, "a", m3, 2000 + kRowAnimMs);
  assert(f.n == 3 && fabsf(f.arcSweep - (360 - 2 * barExtent(S, 3))) < 0.01f);   // settled
  // Half way it is more than half done (ease out).
  roundRowSync(ui, "a", m4, 5000);
  const float mid = rowProgress(ui, 5000 + kRowAnimMs / 2);
  assert(mid > 0.6f && mid < 1);
  // Unsynced state (a host render) draws the target settled.
  RoundUi fresh;
  f = rowFrame(S, fresh, "a", m3, 0);
  assert(f.n == 3);
  // Another agent: no animation.
  roundRowSync(ui, "b", m3, 6000);
  assert(rowProgress(ui, 6000) == 1);
}

static void testNewWaiting() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle)});
  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  assert(!ui.wake);
  roundOpen(ui, f, 0, RoundScreen::Detail);
  // b starts waiting: home, featuring b, and the screen wakes; never the prompt.
  Fleet g = fleetOf({mk("a", 0, Status::Running), waiter("b", 1)});
  roundSync(ui, kOrbit, g, ms);
  assert(ui.screen == RoundScreen::Home && ui.wake && !strcmp(ui.featureId, "b"));
  ui.wake = false;
  roundSync(ui, kOrbit, g, ms);   // the same waiter again is no news
  assert(!ui.wake);
  // An older waiter does not steal the middle from the new one.
  Fleet h = fleetOf({waiter("a", 0, 100), waiter("b", 1, 1000)});
  RoundUi u2;
  roundSync(u2, kOrbit, g, ms);
  roundSync(u2, kOrbit, h, ms);   // a is new now
  assert(!strcmp(u2.featureId, "a"));
  // Once answered, the orbit goes back to the priority order.
  Fleet k = fleetOf({mk("a", 0, Status::Running), waiter("b", 1, 1000)});
  roundSync(u2, kOrbit, k, ms);
  assert(u2.featureId[0] == '\0' && !strcmp(k.agents[roundFeatured(u2, k)].id, "b"));

  // The dial jumps to the new waiter and keeps it until the timeout.
  RoundUi d;
  roundSync(d, kDial, f, ms);
  roundSync(d, kDial, g, ms);
  assert(!strcmp(g.agents[roundFeatured(d, g)].id, "b"));
  roundSync(d, kDial, f, ms);   // answered
  assert(!strcmp(f.agents[roundFeatured(d, f)].id, "b"));
  d.lastTouchMs = ms;
  roundTick(d, kDial, f, ms + kRoundIdleMs);
  assert(d.featureId[0] == '\0' && !strcmp(f.agents[roundFeatured(d, f)].id, "a"));

  // A new waiter while answering that very prompt doesn't yank the screen.
  RoundUi u3;
  roundSync(u3, kOrbit, g, ms);
  tap(u3, kOrbit, g, 240, 240, ms);
  assert(u3.screen == RoundScreen::Prompt);
  Agent again = waiter("b", 1, 2000);   // the same agent, a new prompt
  Fleet g2 = fleetOf({mk("a", 0, Status::Running), again});
  u3.waitCount = 0;                     // as if it had stopped waiting in between
  roundSync(u3, kOrbit, g2, ms);
  assert(u3.screen == RoundScreen::Prompt);
}

static void testPaging() {
  uint32_t ms = 1000;
  Fleet f = crowd(22);
  assert(kOrbit.pages(f) == 2);
  const orbit_detail::Ring r0 = orbit_detail::ring(f, 0), r1 = orbit_detail::ring(f, 1);
  assert(r0.shown == 15 && r0.chip && r0.more == 7 && r0.slots == 16 && r0.scale == 2);
  assert(r1.first == 15 && r1.shown == 7 && r1.chip && r1.more == 15 && r1.slots == 8 && r1.scale == 3);
  assert(kOrbit.pageOf(f, 16) == 1 && kOrbit.pageOf(f, 3) == 0);
  // Up to 16 on one ring, no chip; x3 up to 10 slots.
  Fleet s16 = crowd(16), s10 = crowd(10), s2 = crowd(2);
  assert(kOrbit.pages(s16) == 1 && !orbit_detail::ring(s16, 0).chip && orbit_detail::ring(s16, 0).scale == 2);
  assert(orbit_detail::ring(s10, 0).scale == 3 && orbit_detail::ring(s2, 0).slots == 3);
  // More on the deck than the board keeps: still a chip.
  Fleet kept = crowd(16);
  kept.total = 30;
  assert(kOrbit.pages(kept) == 2 && orbit_detail::ring(kept, 0).more == 15);

  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  // Drag round the rim: the next page, and back.
  rimDrag(ui, kOrbit, f, 10, 60, ms);
  assert(ui.page == 1 && ui.screen == RoundScreen::Home);
  rimDrag(ui, kOrbit, f, 60, 10, ms);
  assert(ui.page == 0);
  // A short nudge is not a drag, and a drag never taps what it started on.
  rimDrag(ui, kOrbit, f, 0, 8, ms);
  assert(ui.page == 0 && ui.screen == RoundScreen::Home);
  // The chip: the next page.
  const RPoint chip = polar(240, 240, r0.radius, 360.f * 15 / 16);
  tap(ui, kOrbit, f, chip.x, chip.y, ms);
  assert(ui.page == 1);
  // Page 2's critters open the right agents.
  const RPoint p = polar(240, 240, r1.radius, 360.f * 2 / 8);
  tap(ui, kOrbit, f, p.x, p.y, ms);
  assert(ui.screen == RoundScreen::Detail && !strcmp(ui.openId, "a17"));
  // The fleet shrinks: the page stays valid.
  swipe(ui, kOrbit, f, 240, 150, 240, 330, ms);
  Fleet small = crowd(5);
  roundSync(ui, kOrbit, small, ms);
  assert(ui.page == 0);
}

static void testDial() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle), mk("c", 2, Status::Idle)});
  RoundUi ui;
  roundSync(ui, kDial, f, ms);
  assert(roundFeatured(ui, f) == 0);
  swipe(ui, kDial, f, 330, 240, 150, 240, ms);   // left: next
  assert(roundFeatured(ui, f) == 1);
  swipe(ui, kDial, f, 150, 240, 330, 240, ms);
  swipe(ui, kDial, f, 150, 240, 330, 240, ms);   // right: previous, wrapping
  assert(roundFeatured(ui, f) == 2);
  // No rim drag on the dial: a drag round the edge is just a swipe (or nothing).
  rimDrag(ui, kDial, f, 300, 330, ms);
  assert(ui.screen == RoundScreen::Home);
  // The middle: the shown agent's detail; swipe down: back.
  tap(ui, kDial, f, 240, 260, ms);
  assert(ui.screen == RoundScreen::Detail);
  swipe(ui, kDial, f, 240, 150, 240, 330, ms);
  assert(ui.screen == RoundScreen::Home);
}

// Every screen of a layout, drawn with a guard band round the frame.
static void checkScreens(const RoundLayout& L) {
  static DeckFleet d;
  d = DeckFleet();
  assert(fixture::load(FIXTURE, d));
  const int PAD = 2 * 480;
  const uint16_t GUARD = 0xDEAD;
  std::vector<uint16_t> mem(480 * 480 + 2 * PAD, GUARD);
  Canvas c{mem.data() + PAD, 480, 480};
  auto guards = [&]() {
    for (int i = 0; i < PAD; i++) assert(mem[i] == GUARD && mem[mem.size() - 1 - i] == GUARD);
  };
  static Fleet f;
  f = d.fleet;
  const int64_t now = f.serverTime + 754;
  RoundUi ui;
  RoundInput in{&f, LinkState::Live, "waveshare-touch-lcd-28c-3c71bf", RoundScope::Admin, 0, now, &ui, 75};
  for (uint32_t ms : {0u, 250u, 777u}) {
    in.ms = ms;
    for (int page = 0; page < 2; page++) {
      ui = RoundUi();
      ui.page = page;
      roundSync(ui, L, f, ms);
      L.draw(c, in);
      guards();
      for (int i = 0; i < f.count; i++) {
        for (RoundScreen s : {RoundScreen::Detail, RoundScreen::Prompt}) {
          roundOpen(ui, f, i, s);
          for (RoundScope sc : {RoundScope::View, RoundScope::Act, RoundScope::Admin}) {
            in.scope = sc;
            L.draw(c, in);
            guards();
          }
          ui.down = true;
          ui.pressed.kind = HitKind::Stop;
          L.draw(c, in);
          ui.down = false;
          ui.pressed = RoundHit();
        }
        roundHome(ui);
      }
    }
    for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Lost}) {
      in.link = ls;
      L.draw(c, in);
      guards();
      // The corners are masked: the glass is round.
      assert(c.px[0] == 0 && c.px[479] == 0 && c.px[480 * 479] == 0);
    }
    in.link = LinkState::Live;
    in.fleet = nullptr;
    L.draw(c, in);
    guards();
    in.fleet = &f;
  }
  // A crowd of 64 and a single agent.
  static Fleet big;
  big = crowd(64);
  in.fleet = &big;
  ui = RoundUi();
  for (int page = 0; page < 5; page++) {
    ui.page = page;
    roundSync(ui, L, big, 0);
    L.draw(c, in);
    guards();
  }
  static Fleet one;
  one = fleetOf({waiter("solo", 0)});
  in.fleet = &one;
  ui = RoundUi();
  roundSync(ui, L, one, 0);
  L.draw(c, in);
  guards();
}

// The prompt's pills fit inside the circle and never overlap, for 1 to 6 answers.
static void testPills() {
  for (int n = 1; n <= 6; n++) {
    Agent a = waiter("p", 0);
    a.optCount = (uint8_t)n;
    for (int first = 0; first < 3; first++) {
      Pill p[4];
      const int k = promptPills(a, first, p);
      assert(k == (n > 3 ? 4 : n));
      for (int i = 0; i < k; i++) {
        assert(p[i].h >= 60 && p[i].w >= 160 && p[i].w <= 340);
        // Every corner of the pill's straight part is inside the panel.
        const float r = p[i].h / 2.f;
        for (float yy : {(float)p[i].y, (float)(p[i].y + p[i].h)})
          for (float xx : {p[i].x + r, p[i].x + p[i].w - r}) assert(inCircle(240, 240, 236, xx, yy));
        if (i) assert(p[i].y >= p[i - 1].y + p[i - 1].h + 4);
        assert(p[i].y > 160);   // under the title and the detail
      }
    }
  }
}

// The way back without a gesture: the detail's close button on the left of
// its row, its big critter, and the prompt's "×" under the top edge.
static void testBack() {
  for (const RoundLayout* L : {(const RoundLayout*)&kOrbit, (const RoundLayout*)&kDial}) {
    uint32_t ms = 1000;
    Fleet f = fleetOf({mk("a", 0, Status::Running), waiter("c", 1), mk("d", 2, Status::Idle)});
    const DetailStyle S = L->kind() == RoundLayout::Kind::Orbit ? detailOrbit() : detailDial();
    for (RoundScope sc : {RoundScope::View, RoundScope::Act, RoundScope::Admin}) {
      RowB b[4];
      rowOf(S, f.agents[0], sc, b);
      assert(b[0].kind == HitKind::Back);
      RoundUi ui;
      roundSync(ui, *L, f, ms);
      roundOpen(ui, f, 0, RoundScreen::Detail);
      tap(ui, *L, f, b[0].x, b[0].y, ms, sc);
      assert(ui.screen == RoundScreen::Home);
      // The big critter: home too.
      roundOpen(ui, f, 0, RoundScreen::Detail);
      tap(ui, *L, f, 240, S.critterCy, ms, sc);
      assert(ui.screen == RoundScreen::Home);
    }
    // The prompt's "×" goes back where it came from.
    RoundUi ui;
    roundSync(ui, *L, f, ms);
    roundOpen(ui, f, 1, RoundScreen::Detail);
    roundOpen(ui, f, 1, RoundScreen::Prompt);
    assert(ui.promptFrom == RoundScreen::Detail);
    roundTouch(ui, *L, f, true, kBackX, kBackY, RoundScope::Act, 0, ms);
    assert(ui.pressed.kind == HitKind::Back);   // drawn pressed
    roundTouch(ui, *L, f, false, 0, 0, RoundScope::Act, 0, ms + 60);
    assert(ui.screen == RoundScreen::Detail);
    for (int n = 1; n <= 6; n++) {
      Agent a = waiter("p", 0);
      a.optCount = (uint8_t)n;
      Pill pl[4];
      const int k = promptPills(a, 0, pl);
      for (int i = 0; i < k; i++) assert(pl[i].y > kBackY + kBackHit);
    }
    // The critter clears the bar, and the text clears the critter.
    assert(S.critterCy - 11 * S.critterScale > 240 - (S.ringR - S.ringW / 2) + 4);
    assert(S.titleBase - fontAscent(*S.titleFont) > S.critterCy + 6 * S.critterScale);
  }
}

// The dial's rim is a slider: a finger on the ring picks the dot under it,
// follows it live, and leaves it selected; nothing else happens.
static void testSlider() {
  Fleet f = crowd(8);
  RoundUi ui;
  uint32_t ms = 1000;
  roundSync(ui, kDial, f, ms);
  assert(dial_detail::dotAt(8, 0) == 0 && dial_detail::dotAt(8, 44) == 1 && dial_detail::dotAt(8, 350) == 0 &&
         dial_detail::dotAt(8, 200) == 4 && dial_detail::dotAt(0, 10) == -1);
  // Down on the ring at 3 o'clock: agent 2 at once.
  RPoint p = polar(240, 240, 220, 90);
  roundTouch(ui, kDial, f, true, p.x, p.y, RoundScope::Act, 0, ms);
  assert(ui.sliding && !strcmp(f.agents[roundFeatured(ui, f)].id, "a02"));
  // Round to 6 o'clock: each dot in turn.
  int seen = 2;
  for (int a = 90; a <= 180; a += 5) {
    p = polar(240, 240, 215, (float)a);
    roundTouch(ui, kDial, f, true, p.x, p.y, RoundScope::Act, 0, ms += 10);
    const int cur = roundFeatured(ui, f);
    assert(cur == seen || cur == seen + 1);
    seen = cur;
  }
  assert(seen == 4);
  roundTouch(ui, kDial, f, false, 0, 0, RoundScope::Act, 0, ms += 10);
  assert(!ui.sliding && ui.lastGesture == RoundGesture::Rim && ui.screen == RoundScreen::Home);
  assert(!strcmp(f.agents[roundFeatured(ui, f)].id, "a04"));   // stays
  // A tap in the middle still opens the detail; a swipe from the middle still changes agent.
  swipe(ui, kDial, f, 300, 240, 160, 240, ms);
  assert(!strcmp(f.agents[roundFeatured(ui, f)].id, "a05"));
  tap(ui, kDial, f, 240, 240, ms);
  assert(ui.screen == RoundScreen::Detail);
  // The orbit's rim is no slider.
  RoundUi uo;
  roundSync(uo, kOrbit, f, ms);
  p = polar(240, 240, 220, 90);
  roundTouch(uo, kOrbit, f, true, p.x, p.y, RoundScope::Act, 0, ms);
  assert(!uo.sliding);
  roundTouch(uo, kOrbit, f, false, 0, 0, RoundScope::Act, 0, ms + 50);
}

// Gestures from the touch samples a board really gets: every direction, from
// dense samples (a 10 ms touch task) and from just two (down and one move).
static void testGestures() {
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle), mk("c", 2, Status::Idle)});
  struct Case { int x0, y0, x1, y1; RoundGesture want; } cases[] = {
      {330, 240, 150, 240, RoundGesture::SwipeLeft}, {150, 240, 330, 240, RoundGesture::SwipeRight},
      {240, 330, 240, 150, RoundGesture::SwipeUp},   {240, 150, 240, 330, RoundGesture::SwipeDown},
  };
  for (const Case& k : cases)
    for (int samples : {2, 20}) {
      RoundUi ui;
      uint32_t ms = 1000;
      roundSync(ui, kDial, f, ms);
      roundOpen(ui, f, 0, RoundScreen::Detail);
      for (int i = 0; i < samples; i++) {
        const int x = k.x0 + (k.x1 - k.x0) * i / (samples - 1), y = k.y0 + (k.y1 - k.y0) * i / (samples - 1);
        roundTouch(ui, kDial, f, true, x, y, RoundScope::Act, 0, ms);
        ms += samples == 2 ? 120 : 10;
      }
      roundTouch(ui, kDial, f, false, 0, 0, RoundScope::Act, 0, ms);
      assert(ui.lastGesture == k.want);
      if (k.want == RoundGesture::SwipeDown) assert(ui.screen == RoundScreen::Home);
    }
  // One sample only (the finger already gone at the next read) is a tap, not a swipe.
  RoundUi ui;
  roundSync(ui, kDial, f, 1000);
  roundTouch(ui, kDial, f, true, 240, 150, RoundScope::Act, 0, 1000);
  roundTouch(ui, kDial, f, false, 0, 0, RoundScope::Act, 0, 1100);
  assert(ui.lastGesture == RoundGesture::Tap);
}

int main() {
  testFeatured();
  testOrbitNavigation();
  testAnswer();
  testButtons();
  testNewWaiting();
  testPaging();
  testDial();
  testPills();
  testBack();
  testSlider();
  testRowAnimation();
  testGestures();
  checkScreens(kOrbit);
  checkScreens(kDial);
  printf("round view: ok\n");
  return 0;
}
