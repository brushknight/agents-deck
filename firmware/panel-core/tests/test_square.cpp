// layout_square.h: the square boards' home (grid and scrolling list), card,
// prompt and notices; touch, scrolling, holds and hit-testing.
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
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

// A finger: down at (x, y), moved through the points, lifted, with times.
static DeckCommand touchAt(const SquareSpec& S, PanelUi& ui, const Fleet& f, int x, int y, uint32_t ms, DeckScope sc,
                           uint32_t holdMs = 50) {
  squareTouch(ui, S, f, true, x, y, sc, ms);
  DeckCommand c = squareTouch(ui, S, f, true, x, y, sc, ms + holdMs);
  DeckCommand up = squareTouch(ui, S, f, false, 0, 0, sc, ms + holdMs + 10);
  return c.cmd != DeckCmd::None ? c : up;
}

static void drag(const SquareSpec& S, PanelUi& ui, const Fleet& f, int x, int y0, int y1, uint32_t ms, uint32_t dur) {
  squareTouch(ui, S, f, true, x, y0, DeckScope::Act, ms);
  const int steps = 10;
  for (int i = 1; i <= steps; i++)
    squareTouch(ui, S, f, true, x, y0 + (y1 - y0) * i / steps, DeckScope::Act, ms + dur * i / steps);
  squareTouch(ui, S, f, false, 0, 0, DeckScope::Act, ms + dur + 5);
}

static void settle(const SquareSpec& S, PanelUi& ui, const Fleet& f, uint32_t& ms) {
  for (int i = 0; i < 40 && ui.animating; i++) {
    ms += 16;
    squareTick(ui, S, f, ms);
  }
}

static void checkScreens(const SquareSpec& S) {
  static DeckFleet d;
  d = DeckFleet();
  assert(fixture::load(FIXTURE, d));
  const SquarePanel P(S);
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
      PanelInput in{&d.fleet, ls, "waveshare-touch-a-very-long-device-name-0123456789", sc, 300,
                    d.fleet.serverTime + 754, &ui, 125};
      P.draw(c, in);
      assert(guards());
      in.fleet = nullptr;
      P.draw(c, in);
      assert(guards());
      in.fleet = &d.fleet;
      for (float sc2 : {0.f, 77.f, 200.f, -40.f, 900.f}) {   // the list anywhere, even past its ends
        ui.scroll = sc2;
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

int main() {
  const SquareSpec A = specAmoled18(), L = specLcd4();

  // ---- the spec's numbers
  assert(A.w == 368 && A.h == 448 && A.cornerR == 44 && A.m == 16 && A.tileW == 172 && A.tileH == 192);
  assert(A.btnR == 36 && A.btnGap == 26 && A.btnFromBottom == 54 && A.optH == 76 && L.optH == 72);
  assert(A.ctxPitch == 8 && L.ctxPitch == 6 && A.posX == 4 && A.posW == 6 && A.posTop == 90 && A.posBottom == 48);
  // Minimum text: 20 px on the AMOLED (cell 12), 16 px on the 4" (cell 10), card and prompts.
  for (const Font* f : {A.label, A.body, A.question, A.value, A.title, A.tileTitle, A.tileMeta}) assert(f->cellW >= 12);
  for (const Font* f : {L.label, L.body, L.question, L.value, L.title}) assert(f->cellW >= 10);

  // ---- the list: geometry, scrolling, settling, the position bar
  {
    Fleet f = fleetOf(9);                                       // 5 rows of 2
    assert(listRows(A, f) == 5 && rowPitch(A) == 200);
    assert(maxScroll(A, f) == 3 * 200);                         // two rows show at once
    const Rect t0 = listTile(A, 0, 0), t1 = listTile(A, 1, 0), t2 = listTile(A, 2, 0);
    assert(t0.y == A.headerH && t1.y == t0.y && t1.x == t0.x + 172 + 8 && t2.y == t0.y + 200);
    assert(t0.x == 8 && t1.x + t1.w == 360);
    Fleet few = fleetOf(3);
    assert(maxScroll(A, few) == 0);

    PanelUi ui;
    uint32_t ms = 1000;
    // A slow drag up by 230 px: the list follows the finger, then settles on row 1.
    drag(A, ui, f, 100, 400, 170, ms, 600);
    assert(ui.animating && !ui.dragging);
    assert(ui.scrollTo == 200);
    ms += 610;
    settle(A, ui, f, ms);
    assert(!ui.animating && ui.scroll == 200);
    // The position bar: on while it settled, gone 1.3 s later.
    assert(posBarAlpha(ui, ms) == 15 && posBarAlpha(ui, ms + 999) == 15);
    assert(posBarAlpha(ui, ms + 1150) > 0 && posBarAlpha(ui, ms + 1150) < 15 && posBarAlpha(ui, ms + 1400) == 0);
    // A fast flick carries further (momentum), and never past the last row.
    drag(A, ui, f, 100, 400, 300, ms + 2000, 60);
    assert(ui.scrollTo >= 400 && ui.scrollTo <= maxScroll(A, f));
    ms += 2100;
    settle(A, ui, f, ms);
    assert((int)ui.scroll % 200 == 0);
    // Dragged past the top: back to 0.
    drag(A, ui, f, 100, 100, 440, ms + 100, 400);
    ms += 600;
    settle(A, ui, f, ms);
    assert(ui.scroll == 0);

    // Hit-testing while scrolled: the tile under the finger, not the header.
    ui.scroll = 200;
    PanelHit h = squareHit(A, ui, f, 100, A.headerH + 20, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 2);                // row 1, left
    h = squareHit(A, ui, f, 300, A.headerH + 220, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 5);                // row 2, right
    h = squareHit(A, ui, f, 100, 20, DeckScope::Act);           // the header covers the row scrolled under it
    assert(h.kind == Hit::None);
    ui.scroll = 150;                                            // mid-scroll: a partly hidden row
    h = squareHit(A, ui, f, 100, A.headerH + 10, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 0);
    // A tap opens the card; a tile catching a moving list doesn't open anything.
    ui.scroll = 200;
    touchAt(A, ui, f, 100, A.headerH + 20, ms + 5000, DeckScope::Act);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a2"));
    squareHome(ui);
    ui.animating = true;
    ui.scrollFrom = 0;
    ui.scrollTo = 200;
    ui.animStart = ms + 6000;
    touchAt(A, ui, f, 100, A.headerH + 20, ms + 6050, DeckScope::Act);
    assert(ui.screen == Screen::Home);
    // A horizontal swipe on the list does nothing (no paging).
    squareSwipe(ui, A, f, Swipe::Left);
    assert(ui.screen == Screen::Home && ui.page == 0);
  }

  // ---- the grid: pages by slot, page bars, swipes and BOOT
  {
    Fleet f = fleetOf(2);
    f.agents[1].slot = 17;
    assert(pageCount(L, f) == 2 && pageCount(A, f) == 1);
    PanelUi ui;
    const GridGeom g = gridGeom(L, 2);
    assert(g.cellW == 110 && g.cellH < 110 && g.bars.w == L.w);
    PanelHit h = squareHit(L, ui, f, L.w - 10, g.bars.y + g.bars.h / 2, DeckScope::Act);
    assert(h.kind == Hit::PageBar && h.index == 1);
    touchAt(L, ui, f, L.w - 10, g.bars.y + g.bars.h / 2, 0, DeckScope::Act);
    assert(ui.page == 1);
    const Rect r1 = cellRect(L, g, 1);
    touchAt(L, ui, f, r1.x + r1.w / 2, r1.y + r1.h / 2, 100, DeckScope::Act);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a1"));
    squareButton(ui, L, f);                                      // BOOT: back
    assert(ui.screen == Screen::Home);
    squareSwipe(ui, L, f, Swipe::Right);
    assert(ui.page == 0);
    squareButton(ui, L, f);                                      // BOOT on the grid: next page
    assert(ui.page == 1);
    squareButton(ui, L, f);
    assert(ui.page == 0);
  }

  // ---- a tile's icons never meet its name
  for (const SquareSpec* S : {&A, &L}) {
    const bool list = S->home == HomeKind::List;
    Rect r = list ? listTile(*S, 0, 0) : cellRect(*S, gridGeom(*S, 2), 0);
    for (int pass = 0; pass < 2; pass++) {
      const square_detail::TileParts p = square_detail::tileParts(*S, r, list);
      assert(!p.chip.intersects(p.name) && !p.sub.intersects(p.name) && !p.count.intersects(p.name));
      assert(!p.chip.intersects(p.meta) && !p.sub.intersects(p.meta));
      assert(p.sub.y + p.sub.h <= p.critter.y + p.scale * 2);   // the sub critter sits above the critter's body
      assert(p.name.y >= p.critter.y + p.critter.h && p.meta.y + p.meta.h <= r.y + r.h);
      if (list) assert(p.bar.y >= p.meta.y + p.meta.h);
      r = list ? r : cellRect(*S, gridGeom(*S, 1), 0);           // the 4": square cells too
    }
  }

  // ---- the card: buttons by scope, focus, interrupt, stop held for 1 s
  {
    Fleet f = fleetOf(1, Status::Running);
    PanelUi ui;
    squareOpen(ui, f, 0);
    assert(ui.screen == Screen::Card);
    assert(cardButtons(A, f.agents[0], DeckScope::View).n == 0);
    CardButtons b = cardButtons(A, f.agents[0], DeckScope::Act);
    assert(b.n == 2 && b.kind[0] == IconKind::Focus && b.kind[1] == IconKind::Interrupt);
    b = cardButtons(A, f.agents[0], DeckScope::Admin);
    assert(b.n == 3 && b.kind[2] == IconKind::Stop && b.cy == A.h - 54);
    assert(b.cx[1] == A.w / 2 && b.cx[2] - b.cx[1] == 2 * 36 + 26);
    f.agents[0].status = Status::Idle;
    b = cardButtons(A, f.agents[0], DeckScope::Admin);
    assert(b.n == 2 && b.kind[1] == IconKind::Stop);                   // no interrupt when idle
    f.agents[0].status = Status::Running;
    const CardButtons ba = cardButtons(L, f.agents[0], DeckScope::Act);
    b = cardButtons(L, f.agents[0], DeckScope::Admin);

    DeckCommand c = touchAt(L, ui, f, ba.cx[0], ba.cy, 0, DeckScope::Act);
    assert(c.cmd == DeckCmd::Focus && !strcmp(c.agent, "a0"));
    c = touchAt(L, ui, f, ba.cx[1], ba.cy, 100, DeckScope::Act);
    assert(c.cmd == DeckCmd::Interrupt);
    // Stop with scope act: not there (a tap where it would be does nothing).
    c = touchAt(L, ui, f, b.cx[2], b.cy, 200, DeckScope::Act);
    assert(c.cmd == DeckCmd::None);
    // Admin: a short tap says to hold; holding 1 s sends Dismiss, once.
    c = touchAt(L, ui, f, b.cx[2], b.cy, 300, DeckScope::Admin, 300);
    assert(c.cmd == DeckCmd::None && !strcmp(ui.toast, "hold to stop"));
    squareTouch(ui, L, f, true, b.cx[2], b.cy, DeckScope::Admin, 1000);
    assert(holdPermille(ui, 1500) == 500);
    assert(squareTouch(ui, L, f, true, b.cx[2], b.cy, DeckScope::Admin, 1500).cmd == DeckCmd::None);
    c = squareTouch(ui, L, f, true, b.cx[2], b.cy, DeckScope::Admin, 2001);
    assert(c.cmd == DeckCmd::Dismiss && !strcmp(c.agent, "a0"));
    assert(squareTouch(ui, L, f, true, b.cx[2], b.cy, DeckScope::Admin, 2500).cmd == DeckCmd::None);
    squareTouch(ui, L, f, false, 0, 0, DeckScope::Admin, 2600);
    // Sliding off the button cancels the hold.
    squareTouch(ui, L, f, true, b.cx[2], b.cy, DeckScope::Admin, 3000);
    squareTouch(ui, L, f, true, b.cx[2] - 60, b.cy, DeckScope::Admin, 3200);
    assert(squareTouch(ui, L, f, true, b.cx[2] - 60, b.cy, DeckScope::Admin, 4200).cmd == DeckCmd::None);
    squareTouch(ui, L, f, false, 0, 0, DeckScope::Admin, 4300);
    // Swipe right: back home.
    squareTouch(ui, L, f, true, 100, 200, DeckScope::Admin, 5000);
    squareTouch(ui, L, f, true, 260, 210, DeckScope::Admin, 5100);
    squareTouch(ui, L, f, false, 0, 0, DeckScope::Admin, 5110);
    assert(ui.screen == Screen::Home);
  }

  // ---- the prompt: one layout for permissions and questions; answers
  {
    Fleet f = fleetOf(1, Status::Waiting);
    Agent& w = f.agents[0];
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    snprintf(w.waitTitle, sizeof w.waitTitle, "which session strategy should the login flow use?");
    snprintf(w.promptId, sizeof w.promptId, "p17");
    w.optCount = 3;
    const char* labels[] = {"rotating tokens \xE2\x80\x94 short access", "sliding sessions - server-side",
                            "long-lived jwt"};
    for (int i = 0; i < 3; i++) {
      snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
      snprintf(w.opts[i].label, sizeof w.opts[i].label, "%s", labels[i]);
    }
    char t[96], h[96];
    splitOption(w.opts[0].label, t, sizeof t, h, sizeof h);
    assert(!strcmp(t, "rotating tokens") && !strcmp(h, "short access"));
    splitOption(w.opts[1].label, t, sizeof t, h, sizeof h);
    assert(!strcmp(t, "sliding sessions") && !strcmp(h, "server-side"));
    splitOption(w.opts[2].label, t, sizeof t, h, sizeof h);
    assert(!strcmp(t, "long-lived jwt") && !h[0]);

    for (const SquareSpec* S : {&A, &L}) {
      const PromptGeom g = promptGeom(*S, w);
      assert(g.n == 3 && g.hints && g.opt[0].h == S->optH);
      assert(g.opt[2].y + g.opt[2].h == S->h - S->m);                  // pinned to the bottom
      assert(g.questionY + g.questionLines * (S->question->cellH - 4) <= g.opt[0].y);
      assert(!g.box.h);                                                // questions have no command box
    }
    PanelUi ui;
    squareOpen(ui, f, 0);
    assert(ui.screen == Screen::Prompt);                               // a waiting agent opens its prompt
    const PromptGeom g = promptGeom(A, w);
    const Rect o2 = g.opt[1];
    // Scope view: no answer, a note why.
    DeckCommand c = touchAt(A, ui, f, o2.x + 40, o2.y + o2.h / 2, 0, DeckScope::View);
    assert(c.cmd == DeckCmd::None && strstr(ui.toast, "view only"));
    c = touchAt(A, ui, f, o2.x + 40, o2.y + o2.h / 2, 100, DeckScope::Act);
    assert(c.cmd == DeckCmd::Answer && !strcmp(c.prompt, "p17") && !strcmp(c.key, "2") && !strcmp(c.agent, "a0"));
    assert(ui.backAfterToast && !strcmp(ui.toast, "sent"));
    // A second tap while "sent" shows sends nothing; then it goes home.
    assert(touchAt(A, ui, f, o2.x + 40, o2.y + o2.h / 2, 200, DeckScope::Act).cmd == DeckCmd::None);
    squareTick(ui, A, f, 2000);
    assert(ui.screen == Screen::Home);
    // A refused answer keeps the prompt up.
    squareOpen(ui, f, 0);
    c = touchAt(A, ui, f, o2.x + 40, o2.y + o2.h / 2, 3000, DeckScope::Act);
    ui.cmdSeq = 9;
    squareResult(ui, 9, 409, 3100);
    assert(!ui.backAfterToast && !strcmp(ui.toast, "the prompt changed"));
    squareTick(ui, A, f, 9000);
    assert(ui.screen == Screen::Prompt);
    // Answered elsewhere: the prompt turns into the card.
    w.status = Status::Running;
    squareTick(ui, A, f, 9100);
    assert(ui.screen == Screen::Card);

    // A permission: the command box, no hints, still the same option rows.
    w.status = Status::Waiting;
    snprintf(w.waitKind, sizeof w.waitKind, "permission");
    snprintf(w.waitTitle, sizeof w.waitTitle, "run this command?");
    snprintf(w.waitDetail, sizeof w.waitDetail, "terraform apply -target=module.staging_net");
    snprintf(w.promptCtx, sizeof w.promptCtx, "bash \xC2\xB7 ~/dev/infra");
    for (int i = 0; i < 3; i++) snprintf(w.opts[i].label, sizeof w.opts[i].label, "%s", i == 0 ? "yes" : i == 1 ? "always" : "no");
    for (const SquareSpec* S : {&A, &L}) {
      const PromptGeom p = promptGeom(*S, w);
      assert(p.box.h > 0 && p.boxLines >= 1 && p.box.y + p.box.h <= p.opt[0].y);
      assert(p.questionLines == 1 && p.box.y >= p.questionY + S->question->cellH - 4);
    }
    // Six options still fit the screen.
    w.optCount = 6;
    for (int i = 3; i < 6; i++) snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
    for (const SquareSpec* S : {&A, &L}) {
      const PromptGeom p = promptGeom(*S, w);
      assert(p.n == 6 && p.opt[0].y > p.questionY && p.opt[5].y + p.opt[5].h <= S->h);
    }
  }

  // ---- results and the vanished agent
  {
    Fleet f = fleetOf(2);
    PanelUi ui;
    squareOpen(ui, f, 1);
    f.count = 1;
    squareTick(ui, A, f, 0);
    assert(ui.screen == Screen::Home);
    ui.cmdSeq = 4;
    squareResult(ui, 3, 500, 0);
    assert(ui.cmdSeq == 4);
    squareResult(ui, 4, 403, 0);
    assert(!strcmp(ui.toast, "not allowed: scope") && ui.cmdSeq == 0);
  }

  // ---- every screen of both boards stays inside the frame; the AMOLED's corners are black
  checkScreens(A);
  checkScreens(L);
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> buf(A.w * A.h);
    Canvas c{buf.data(), A.w, A.h};
    PanelUi ui;
    PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0};
    for (Screen s : {Screen::Home, Screen::Card, Screen::Prompt}) {
      ui.screen = s;
      snprintf(ui.openId, sizeof ui.openId, "%s", s == Screen::Prompt ? "h0m3l4" : "k3f9a2");
      SquarePanel(A).draw(c, in);
      assert(c.px[0] == 0 && c.px[A.w - 1] == 0 && c.px[(A.h - 1) * A.w] == 0 && c.px[A.h * A.w - 1] == 0);
    }
  }
  // ---- on Wi-Fi: the grid keeps its bars' row (the mark's), hits follow the drawing; every notice and
  // toast stays inside the frame, the AMOLED's corners black
  {
    const GridGeom w = gridGeom(L, 1, true);
    assert(w.bars.w == L.w && w.bars.h == L.barsH && w.cellW == w.cellH);
    assert(cellRect(L, w, 15).y + cellRect(L, w, 15).h <= w.bars.y);
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    for (const SquareSpec* S : {&A, &L}) {
      std::vector<uint16_t> buf(S->w * S->h);
      Canvas c{buf.data(), S->w, S->h};
      PanelUi ui;
      ui.wifi = true;
      NetView net;
      net.via = NetVia::Wifi;
      snprintf(net.ssid, sizeof net.ssid, "%s", "a-network-name-of-32-characters!");
      PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0, &net};
      SquarePanel(*S).draw(c, in);
      uiToast(ui, "paired for Wi-Fi", 0);
      in.link = LinkState::Connecting;
      for (NetPhase ph : {NetPhase::Joining, NetPhase::Connecting, NetPhase::WrongPassword, NetPhase::NoNetwork,
                          NetPhase::WifiFailed, NetPhase::DeckUnreachable, NetPhase::CertRejected}) {
        net.phase = ph;
        SquarePanel(*S).draw(c, in);
        if (S == &A) assert(c.px[0] == 0 && c.px[A.w - 1] == 0 && c.px[(A.h - 1) * A.w] == 0 && c.px[A.h * A.w - 1] == 0);
      }
    }
  }
  printf("ok  square layouts\n");
  return 0;
}
