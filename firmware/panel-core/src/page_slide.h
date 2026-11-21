#pragma once
// Home's pages sliding sideways under the finger, on every grid and list
// home: the "Needs you" screen (when the board has it) and then each page.
// While a finger is down the pages follow it one to one; on release they
// carry on to the neighbour page when the finger went past a commit point
// (kControlCommitMm on the glass, as the board's panel does) or flicked that
// way, and otherwise spring back.
//
// The pages are counted together: index 0 is "Needs you" when there is one,
// then the pages in order (a list home has one page). PanelUi::slide is where
// they are, in pages, while they move (sliding, slideAnim).
//
// PageSlide draws the two pages either side of where they are once each, into
// two kept pictures, and every moving frame is cut from them: on the canvas
// (two pieces of each row), or handed to a board that composes the rows as
// it sends them (frameSplit). Pure C++ (no Arduino).
#include <math.h>
#include <stdlib.h>
#include <string.h>

#include "canvas.h"
#include "panel_layout.h"

constexpr uint32_t kPageSettleMs = 260;
constexpr float kPageFlick = 0.5f;               // px per ms: a flick, whatever the distance

static inline int pagerCount(const PanelUi& ui, int pages) { return (ui.att ? 1 : 0) + (pages > 0 ? pages : 1); }
static inline int pagerIndex(const PanelUi& ui) { return ui.att && ui.needsYou ? 0 : (ui.att ? 1 : 0) + ui.page; }
static inline bool pagerMoving(const PanelUi& ui) { return ui.sliding || ui.slideAnim; }

// Where the pages are now, in pages.
static inline float pagerAt(const PanelUi& ui) { return pagerMoving(ui) ? ui.slide : (float)pagerIndex(ui); }

// Shows page index i at once.
static inline void pagerGo(PanelUi& ui, int i) {
  if (ui.att && i == 0) {
    ui.needsYou = true;
    return;
  }
  ui.needsYou = false;
  ui.page = i - (ui.att ? 1 : 0);
}

// The commit point on a glass of w x h pixels and diagIn inches (0: unknown, a fifth of the width).
static inline int pagerCommitPx(int w, int h, float diagIn) {
  const float ppi = diagIn > 0 ? sqrtf((float)(w * w + h * h)) / diagIn : 0;
  return ppi > 0 ? (int)lroundf(kControlCommitMm / 25.4f * ppi) : w / 5;
}

// A finger starts sliding the pages (it moved clearly sideways): they go on from where they are.
static inline void pagerGrab(PanelUi& ui, int x, uint32_t ms) {
  ui.slide0 = pagerAt(ui);
  ui.slide = ui.slide0;
  ui.sliding = true;
  ui.slideAnim = false;
  ui.velocity = 0;
  ui.lastX = x;
  ui.lastMs = ms;
}

// The finger at dx from where it went down: the pages under it, one to one, with a little give past the ends.
static inline void pagerFollow(PanelUi& ui, int pages, int w, int dx, int x, uint32_t ms) {
  const float last = (float)(pagerCount(ui, pages) - 1);
  float p = ui.slide0 - (float)dx / (float)w;
  if (p < 0) p *= 0.35f;
  if (p > last) p = last + (p - last) * 0.35f;
  ui.slide = p;
  const uint32_t dt = ms - ui.lastMs;
  if (dt > 0) {
    const float v = (float)(x - ui.lastX) / (float)dt;
    ui.velocity = ui.velocity * 0.4f + v * 0.6f;
    ui.lastX = x;
    ui.lastMs = ms;
  }
}

// Where released pages go: the neighbour the finger went past the commit point
// (or flicked) towards, else back to the page they started from.
static inline int pagerTargetOf(float from, float at, float velocity, int count, int w, int commitPx) {
  const int start = (int)lroundf(from);
  int to = start;
  const float moved = (at - from) * (float)w;    // pixels; positive: towards the next page (finger to the left)
  if (velocity < -kPageFlick || (velocity <= kPageFlick && moved > (float)commitPx)) to = start + 1;
  else if (velocity > kPageFlick || moved < -(float)commitPx) to = start - 1;
  if (to < 0) to = 0;
  if (to > count - 1) to = count - 1;
  return to;
}

// The finger lifted: the page is chosen now (code that reads ui.page or
// ui.needsYou sees it at once); the slide only shows the way there.
static inline void pagerRelease(PanelUi& ui, int pages, int w, int commitPx, uint32_t ms) {
  ui.sliding = false;
  if (ms - ui.lastMs > 80) ui.velocity = 0;      // it stopped before letting go
  const int to = pagerTargetOf(ui.slide0, ui.slide, ui.velocity, pagerCount(ui, pages), w, commitPx);
  pagerGo(ui, to);
  ui.slideFrom = ui.slide;
  ui.slideTo = (float)to;
  ui.slideStart = ms;
  ui.slideAnim = true;
}

// The settle animation. Over, or the page was changed under it (BOOT, the deck): that page at once.
static inline void pagerTick(PanelUi& ui, uint32_t ms) {
  if (!ui.slideAnim) return;
  const uint32_t t = ms - ui.slideStart;
  if (t >= kPageSettleMs || ui.slideTo != (float)pagerIndex(ui)) {
    ui.slideAnim = false;
    return;
  }
  const float k = (float)t / (float)kPageSettleMs, e = 1 - (1 - k) * (1 - k) * (1 - k);
  ui.slide = ui.slideFrom + (ui.slideTo - ui.slideFrom) * e;
}

struct PageSlide {
  PageSlide() = default;
  PageSlide(const PageSlide&) = delete;
  PageSlide& operator=(const PageSlide&) = delete;
  ~PageSlide() {
    free(pic[0]);
    free(pic[1]);
  }

  // Not moving: the pictures are out of date.
  void reset() {
    key[0] = key[1] = -1000;
    ready = false;
  }

  // After draw(): the frame as two pictures for a board that composes them, or nullptr.
  const FrameSplit* split() const { return ready ? &out : nullptr; }

  // A moving frame with the pages at `at` (in pages, between 0 and count - 1,
  // a little past while pulled). drawPage(c, i) draws page index i whole on c.
  // splitOk: the board composes the rows itself (split()); else the frame goes
  // on c. False: no room for the pictures (draw the frame instead).
  template <class Draw>
  bool slide(Canvas& c, float at, int count, bool splitOk, uint16_t fill, Draw drawPage) {
    ready = false;
    const int W = c.w, H = c.h;
    const size_t n = (size_t)W * H;
    if (c.top != 0) return false;
    if (W != w || H != h) {
      free(pic[0]);
      free(pic[1]);
      pic[0] = pic[1] = nullptr;
      reset();
      w = W;
      h = H;
    }
    for (int k = 0; k < 2; k++)
      if (!pic[k] && !(pic[k] = (uint16_t*)malloc(n * 2))) return false;
    const int i0 = (int)floorf(at);
    float frac = at - (float)i0;
    int shift = (int)lroundf(frac * (float)W);
    int left = i0, right = i0 + 1;
    if (shift >= W) { left++; right++; shift = 0; }
    const uint16_t* pa = left >= 0 && left < count ? picture(c, left, drawPage) : nullptr;
    const uint16_t* pb = shift > 0 && right >= 0 && right < count ? picture(c, right, drawPage) : nullptr;
    if (left >= 0 && left < count && !pa) return false;
    out = FrameSplit{pa, pb, shift, c.enc(fill)};
    if (splitOk) {
      ready = true;
      return true;
    }
    compose(c.px, W, H, out);
    return true;
  }

  // Two pictures into one frame, row by row.
  static void compose(uint16_t* dst, int W, int H, const FrameSplit& f) {
    const int na = W - f.shift;
    for (int y = 0; y < H; y++) {
      uint16_t* row = dst + (size_t)y * W;
      if (f.a) memcpy(row, f.a + (size_t)y * W + f.shift, (size_t)na * 2);
      else Canvas::run(row, na, f.fill);
      if (f.shift > 0) {
        if (f.b) memcpy(row + na, f.b + (size_t)y * W, (size_t)f.shift * 2);
        else Canvas::run(row + na, f.shift, f.fill);
      }
    }
  }

 private:
  template <class Draw>
  const uint16_t* picture(Canvas& c, int index, Draw drawPage) {
    for (int k = 0; k < 2; k++)
      if (key[k] == index) {
        used[k] = ++clock;
        return pic[k];
      }
    const int k = used[0] <= used[1] ? 0 : 1;        // the one used longest ago
    Canvas pc = c;
    pc.px = pic[k];
    pc.top = 0;
    drawPage(pc, index);
    key[k] = index;
    used[k] = ++clock;
    return pic[k];
  }

  uint16_t* pic[2] = {nullptr, nullptr};
  int key[2] = {-1000, -1000};
  uint32_t used[2] = {0, 0};
  uint32_t clock = 0;
  int w = 0, h = 0;
  FrameSplit out{};
  bool ready = false;
};
