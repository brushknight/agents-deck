// layout_grid.h: geometry, paging, hit-testing, gestures, and that every
// screen of both grid layouts stays inside the frame.
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_grid.h"

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

static void center(const GridLayout& L, const GridGeom& g, int cell, int& x, int& y) {
  const Rect r = cellRect(L, g, cell);
  x = r.x + r.w / 2;
  y = r.y + r.h / 2;
}

static void checkScreens(const GridLayout& L) {
  static DeckFleet d;
  d = DeckFleet();
  assert(fixture::load(FIXTURE, d));
  const Theme T = deckTheme(L.trueBlack);
  const int PAD = 2 * L.w;
  const uint16_t GUARD = 0xDEAD;
  std::vector<uint16_t> mem(L.w * L.h + 2 * PAD, GUARD);
  Canvas c{mem.data() + PAD, L.w, L.h};
  auto guards = [&] {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[PAD + L.w * L.h + i] != GUARD) return false;
    return true;
  };
  PanelUi ui;
  for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Live, LinkState::Lost})
    for (int withFleet = 0; withFleet < 2; withFleet++)
      for (bool act : {false, true}) {
        PanelInput in{withFleet ? &d.fleet : nullptr, ls, "waveshare-touch-lcd-4-a-very-long-name-here", act, 300,
                      d.fleet.serverTime + 754, &ui};
        ui.openId[0] = '\0';
        for (int p = 0; p < pageCount(L, d.fleet); p++) {
          ui.page = p;
          drawPanel(c, L, T, in);
          assert(guards());
        }
        for (int i = 0; i < d.fleet.count; i++) {   // every agent's card
          snprintf(ui.openId, sizeof ui.openId, "%s", d.fleet.agents[i].id);
          for (uint32_t ms : {0u, 450u, 1300u}) {
            in.ms = ms;
            drawPanel(c, L, T, in);
            assert(guards());
          }
        }
      }
  // A waiting tile is accent-filled; an idle one is the panel colour.
  {
    PanelInput in{&d.fleet, LinkState::Live, "x", true, 0, d.fleet.serverTime, &ui};
    ui.openId[0] = '\0';
    ui.page = 0;
    drawPanel(c, L, T, in);
    const GridGeom g = gridGeom(L, pageCount(L, d.fleet));
    for (int cell = 0; cell < perPage(L); cell++) {
      const int i = agentAt(L, d.fleet, 0, cell);
      if (i < 0) continue;
      const Rect r = cellRect(L, g, cell);
      const uint16_t probe = c.px[(r.y + r.h - 12) * L.w + r.x + 12];   // inside the ring, below the text
      const Agent& a = d.fleet.agents[i];
      if (a.status == Status::Waiting) assert(probe == T.accent);
      if (a.status == Status::Running && !a.focused) assert(probe == T.panel);
    }
  }
}

int main() {
  // ---- geometry
  {
    const GridLayout L = layoutLcd4();
    const GridGeom one = gridGeom(L, 1), two = gridGeom(L, 2);
    assert(one.cellW == 110 && one.cellH == 110 && one.bars.w == 0);       // square with one page
    assert(two.cellW == 110 && two.cellH < 110 && two.bars.w == L.w);      // bars below with more
    for (int cell = 0; cell < 16; cell++) {
      const Rect r = cellRect(L, two, cell);
      assert(r.x >= 0 && r.y >= 0 && r.x + r.w <= L.w && r.y + r.h <= two.bars.y);
    }
    int x, y;
    center(L, one, 5, x, y);
    assert(cellAt(L, one, x, y) == 5);
    const Rect r = cellRect(L, one, 5);
    assert(cellAt(L, one, r.x - L.gap / 2, y) == -1);                        // the gap is no cell
    assert(cellAt(L, one, -5, -5) == -1);

    const GridLayout A = layoutAmoled18();
    const GridGeom ga = gridGeom(A, 3);
    assert(ga.cellW == 172 && ga.cellH >= 170);
    assert(ga.bars.y == A.h - A.footerH && ga.bars.h == A.footerH);
    for (int cell = 0; cell < 4; cell++) {
      const Rect q = cellRect(A, ga, cell);
      assert(q.y >= A.headerH && q.y + q.h <= A.h - A.footerH);
    }
  }

  // ---- paging: by slot on the 4x4, in slot order on the 2x2
  {
    const GridLayout L = layoutLcd4(), A = layoutAmoled18();
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("b", 3, Status::Running), mk("c", 17, Status::Waiting)});
    assert(pageCount(L, f) == 2 && pageCount(A, f) == 1);
    assert(agentAt(L, f, 0, 0) == 0 && agentAt(L, f, 0, 1) == -1 && agentAt(L, f, 0, 3) == 1);
    assert(agentAt(L, f, 1, 1) == 2 && agentAt(L, f, 1, 0) == -1);
    assert(agentAt(A, f, 0, 0) == 0 && agentAt(A, f, 0, 1) == 1 && agentAt(A, f, 0, 2) == 2 && agentAt(A, f, 0, 3) == -1);
    Fleet none = fleetOf({});
    assert(pageCount(L, none) == 1 && pageCount(A, none) == 1);

    PanelUi ui;
    uiSwipe(ui, L, f, Swipe::Left);
    assert(ui.page == 1);
    uiSwipe(ui, L, f, Swipe::Left);                                           // no page 3
    assert(ui.page == 1);
    uiSwipe(ui, L, f, Swipe::Right);
    assert(ui.page == 0);
    uiButton(ui, L, f);                                                       // BOOT: next page, wrapping
    uiButton(ui, L, f);
    assert(ui.page == 0);
    ui.page = 5;
    uiFix(ui, L, f);
    assert(ui.page == 1);
  }

  // ---- taps: a tile opens its card; back and swipes close it; answers
  {
    const GridLayout L = layoutLcd4();
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("w", 5, Status::Waiting)});
    Agent& w = f.agents[1];
    snprintf(w.promptId, sizeof w.promptId, "p17");
    w.optCount = 3;
    const char* keys[] = {"1", "2", "3"};
    for (int i = 0; i < 3; i++) {
      snprintf(w.opts[i].key, sizeof w.opts[i].key, "%s", keys[i]);
      snprintf(w.opts[i].label, sizeof w.opts[i].label, "option %d", i);
    }
    w.opts[0].primary = true;

    PanelUi ui;
    const GridGeom g = gridGeom(L, 1);
    int x, y;
    center(L, g, 1, x, y);                                                    // a free cell
    assert(!uiTap(ui, L, f, x, y, true, 0).send && !ui.openId[0]);
    center(L, g, 5, x, y);
    assert(!uiTap(ui, L, f, x, y, true, 0).send && !strcmp(ui.openId, "w"));

    // Scope view: the options are listed, not buttons; a tap there sends nothing.
    const CardGeom view = cardGeom(L, w, false);
    assert(view.buttonCount == 0);
    assert(!uiTap(ui, L, f, L.w / 2, L.h - 20, false, 0).send && !strcmp(ui.openId, "w"));

    // Scope act: the second button answers with its key and the prompt's id.
    const CardGeom cg = cardGeom(L, w, true);
    assert(cg.buttonCount == 3);
    for (int b = 0; b < 3; b++) {
      assert(cg.buttons[b].y >= cg.optionsTop && cg.buttons[b].y + cg.buttons[b].h <= L.h);
      if (b) assert(cg.buttons[b].y >= cg.buttons[b - 1].y + cg.buttons[b - 1].h);
    }
    const Rect b2 = cg.buttons[1];
    UiAnswer ans = uiTap(ui, L, f, b2.x + b2.w / 2, b2.y + b2.h / 2, true, 1000);
    assert(ans.send && !strcmp(ans.agent, "w") && !strcmp(ans.prompt, "p17") && !strcmp(ans.key, "2"));
    assert(!strcmp(ui.toast, "sending\xE2\x80\xA6"));
    ui.answerSeq = 42;
    uiResult(ui, 41, 204, 1100);                                              // not ours
    assert(ui.answerSeq == 42 && ui.openId[0]);
    uiResult(ui, 42, 409, 1100);
    assert(!strcmp(ui.toast, "the prompt changed") && ui.openId[0] && ui.answerSeq == 0);
    ui.answerSeq = 43;
    uiResult(ui, 43, 204, 1200);
    assert(!strcmp(ui.toast, "answered") && !ui.openId[0]);                 // answered: back to the grid

    // Six options: two columns, still on screen.
    w.optCount = 6;
    for (int i = 3; i < 6; i++) snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
    const CardGeom six = cardGeom(L, w, true);
    assert(six.buttonCount == 6 && six.buttons[1].x > six.buttons[0].x && six.buttons[1].y == six.buttons[0].y);
    for (int b = 0; b < 6; b++) assert(six.buttons[b].x + six.buttons[b].w <= L.w && six.buttons[b].y + six.buttons[b].h <= L.h);

    // Back: the tap target top left, a right swipe, the BOOT button; a vanished agent.
    snprintf(ui.openId, sizeof ui.openId, "w");
    uiTap(ui, L, f, 10, 10, true, 0);
    assert(!ui.openId[0]);
    snprintf(ui.openId, sizeof ui.openId, "w");
    uiSwipe(ui, L, f, Swipe::Left);                                           // no paging under a card
    assert(ui.openId[0] && ui.page == 0);
    uiSwipe(ui, L, f, Swipe::Right);
    assert(!ui.openId[0]);
    snprintf(ui.openId, sizeof ui.openId, "w");
    uiButton(ui, L, f);
    assert(!ui.openId[0] && ui.page == 0);
    snprintf(ui.openId, sizeof ui.openId, "gone");
    uiFix(ui, L, f);
    assert(!ui.openId[0]);

    // A page bar takes you to its page.
    Fleet two = fleetOf({mk("a", 0, Status::Idle), mk("z", 20, Status::Waiting)});
    const GridGeom g2 = gridGeom(L, 2);
    PanelUi u2;
    uiTap(u2, L, two, L.w - 10, g2.bars.y + g2.bars.h / 2, true, 0);
    assert(u2.page == 1 && !u2.openId[0]);
  }

  // ---- gestures
  {
    Gesture g;
    assert(gestureFeed(g, true, 100, 100, 0).kind == GestureKind::None);
    gestureFeed(g, true, 104, 103, 50);
    GestureEvent e = gestureFeed(g, false, 0, 0, 120);
    assert(e.kind == GestureKind::Tap && e.x == 100 && e.y == 100);
    gestureFeed(g, true, 300, 200, 0);
    gestureFeed(g, true, 200, 210, 100);
    e = gestureFeed(g, false, 0, 0, 200);
    assert(e.kind == GestureKind::Swipe && e.swipe == Swipe::Left);
    gestureFeed(g, true, 100, 300, 0);
    gestureFeed(g, true, 110, 200, 100);
    e = gestureFeed(g, false, 0, 0, 200);
    assert(e.kind == GestureKind::Swipe && e.swipe == Swipe::Up);
    gestureFeed(g, true, 100, 100, 0);                                        // a long press is no tap
    e = gestureFeed(g, false, 0, 0, 2000);
    assert(e.kind == GestureKind::None);
    gestureFeed(g, true, 100, 100, 0);                                        // a short drag is neither
    gestureFeed(g, true, 125, 100, 100);
    assert(gestureFeed(g, false, 0, 0, 150).kind == GestureKind::None);
    assert(gestureFeed(g, false, 0, 0, 160).kind == GestureKind::None);       // no release without a press
  }

  // ---- the tile's status line (agentMetaLine)
  {
    char b[64];
    Agent a = mk("a", 0, Status::Running);
    a.ctxUsed = 41; a.ctxWindow = 100;
    snprintf(a.actTool, sizeof a.actTool, "WebFetch");
    metaLine(a, 0, b, sizeof b);
    assert(!strcmp(b, "fetch \xC2\xB7 41% ctx"));
    a.actTool[0] = '\0';
    metaLine(a, 0, b, sizeof b);
    assert(!strcmp(b, "working \xC2\xB7 41% ctx"));
    Agent w = mk("w", 0, Status::Waiting);
    metaLine(w, 0, b, sizeof b);
    assert(!strcmp(b, "needs you"));
    snprintf(w.waitKind, sizeof w.waitKind, "permission");
    snprintf(w.promptCtx, sizeof w.promptCtx, "Bash \xC2\xB7 ~/dev/x");
    metaLine(w, 0, b, sizeof b);
    assert(!strcmp(b, "allow bash?"));
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    w.optCount = 3;
    metaLine(w, 0, b, sizeof b);
    assert(!strcmp(b, "pick 1 of 3"));
    Agent h = mk("h", 0, Status::Idle);
    h.unseen = true;
    metaLine(h, 0, b, sizeof b);
    assert(!strcmp(b, "hungry \xC2\xB7 feed me"));
    h.unseen = false;
    h.statusSince = 1000;
    metaLine(h, 1000 + 300, b, sizeof b);
    assert(!strcmp(b, "done \xC2\xB7 5m ago"));
    Agent x = mk("x", 0, Status::Exited);
    x.resumable = true;
    metaLine(x, 0, b, sizeof b);
    assert(!strcmp(b, "ended \xC2\xB7 resumable"));
    x.lost = true;
    metaLine(x, 0, b, sizeof b);
    assert(!strcmp(b, "lost with tmux"));
  }

  // ---- the PanelLayout interface is the same code
  {
    const GridPanel P(layoutLcd4());
    const PanelLayout& pl = P;
    assert(pl.width() == 480 && pl.height() == 480 && !pl.trueBlack());
    const GridPanel A(layoutAmoled18());
    assert(A.width() == 368 && A.height() == 448 && A.trueBlack());
    Fleet f = fleetOf({mk("a", 0, Status::Idle)});
    PanelUi ui;
    const GridGeom g = gridGeom(P.L, 1);
    int x, y;
    center(P.L, g, 0, x, y);
    pl.tap(ui, f, x, y, false, 0);
    assert(!strcmp(ui.openId, "a"));
    pl.button(ui, f);
    assert(!ui.openId[0]);
  }

  // ---- the AMOLED's rounded corners: black outside the arcs, nothing important inside them
  {
    const GridLayout A = layoutAmoled18();
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> buf(A.w * A.h);
    Canvas c{buf.data(), A.w, A.h};
    PanelUi ui;
    PanelInput in{&d.fleet, LinkState::Live, "x", true, 0, d.fleet.serverTime, &ui};
    drawPanel(c, A, deckTheme(true), in);
    assert(c.px[0] == 0 && c.px[A.w - 1] == 0 && c.px[(A.h - 1) * A.w] == 0 && c.px[A.h * A.w - 1] == 0);
    // Every tile lies inside the rounded rectangle.
    const GridGeom g = gridGeom(A, pageCount(A, d.fleet));
    auto inside = [&](int px, int py) {
      const int r = A.cornerR;
      const int cx = px < r ? r : px >= A.w - r ? A.w - 1 - r : px;
      const int cy = py < r ? r : py >= A.h - r ? A.h - 1 - r : py;
      return (px - cx) * (px - cx) + (py - cy) * (py - cy) <= r * r;
    };
    for (int cell = 0; cell < perPage(A); cell++) {
      const Rect r = cellRect(A, g, cell);
      assert(inside(r.x, r.y) && inside(r.x + r.w - 1, r.y) && inside(r.x, r.y + r.h - 1) &&
             inside(r.x + r.w - 1, r.y + r.h - 1));
    }
  }

  // ---- every screen of both layouts stays inside the frame
  checkScreens(layoutLcd4());
  checkScreens(layoutAmoled18());
  return 0;
}
