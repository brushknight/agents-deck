#pragma once
// Dial, the 2.1″ round board's home: the orbit, reduced. One agent fills the
// face (the featured one first) with its name and state; every agent is a
// dot on a ring at the edge, and that ring is a slider: put a finger on it
// and slide, and the face follows the dot under it. Swipe left / right
// changes agent too. A small bar under the critter is its context use. When an agent starts waiting
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

constexpr float kTrackR = 212;   // the slider: the fleet as dots round the edge (no line drawn)

static inline RPoint dot(int i, int n) { return polar(kRoundC, kRoundC, kTrackR, n ? 360.f * i / n : 0); }

// The agent whose dot is nearest the angle.
// The home's dots: twice the detail's, so a finger finds them.
static inline float homeDotRadius(int n, bool waiting) { return 2 * dotRadius(n, waiting); }

static inline int dotAt(int n, float ang) {
  if (n <= 0) return -1;
  const float per = 360.f / n;
  return ((int)lroundf(normDeg(ang) / per)) % n;
}

// The face: a big critter above the centre, the context bar at the centre,
// the name and the state below.
constexpr int kDialCritterScale = 7, kDialCritterCy = 172, kDialBarY = 240, kDialNameBase = 292;

}  // namespace dial_detail

static inline void drawDialHome(Canvas& c, const DialLayout& L, const RoundInput& in) {
  using namespace dial_detail;
  const RoundTheme& T = L.T;
  const Fleet& f = *in.fleet;
  const int cur = roundFeatured(*in.ui, f);
  if (cur < 0) return;
  const Agent& a = f.agents[cur];
  const bool waiting = a.status == Status::Waiting;
  if (waiting) c.fill(T.t.accent);
  const uint16_t ink = waiting ? T.t.bg : T.t.ink, dim = waiting ? T.t.bg : T.t.dim;

  // The slider: a dot per agent round the edge; the shown one ringed (in
  // accent while a finger slides).
  for (int i = 0; i < f.count; i++) {
    const Agent& o = f.agents[i];
    const RPoint p = dot(i, f.count);
    const float r = homeDotRadius(f.count, o.status == Status::Waiting);
    fillDisc(c, p.x, p.y, r, waiting ? T.t.bg : statusColor(T, o));
    if (i == cur) strokeCircle(c, p.x, p.y, r + 5, 3, in.ui->sliding ? (waiting ? T.t.bg : T.t.accent) : ink);
  }

  // The agent: critter, a small context bar, the name, the state.
  agentCritter(c, a, 240, kDialCritterCy, kDialCritterScale, waiting ? T.t.bg : bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms,
               in.nowEpoch, true);
  if (a.ctxWindow) {
    const int w = 120, h = 6, x = 240 - w / 2, y = kDialBarY - h / 2;   // the bar at the very centre
    fillPill(c, x, y, w, h, waiting ? T.darkAccent : T.t.line);
    const int fw = w * ctxPct(a) / 100;
    if (fw >= h) fillPill(c, x, y, fw, h, waiting ? T.t.bg : T.t.ink);
  }
  auto fit = [](const Font& fo, int base) { return fitWidth(fo, base, 40); };
  textOn(c, FONT_R35, 240, kDialNameBase, a.title[0] ? a.title : a.id, ink, fit(FONT_R35, kDialNameBase));
  char buf[96];
  if (waiting) {
    textOn(c, FONT_R30, 240, kDialNameBase + 40, a.waitTitle[0] ? a.waitTitle : "needs you", ink,
           fit(FONT_R30, kDialNameBase + 40));
    textOn(c, FONT_XXL, 240, kDialNameBase + 74, canAct(in.scope) ? "tap to answer" : "tap to see it", ink,
           fit(FONT_XXL, kDialNameBase + 74));
  } else {
    roundStateLine(a, in.nowEpoch, buf, sizeof buf);
    textOn(c, FONT_XXL, 240, kDialNameBase + 38, buf, dim, fit(FONT_XXL, kDialNameBase + 38));
  }
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
  float rimInner() const override { return 175; }   // the slider: a finger on the outer ring
  int sliderAt(const Fleet& f, float ang) const override { return dial_detail::dotAt(f.count, ang); }
  const DialLayout L;
};
