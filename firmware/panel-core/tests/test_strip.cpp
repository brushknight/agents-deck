// layout_strip.h: the 640 x 172 strip's home (a sideways row of tiles), card,
// prompt and notices; touch, scrolling, holds and hit-testing.
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_strip.h"

static Agent mk(const char* id, uint8_t slot, Status st) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.status = st;
  return a;
}

static Fleet fleetOf(int n, Status st = Status::Idle) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (int i = 0; i < n; i++) {
    char id[16];
    snprintf(id, sizeof id, "a%d", i);
    f.agents[f.count++] = mk(id, (uint8_t)i, st);
  }
  f.total = f.count;
  return f;
}

// A waiting agent with n options and long labels; a permission with a command if perm.
static Agent asking(const char* id, int n, bool perm) {
  Agent a = mk(id, 0, Status::Waiting);
  snprintf(a.promptId, sizeof a.promptId, "p-%s", id);
  snprintf(a.waitKind, sizeof a.waitKind, "%s", perm ? "permission" : "question");
  snprintf(a.waitTitle, sizeof a.waitTitle, "which of these long and winding options should the agent take next, please?");
  if (perm) snprintf(a.waitDetail, sizeof a.waitDetail, "terraform apply -target=module.staging_net -auto-approve -var-file=staging.tfvars");
  a.optCount = (uint8_t)n;
  for (int i = 0; i < n; i++) {
    snprintf(a.opts[i].key, sizeof a.opts[i].key, "%d", (i + 1) % 100);
    snprintf(a.opts[i].label, sizeof a.opts[i].label, "option %d, a long label \xE2\x80\x94 a hint", (i + 1) % 100);
  }
  return a;
}

static const StripPanel P;

static DeckCommand touchAt(PanelUi& ui, const Fleet& f, int x, int y, uint32_t ms, DeckScope sc, uint32_t holdMs = 50) {
  P.touch(ui, f, true, x, y, sc, ms);
  DeckCommand c = P.touch(ui, f, true, x, y, sc, ms + holdMs);
  DeckCommand up = P.touch(ui, f, false, 0, 0, sc, ms + holdMs + 10);
  return c.cmd != DeckCmd::None ? c : up;
}

static void drag(PanelUi& ui, const Fleet& f, int x0, int x1, int y, uint32_t ms, uint32_t dur) {
  P.touch(ui, f, true, x0, y, DeckScope::Act, ms);
  const int steps = 10;
  for (int i = 1; i <= steps; i++) P.touch(ui, f, true, x0 + (x1 - x0) * i / steps, y, DeckScope::Act, ms + dur * i / steps);
  P.touch(ui, f, false, 0, 0, DeckScope::Act, ms + dur + 5);
}

static void settle(PanelUi& ui, const Fleet& f, uint32_t& ms) {
  for (int i = 0; i < 40 && ui.animating; i++) {
    ms += 16;
    P.tick(ui, f, ms);
  }
}

static bool inside(const Rect& r) { return r.x >= 0 && r.y >= 0 && r.x + r.w <= strip::W && r.y + r.h <= strip::H; }

// Every screen in every state, scope and scroll, with guard bytes around the frame.
static void checkScreens() {
  static DeckFleet d;
  d = DeckFleet();
  assert(fixture::load(FIXTURE, d));
  const int PAD = 2 * strip::W;
  const uint16_t GUARD = 0xDEAD;
  std::vector<uint16_t> mem(strip::W * strip::H + 2 * PAD, GUARD);
  Canvas c{mem.data() + PAD, strip::W, strip::H};
  auto guards = [&] {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[PAD + strip::W * strip::H + i] != GUARD) return false;
    return true;
  };
  for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Live, LinkState::Lost})
    for (DeckScope sc : {DeckScope::View, DeckScope::Act, DeckScope::Admin})
      for (int bat : {-1, 0, 9, 100}) {
        PanelUi ui;
        PanelInput in{&d.fleet, ls, "waveshare-touch-a-very-long-device-name-0123456789", sc, 300,
                      d.fleet.serverTime + 754, &ui, 125};
        in.battery = bat;
        P.draw(c, in);
        assert(guards());
        in.fleet = nullptr;
        P.draw(c, in);
        assert(guards());
        in.fleet = &d.fleet;
        for (float s : {0.f, 77.f, 300.f, -60.f, 2000.f}) {   // the row anywhere, even past its ends
          ui.scroll = s;
          ui.dragging = true;
          P.draw(c, in);
          assert(guards());
        }
        ui.dragging = false;
        ui.scroll = 0;
        for (int i = 0; i < d.fleet.count; i++) {
          snprintf(ui.openId, sizeof ui.openId, "%s", d.fleet.agents[i].id);
          for (Screen s : {Screen::Card, Screen::Prompt}) {
            ui.screen = s;
            for (uint32_t ms : {0u, 450u, 1300u}) {
              in.ms = ms;
              P.draw(c, in);
              assert(guards());
            }
          }
        }
      }
}

// The Wi-Fi mark in the header: shown once an SSID is set, even while on USB.
static void checkWifiMark() {
  static DeckFleet d;
  d = DeckFleet();
  assert(fixture::load(FIXTURE, d));
  std::vector<uint16_t> a(strip::W * strip::H), b(strip::W * strip::H);
  PanelUi ui;
  NetView net;
  net.wifiCapable = true;
  PanelInput in{&d.fleet, LinkState::Live, "board", DeckScope::Act, 300, d.fleet.serverTime, &ui, 0, &net};
  Canvas ca{a.data(), strip::W, strip::H}, cb{b.data(), strip::W, strip::H};
  P.draw(ca, in);
  snprintf(net.ssid, sizeof net.ssid, "home");
  P.draw(cb, in);
  assert(a != b);
  net.wifiCapable = false;
  P.draw(cb, in);
  assert(a == b);
}

int main() {
  checkWifiMark();
  // ---- the row: geometry, scrolling, settling
  {
    assert(P.width() == 640 && P.height() == 172);
    Fleet f = fleetOf(12);
    assert(strip::maxScroll(f) == 7 * strip::kPitch);              // five on screen
    const Rect t0 = strip::tileRect(0, 0), t4 = strip::tileRect(4, 0);
    assert(t0.y == strip::kHeaderH && t0.x >= 0 && t4.x + t4.w <= strip::W && inside(t0) && inside(t4));
    Fleet few = fleetOf(5);
    assert(strip::maxScroll(few) == 0);

    PanelUi ui;
    uint32_t ms = 1000;
    // A slow drag left by 180 px: the row follows, then rests on tile 1.
    drag(ui, f, 500, 320, 100, ms, 800);
    assert(ui.animating && !ui.dragging);
    assert(ui.scrollTo == strip::kPitch);
    settle(ui, f, ms);
    assert(ui.scroll == strip::kPitch && !ui.animating);
    // A flick carries further, never past the end.
    drag(ui, f, 600, 100, 100, ms, 120);
    assert(ui.scrollTo > 2 * strip::kPitch && ui.scrollTo <= strip::maxScroll(f));
    settle(ui, f, ms);
    // Dragging the other way past the start gives a little, then rests at 0.
    drag(ui, f, 100, 639, 100, ms, 900);
    settle(ui, f, ms);
    drag(ui, f, 100, 639, 100, ms, 900);
    settle(ui, f, ms);
    assert(ui.scroll == 0);
    // A vertical drag on home doesn't scroll.
    drag(ui, f, 300, 300, 40, ms, 300);
    assert(ui.scroll == 0 && !ui.animating);
    // The row shrinks under the scroll: clamped on the next tick.
    ui.scroll = strip::maxScroll(f);
    P.tick(ui, few, ms);
    assert(ui.scroll == 0);
    // BOOT pages by five tiles, then back to the first.
    ui = PanelUi();
    P.button(ui, f);
    assert(ui.scroll == 5 * strip::kPitch);
    P.button(ui, f);
    assert(ui.scroll == strip::maxScroll(f));
    P.button(ui, f);
    assert(ui.scroll == 0);
  }

  // ---- taps: a tile opens its card; a waiting agent's opens the prompt
  {
    Fleet f = fleetOf(6);
    f.agents[2] = asking("w2", 3, false);
    f.agents[2].slot = 2;
    PanelUi ui;
    const Rect t1 = strip::tileRect(1, 0);
    assert(touchAt(ui, f, t1.x + 10, t1.y + 10, 100, DeckScope::Act).cmd == DeckCmd::None);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a1"));
    // Swipe right: back.
    drag(ui, f, 100, 300, 100, 500, 200);
    assert(ui.screen == Screen::Home);
    const Rect t2 = strip::tileRect(2, 0);
    touchAt(ui, f, t2.x + 60, t2.y + 60, 1000, DeckScope::Act);
    assert(ui.screen == Screen::Prompt && !strcmp(ui.openId, "w2"));
    P.button(ui, f);                                             // BOOT: back
    assert(ui.screen == Screen::Home);
    // The header is no tile.
    touchAt(ui, f, t1.x + 10, 5, 2000, DeckScope::Act);
    assert(ui.screen == Screen::Home);
  }

  // ---- the card's buttons
  {
    Fleet f = fleetOf(2, Status::Running);
    PanelUi ui;
    snprintf(ui.openId, sizeof ui.openId, "a0");
    ui.screen = Screen::Card;
    const CardButtons view = strip::cardButtons(f.agents[0], DeckScope::View);
    assert(view.n == 0);
    const CardButtons act = strip::cardButtons(f.agents[0], DeckScope::Act);
    assert(act.n == 2 && act.kind[0] == IconKind::Focus && act.kind[1] == IconKind::Interrupt);
    const CardButtons adm = strip::cardButtons(f.agents[0], DeckScope::Admin);
    assert(adm.n == 3 && adm.kind[2] == IconKind::Stop);
    // Every button, with the stop's hold ring, inside the screen.
    for (int i = 0; i < adm.n; i++) {
      const int r = strip::kBtnR + strip::kBtnRing;
      assert(adm.cx[i] - r >= 0 && adm.cx[i] + r <= strip::W && adm.cy - r >= 0 && adm.cy + r <= strip::H);
    }
    DeckCommand c = touchAt(ui, f, act.cx[0], act.cy, 100, DeckScope::Act);
    assert(c.cmd == DeckCmd::Focus && !strcmp(c.agent, "a0"));
    c = touchAt(ui, f, act.cx[1], act.cy, 500, DeckScope::Act);
    assert(c.cmd == DeckCmd::Interrupt);
    // Stop: a tap does nothing, a 1 s hold sends Dismiss; act scope can't.
    c = touchAt(ui, f, adm.cx[2], adm.cy, 1000, DeckScope::Admin);
    assert(c.cmd == DeckCmd::None);
    c = touchAt(ui, f, adm.cx[2], adm.cy, 3000, DeckScope::Admin, kStopHoldMs + 10);
    assert(c.cmd == DeckCmd::Dismiss && !strcmp(c.agent, "a0"));
    // With act scope the buttons sit right-aligned without stop: no hold there dismisses.
    c = touchAt(ui, f, adm.cx[2], adm.cy, 6000, DeckScope::Act, kStopHoldMs + 10);
    assert(c.cmd != DeckCmd::Dismiss);
  }

  // ---- the prompt: every option reachable, on screen, apart; Answer carries the prompt
  for (bool perm : {false, true})
    for (int n = 0; n <= kMaxOptions; n++) {
      Fleet f = fleetOf(1);
      f.agents[0] = asking("q0", n, perm);
      const strip::StripPrompt g = strip::promptGeom(f.agents[0]);
      assert(g.n == n && g.cols == (n > 3 ? 2 : 1));
      for (int i = 0; i < n; i++) {
        assert(inside(g.opt[i]) && g.opt[i].x >= strip::kOptX && g.opt[i].h >= 40);
        for (int k = 0; k < i; k++) assert(!g.opt[i].intersects(g.opt[k]));
      }
      if (perm) {
        assert(g.box.h && g.boxLines == 2 && inside(g.box) && g.box.x + g.box.w <= strip::kOptX);
        assert(g.questionY + g.questionLines * strip::questionLineH() <= g.box.y);
      } else {
        assert(!g.box.h && g.questionLines >= 2);
      }
      for (int i = 0; i < n; i++) {
        PanelUi ui;
        snprintf(ui.openId, sizeof ui.openId, "q0");
        ui.screen = Screen::Prompt;
        const Rect& r = g.opt[i];
        const DeckCommand c = touchAt(ui, f, r.x + r.w / 2, r.y + r.h / 2, 100, DeckScope::Act);
        assert(c.cmd == DeckCmd::Answer && !strcmp(c.prompt, "p-q0") && !strcmp(c.key, f.agents[0].opts[i].key));
        assert(ui.backAfterToast);
        P.tick(ui, f, 100 + 60 + 2000);                         // "sent" ends: home
        assert(ui.screen == Screen::Home);
        // View scope only says so.
        ui = PanelUi();
        snprintf(ui.openId, sizeof ui.openId, "q0");
        ui.screen = Screen::Prompt;
        assert(touchAt(ui, f, r.x + r.w / 2, r.y + r.h / 2, 100, DeckScope::View).cmd == DeckCmd::None);
        assert(!strcmp(ui.toast, "view only: needs scope act"));
      }
    }

  // ---- the prompt goes when it's answered elsewhere; the card turns into the prompt when it asks
  {
    Fleet f = fleetOf(1);
    f.agents[0] = asking("q0", 2, false);
    PanelUi ui;
    snprintf(ui.openId, sizeof ui.openId, "q0");
    ui.screen = Screen::Prompt;
    f.agents[0].status = Status::Running;
    P.tick(ui, f, 10);
    assert(ui.screen == Screen::Card);
    f.agents[0].status = Status::Waiting;
    P.tick(ui, f, 20);
    assert(ui.screen == Screen::Prompt);
    f.count = 0;
    P.tick(ui, f, 30);
    assert(ui.screen == Screen::Home);
  }

  checkScreens();
  printf("strip: ok\n");
  return 0;
}
