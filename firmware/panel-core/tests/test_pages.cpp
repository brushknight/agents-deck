// Home's pages sliding under the finger (page_slide.h) on the grid and list
// homes: where released pages go, and the moving frame cut from two pictures
// of the pages at rest, on the canvas and as a split for the board.
#include <assert.h>
#include <math.h>
#include <stdio.h>

#include <vector>

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

// The page at rest, drawn whole.
static std::vector<uint16_t> atRest(const PanelLayout& L, const Fleet& f, const PanelUi& ui0, int index, uint32_t ms) {
  const int W = L.width(), H = L.height();
  std::vector<uint16_t> px(W * H);
  Canvas c{px.data(), W, H};
  PanelUi u = ui0;
  u.sliding = u.slideAnim = false;
  pagerGo(u, index);
  PanelInput in{&f, LinkState::Live, "x", DeckScope::Act, ms, 0, &u, 0};
  L.draw(c, in);
  return px;
}

// The pages at `at`: the frame on the canvas, and as a split.
static void checkSlide(const PanelLayout& L, const Fleet& f, PanelUi ui, float at, int rowFrom, int rowTo) {
  const int W = L.width(), H = L.height();
  const int i0 = (int)floorf(at), shift = (int)lroundf((at - i0) * W);
  const std::vector<uint16_t> a = atRest(L, f, ui, i0, 500), b = atRest(L, f, ui, i0 + 1, 500);
  ui.sliding = true;
  ui.slide = at;
  std::vector<uint16_t> px(W * H);
  Canvas c{px.data(), W, H};
  PanelInput in{&f, LinkState::Live, "x", DeckScope::Act, 500, 0, &ui, 0};
  L.draw(c, in);
  assert(L.frameSplit() == nullptr);
  for (int y = rowFrom; y < rowTo; y++)
    for (int x = 0; x < W; x++) {
      const uint16_t want = x < W - shift ? a[y * W + x + shift] : b[y * W + x - (W - shift)];
      assert(px[y * W + x] == want);
    }
  // As a split: the same two pictures, nothing drawn on the canvas; another
  // offset uses the same pictures (they were drawn once).
  in.splitOk = true;
  L.draw(c, in);
  const FrameSplit* s = L.frameSplit();
  assert(s && s->a && s->b && s->shift == shift);
  const uint16_t *pa = s->a, *pb = s->b;
  for (int y = rowFrom; y < rowTo; y++) assert(memcmp(pa + y * W, a.data() + y * W, W * 2) == 0);
  ui.slide = at + 0.1f * (at - i0 < 0.5f ? 1 : -1);
  L.draw(c, in);
  s = L.frameSplit();
  assert(s && s->a == pa && s->b == pb && s->shift == (int)lroundf((ui.slide - i0) * W));
  // At rest again: drawn as usual.
  ui.sliding = false;
  L.draw(c, in);
  assert(L.frameSplit() == nullptr);
  (void)pb;
}

int main() {
  // ---- where released pages go
  {
    const int W = 480, commit = 34;
    assert(pagerTargetOf(1, 1 + 30.f / W, 0, 3, W, commit) == 1);    // short of the commit point: back
    assert(pagerTargetOf(1, 1 + 40.f / W, 0, 3, W, commit) == 2);    // past it: the next page
    assert(pagerTargetOf(1, 1 - 40.f / W, 0, 3, W, commit) == 0);    // and the other way
    assert(pagerTargetOf(1, 1 + 10.f / W, -0.8f, 3, W, commit) == 2);   // a flick to the left: next, however short
    assert(pagerTargetOf(1, 1 + 200.f / W, 0.8f, 3, W, commit) == 0);   // a flick back wins over the distance
    assert(pagerTargetOf(2, 2 + 100.f / W, 0, 3, W, commit) == 2);   // no page past the last
    assert(pagerTargetOf(0, -100.f / W, 0, 3, W, commit) == 0);      // nor before the first
    assert(pagerTargetOf(0, 0.9f, 0, 3, W, commit) == 1);            // one page at a time
    assert(pagerCommitPx(480, 480, 4.0f) == 33 && pagerCommitPx(368, 448, 1.8f) == 63);
  }

  // ---- the finger: one to one while down, then the release decides
  {
    static Attention att;
    att = Attention();
    const BigSpec S = specBig480();
    Fleet f;
    memset(&f, 0, sizeof f);
    f.agents[f.count++] = mk("a", 0, Status::Idle);
    f.agents[f.count++] = mk("b", 17, Status::Running);
    f.total = f.count;
    PanelUi ui;
    ui.att = &att;
    bigTick(ui, f, 0);
    assert(pagerCount(ui, bigPages(f)) == 3 && pagerIndex(ui) == 1);
    bigTouch(ui, S, f, true, 400, 240, DeckScope::Act, 100);
    for (int k = 1; k <= 10; k++) {                  // 15 px a sample to the left
      bigTouch(ui, S, f, true, 400 - 15 * k, 242, DeckScope::Act, 100 + 30 * k);
      if (15 * k > 14) assert(ui.sliding && fabsf(pagerAt(ui) - (1 + 15.f * k / S.w)) < 0.001f);
    }
    bigTouch(ui, S, f, true, 250, 242, DeckScope::Act, 600);   // held still, then let go: 150 px is past the commit point
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 610);
    assert(ui.page == 1 && ui.slideAnim && ui.slideTo == 2 && bigTick(ui, f, 620));
    for (uint32_t t = 620; ui.slideAnim; t += 16) bigTick(ui, f, t);
    assert(pagerAt(ui) == 2 && !bigTick(ui, f, 2000));
    // A short slow pull springs back.
    bigTouch(ui, S, f, true, 100, 240, DeckScope::Act, 3000);
    bigTouch(ui, S, f, true, 125, 240, DeckScope::Act, 3100);
    bigTouch(ui, S, f, true, 125, 240, DeckScope::Act, 3400);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 3410);
    assert(ui.page == 1 && ui.slideTo == 2);
  }

  // ---- the moving frame: the two pages at rest, side by side at the finger's offset
  {
    static Attention att;
    att = Attention();
    Fleet f;
    memset(&f, 0, sizeof f);
    f.agents[f.count++] = mk("a", 0, Status::Idle);
    f.agents[f.count++] = mk("w", 5, Status::Waiting);
    f.agents[f.count++] = mk("z", 20, Status::Running);
    f.total = f.count;
    PanelUi ui;
    ui.att = &att;
    attSync(att, f, 16, 0);
    const BigPanel B(specBig480());
    checkSlide(B, f, ui, 1.25f, 0, 480);             // page 1 to page 2
    checkSlide(B, f, ui, 0.6f, 0, 480);              // "Needs you" to page 1
    const SquarePanel G(specLcd4());
    checkSlide(G, f, ui, 1.4f, 0, 480);
    // A list board: "Needs you" and the list, below the header (which stays put) and clear of the corners.
    const SquareSpec A = specAmoled18();
    const SquarePanel P(A);
    PanelUi lu;
    lu.att = &att;
    checkSlide(P, f, lu, 0.3f, A.headerH, A.h - A.cornerR);
  }
  printf("pages: ok\n");
  return 0;
}
