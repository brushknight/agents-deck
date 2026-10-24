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

static void testButtons() {
  uint32_t ms = 1000;
  Fleet f = fleetOf({mk("a", 0, Status::Running), mk("b", 1, Status::Idle)});
  const DetailStyle S = detailOrbit();
  DetailButton b[3];
  assert(detailButtons(S, f.agents[0], RoundScope::Admin, b) == 3);
  assert(b[0].kind == HitKind::Focus && b[1].kind == HitKind::Interrupt && b[2].kind == HitKind::Stop);
  assert(b[0].x > 240 && b[1].x == 240 && b[2].x < 240);   // focus right, stop left
  assert(detailButtons(S, f.agents[1], RoundScope::Admin, b) == 2);   // no interrupt when idle
  assert(detailButtons(S, f.agents[0], RoundScope::Act, b) == 2 && b[1].kind == HitKind::Interrupt);   // no stop
  assert(detailButtons(S, f.agents[0], RoundScope::View, b) == 0);

  RoundUi ui;
  roundSync(ui, kOrbit, f, ms);
  roundOpen(ui, f, 0, RoundScreen::Detail);
  detailButtons(S, f.agents[0], RoundScope::Admin, b);
  RoundCommand cmd = tap(ui, kOrbit, f, b[0].x, b[0].y, ms);
  assert(cmd.cmd == RoundCmd::Focus && !strcmp(cmd.agent, "a"));
  cmd = tap(ui, kOrbit, f, b[1].x + 10, b[1].y - 10, ms);
  assert(cmd.cmd == RoundCmd::Interrupt);
  cmd = tap(ui, kOrbit, f, b[0].x, b[0].y, ms, RoundScope::View);
  assert(cmd.cmd == RoundCmd::None);

  // Stop: a tap only says "hold to stop".
  cmd = tap(ui, kOrbit, f, b[2].x, b[2].y, ms);
  assert(cmd.cmd == RoundCmd::None && !strcmp(ui.toast, "hold to stop"));
  // Held 1 s: Dismiss, once.
  int fired = 0;
  roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Admin, 0, ms);
  assert(roundHoldPermille(ui, ms) == 0);
  for (uint32_t t = 16; t <= 1500; t += 16) {
    if (t > 480 && t < 520) assert(roundHoldPermille(ui, ms + t) > 450 && roundHoldPermille(ui, ms + t) < 550);
    if (roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Admin, 0, ms + t).cmd == RoundCmd::Dismiss) {
      fired++;
      assert(t >= kRoundHoldMs);
    }
  }
  assert(roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 1600).cmd == RoundCmd::None);
  assert(fired == 1);
  ms += 2000;
  // Let go early: nothing.
  roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Admin, 0, ms);
  assert(roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Admin, 0, ms + 900).cmd == RoundCmd::None);
  assert(roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 950).cmd == RoundCmd::None);
  // Slide off the button: cancelled.
  ms += 2000;
  roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Admin, 0, ms);
  roundTouch(ui, kOrbit, f, true, b[2].x + 40, b[2].y - 40, RoundScope::Admin, 0, ms + 100);
  assert(roundTouch(ui, kOrbit, f, true, b[2].x + 40, b[2].y - 40, RoundScope::Admin, 0, ms + 1200).cmd == RoundCmd::None);
  roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Admin, 0, ms + 1300);
  // Scope act: no stop to hold.
  ms += 2000;
  roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Act, 0, ms);
  assert(roundTouch(ui, kOrbit, f, true, b[2].x, b[2].y, RoundScope::Act, 0, ms + 1200).cmd == RoundCmd::None);
  roundTouch(ui, kOrbit, f, false, 0, 0, RoundScope::Act, 0, ms + 1300);

  // The buttons sit inside the circle, apart, below the activity line, clear of the context bar.
  for (const DetailStyle& st : {detailOrbit(), detailDial()}) {
    const int n = detailButtons(st, f.agents[0], RoundScope::Admin, b);
    for (int i = 0; i < n; i++) {
      const float d = sqrtf((float)(b[i].x - 240) * (b[i].x - 240) + (float)(b[i].y - 240) * (b[i].y - 240));
      assert(d + st.btnR + 7 + 3 <= 240);              // with the hold ring
      assert(b[i].y - st.btnR > st.activityBase + 8);   // under the text
      const float a = angleAt(240, 240, b[i].x, b[i].y);
      const float halfDeg = asinf((st.btnR + 9) / d) * 180 / kRoundPi;
      assert(!angleOnArc(a - halfDeg, st.arcStart, st.arcSweep) && !angleOnArc(a + halfDeg, st.arcStart, st.arcSweep));
      for (int k = i + 1; k < n; k++) {
        const float dx = (float)(b[i].x - b[k].x), dy = (float)(b[i].y - b[k].y);
        assert(sqrtf(dx * dx + dy * dy) >= 2 * st.btnR + 8);
      }
    }
  }
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

// The way back without a gesture: the "×" under the top edge on the detail
// and the prompt, and the detail's big critter; clear of everything else.
static void testBack() {
  for (const RoundLayout* L : {(const RoundLayout*)&kOrbit, (const RoundLayout*)&kDial}) {
    uint32_t ms = 1000;
    Fleet f = fleetOf({mk("a", 0, Status::Running), waiter("c", 1), mk("d", 2, Status::Idle)});
    RoundUi ui;
    roundSync(ui, *L, f, ms);
    roundOpen(ui, f, 0, RoundScreen::Detail);
    tap(ui, *L, f, kBackX, kBackY, ms);
    assert(ui.screen == RoundScreen::Home);
    roundOpen(ui, f, 0, RoundScreen::Detail);
    tap(ui, *L, f, kBackX + 25, kBackY + 20, ms);   // the hit circle is bigger than the button
    assert(ui.screen == RoundScreen::Home);
    // The big critter: home too.
    const DetailStyle S = L->kind() == RoundLayout::Kind::Orbit ? detailOrbit() : detailDial();
    roundOpen(ui, f, 0, RoundScreen::Detail);
    tap(ui, *L, f, 240, S.critterCy, ms);
    assert(ui.screen == RoundScreen::Home);
    // The prompt's "×" goes back where it came from.
    roundOpen(ui, f, 1, RoundScreen::Detail);
    roundOpen(ui, f, 1, RoundScreen::Prompt);
    assert(ui.promptFrom == RoundScreen::Detail);
    roundTouch(ui, *L, f, true, kBackX, kBackY, RoundScope::Act, 0, ms);
    assert(ui.pressed.kind == HitKind::Back);   // drawn pressed
    roundTouch(ui, *L, f, false, 0, 0, RoundScope::Act, 0, ms + 60);
    assert(ui.screen == RoundScreen::Detail);
    ms += 300;
    // Clear of the round buttons, the context bar, the dots and the pills.
    DetailButton b[3];
    const int nb = detailButtons(S, f.agents[0], RoundScope::Admin, b);
    for (int i = 0; i < nb; i++) {
      const float dx = (float)(b[i].x - kBackX), dy = (float)(b[i].y - kBackY);
      assert(sqrtf(dx * dx + dy * dy) > S.btnHit + kBackHit);
    }
    assert(kBackY - kBackR > 240 - (S.ringR - S.ringW / 2) + 2);   // under the bar's inner edge
    if (S.dots)
      for (int n : {1, 2, 3, 8, 22, 64})
        for (int i = 0; i < n; i++) {
          const RPoint p = detailDot(S, i, n);
          const float dx = (float)(p.x - kBackX), dy = (float)(p.y - kBackY);
          assert(sqrtf(dx * dx + dy * dy) > kBackR + 8 + 4);
        }
    for (int n = 1; n <= 6; n++) {
      Agent a = waiter("p", 0);
      a.optCount = (uint8_t)n;
      Pill pl[4];
      const int k = promptPills(a, 0, pl);
      for (int i = 0; i < k; i++) assert(pl[i].y > kBackY + kBackHit);
    }
    // The critter and the back button sit apart.
    assert(S.critterCy - 11 * S.critterScale > kBackY + kBackR);
  }
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
  testGestures();
  checkScreens(kOrbit);
  checkScreens(kDial);
  printf("round view: ok\n");
  return 0;
}
