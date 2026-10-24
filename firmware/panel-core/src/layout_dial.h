#pragma once
// Dial, the 2.1″ round board's home: the orbit, reduced. One agent fills the
// face (the featured one first), every agent is a dot on an inner ring, and
// the outer ring is the shown agent's context use. Swipe left / right to
// change agent; the dots show where you are (the owner's call: no rim drag
// and no dot taps for now). When an agent starts waiting
// the dial jumps to it and the whole face turns accent, like the deck's
// needs-you tile; a tap on the middle then opens its prompt.
//
// Measurements: the design canvas, "round: Orbit 2.8″ + Dial 2.1″".
// The detail and the prompt are round_ui.h's.
#include "round_ui.h"

struct DialLayout {
  RoundTheme T;
  DetailStyle detail;
};

static inline DialLayout layoutDial21() { return DialLayout{roundTheme(), detailDial()}; }

namespace dial_detail {

static inline RPoint dot(int i, int n) { return polar(kRoundC, kRoundC, 203, n ? 360.f * i / n : 0); }

}  // namespace dial_detail

static inline void drawDialHome(Canvas& c, const DialLayout& L, const RoundInput& in) {
  const RoundTheme& T = L.T;
  const Fleet& f = *in.fleet;
  const int cur = roundFeatured(*in.ui, f);
  if (cur < 0) return;
  const Agent& a = f.agents[cur];
  const bool waiting = a.status == Status::Waiting;
  const uint16_t face = waiting ? T.t.accent : T.t.bg;
  if (waiting) c.fill(face);

  // The context ring.
  strokeCircle(c, kRoundC, kRoundC, 228, 10, waiting ? T.darkAccent : T.t.line);
  const int pct = ctxPct(a);
  if (a.ctxWindow && pct > 0) strokeArc(c, kRoundC, kRoundC, 228, 10, 0, 3.6f * pct, waiting ? T.t.bg : T.t.ink);

  // The fleet as dots.
  for (int i = 0; i < f.count; i++) {
    const Agent& o = f.agents[i];
    const RPoint p = dial_detail::dot(i, f.count);
    const float r = dotRadius(f.count, o.status == Status::Waiting);
    fillDisc(c, p.x, p.y, r, waiting ? T.t.bg : statusColor(T, o));
    if (i == cur) strokeCircle(c, p.x, p.y, r + 5, 2, waiting ? T.t.bg : T.t.ink);
  }

  // The agent.
  agentCritter(c, a, 240, 172, 6, waiting ? T.t.bg : bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms,
               in.nowEpoch, true);
  const uint16_t ink = waiting ? T.t.bg : T.t.ink, dim = waiting ? T.t.bg : T.t.dim;
  // Text stays inside the ring of dots.
  auto fit = [](const Font& fo, int base) { return fitWidth(fo, base, 52); };
  textOn(c, FONT_R35, 240, 282, a.title[0] ? a.title : a.id, ink, fit(FONT_R35, 282));
  char buf[96];
  if (waiting) {
    textOn(c, FONT_R30, 240, 320, a.waitTitle[0] ? a.waitTitle : "needs you", ink, fit(FONT_R30, 320));
    textOn(c, FONT_XXL, 240, 354, canAct(in.scope) ? "tap to answer" : "tap to see it", ink, fit(FONT_XXL, 354));
  } else {
    statusLine(a, in.nowEpoch, buf, sizeof buf);
    textOn(c, FONT_XXL, 240, 320, buf, dim, fit(FONT_XXL, 320));
    roundActivity(a, buf, sizeof buf);
    textOn(c, FONT_XXL, 240, 354, buf, dim, fit(FONT_XXL, 354));
  }
  snprintf(buf, sizeof buf, "\xE2\x80\xB9  %d / %d  \xE2\x80\xBA", cur + 1, (int)f.count);
  if (textW(FONT_XXL, buf) > fitWidth(FONT_XXL, 414, 40))
    snprintf(buf, sizeof buf, "\xE2\x80\xB9 %d/%d \xE2\x80\xBA", cur + 1, (int)f.count);
  textOn(c, FONT_XXL, 240, 414, buf, dim, fitWidth(FONT_XXL, 414, 24));
}

static inline RoundHit dialHomeHit(const Fleet& f, const RoundUi& ui, int x, int y) {
  RoundHit h;
  if (inCircle(kRoundC, kRoundC, 150, x, y) && roundFeatured(ui, f) >= 0) h.kind = HitKind::Middle;
  return h;
}

class DialPanel : public RoundLayout {
 public:
  explicit DialPanel(const DialLayout& l) : L(l) {}
  Kind kind() const override { return Kind::Dial; }
  void draw(Canvas& c, const RoundInput& in) const override {
    if (!drawRoundCommon(c, L.T, L.detail, in)) drawDialHome(c, L, in);
    drawRoundOverlay(c, L.T, in);
  }
  RoundHit hit(const RoundUi& ui, const Fleet& f, int x, int y, RoundScope scope, int64_t) const override {
    if (ui.screen != RoundScreen::Home) return roundCommonHit(L.detail, ui, f, x, y, scope);
    return dialHomeHit(f, ui, x, y);
  }
  int pages(const Fleet&) const override { return 1; }
  int pageOf(const Fleet&, int) const override { return 0; }
  float rimInner() const override { return 1000; }   // no rim drag: swipes only
  const DialLayout L;
};
