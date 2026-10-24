// layout_big.h: the Flutter desk panel at 2/3 for the 4" board. Geometry,
// hit-testing, the card's buttons (stop arms on the first tap), the shared
// prompt layout (choose, then send), and every screen inside the frame.
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_big.h"
#include "layout_square.h"

static Agent mk(const char* id, uint8_t slot, Status st) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.status = st;
  return a;
}

static Fleet fleetOf(std::initializer_list<Agent> as) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (const Agent& a : as) f.agents[f.count++] = a;
  f.total = f.count;
  return f;
}

static DeckCommand tap(const BigSpec& S, PanelUi& ui, const Fleet& f, const Rect& r, DeckScope sc, uint32_t ms) {
  const int x = r.x + r.w / 2, y = r.y + r.h / 2;
  bigTouch(ui, S, f, true, x, y, sc, ms);
  bigTouch(ui, S, f, true, x, y, sc, ms + 40);
  return bigTouch(ui, S, f, false, 0, 0, sc, ms + 60);
}

int main() {
  const BigSpec S = specBig480();

  // ---- the scale: the desk panel's numbers times 2/3
  assert(bk(S, 720) == 480 && bk(S, 8) == 5 && bk(S, 68) == 45 && bk(S, 36) == 24);
  assert(fontPx(*S.title) == 20 && fontPx(*S.heading) == 15 && fontPx(*S.body) == 13);
  assert(18 * S.cardScale == bk(S, 54));                       // the card's critter: 54 px -> 36 px exactly

  // ---- the grid: DeckGrid's padding and gap at 2/3, square cells, bars with more pages
  {
    const BigGrid one = bigGrid(S, 1), two = bigGrid(S, 2);
    assert(one.gap == 5 && one.cell == 113 && one.cellH == 113 && !one.bars.w);
    assert(two.cell == 113 && two.cellH < 113 && two.bars.w == S.w && two.bars.h == bk(S, 34));
    const Rect last = bigCell(one, 15);
    assert(last.x + last.w <= S.w - 5 && last.y + last.h <= S.h - 5);
    for (int cell = 0; cell < 16; cell++) {
      const Rect r = bigCell(two, cell);
      assert(r.y + r.h <= two.bars.y);
    }
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("w", 5, Status::Waiting), mk("z", 20, Status::Running)});
    assert(bigPages(f) == 2);
    PanelUi ui;
    const BigGrid g = bigGrid(S, 2);
    PanelHit h = bigHit(S, ui, f, bigCell(g, 1).x + 10, bigCell(g, 1).y + 10, DeckScope::Act);
    assert(h.kind == Hit::None);                                // a free cell
    h = bigHit(S, ui, f, bigCell(g, 5).x + 10, bigCell(g, 5).y + 10, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 1);
    // A page bar, a swipe, BOOT.
    tap(S, ui, f, Rect{S.w - 20, g.bars.y, 10, g.bars.h}, DeckScope::Act, 0);
    assert(ui.page == 1);
    assert(bigHit(S, ui, f, bigCell(g, 4).x + 5, bigCell(g, 4).y + 5, DeckScope::Act).agent == 2);
    bigTouch(ui, S, f, true, 300, 200, DeckScope::Act, 100);
    bigTouch(ui, S, f, true, 400, 205, DeckScope::Act, 200);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 210);
    assert(ui.page == 0);
    bigButton(ui, f);
    assert(ui.page == 1);
    bigButton(ui, f);
    assert(ui.page == 0);
    // Tap a waiting tile: its prompt, with the primary option chosen.
    f.agents[1].optCount = 2;
    snprintf(f.agents[1].opts[0].key, sizeof f.agents[1].opts[0].key, "1");
    snprintf(f.agents[1].opts[1].key, sizeof f.agents[1].opts[1].key, "2");
    f.agents[1].opts[1].primary = true;
    tap(S, ui, f, bigCell(bigGrid(S, 2), 5), DeckScope::Act, 1000);
    assert(ui.screen == Screen::Prompt && !strcmp(ui.openId, "w") && ui.selected == 1);
    // Swipe right: back.
    bigTouch(ui, S, f, true, 100, 200, DeckScope::Act, 2000);
    bigTouch(ui, S, f, true, 250, 210, DeckScope::Act, 2100);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 2110);
    assert(ui.screen == Screen::Home);
  }

  // ---- the card's buttons (AgentButton row) by status and scope
  {
    Agent run = mk("r", 0, Status::Running), idle = mk("i", 1, Status::Idle), ex = mk("x", 2, Status::Exited);
    BigButtons b = bigCardButtons(S, run, DeckScope::View);
    assert(b.n == 0);
    b = bigCardButtons(S, run, DeckScope::Act);
    assert(b.n == 2 && b.kind[0] == Hit::Focus && b.kind[1] == Hit::Interrupt);
    assert(b.r[1].w == bk(S, 200) && b.r[0].w == bigCardInner(S).w - bk(S, 14) - bk(S, 200) && b.r[0].h == bk(S, 68));
    assert(b.r[0].y + b.r[0].h == S.h - bk(S, 30));
    b = bigCardButtons(S, idle, DeckScope::Act);
    assert(b.n == 1 && b.kind[0] == Hit::Focus && b.r[0].w == bigCardInner(S).w);   // stop needs admin
    b = bigCardButtons(S, idle, DeckScope::Admin);
    assert(b.n == 2 && b.kind[1] == Hit::Stop);
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 1 && b.kind[0] == Hit::Stop);                                       // remove
    ex.resumable = true;
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 2 && b.kind[0] == Hit::Resume && b.kind[1] == Hit::Stop);

    // Stop: the first tap arms it ("tap again to stop"), the second sends Dismiss;
    // armed, it lapses after 3 s.
    Fleet f = fleetOf({idle});
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Card);
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    DeckCommand c = tap(S, ui, f, b.r[1], DeckScope::Admin, 0);
    assert(c.cmd == DeckCmd::None && ui.stopArmedUntil == 60 + kStopArmMs);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 1000);
    assert(c.cmd == DeckCmd::Dismiss && !strcmp(c.agent, "i") && ui.stopArmedUntil == 0);
    tap(S, ui, f, b.r[1], DeckScope::Admin, 5000);
    bigTick(ui, f, 5000 + 60 + kStopArmMs + 1);
    assert(ui.stopArmedUntil == 0);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 9000);
    assert(c.cmd == DeckCmd::None);                                                  // armed again, not sent
    c = tap(S, ui, f, b.r[0], DeckScope::Admin, 9500);
    assert(c.cmd == DeckCmd::Focus);
    // Interrupt while running.
    f.agents[0].status = Status::Running;
    b = bigCardButtons(S, f.agents[0], DeckScope::Act);
    c = tap(S, ui, f, b.r[1], DeckScope::Act, 10000);
    assert(c.cmd == DeckCmd::Interrupt);
    // Resume an exited agent.
    f.agents[0].status = Status::Exited;
    f.agents[0].resumable = true;
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    c = tap(S, ui, f, b.r[0], DeckScope::Admin, 11000);
    assert(c.cmd == DeckCmd::Resume);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 12000);                              // remove: one tap
    assert(c.cmd == DeckCmd::Dismiss);
  }

  // ---- the prompt: one layout; choose an option, then send
  {
    Fleet f = fleetOf({mk("w", 0, Status::Waiting)});
    Agent& w = f.agents[0];
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    snprintf(w.waitTitle, sizeof w.waitTitle, "which session strategy should the login flow use?");
    snprintf(w.promptId, sizeof w.promptId, "p9");
    w.optCount = 3;
    for (int i = 0; i < 3; i++) {
      snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
      snprintf(w.opts[i].label, sizeof w.opts[i].label, "option %d - hint %d", i + 1, i + 1);
    }
    for (const char* kind : {"question", "permission"}) {
      snprintf(w.waitKind, sizeof w.waitKind, "%s", kind);
      snprintf(w.waitDetail, sizeof w.waitDetail, "%s", !strcmp(kind, "permission") ? "terraform apply" : "");
      const BigPrompt p = bigPrompt(S, w);
      assert(p.n == 3 && p.opt[0].h == bk(S, 74) && p.subs);
      assert(p.opt[2].y + p.opt[2].h <= p.send.y - bk(S, 10));
      assert(p.titleY >= bigTopBarBottom(S) && (p.box.h == 0) == (w.waitDetail[0] == 0));
      if (p.box.h) assert(p.box.y + p.box.h <= p.opt[0].y);
      assert(p.send.y == S.h - bk(S, 30) - bk(S, 68) && p.terminal.w == bk(S, 260));
    }
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Prompt && ui.selected == -1);    // no primary: nothing chosen
    BigPrompt p = bigPrompt(S, w);
    DeckCommand c = tap(S, ui, f, p.send, DeckScope::Act, 0);    // send with nothing chosen: nothing
    assert(c.cmd == DeckCmd::None);
    c = tap(S, ui, f, p.opt[2], DeckScope::Act, 100);
    assert(c.cmd == DeckCmd::None && ui.selected == 2);
    c = tap(S, ui, f, p.send, DeckScope::View, 200);             // view scope: a note, nothing sent
    assert(c.cmd == DeckCmd::None && strstr(ui.toast, "view only"));
    c = tap(S, ui, f, p.send, DeckScope::Act, 300);
    assert(c.cmd == DeckCmd::Answer && !strcmp(c.prompt, "p9") && !strcmp(c.key, "3"));
    assert(ui.backAfterToast);
    assert(tap(S, ui, f, p.send, DeckScope::Act, 400).cmd == DeckCmd::None);   // once
    bigTick(ui, f, 2000);
    assert(ui.screen == Screen::Home);
    // A refused answer keeps the prompt; "focus terminal" focuses.
    bigOpen(ui, f, 0);
    tap(S, ui, f, p.opt[0], DeckScope::Act, 3000);
    tap(S, ui, f, p.send, DeckScope::Act, 3100);
    ui.cmdSeq = 5;
    bigResult(ui, 5, 409, 3200);
    assert(!ui.backAfterToast);
    bigTick(ui, f, 9000);
    assert(ui.screen == Screen::Prompt);
    c = tap(S, ui, f, p.terminal, DeckScope::Act, 9100);
    assert(c.cmd == DeckCmd::Focus);
    // Six options with hints still fit above the buttons (shorter, without hints).
    w.optCount = 6;
    for (int i = 3; i < 6; i++) snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
    p = bigPrompt(S, w);
    assert(p.n == 6 && p.opt[0].y > p.titleY && p.opt[5].y + p.opt[5].h <= p.send.y);
  }

  // ---- every screen at every scope and link state stays inside the frame
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    const BigPanel P(S);
    const int PAD = 2 * S.w;
    const uint16_t GUARD = 0xDEAD;
    std::vector<uint16_t> mem(S.w * S.h + 2 * PAD, GUARD);
    Canvas c{mem.data() + PAD, S.w, S.h};
    auto guards = [&] {
      for (int i = 0; i < PAD; i++)
        if (mem[i] != GUARD || mem[PAD + S.w * S.h + i] != GUARD) return false;
      return true;
    };
    for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Live, LinkState::Lost})
      for (DeckScope sc : {DeckScope::View, DeckScope::Act, DeckScope::Admin}) {
        PanelUi ui;
        PanelInput in{&d.fleet, ls, "waveshare-touch-a-very-long-device-name-0123456789", sc, 300, d.fleet.serverTime + 60,
                      &ui, 125};
        P.draw(c, in);
        assert(guards());
        in.fleet = nullptr;
        P.draw(c, in);
        assert(guards());
        in.fleet = &d.fleet;
        for (int i = 0; i < d.fleet.count; i++) {
          bigOpen(ui, d.fleet, i);
          for (Screen s : {Screen::Card, Screen::Prompt}) {
            ui.screen = s;
            ui.stopArmedUntil = 1000;
            for (uint32_t ms : {0u, 450u}) {
              in.ms = ms;
              P.draw(c, in);
              assert(guards());
            }
          }
        }
      }
  }
  // ---- the 4"'s card is the big panel's (the desk panel's detail, its "focus terminal" button), not
  // the small boards' card that a 480 x 480 SquareSpec would draw
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> big(S.w * S.h), small(S.w * S.h);
    Canvas cb{big.data(), S.w, S.h}, cs{small.data(), S.w, S.h};
    PanelUi ui;
    const int i = findAgent(d.fleet, "k3f9a2");
    assert(i >= 0);
    bigOpen(ui, d.fleet, i);
    PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0};
    const BigPanel B(S);
    B.draw(cb, in);
    SquarePanel(specLcd4()).draw(cs, in);
    const BigButtons b = bigCardButtons(S, d.fleet.agents[i], DeckScope::Act);
    assert(b.n == 2 && b.kind[0] == Hit::Focus);
    const Theme T = deckTheme(false);
    // The focus button's face, beside its label, is filled; the small card has its round buttons elsewhere.
    const uint16_t face = big[(b.r[0].y + 6) * S.w + b.r[0].x + 12];
    assert(face != T.bg && face == big[(b.r[0].y + b.r[0].h - 8) * S.w + b.r[0].x + b.r[0].w - 14]);
    assert(memcmp(big.data(), small.data(), big.size() * 2) != 0);
    assert(B.width() == 480 && B.height() == 480);
  }

  // ---- on Wi-Fi: the bars' row stays with one page (the mark's), hits follow the drawing, every notice fits
  {
    const BigGrid w = bigGrid(S, 1, true);
    assert(w.bars.w == S.w && w.bars.h == bk(S, 34) && w.cell == w.cellH);
    for (int cell = 0; cell < 16; cell++) assert(bigCell(w, cell).y + bigCell(w, cell).h <= w.bars.y);
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("b", 15, Status::Running)});
    PanelUi ui;
    ui.wifi = true;
    PanelHit h = bigHit(S, ui, f, bigCell(w, 15).x + 3, bigCell(w, 15).y + 3, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 1);
    h = bigHit(S, ui, f, S.w / 2, w.bars.y + w.bars.h / 2, DeckScope::Act);
    assert(h.kind == Hit::None);                                 // no page bars with one page
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> buf(S.w * S.h);
    Canvas c{buf.data(), S.w, S.h};
    NetView net;
    net.via = NetVia::Wifi;
    snprintf(net.ssid, sizeof net.ssid, "%s", "a-network-name-of-32-characters!");
    PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0, &net};
    BigPanel(S).draw(c, in);
    uiToast(ui, "paired for Wi-Fi", 0);
    in.link = LinkState::Connecting;
    for (NetPhase ph : {NetPhase::Joining, NetPhase::Connecting, NetPhase::WrongPassword, NetPhase::NoNetwork,
                        NetPhase::WifiFailed, NetPhase::DeckUnreachable, NetPhase::CertRejected}) {
      net.phase = ph;
      BigPanel(S).draw(c, in);
    }
  }
  printf("ok  big layout\n");
  return 0;
}
