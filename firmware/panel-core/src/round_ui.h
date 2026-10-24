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
    case LinkState::Connecting:
      noticeRing(c, T.t.line, 6, 10, 6000, in.ms);
      critterAt(c, 240, 186, 6, Species::Claude, Pose::Run, dim, T.t.bg, in.ms, false);
      textOn(c, FONT_R30, 240, 288, "connecting", ink);
      textOn(c, FONT_XXL, 240, 326, "waiting for the deck", dim);
      textOn(c, FONT_XXL, 240, 360, "on USB", dim);
      return;
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

// How a layout draws an agent's detail. The context bar is an arc from
// `arcStart` clockwise over `arcSweep` degrees (over the top, from about 8 to
// 4 o'clock), so the bottom stays free for the buttons.
struct DetailStyle {
  float ringR, ringW;          // the context bar
  float arcStart, arcSweep;
  bool dots;                   // the fleet as dots inside the bar (the dial)
  float dotsR;
  int ctxLabelBase;            // "context 41%" (0: none)
  int critterScale, critterCy;
  const Font* titleFont;
  int titleBase;
  const Font* statusFont;
  int statusBase;
  int modelBase;               // "opus 5.5 · high" (0: none)
  int activityBase;
  // The buttons (V1): circles of btnR on an arc of btnArcR round the centre,
  // at 180° and ± btnSpread3 (three) or ± btnSpread2 (two).
  float btnR, btnArcR, btnSpread3, btnSpread2;
  float btnHit;                // the hit circle's radius
};

// The Orbit's (2.8″): the bar over two thirds of the circle.
static inline DetailStyle detailOrbit() {
  DetailStyle s{};
  s.ringR = 226; s.ringW = 14; s.arcStart = 240; s.arcSweep = 240;
  s.dots = false; s.dotsR = 0;
  s.ctxLabelBase = 58;
  s.critterScale = 5; s.critterCy = 122;
  s.titleFont = &FONT_R35; s.titleBase = 212;
  s.statusFont = &FONT_XXL; s.statusBase = 246;
  s.modelBase = 278; s.activityBase = 310;
  s.btnR = 38; s.btnArcR = 168; s.btnSpread3 = 36; s.btnSpread2 = 22; s.btnHit = 44;
  return s;
}

// The Dial's (2.1″): a shorter bar (three fifths), the fleet's dots inside it,
// bigger buttons for the denser screen.
static inline DetailStyle detailDial() {
  DetailStyle s{};
  s.ringR = 228; s.ringW = 10; s.arcStart = 252; s.arcSweep = 216;
  s.dots = true; s.dotsR = 205;
  s.ctxLabelBase = 0;
  s.critterScale = 5; s.critterCy = 132;
  s.titleFont = &FONT_R35; s.titleBase = 222;
  s.statusFont = &FONT_R30; s.statusBase = 258;
  s.modelBase = 0; s.activityBase = 294;
  s.btnR = 40; s.btnArcR = 170; s.btnSpread3 = 38; s.btnSpread2 = 23; s.btnHit = 46;
  return s;
}

struct DetailButton {
  HitKind kind;
  int x, y;
};

// The buttons the agent and the board's scope allow, right to left (focus,
// interrupt while running, stop). Scope view: none.
static inline int detailButtons(const DetailStyle& S, const Agent& a, RoundScope scope, DetailButton out[3]) {
  HitKind k[3];
  int n = 0;
  if (!canAct(scope)) return 0;
  k[n++] = HitKind::Focus;
  if (a.status == Status::Running) k[n++] = HitKind::Interrupt;
  if (scope == RoundScope::Admin) k[n++] = HitKind::Stop;
  for (int i = 0; i < n; i++) {
    float ang = 180;
    if (n == 3) ang = 180 - S.btnSpread3 + i * S.btnSpread3;
    else if (n == 2) ang = 180 - S.btnSpread2 + i * 2 * S.btnSpread2;
    const RPoint p = polar(kRoundC, kRoundC, S.btnArcR, ang);
    out[i] = DetailButton{k[i], p.x, p.y};
  }
  return n;
}

namespace round_detail {

// The buttons' icons: 4 px cells, centred on the button.
static const char* const kIconFocus[] = {"#########", "#.......#", "#.#.....#", "#..#....#", "#...#...#",
                                         "#..#....#", "#.#.###.#", "#.......#", "#########"};
static const char* const kIconPause[] = {"##.##", "##.##", "##.##", "##.##", "##.##", "##.##", "##.##"};
static const char* const kIconStop[] = {"#####", "#####", "#####", "#####", "#####"};

static inline void icon(Canvas& c, const char* const* rows, int n, int cx, int cy, uint16_t col) {
  const int w = (int)strlen(rows[0]) * 4, h = n * 4;
  for (int r = 0; r < n; r++)
    for (int k = 0; rows[r][k]; k++)
      if (rows[r][k] == '#') c.rect(cx - w / 2 + k * 4, cy - h / 2 + r * 4, 4, 4, col);
}

}  // namespace round_detail

// One round button: idle (outlined), pressed (filled ink), stop held (filled
// accent with the hold ring filling over 1 s).
static inline void drawButton(Canvas& c, const RoundTheme& T, const DetailStyle& S, const DetailButton& b, bool pressed,
                              int holdPermille) {
  using namespace round_detail;
  const bool stop = b.kind == HitKind::Stop;
  const uint16_t line = stop ? T.t.accent : T.t.ink;
  uint16_t fill = T.t.bg, ico = line;
  if (pressed) { fill = stop ? T.t.accent : T.t.ink; ico = T.t.bg; }
  fillDisc(c, b.x, b.y, S.btnR, fill);
  if (!pressed) strokeCircle(c, b.x, b.y, S.btnR - 1.5f, 3, line);
  if (b.kind == HitKind::Focus) icon(c, kIconFocus, 9, b.x, b.y, ico);
  else if (b.kind == HitKind::Interrupt) icon(c, kIconPause, 7, b.x, b.y, ico);
  else icon(c, kIconStop, 5, b.x, b.y, ico);
  if (stop && pressed) {
    strokeCircle(c, b.x, b.y, S.btnR + 7, 5, T.idle);
    if (holdPermille > 0) strokeArc(c, b.x, b.y, S.btnR + 7, 5, 0, 360.f * holdPermille / 1000, T.t.accent);
  }
}

// Where agent i of n sits on the detail's arc of dots.
static inline RPoint detailDot(const DetailStyle& S, int i, int n) {
  const float pad = 4, span = S.arcSweep - 2 * pad;
  const float ang = n > 1 ? S.arcStart + pad + span * i / (n - 1) : S.arcStart + S.arcSweep / 2;
  return polar(kRoundC, kRoundC, S.dotsR, ang);
}

static inline float dotRadius(int n, bool waiting) {
  float r = waiting ? 8 : 5;
  if (n > 20) r -= 2;
  return r < 3 ? 3 : r;
}

static inline void drawDetail(Canvas& c, const RoundTheme& T, const DetailStyle& S, const RoundInput& in,
                              const Agent& a, int index) {
  const Fleet& f = *in.fleet;
  const RoundUi& ui = *in.ui;
  char buf[96];
  // The context bar: the track, then the share used, along the same arc.
  strokeArc(c, kRoundC, kRoundC, S.ringR, S.ringW, S.arcStart, S.arcSweep, T.t.line);
  const int pct = ctxPct(a);
  if (a.ctxWindow && pct > 0) strokeArc(c, kRoundC, kRoundC, S.ringR, S.ringW, S.arcStart, S.arcSweep * pct / 100, T.t.accent);
  if (S.ctxLabelBase) {
    if (a.ctxWindow) snprintf(buf, sizeof buf, "context %d%%", pct);
    else snprintf(buf, sizeof buf, "context \xE2\x80\x93");
    textOn(c, FONT_XXL, 240, S.ctxLabelBase, buf, T.t.dim);
  }
  if (S.dots) {
    for (int i = 0; i < f.count; i++) {
      const Agent& o = f.agents[i];
      const RPoint p = detailDot(S, i, f.count);
      const float r = dotRadius(f.count, o.status == Status::Waiting);
      fillDisc(c, p.x, p.y, r, statusColor(T, o));
      if (i == index) strokeCircle(c, p.x, p.y, r + 5, 2, T.t.ink);
    }
  }
  const bool waiting = a.status == Status::Waiting;
  if (waiting) fillDisc(c, 240, S.critterCy, 12 * S.critterScale, T.t.accent);
  agentCritter(c, a, 240, S.critterCy, S.critterScale, bodyColor(T, a), waiting ? T.t.accent : T.t.bg, in.ms,
               in.nowEpoch, !waiting);
  textOn(c, *S.titleFont, 240, S.titleBase, a.title[0] ? a.title : a.id, T.t.ink);
  statusLine(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status != Status::Idle || a.unseen;
  textOn(c, *S.statusFont, 240, S.statusBase, buf, loud && a.status != Status::Exited ? T.t.accent : T.t.dim);
  if (S.modelBase && (a.model[0] || a.effort[0])) {
    if (a.model[0] && a.effort[0]) snprintf(buf, sizeof buf, "%s" DECK_DOT "%s", a.model, a.effort);
    else snprintf(buf, sizeof buf, "%s", a.model[0] ? a.model : a.effort);
    textOn(c, FONT_XXL, 240, S.modelBase, buf, T.t.dim);
  }
  // The activity line, or what the finger is about to do.
  DetailButton btn[3];
  const int nb = detailButtons(S, a, in.scope, btn);
  const int hold = roundHoldPermille(ui, in.ms);
  uint16_t actCol = S.modelBase ? T.t.ink : T.t.dim;
  if (ui.pressed.kind == HitKind::Stop) { snprintf(buf, sizeof buf, "hold to stop"); actCol = T.t.accent; }
  else if (waiting) { snprintf(buf, sizeof buf, "%s", canAct(in.scope) ? "tap to answer" : a.waitTitle); actCol = T.t.accent; }
  else roundActivity(a, buf, sizeof buf);
  textOn(c, FONT_XXL, 240, S.activityBase, buf, actCol);
  for (int i = 0; i < nb; i++) drawButton(c, T, S, btn[i], ui.pressed.kind == btn[i].kind, hold);
}

// What a touch on the detail lands on.
static inline RoundHit detailHit(const DetailStyle& S, const Fleet& f, const Agent& a, int x, int y, RoundScope scope) {
  RoundHit h;
  DetailButton btn[3];
  const int nb = detailButtons(S, a, scope, btn);
  for (int i = 0; i < nb; i++)
    if (inCircle(btn[i].x, btn[i].y, S.btnHit, x, y)) { h.kind = btn[i].kind; return h; }
  (void)f;
  if (inCircle(240, (S.critterCy + S.activityBase) / 2.f, 130, x, y)) h.kind = HitKind::Middle;
  return h;
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
  critterAt(c, 240, 62, 3, a.species, Pose::Wait, T.t.accent, T.t.bg, in.ms, false, a.hat, a.hatColor);
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
  char buf[64];
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
      char first[64];
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
static inline void drawToast(Canvas& c, const RoundTheme& T, const RoundUi& ui, uint32_t ms) {
  if (!ui.toast[0] || (int32_t)(ui.toastUntil - ms) <= 0) return;
  const Font& f = FONT_XXL;
  const int w = textW(f, ui.toast) + 40, h = 52, y = 404;
  fillPill(c, 240 - w / 2, y, w, h, T.t.ink);
  textCentered(c, f, 240, y + (h - f.cellH) / 2, ui.toast, T.t.bg, w - 20);
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

// The finishing touches on any frame: the toast, and the round mask.
static inline void drawRoundOverlay(Canvas& c, const RoundTheme& T, const RoundInput& in) {
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
