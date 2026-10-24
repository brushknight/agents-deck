#pragma once
// Orbit, the 2.8″ round board's home: every agent is a critter on a ring
// near the edge, in deck slot order, and the agent that matters most sits in
// the middle. The rim under each critter is its status: a thin accent arc
// while it works (or is hungry, or failed), a thick one when it needs you,
// grey when idle. When anyone waits the rim gets an accent halo and the
// waiter sits on an accent disc; the middle then shows its prompt, one tap
// from answering. Past 16 agents the ring shows 15 and a "+N" chip, and a
// drag round the rim (or a tap on the chip) pages.
//
// Measurements: the design canvas, "round: Orbit 2.8″ + Dial 2.1″".
// The detail and the prompt are round_ui.h's.
#include "round_ui.h"

namespace orbit_detail {

constexpr int kPerPageMax = 16;   // all on one ring up to this many
constexpr int kPerPage = 15;      // beyond: 15 and the chip

// The ring of one page: which agents, and whether a "+N" chip follows.
struct Ring {
  int first, shown;    // f.agents[first .. first + shown)
  bool chip;
  int more;            // the chip's N: agents not on this page
  int slots;           // positions round the ring (at least 3)
  int scale;           // critters x3 up to 10 slots, else x2
  float radius;        // where the critters' bodies sit
};

static inline int pages(const Fleet& f) {
  if (f.count <= kPerPageMax && f.total <= kPerPageMax) return 1;
  return f.count ? (f.count + kPerPage - 1) / kPerPage : 1;
}

static inline Ring ring(const Fleet& f, int page) {
  Ring r{};
  const int np = pages(f);
  if (page >= np) page = np - 1;
  if (page < 0) page = 0;
  if (np == 1 && f.total <= kPerPageMax) {
    r.first = 0;
    r.shown = f.count;
    r.chip = false;
  } else {
    r.first = page * kPerPage;
    r.shown = f.count - r.first < kPerPage ? f.count - r.first : kPerPage;
    r.chip = true;
    r.more = (f.total > f.count ? f.total : f.count) - r.shown;
  }
  r.slots = r.shown + (r.chip ? 1 : 0);
  if (r.slots < 3) r.slots = 3;
  r.scale = r.slots <= 10 ? 3 : 2;
  r.radius = r.slots <= 10 ? 178 : 194;
  return r;
}

static inline float slotAngle(const Ring& r, int k) { return 360.f * k / r.slots; }

}  // namespace orbit_detail

struct OrbitLayout {
  RoundTheme T;
  DetailStyle detail;
};

static inline OrbitLayout layoutOrbit28() { return OrbitLayout{roundTheme(), detailOrbit()}; }

static inline void drawOrbitHome(Canvas& c, const OrbitLayout& L, const RoundInput& in) {
  using namespace orbit_detail;
  const RoundTheme& T = L.T;
  const Fleet& f = *in.fleet;
  const RoundUi& ui = *in.ui;
  const Ring r = ring(f, ui.page);
  const int feat = roundFeatured(ui, f);
  const float seg = 360.f / r.slots;

  bool anyWaiting = false;
  for (int i = 0; i < f.count; i++) anyWaiting |= f.agents[i].status == Status::Waiting;
  if (anyWaiting) strokeCircle(c, kRoundC, kRoundC, 238, 3, T.t.accent);   // the urgent halo

  for (int k = 0; k < r.shown; k++) {
    const int i = r.first + k;
    const Agent& a = f.agents[i];
    const float ang = slotAngle(r, k);
    const bool waiting = a.status == Status::Waiting;
    // The status arc under it.
    strokeArc(c, kRoundC, kRoundC, 229, waiting ? 14 : 5, ang - seg / 2 + 2.5f, seg - 5, statusColor(T, a));
    // The critter, on an accent disc when it waits, ringed when featured.
    const RPoint p = polar(kRoundC, kRoundC, r.radius, ang);
    const int s = r.scale;
    if (waiting) fillDisc(c, p.x, p.y, 12 * s, T.t.accent);
    if (i == feat) strokeCircle(c, p.x, p.y, 12 * s + 5, 2, T.t.ink);
    agentCritter(c, a, p.x, p.y, s, bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms, in.nowEpoch, false);
  }
  if (r.chip) {
    const RPoint p = polar(kRoundC, kRoundC, r.radius, slotAngle(r, r.slots - 1));
    fillDisc(c, p.x, p.y, 24, T.t.panel);
    strokeCircle(c, p.x, p.y, 23, 2, T.t.line);
    char buf[8];
    snprintf(buf, sizeof buf, "+%d", r.more);
    textOn(c, FONT_XXL, p.x, p.y + 9, buf, T.t.ink, 60);
  }

  // The middle.
  if (feat < 0) return;
  const Agent& a = f.agents[feat];
  const bool waiting = a.status == Status::Waiting;
  const int cy = r.slots <= 10 ? 192 : 196;
  if (waiting) fillDisc(c, 240, 192, 56, T.t.accent);
  agentCritter(c, a, 240, cy, 4, bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms, in.nowEpoch, !waiting);
  textOn(c, FONT_R30B, 240, 276, a.title[0] ? a.title : a.id, T.t.ink);
  char buf[96];
  if (waiting) {
    textOn(c, FONT_XXL, 240, 310, a.waitTitle[0] ? a.waitTitle : "needs you", T.t.accent);
    const int others = othersWaiting(f, feat);
    const char* verb = canAct(in.scope) ? "tap to answer" : "tap to see it";
    if (others) snprintf(buf, sizeof buf, "%s" DECK_DOT "+%d", verb, others);
    else snprintf(buf, sizeof buf, "%s", verb);
    textOn(c, FONT_XXL, 240, 342, buf, T.t.dim);
  } else {
    roundStateLine(a, in.nowEpoch, buf, sizeof buf);
    textOn(c, FONT_XXL, 240, 310, buf, T.t.dim);
  }
}

static inline RoundHit orbitHomeHit(const Fleet& f, const RoundUi& ui, int x, int y) {
  using namespace orbit_detail;
  RoundHit h;
  if (inCircle(kRoundC, kRoundC, 110, x, y)) {
    if (roundFeatured(ui, f) >= 0) h.kind = HitKind::Middle;
    return h;
  }
  const Ring r = ring(f, ui.page);
  const float hitR = 12.f * r.scale + 6;
  for (int k = 0; k < r.shown; k++) {
    const RPoint p = polar(kRoundC, kRoundC, r.radius, slotAngle(r, k));
    if (inCircle(p.x, p.y, hitR, x, y)) { h.kind = HitKind::Agent; h.agent = r.first + k; return h; }
  }
  if (r.chip) {
    const RPoint p = polar(kRoundC, kRoundC, r.radius, slotAngle(r, r.slots - 1));
    if (inCircle(p.x, p.y, 30, x, y)) h.kind = HitKind::More;
  }
  return h;
}

class OrbitPanel : public RoundLayout {
 public:
  explicit OrbitPanel(const OrbitLayout& l) : L(l) {}
  Kind kind() const override { return Kind::Orbit; }
  void draw(Canvas& c, const RoundInput& in) const override {
    if (!drawRoundCommon(c, L.T, L.detail, in)) drawOrbitHome(c, L, in);
    drawRoundOverlay(c, L.T, in);
  }
  RoundHit hit(const RoundUi& ui, const Fleet& f, int x, int y, RoundScope scope, int64_t) const override {
    if (ui.screen != RoundScreen::Home) return roundCommonHit(L.detail, ui, f, x, y, scope);
    return orbitHomeHit(f, ui, x, y);
  }
  int pages(const Fleet& f) const override { return orbit_detail::pages(f); }
  int pageOf(const Fleet& f, int agent) const override {
    (void)f;
    return orbit_detail::pages(f) > 1 ? agent / orbit_detail::kPerPage : 0;
  }
  float rimInner() const override { return 150; }
  const OrbitLayout L;
};
