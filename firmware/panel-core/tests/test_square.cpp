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
      static Attention att;                                  // the pages anywhere in their slide, past the ends too
      att = Attention();
      ui.att = &att;
      squareTick(ui, S, d.fleet, 300);
      ui.sliding = true;
      for (float p : {-0.2f, 0.f, 0.01f, 0.3f, 0.5f, 0.97f, 1.f, 1.2f}) {
        ui.slide = p;
        ui.scroll = p * 120;
        P.draw(c, in);
        assert(guards());
      }
      ui.sliding = false;
      ui.scroll = 0;
      ui.att = nullptr;
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

  // ---- swipe direction: only a move clearly more across than up or down
  // (|dx| > 1.3 |dy|) slides the page; a diagonal that goes mostly up does not.
  {
    Fleet f = fleetOf(2);
    f.agents[1].slot = 17;
    auto swipe = [&](PanelUi& ui, int dx, int dy, uint32_t ms) {
      squareTouch(ui, L, f, true, 240, 300, DeckScope::Act, ms);
      squareTouch(ui, L, f, true, 240 + dx / 2, 300 + dy / 2, DeckScope::Act, ms + 40);
      squareTouch(ui, L, f, true, 240 + dx, 300 + dy, DeckScope::Act, ms + 80);
      squareTouch(ui, L, f, false, 0, 0, DeckScope::Act, ms + 90);
      for (uint32_t t = 100; t <= 400; t += 10) squareTick(ui, L, f, ms + t);
    };
    PanelUi ui;
    swipe(ui, -60, -100, 0);                                     // up and a bit left: no page
    assert(ui.page == 0);
    swipe(ui, -80, -70, 1000);                                    // about as much up as across: no page
    assert(ui.page == 0);
    swipe(ui, 0, -120, 2000);                                     // straight up: no page either
    assert(ui.page == 0);
    swipe(ui, -100, -60, 3000);                                   // mostly across: the next page
    assert(ui.page == 1);
    swipe(ui, 60, 100, 4000);                                     // down and right: stays
    assert(ui.page == 1);
    swipe(ui, 100, 20, 5000);                                    // across: back
    assert(ui.page == 0);
    // On a card, back is a sideways move too.
    const Rect r0 = cellRect(L, gridGeom(L, 2), 0);
    touchAt(L, ui, f, r0.x + r0.w / 2, r0.y + r0.h / 2, 6000, DeckScope::Act);
    assert(ui.screen == Screen::Card);
    swipe(ui, 70, -110, 7000);
    assert(ui.screen == Screen::Card);
    swipe(ui, 120, -40, 8000);
    assert(ui.screen == Screen::Home);
    assert(swipeAcross(131, 100) && !swipeAcross(130, 100) && !swipeAcross(0, 0));
  }

  // ---- the "Needs you" screen on the list boards: what the list shows at
  // once (2 x 2, 2 x 3 on the T4-S3), to the left of the list, not scrolling
  {
    assert(squareSpots(A) == 4 && squareSpots(specT4S3()) == 6 && squareSpots(L) == 16);
    static Attention att;
    att = Attention();
    Fleet f = fleetOf(6);
    f.agents[1].status = Status::Running;
    f.agents[4].status = Status::Waiting;
    f.agents[5].unseen = true;                                   // hungry
    PanelUi ui;
    ui.att = &att;
    uint32_t ms = 0;
    squareTick(ui, A, f, ms);
    assert(att.cap == 4 && att.count == 3 && !ui.needsYou);
    // Swipe right on the list: the screen. Its spots sit where the list's first tiles do.
    squareTouch(ui, A, f, true, 60, 250, DeckScope::Act, 100);
    squareTouch(ui, A, f, true, 200, 255, DeckScope::Act, 150);
    squareTouch(ui, A, f, false, 0, 0, DeckScope::Act, 160);
    assert(ui.needsYou && !ui.dragging);
    for (uint32_t t = 176; ui.slideAnim; t += 16) squareTick(ui, A, f, t);   // the pages slide there
    const Rect s0 = listTile(A, 0, 0), s2 = listTile(A, 2, 0), s3 = listTile(A, 3, 0);
    assert(squareHit(A, ui, f, s0.x + 5, s0.y + 5, DeckScope::Act).agent == 1);
    assert(squareHit(A, ui, f, s2.x + 5, s2.y + 5, DeckScope::Act).agent == 5);
    assert(squareHit(A, ui, f, s3.x + 5, s3.y + 5, DeckScope::Act).kind == Hit::None);
    // It doesn't scroll.
    drag(A, ui, f, 100, 400, 100, 1000, 200);
    settle(A, ui, f, ms);
    assert(ui.scroll == 0 && ui.needsYou);
    // A tile opens its card or prompt; back is the screen again.
    touchAt(A, ui, f, s0.x + 20, s0.y + 20, 2000, DeckScope::Act);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a1"));
    squareButton(ui, A, f);
    assert(ui.screen == Screen::Home && ui.needsYou);
    // Swipe left: the list, at its top. BOOT goes there and back.
    squareSwipe(ui, A, f, Swipe::Right);
    assert(ui.needsYou);
    squareSwipe(ui, A, f, Swipe::Left);
    assert(!ui.needsYou);
    squareButton(ui, A, f);
    assert(ui.needsYou);
    squareButton(ui, A, f);
    assert(!ui.needsYou);
    // Drawn: free spots as empty tiles, the header's two marks.
    std::vector<uint16_t> buf(A.w * A.h);
    Canvas c{buf.data(), A.w, A.h};
    const Theme T = deckTheme(true);
    PanelInput in{&f, LinkState::Live, "x", DeckScope::Act, 3000, 0, &ui, 0};
    ui.needsYou = true;
    SquarePanel(A).draw(c, in);
    assert(buf[(s3.y + s3.h / 2) * A.w + s3.x + s3.w / 2] == T.emptyFill);
    int orange = 0;
    for (int y = 0; y < 10; y++)
      for (int x = 0; x < A.w; x++) orange += buf[y * A.w + x] == T.accent;
    assert(orange > 60);                                         // the solid mark, over the header
    ui.needsYou = false;
    SquarePanel(A).draw(c, in);
    int orange2 = 0;
    for (int y = 0; y < 10; y++)
      for (int x = 0; x < A.w; x++) orange2 += buf[y * A.w + x] == T.accent;
    assert(orange2 > 0 && orange2 < orange);                     // the outline: someone is there
  }

  // ---- the list's two pages slide: a sideways drag moves both with the finger, and they settle on the
  // nearer one or on the one a flick points at; up and down still scrolls; a tap still opens a card
  for (const SquareSpec& S : {A, specT4S3()}) {
    static Attention att;
    att = Attention();
    Fleet f = fleetOf(12);
    f.agents[1].status = Status::Running;
    f.agents[4].status = Status::Waiting;
    PanelUi ui;
    ui.att = &att;
    uint32_t ms = 0;
    squareTick(ui, S, f, ms);
    const DeckScope sc = DeckScope::Act;
    const int y = S.headerH + 150;
    auto slideDone = [&] {                                      // let the pages settle; true if they did in time
      const uint32_t t0 = ms;
      while (ui.slideAnim && ms - t0 < 1000) { ms += 16; squareTick(ui, S, f, ms); }
      return !ui.slideAnim && ms - t0 >= 200 && ms - t0 <= kSettleMs + 16;
    };
    assert(pagerPos(ui) == 1 && !squareTick(ui, S, f, ms));

    // One direction to a touch. Inside the slop nothing moves; mostly sideways slides the pages and the
    // list stays put, even when the finger then wanders up.
    squareTouch(ui, S, f, true, 300, y, sc, ms = 1000);
    squareTouch(ui, S, f, true, 290, y + 4, sc, ms += 20);
    assert(!ui.sliding && !ui.dragging && pagerPos(ui) == 1);
    squareTouch(ui, S, f, true, 270, y + 10, sc, ms += 20);
    assert(ui.sliding && !ui.dragging && ui.pressed.kind == Hit::None);
    squareTouch(ui, S, f, true, 260, y - 120, sc, ms += 20);
    assert(ui.sliding && !ui.dragging && ui.scroll == 0);
    assert(squareTick(ui, S, f, ms));                           // the board draws often while it slides
    // No page right of the list: the pages give a little and come back (no wrap).
    squareTouch(ui, S, f, true, 100, y, sc, ms += 200);
    assert(ui.slide > 1 && ui.slide < 1.25f);
    squareTouch(ui, S, f, true, 100, y, sc, ms += 200);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(!ui.needsYou && ui.slideAnim && ui.slideTo == 1);
    assert(slideDone() && pagerPos(ui) == 1 && !ui.needsYou);
    // Mostly up: the list scrolls and the pages stay, even when the finger then wanders sideways.
    squareTouch(ui, S, f, true, 100, 400, sc, ms += 100);
    squareTouch(ui, S, f, true, 108, 370, sc, ms += 20);
    assert(ui.dragging && !ui.sliding);
    squareTouch(ui, S, f, true, 300, 340, sc, ms += 200);
    assert(ui.dragging && !ui.sliding && ui.scroll == 60 && pagerPos(ui) == 1);
    squareTouch(ui, S, f, true, 300, 340, sc, ms += 200);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(!ui.needsYou && !ui.slideAnim && ui.animating);
    settle(S, ui, f, ms);
    assert(ui.scroll == 0);

    // The pages follow the finger: 30 % of the width to the right shows 30 % of "Needs you".
    const int d30 = S.w * 3 / 10, d70 = S.w * 7 / 10;
    squareTouch(ui, S, f, true, 20, y, sc, ms += 100);
    squareTouch(ui, S, f, true, 20 + d30, y, sc, ms += 300);
    assert(ui.sliding && fabsf(pagerPos(ui) - 0.7f) < 0.01f && !ui.needsYou);
    // Let go slowly there: past the commit point (5 mm on the glass), so on to "Needs you", eased over about
    // a quarter second.
    squareTouch(ui, S, f, true, 20 + d30, y, sc, ms += 300);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(!ui.sliding && ui.slideAnim && ui.needsYou && ui.slideFrom < 0.71f && ui.slideTo == 0);
    squareTick(ui, S, f, ms += 60);
    const float early = pagerPos(ui);
    assert(early < 0.7f - 0.7f * 60 / kSettleMs && early > 0);  // ahead of a straight line: it eases out
    assert(slideDone() && pagerPos(ui) == 0 && !squareTick(ui, S, f, ms + 2000));
    // Back to the list, then a short slow pull short of the commit point: it springs back.
    squareButton(ui, S, f);
    assert(!ui.needsYou);
    const int commit = pagerCommitPx(S.w, S.h, S.diagIn);
    squareTouch(ui, S, f, true, 20, y, sc, ms += 100);
    squareTouch(ui, S, f, true, 20 + commit - 10, y, sc, ms += 300);
    assert(ui.sliding && fabsf(pagerPos(ui) - (1 - (float)(commit - 10) / S.w)) < 0.01f);
    squareTouch(ui, S, f, true, 20 + commit - 10, y, sc, ms += 300);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(!ui.needsYou && ui.slideAnim && ui.slideTo == 1);
    assert(slideDone() && pagerPos(ui) == 1);
    // Just past it, slowly: on to "Needs you".
    squareTouch(ui, S, f, true, 20, y, sc, ms += 100);
    squareTouch(ui, S, f, true, 20 + commit + 10, y, sc, ms += 300);
    squareTouch(ui, S, f, true, 20 + commit + 10, y, sc, ms += 300);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(ui.needsYou && ui.slideAnim && ui.slideTo == 0);
    assert(slideDone() && pagerPos(ui) == 0 && ui.needsYou);
    (void)d70;
    // No page left of "Needs you" (no wrap); an up-and-down drag there moves nothing.
    squareTouch(ui, S, f, true, 20, y, sc, ms += 100);
    squareTouch(ui, S, f, true, 320, y, sc, ms += 300);
    assert(ui.slide < 0 && ui.slide > -0.3f);
    squareTouch(ui, S, f, true, 320, y, sc, ms += 300);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(ui.needsYou && slideDone() && pagerPos(ui) == 0);
    squareTouch(ui, S, f, true, 100, 400, sc, ms += 100);
    squareTouch(ui, S, f, true, 100, 200, sc, ms += 100);
    assert(!ui.sliding && !ui.dragging);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(ui.needsYou && !ui.slideAnim && ui.scroll == 0);

    // A flick: a short fast drag goes to the page it points at, though the nearer one is behind it.
    squareTouch(ui, S, f, true, 300, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 270, y, sc, ms += 16);
    squareTouch(ui, S, f, true, 230, y, sc, ms += 16);
    assert(pagerPos(ui) < 0.3f);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    assert(!ui.needsYou && ui.slideTo == 1 && slideDone() && pagerPos(ui) == 1);
    squareTouch(ui, S, f, true, 60, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 90, y, sc, ms += 16);
    squareTouch(ui, S, f, true, 130, y, sc, ms += 16);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    assert(ui.needsYou && ui.slideTo == 0 && slideDone());
    // A flick back at the end of a long drag wins over where the pages are.
    squareTouch(ui, S, f, true, S.w - 40, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 40, y, sc, ms += 300);
    squareTouch(ui, S, f, true, 40, y, sc, ms += 100);
    assert(pagerPos(ui) > 0.75f);
    squareTouch(ui, S, f, true, 70, y, sc, ms += 16);
    squareTouch(ui, S, f, true, 110, y, sc, ms += 16);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    assert(ui.needsYou && slideDone());

    // A tap during the slide catches the pages and opens nothing; they settle on the nearer page.
    squareTouch(ui, S, f, true, 300, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 200, y, sc, ms += 16);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    assert(!ui.needsYou && ui.slideAnim);
    squareTick(ui, S, f, ms += 16);
    const float mid = pagerPos(ui);
    assert(mid > 0 && mid < 0.5f);
    touchAt(S, ui, f, 100, y, ms, sc);
    assert(ui.screen == Screen::Home && ui.slideAnim && ui.slideFrom == mid && ui.needsYou && ui.slideTo == 0);
    ms += 60;
    assert(slideDone() && pagerPos(ui) == 0);
    // At rest a tap opens the card, on either page; back is the page it was opened from.
    const Rect s0 = listTile(S, 0, 0);
    touchAt(S, ui, f, s0.x + 20, s0.y + 20, ms += 100, sc);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a1"));
    squareButton(ui, S, f);
    assert(ui.screen == Screen::Home && ui.needsYou && pagerPos(ui) == 0);
    squareButton(ui, S, f);                                     // BOOT: the list, at once
    assert(!ui.needsYou && pagerPos(ui) == 1 && !squareTick(ui, S, f, ms));
    const Rect t3 = listTile(S, 3, 0);
    touchAt(S, ui, f, t3.x + 20, t3.y + 20, ms += 100, sc);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a3"));
    // A card opened while the pages slide ends the slide: home is the chosen page.
    ui.slideAnim = true;
    squareTick(ui, S, f, ms);
    assert(!ui.slideAnim);
    squareHome(ui);

    // The page set from outside while the pages slide (BOOT, the deck): it shows at once, no slide left over.
    squareTouch(ui, S, f, true, 60, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 200, y, sc, ms += 16);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    assert(ui.needsYou && ui.slideAnim);
    squareTick(ui, S, f, ms += 16);
    ui.needsYou = false;                                        // set directly
    squareTick(ui, S, f, ms += 16);
    assert(!ui.slideAnim && pagerPos(ui) == 1);
    squareTouch(ui, S, f, true, 60, y, sc, ms += 500);
    squareTouch(ui, S, f, true, 200, y, sc, ms += 16);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 8);
    squareButton(ui, S, f);                                     // BOOT mid-slide
    assert(!ui.needsYou && !ui.slideAnim && pagerPos(ui) == 1);
    squareTouch(ui, S, f, true, 60, y, sc, ms += 500);          // BOOT with the finger still down
    squareTouch(ui, S, f, true, 200, y, sc, ms += 100);
    squareButton(ui, S, f);
    assert(ui.needsYou && !ui.sliding && pagerPos(ui) == 0);
    squareTouch(ui, S, f, true, 205, y, sc, ms += 100);
    squareTouch(ui, S, f, false, 0, 0, sc, ms += 10);
    assert(ui.needsYou && ui.screen == Screen::Home && !ui.slideAnim && pagerPos(ui) == 0);
    settle(S, ui, f, ms);
    ui.needsYou = false;

    // Drawn mid-slide: both pages side by side under the header, "Needs you" at the left.
    std::vector<uint16_t> buf(S.w * S.h);
    Canvas c{buf.data(), S.w, S.h};
    const Theme T = deckTheme(true);
    PanelInput in{&f, LinkState::Live, "x", sc, ms, 0, &ui, 0};
    auto at = [&](int px, int py) { return buf[py * S.w + px]; };
    ui.sliding = true;
    ui.slide = 0.7f;
    SquarePanel(S).draw(c, in);
    const int listX = S.w - (int)lroundf(0.7f * S.w);
    const Rect n1 = listTile(S, 1, 0), n3 = listTile(S, 3, 0), l0 = listTile(S, 0, 0);
    assert(at(n1.x + listX - S.w + n1.w - 30, n1.y + 12) == T.accent);     // the waiting agent's spot, sliding in
    assert(at(n3.x + listX - S.w + n3.w - 30, n3.y + 60) == T.emptyFill);  // a free spot under it
    assert(at(l0.x + listX + 30, l0.y + 12) == T.panel);                   // the list's first tile, sliding out
    assert(at(listX - 4, l0.y + 60) == T.bg);                              // the gap between the pages
    // The header's marks follow: the list's still the wider, and bright.
    auto markRun = [&](uint16_t col) { int n = 0; for (int x = 0; x < S.w; x++) n += at(x, 6) == col; return n; };
    ui.sliding = false;
    SquarePanel(S).draw(c, in);
    const int restInk = markRun(T.ink);
    assert(restInk >= 18 && restInk <= 20);
    ui.sliding = true;
    ui.slide = 0.5f;
    SquarePanel(S).draw(c, in);
    assert(markRun(T.ink) == 0 && markRun(T.ringTrack) == 0);              // halfway: between bright and dim
    ui.slide = 0.2f;
    SquarePanel(S).draw(c, in);
    assert(markRun(T.accent) >= 16);                                       // "Needs you" filled and wide
    ui.sliding = false;

    // A board without the screen has no pages: sideways does nothing, as before.
    PanelUi plain;
    squareTouch(plain, S, f, true, 60, y, sc, 100);
    squareTouch(plain, S, f, true, 200, y, sc, 150);
    assert(!plain.sliding);
    squareTouch(plain, S, f, false, 0, 0, sc, 160);
    assert(!plain.needsYou && !plain.slideAnim && !squareTick(plain, S, f, 200));
  }
  // A grid's pages slide too: "Needs you", then every page, under the finger.
  {
    static Attention att;
    att = Attention();
    Fleet f = fleetOf(2);
    f.agents[1].status = Status::Running;
    f.agents[1].slot = 17;                                       // two pages
    PanelUi ui;
    ui.att = &att;
    squareTick(ui, L, f, 0);
    assert(pagerCount(ui, pageCount(L, f)) == 3 && pagerIndex(ui) == 1);
    squareTouch(ui, L, f, true, 300, 200, DeckScope::Act, 100);
    squareTouch(ui, L, f, true, 200, 205, DeckScope::Act, 150);   // 100 px to the left: towards page 2
    assert(ui.sliding && fabsf(pagerAt(ui) - (1 + 100.f / L.w)) < 0.01f);
    squareTouch(ui, L, f, true, 200, 205, DeckScope::Act, 400);
    squareTouch(ui, L, f, false, 0, 0, DeckScope::Act, 410);
    assert(ui.page == 1 && !ui.needsYou && ui.slideAnim && ui.slideTo == 2);
    for (uint32_t t = 420; ui.slideAnim; t += 16) squareTick(ui, L, f, t);
    assert(pagerAt(ui) == 2);
    // Back two pages to "Needs you", one at a time; past it, nothing.
    for (int k = 0; k < 3; k++) {
      const uint32_t t0 = 2000 + 1000 * k;
      squareTouch(ui, L, f, true, 100, 200, DeckScope::Act, t0);
      squareTouch(ui, L, f, true, 250, 205, DeckScope::Act, t0 + 300);
      squareTouch(ui, L, f, true, 250, 205, DeckScope::Act, t0 + 500);
      squareTouch(ui, L, f, false, 0, 0, DeckScope::Act, t0 + 510);
      for (uint32_t t = t0 + 520; ui.slideAnim; t += 16) squareTick(ui, L, f, t);
    }
    assert(ui.needsYou && pagerAt(ui) == 0);
  }

  // ---- the deck's spots on the list screen: lowest spot first, the rest not
  // shown but counted (the fleet above had none: an older deck, deck order)
  {
    static Attention att;
    att = Attention();
    Fleet f = fleetOf(6);
    f.agents[1].status = Status::Running, f.agents[1].attnSlot = 9;
    f.agents[2].status = Status::Running, f.agents[2].attnSlot = 14;
    f.agents[3].status = Status::Running, f.agents[3].attnSlot = 5;
    f.agents[4].status = Status::Waiting, f.agents[4].attnSlot = 2;
    f.agents[5].unseen = true, f.agents[5].attnSlot = 1;
    PanelUi ui;
    ui.att = &att;
    squareTick(ui, A, f, 0);
    assert(att.cap == 4 && att.count == 5 && att.fromDeck);
    ui.needsYou = true;
    int want[4] = {5, 4, 3, 1};
    for (int k = 0; k < 4; k++) {
      const Rect r = listTile(A, k, 0);
      assert(squareHit(A, ui, f, r.x + 5, r.y + 5, DeckScope::Act).agent == want[k]);
    }
  }

  // ---- the "Needs you" screen on the grid: left of the first page, the mark at the left of the bars
  {
    static Attention att;
    att = Attention();
    Fleet f = fleetOf(2);
    f.agents[1].slot = 17;
    f.agents[1].status = Status::Running;
    PanelUi ui;
    ui.att = &att;
    squareTick(ui, L, f, 0);
    const GridGeom g = gridGeomFor(L, ui, 2);
    squareSwipe(ui, L, f, Swipe::Right);
    assert(ui.needsYou);
    const Rect r0 = cellRect(L, g, 0);
    assert(squareHit(L, ui, f, r0.x + 5, r0.y + 5, DeckScope::Act).agent == 1);
    squareSwipe(ui, L, f, Swipe::Down);                          // up or down: not a page change
    assert(ui.needsYou);
    squareSwipe(ui, L, f, Swipe::Left);
    assert(!ui.needsYou && ui.page == 0);
    const SquareBars b = squareBars(L, g, ui, 2);
    assert(b.n == 3 && b.mark && b.countX >= 0);
    assert(squareHit(L, ui, f, b.r[0].x + 2, g.bars.y + 2, DeckScope::Act).kind == Hit::NeedsYou);
    touchAt(L, ui, f, b.r[0].x + 2, g.bars.y + g.bars.h / 2, 100, DeckScope::Act);
    assert(ui.needsYou);
    touchAt(L, ui, f, b.r[2].x + 2, g.bars.y + g.bars.h / 2, 300, DeckScope::Act);
    assert(!ui.needsYou && ui.page == 1);
    squareButton(ui, L, f);                                      // BOOT after the last page: the screen
    assert(ui.needsYou);
    squareButton(ui, L, f);
    assert(!ui.needsYou && ui.page == 0);
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
    // The whole band under the rule answers, split left and right: a corner of the glass is focus.
    c = touchAt(L, ui, f, 2, L.h - 2, 50, DeckScope::Act);
    assert(c.cmd == DeckCmd::Focus);
    // Interrupt asks first; cancel sends nothing, interrupt sends it.
    c = touchAt(L, ui, f, ba.cx[1], ba.cy, 100, DeckScope::Act);
    assert(c.cmd == DeckCmd::None && ui.confirm);
    {
      const ConfirmGeom g = confirmGeom(L);
      c = touchAt(L, ui, f, 4, g.titleY + 4, 110, DeckScope::Act);           // the sheet itself: nothing
      assert(c.cmd == DeckCmd::None && ui.confirm);
      c = touchAt(L, ui, f, g.no.x + g.no.w / 2, g.no.y + g.no.h / 2, 120, DeckScope::Act);
      assert(c.cmd == DeckCmd::None && !ui.confirm);
      touchAt(L, ui, f, ba.cx[1], ba.cy, 130, DeckScope::Act);
      squareButton(ui, L, f);                                             // BOOT cancels too
      assert(!ui.confirm && ui.screen == Screen::Card);
      touchAt(L, ui, f, ba.cx[1], ba.cy, 140, DeckScope::Act);
      c = touchAt(L, ui, f, g.yes.x + g.yes.w / 2, g.yes.y + g.yes.h / 2, 150, DeckScope::Act);
      assert(c.cmd == DeckCmd::Interrupt && !ui.confirm);
    }
    // Stop with scope act: not there (where it would be is interrupt's half, which asks).
    c = touchAt(L, ui, f, b.cx[2], b.cy, 200, DeckScope::Act);
    assert(c.cmd == DeckCmd::None);
    ui.confirm = false;
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
  checkScreens(specT4S3());

  // ---- the T4-S3: the 1.8"'s layout, three rows of tiles on screen, bigger critters
  {
    const SquareSpec T = specT4S3();
    assert(T.w == 450 && T.h == 600 && T.home == HomeKind::List && T.cols == 2 && T.statLines && !A.statLines);
    assert(T.tileScale > A.tileScale && A.tileScale == 5 && T.cardScale > A.cardScale);
    assert(T.label == A.label && T.body == A.body && T.title == A.title && T.tileTitle == A.tileTitle);
    Fleet f = fleetOf(9);                                       // 5 rows of 2
    assert(maxScroll(T, f) == 2 * rowPitch(T));                 // three rows show at once
    assert(maxScroll(T, fleetOf(6)) == 0);
    const Rect last = listTile(T, 5, 0);
    assert(last.y + last.h <= T.h && listTile(T, 1, 0).x + T.tileW <= T.w);
    // Each part of a tile inside it, the text above the bar, the critter above the name (both panels).
    for (const SquareSpec* S : {&A, &T}) {
      const Rect r = listTile(*S, 0, 0);
      const square_detail::TileParts p = square_detail::tileParts(*S, r, true);
      assert(p.critter.x >= r.x && p.critter.x + p.critter.w <= r.x + r.w && p.critter.y >= r.y);
      assert(p.critter.y + p.critter.h <= p.name.y && p.meta.y + p.meta.h <= p.bar.y && p.bar.y + p.bar.h <= r.y + r.h);
    }
    // A tap on a third-row tile opens it; the card's focus button is an icon; swipe right goes back.
    PanelUi ui;
    const Rect t4 = listTile(T, 4, 0);
    touchAt(T, ui, f, t4.x + t4.w / 2, t4.y + t4.h / 2, 0, DeckScope::Act);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a4"));
    const CardButtons b = cardButtons(T, f.agents[4], DeckScope::Act);
    assert(b.n == 1 && b.kind[0] == IconKind::Focus && b.cy == T.h - T.btnFromBottom);
    const DeckCommand c = touchAt(T, ui, f, b.cx[0], b.cy, 100, DeckScope::Act);
    assert(c.cmd == DeckCmd::Focus);
    squareTouch(ui, T, f, true, 60, 300, DeckScope::Act, 1000);
    squareTouch(ui, T, f, true, 260, 305, DeckScope::Act, 1100);
    squareTouch(ui, T, f, false, 0, 0, DeckScope::Act, 1110);
    assert(ui.screen == Screen::Home);
  }
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
    const SquareSpec T4 = specT4S3();
    const GridGeom w = gridGeom(L, 1, true);
    assert(w.bars.w == L.w && w.bars.h == L.barsH && w.cellW == w.cellH);
    assert(cellRect(L, w, 15).y + cellRect(L, w, 15).h <= w.bars.y);
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    for (const SquareSpec* S : {&A, &L, &T4}) {
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
  // ---- the card on the AMOLEDs: the name and the pill as one block centred on the critter; the working
  // subagents under "doing now", their count beside it; the last prompt only where a line of it fits
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    const SquareSpec T4 = specT4S3();
    for (const SquareSpec* S : {&A, &T4}) {
      for (DeckScope sc : {DeckScope::Admin, DeckScope::View}) {
        for (const char* id : {"k3f9a2", "scr4tc", "1nfr4t", "bare", "d0cs1t", "t3st5a"}) {
          if (!strcmp(id, "bare") && findAgent(d.fleet, "bare") < 0) {   // a block critter without a hat
            Agent& b = d.fleet.agents[findAgent(d.fleet, "k8sn0d")];
            snprintf(b.id, sizeof b.id, "bare");
            b.hat = 0;
            b.species = Species::Claude;
          }
          const int ai = findAgent(d.fleet, id);
          assert(ai >= 0);
          const Agent& a = d.fleet.agents[ai];
          const square_detail::CardGeom g = square_detail::cardGeom(*S, a, sc);
          // The block (the name's capitals to the pill's foot) centred on the drawn critter, +-2 px.
          const int blockTop = g.name.y + square_detail::capTop(*S->title), blockBot = g.pill.y + g.pill.h;
          const int mid = square_detail::critterMid(g.critter, S->cardScale, a);
          assert(abs((blockTop + blockBot) / 2 - mid) <= 2);
          assert(g.name.y + g.name.h <= g.pill.y && blockTop >= g.critter.y - 4 && blockBot <= g.critter.y + g.critter.h + 6);
          // And so it is drawn: the header's ink beside the critter, and the critter's own.
          std::vector<uint16_t> buf(S->w * S->h);
          Canvas c{buf.data(), S->w, S->h};
          PanelUi ui;
          ui.screen = Screen::Card;
          snprintf(ui.openId, sizeof ui.openId, "%s", id);
          PanelInput in{&d.fleet, LinkState::Live, "x", sc, 0, d.fleet.serverTime, &ui, 0};
          SquarePanel(*S).draw(c, in);
          const uint16_t bg = c.px[(S->h / 2) * S->w + 2];
          auto inkRows = [&](int x0, int x1, int y0, int y1, int& top, int& bot) {
            top = -1; bot = -1;
            for (int y = y0; y < y1; y++)
              for (int x = x0; x < x1; x++)
                if (c.px[y * S->w + x] != bg) { if (top < 0) top = y; bot = y; break; }
          };
          int ct, cb, tt, tb;
          inkRows(g.critter.x, g.critter.x + 14 * S->cardScale, 0, g.doingY, ct, cb);   // the body and hat, no bubble
          inkRows(g.name.x, S->w - S->m, 0, g.doingY, tt, tb);
          assert(ct >= 0 && tt >= 0);
          assert(abs((ct + cb) / 2 - (tt + tb) / 2) <= 2);
          // Subagents: a row each for those kept, as many as fit; none without them.
          if (a.subagents) {
            assert(g.subRows >= 2 && g.subRows <= a.subCount);
            if (S == &T4 || sc == DeckScope::View) assert(g.subRows == a.subCount);
            for (int r = 0; r < g.subRows; r++) {
              int rt, rb;
              const int y0 = g.subY + r * g.subPitch;
              inkRows(S->m, S->m + 14 * S->subScale, y0, y0 + g.subPitch, rt, rb);   // its critter
              assert(rt >= 0);
              inkRows(S->m + 14 * S->subScale + 10, S->w - S->m, y0, y0 + g.subPitch, rt, rb);   // its task
              assert(rt >= 0);
            }
            int rt, rb;
            inkRows(S->w / 2, S->w - S->m, g.doingY, g.doingY + S->label->cellH - 4, rt, rb);   // "3 subagents"
            assert(rt >= 0);
          } else {
            assert(g.subRows == 0 && g.ctxY == g.subY);
            int rt, rb;
            inkRows(S->w / 2, S->w - S->m, g.doingY, g.doingY + S->label->cellH - 4, rt, rb);
            assert(rt < 0);
          }
          assert(g.subY + g.subRows * g.subPitch <= g.ctxY && g.ctxY < g.statsY && g.statsY < g.lastY);
          assert(g.lastY <= g.ruleY);
          if (g.lastLines) assert(g.lastY + S->label->cellH - 4 + g.lastLines * (S->body->cellH - 2) <= g.ruleY - 4);
          // Without subagents the last prompt always has a line.
          if (!a.subagents) assert(g.lastLines >= 1);
        }
      }
    }
  }
  printf("ok  square layouts\n");
  return 0;
}
