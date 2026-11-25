#pragma once
// The square boards' layout: one layout system with per-board numbers
// (SquareSpec), after the owner's approved designs ("small square boards",
// boards 22-26 of the design canvas):
//
//   home    the 4": the deck's 4 x 4 grid by slot, paged, as built
//           the AMOLEDs: 2 tiles across in one vertical list that scrolls with
//           momentum and settles on a row, a position bar while it moves
//           (2 rows on screen on the 1.8", 3 on the T4-S3's 2.41")
//   card    header (critter; name over status pill, centred on it), doing
//           now with the subagent count, the working subagents (critter posed
//           by its tool, its task), context with the dotted bar, tokens /
//           cost / turns (on the T4-S3 one to a line, with cache read), the
//           last prompt where a line fits, round buttons:
//           focus, interrupt while running, stop held for 1 s (admin)
//   prompt  one layout for permissions and questions: a one-line header,
//           the whole question, the command box (permissions), the options
//           with their hint lines; a permission is the orange card, a
//           question dark with the orange border
//   states  waiting for approval, connecting, deck offline, no agents;
//           on Wi-Fi, where it is (joining, connecting, wrong password,
//           network not found, deck not reachable, certificate rejected)
//   Wi-Fi   on Wi-Fi a small mark: in the list's header after "agents"; the
//           grid keeps the page bars' row for it, at the right
//   attn    the "Needs you" screen (attention.h): on a grid, a page left of
//           the first, its mark at the left of the page bars; on a list, one
//           screen of what the list shows at once, left of the list, with
//           two small marks at the top of the header
//   slide   on a list the two are pages side by side: a sideways drag moves
//           both with the finger, and on release they settle on the nearer
//           one, or on the one a flick points at. The header stays; its two
//           marks follow the slide. The grid changes page at once.
//
// Back from a card or a prompt: swipe right, or BOOT. A tile's icons (tool
// chip, subagent critter) keep to a band above the critter, so they never
// meet the name.
//
// Pure C++ (no Arduino): the tests and sim/render draw exactly what the
// boards draw.
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "canvas.h"
#include "deck_cmd.h"
#include "deck_ui.h"
#include "font_data.h"
#include "font_round.h"
#include "font_square.h"
#include "model.h"
#include "page_slide.h"
#include "panel_layout.h"
#include "round_geom.h"
#include "sprites.h"
#include "ui_buttons.h"
#include "view.h"

// ---------------------------------------------------------------- the spec

enum class HomeKind : uint8_t { Grid, List };

struct SquareSpec {
  const char* name;
  int w, h, cornerR, m;        // panel, glass corner radius, margin
  float diagIn;                // the glass's diagonal in inches: physical distances (the drawer's 5 mm)
  bool imu;                    // a motion sensor on the board (set by its firmware): the tricks in fun_motion.h
  bool trueBlack;
  // Type: label / body / question / value / name.
  const Font *label, *body, *question, *value, *title;
  // Home.
  HomeKind home;
  int cols, rows;              // grid: cells per page; list: columns (rows scroll)
  int pad, gap;                // grid: outer padding and gap; list: gap
  int barsH;                   // grid: page bars under the grid when there is more than one page
  int tileW, tileH;            // list: tile size
  int headerH;                 // list: the header; the top row sits right under it
  const Font *tileTitle, *tileMeta, *hud;
  int tileScale, subScale;     // critters on the tiles
  // Card, prompt, notices.
  int cardScale, promptScale, noticeScale;
  int ctxPitch, ctxTickW, ctxTickH, ctxFillH;   // the dotted context bar
  int btnR, btnGap, btnFromBottom;              // round buttons: radius, gap, centre above the bottom
  int optH, optGap, optR;                       // prompt options
  // The list's position bar: x, width, between top and h - bottom.
  int posX, posW, posTop, posBottom;
  // The card's numbers one to a line (tokens out, cache read, cost, turns)
  // instead of three columns: a taller panel has the room.
  bool statLines;
};

// The 1.8" AMOLED, 368 x 448, rounded glass.
static inline SquareSpec specAmoled18() {
  SquareSpec s{};
  s.name = "amoled18";
  s.w = 368; s.h = 448; s.cornerR = 44; s.m = 16; s.trueBlack = true; s.diagIn = 1.8f;
  s.label = &FONT_XL; s.body = &FONT_Q22; s.question = &FONT_Q24B; s.value = &FONT_Q28B; s.title = &FONT_Q28B;
  s.home = HomeKind::List; s.cols = 2; s.rows = 0; s.pad = 8; s.gap = 8; s.barsH = 0;
  s.tileW = 172; s.tileH = 192; s.headerH = 50;
  s.tileTitle = &FONT_XL; s.tileMeta = &FONT_XL; s.hud = &FONT_XL;
  s.tileScale = 5; s.subScale = 2;     // critters 90 x 65 on a tile (x4 until the T4-S3 joined the family)
  s.cardScale = 5; s.promptScale = 2; s.noticeScale = 6;
  s.ctxPitch = 8; s.ctxTickW = 2; s.ctxTickH = 12; s.ctxFillH = 10;
  s.btnR = 36; s.btnGap = 26; s.btnFromBottom = 54;
  s.optH = 76; s.optGap = 8; s.optR = 12;
  s.posX = 4; s.posW = 6; s.posTop = 90; s.posBottom = 48;
  s.statLines = false;
  return s;
}

// The LilyGO T4-S3's 2.41" AMOLED, 450 x 600 portrait: the 1.8"'s layout
// (about the same pixel density, so the same type) with a third row of tiles
// on screen, bigger critters, and the card's numbers one to a line.
static inline SquareSpec specT4S3() {
  SquareSpec s = specAmoled18();
  s.name = "t4s3";
  s.w = 450; s.h = 600; s.cornerR = 0; s.diagIn = 2.41f;
  s.tileW = 213; s.tileH = 178;           // 2 x 3 on screen: 50 + 3 x 178 + 2 x 8 = 600
  s.tileScale = 6;                        // critters 108 x 78
  s.cardScale = 6; s.promptScale = 3; s.noticeScale = 7;
  s.btnR = 40; s.btnGap = 32; s.btnFromBottom = 60;
  s.optH = 84; s.optGap = 10;
  s.posTop = 100; s.posBottom = 40;
  s.statLines = true;
  return s;
}

// The 4" LCD, 480 x 480: the grid as built.
static inline SquareSpec specLcd4() {
  SquareSpec s{};
  s.name = "lcd4";
  s.w = 480; s.h = 480; s.cornerR = 0; s.m = 24; s.trueBlack = false; s.diagIn = 4.0f;
  s.label = &FONT_Q16; s.body = &FONT_XL; s.question = &FONT_Q24B; s.value = &FONT_Q28B; s.title = &FONT_R30B;
  s.home = HomeKind::Grid; s.cols = 4; s.rows = 4; s.pad = 8; s.gap = 8; s.barsH = 30;
  s.tileW = 0; s.tileH = 0; s.headerH = 0;
  s.tileTitle = &FONT_S; s.tileMeta = &FONT_XS; s.hud = &FONT_S;
  s.tileScale = 3; s.subScale = 1;   // the critter at the deck's proportion: 54 px on a 110 px tile (Flutter: 92 on 170)
  s.cardScale = 4; s.promptScale = 2; s.noticeScale = 6;
  s.ctxPitch = 6; s.ctxTickW = 2; s.ctxTickH = 10; s.ctxFillH = 8;
  s.btnR = 36; s.btnGap = 26; s.btnFromBottom = 54;
  s.optH = 72; s.optGap = 8; s.optR = 12;
  s.posX = 4; s.posW = 6; s.posTop = 90; s.posBottom = 48;
  s.statLines = false;
  return s;
}

// ---------------------------------------------------------------- the home: grid (4")

static inline int perPage(const SquareSpec& S) { return S.cols * S.rows; }

static inline int pageCount(const SquareSpec& S, const Fleet& f) {
  if (S.home != HomeKind::Grid || f.count == 0) return 1;
  return f.agents[f.count - 1].slot / perPage(S) + 1;   // agents are ordered by slot
}

struct GridGeom {
  int x0, y0, cellW, cellH;
  Rect bars;                   // page bars; w = 0 when there are none
};

// wifi: the row under the grid stays, for the Wi-Fi mark, with one page too.
static inline GridGeom gridGeom(const SquareSpec& S, int pages, bool wifi = false) {
  GridGeom g{};
  const int barsH = pages > 1 || wifi ? S.barsH : 0;
  const int top = S.pad, bottom = S.h - S.pad - barsH;
  const int availW = S.w - 2 * S.pad, availH = bottom - top;
  g.cellW = (availW - S.gap * (S.cols - 1)) / S.cols;
  g.cellH = (availH - S.gap * (S.rows - 1)) / S.rows;
  if (pages < 2 && g.cellH > g.cellW) g.cellH = g.cellW;   // square with one page, like the deck
  if (pages < 2 && g.cellW > g.cellH) g.cellW = g.cellH;
  const int gridW = g.cellW * S.cols + S.gap * (S.cols - 1), gridH = g.cellH * S.rows + S.gap * (S.rows - 1);
  g.x0 = (S.w - gridW) / 2;
  g.y0 = top + (availH - gridH) / 2;
  g.bars = barsH ? Rect{0, bottom, S.w, barsH} : Rect{0, 0, 0, 0};
  return g;
}

static inline Rect cellRect(const SquareSpec& S, const GridGeom& g, int cell) {
  const int col = cell % S.cols, row = cell / S.cols;
  return Rect{g.x0 + col * (g.cellW + S.gap), g.y0 + row * (g.cellH + S.gap), g.cellW, g.cellH};
}

static inline int cellAt(const SquareSpec& S, const GridGeom& g, int x, int y) {
  for (int i = 0; i < perPage(S); i++)
    if (cellRect(S, g, i).contains(x, y)) return i;
  return -1;
}

// The agent in a cell of a page (by slot), or -1 for a free cell.
static inline int agentAt(const SquareSpec& S, const Fleet& f, int page, int cell) {
  if (cell < 0 || cell >= perPage(S)) return -1;
  const int slot = page * perPage(S) + cell;
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].slot == slot) return i;
  return -1;
}

// ---------------------------------------------------------------- the home: list (AMOLED)

static inline int listRows(const SquareSpec& S, const Fleet& f) { return (f.count + S.cols - 1) / S.cols; }
static inline int rowPitch(const SquareSpec& S) { return S.tileH + S.gap; }

// How far the list scrolls: until its last row sits fully on screen, on a row.
static inline float maxScroll(const SquareSpec& S, const Fleet& f) {
  const int visible = (S.h - S.headerH + S.gap) / rowPitch(S);   // whole rows on screen
  const int rows = listRows(S, f);
  return rows > visible ? (float)((rows - visible) * rowPitch(S)) : 0.f;
}

static inline Rect listTile(const SquareSpec& S, int i, float scroll) {
  const int x0 = (S.w - S.cols * S.tileW - (S.cols - 1) * S.gap) / 2;
  const int col = i % S.cols, row = i / S.cols;
  return Rect{x0 + col * (S.tileW + S.gap), S.headerH + row * rowPitch(S) - (int)lroundf(scroll), S.tileW, S.tileH};
}

// Where a released list comes to rest: the row nearest to where its momentum
// would carry it.
static inline float settleTarget(const SquareSpec& S, const Fleet& f, float scroll, float velocity) {
  // A flick (faster than 0.5 px/ms) carries on for a quarter second; a slow
  // drag rests on the nearest row.
  const float v = velocity > 0.5f || velocity < -0.5f ? velocity : 0.f;
  float t = scroll + v * 250.f;
  const float mx = maxScroll(S, f);
  if (t < 0) t = 0;
  if (t > mx) t = mx;
  const float pitch = (float)rowPitch(S);
  t = roundf(t / pitch) * pitch;
  return t > mx ? mx : t;
}

constexpr uint32_t kSettleMs = 260, kPosBarHoldMs = 1000, kPosBarFadeMs = 300;

// The position bar's opacity, 0..15.
static inline int posBarAlpha(const PanelUi& ui, uint32_t ms) {
  if (ui.dragging || ui.animating) return 15;
  if (!ui.settledMs) return 0;
  const uint32_t t = ms - ui.settledMs;
  if (t < kPosBarHoldMs) return 15;
  if (t >= kPosBarHoldMs + kPosBarFadeMs) return 0;
  return (int)(15 - (t - kPosBarHoldMs) * 15 / kPosBarFadeMs);
}

// ---------------------------------------------------------------- the "Needs you" screen

// Its spots: one page of the grid, or what the list shows at once (2 x 2 on
// the 1.8", 2 x 3 on the T4-S3).
static inline int squareSpots(const SquareSpec& S) {
  const int n = S.home == HomeKind::Grid ? perPage(S) : S.cols * ((S.h - S.headerH + S.gap) / rowPitch(S));
  return n < kAttMaxSpots ? n : kAttMaxSpots;
}

static inline bool onNeedsYou(const PanelUi& ui) { return ui.needsYou && ui.att; }

// Home's pages slide sideways under the finger (page_slide.h): on a list
// home "Needs you" (0) and the list (1) when the board has both, on a grid
// "Needs you" (when there is one) and every page.
static inline bool hasPager(const SquareSpec& S, const PanelUi& ui, const Fleet& f) {
  return ui.screen == Screen::Home && pagerCount(ui, pageCount(S, f)) > 1;
}

// A list home's pages: 0 = "Needs you" on screen, 1 = the list, between the
// two while they slide (a little past either end while a finger pulls there).
static inline float pagerPos(const PanelUi& ui) {
  if (ui.att && (ui.sliding || ui.slideAnim)) return ui.slide;
  return onNeedsYou(ui) ? 0.f : 1.f;
}

// The settle animations' curve: fast at first, slowing into place.
static inline float easeOut(float p) { return 1 - (1 - p) * (1 - p) * (1 - p); }

// The grid's row under the cells stays with one page on Wi-Fi (the mark's)
// and with the "Needs you" screen (its mark at the left).
static inline GridGeom gridGeomFor(const SquareSpec& S, const PanelUi& ui, int pages) {
  return gridGeom(S, pages, ui.wifi || ui.att);
}

// The page bars: the "Needs you" mark (with a board that has the screen),
// then a bar per page. r[0] is the mark when `mark`. Hits go to the nearest.
struct SquareBars {
  int n = 0;
  bool mark = false;
  Rect r[20];
  int countX = -1;             // the count before the mark, -1 for none
};

static inline SquareBars squareBars(const SquareSpec& S, const GridGeom& g, const PanelUi& ui, int pages) {
  SquareBars b;
  if (!g.bars.w || (pages < 2 && !ui.att)) return b;
  if (pages > 19) pages = 19;
  const bool here = onNeedsYou(ui);
  const int page = here ? -1 : ui.page;
  const int cur = 56, other = 30, h = 8, gap = 12, mh = 10, mw = here ? cur : other;
  const int count = ui.att ? ui.att->count : 0;
  const int countW = ui.att && !here && count > 0 ? (count > 9 ? 2 : 1) * S.label->cellW + 7 : 0;
  int total = 0;
  if (ui.att) total += countW + mw + gap;
  for (int p = 0; p < pages; p++) total += (p == page ? cur : other) + (p ? gap : 0);
  int x = (S.w - total) / 2;
  if (ui.att) {
    if (countW) b.countX = x;
    b.r[b.n++] = Rect{x + countW, g.bars.y + (g.bars.h - mh) / 2, mw, mh};
    b.mark = true;
    x += countW + mw + gap;
  }
  for (int p = 0; p < pages; p++) {
    const int w = p == page ? cur : other;
    b.r[b.n++] = Rect{x, g.bars.y + (g.bars.h - h) / 2, w, h};
    x += w + gap;
  }
  return b;
}

static inline int squareBarAt(const SquareBars& b, int x) {
  int best = -1, bestD = 0;
  for (int i = 0; i < b.n; i++) {
    const int c = b.r[i].x + b.r[i].w / 2, d = x > c ? x - c : c - x;
    if (best < 0 || d < bestD) { best = i; bestD = d; }
  }
  return best;
}

// ---------------------------------------------------------------- card and prompt geometry

// The card's round buttons, left to right: focus, interrupt (running), stop (admin).
struct CardButtons {
  int n = 0;
  IconKind kind[3];
  int cx[3];
  int cy = 0;
};

static inline CardButtons cardButtons(const SquareSpec& S, const Agent& a, DeckScope scope) {
  CardButtons b;
  if (!canAct(scope)) return b;
  b.kind[b.n++] = IconKind::Focus;
  if (a.status == Status::Running) b.kind[b.n++] = IconKind::Interrupt;
  if (scope == DeckScope::Admin) b.kind[b.n++] = IconKind::Stop;
  const int total = b.n * 2 * S.btnR + (b.n - 1) * S.btnGap;
  for (int i = 0; i < b.n; i++) b.cx[i] = (S.w - total) / 2 + S.btnR + i * (2 * S.btnR + S.btnGap);
  b.cy = S.h - S.btnFromBottom;
  return b;
}

// "rotating refresh tokens — short access token…": the title and its hint.
static inline void splitOption(const char* label, char* title, size_t tcap, char* hint, size_t hcap) {
  const char* seps[] = {" \xE2\x80\x94 ", " \xE2\x80\x93 ", " - "};
  for (const char* sep : seps) {
    const char* p = strstr(label, sep);
    if (!p) continue;
    size_t n = (size_t)(p - label);
    if (n >= tcap) n = tcap - 1;
    memcpy(title, label, n);
    title[n] = '\0';
    copyUtf8(title, tcap, title);
    snprintf(hint, hcap, "%s", p + strlen(sep));
    return;
  }
  snprintf(title, tcap, "%s", label);
  hint[0] = '\0';
}

static inline bool isPermission(const Agent& a) { return !strcmp(a.waitKind, "permission"); }

// The prompt's parts: the header line, then the question, the command box and
// the options (pinned to the bottom).
struct PromptGeom {
  int headerH;
  int questionY, questionLines;
  Rect box;                    // the command (permissions); h = 0 for none
  int boxLines;
  int whereY;                  // the "where" line under the box, 0 for none
  int n;                       // options shown
  Rect opt[kMaxOptions];
  bool hints;                  // room for the options' hint lines
};

// How many lines textWrapped() needs for s (up to maxLines).
static inline int wrapLines(const Font& f, const char* s, int maxW, int maxLines) {
  Canvas none{nullptr, 0, 0};
  return textWrapped(none, f, 0, 0, s, 0, maxW, maxLines);
}

static inline bool anyHint(const Agent& a) {
  for (int i = 0; i < a.optCount; i++) {
    char t[96], h[96];
    splitOption(a.opts[i].label, t, sizeof t, h, sizeof h);
    if (h[0]) return true;
  }
  return false;
}

static inline PromptGeom promptGeom(const SquareSpec& S, const Agent& a) {
  PromptGeom g{};
  const int m = S.m;
  g.headerH = 18 + 13 * S.promptScale + 10;
  g.n = a.optCount;
  // Options from the bottom, as tall as the spec allows and the room left;
  // without hint lines they need less.
  const int bottom = S.h - m;
  const int qLine = S.question->cellH - 4;
  const int minTop = g.headerH + qLine + 10;                      // keep at least one question line
  int optH = anyHint(a) ? S.optH : S.optH * 5 / 6;
  if (g.n) {
    const int room = bottom - minTop;
    const int fit = (room - (g.n - 1) * S.optGap) / g.n;
    if (fit < optH) optH = fit;
  }
  g.hints = optH >= S.body->cellH + S.label->cellH - 4;
  const int optTop = g.n ? bottom - g.n * optH - (g.n - 1) * S.optGap : bottom;
  for (int i = 0; i < g.n; i++) g.opt[i] = Rect{m, optTop + i * (optH + S.optGap), S.w - 2 * m, optH};

  // The question: up to three lines, then the command box with what's left.
  g.questionY = g.headerH;
  const int qRoom = (optTop - 12 - g.questionY) / qLine;
  const int qMax = qRoom > 3 ? 3 : qRoom < 1 ? 1 : qRoom;
  g.questionLines = wrapLines(*S.question, a.waitTitle[0] ? a.waitTitle : "needs you", S.w - 2 * m, qMax);
  if (g.questionLines < 1) g.questionLines = 1;
  int y = g.questionY + g.questionLines * qLine + 8;
  if (isPermission(a) && a.waitDetail[0]) {
    const int line = S.body->cellH - 4;
    const int whereH = a.promptCtx[0] ? S.label->cellH + 2 : 0;
    int lines = (optTop - 14 - whereH - y - 16) / line;
    if (lines > 4) lines = 4;
    const int need = wrapLines(*S.body, a.waitDetail, S.w - 2 * m - 24, lines > 0 ? lines : 1);
    if (need < lines) lines = need;
    if (lines >= 1) {
      g.boxLines = lines;
      g.box = Rect{m, y, S.w - 2 * m, lines * line + 16};
      y += g.box.h + 4;
      if (whereH && y + whereH <= optTop - 6) g.whereY = y;
    }
  }
  return g;
}

// ---------------------------------------------------------------- hit-testing

// "Interrupt?" over a card: the whole screen, the agent's name and what
// interrupting does, and two wide buttons well apart (interrupt, orange;
// cancel under it), so a stray tap can't stop an agent at work.
struct ConfirmGeom {
  Rect sheet, yes, no;
  int titleY, lineY;
};

static inline ConfirmGeom confirmGeom(const SquareSpec& S) {
  ConfirmGeom g{};
  const bool big = S.h >= 560;
  const int bh = big ? 80 : 68, gap = big ? 52 : 44, m = S.m + (S.cornerR ? S.cornerR / 3 : 8);
  g.no = Rect{m, S.h - S.m - (S.cornerR ? S.cornerR / 2 : 12) - bh, S.w - 2 * m, bh};
  g.yes = Rect{m, g.no.y - gap - bh, S.w - 2 * m, bh};
  const int block = S.question->cellH + 6 + S.body->cellH;   // the name and the line, in the middle of what is above
  g.titleY = (g.yes.y - block) / 2;
  g.lineY = g.titleY + S.question->cellH + 6;
  g.sheet = Rect{0, 0, S.w, S.h};
  return g;
}

static inline PanelHit squareHit(const SquareSpec& S, const PanelUi& ui, const Fleet& f, int x, int y, DeckScope scope) {
  PanelHit h;
  if (ui.screen == Screen::Home) {
    if (S.home == HomeKind::Grid) {
      const int pages = pageCount(S, f);
      const GridGeom g = gridGeomFor(S, ui, pages);
      const SquareBars bars = squareBars(S, g, ui, pages);
      if (bars.n && g.bars.contains(x, y)) {
        const int k = squareBarAt(bars, x);
        if (bars.mark && k == 0) h.kind = Hit::NeedsYou;
        else { h.kind = Hit::PageBar; h.index = k - (bars.mark ? 1 : 0); }
        return h;
      }
      const int cell = cellAt(S, g, x, y);
      const int i = onNeedsYou(ui) ? attAgent(*ui.att, f, cell) : agentAt(S, f, ui.page < pages ? ui.page : 0, cell);
      if (i >= 0) { h.kind = Hit::Tile; h.agent = i; }
      return h;
    }
    if (y < S.headerH) return h;
    if (onNeedsYou(ui)) {                          // the list's first screen, spot by spot
      for (int s = 0; s < squareSpots(S); s++)
        if (listTile(S, s, 0).contains(x, y)) {
          const int i = attAgent(*ui.att, f, s);
          if (i >= 0) { h.kind = Hit::Tile; h.agent = i; }
          return h;
        }
      return h;
    }
    for (int i = 0; i < f.count; i++)
      if (listTile(S, i, ui.scroll).contains(x, y)) { h.kind = Hit::Tile; h.agent = i; return h; }
    return h;
  }
  const int i = findAgent(f, ui.openId);
  if (i < 0) return h;
  const Agent& a = f.agents[i];
  if (ui.screen == Screen::Card) {
    if (ui.confirm) {                              // only its two buttons answer
      const ConfirmGeom g = confirmGeom(S);
      if (g.yes.contains(x, y)) { h.kind = Hit::ConfirmYes; h.agent = i; }
      else if (g.no.contains(x, y)) { h.kind = Hit::ConfirmNo; h.agent = i; }
      return h;
    }
    // The buttons answer for the whole band under the card's rule, split
    // evenly between them left to right (a corner of the glass counts): the
    // round buttons are drawn as they were.
    const CardButtons b = cardButtons(S, a, scope);
    if (b.n && y >= b.cy - S.btnR - 10) {
      int k = x * b.n / S.w;
      if (k < 0) k = 0;
      if (k >= b.n) k = b.n - 1;
      h.kind = b.kind[k] == IconKind::Focus ? Hit::Focus : b.kind[k] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
      h.agent = i;
    }
    return h;
  }
  const PromptGeom g = promptGeom(S, a);
  for (int k = 0; k < g.n; k++)
    if (g.opt[k].contains(x, y)) { h.kind = Hit::Option; h.agent = i; h.index = k; return h; }
  return h;
}

// ---------------------------------------------------------------- the state machine

constexpr int kSlop = 14, kSwipeMin = 40;
constexpr uint32_t kTapMaxMs = 800;

static inline void squareOpen(PanelUi& ui, const Fleet& f, int i) {
  const Agent& a = f.agents[i];
  snprintf(ui.openId, sizeof ui.openId, "%s", a.id);
  ui.screen = a.status == Status::Waiting ? Screen::Prompt : Screen::Card;
  ui.backAfterToast = false;
}

static inline void squareHome(PanelUi& ui) {
  ui.screen = Screen::Home;
  ui.confirm = false;
  ui.openId[0] = '\0';
  ui.backAfterToast = false;
}

// Keeps the UI valid for a changed fleet.
static inline void squareFix(PanelUi& ui, const SquareSpec& S, const Fleet& f) {
  const int pages = pageCount(S, f);
  if (ui.page >= pages) ui.page = pages - 1;
  if (ui.page < 0) ui.page = 0;
  if (ui.screen != Screen::Home) {
    const int i = findAgent(f, ui.openId);
    if (i < 0) squareHome(ui);
    else if (ui.screen == Screen::Prompt && f.agents[i].status != Status::Waiting && !ui.backAfterToast)
      ui.screen = Screen::Card;                    // answered elsewhere: show the card
    else if (ui.screen == Screen::Card && f.agents[i].status == Status::Waiting)
      ui.screen = Screen::Prompt;                  // it started asking
  }
  if (S.home == HomeKind::List && !ui.dragging && !ui.animating) {
    const float mx = maxScroll(S, f);
    if (ui.scroll > mx) ui.scroll = mx;
    if (ui.scroll < 0) ui.scroll = 0;
  }
  if (!hasPager(S, ui, f)) ui.sliding = ui.slideAnim = false;   // a card opened, or the screen is gone
}

// Lets the list come to rest on a row from where it is.
static inline void listRest(PanelUi& ui, const SquareSpec& S, const Fleet& f, float velocity, uint32_t ms) {
  ui.scrollFrom = ui.scroll;
  ui.scrollTo = settleTarget(S, f, ui.scroll, velocity);
  ui.animStart = ms;
  ui.animating = true;
}

static inline DeckCommand squareTap(PanelUi& ui, const SquareSpec& S, const Fleet& f, const PanelHit& h, DeckScope scope,
                                    uint32_t ms) {
  DeckCommand out;
  switch (h.kind) {
    case Hit::Tile: squareOpen(ui, f, h.agent); break;
    case Hit::PageBar: ui.page = h.index; ui.needsYou = false; break;
    case Hit::NeedsYou: ui.needsYou = ui.att != nullptr; break;
    case Hit::Focus:
      out = commandFor(DeckCmd::Focus, f.agents[h.agent]);
      uiToast(ui, "focus", ms, 1200);
      break;
    case Hit::Interrupt:                           // asks first: interrupting loses the agent's turn
      ui.confirm = true;
      break;
    case Hit::ConfirmYes:
      ui.confirm = false;
      if (f.agents[h.agent].status != Status::Running) { uiToast(ui, "not running any more", ms, 1500); break; }
      out = commandFor(DeckCmd::Interrupt, f.agents[h.agent]);
      uiToast(ui, "interrupted", ms, 1200);
      break;
    case Hit::ConfirmNo:
      ui.confirm = false;
      break;
    case Hit::Stop:
      if (!ui.holdFired) uiToast(ui, "hold to stop", ms, 1500);
      break;
    case Hit::Option: {
      if (!canAct(scope)) { uiToast(ui, "view only: needs scope act", ms); break; }
      if (ui.backAfterToast) break;
      const Agent& a = f.agents[h.agent];
      out = commandFor(DeckCmd::Answer, a);
      snprintf(out.prompt, sizeof out.prompt, "%s", a.promptId);
      snprintf(out.key, sizeof out.key, "%s", a.opts[h.index].key);
      uiToast(ui, "sent", ms, 1000);
      ui.backAfterToast = true;
      break;
    }
    default: break;
  }
  (void)S;
  return out;
}

static inline void squareSwipe(PanelUi& ui, const SquareSpec& S, const Fleet& f, Swipe s) {
  if (ui.confirm) return;                          // answer it first (or BOOT)
  if (ui.screen != Screen::Home) {
    if (s == Swipe::Right) squareHome(ui);
    return;
  }
  if (s == Swipe::Up || s == Swipe::Down) return;  // only a sideways move changes the page
  const bool next = s == Swipe::Left;
  if (onNeedsYou(ui)) {                            // back to the first page (the top of the list)
    if (next) { ui.needsYou = false; ui.page = 0; }
    return;
  }
  if (S.home == HomeKind::Grid) {
    const int pages = pageCount(S, f);
    if (next && ui.page < pages - 1) ui.page++;
    else if (!next && ui.page > 0) ui.page--;
    else if (!next && ui.att) ui.needsYou = true;  // left of the first page
  } else if (s == Swipe::Right && ui.att) {        // the list scrolls up and down; "Needs you" is to its left
    ui.needsYou = true;
  }
}

static inline DeckCommand squareTouch(PanelUi& ui, const SquareSpec& S, const Fleet& f, bool touching, int x, int y,
                                      DeckScope scope, uint32_t ms) {
  DeckCommand out;
  if (ui.att) attSync(*ui.att, f, squareSpots(S), ms);   // the spots the finger lands on are the ones drawn
  const bool list = S.home == HomeKind::List && ui.screen == Screen::Home && !onNeedsYou(ui);   // "Needs you" doesn't scroll
  const bool pager = hasPager(S, ui, f);
  const int pages = pageCount(S, f);
  if (touching) {
    if (!ui.down) {
      const bool sliding = pager && ui.slideAnim;  // the pages are still settling
      ui.down = true;
      ui.dragging = false;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ui.lastMs = ms;
      ui.lastX = x;
      ui.lastY = y;
      ui.velocity = 0;
      ui.holdFired = false;
      ui.caught = (list && ui.animating) || sliding;
      if (list && ui.animating) {                  // catch the moving list
        ui.animating = false;
        ui.settledMs = ms ? ms : 1;
      }
      // Catch the sliding pages where they are: this touch moves them on.
      ui.slide0 = pagerAt(ui);
      ui.slideAnim = false;
      ui.sliding = sliding;
      if (ui.caught) ui.pressed = PanelHit();
      else ui.pressed = squareHit(S, ui, f, x, y, scope);
      ui.scroll0 = ui.scroll;
      if (ui.pressed.kind == Hit::Stop && scope != DeckScope::Admin) ui.pressed = PanelHit();
      return out;
    }
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > kSlop * kSlop) ui.pressed = PanelHit();   // it became a drag
    // One direction to a touch, chosen when it leaves the slop: up and down
    // scrolls the list, sideways slides the pages.
    const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
    if (list && !ui.dragging && !ui.sliding && ady > kSlop && ady >= adx) ui.dragging = true;
    if (pager && !ui.dragging && !ui.sliding && adx > kSlop && swipeAcross(adx, ady)) {
      pagerGrab(ui, x, ms);
    }
    if (ui.sliding) pagerFollow(ui, pages, S.w, x - ui.x0, x, ms);   // the pages follow the finger
    if (ui.dragging) {
      const float mx = maxScroll(S, f);
      float s = ui.scroll0 - dy;
      if (s < 0) s *= 0.35f;                         // a little give past the ends
      if (s > mx) s = mx + (s - mx) * 0.35f;
      ui.scroll = s;
      const uint32_t dt = ms - ui.lastMs;
      if (dt > 0) {
        const float v = -(float)(y - ui.lastY) / dt;
        ui.velocity = ui.velocity * 0.4f + v * 0.6f;
        ui.lastY = y;
        ui.lastMs = ms;
      }
    }
    // Stop: held long enough while still on the button.
    if (ui.pressed.kind == Hit::Stop && !ui.holdFired && (uint32_t)(ms - ui.downMs) >= kStopHoldMs) {
      ui.holdFired = true;
      out = commandFor(DeckCmd::Dismiss, f.agents[ui.pressed.agent]);
      uiToast(ui, "stopping\xE2\x80\xA6", ms, 1500);
    }
    return out;
  }
  if (!ui.down) return out;
  ui.down = false;
  const PanelHit pressed = ui.pressed;
  ui.pressed = PanelHit();
  if (ui.sliding) {                                // the pages: on to the one they settle on
    pagerRelease(ui, pages, S.w, pagerCommitPx(S.w, S.h, S.diagIn), ms);   // past the commit point: the next page
    if (ui.scroll != settleTarget(S, f, ui.scroll, 0)) listRest(ui, S, f, 0, ms);   // a list caught between rows
    return out;
  }
  if (ui.dragging) {
    ui.dragging = false;
    if (ms - ui.lastMs > 80) ui.velocity = 0;       // it stopped before letting go
    listRest(ui, S, f, ui.velocity, ms);
    return out;
  }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (ui.caught) {                                 // it only stopped the list: rest on a row
    listRest(ui, S, f, 0, ms);
  } else if (adx <= kSlop && ady <= kSlop) {
    if (pressed.kind != Hit::None || (uint32_t)(ms - ui.downMs) <= kTapMaxMs)
      out = squareTap(ui, S, f, pressed.kind != Hit::None ? pressed : squareHit(S, ui, f, ui.x0, ui.y0, scope), scope, ms);
  } else if (adx >= kSwipeMin || ady >= kSwipeMin) {
    squareSwipe(ui, S, f, swipeAcross(adx, ady) ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down));
  }
  return out;
}

// Animations and timeouts. True while something moves.
static inline bool squareTick(PanelUi& ui, const SquareSpec& S, const Fleet& f, uint32_t ms) {
  squareFix(ui, S, f);
  if (ui.att) attSync(*ui.att, f, squareSpots(S), ms);
  else ui.needsYou = false;
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) squareHome(ui);
  if (ui.animating) {
    const uint32_t t = ms - ui.animStart;
    if (t >= kSettleMs) {
      ui.scroll = ui.scrollTo;
      ui.animating = false;
      ui.settledMs = ms ? ms : 1;
    } else {
      ui.scroll = ui.scrollFrom + (ui.scrollTo - ui.scrollFrom) * easeOut((float)t / kSettleMs);
    }
  }
  pagerTick(ui, ms);                               // released pages settling
  return ui.dragging || ui.animating || ui.sliding || ui.slideAnim || posBarAlpha(ui, ms) > 0 || (ui.down && ui.pressed.kind == Hit::Stop);
}

static inline void squareResult(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) {
  if (!seq || seq != ui.cmdSeq) return;
  ui.cmdSeq = 0;
  if (code == 204 || code == 200) return;
  if (code == 403) uiToast(ui, "not allowed: scope", ms);
  else if (code == 409) uiToast(ui, "the prompt changed", ms);
  else {
    char b[24];
    snprintf(b, sizeof b, "error %u", (unsigned)code);
    uiToast(ui, b, ms);
  }
  ui.backAfterToast = false;   // stay: show what went wrong
}

// BOOT: back from a card or a prompt. On a grid, the next page, round to the
// "Needs you" screen after the last one, then the first page again; on a
// list, the "Needs you" screen and back.
static inline void squareButton(PanelUi& ui, const SquareSpec& S, const Fleet& f) {
  if (ui.confirm) { ui.confirm = false; return; }   // BOOT is cancel first
  if (ui.screen != Screen::Home) { squareHome(ui); return; }
  if (ui.sliding || ui.slideAnim) {                // pressed while the pages slide: the page changes at once,
    ui.sliding = ui.slideAnim = false;             // and a finger still down does nothing more
    if (ui.down) { ui.caught = true; ui.x0 = ui.x; ui.y0 = ui.y; ui.pressed = PanelHit(); }
  }
  if (onNeedsYou(ui)) { ui.needsYou = false; ui.page = 0; return; }
  if (S.home == HomeKind::Grid) {
    const int pages = pageCount(S, f);
    if (ui.att && ui.page >= pages - 1) ui.needsYou = true;
    else ui.page = (ui.page + 1) % pages;
  } else if (ui.att) {
    ui.needsYou = true;
  }
}

static inline int holdPermille(const PanelUi& ui, uint32_t ms) {
  if (!ui.down || ui.pressed.kind != Hit::Stop) return 0;
  const uint32_t t = ms - ui.downMs;
  return t >= kStopHoldMs ? 1000 : (int)(t * 1000 / kStopHoldMs);
}

// ---------------------------------------------------------------- drawing

namespace square_detail {

// Coverage (0..15) of a corner square of radius r, pixel (i, j) counted out
// from the corner's centre: the same as fillArc gives a quarter disc.
constexpr int kCornerMax = 16;
static inline const uint8_t* cornerCover(int r) {
  static uint8_t table[kCornerMax + 1][kCornerMax * kCornerMax];
  static bool made[kCornerMax + 1];
  if (!made[r]) {
    for (int j = 0; j < r; j++)
      for (int i = 0; i < r; i++)
        table[r][j * kCornerMax + i] = (uint8_t)round_detail::ringCover(sqrtf((i + 0.5f) * (i + 0.5f) + (j + 0.5f) * (j + 0.5f)), 0, (float)r);
    made[r] = true;
  }
  return table[r];
}

// A rounded rectangle, filled or as a `t`-pixel outline.
static inline void roundRect(Canvas& c, int x, int y, int w, int h, int r, uint16_t col) {
  if (r * 2 > h) r = h / 2;
  if (r * 2 > w) r = w / 2;
  c.rect(x + r, y, w - 2 * r, h, col);
  c.rect(x, y + r, r, h - 2 * r, col);
  c.rect(x + w - r, y + r, r, h - 2 * r, col);
  // The four corners from one table of coverage per radius, worked out once
  // (fillArc works out every pixel's distance and angle: most of a list
  // frame's drawing time when every tile has four).
  if (r <= 0) return;
  if (r > kCornerMax) {
    fillArc(c, x + r, y + r, 0, r, 270, 90, col);
    fillArc(c, x + w - r, y + r, 0, r, 0, 90, col);
    fillArc(c, x + w - r, y + h - r, 0, r, 90, 90, col);
    fillArc(c, x + r, y + h - r, 0, r, 180, 90, col);
    return;
  }
  const uint8_t* cov = cornerCover(r);
  const uint16_t full = c.enc(col);
  for (int j = 0; j < r; j++)
    for (int i = 0; i < r; i++) {
      const int a = cov[j * kCornerMax + i];
      if (!a) continue;
      const int xs[2] = {x + r - 1 - i, x + w - r + i}, ys[2] = {y + r - 1 - j, y + h - r + j};
      for (int yy : ys)
        for (int xx : xs) {
          if (xx < 0 || xx >= c.w || yy < c.top || yy >= c.h) continue;
          if (a >= 15) c.px[yy * c.w + xx] = full;
          else round_detail::blendPx(c, xx, yy, col, a);
        }
    }
}

static inline void roundFrame(Canvas& c, int x, int y, int w, int h, int r, int t, uint16_t col) {
  if (r * 2 > h) r = h / 2;
  c.rect(x + r, y, w - 2 * r, t, col);
  c.rect(x + r, y + h - t, w - 2 * r, t, col);
  c.rect(x, y + r, t, h - 2 * r, col);
  c.rect(x + w - t, y + r, t, h - 2 * r, col);
  fillArc(c, x + r, y + r, r - t, r, 270, 90, col);
  fillArc(c, x + w - r, y + r, r - t, r, 0, 90, col);
  fillArc(c, x + w - r, y + h - r, r - t, r, 90, 90, col);
  fillArc(c, x + r, y + h - r, r - t, r, 180, 90, col);
}

// Diagonal accent stripes sliding across a hungry tile (HungryStripes).
static inline void stripes(Canvas& c, const Rect& r, uint16_t fill, uint16_t accent, uint32_t ms, int period, int clipTop) {
  const uint16_t s = blend565(fill, accent, 3);
  const int shift = (int)((ms / 100) % 6) * period / 6;
  const uint16_t fe = c.enc(fill), se = c.enc(s);
  const int half = period / 2;
  for (int yy = 0; yy < r.h; yy++) {
    const int py = r.y + yy;
    if (py < clipTop || py < 0 || py >= c.h) continue;
    uint16_t* row = c.px + (size_t)py * c.w;
    int u = ((yy - shift) % period + period) % period;   // the phase at xx = 0, stepped along the row
    for (int xx = 0; xx < r.w; xx++, u = u + 1 == period ? 0 : u + 1) {
      const int px = r.x + xx;
      if (u < half && px >= 0 && px < c.w && row[px] == fe) row[px] = se;
    }
  }
}

// The 4" tile's border as a context gauge: `pct` of the perimeter, clockwise
// from the top centre, over a faint track (ContextRingPainter).
static inline void ring(Canvas& c, const Rect& r, int t, int pct, uint16_t color, uint16_t track) {
  c.frame(r.x, r.y, r.w, r.h, track, t);
  if (pct <= 0) return;
  const int W = r.w - t, H = r.h - t;
  int left = 2 * (W + H) * (pct > 100 ? 100 : pct) / 100;
  struct Seg { int x, y, dx, dy, len; } segs[] = {
      {r.x + W / 2, r.y, 1, 0, W - W / 2}, {r.x + W, r.y, 0, 1, H}, {r.x + W, r.y + H, -1, 0, W},
      {r.x, r.y + H, 0, -1, H},            {r.x, r.y, 1, 0, W / 2},
  };
  for (const Seg& s : segs) {
    if (left <= 0) break;
    const int n = left < s.len ? left : s.len;
    if (s.dx > 0) c.rect(s.x, s.y, n + t, t, color);
    else if (s.dx < 0) c.rect(s.x - n, s.y, n + t, t, color);
    else if (s.dy > 0) c.rect(s.x, s.y, t, n + t, color);
    else c.rect(s.x, s.y - n, t, n + t, color);
    left -= n;
  }
}

// Where a tile's parts go. The icons (tool chip, subagent critter and its
// count) keep to a band at the top; the critter, the name and the status
// line come below it, so an icon never meets the name.
struct TileParts {
  Rect chip, sub, count, critter, name, meta, bar;
  int scale;
};

static inline TileParts tileParts(const SquareSpec& S, const Rect& r, bool list) {
  TileParts p{};
  const int in = list ? 12 : (r.w * 9 / 110 > 6 ? r.w * 9 / 110 : 6);
  const int chip = list ? 10 : 8;
  p.chip = Rect{r.x + r.w - in - chip, r.y + in, chip, chip};
  p.scale = S.tileScale;
  const int subH = 9 * S.subScale;
  p.sub = Rect{r.x + in - 2, r.y + in - 2, 14 * S.subScale, subH + 2};
  p.count = Rect{p.sub.x + p.sub.w + 2, r.y + in - 2, S.tileMeta->cellW * 3, S.tileMeta->cellH};
  int band = in + (subH > chip ? subH : chip) + 2;
  if (p.count.y + p.count.h > r.y + band) band = p.count.y + p.count.h - r.y;
  const int critterH = 13 * p.scale;
  const int barH = list ? 14 : 0;
  // The critter's box starts below the band (its hat rises one unit into it).
  const int textH = S.tileTitle->cellH + S.tileMeta->cellH;
  const int free = r.h - band - critterH - textH - barH - 6;
  int y;
  if (list) {
    const int gapTop = free > 0 ? free / 3 : 0;
    y = r.y + band + gapTop - p.scale;
    p.critter = Rect{r.x + r.w / 2 - 7 * p.scale, y, 18 * p.scale, critterH};
    y += critterH + (free > 0 ? free / 3 : 0) + 2;
  } else {
    // The deck's tile (Flutter, 170 px): 30 px down to the critter, 14 px from
    // it to the name. Same proportions here, never above the icon band.
    int top = r.h * 30 / 170;
    if (top < band) top = band;
    y = r.y + top;
    p.critter = Rect{r.x + r.w / 2 - 7 * p.scale, y, 18 * p.scale, critterH};
    y += critterH + r.h * 14 / 170;
  }
  p.name = Rect{r.x + in / 2, y, r.w - in, S.tileTitle->cellH};
  y += S.tileTitle->cellH;
  p.meta = Rect{r.x + in / 2, y, r.w - in, S.tileMeta->cellH};
  // A tile with no room to spare (the T4-S3's three rows) keeps its bar lower.
  if (list) p.bar = Rect{r.x + 10, r.y + r.h - (free < 0 ? 10 : 14), r.w - 20, 4};
  return p;
}

static inline void tile(Canvas& c, const SquareSpec& S, const Theme& T, const Agent& a, const Rect& r, uint32_t ms,
                        int64_t now, bool list, bool pressed, int clipTop) {
  Look k = lookOf(T, a);
  if (pressed) { k.fill = T.ink; k.body = k.label = k.meta = k.chip = T.bg; k.stripes = false; }
  const TileParts p = tileParts(S, r, list);
  SQ_T0(t0);
  if (list) square_detail::roundRect(c, r.x, r.y, r.w, r.h, 8, k.fill);
  else c.rect(r.x, r.y, r.w, r.h, k.fill);
  SQ_ADD(tileBg, t0);
  SQ_T0(ts);
  if (k.stripes) stripes(c, r, k.fill, T.accent, ms, list ? 32 : 24, clipTop);
  SQ_ADD(other, ts);
  SQ_T0(t1);
  c.rect(p.chip.x, p.chip.y, p.chip.w, p.chip.h, k.chip);
  if (a.subagents) {
    const Pose sp = a.subTool[0] ? poseOfTool(a.subTool) : Pose::Think;
    drawSprite(c, p.sub.x + 2 - 0, p.sub.y + 2 - 4 * S.subScale + S.subScale, S.subScale, Species::Claude, sp, k.body,
               k.fill, ms, false);
    if (a.subagents > 1) {
      char n[8];
      snprintf(n, sizeof n, "\xC3\x97%u", (unsigned)a.subagents);
      c.text(*S.tileMeta, p.count.x, p.count.y, n, k.meta, p.count.w);
    }
  }
  drawCritter(c, p.critter.x, p.critter.y, p.scale, a, poseOf(a, now), k.body, k.fill, ms, true);
  SQ_ADD(sprite, t1);
  SQ_T0(t2);
  textCentered(c, *S.tileTitle, r.x + r.w / 2, p.name.y, a.title[0] ? a.title : a.id, k.label, p.name.w);
  char meta[80];
  metaLine(a, now, meta, sizeof meta);
  textCentered(c, *S.tileMeta, r.x + r.w / 2, p.meta.y, meta, k.meta, p.meta.w);
  SQ_ADD(text, t2);
  const int pct = a.ctxWindow ? ctxPct(a) : 0;
  if (list) {
    const bool waiting = a.status == Status::Waiting;
    c.rect(p.bar.x, p.bar.y, p.bar.w, p.bar.h, waiting ? blend565(T.accent, T.bg, 8) : T.ringTrack);
    c.rect(p.bar.x, p.bar.y, p.bar.w * pct / 100, p.bar.h, waiting ? T.bg : pressed ? T.bg : T.ink);
  } else if (!pressed) {
    const int t = r.w * 4 / 170 < 2 ? 2 : r.w * 4 / 170;
    ring(c, r, t, pct, k.ring, k.ringTrack);
  }
}

static inline void emptyCell(Canvas& c, const Theme& T, const Rect& r) {
  c.rect(r.x, r.y, r.w, r.h, T.emptyFill);
  c.frame(r.x, r.y, r.w, r.h, T.emptyOutline);
}

// A pill with text, `fill` behind `ink`.
static inline int pillText(Canvas& c, const Font& f, int x, int y, const char* s, uint16_t fill, uint16_t ink, int maxW) {
  const int padX = 12, h = f.cellH + 2;
  int w = textW(f, s) + 2 * padX;
  if (w > maxW) w = maxW;
  fillPill(c, x, y, w, h, fill);
  c.text(f, x + padX, y + 1, s, ink, w - 2 * padX);
  return w;
}

// A toast: in the middle; on a notice, low (bottom = true), clear of its text
// and of the glass's corners.
static inline void toastPill(Canvas& c, const SquareSpec& S, const Theme& T, const char* s, bool bottom = false) {
  const Font& f = *S.label;
  const int room = S.w - 2 * S.m - (bottom ? S.cornerR : 0);
  const int w = textW(f, s) + 32 > room ? room : textW(f, s) + 32;
  const int h = f.cellH + 14, x = (S.w - w) / 2;
  const int y = bottom ? S.h - S.m - h - S.cornerR / 4 : S.h / 2 - h / 2;
  fillPill(c, x, y, w, h, T.panel);
  strokePill(c, x, y, w, h, 2, T.ink);
  c.text(f, x + 16, y + 7, s, T.ink, w - 32);
}

static inline void notice(Canvas& c, const SquareSpec& S, const Theme& T, Pose pose, uint16_t body,
                          const char* title, const char* l1, uint16_t c1, const char* l2, uint16_t c2, const char* l3,
                          uint16_t c3, uint32_t ms) {
  const int s = S.noticeScale;
  const Font& head = *S.question;
  const Font& txt = *S.body;
  const int lines = (l1 ? 1 : 0) + (l2 ? 1 : 0) + (l3 ? 1 : 0);
  int y = S.h / 2 - (13 * s + 24 + head.cellH + lines * (txt.cellH + 6)) / 2;
  drawSprite(c, S.w / 2 - 7 * s, y + s, s, Species::Claude, pose, body, T.bg, ms, true);
  y += 13 * s + 24;
  textCentered(c, head, S.w / 2, y, title, T.ink, S.w - 2 * S.m);
  y += head.cellH + 8;
  const char* ls[3] = {l1, l2, l3};
  const uint16_t cs[3] = {c1, c2, c3};
  for (int i = 0; i < 3; i++) {
    if (!ls[i]) continue;
    const Font& lf = textW(txt, ls[i]) <= S.w - 2 * S.m ? txt : *S.label;
    textCentered(c, lf, S.w / 2, y + (txt.cellH - lf.cellH) / 2, ls[i], cs[i], S.w - 2 * S.m);
    y += txt.cellH + 6;
  }
}

// The list home's header: "agents", and who waits and runs (or the link's trouble).
// The Wi-Fi mark, or the Bluetooth one (round_geom.h's), centred on cx, its base at `base`.
static inline void wifiMark(Canvas& c, const Theme& T, int cx, int base, bool ble = false) {
  if (ble) drawBleMark(c, (float)cx, (float)base, T.dim);
  else drawNetMark(c, (float)cx, (float)base, T.dim);
}

// The list's two views as two small marks over the header, in the middle
// (the header's text leaves no room for them on the 1.8" on Wi-Fi): "Needs
// you" (solid orange while you are on it, an orange outline while someone is
// on it), then the list (bright while you are on it).
// While the pages slide the marks follow: the wide one narrows as the other
// widens, the list's brightens, and "Needs you" fills at halfway.
static inline void listMarks(Canvas& c, const SquareSpec& S, const Theme& T, const PanelUi& ui) {
  float p = pagerPos(ui);                          // 0 = "Needs you", 1 = the list
  p = p < 0 ? 0 : p > 1 ? 1 : p;
  const bool here = p < 0.5f;
  const int cur = 20, other = 10, h = 6, gap = 5, y = 3;
  const int lw = other + (int)lroundf((cur - other) * p), mw = cur + other - lw;
  const int x = (S.w - mw - gap - lw) / 2;
  if (here) fillPill(c, x, y, mw, h, T.accent);
  else strokePill(c, x, y, mw, h, 2, ui.att->count ? T.accent : T.ringTrack);
  fillPill(c, x + mw + gap, y, lw, h, blend565(T.ringTrack, T.ink, (uint8_t)lroundf(p * 15)));
}

static inline void listHeader(Canvas& c, const SquareSpec& S, const Theme& T, const Fleet& f, LinkState link,
                              bool wifi, bool ble = false, const PanelUi* ui = nullptr) {
  c.rect(0, 0, S.w, S.headerH, T.bg);
  const Font& hf = *S.question;
  const int x0 = S.m + S.cornerR / 4;
  const int ty = (S.headerH - hf.cellH) / 2 + 2;
  c.text(hf, x0, ty, "agents", T.ink);
  // On Wi-Fi the mark follows the title, its base about on the title's baseline.
  if (wifi) wifiMark(c, T, x0 + textW(hf, "agents") + 26, ty + hf.cellH * 3 / 4, ble);
  if (ui && ui->att) listMarks(c, S, T, *ui);
  char buf[48];
  if (link == LinkState::Lost) snprintf(buf, sizeof buf, "offline");
  else {
    const Counts n = countFleet(f);
    snprintf(buf, sizeof buf, "%u wait" DECK_DOT "%u run", (unsigned)n.waiting, (unsigned)n.running);
  }
  const Font& rf = *S.hud;
  const int w = textW(rf, buf);
  const Counts n = countFleet(f);
  c.text(rf, S.w - x0 - w, (S.headerH - rf.cellH) / 2 + 3, buf, link == LinkState::Lost || n.waiting ? T.accent : T.dim);
}

// A free spot on the list's "Needs you" screen: the grid's empty cell with the list's corners.
static inline void listEmpty(Canvas& c, const Theme& T, const Rect& r) {
  roundRect(c, r.x, r.y, r.w, r.h, 8, T.emptyFill);
  roundFrame(c, r.x, r.y, r.w, r.h, 8, 1, T.emptyOutline);
}

// The list's "Needs you" page: one screen of spots, no scrolling. dx: how far
// right of its place it is drawn (the pages sliding).
static inline void listSpots(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in, int dx) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  for (int s = 0; s < squareSpots(S); s++) {
    Rect r = listTile(S, s, 0);
    r.x += dx;
    if (r.x + r.w <= 0 || r.x >= S.w) continue;
    const int i = attAgent(*ui.att, f, s);
    if (i >= 0) tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, true, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i, S.headerH);
    else listEmpty(c, T, r);
  }
}

// The list itself, and its position bar, dx to the right of its place.
static inline void listTiles(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in, int dx) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < f.count; i++) {
    Rect r = listTile(S, i, ui.scroll);
    r.x += dx;
    if (r.y + r.h <= S.headerH || r.y >= S.h || r.x + r.w <= 0 || r.x >= S.w) continue;
    tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, true, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i,
         S.headerH);
  }
  // The position bar while the list moves, fading a second after it settles.
  const int alpha = posBarAlpha(ui, in.ms);
  const float mx = maxScroll(S, f);
  if (alpha > 0 && mx > 0) {
    const int top = S.posTop, span = S.h - S.posBottom - S.posTop;
    const int rows = listRows(S, f);
    const int visible = (S.h - S.headerH) / rowPitch(S);
    int len = span * (visible < rows ? visible : rows) / (rows ? rows : 1);
    if (len < 24) len = 24;
    float s = ui.scroll < 0 ? 0 : ui.scroll > mx ? mx : ui.scroll;
    const int y = top + (int)((span - len) * s / mx);
    const uint16_t col = blend565(T.bg, blend565(T.bg, T.ink, 10), (uint8_t)alpha);
    roundRect(c, S.posX + dx, y, S.posW, len, S.posW / 2, col);
  }
}

// The list home: the page that shows, or, while the pages slide, both side by
// side ("Needs you" at the left), each where the slide puts it. Each tile is
// drawn once into the one frame, and the canvas cuts what leaves the screen.
// The header stays in place over them.
// The background of a list home, everywhere but under the tiles (they paint
// their own), so most pixels are written once a frame, not twice. The rounded
// corners blend onto what is under them: their squares get the background too.
static inline void listBackground(Canvas& c, const SquareSpec& S, const Theme& T, const Rect* cov, int n) {
  constexpr int kR = 8;   // the tiles' corner radius (tile(), listEmpty())
  int ys[2 + 2 * 32];
  int ny = 0;
  ys[ny++] = S.headerH;
  ys[ny++] = S.h;
  for (int i = 0; i < n; i++) {
    if (cov[i].y > S.headerH && cov[i].y < S.h) ys[ny++] = cov[i].y;
    if (cov[i].y + cov[i].h > S.headerH && cov[i].y + cov[i].h < S.h) ys[ny++] = cov[i].y + cov[i].h;
  }
  for (int a = 1; a < ny; a++)
    for (int b = a; b > 0 && ys[b - 1] > ys[b]; b--) { const int t = ys[b]; ys[b] = ys[b - 1]; ys[b - 1] = t; }
  for (int k = 0; k + 1 < ny; k++) {
    const int y0 = ys[k], y1 = ys[k + 1];
    if (y1 <= y0) continue;
    int xs[32], xe[32], m = 0;                 // the tiles across this band, by x
    for (int i = 0; i < n && m < 32; i++)
      if (cov[i].y <= y0 && cov[i].y + cov[i].h >= y1) {
        int j = m++;
        for (; j > 0 && xs[j - 1] > cov[i].x; j--) { xs[j] = xs[j - 1]; xe[j] = xe[j - 1]; }
        xs[j] = cov[i].x;
        xe[j] = cov[i].x + cov[i].w;
      }
    int x = 0;
    for (int j = 0; j < m; j++) {
      if (xs[j] > x) c.rect(x, y0, xs[j] - x, y1 - y0, T.bg);
      if (xe[j] > x) x = xe[j];
    }
    if (x < S.w) c.rect(x, y0, S.w - x, y1 - y0, T.bg);
  }
  for (int i = 0; i < n; i++) {
    const Rect& r = cov[i];
    c.rect(r.x, r.y, kR, kR, T.bg);
    c.rect(r.x + r.w - kR, r.y, kR, kR, T.bg);
    c.rect(r.x, r.y + r.h - kR, kR, kR, T.bg);
    c.rect(r.x + r.w - kR, r.y + r.h - kR, kR, kR, T.bg);
  }
}

static inline void listHome(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  const PanelUi& ui = *in.ui;
  const int listX = S.w - (int)lroundf(pagerPos(ui) * S.w);   // 0 with the list on screen, S.w with "Needs you"
  {                                            // where the tiles go this frame (as listSpots and listTiles place them)
    Rect cov[32];
    int n = 0;
    if (ui.att && listX > 0)
      for (int s = 0; s < squareSpots(S) && n < 32; s++) {
        Rect r = listTile(S, s, 0);
        r.x += listX - S.w;
        if (r.x + r.w > 0 && r.x < S.w) cov[n++] = r;
      }
    if (listX < S.w)
      for (int i = 0; i < in.fleet->count && n < 32; i++) {
        Rect r = listTile(S, i, ui.scroll);
        r.x += listX;
        if (r.y + r.h > S.headerH && r.y < S.h && r.x + r.w > 0 && r.x < S.w) cov[n++] = r;
      }
    listBackground(c, S, T, cov, n);
  }
  if (ui.att && listX > 0) listSpots(c, S, T, in, listX - S.w);
  if (listX < S.w) listTiles(c, S, T, in, listX);
  listHeader(c, S, T, *in.fleet, in.link, ui.wifi, ui.ble, &ui);
}

static inline void pageBars(Canvas& c, const SquareSpec& S, const Theme& T, const Fleet& f, int page, int pages,
                            const GridGeom& g, const PanelUi& ui) {
  const SquareBars b = squareBars(S, g, ui, pages);
  if (!b.n) return;
  bool hot[64] = {};
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].status == Status::Waiting && f.agents[i].slot / perPage(S) < 64) hot[f.agents[i].slot / perPage(S)] = true;
  const bool here = onNeedsYou(ui);
  if (b.mark) {                                    // "Needs you": solid orange while you are on it, an orange
    const Rect m = b.r[0];                         // outline (and the count) while someone is on it
    if (here) fillPill(c, m.x, m.y, m.w, m.h, T.accent);
    else strokePill(c, m.x, m.y, m.w, m.h, 2, ui.att->count ? T.accent : T.ringTrack);
    if (b.countX >= 0) {
      char n[4];
      snprintf(n, sizeof n, "%d", ui.att->count > 99 ? 99 : ui.att->count);
      c.text(*S.label, b.countX, m.y + m.h / 2 - S.label->cellH / 2, n, T.accent);
    }
  }
  for (int k = b.mark ? 1 : 0; k < b.n; k++) {
    const int p = k - (b.mark ? 1 : 0);
    const Rect r = b.r[k];
    fillPill(c, r.x, r.y, r.w, r.h, !here && p == page ? T.ink : (p < 64 && hot[p]) ? T.accent : T.ringTrack);
  }
}

static inline void gridHome(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  const int pages = pageCount(S, f);
  const int page = ui.page < pages ? ui.page : pages - 1;
  const GridGeom g = gridGeomFor(S, ui, pages);
  for (int cell = 0; cell < perPage(S); cell++) {
    const Rect r = cellRect(S, g, cell);
    const int i = onNeedsYou(ui) ? attAgent(*ui.att, f, cell) : agentAt(S, f, page, cell);
    if (i >= 0) tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, false, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i, 0);
    else emptyCell(c, T, r);
  }
  pageBars(c, S, T, f, page, pages, g, ui);
  if (ui.wifi && g.bars.w) wifiMark(c, T, g.x0 + S.cols * g.cellW + (S.cols - 1) * S.gap - 18, g.bars.y + g.bars.h / 2 + 6, ui.ble);
  if (in.link == LinkState::Lost) {
    const Font& lf = *S.label;
    const char* s = "deck offline" DECK_DOT "retrying";
    const int w = textW(lf, s) + 36, h = lf.cellH + 14;
    const int x = (S.w - w) / 2, y = S.h - S.pad - h - 24;
    fillPill(c, x, y, w, h, T.panel);
    strokePill(c, x, y, w, h, 2, T.accent);
    c.text(lf, x + 18, y + 7, s, T.accent);
  }
}

// "84k", "1.5k", "3.1m" (compactCount in the Flutter deck).
static inline void compact(uint64_t n, char* out, size_t cap) {
  if (n < 1000) { snprintf(out, cap, "%u", (unsigned)n); return; }
  const double v = n < 999500 ? n / 1000.0 : n / 1000000.0;
  const char* unit = n < 999500 ? "k" : "m";
  if (v < 10) {
    snprintf(out, cap, "%.1f%s", v, unit);
    char* dot = strstr(out, ".0");
    if (dot) memmove(dot, dot + 2, strlen(dot + 2) + 1);
  } else {
    snprintf(out, cap, "%d%s", (int)(v + 0.5), unit);
  }
}

// The card's pill: "running · 38m" (agentPillLabel).
static inline void pillLabel(const Agent& a, int64_t now, char* out, size_t cap) {
  char age[8];
  fmtAge(a.statusSince ? now - a.statusSince : 0, age, sizeof age);
  switch (a.status) {
    case Status::Running:  snprintf(out, cap, "running" DECK_DOT "%s", age); break;
    case Status::Waiting:  snprintf(out, cap, "needs you" DECK_DOT "%s", age); break;
    case Status::Idle:     snprintf(out, cap, "%s" DECK_DOT "%s", a.unseen ? "hungry" : "idle", age); break;
    case Status::Error:    snprintf(out, cap, "error" DECK_DOT "%s", age); break;
    case Status::Starting: snprintf(out, cap, "starting"); break;
    case Status::Exited:   snprintf(out, cap, "%s", a.lost ? "lost" : "exited"); break;
    default:               snprintf(out, cap, "%s", age);
  }
}

// "edit · session.ts", "done", the error...
static inline void doingNow(const Agent& a, char* out, size_t cap) {
  switch (a.status) {
    case Status::Running: {
      char verb[80];
      metaLine(a, 0, verb, sizeof verb);           // "edit · 41% ctx": take the verb
      char* dot = strstr(verb, DECK_DOT);
      if (dot) *dot = '\0';
      if (a.actDetail[0]) {
        const char* base = strrchr(a.actDetail, '/');
        snprintf(out, cap, "%s" DECK_DOT "%s", verb, base && base[1] ? base + 1 : a.actDetail);
      } else {
        snprintf(out, cap, "%s", a.actTool[0] ? verb : "thinking");
      }
      return;
    }
    case Status::Waiting:  snprintf(out, cap, "%s", a.waitTitle[0] ? a.waitTitle : "needs you"); return;
    case Status::Error:    snprintf(out, cap, "%s", a.errMsg[0] ? a.errMsg : "error"); return;
    case Status::Starting: snprintf(out, cap, "starting"); return;
    case Status::Exited:   snprintf(out, cap, "%s", a.resumable ? "ended" DECK_DOT "resumable" : "exited"); return;
    default:               snprintf(out, cap, "done"); return;
  }
}

// Where the card's parts go, top to bottom: the header (the critter, and the
// name over the pill as one block centred on the critter's box), "doing now"
// (with the subagent count on its right), the working subagents one to a row,
// context, the numbers, the last prompt (if a line of it fits) and the
// buttons' rule. Live subagents come before the last prompt: on a short panel
// the prompt gives way to them.
struct CardGeom {
  Rect critter;                // the critter's 18 x 13 unit box
  Rect name, pill;             // the header's text block: name, then the pill under it
  Rect pet;                    // then the pet's name, stage and level (h 0: no pet)
  int doingY, doingValueY;
  int subY, subPitch, subRows; // rows of working subagents (subRows may be fewer than kept)
  int ctxY, statsY;
  int lastY, lastLines;        // lastLines 0: no room for the last prompt
  int ruleY;
};

static inline int cardStatsH(const SquareSpec& S) {
  const Font& lf = *S.label;
  return S.statLines ? 4 * (S.value->cellH - 4) + 8 : lf.cellH - 6 + S.value->cellH + 4;
}

// How far below a line's top its capitals start (Roboto Mono: the baseline at
// 0.795 of the cell, capitals 0.711 em tall, 0.6 em to a character).
static inline int capTop(const Font& f) { return (f.cellH * 795 + 500) / 1000 - (f.cellW * 711 + 300) / 600; }

// The middle of what a critter draws in its box (scale s), from its hat (or
// head) to its feet, standing still: the block and the onigiri, with and
// without a hat, differ, and so does a pet. Measured on a small drawing at scale 2.
static inline int critterMid(const Rect& box, int s, const Agent& a) {
  constexpr int kS = 2, kW = 26 * kS, kH = 30 * kS, kX = 5 * kS, kY = 12 * kS;
  constexpr uint16_t kBlank = 0x1234;
  static uint16_t px[kW * kH];
  for (int i = 0; i < kW * kH; i++) px[i] = kBlank;
  Canvas c{px, kW, kH};
  drawCritter(c, kX, kY, kS, a, Pose::Idle, 0xFFFF, 0x0001, 0, false);
  int top = -1, bot = -1;
  for (int y = 0; y < kH; y++)
    for (int x = 0; x < kW; x++)
      if (px[y * kW + x] != kBlank) { if (top < 0) top = y; bot = y + 1; break; }
  if (top < 0) return box.y + box.h / 2;
  return box.y + ((top - kY) + (bot - kY)) * s / (2 * kS);
}

static inline CardGeom cardGeom(const SquareSpec& S, const Agent& a, DeckScope scope) {
  CardGeom g{};
  const int m = S.m, s = S.cardScale;
  const Font& nf = *S.title;
  const Font& lf = *S.label;
  const Font& bf = *S.body;
  g.critter = Rect{m, 20, 18 * s, 13 * s};
  const int tx = m + 18 * s + 14, tw = S.w - tx - m;
  // The name and the pill as one block, from the name's capitals to the
  // pill's foot, its middle on the drawn critter's.
  const int pillH = lf.cellH + 2, gap = 2;
  const int petH = a.pet.has ? lf.cellH - 2 : 0;
  const int cap = capTop(nf);
  const int blockH = nf.cellH - cap + gap + pillH + (petH ? gap + petH : 0);
  const int top = critterMid(g.critter, s, a) - blockH / 2 - cap;
  g.name = Rect{tx, top, tw, nf.cellH};
  g.pill = Rect{tx, top + nf.cellH + gap, tw, pillH};
  g.pet = Rect{tx, g.pill.y + g.pill.h + gap, tw, petH};
  int y = (petH ? g.pet.y + g.pet.h : g.pill.y + g.pill.h) + 14;
  if (y < g.critter.y + g.critter.h + 8) y = g.critter.y + g.critter.h + 8;

  g.doingY = y;
  y += lf.cellH - 4;
  g.doingValueY = y;
  y += bf.cellH + 2;

  const CardButtons b = cardButtons(S, a, scope);
  g.ruleY = b.n ? b.cy - S.btnR - 10 : S.h - m;
  const int bottom = g.ruleY - 4;
  const int ctxH = S.question->cellH + 2 + S.ctxTickH + 12;
  // Subagent rows: as many as are kept and fit above context and the numbers.
  g.subPitch = lf.cellH - 4;
  g.subY = y;
  const int room = bottom - y - ctxH - cardStatsH(S);
  g.subRows = a.subCount < room / g.subPitch ? a.subCount : room / g.subPitch;
  if (g.subRows < 0) g.subRows = 0;
  if (!a.subagents) g.subRows = 0;
  y += g.subRows * g.subPitch;

  g.ctxY = y;
  y += ctxH;
  g.statsY = y;
  y += cardStatsH(S);
  g.lastY = y;
  const int line = bf.cellH - 2;
  g.lastLines = (bottom - 4 - (y + lf.cellH - 4)) / line;   // its descenders clear of the rule
  if (g.lastLines < 0) g.lastLines = 0;
  return g;
}

// "3 subagents", "1 subagent".
static inline void subCountLabel(const Agent& a, char* out, size_t cap) {
  snprintf(out, cap, "%u subagent%s", (unsigned)a.subagents, a.subagents == 1 ? "" : "s");
}

static inline void card(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  const int m = S.m, s = S.cardScale;
  char buf[224];
  c.fill(T.bg);
  const CardGeom g = cardGeom(S, a, in.scope);
  // Header: the critter, and beside it the name over the pill, centred on it.
  const Look k = lookOf(T, a);
  const uint16_t body = a.status == Status::Running || hungry(a) ? T.accent : k.body == T.bg ? T.ink : k.body;
  drawCritter(c, g.critter.x, g.critter.y, s, a, poseOf(a, in.nowEpoch), body, T.bg, in.ms, true);
  const Font& nf = *S.title;
  c.text(nf, g.name.x, g.name.y, a.title[0] ? a.title : a.id, T.ink, g.name.w);
  pillLabel(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Error || hungry(a);
  pillText(c, *S.label, g.pill.x, g.pill.y, buf, loud ? T.accent : T.ink, T.bg, g.pill.w);
  if (g.pet.h) {
    petLine(a, buf, sizeof buf);
    c.text(*S.label, g.pet.x, g.pet.y, buf, T.dim, g.pet.w);
  }

  const Font& lf = *S.label;
  const Font& bf = *S.body;
  const int lw = S.w - 2 * m;
  int y = g.doingY;
  // Doing now, and how many subagents work for it on the right.
  int cw = 0;
  if (a.subagents) {
    subCountLabel(a, buf, sizeof buf);
    cw = textW(lf, buf);
    c.text(lf, S.w - m - cw, y, buf, T.dim);
  }
  c.text(lf, m, y, "doing now", T.dim, lw - cw - 8);
  doingNow(a, buf, sizeof buf);
  c.text(bf, m, g.doingValueY, buf, T.ink, lw);

  // The working subagents: a small critter posed by its latest tool, its task, its type.
  for (int i = 0; i < g.subRows; i++) {
    const Subagent& sa = a.subs[i];
    const int ss = S.subScale, ry = g.subY + i * g.subPitch;
    const Pose p = sa.tool[0] ? poseOfTool(sa.tool) : Pose::Think;
    // The body (units 0..9 of the box) in the middle of the row.
    drawSprite(c, m, ry + (g.subPitch - 9 * ss) / 2 - 4 * ss, ss, Species::Claude, p, body, T.bg, in.ms, false);
    const int x = m + 14 * ss + 10, w = S.w - m - x;
    const char* title = sa.title[0] ? sa.title : sa.type[0] ? sa.type : "subagent";
    const int ty = ry + (g.subPitch - lf.cellH) / 2 + 1;
    const int used = c.text(lf, x, ty, title, T.ink, w);
    if (sa.title[0] && sa.type[0]) {
      snprintf(buf, sizeof buf, DECK_DOT "%s", sa.type);
      if (used + textW(lf, buf) <= w) c.text(lf, x + used, ty, buf, T.dim);
    }
  }
  y = g.ctxY;

  // Context: label, numbers, percent, and the dotted bar.
  const int pct = ctxPct(a);
  int cx = m + c.text(*S.question, m, y, "context", T.ink) + 10;
  if (a.ctxWindow) {
    char u[16], w[16];
    compact(a.ctxUsed, u, sizeof u);
    compact(a.ctxWindow, w, sizeof w);
    snprintf(buf, sizeof buf, "%s / %s", u, w);
  } else {
    snprintf(buf, sizeof buf, "--");
  }
  c.text(lf, cx, y + (S.question->cellH - lf.cellH) / 2 + 1, buf, T.dim);
  snprintf(buf, sizeof buf, a.ctxWindow ? "%d%%" : "--", pct);
  c.text(*S.question, S.w - m - textW(*S.question, buf), y, buf, T.ink);
  y += S.question->cellH + 2;
  {
    const int ticks = (lw + S.ctxPitch - S.ctxTickW) / S.ctxPitch;
    const int fillW = a.ctxWindow ? lw * pct / 100 : 0;
    for (int i = 0; i < ticks; i++) {
      const int x = m + i * S.ctxPitch;
      if (x + S.ctxTickW <= m + fillW) continue;
      c.rect(x, y, S.ctxTickW, S.ctxTickH, T.dim);
    }
    if (fillW) c.rect(m, y + (S.ctxTickH - S.ctxFillH) / 2, fillW, S.ctxFillH, pct >= 85 ? T.accent : T.ink);
    y += S.ctxTickH + 12;
  }

  // Tokens, cost, turns: one to a line on a tall panel, else three columns.
  if (S.statLines) {
    const char* labels[4] = {"tokens out", "cache read", "cost", "turns"};
    char vals[4][16];
    compact(a.tokensOut, vals[0], sizeof vals[0]);
    compact(a.cacheRead, vals[1], sizeof vals[1]);
    if (a.costUsd > 0) snprintf(vals[2], sizeof vals[2], "~$%.2f", (double)a.costUsd);
    else snprintf(vals[2], sizeof vals[2], "\xE2\x80\x94");
    snprintf(vals[3], sizeof vals[3], "%u", a.turns);
    const Font& vf = *S.value;
    const int pitch = vf.cellH - 4;
    for (int i = 0; i < 4; i++) {
      c.text(lf, m, y + (vf.cellH - lf.cellH) / 2 + 1, labels[i], T.dim, lw / 2);
      c.text(vf, S.w - m - textW(vf, vals[i]), y, vals[i], T.ink, lw / 2);
      y += pitch;
    }
    y += 8;
  } else {
    const int colW = lw / 3;
    const char* labels[3] = {"tokens", "cost", "turns"};
    char vals[3][16];
    compact(a.tokensOut, vals[0], sizeof vals[0]);
    if (a.costUsd > 0) snprintf(vals[1], sizeof vals[1], "$%.2f", (double)a.costUsd);
    else snprintf(vals[1], sizeof vals[1], "\xE2\x80\x94");
    snprintf(vals[2], sizeof vals[2], "%u", a.turns);
    for (int i = 0; i < 3; i++) {
      c.text(lf, m + i * colW, y, labels[i], T.dim, colW - 4);
      c.text(*S.value, m + i * colW, y + lf.cellH - 6, vals[i], T.ink, colW - 4);
    }
    y += lf.cellH - 6 + S.value->cellH + 4;
  }

  // The last prompt, as many lines as fit above the buttons' rule.
  const CardButtons b = cardButtons(S, a, in.scope);
  if (g.lastLines > 0) {
    c.text(lf, m, g.lastY, "last prompt", T.dim, lw);
    const int line = bf.cellH - 2;
    textWrapped(c, bf, m, g.lastY + lf.cellH - 4, a.lastPrompt[0] ? a.lastPrompt : "\xE2\x80\x94", T.ink, lw,
                g.lastLines, line);
  }

  // The buttons.
  if (b.n) {
    const PanelUi& ui = *in.ui;
    for (int i = 0; i < b.n; i++) {
      const Hit hk = b.kind[i] == IconKind::Focus ? Hit::Focus : b.kind[i] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
      const bool pressed = ui.pressed.kind == hk;
      iconButton(c, b.cx[i], b.cy, S.btnR, b.kind[i], pressed, hk == Hit::Stop ? holdPermille(ui, in.ms) : 0, T.bg, T.ink,
                 T.accent, T.line);
    }
  }
}

// The sheet of "interrupt?" (confirmGeom), over the card.
static inline void confirmSheet(Canvas& c, const SquareSpec& S, const Theme& T, const Agent& a, const PanelUi& ui) {
  using namespace square_detail;
  const ConfirmGeom g = confirmGeom(S);
  c.fill(T.bg);
  // The agent, and what interrupting does; the orange button says the verb.
  textCentered(c, *S.question, S.w / 2, g.titleY, a.title[0] ? a.title : a.id, T.ink, S.w - 2 * g.yes.x);
  textCentered(c, *S.body, S.w / 2, g.lineY, "stops and waits for you", T.dim, S.w - 2 * g.yes.x);
  const bool yesDown = ui.pressed.kind == Hit::ConfirmYes, noDown = ui.pressed.kind == Hit::ConfirmNo;
  roundRect(c, g.yes.x, g.yes.y, g.yes.w, g.yes.h, g.yes.h / 2, yesDown ? T.focusedAccent : T.accent);
  textCentered(c, *S.question, S.w / 2, g.yes.y + (g.yes.h - S.question->cellH) / 2 + 1, "interrupt", T.bg, g.yes.w - 24);
  roundRect(c, g.no.x, g.no.y, g.no.w, g.no.h, g.no.h / 2, noDown ? T.ringTrack : T.panel);
  roundFrame(c, g.no.x, g.no.y, g.no.w, g.no.h, g.no.h / 2, 2, T.ringTrack);
  textCentered(c, *S.question, S.w / 2, g.no.y + (g.no.h - S.question->cellH) / 2 + 1, "cancel", T.ink, g.no.w - 24);
}

static inline void prompt(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  const bool perm = isPermission(a);
  const uint16_t bg = perm ? T.accent : T.bg;
  const uint16_t fg = perm ? T.bg : T.ink;
  c.fill(bg);
  if (!perm) {
    if (S.cornerR) {
      // The orange border follows the glass.
      roundFrame(c, 2, 2, S.w - 4, S.h - 4, S.cornerR - 2, 3, T.accent);
    } else {
      c.frame(0, 0, S.w, S.h, T.accent, 3);
    }
  }
  const PromptGeom g = promptGeom(S, a);
  const int m = S.m;
  char buf[224];
  // Header: ×2 critter, the name, the age in a pill on the right.
  const int ps = S.promptScale;
  const int hy = 18;
  drawCritter(c, m + (S.cornerR ? 6 : 0), hy, ps, a, Pose::Wait, perm ? T.bg : T.dim, bg, in.ms, false);
  char age[8];
  fmtAge(a.statusSince ? in.nowEpoch - a.statusSince : 0, age, sizeof age);
  const Font& lf = *S.label;
  const int pw = textW(lf, age) + 24;
  const int px = S.w - m - pw - (S.cornerR ? 6 : 0);
  const int nameX = m + (S.cornerR ? 6 : 0) + 14 * ps + 8;
  const int nameY = hy + 13 * ps / 2 + ps * 2 - S.question->cellH / 2;
  c.text(*S.question, nameX, nameY, a.title[0] ? a.title : a.id, fg, px - 8 - nameX);
  pillText(c, lf, px, nameY + (S.question->cellH - lf.cellH - 2) / 2, age, perm ? T.bg : T.accent, perm ? T.ink : T.bg, pw);

  // The question, whole (up to three lines).
  const int qLine = S.question->cellH - 4;
  textWrapped(c, *S.question, m, g.questionY, a.waitTitle[0] ? a.waitTitle : "needs you", fg, S.w - 2 * m,
              g.questionLines, qLine);
  // The command (permissions), and where it runs.
  if (g.box.h) {
    roundRect(c, g.box.x, g.box.y, g.box.w, g.box.h, 8, T.bg);
    textWrapped(c, *S.body, g.box.x + 12, g.box.y + 8, a.waitDetail, T.ink, g.box.w - 24, g.boxLines, S.body->cellH - 4);
  }
  if (g.whereY) c.text(lf, m, g.whereY, a.promptCtx, T.bg, S.w - 2 * m);

  // The options.
  if (!g.n) {
    textCentered(c, *S.body, S.w / 2, S.h - m - S.body->cellH - 8, "answer in the terminal", fg, S.w - 2 * m);
    return;
  }
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < g.n; i++) {
    const Rect& r = g.opt[i];
    const Option& o = a.opts[i];
    const bool pressed = ui.pressed.kind == Hit::Option && ui.pressed.index == i;
    uint16_t fill, edge, num, ttl, hnt;
    if (pressed) {
      fill = edge = T.ink;
      num = ttl = hnt = T.bg;
    } else if (perm) {
      const bool primary = o.primary;
      fill = primary ? T.bg : T.accent;
      edge = T.bg;
      num = ttl = primary ? T.accent : T.bg;
      hnt = primary ? T.dim : blend565(T.accent, T.bg, 9);
    } else {
      fill = T.panel;
      edge = T.ringTrack;
      num = ttl = T.ink;
      hnt = T.dim;
    }
    roundRect(c, r.x, r.y, r.w, r.h, S.optR, edge);
    roundRect(c, r.x + 2, r.y + 2, r.w - 4, r.h - 4, S.optR - 2, fill);
    if (perm && !pressed && !o.primary) {
      roundRect(c, r.x + 3, r.y + 3, r.w - 6, r.h - 6, S.optR - 3, fill);   // a 3 px outline on orange
    }
    const Font& nfnt = *S.value;
    const int numW = nfnt.cellW + 22;
    c.text(nfnt, r.x + 14, r.y + (r.h - nfnt.cellH) / 2 + 1, o.key, num, numW - 14);
    char title[96], hint[96];
    splitOption(o.label, title, sizeof title, hint, sizeof hint);
    const int tx = r.x + 14 + numW, tw = r.w - (tx - r.x) - 14;
    if (g.hints && hint[0]) {
      const int th = S.body->cellH - 6 + lf.cellH - 4;
      const int ty = r.y + (r.h - th) / 2 - 1;
      c.text(*S.body, tx, ty, title, ttl, tw);
      c.text(lf, tx, ty + S.body->cellH - 6, hint, hnt, tw);
    } else {
      c.text(*S.body, tx, r.y + (r.h - S.body->cellH) / 2 + 1, title, ttl, tw);
    }
  }
  (void)buf;
}

// The rounded glass: outside the corner arcs is black (what the panel shows
// anyway; drawn so the host pictures look like the board).
static inline void cornerMask(Canvas& c, int r) {
  for (int dy = 0; dy < r; dy++) {
    const float yy = (float)(r - dy) - 0.5f;
    const float inside = sqrtf((float)(r * r) - yy * yy);
    const int cut = r - (int)(inside + 0.5f);
    if (cut <= 0) continue;
    c.rect(0, dy, cut, 1, 0);
    c.rect(c.w - cut, dy, cut, 1, 0);
    c.rect(0, c.h - 1 - dy, cut, 1, 0);
    c.rect(c.w - cut, c.h - 1 - dy, cut, 1, 0);
  }
}

}  // namespace square_detail

// The whole frame.
static inline void drawSquare(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  using namespace square_detail;
  // A list home paints its own background around its tiles (listBackground);
  // every other screen starts from a cleared frame.
  const bool listNext = S.home == HomeKind::List && in.link != LinkState::Connecting && in.link != LinkState::Unapproved &&
                        in.fleet && in.fleet->count > 0 && in.ui && in.ui->screen == Screen::Home;
  SQ_T0(tf);
  if (!listNext) c.fill(T.bg);
  SQ_ADD(fill, tf);
  char buf[96];
  const char* name = in.device && in.device[0] ? in.device : "<name>";
  const PanelUi* tu = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0 ? in.ui : nullptr;
  switch (in.link) {
    case LinkState::Connecting:
      if (in.net && netNoticeShown(*in.net)) {   // where Wi-Fi or Bluetooth is, a passkey (net_model.h's words)
        const NetNotice n = netNotice(*in.net);
        notice(c, S, T, n.problem ? Pose::Exit : Pose::Think, T.dim, n.title, n.line1, n.problem || n.code ? T.accent : T.dim,
               n.line2, T.dim, nullptr, T.dim, in.ms);
      } else {
        notice(c, S, T, Pose::Think, T.dim, "connecting to the deck", "plugged into the Mac?", T.dim,
               "agentctl link add", T.ink, "/dev/cu.usbmodem\xE2\x80\xA6", T.dim, in.ms);
      }
      if (tu) toastPill(c, S, T, tu->toast, true);
      if (S.cornerR) cornerMask(c, S.cornerR);
      return;
    case LinkState::Unapproved:
      snprintf(buf, sizeof buf, "approve %s", name);
      if (textW(*S.body, buf) <= S.w - 2 * S.m)
        notice(c, S, T, Pose::Wait, T.ink, "waiting for approval", "on the Mac:", T.dim, "agentctl devices", T.ink, buf,
               T.accent, in.ms);
      else                                          // a long name gets a line of its own
        notice(c, S, T, Pose::Wait, T.ink, "waiting for approval", "on the Mac:", T.dim, "agentctl devices approve",
               T.ink, name, T.accent, in.ms);
      if (tu) toastPill(c, S, T, tu->toast, true);
      if (S.cornerR) cornerMask(c, S.cornerR);
      return;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0) {
    if (in.link == LinkState::Lost) {
      char seen[16];
      fmtAge(in.silentSec, seen, sizeof seen);
      snprintf(buf, sizeof buf, "last seen %s ago", seen);
      notice(c, S, T, Pose::Exit, T.dim, "deck offline", buf, T.dim, "retrying\xE2\x80\xA6", T.dim, "agentctl serve", T.ink,
             in.ms);
    } else {
      notice(c, S, T, Pose::Idle, T.dim, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr, T.dim,
             "agentctl new", T.ink, nullptr, T.ink, in.ms);
    }
    if (tu) toastPill(c, S, T, tu->toast, true);
    if (S.cornerR) cornerMask(c, S.cornerR);
    return;
  }
  const PanelUi& ui = *in.ui;
  const int open = ui.screen != Screen::Home ? findAgent(*in.fleet, ui.openId) : -1;
  if (open >= 0 && ui.screen == Screen::Card) {
    card(c, S, T, in, in.fleet->agents[open]);
    if (ui.confirm) {
      confirmSheet(c, S, T, in.fleet->agents[open], ui);
      if (S.cornerR) cornerMask(c, S.cornerR);
    }
  }
  else if (open >= 0) prompt(c, S, T, in, in.fleet->agents[open]);
  else if (S.home == HomeKind::List) listHome(c, S, T, in);
  else gridHome(c, S, T, in);
  if (tu) toastPill(c, S, T, ui.toast);
  if (S.cornerR) cornerMask(c, S.cornerR);
}

// The square boards' PanelLayout.
#include "control_panel.h"
#include "fun_motion.h"

class SquarePanel : public PanelLayout {
 public:
  explicit SquarePanel(const SquareSpec& s) : S(s), T(deckTheme(s.trueBlack)), G(controlGeom(s.w, s.h, s.m, s.diagIn, s.imu)) {}
  ~SquarePanel() override {
    free(homePic);
    free(panelPic);
    free(rowTable);
    free(edgeRow);
    free(scratch);
  }
  const uint16_t* const* frameRows() const override { return rowsReady ? rowTable : nullptr; }
  const FrameSplit* frameSplit() const override { return pageSlide.split(); }
  SquarePanel(const SquarePanel&) = delete;
  SquarePanel& operator=(const SquarePanel&) = delete;
  int width() const override { return S.w; }
  int height() const override { return S.h; }
  bool trueBlack() const override { return S.trueBlack; }
  // The board's panel (control_panel.h): pulled down from the header on a
  // list board, from the top strip on a grid (controlEdgeTouch).
  bool hasPanel() const { return true; }
  void draw(Canvas& c, const PanelInput& in) const override {
    if (in.ui && in.fleet && (S.imu || in.ui->fun.role == 1)) {
      // The trick's empty grid with the board's panel over it (pulled down
      // or open): drawn whole each frame, the grid stays under it.
      if (in.ctl && hasPanel() && controlShown(in.ui->ctl) && (magicGrid(in) || giverScreen(in))) {
        const ControlUi& u = in.ui->ctl;
        rowsReady = false;
        if ((u.pulling || u.animating) && slideFromCache(c, in, u)) return;   // moving: slid, as over home
        cached = false;
        if (giverScreen(in)) drawGiver(c, in);
        else drawReveal(c, in);
        controlDraw(c, G, T, *in.ctl, in.ui->ctl, in.ms);
        if (S.cornerR) square_detail::cornerMask(c, S.cornerR);
        return;
      }
      if (drawFun(c, in)) return;
    }
    if (slideHome(c, in)) return;                 // home's pages under the finger: cut from two pictures
    const ControlUi* u = in.ui && in.ctl && hasPanel() ? &in.ui->ctl : nullptr;
    // While the panel moves, home and the panel are each drawn once, then slid
    // (a copy of rows a frame): the motion costs the send, not two drawings.
    rowsReady = false;
    if (u && (u->pulling || u->animating) && slideFromCache(c, in, *u)) return;
    cached = false;
    const bool covered = u && u->open >= 0.999f && !u->animating && !u->pulling;
    if (!covered) drawSquare(c, S, T, in);   // all the way down, what is under it doesn't show
    if (u && controlShown(*u)) {
      controlDraw(c, G, T, *in.ctl, *u, in.ms);
      if (S.cornerR) square_detail::cornerMask(c, S.cornerR);
    }
  }
  // The motion tricks (fun_motion.h): a knock reveals the agents when the
  // trick is armed; a shake on an agent's card makes its critter sick.
  void motion(PanelUi& ui, float ax, float ay, float az, uint32_t ms) const override {
    if (!S.imu) return;
    const Motion m = funDetect(ui.fun, ax, ay, az, ms);
    if (m == Motion::Knock && ui.fun.magic && !ui.fun.spent && ui.screen == Screen::Home && !funSick(ui.fun, ms) && !controlShown(ui.ctl)) {   // on the grid only: knock 1 takes, knock 2 gives back (a shake on a card starts with a jolt too)
      funKnock(ui.fun, ms);
      ui.fun.stashDue = true;                  // every agent leaves the other screens, or comes back
      if (!ui.fun.taken) {                     // given back: from wherever it was, to the grid the tiles leave
        ui.screen = Screen::Home;
        ui.confirm = false;
      }
    }
    else if (m == Motion::Shake && ui.screen == Screen::Card && !funSick(ui.fun, ms) && !ui.confirm && !controlShown(ui.ctl))
      funSickStart(ui.fun, ui.openId, ms);
  }
  bool panelTouch(PanelUi& ui, bool down, int x, int y, uint32_t ms) const override {
    if (!hasPanel()) return false;
    if (S.home == HomeKind::Grid) {                // tiles reach the top: a tap there stays the tile's
      const bool was = ui.ctl.down;
      const bool took = controlEdgeTouch(ui.ctl, G, down, x, y, ms, S.h / 8);
      if (took && !was) { ui.down = false; ui.pressed = PanelHit(); }   // the screen lets go of the finger
      return took;
    }
    // From the top edge of any screen: home, an agent's card, a prompt, a notice.
    const bool mayBegin = !ui.down && y < S.headerH;
    return controlTouch(ui.ctl, G, down, x, y, ms, mayBegin);
  }
  DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope, uint32_t ms) const override {
    if (S.imu && ui.fun.magic && !ui.fun.taken) return DeckCommand{};   // the empty grid: nothing to touch
    if (ui.fun.role == 1 && ui.fun.given && ui.screen == Screen::Home) return DeckCommand{};   // the giver's emptied grid
    if (S.imu && funSick(ui.fun, ms)) return DeckCommand{};
    return squareTouch(ui, S, f, down, x, y, scope, ms);
  }
  bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const override {
    bool fun = false;
    if (S.imu) {
      (void)f;
      if (ui.fun.sickAt && !funSick(ui.fun, ms)) ui.fun.sickAt = 0;
      if (ui.fun.revealAt && !funRevealing(ui.fun, ms)) ui.fun.revealAt = 0;
      if (ui.fun.returnAt && !funReturning(ui.fun, ms)) ui.fun.returnAt = 0;
      if (ui.fun.backAt && !funBack(ui.fun, ms)) ui.fun.backAt = 0;
      if (ui.fun.giveAt && !funGiving(ui.fun, ms)) ui.fun.giveAt = 0;   // given back: the empty grid stays until the drawer switches it off
      fun = funBusy(ui.fun, ms);
    }
    const bool panel = (hasPanel() && controlTick(ui.ctl, ms)) || fun;
    return squareTick(ui, S, f, ms) || panel;
  }
  void button(PanelUi& ui, const Fleet& f) const override {
    if (S.imu && ui.fun.magic && !ui.fun.taken) return;   // the empty grid: BOOT has nothing to do
    if (hasPanel() && controlShown(ui.ctl)) {   // BOOT closes the panel first
      ui.ctl = ControlUi();
      return;
    }
    squareButton(ui, S, f);
  }
  void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const override { squareResult(ui, seq, code, ms); }
  const SquareSpec S;
  const Theme T;
  const ControlGeom G;

 private:
  // The sick critter's pose: from its place on the card to the middle and back.
  SickPose sickPose(const PanelUi& ui, const Fleet& f, uint32_t ms) const {
    const int i = findAgent(f, ui.fun.sickId);
    Rect from{S.w / 2 - 9 * S.cardScale, 20, 0, 0};
    if (i >= 0) from = square_detail::cardGeom(S, f.agents[i], DeckScope::Act).critter;
    return funSickPose(ui.fun, S.w, S.h, from.x, from.y, S.cardScale, ms);
  }

  // The magic trick on a list home: an empty grid while it is armed (no
  // header, no agents); then the tiles on screen fly in from the top-right
  // corner one after another, growing from kStart of their size, drawn and
  // animated as they go (each into a scratch tile, then scaled into place).
  void drawReveal(Canvas& c, const PanelInput& in) const {
    using namespace square_detail;
    const FunUi& f = in.ui->fun;
    const PanelUi& ui = *in.ui;
    c.fill(T.bg);
    const uint16_t spot = rgb565(0x77, 0x74, 0x6C);   // the empty places: light grey
    for (int sp = 0; sp < squareSpots(S); sp++) {
      const Rect r = listTile(S, sp, 0);
      if (r.y < S.h) roundRect(c, r.x, r.y, r.w, r.h, 8, spot);
    }
    listHeader(c, S, T, *in.fleet, in.link, ui.wifi, ui.ble, nullptr);
    if (f.revealAt || f.returnAt) {
      const bool out = f.returnAt != 0;           // giving them back: the same flight, backwards
      const int tw = S.tileW, th = S.tileH;
      if (!scratch) scratch = (uint16_t*)malloc((size_t)tw * th * 2);
      constexpr float kStart = 0.55f;
      int k = 0;
      for (int i = 0; i < in.fleet->count; i++) {
        const Rect r = listTile(S, i, ui.scroll);
        if (r.y + r.h <= S.headerH || r.y >= S.h) continue;
        float t = funRevealT(f, k++, in.ms);
        if (out) t = 1.f - t;
        if (t <= 0) continue;
        if (t >= 1 || !scratch) {
          tile(c, S, T, in.fleet->agents[i], r, in.ms, in.nowEpoch, true, false, S.headerH);
          continue;
        }
        Canvas sc{scratch, tw, th};
        sc.be = c.be;
        sc.fill(T.bg);
        tile(sc, S, T, in.fleet->agents[i], Rect{0, 0, tw, th}, in.ms, in.nowEpoch, true, false, 0);
        const float scale = kStart + (1.f - kStart) * t;
        // In from past its arrive corner; given back, out past its leave corner (Settings, Magic trick).
        float sx0, sy0;
        flightCorner(out ? f.leave : f.arrive, kStart, sx0, sy0);
        const float dx = sx0 + (r.x - sx0) * t, dy = sy0 + (r.y - sy0) * t;
        const int dw = (int)(tw * scale), dh = (int)(th * scale);
        for (int yy = 0; yy < dh; yy++) {
          const int py = (int)dy + yy;
          if (py < 0 || py >= c.h) continue;
          const uint16_t* src = scratch + (size_t)(yy * th / dh) * tw;
          uint16_t* dst = c.px + (size_t)py * c.w;
          for (int xx = 0; xx < dw; xx++) {
            const int px = (int)dx + xx;
            if (px >= 0 && px < c.w) dst[px] = src[xx * tw / dw];
          }
        }
      }
    }
    if (S.cornerR) cornerMask(c, S.cornerR);
  }

  // Just past one of the screen's corners, off the glass, for a tile at scale k.
  void flightCorner(uint8_t corner, float k, float& x, float& y) const {
    const bool left = corner == 0 || corner == 2, top = corner == 0 || corner == 1;
    x = left ? -S.tileW * k - 8 : S.w + 8;
    y = top ? -S.tileH * k - 8 : S.h + 8;
  }

  // One tile (w x h from src, rows src_stride apart) at scale, its top-left at (dx, dy).
  static void blitScaled(Canvas& c, const uint16_t* src, int stride, int w, int h, float scale, float dx, float dy) {
    const int dw = (int)(w * scale), dh = (int)(h * scale);
    if (dw <= 0 || dh <= 0) return;
    for (int yy = 0; yy < dh; yy++) {
      const int py = (int)dy + yy;
      if (py < 0 || py >= c.h) continue;
      const uint16_t* s = src + (size_t)(yy * h / dh) * stride;
      uint16_t* d = c.px + (size_t)py * c.w;
      for (int xx = 0; xx < dw; xx++) {
        const int px = (int)dx + xx;
        if (px >= 0 && px < c.w) d[px] = s[xx * w / dw];
      }
    }
  }

  // The giver's screen while its agents are taken, and their flights: out to
  // its leave corner from the picture it showed, back in from its arrive
  // corner as they are now.
  bool giverScreen(const PanelInput& in) const {
    const FunUi& f = in.ui->fun;
    return f.role == 1 && S.home == HomeKind::List && in.ui->screen == Screen::Home && (f.given || funBack(f, in.ms));
  }

  void drawGiver(Canvas& c, const PanelInput& in) const {
    using namespace square_detail;
    const FunUi& f = in.ui->fun;
    const PanelUi& ui = *in.ui;
    c.fill(T.bg);
    const uint16_t spot = rgb565(0x77, 0x74, 0x6C);
    for (int sp = 0; sp < squareSpots(S); sp++) {
      const Rect r = listTile(S, sp, 0);
      if (r.y < S.h) roundRect(c, r.x, r.y, r.w, r.h, 8, spot);
    }
    listHeader(c, S, T, *in.fleet, in.link, ui.wifi, ui.ble, nullptr);
    constexpr float kEnd = 0.55f;
    if (f.given && funGiving(f, in.ms) && f.pic) {           // out, from the picture
      for (int k = 0; k < f.picN; k++) {
        const float t = funFlightT(f.giveAt, k, in.ms);
        if (t >= 1) continue;
        float ex, ey;
        flightCorner(f.leave, kEnd, ex, ey);
        const float x = f.picX[k] + (ex - f.picX[k]) * t, y = f.picY[k] + (ey - f.picY[k]) * t;
        blitScaled(c, f.pic + (size_t)f.picY[k] * c.w + f.picX[k], c.w, S.tileW, S.tileH, 1.f - (1.f - kEnd) * t, x, y);
      }
    } else if (!f.given && funBack(f, in.ms)) {               // back in, as they are now
      const int tw = S.tileW, th = S.tileH;
      if (!scratch) scratch = (uint16_t*)malloc((size_t)tw * th * 2);
      int k = 0;
      for (int i = 0; i < in.fleet->count; i++) {
        const Rect r = listTile(S, i, ui.scroll);
        if (r.y + r.h <= S.headerH || r.y >= S.h) continue;
        const float t = funFlightT(f.backAt, k++, in.ms);
        if (t <= 0) continue;
        if (t >= 1 || !scratch) {
          tile(c, S, T, in.fleet->agents[i], r, in.ms, in.nowEpoch, true, false, S.headerH);
          continue;
        }
        Canvas sc{scratch, tw, th};
        sc.be = c.be;
        sc.fill(T.bg);
        tile(sc, S, T, in.fleet->agents[i], Rect{0, 0, tw, th}, in.ms, in.nowEpoch, true, false, 0);
        float sx, sy;
        flightCorner(f.arrive, kEnd, sx, sy);
        blitScaled(c, scratch, tw, tw, th, kEnd + (1.f - kEnd) * t, sx + (r.x - sx) * t, sy + (r.y - sy) * t);
      }
    }
    if (S.cornerR) cornerMask(c, S.cornerR);
  }

  // The tiles on screen now (list home): their agents' indexes and places.
  int shownTiles(const PanelUi& ui, const Fleet& f, int* idx, int16_t* x, int16_t* y, int max) const override {
    if (S.home != HomeKind::List || ui.screen != Screen::Home) return 0;
    int n = 0;
    for (int i = 0; i < f.count && n < max; i++) {
      const Rect r = listTile(S, i, ui.scroll);
      if (r.y < S.headerH || r.y + r.h > S.h) continue;   // whole tiles only
      idx[n] = i;
      x[n] = (int16_t)r.x;
      y[n] = (int16_t)r.y;
      n++;
    }
    return n;
  }

  // The magic trick's own screen: the grid, empty, or the tiles flying.
  bool magicGrid(const PanelInput& in) const {
    const FunUi& f = in.ui->fun;
    return S.home == HomeKind::List && in.ui->screen == Screen::Home && f.magic && (!f.taken || funRevealing(f, in.ms));
  }

  // The tricks over the frame: true when this drew the whole frame.
  bool drawFun(Canvas& c, const PanelInput& in) const {
    const FunUi& f = in.ui->fun;
    if (giverScreen(in)) {
      drawGiver(c, in);
      return true;
    }
    if (magicGrid(in)) {
      drawReveal(c, in);
      return true;
    }
    if (!funSick(f, in.ms)) return false;
    const int i = findAgent(*in.fleet, f.sickId);
    if (i < 0 || in.ui->screen != Screen::Card) return false;
    const Agent& a = in.fleet->agents[i];
    const SickPose p = sickPose(*in.ui, *in.fleet, in.ms);
    drawSquare(c, S, T, in);                   // the card stays under it
    {                                          // its own place on the card is empty while it is out: body,
      const square_detail::CardGeom cg = square_detail::cardGeom(S, a, in.scope);   // legs, hat and its bubble,
      const Rect from = cg.critter;            // up to the name beside it
      const int k = S.cardScale;
      int right = from.x + 20 * k;
      if (cg.name.x > from.x && right > cg.name.x - 2) right = cg.name.x - 2;
      if (a.pet.has) c.rect(from.x - 7 * k, from.y - 12 * k, right - (from.x - 7 * k), 28 * k, T.bg);   // a pet's reach
      else c.rect(from.x - k, from.y - 3 * k, right - (from.x - k), 17 * k, T.bg);
    }
    c.dim50();                                 // the card behind it at half brightness
    // Crossed eyes while it is sick (the error pose's), its own colours.
    drawCritter(c, (int)p.x, (int)p.y, p.scale, a, p.dizzy ? Pose::Err : Pose::Run, T.accent, T.bg, in.ms, false);
    funPour(c, p, f, T.bg, in.ms);
    if (S.cornerR) square_detail::cornerMask(c, S.cornerR);
    return true;
  }

  // The two pictures the panel slides between, a frame each (in PSRAM on a
  // board: malloc puts blocks this big there). Made the first time the panel
  // moves; nullptr if there is no room (then every frame is drawn).
  mutable uint16_t* homePic = nullptr;
  mutable uint16_t* panelPic = nullptr;
  mutable bool cached = false;
  mutable uint16_t* scratch = nullptr;   // one tile, for the magic trick's flying tiles
  // The moving frame as rows of those pictures, for a board that sends rows.
  mutable const uint16_t** rowTable = nullptr;
  mutable uint16_t* edgeRow = nullptr;
  mutable bool rowsReady = false;

  // Home's pages while they slide (page_slide.h): each page drawn once into
  // a picture, then every frame cut from the two either side of the finger.
  // The pictures have no glass corners (the seam would show them) and no
  // toast; on the canvas the list's header, the toast and the corners go on
  // over the pages each frame.
  mutable PageSlide pageSlide;
  bool slideHome(Canvas& c, const PanelInput& in) const {
    using namespace square_detail;
    const bool moving = in.ui && in.fleet && in.fleet->count > 0 && pagerMoving(*in.ui) && in.ui->screen == Screen::Home &&
                        (in.link == LinkState::Live || in.link == LinkState::Lost) && !(in.ctl && controlShown(in.ui->ctl));
    if (!moving) {
      pageSlide.reset();
      return false;
    }
    rowsReady = false;
    const PanelUi& ui = *in.ui;
    SquareSpec flat = S;
    flat.cornerR = 0;
    const int count = pagerCount(ui, pageCount(S, *in.fleet));
    const bool ok = pageSlide.slide(c, pagerAt(ui), count, in.splitOk, T.bg, [&](Canvas& k, int i) {
      PanelUi u = ui;
      u.sliding = u.slideAnim = false;
      u.toast[0] = '\0';
      u.pressed = PanelHit();
      pagerGo(u, i);
      PanelInput pi = in;
      pi.ui = &u;
      drawSquare(k, flat, T, pi);
    });
    if (!ok) return false;
    if (pageSlide.split()) return true;
    if (S.home == HomeKind::List) listHeader(c, S, T, *in.fleet, in.link, ui.wifi, ui.ble, &ui);
    if (ui.toast[0] && (int32_t)(ui.toastUntil - in.ms) > 0) toastPill(c, S, T, ui.toast);
    if (S.cornerR) cornerMask(c, S.cornerR);
    return true;
  }

  bool slideFromCache(Canvas& c, const PanelInput& in, const ControlUi& u) const {
    const size_t n = (size_t)S.w * S.h;
    if (c.top != 0 || c.w != S.w || c.h != S.h) return false;
    if (!homePic) homePic = (uint16_t*)malloc(n * 2);
    if (!panelPic) panelPic = (uint16_t*)malloc(n * 2);
    if (!homePic || !panelPic) return false;
    if (!cached) {
      if (in.ui && giverScreen(in)) drawGiver(c, in);       // under the panel: the trick's grids
      else if (S.imu && in.ui && magicGrid(in)) drawReveal(c, in);
      else drawSquare(c, S, T, in);
      memcpy(homePic, c.px, n * 2);
      ControlUi full = u;
      full.open = 1.f;
      full.pulling = full.animating = full.down = false;
      full.pressed = ControlHit::None;
      controlDraw(c, G, T, *in.ctl, full, in.ms);
      if (S.cornerR) square_detail::cornerMask(c, S.cornerR);
      memcpy(panelPic, c.px, n * 2);
      cached = true;
    }
    const int edge0 = (int)lroundf(u.open * (float)S.h);
    if (in.rowsOk) {                     // nothing to copy: the board sends these rows as they are
      if (!rowTable) rowTable = (const uint16_t**)malloc(sizeof(uint16_t*) * S.h);
      if (!edgeRow) {
        edgeRow = (uint16_t*)malloc((size_t)S.w * 2);
        if (edgeRow) for (int x = 0; x < S.w; x++) edgeRow[x] = c.enc(T.ringTrack);
      }
      if (rowTable && edgeRow) {
        for (int y = 0; y < S.h; y++)
          rowTable[y] = y < edge0 ? panelPic + (size_t)(S.h - edge0 + y) * S.w
                      : (y < edge0 + 2 && edge0 > 0 && edge0 < S.h) ? edgeRow
                      : homePic + (size_t)y * S.w;
        rowsReady = true;
        return true;
      }
    }
    // The panel's bottom edge at y = edge: above it the panel's last rows, a
    // 2-pixel line, then home.
    const int edge = (int)lroundf(u.open * (float)S.h);
    const size_t row = (size_t)S.w * 2;
    for (int y = 0; y < S.h; y++) {
      uint16_t* dst = c.px + (size_t)y * S.w;
      if (y < edge) memcpy(dst, panelPic + (size_t)(S.h - edge + y) * S.w, row);
      else if (y < edge + 2 && edge > 0 && edge < S.h) for (int x = 0; x < S.w; x++) dst[x] = c.enc(T.ringTrack);
      else memcpy(dst, homePic + (size_t)y * S.w, row);
    }
    if (S.cornerR) square_detail::cornerMask(c, S.cornerR);
    return true;
  }
};
