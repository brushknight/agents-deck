#pragma once
// What the Orbit and the Dial share: the palette, critters placed by their
// body centre, text set on a baseline and fitted to the circle, the full
// screen notices (connecting, waiting for approval, deck offline, no
// agents), an agent's detail with its round buttons, and the prompt with its
// answers as pills. Measurements follow the design canvas "round: Orbit 2.8″
// + Dial 2.1″" (every number in px on the 480 x 480 panel; angles clockwise
// from 12 o'clock), with the owner's changes: the detail's context bar is a
// partial arc over the top with the buttons on an arc under the text (V1),
// and the prompt's answers are stacked pills (P1).
//
// The detail's buttons and the prompt's pills each live in one place
// (DetailStyle / detailButtons() and promptPills()) so a different variation
// can replace them without touching the rest.
//
// Pure C++: runs on the host (tests, sim/render) as on the board.
#include <ctype.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "deck_ui.h"
#include "font_data.h"
#include "font_round.h"
#include "model.h"
#include "round_geom.h"
#include "ui_buttons.h"
#include "round_view.h"
#include "sprites.h"
#include "view.h"

constexpr int kRoundW = 480, kRoundH = 480;
constexpr float kRoundC = 240.f;          // the centre, both axes

// ---------------------------------------------------------------- palette

struct RoundTheme {
  Theme t;              // bg, panel, ink, dim, accent, line …
  uint16_t idle;        // #3A3934: idle arcs and dots
  uint16_t darkAccent;  // #C23A00: the dial's ring track on an orange face
};

static inline RoundTheme roundTheme() {
  RoundTheme r;
  r.t = deckTheme(false);
  r.t.line = rgb565(0x2A, 0x29, 0x25);
  r.idle = rgb565(0x3A, 0x39, 0x34);
  r.darkAccent = rgb565(0xC2, 0x3A, 0x00);
  return r;
}

// The status colour of an arc or a dot on home: working, hungry, waiting and
// (the owner's call) errors in accent; idle, starting and exited grey.
static inline uint16_t statusColor(const RoundTheme& T, const Agent& a) {
  switch (a.status) {
    case Status::Running:
    case Status::Waiting:
    case Status::Error: return T.t.accent;
    case Status::Idle: return a.unseen ? T.t.accent : T.idle;
    default: return T.idle;
  }
}

// A critter's body colour on the dark background.
static inline uint16_t bodyColor(const RoundTheme& T, const Agent& a) {
  switch (a.status) {
    case Status::Running:
    case Status::Error: return T.t.accent;
    case Status::Waiting: return T.t.bg;      // drawn on an accent disc
    case Status::Idle: return a.unseen ? T.t.accent : T.t.dim;
    default: return T.t.dim;
  }
}

// ---------------------------------------------------------------- critters and text

// A critter by its body centre (cx, cy) at scale s: the canvas's 22 x 17
// cell box (cells x -3..19, y -8..9) sits at (cx - 10s, cy - 11s).
static inline void critterAt(Canvas& c, int cx, int cy, int s, Species sp, Pose pose, uint16_t body, uint16_t cut,
                             uint32_t ms, bool bubble, uint8_t hat = 0, uint8_t hatColor = 0) {
  drawSprite(c, cx - 7 * s, cy - 7 * s, s, sp, pose, body, cut, ms, bubble, hat, hatColor);
}

static inline void agentCritter(Canvas& c, const Agent& a, int cx, int cy, int s, uint16_t body, uint16_t cut,
                                uint32_t ms, int64_t now, bool bubble) {
  critterAt(c, cx, cy, s, a.species, poseOf(a, now), body, cut, ms, bubble, a.hat, a.hatColor);
}

static inline int fontAscent(const Font& f) { return (f.cellH * 795 + 500) / 1000; }   // Roboto Mono: 1.048 of 1.319 em

// The text width that fits inside the circle (radius 240 - pad) on the line
// whose baseline is `base`: from the capitals' top to half the descender.
static inline int fitWidth(const Font& f, int base, int pad = 14) {
  const float size = f.cellW / 0.6f;   // Roboto Mono advances 0.6 em
  const float top = base - 0.74f * size, bottom = base + 0.12f * size;
  const float half = chordHalfSpan(kRoundC, kRoundC - pad, top, bottom);
  return (int)(2 * half);
}

// Centred text on a baseline, cut with "…" to `maxW` (default: what fits the circle).
static inline void textOn(Canvas& c, const Font& f, int cx, int base, const char* s, uint16_t col, int maxW = -1) {
  if (maxW < 0) maxW = fitWidth(f, base);
  if (maxW <= 0) return;
  textCentered(c, f, cx, base - fontAscent(f), s, col, maxW);
}

// Text in at most `maxLines` centred lines on baselines base, base + step …,
// broken after spaces and dashes so each line fits the circle where it sits.
// Returns the lines drawn.
static inline int textLinesOn(Canvas& c, const Font& f, int base, int step, const char* s, uint16_t col,
                              int maxLines, int pad = 14) {
  int lines = 0;
  const char* p = s;
  char buf[128];
  while (*p && lines < maxLines) {
    while (*p == ' ') p++;
    if (!*p) break;
    const int y = base + lines * step;
    const int maxW = fitWidth(f, y, pad);
    const int per = maxW / f.cellW;
    if (per <= 0) break;
    const char* q = p;
    const char* lastBreak = nullptr;
    int n = 0;
    while (*q && n < per) {
      if (*q == ' ') lastBreak = q;
      else if (*q == '-' && n + 1 < per) lastBreak = q + 1;   // keep the dash on this line
      utf8Next(q);
      n++;
    }
    const char* end = q;
    if (*q && lastBreak && lastBreak > p && lines < maxLines - 1) end = lastBreak;
    if (lines == maxLines - 1) end = p + strlen(p);   // the last line: textOn() adds the ellipsis
    size_t len = (size_t)(end - p);
    if (len >= sizeof buf) len = sizeof buf - 1;
    memcpy(buf, p, len);
    buf[len] = '\0';
    while (len && buf[len - 1] == ' ') buf[--len] = '\0';
    textOn(c, f, (int)kRoundC, y, buf, col, maxW);
    lines++;
    p = end;
  }
  return lines;
}

// "edit · session.ts", "bash · go test ./...", "thinking": what a running agent does.
static inline void roundActivity(const Agent& a, char* out, size_t cap) {
  out[0] = '\0';
  if (a.status == Status::Running) {
    if (!a.actTool[0]) { snprintf(out, cap, "thinking"); return; }
    char verb[16];
    size_t i = 0;
    for (; a.actTool[i] && i < sizeof verb - 1; i++) verb[i] = (char)tolower((unsigned char)a.actTool[i]);
    verb[i] = '\0';
    const char* v = verb;
    if (!strcmp(verb, "todowrite")) v = "plan";
    else if (!strcmp(verb, "multiedit") || !strcmp(verb, "notebookedit")) v = "edit";
    else if (!strcmp(verb, "webfetch")) v = "fetch";
    else if (!strcmp(verb, "websearch")) v = "search";
    const char* d = a.actDetail;
    // A path shows its file name.
    if (d[0] && !strchr(d, ' ')) {
      const char* slash = strrchr(d, '/');
      if (slash && slash[1]) d = slash + 1;
    }
    if (d[0]) snprintf(out, cap, "%s" DECK_DOT "%s", v, d);
    else snprintf(out, cap, "%s", v);
    return;
  }
  if (a.status == Status::Error) { snprintf(out, cap, "%s", a.errMsg[0] ? a.errMsg : "error"); return; }
  if (a.status == Status::Exited) {
    snprintf(out, cap, "%s", a.lost ? "lost with tmux" : a.resumable ? "ended" DECK_DOT "resumable" : "exited");
    return;
  }
  snprintf(out, cap, "%s", a.aiTitle);
}

// ---------------------------------------------------------------- notices

namespace round_detail {

// The full-screen states: a ring, a critter and a few lines.
static inline void noticeRing(Canvas& c, uint16_t col, int dashOn, int dashOff, uint32_t spinMs, uint32_t ms) {
  const float r = 218, t = 4;
  if (!dashOn) { strokeCircle(c, kRoundC, kRoundC, r, t, col); return; }
  const float circ = 2 * kRoundPi * r;
  const int n = (int)(circ / (dashOn + dashOff));
  const float dashDeg = 360.f * dashOn / circ;
  const float off = spinMs ? 360.f * (ms % spinMs) / spinMs : 0;
  for (int i = 0; i < n; i++) strokeArc(c, kRoundC, kRoundC, r, t, off + 360.f * i / n, dashDeg, col);
}

}  // namespace round_detail

static inline void drawNotice(Canvas& c, const RoundTheme& T, const RoundInput& in) {
  using namespace round_detail;
  const uint16_t ink = T.t.ink, dim = T.t.dim, acc = T.t.accent;
  char buf[64];
  switch (in.link) {
    case LinkState::Connecting: {
      // On USB: "connecting / waiting for the deck / on USB"; on Wi-Fi, where it is.
      const NetNotice n = netNotice(in.net ? *in.net : NetView());
      noticeRing(c, n.problem ? acc : T.t.line, n.problem ? 2 : 6, n.problem ? 12 : 10, n.problem ? 0 : 6000, in.ms);
      critterAt(c, 240, 186, 6, Species::Claude, n.problem ? Pose::Exit : Pose::Run, dim, T.t.bg, in.ms, false);
      textOn(c, FONT_R30, 240, 288, n.title, ink);
      if (n.line1) textOn(c, FONT_XXL, 240, 326, n.line1, n.problem ? acc : dim);
      if (n.line2) textOn(c, FONT_XXL, 240, 360, n.line2, dim);
      return;
    }
    case LinkState::Unapproved: {
      noticeRing(c, acc, 2, 12, 0, in.ms);
      critterAt(c, 240, 166, 5, Species::Claude, Pose::Wait, ink, T.t.bg, in.ms, true);
      textOn(c, FONT_R30, 240, 256, "waiting for", ink);
      textOn(c, FONT_R30, 240, 290, "approval", ink);
      textOn(c, FONT_XXL, 240, 340, "agentctl devices", dim);
      // A board's name is long ("waveshare-touch-lcd-28c-1a2b3c"): it breaks after a dash.
      snprintf(buf, sizeof buf, "approve %s", in.device && in.device[0] ? in.device : "<name>");
      textLinesOn(c, FONT_XXL, 372, 32, buf, acc, 3, 30);   // inside the dashed ring
      return;
    }
    case LinkState::Lost: {
      noticeRing(c, T.t.line, 0, 0, 0, in.ms);
      critterAt(c, 240, 186, 6, Species::Claude, Pose::Exit, dim, T.t.bg, in.ms, false);
      textOn(c, FONT_R30, 240, 288, "deck offline", ink);
      char age[8];
      fmtAge(in.silentSec, age, sizeof age);
      snprintf(buf, sizeof buf, "last seen %s ago", age);
      textOn(c, FONT_XXL, 240, 326, buf, dim);
      textOn(c, FONT_XXL, 240, 360, "retrying\xE2\x80\xA6", dim);
      return;
    }
    default:   // live, but nobody on the deck
      noticeRing(c, T.t.line, 6, 10, 0, in.ms);
      critterAt(c, 240, 181, 6, Species::Claude, Pose::Idle, dim, T.t.bg, in.ms, true);
      textOn(c, FONT_R30, 240, 300, "no agents", ink);
      textOn(c, FONT_XXL, 240, 338, "agentctl new", dim);
      return;
  }
}

// ---------------------------------------------------------------- the detail

// What an agent is doing, in a word, and for how long: "working · 12m",
// "thinking · 3s", "waiting · 2m", "hungry · 12m", "idle · 1h", "error · 5m".
// No commands or comments: the round screens say only the state.
static inline const char* roundStateWord(const Agent& a) {
  switch (a.status) {
    case Status::Running:  return a.actTool[0] ? "working" : "thinking";
    case Status::Waiting:  return "waiting";
    case Status::Error:    return "error";
    case Status::Idle:     return a.unseen ? "hungry" : "idle";
    case Status::Starting: return "starting";
    case Status::Exited:   return a.lost ? "lost" : "exited";
    default:               return "idle";
  }
}

static inline void roundStateLine(const Agent& a, int64_t now, char* out, size_t cap) {
  if (!a.statusSince || a.status == Status::Starting || now < a.statusSince) {
    snprintf(out, cap, "%s", roundStateWord(a));
    return;
  }
  char age[8];
  fmtAge(now - a.statusSince, age, sizeof age);
  snprintf(out, cap, "%s" DECK_DOT "%s", roundStateWord(a), age);
}

// How a layout draws an agent's detail: the context bar round the top, the
// critter, the name, the state, model · effort · context, and a row of
// round buttons on the bottom arc (close on the left, then focus, interrupt
// while it runs, stop). The bar ends a little short of the row, so it grows
// when the row has fewer buttons; both move with a short ease.
struct DetailStyle {
  float ringR, ringW;          // the context bar
  int critterScale, critterCy;
  const Font* titleFont;
  int titleBase;
  const Font* statusFont;
  int statusBase;
  int modelBase;               // "opus 5.5 · high · 41%", or the prompt's question
  float btnR, btnArcR, btnStep;   // the row: circles of btnR on an arc of btnArcR, btnStep degrees apart
  float btnHit;                // a button's hit circle
  float arcMargin;             // degrees between the bar's ends and the row
};

// The Orbit's (2.8″).
static inline DetailStyle detailOrbit() {
  DetailStyle s{};
  s.ringR = 226; s.ringW = 14;
  s.critterScale = 6; s.critterCy = 134;
  s.titleFont = &FONT_R35; s.titleBase = 226;
  s.statusFont = &FONT_XXL; s.statusBase = 262;
  s.modelBase = 294;
  s.btnR = 34; s.btnArcR = 172; s.btnStep = 30; s.btnHit = 42; s.arcMargin = 6;
  return s;
}

// The Dial's (2.1″): the same, with a thinner bar and bigger buttons.
static inline DetailStyle detailDial() {
  DetailStyle s = detailOrbit();
  s.ringR = 228; s.ringW = 10;
  s.statusFont = &FONT_R30; s.statusBase = 264;
  s.modelBase = 298;
  s.btnR = 36; s.btnStep = 32; s.btnHit = 44;
  return s;
}

// The buttons, as bits, in the row's order from the left.
constexpr uint8_t kBtnClose = 1, kBtnFocus = 2, kBtnInterrupt = 4, kBtnStop = 8;
static const HitKind kRowKinds[4] = {HitKind::Back, HitKind::Focus, HitKind::Interrupt, HitKind::Stop};

// Which buttons an agent's detail has: close always; focus and interrupt
// (while it runs) with scope act; stop with scope admin.
static inline uint8_t detailRowMask(const Agent& a, RoundScope scope) {
  uint8_t m = kBtnClose;
  if (canAct(scope)) {
    m |= kBtnFocus;
    if (a.status == Status::Running) m |= kBtnInterrupt;
  }
  if (scope == RoundScope::Admin) m |= kBtnStop;
  return m;
}

static inline int maskCount(uint8_t m) {
  int n = 0;
  for (int i = 0; i < 4; i++) n += (m >> i) & 1;
  return n;
}

// Where button `bit` of row `mask` sits: its angle (180 is 6 o'clock; the
// row runs from the left, larger angles, to the right). -1 when not in the row.
static inline float rowAngle(const DetailStyle& S, uint8_t mask, uint8_t bit) {
  if (!(mask & bit)) return -1;
  const int n = maskCount(mask);
  int i = 0;
  for (uint8_t b = 1; b < bit; b <<= 1) i += (mask & b) ? 1 : 0;
  return 180 + (n - 1) * S.btnStep / 2 - i * S.btnStep;
}

// How far from 6 o'clock the bar keeps away, either side, for n buttons.
static inline float barExtent(const DetailStyle& S, int n) {
  if (n <= 0) return 0;
  return (n - 1) * S.btnStep / 2 + asinf(S.btnR / S.btnArcR) * 180 / kRoundPi + S.arcMargin;
}

constexpr uint32_t kRowAnimMs = 250;

// The name keeps 10 px clear of the context bar's inner edge on either side.
static inline int detailTitleWidth(const DetailStyle& S) {
  return fitWidth(*S.titleFont, S.titleBase, (int)(240 - (S.ringR - S.ringW / 2)) + 10);
}

// Keeps the row's animation state current: call before drawing with what the
// detail shows. A new agent starts without animation; a change of buttons
// eases from the old row to the new over kRowAnimMs.
static inline void roundRowSync(RoundUi& ui, const char* agentId, uint8_t mask, uint32_t ms) {
  if (strcmp(ui.row.agent, agentId)) {
    snprintf(ui.row.agent, sizeof ui.row.agent, "%s", agentId);
    ui.row.from = ui.row.to = mask;
    ui.row.t0 = ms - kRowAnimMs;
    return;
  }
  if (mask == ui.row.to) return;
  ui.row.from = ui.row.to;
  ui.row.to = mask;
  ui.row.t0 = ms;
}

// The eased progress of the row's change, 0..1 (1 when settled).
static inline float rowProgress(const RoundUi& ui, uint32_t ms) {
  const uint32_t dt = ms - ui.row.t0;
  if (dt >= kRowAnimMs || ui.row.from == ui.row.to) return 1;
  const float t = (float)dt / kRowAnimMs;
  const float u = 1 - t;
  return 1 - u * u * u;   // ease out
}

struct RowButton {
  HitKind kind;
  float x, y, r;               // r shrinks to 0 as a button leaves, grows as one arrives
};

struct RowFrame {
  RowButton btn[4];
  int n;
  float arcStart, arcSweep;    // the context bar
};

// The row and the bar at time ms. `mask` is what the detail should show now;
// if the animation state is for something else, it is drawn settled.
static inline RowFrame rowFrame(const DetailStyle& S, const RoundUi& ui, const char* agentId, uint8_t mask, uint32_t ms) {
  uint8_t from = mask, to = mask;
  float t = 1;
  if (!strcmp(ui.row.agent, agentId) && ui.row.to == mask) {
    from = ui.row.from;
    t = rowProgress(ui, ms);
  }
  RowFrame f{};
  for (int i = 0; i < 4; i++) {
    const uint8_t bit = (uint8_t)(1 << i);
    const float a0 = rowAngle(S, from, bit), a1 = rowAngle(S, to, bit);
    if (a0 < 0 && a1 < 0) continue;
    float ang, scale;
    if (a0 >= 0 && a1 >= 0) { ang = a0 + (a1 - a0) * t; scale = 1; }
    else if (a1 >= 0) { ang = a1; scale = t; }
    else { ang = a0; scale = 1 - t; }
    if (scale <= 0.01f) continue;
    const float rad = ang * kRoundPi / 180;
    f.btn[f.n++] = RowButton{kRowKinds[i], kRoundC + S.btnArcR * sinf(rad), kRoundC - S.btnArcR * cosf(rad), S.btnR * scale};
  }
  const float e0 = barExtent(S, maskCount(from)), e1 = barExtent(S, maskCount(to));
  const float e = e0 + (e1 - e0) * t;
  f.arcStart = 180 + e;
  f.arcSweep = 360 - 2 * e;
  return f;
}

static inline void drawRowButton(Canvas& c, const RoundTheme& T, const RowButton& b, bool pressed, int holdPermille) {
  const int r = (int)lroundf(b.r);
  if (r < 4) return;
  if (b.r < 0.7f * 34) {          // arriving or leaving: a plain ring, no icon yet
    strokeCircle(c, b.x, b.y, b.r - 1.5f, 3, b.kind == HitKind::Stop ? T.t.accent : T.t.ink);
    return;
  }
  const IconKind k = b.kind == HitKind::Focus ? IconKind::Focus
                     : b.kind == HitKind::Interrupt ? IconKind::Interrupt
                     : b.kind == HitKind::Back ? IconKind::Close : IconKind::Stop;
  iconButton(c, (int)lroundf(b.x), (int)lroundf(b.y), r, k, pressed, holdPermille, T.t.bg, T.t.ink, T.t.accent, T.idle);
}

static inline void drawDetail(Canvas& c, const RoundTheme& T, const DetailStyle& S, const RoundInput& in,
                              const Agent& a, int /*index*/) {
  const RoundUi& ui = *in.ui;
  char buf[96];
  const uint8_t mask = detailRowMask(a, in.scope);
  const RowFrame row = rowFrame(S, ui, a.id, mask, in.ms);

  // The context bar: the track, then the share used, along the same arc.
  strokeArc(c, kRoundC, kRoundC, S.ringR, S.ringW, row.arcStart, row.arcSweep, T.t.line);
  const int pct = ctxPct(a);
  if (a.ctxWindow && pct > 0)
    strokeArc(c, kRoundC, kRoundC, S.ringR, S.ringW, row.arcStart, row.arcSweep * pct / 100, T.t.accent);

  const bool waiting = a.status == Status::Waiting;
  const int s = S.critterScale;
  if (waiting) fillDisc(c, 240, S.critterCy, 12.f * s, T.t.accent);
  agentCritter(c, a, 240, S.critterCy, s, bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms, in.nowEpoch, !waiting);
  textOn(c, *S.titleFont, 240, S.titleBase, a.title[0] ? a.title : a.id, T.t.ink, detailTitleWidth(S));
  roundStateLine(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Running || waiting || a.status == Status::Error || hungry(a);
  textOn(c, *S.statusFont, 240, S.statusBase, buf, loud ? T.t.accent : T.t.dim);

  // The third line: model · effort · context; or the prompt's question; or "hold to stop".
  uint16_t col = T.t.dim;
  if (ui.pressed.kind == HitKind::Stop) { snprintf(buf, sizeof buf, "hold to stop"); col = T.t.accent; }
  else if (waiting) { snprintf(buf, sizeof buf, "%s", a.waitTitle[0] ? a.waitTitle : "needs you"); col = T.t.accent; }
  else {
    buf[0] = '\0';
    auto add = [&](const char* part) {
      if (!part[0]) return;
      const size_t n = strlen(buf);
      snprintf(buf + n, sizeof buf - n, "%s%s", n ? DECK_DOT : "", part);
    };
    add(a.model);
    add(a.effort);
    char ctx[12] = "";
    if (a.ctxWindow) snprintf(ctx, sizeof ctx, "%d%%", pct);
    add(ctx);
  }
  if (buf[0]) textOn(c, FONT_XXL, 240, S.modelBase, buf, col);

  const int hold = roundHoldPermille(ui, in.ms);
  for (int i = 0; i < row.n; i++) drawRowButton(c, T, row.btn[i], ui.pressed.kind == row.btn[i].kind, hold);
}

// What a touch on the detail lands on: the row (where it will settle), the
// big critter (home), then the middle (a waiter's prompt).
static inline RoundHit detailHit(const DetailStyle& S, const Fleet& f, const Agent& a, int x, int y, RoundScope scope) {
  (void)f;
  RoundHit h;
  const uint8_t mask = detailRowMask(a, scope);
  for (int i = 0; i < 4; i++) {
    const float ang = rowAngle(S, mask, (uint8_t)(1 << i));
    if (ang < 0) continue;
    const RPoint p = polar(kRoundC, kRoundC, S.btnArcR, ang);
    if (inCircle(p.x, p.y, S.btnHit, x, y)) { h.kind = kRowKinds[i]; return h; }
  }
  if (inCircle(240, S.critterCy, 9.f * S.critterScale, x, y)) { h.kind = HitKind::Back; return h; }
  if (inCircle(240, (S.critterCy + S.modelBase) / 2.f, 120, x, y)) h.kind = HitKind::Middle;
  return h;
}

// Before drawing a frame: keeps the detail's row animation current.
static inline void roundAnimate(RoundUi& ui, const Fleet& f, RoundScope scope, uint32_t ms) {
  if (ui.screen != RoundScreen::Detail) return;
  const int i = findAgent(f, ui.openId);
  if (i >= 0) roundRowSync(ui, f.agents[i].id, detailRowMask(f.agents[i], scope), ms);
}

// The prompt keeps its close button under the top edge (it has no row).
constexpr int kBackX = 240, kBackY = 50, kBackR = 24, kBackHit = 36;

static inline void drawBackButton(Canvas& c, const RoundTheme& T, bool pressed) {
  iconButton(c, kBackX, kBackY, kBackR, IconKind::Close, pressed, 0, T.t.bg, T.t.ink, T.t.accent, T.idle);
}

static inline bool backHit(int x, int y) { return inCircle(kBackX, kBackY, kBackHit, x, y); }

static inline float dotRadius(int n, bool waiting) {
  float r = waiting ? 8 : 5;
  if (n > 20) r -= 2;
  return r < 3 ? 3 : r;
}

// ---------------------------------------------------------------- the prompt (P1)

struct Pill {
  int x, y, w, h;
  int option;   // index into Agent.opts
};

// The answers as stacked pills, each as wide as the circle allows at its
// height (at most 340): 72 px tall for up to three, 60 px and scrolling for more.
static inline int promptPills(const Agent& a, int first, Pill out[4]) {
  const int n = a.optCount;
  if (n <= 0) return 0;
  int top, h, gap, shown;
  if (n > 3) { top = 172; h = 60; gap = 6; shown = kRoundOptShown; }
  else { h = 72; gap = 10; shown = n; top = n == 3 ? 178 : n == 2 ? 200 : 214; }
  if (n <= 3) first = 0;
  else if (first > n - shown) first = n - shown;
  if (first < 0) first = 0;
  int count = 0;
  for (int k = 0; k < shown && first + k < n; k++) {
    const int y = top + k * (h + gap);
    // Sides inset from a 214 px circle at the pill's far edge, capped at 340.
    float half = chordHalfSpan(kRoundC, 214, (float)y, (float)(y + h));
    if (half > 170) half = 170;
    const int w = (int)(2 * half) & ~1;
    out[count++] = Pill{240 - w / 2, y, w, h, first + k};
  }
  return count;
}

static inline void drawPrompt(Canvas& c, const RoundTheme& T, const RoundInput& in, const Agent& a) {
  const RoundUi& ui = *in.ui;
  const bool act = canAct(in.scope);
  strokeCircle(c, kRoundC, kRoundC, 234, 8, T.t.accent);
  drawBackButton(c, T, ui.pressed.kind == HitKind::Back);
  const char* title = a.waitTitle[0] ? a.waitTitle : "needs you";
  const char* detail = a.waitDetail[0] ? a.waitDetail : a.promptCtx;
  // A title too long for 34 px drops to 26 px; one too long for a line takes
  // two, and the detail gives way.
  bool longTitle = false;
  if (textW(FONT_R35, title) <= fitWidth(FONT_R35, 118)) textOn(c, FONT_R35, 240, 118, title, T.t.ink);
  else if (textW(FONT_XXL, title) <= fitWidth(FONT_XXL, 118)) textOn(c, FONT_XXL, 240, 118, title, T.t.ink);
  else longTitle = textLinesOn(c, FONT_XXL, 112, 32, title, T.t.ink, 2) > 1;
  if (!act) textOn(c, FONT_XXL, 240, longTitle ? 158 : 152, "answer on the deck", T.t.accent);
  else if (!longTitle && detail[0]) textOn(c, FONT_XXL, 240, 152, detail, T.t.dim);

  if (a.optCount == 0) {
    textOn(c, FONT_XXL, 240, 260, "answer in the terminal", T.t.dim);
    return;
  }
  Pill p[4];
  const int n = promptPills(a, ui.optFirst, p);
  char buf[PANEL_OPTION_LABEL + 16];
  for (int i = 0; i < n; i++) {
    const Option& o = a.opts[p[i].option];
    const bool pressed = ui.pressed.kind == HitKind::Option && ui.pressed.option == p[i].option;
    const bool primary = o.primary && act;
    uint16_t label = act ? T.t.ink : T.t.dim;
    const Font* f = &FONT_R30;
    if (pressed) {
      fillPill(c, p[i].x, p[i].y, p[i].w, p[i].h, T.t.ink);
      label = T.t.bg;
    } else if (primary) {
      fillPill(c, p[i].x, p[i].y, p[i].w, p[i].h, T.t.accent);
      label = T.t.bg;
      f = &FONT_R30B;
    } else {
      strokePill(c, p[i].x, p[i].y, p[i].w, p[i].h, 3, T.t.dim);
    }
    snprintf(buf, sizeof buf, "%s" DECK_DOT "%s", o.key, o.label);
    // A long answer drops to the 26 px floor, then takes two lines, before it is cut.
    const int room = p[i].w - 40;   // the ends are round, but the text sits near the middle
    if (textW(*f, buf) <= room) {
      textCentered(c, *f, 240, p[i].y + p[i].h / 2 + 11 - fontAscent(*f), buf, label, room);
    } else if (textW(FONT_XXL, buf) <= room) {
      textCentered(c, FONT_XXL, 240, p[i].y + p[i].h / 2 + 9 - fontAscent(FONT_XXL), buf, label, room);
    } else {
      // Break at the last space that fits the first line.
      const int per = room / FONT_XXL.cellW;
      const char* q = buf;
      const char* cut = nullptr;
      for (int k = 0; *q && k <= per; k++) {
        if (*q == ' ') cut = q;
        utf8Next(q);
      }
      if (!cut) cut = buf + strlen(buf) / 2;
      char first[sizeof buf];
      snprintf(first, sizeof first, "%.*s", (int)(cut - buf), buf);
      const int mid = p[i].y + p[i].h / 2;
      textCentered(c, FONT_XXL, 240, mid - 4 - fontAscent(FONT_XXL), first, label, room);
      textCentered(c, FONT_XXL, 240, mid + 22 - fontAscent(FONT_XXL), cut + (*cut == ' '), label, room);
    }
  }
  // More than fit: a small mark that the list goes on.
  if (a.optCount > n) {
    const int first = p[0].option, last = p[n - 1].option;
    if (first > 0) fillDisc(c, 240, 164, 4, T.t.dim);
    if (last < a.optCount - 1) fillDisc(c, 240, 446, 4, T.t.dim);
  }
}

static inline RoundHit promptHit(const Agent& a, int first, int x, int y) {
  RoundHit h;
  if (backHit(x, y)) { h.kind = HitKind::Back; return h; }
  Pill p[4];
  const int n = promptPills(a, first, p);
  for (int i = 0; i < n; i++)
    if (x >= p[i].x && x < p[i].x + p[i].w && y >= p[i].y && y < p[i].y + p[i].h) {
      h.kind = HitKind::Option;
      h.option = p[i].option;
      return h;
    }
  return h;
}

// ---------------------------------------------------------------- the frame

// A short message in a pill near the bottom.
// Where a toast's pill goes: low on the glass, raised until it fits inside the
// circle (with a margin), and never wider than the circle allows there.
struct ToastBox {
  int x, y, w, h;
};

static inline ToastBox toastBox(const char* text) {
  const Font& f = FONT_XXL;
  const int h = 52, margin = 10;
  int w = textW(f, text) + 40, y = 404;
  auto room = [&](int yy) { return (int)(2 * chordHalfSpan(kRoundC, kRoundC - margin, (float)yy, (float)(yy + h))); };
  while (y > 300 && room(y) < w) y -= 4;
  if (w > room(y)) w = room(y);   // still too long: the text is cut with "…"
  return ToastBox{240 - w / 2, y, w, h};
}

static inline void drawToast(Canvas& c, const RoundTheme& T, const RoundUi& ui, uint32_t ms) {
  if (!ui.toast[0] || (int32_t)(ui.toastUntil - ms) <= 0) return;
  const Font& f = FONT_XXL;
  const ToastBox b = toastBox(ui.toast);
  fillPill(c, b.x, b.y, b.w, b.h, T.t.ink);
  textCentered(c, f, 240, b.y + (b.h - f.cellH) / 2, ui.toast, T.t.bg, b.w - 20);
}

// Everything but home: notices, the detail, the prompt. Returns false when
// the layout should draw its home screen. The frame is cleared first.
static inline bool drawRoundCommon(Canvas& c, const RoundTheme& T, const DetailStyle& S, const RoundInput& in) {
  c.fill(T.t.bg);
  if (in.link != LinkState::Live || !in.fleet || in.fleet->count == 0) {
    drawNotice(c, T, in);
    return true;
  }
  const RoundUi& ui = *in.ui;
  const int i = ui.screen == RoundScreen::Home ? -1 : findAgent(*in.fleet, ui.openId);
  if (i < 0) return false;
  if (ui.screen == RoundScreen::Prompt) drawPrompt(c, T, in, in.fleet->agents[i]);
  else drawDetail(c, T, S, in, in.fleet->agents[i], i);
  return true;
}

// On Wi-Fi, a small Wi-Fi mark under home's middle (on USB, nothing: the
// cable says it). The notices say it in words.
constexpr int kNetBadgeY = 398;   // the mark's base: below home's text, above the ring

static inline void drawNetBadge(Canvas& c, const RoundTheme& T, const RoundInput& in) {
  if (!in.net || in.net->via != NetVia::Wifi || in.link != LinkState::Live || !in.fleet || !in.fleet->count ||
      !in.ui || in.ui->screen != RoundScreen::Home)
    return;
  // Dark on an orange face (someone waits on the dial), dim otherwise.
  const uint16_t under = c.px[(size_t)(kNetBadgeY - 8) * (size_t)c.w + (size_t)c.w / 2];
  const uint16_t col = under == T.t.accent ? T.t.bg : T.t.dim;
  const float cx = kRoundC, cy = kNetBadgeY;   // the arcs' common centre
  fillDisc(c, cx, cy - 1, 2.5f, col);
  for (int i = 1; i <= 2; i++) strokeArc(c, cx, cy, 6.5f * i + 1, 2.5f, -45, 90, col);
}

// The finishing touches on any frame: the Wi-Fi mark, the toast, and the round mask.
static inline void drawRoundOverlay(Canvas& c, const RoundTheme& T, const RoundInput& in) {
  drawNetBadge(c, T, in);
  if (in.ui) drawToast(c, T, *in.ui, in.ms);
  maskOutside(c, kRoundC, kRoundC, 240, rgb565(0, 0, 0));
}

// What a touch lands on, on the detail or the prompt (home is the layout's).
static inline RoundHit roundCommonHit(const DetailStyle& S, const RoundUi& ui, const Fleet& f, int x, int y,
                                      RoundScope scope) {
  const int i = findAgent(f, ui.openId);
  if (i < 0) return RoundHit();
  if (ui.screen == RoundScreen::Prompt) return promptHit(f.agents[i], ui.optFirst, x, y);
  return detailHit(S, f, f.agents[i], x, y, scope);
}
