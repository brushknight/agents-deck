#pragma once
// The spring carousel, the 2.1″ round board's home (the Dial's successor).
// The agents sit on a helix in deck slot order; the screen shows one turn of
// it: the selected agent large at 12 o'clock under a small pointer, the next
// six clockwise round the ring, a little smaller towards the end of the turn.
// The middle is the selected agent: its name, a phrase in plain words, its
// state and time, and its context bar. Past seven agents a short row of dots
// at 6 o'clock says where in the spring you are, and an orange arc on the
// rim points to a waiter you can't see. Geometry and motion: spring.h; touch:
// round_view.h. The detail and the prompt are round_ui.h's.
//
// A frame is a list of elements (the pointer, each critter, each line of the
// middle, the dots, the arc), each with its box. With RoundInput.partial the
// board keeps the last frame, and only the boxes that changed are cleared and
// drawn again (with everything that overlaps them); damage() lists them so
// the board copies only those to the panel. The critters animate, so their
// boxes are redrawn every frame; the middle only when its text changes.
#include "round_ui.h"
#include "spring.h"

struct SpringLayout {
  RoundTheme T;
  DetailStyle detail;
  SpringGeom G;
};

static inline SpringLayout layoutSpring21() { return SpringLayout{roundTheme(), detailDial(), springDial21()}; }

namespace spring_draw {

using spring_detail::iround;

enum class El : uint8_t { Notch, Critter, Name, Phrase, Meta, Bar, Dots, Arrow };

struct Elem {
  El kind;
  uint8_t slot;        // the middle: 0 the agent leaving the seam, 1 the one arriving
  int16_t agent;       // fleet index (critters and the middle)
  RoundRect box;       // everything it draws lies inside
  uint32_t sig;        // what it shows (the middle, the dots, the arc): redrawn when it changes
  // Critters.
  int16_t x, y;
  uint8_t scale, alpha, z;
  bool bubble;
  int8_t side;         // the arc: +1 right, -1 left
};

constexpr int kMaxElems = 40;

struct Frame {
  int n = 0;
  Elem e[kMaxElems];
};

// What the board shows when a turn is between agents: the selection position.
static inline float posOf(const RoundInput& in) { return springPos(in.ui->spring, in.ms); }

static inline uint32_t fnv(uint32_t h, const void* p, size_t n) {
  const uint8_t* b = (const uint8_t*)p;
  for (size_t i = 0; i < n; i++) h = (h ^ b[i]) * 16777619u;
  return h;
}
static inline uint32_t fnvStr(uint32_t h, const char* s) { return fnv(h, s, strlen(s)); }

static inline RoundRect rectOf(int x0, int y0, int x1, int y1) {
  if (x0 < 0) x0 = 0;
  if (y0 < 0) y0 = 0;
  if (x1 > kRoundW) x1 = kRoundW;
  if (y1 > kRoundH) y1 = kRoundH;
  if (x1 < x0) x1 = x0;
  if (y1 < y0) y1 = y0;
  return RoundRect{(int16_t)x0, (int16_t)y0, (int16_t)(x1 - x0), (int16_t)(y1 - y0)};
}

static inline bool overlaps(const RoundRect& a, const RoundRect& b) {
  return a.w > 0 && a.h > 0 && b.w > 0 && b.h > 0 && a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h &&
         b.y < a.y + a.h;
}

// A critter's body colour: orange while it works, needs you or is done
// unseen; ink when it failed; dim at rest.
static inline uint16_t body(const RoundTheme& T, const Agent& a) {
  switch (a.status) {
    case Status::Running:
    case Status::Waiting: return T.t.accent;
    case Status::Error: return T.t.ink;
    case Status::Idle: return a.unseen ? T.t.accent : T.t.dim;
    default: return T.t.dim;
  }
}

// One line in plain words under the name.
static inline const char* phrase(const Agent& a) {
  switch (a.status) {
    case Status::Running:  return a.actTool[0] ? "on it\xE2\x80\xA6" : "one moment\xE2\x80\xA6";
    case Status::Waiting:  return a.waitTitle[0] ? a.waitTitle : "needs you";
    case Status::Error:    return "hit an error";
    case Status::Idle:     return a.unseen ? "done" DECK_DOT "your turn" : "resting";
    case Status::Starting: return "starting\xE2\x80\xA6";
    case Status::Exited:   return a.lost ? "lost" : "ended";
    default:               return "";
  }
}

static inline bool loud(const Agent& a) { return a.status == Status::Running || a.status == Status::Waiting || hungry(a); }

// The ring's halo radius round a critter at scale s.
static inline int haloR(int s) { return 12 * s + 4; }

static inline RoundRect critterBox(int x, int y, int s, bool halo) {
  int x0 = x - 10 * s - 1, x1 = x + 12 * s + 1, y0 = y - 11 * s - 1, y1 = y + 6 * s + 1;
  if (halo) {
    const int r = haloR(s) + 10;
    if (x - r < x0) x0 = x - r;
    if (x + r > x1) x1 = x + r;
    if (y - r < y0) y0 = y - r;
    if (y + r > y1) y1 = y + r;
  }
  return rectOf(x0, y0, x1, y1);
}

// Fonts and widths of the middle's lines.
// The phrase: 30 px while it fits between the ring's side critters, else 26 px.
constexpr int kPhraseMax = 256;
static inline const Font& phraseFont(const char* s) { return textW(FONT_R30, s) <= 234 ? FONT_R30 : FONT_XXL; }
static inline int nameMax(const SpringGeom& G) {
  const int w = fitWidth(FONT_R35, G.nameBase, 40);
  return w < 340 ? w : 340;
}

static inline RoundRect textBox(const Font& f, int base, const char* s, int maxW) {
  int w = textW(f, s);
  if (w > maxW) w = maxW;
  const int top = base - fontAscent(f);
  return rectOf(240 - w / 2 - f.cellW, top - 1, 240 + w / 2 + f.cellW, top + f.cellH + 1);
}

// Past K agents: one dot per agent round 6 o'clock.
struct Dot {
  float x, y, r;
  uint16_t col;
};

static inline Dot dotOf(const SpringLayout& L, const Fleet& f, int j, int sel) {
  const SpringGeom& G = L.G;
  const int n = f.count;
  float spread = (n - 1) * 1.6f;
  if (spread > G.dotsSpread) spread = G.dotsSpread;
  const float ang = 180 + spread / 2 - (n > 1 ? j * spread / (n - 1) : 0);
  Dot d;
  springPolar(G.dotsR, ang, d.x, d.y);
  const bool inWin = ((j - sel) % n + n) % n < G.K;
  const bool w = f.agents[j].status == Status::Waiting;
  if (j == sel) { d.r = 4.5f; d.col = w ? L.T.t.accent : L.T.t.ink; }
  else if (w) { d.r = 3.5f; d.col = L.T.t.accent; }
  else if (inWin) { d.r = 2.4f; d.col = L.T.t.ink; }
  else { d.r = 1.6f; d.col = rgb565(0x5A, 0x57, 0x4F); }
  return d;
}

// The nearest waiter off the turn: which side of the rim to pull (+1 right, -1 left), 0 for none.
static inline int arrowSide(const SpringGeom& G, const Fleet& f, int sel) {
  const int n = f.count;
  if (n <= G.K || sel < 0) return 0;
  int best = 0;
  bool found = false;
  for (int j = 0; j < n; j++) {
    if (f.agents[j].status != Status::Waiting || ((j - sel) % n + n) % n < G.K) continue;
    const int d = springShortest(sel, j, n);
    if (!found || (d < 0 ? -d : d) < (best < 0 ? -best : best)) { best = d; found = true; }
  }
  return !found ? 0 : best > 0 ? 1 : -1;
}

constexpr float kArrowR = 229;

static inline RoundRect arrowBox(int side) {
  float x0 = 1e9f, y0 = 1e9f, x1 = -1e9f, y1 = -1e9f;
  for (int a = 8; a <= 44; a += 2) {
    float x, y;
    springPolar(kArrowR, (float)(a * side), x, y);
    if (x < x0) x0 = x;
    if (x > x1) x1 = x;
    if (y < y0) y0 = y;
    if (y > y1) y1 = y;
  }
  return rectOf((int)x0 - 16, (int)y0 - 16, (int)x1 + 17, (int)y1 + 17);
}

// ---------------------------------------------------------------- the frame's elements

static inline void push(Frame& fr, const Elem& e) {
  if (fr.n < kMaxElems) fr.e[fr.n++] = e;
}

static inline Elem elem(El k) {
  Elem e{};
  e.kind = k;
  e.agent = -1;
  return e;
}

// Every element of the home frame, in drawing order.
static inline void build(const SpringLayout& L, const RoundInput& in, Frame& fr) {
  const SpringGeom& G = L.G;
  const Fleet& f = *in.fleet;
  const int n = f.count;
  const float pos = posOf(in);
  const bool moving = springMoving(in.ui->spring, in.ms);
  fr.n = 0;

  Elem notch = elem(El::Notch);
  notch.box = rectOf(229, 1, 252, 16);
  push(fr, notch);

  // The critters, back to front.
  Elem cr[kMaxElems];
  int nc = 0;
  for (int j = 0; j < n && nc < 16; j++) {
    const float s = springSlot(G, j, pos, n);
    const SpringPlace p = springPlace(G, s, n);
    if (!p.shown || p.alpha <= 0) continue;
    Elem e = elem(El::Critter);
    e.agent = (int16_t)j;
    e.x = (int16_t)iround(p.x);
    e.y = (int16_t)iround(p.y);
    e.scale = (uint8_t)p.scale;
    e.alpha = (uint8_t)p.alpha;
    e.z = (uint8_t)p.z;
    e.bubble = !moving && (s < 1e-3f && s > -1e-3f);
    const Agent& a = f.agents[j];
    const bool halo = e.alpha >= 15 && e.z >= 2 && (a.status == Status::Waiting || hungry(a));
    e.box = critterBox(e.x, e.y, e.scale, halo);
    cr[nc++] = e;
  }
  for (int i = 1; i < nc; i++)   // insertion sort by (z, scale): stable, few items
    for (int k = i; k > 0 && (cr[k - 1].z > cr[k].z || (cr[k - 1].z == cr[k].z && cr[k - 1].scale > cr[k].scale)); k--) {
      const Elem t = cr[k];
      cr[k] = cr[k - 1];
      cr[k - 1] = t;
    }
  for (int i = 0; i < nc; i++) push(fr, cr[i]);

  // The middle: the agent leaving the seam fades out over the first half of a
  // step, the one arriving fades in over the second.
  const float base = floorf(pos), frac = pos - base;
  const int ids[2] = {springIndex(base, n), springIndex(base + 1, n)};
  const int alphas[2] = {spring_detail::q4alpha(1 - 2 * frac), spring_detail::q4alpha(2 * frac - 1)};
  char buf[96];
  for (int k = 0; k < 2; k++) {
    if (alphas[k] <= 0 || ids[k] < 0) continue;
    const Agent& a = f.agents[ids[k]];
    const uint32_t h0 = fnv(2166136261u, &alphas[k], sizeof alphas[k]);
    Elem e = elem(El::Name);
    e.slot = (uint8_t)k;
    e.agent = (int16_t)ids[k];
    e.alpha = (uint8_t)alphas[k];
    const char* title = a.title[0] ? a.title : a.id;
    e.box = textBox(FONT_R35, G.nameBase, title, nameMax(G));
    e.sig = fnvStr(h0, title);
    push(fr, e);
    e.kind = El::Phrase;
    const char* ph = phrase(a);
    const Font& pf = phraseFont(ph);
    e.box = textBox(pf, G.phraseBase, ph, kPhraseMax);
    e.sig = fnvStr(h0 ^ (loud(a) ? 1u : 0u), ph);
    push(fr, e);
    e.kind = El::Meta;
    roundStateLine(a, in.nowEpoch, buf, sizeof buf);
    e.box = textBox(FONT_XXL, G.metaBase, buf, 260);
    e.sig = fnvStr(h0, buf);
    push(fr, e);
    if (a.ctxWindow) {
      e.kind = El::Bar;
      e.box = rectOf(240 - G.barW / 2 - 2, G.barY - 5, 240 + G.barW / 2 + 2, G.barY + 5);
      const int pct = ctxPct(a);
      e.sig = fnv(h0, &pct, sizeof pct);
      push(fr, e);
    }
  }

  const int sel = springIndex(pos, n);
  if (n > G.K) {
    Elem e = elem(El::Dots);
    e.agent = (int16_t)sel;
    float x0 = 1e9f, x1 = -1e9f, y0 = 1e9f, y1 = -1e9f;
    uint32_t h = 2166136261u;
    for (int j = 0; j < n; j++) {
      const Dot d = dotOf(L, f, j, sel);
      if (d.x - d.r < x0) x0 = d.x - d.r;
      if (d.x + d.r > x1) x1 = d.x + d.r;
      if (d.y - d.r < y0) y0 = d.y - d.r;
      if (d.y + d.r > y1) y1 = d.y + d.r;
      h = fnv(h, &d, sizeof d);
    }
    e.box = rectOf((int)x0 - 2, (int)y0 - 2, (int)x1 + 3, (int)y1 + 3);
    e.sig = h;
    push(fr, e);
  }
  const int side = arrowSide(G, f, springIndex(in.ui->spring.to, n));
  if (side) {
    Elem e = elem(El::Arrow);
    e.side = (int8_t)side;
    e.sig = (uint32_t)(side + 2);
    e.box = arrowBox(side);
    push(fr, e);
  }
}

// ---------------------------------------------------------------- drawing one element

// A filled triangle (no anti-aliasing: the pointer and the arrowhead are small and sharp).
static inline void fillTri(Canvas& c, float ax, float ay, float bx, float by, float cx, float cy, uint16_t col) {
  int x0 = (int)floorf(fminf(ax, fminf(bx, cx))), x1 = (int)ceilf(fmaxf(ax, fmaxf(bx, cx)));
  int y0 = (int)floorf(fminf(ay, fminf(by, cy))), y1 = (int)ceilf(fmaxf(ay, fmaxf(by, cy)));
  auto edge = [](float px, float py, float qx, float qy, float x, float y) { return (qx - px) * (y - py) - (qy - py) * (x - px); };
  const float area = edge(ax, ay, bx, by, cx, cy);
  if (area == 0) return;
  for (int y = y0; y <= y1; y++)
    for (int x = x0; x <= x1; x++) {
      const float px = x + 0.5f, py = y + 0.5f;
      const float w0 = edge(bx, by, cx, cy, px, py), w1 = edge(cx, cy, ax, ay, px, py), w2 = edge(ax, ay, bx, by, px, py);
      if ((area > 0 && w0 >= 0 && w1 >= 0 && w2 >= 0) || (area < 0 && w0 <= 0 && w1 <= 0 && w2 <= 0)) c.rect(x, y, 1, 1, col);
    }
}

// A critter at a 4-bit opacity: drawn into a small buffer, then mixed in.
static inline void fadedCritter(Canvas& c, const Agent& a, int cx, int cy, int s, uint16_t col, uint16_t cut, uint32_t ms,
                                int64_t now, int alpha) {
  constexpr int kMaxS = 6, kW = 22 * kMaxS, kH = 17 * kMaxS;
  constexpr uint16_t kKey = 0xF81F;   // magenta: in no palette
  static uint16_t buf[kW * kH];
  if (s > kMaxS) { agentCritter(c, a, cx, cy, s, col, cut, ms, now, false); return; }
  Canvas t{buf, 22 * s, 17 * s};
  t.fill(kKey);
  agentCritter(t, a, 10 * s, 11 * s, s, col, cut, ms, now, false);
  const int ox = cx - 10 * s, oy = cy - 11 * s;
  for (int y = 0; y < t.h; y++) {
    const int yy = oy + y;
    if (yy < 0 || yy >= c.h) continue;
    for (int x = 0; x < t.w; x++) {
      const uint16_t p = buf[y * t.w + x];
      const int xx = ox + x;
      if (p == kKey || xx < 0 || xx >= c.w) continue;
      uint16_t& d = c.px[yy * c.w + xx];
      d = blend565(d, p, (uint8_t)alpha);
    }
  }
}

static inline void drawElem(Canvas& c, const SpringLayout& L, const RoundInput& in, const Elem& e) {
  const RoundTheme& T = L.T;
  const SpringGeom& G = L.G;
  const Fleet& f = *in.fleet;
  switch (e.kind) {
    case El::Notch:
      fillTri(c, 231, 3, 249, 3, 240, 14, T.t.ink);
      return;
    case El::Critter: {
      const Agent& a = f.agents[e.agent];
      const int s = e.scale;
      if (e.alpha >= 15 && e.z >= 2) {
        const float r = (float)haloR(s);
        if (a.status == Status::Waiting) {
          strokeCircle(c, e.x, e.y, r + 7, 2, blend565(T.t.bg, T.t.accent, 5));   // the 35 % ring
          strokeCircle(c, e.x, e.y, r, 4, T.t.accent);
        } else if (hungry(a)) {
          const float circ = 2 * kRoundPi * r;
          const int count = (int)(circ / 11);
          dashedCircle(c, e.x, e.y, r, 3, count, 360.f * 5 / circ, T.t.accent);
        }
      }
      if (e.alpha >= 15) agentCritter(c, a, e.x, e.y, s, body(T, a), T.t.bg, in.ms, in.nowEpoch, e.bubble);
      else fadedCritter(c, a, e.x, e.y, s, body(T, a), T.t.bg, in.ms, in.nowEpoch, e.alpha);
      return;
    }
    case El::Name: {
      const Agent& a = f.agents[e.agent];
      textOn(c, FONT_R35, 240, G.nameBase, a.title[0] ? a.title : a.id, blend565(T.t.bg, T.t.ink, e.alpha), nameMax(G));
      return;
    }
    case El::Phrase: {
      const Agent& a = f.agents[e.agent];
      const char* ph = phrase(a);
      textOn(c, phraseFont(ph), 240, G.phraseBase, ph, blend565(T.t.bg, loud(a) ? T.t.accent : T.t.dim, e.alpha), kPhraseMax);
      return;
    }
    case El::Meta: {
      char buf[96];
      roundStateLine(f.agents[e.agent], in.nowEpoch, buf, sizeof buf);
      textOn(c, FONT_XXL, 240, G.metaBase, buf, blend565(T.t.bg, T.t.dim, e.alpha), 260);
      return;
    }
    case El::Bar: {
      const Agent& a = f.agents[e.agent];
      const int w = G.barW, x = 240 - w / 2, y = G.barY - 3;
      fillPill(c, x, y, w, 6, blend565(T.t.bg, T.t.line, e.alpha));
      int fw = w * ctxPct(a) / 100;
      if (fw < 6) fw = 6;
      fillPill(c, x, y, fw, 6, blend565(T.t.bg, T.t.ink, e.alpha));
      return;
    }
    case El::Dots:
      for (int j = 0; j < f.count; j++) {
        const Dot d = dotOf(L, f, j, e.agent);
        fillDisc(c, d.x, d.y, d.r, d.col);
      }
      return;
    case El::Arrow: {
      const int side = e.side;
      strokeArc(c, kRoundC, kRoundC, kArrowR, 4, side > 0 ? 12 : 320, 28, T.t.accent);
      const float t = (12.f * side - 90) * kRoundPi / 180;
      float x1, y1;
      springPolar(kArrowR, 12.f * side, x1, y1);
      const float tx = side < 0 ? -sinf(t) : sinf(t), ty = side < 0 ? cosf(t) : -cosf(t);
      const float nx = cosf(t), ny = sinf(t);
      fillTri(c, x1 + tx * 12, y1 + ty * 12, x1 + nx * 8, y1 + ny * 8, x1 - nx * 8, y1 - ny * 8, T.t.accent);
      return;
    }
  }
}

// The panel's round mask on part of the frame: the rects that reach past
// the edge, each pixel once (a second pass would darken the edge).
static inline void maskRects(Canvas& c, const RoundRect* r, int n) {
  const float R = 240.f, in2 = (R - 0.5f) * (R - 0.5f);
  int y0 = kRoundH, y1 = 0;
  RoundRect edge[kRoundMaxDamage];
  int ne = 0;
  for (int i = 0; i < n; i++) {
    bool inside = true;
    for (int k = 0; k < 4 && inside; k++) {
      const float dx = (k & 1 ? r[i].x + r[i].w : r[i].x) - kRoundC, dy = (k & 2 ? r[i].y + r[i].h : r[i].y) - kRoundC;
      inside = dx * dx + dy * dy < in2;
    }
    if (inside) continue;
    edge[ne++] = r[i];
    if (r[i].y < y0) y0 = r[i].y;
    if (r[i].y + r[i].h > y1) y1 = r[i].y + r[i].h;
  }
  int16_t span[kRoundMaxDamage][2];
  for (int y = y0; y < y1; y++) {
    const float dy = y + 0.5f - kRoundC;
    const int ns = roundRowSpans(edge, ne, y, span, kRoundMaxDamage);
    for (int s = 0; s < ns; s++)
      for (int x = span[s][0]; x < span[s][1]; x++) {
        const float dx = x + 0.5f - kRoundC;
        const float d2 = dx * dx + dy * dy;
        if (d2 < in2) continue;
        const float out = sqrtf(d2) - (R - 0.5f);
        const int a = out >= 1 ? 15 : (int)(out * 15.f + 0.5f);
        if (a > 0) {
          uint16_t& p = c.px[y * c.w + x];
          p = blend565(p, 0, (uint8_t)a);
        }
      }
  }
}

// Whether two frames' elements are the same thing (to compare what they show).
static inline bool same(const Elem& a, const Elem& b) {
  return a.kind == b.kind && a.slot == b.slot && a.agent == b.agent;
}

}  // namespace spring_draw

class SpringPanel : public RoundLayout {
 public:
  explicit SpringPanel(const SpringLayout& l) : L(l) {}
  Kind kind() const override { return Kind::Spring; }
  const SpringGeom* spring() const override { return &L.G; }
  void damage(RoundDamage& d) const override { d = dmg_; }

  void draw(Canvas& c, const RoundInput& in) const override {
    using namespace spring_draw;
    const bool home = in.link == LinkState::Live && in.fleet && in.fleet->count > 0 && in.ui &&
                      in.ui->screen == RoundScreen::Home;
    const bool toast = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0;
    dmg_.full = true;
    dmg_.n = 0;
    if (!home || toast) {
      if (!drawRoundCommon(c, L.T, L.detail, in)) drawHome(c, in);
      drawRoundOverlay(c, L.T, in);
      haveLast_ = false;
      return;
    }
    Frame& cur = frames_[which_ ^ 1];
    build(L, in, cur);
    if (!in.partial || !haveLast_ || !drawPartial(c, in, frames_[which_], cur)) {
      c.fill(L.T.t.bg);
      for (int i = 0; i < cur.n; i++) drawElem(c, L, in, cur.e[i]);
      drawRoundOverlay(c, L.T, in);
      dmg_.full = true;
      dmg_.n = 0;
    }
    which_ ^= 1;
    haveLast_ = true;
  }

  RoundHit hit(const RoundUi& ui, const Fleet& f, int x, int y, RoundScope scope, int64_t) const override {
    if (ui.screen != RoundScreen::Home) return roundCommonHit(L.detail, ui, f, x, y, scope);
    RoundHit h;
    const int n = f.count;
    if (!n) return h;
    // Where the ring is going (a tap mid-turn means where it will stop).
    const float pos = ui.spring.to;
    int best = -1;
    float bestD = 1e9f;
    for (int j = 0; j < n; j++) {
      const float s = springSlot(L.G, j, pos, n);
      const SpringPlace p = springPlace(L.G, s, n);
      if (!p.shown || p.alpha < 15) continue;
      const float r = 12.f * p.scale + 6, dx = x - p.x, dy = y - p.y, d = dx * dx + dy * dy;
      if (d <= r * r && d < bestD) { best = j; bestD = d; }
    }
    if (best >= 0) {
      if (best == springIndex(pos, n)) h.kind = HitKind::Middle;   // the selected one
      else { h.kind = HitKind::Agent; h.agent = best; }
      return h;
    }
    if (inCircle(kRoundC, kRoundC, 130, x, y)) h.kind = HitKind::Middle;
    return h;
  }
  int pages(const Fleet&) const override { return 1; }
  int pageOf(const Fleet&, int) const override { return 0; }
  float rimInner() const override { return 150; }   // a drag round the ring starts outside the middle

  const SpringLayout L;

 private:
  void drawHome(Canvas& c, const RoundInput& in) const {
    spring_draw::Frame& fr = frames_[which_ ^ 1];
    spring_draw::build(L, in, fr);
    for (int i = 0; i < fr.n; i++) spring_draw::drawElem(c, L, in, fr.e[i]);
  }

  // Redraws only what changed since `prev`. False when it can't (too much changed): draw it all.
  bool drawPartial(Canvas& c, const RoundInput& in, const spring_draw::Frame& prev, const spring_draw::Frame& cur) const {
    using namespace spring_draw;
    RoundDamage& d = dmg_;
    d.full = false;
    d.n = 0;
    bool dirty[kMaxElems] = {};
    auto add = [&](const RoundRect& r) {
      if (r.w <= 0 || r.h <= 0) return true;
      if (d.n >= kRoundMaxDamage) return false;
      d.r[d.n++] = r;
      return true;
    };
    // Critters move or animate: theirs, old and new, always. The rest when what they show changed.
    for (int i = 0; i < prev.n; i++) {
      const Elem& p = prev.e[i];
      bool keep = false;
      if (p.kind != El::Critter)
        for (int k = 0; k < cur.n; k++)
          if (same(p, cur.e[k]) && p.sig == cur.e[k].sig && !memcmp(&p.box, &cur.e[k].box, sizeof p.box)) keep = true;
      if (!keep && !add(p.box)) return false;
    }
    for (int k = 0; k < cur.n; k++) {
      const Elem& e = cur.e[k];
      bool keep = false;
      if (e.kind != El::Critter)
        for (int i = 0; i < prev.n; i++)
          if (same(e, prev.e[i]) && e.sig == prev.e[i].sig && !memcmp(&e.box, &prev.e[i].box, sizeof e.box)) keep = true;
      if (!keep) {
        dirty[k] = true;
        if (!add(e.box)) return false;
      }
    }
    // Whatever overlaps a cleared box is drawn again whole.
    for (bool grew = true; grew;) {
      grew = false;
      for (int k = 0; k < cur.n; k++) {
        if (dirty[k]) continue;
        for (int i = 0; i < d.n; i++)
          if (overlaps(cur.e[k].box, d.r[i])) {
            dirty[k] = true;
            if (!add(cur.e[k].box)) return false;
            grew = true;
            break;
          }
      }
    }
    for (int i = 0; i < d.n; i++) c.rect(d.r[i].x, d.r[i].y, d.r[i].w, d.r[i].h, L.T.t.bg);
    for (int k = 0; k < cur.n; k++)
      if (dirty[k]) drawElem(c, L, in, cur.e[k]);
    maskRects(c, d.r, d.n);
    return true;
  }

  mutable spring_draw::Frame frames_[2];
  mutable int which_ = 0;            // frames_[which_] is the last frame drawn
  mutable bool haveLast_ = false;
  mutable RoundDamage dmg_;
};
