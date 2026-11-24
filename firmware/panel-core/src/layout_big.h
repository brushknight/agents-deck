#pragma once
// The big panel layout: the Flutter desk panel's UI (clients/flutter/
// agents_ui: AgentsApp, AgentTile, DeckGrid, agent_detail.dart) drawn at a
// scale. The desk panel is a 4" 720 x 720 screen; a board of the same size
// at 480 x 480 gets every Flutter size times 2/3 (specBig480()), with the
// nearest bitmap fonts and whole-number critter scales. A 720 x 720 board
// would use k = 1 and larger fonts.
//
//   home    the deck's 4 x 4 grid of square tiles by slot, paged, with the
//           page bars under it (AgentTile, DeckGrid, _PageBars); a pet is
//           drawn small enough that all it reaches stays in its tile
//   card    the agent page with its pet (agent_detail_pets.dart): the head
//           (pet, status chip, hearts and level, name, session); doing now,
//           or the status and the account; the last prompt, or the last
//           answer once it has finished; context and where it runs; back,
//           focus terminal, then interrupt while running, otherwise stop
//           ("again" arms it for 3 s), or resume and remove once exited. An
//           egg ready to hatch sits in a box that says "tap to hatch" (HatchPet).
//   prompt  a permission on orange, a question dark with an orange edge: the
//           pet boxed with its "?", what it asks, the command, every answer as
//           a row (tap one to choose it, tap it again or send to send); back
//           and focus terminal at the foot
//   states  waiting for approval, connecting, deck offline, no agents; on
//           Wi-Fi, where it is (joining, connecting, wrong password, network
//           not found, deck not reachable, certificate rejected)
//   Wi-Fi   on Wi-Fi a small mark at the right of the page bars' row, which
//           then stays with one page too
//   attn    the "Needs you" screen (attention.h), one page left of the first:
//           the same grid, the deck's spot n in cell n; its mark sits at the left of the
//           page bars (whose row then stays with one page too)
//
// Back: swipe right, or BOOT. Pure C++ (no Arduino).
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "canvas.h"
#include "deck_cmd.h"
#include "deck_ui.h"
#include "font_big.h"
#include "font_data.h"
#include "font_square.h"
#include "model.h"
#include "page_slide.h"
#include "panel_layout.h"
#include "round_geom.h"
#include "sprites.h"
#include "view.h"

struct BigSpec {
  const char* name;
  int w, h;
  float k;                     // Flutter pixels to board pixels
  // Fonts for the Flutter sizes (at k): 30 bold, 22-24 bold, 19-20, 16-18, 13-15, 12.5.
  const Font *title, *heading, *body, *text, *label, *tileTitle, *tileMeta;
  int tileScale, subScale, cardScale, crewScale, noticeScale;
};

// A 4" 480 x 480 board: the desk panel at 2/3.
static inline BigSpec specBig480() {
  BigSpec s{};
  s.name = "big480";
  s.w = 480; s.h = 480; s.k = 480.f / 720.f;
  s.title = &FONT_B20;       // rm 30 bold  -> 20
  s.heading = &FONT_B15;     // rm 22-24 bold -> 15-16
  s.body = &FONT_M13;        // rm 20, buttons 19 -> 13
  s.text = &FONT_S;          // rm 17, mono 16-18 -> 11-12
  s.label = &FONT_XS;        // mono 13-15 -> 9-10
  s.tileTitle = &FONT_S;     // mono 16 -> 10.7
  s.tileMeta = &FONT_XS;     // mono 12.5 -> 8.3 (10: the smallest that reads)
  s.tileScale = 3;           // 92 px on a 170 px cell -> 61 px: x3 is the nearest (54)
  s.subScale = 1;            // 36 px -> 24 px
  s.cardScale = 2;           // 54 px -> 36 px: exactly x2
  s.crewScale = 1;           // 36 px -> 24 px
  s.noticeScale = 5;         // 126 px -> 84 px: x5 (90)
  return s;
}

// A Flutter length at the spec's scale.
static inline int bk(const BigSpec& S, float v) { return (int)lroundf(v * S.k); }

// ---------------------------------------------------------------- text

// A font's size in pixels (Roboto Mono advances 0.6 em).
static inline int fontPx(const Font& f) { return (int)lroundf(f.cellW / 0.6f); }

// Text whose em box (Flutter's height-1.0 line) starts at `top`.
static inline int textAt(Canvas& c, const Font& f, int x, int top, const char* s, uint16_t col, int maxW = -1) {
  return c.text(f, x, top - (f.cellH - fontPx(f)) / 2, s, col, maxW);
}

// ---------------------------------------------------------------- the grid

static inline int bigPages(const Fleet& f) { return f.count ? f.agents[f.count - 1].slot / 16 + 1 : 1; }

struct BigGrid {
  int x0, y0, cell, cellH, gap;
  Rect bars;                   // w = 0 with one page
};

// DeckGrid: square cells with an 8 px gap inside 8 px padding; with more than
// one page the bars take 34 px at the bottom and the cells get shorter. On
// Wi-Fi (wifi) that row stays with one page too: the Wi-Fi mark sits in it.
static inline BigGrid bigGrid(const BigSpec& S, int pages, bool wifi = false) {
  BigGrid g{};
  const int pad = bk(S, 8), barsH = pages > 1 || wifi ? bk(S, 34) : 0;
  g.gap = bk(S, 8);
  const int availW = S.w - 2 * pad, availH = S.h - pad - (barsH ? 0 : pad) - barsH;
  g.cell = (availW - 3 * g.gap) / 4;
  g.cellH = (availH - 3 * g.gap) / 4;
  if (pages < 2 && g.cellH > g.cell) g.cellH = g.cell;
  if (g.cell > g.cellH && pages < 2) g.cell = g.cellH;
  const int gw = 4 * g.cell + 3 * g.gap, gh = 4 * g.cellH + 3 * g.gap;
  g.x0 = (S.w - gw) / 2;
  g.y0 = pad + (availH - gh) / 2;
  g.bars = barsH ? Rect{0, S.h - barsH, S.w, barsH} : Rect{0, 0, 0, 0};
  return g;
}

static inline Rect bigCell(const BigGrid& g, int cell) {
  return Rect{g.x0 + (cell % 4) * (g.cell + g.gap), g.y0 + (cell / 4) * (g.cellH + g.gap), g.cell, g.cellH};
}

static inline int bigAgentAt(const Fleet& f, int page, int cell) {
  if (cell < 0 || cell >= 16) return -1;
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].slot == page * 16 + cell) return i;
  return -1;
}

// The page bars' row stays with one page on Wi-Fi (the mark's) and with the
// "Needs you" screen (its mark at the left).
static inline bool bigRow(const PanelUi& ui) { return ui.wifi || ui.att; }

static inline BigGrid bigGridFor(const BigSpec& S, const PanelUi& ui, int pages) { return bigGrid(S, pages, bigRow(ui)); }

// What a home cell holds: the deck's page by slot, or on the "Needs you"
// screen the agent in that spot. -1 for a free cell.
static inline int bigCellAgent(const PanelUi& ui, const Fleet& f, int page, int cell) {
  if (ui.needsYou && ui.att) return attAgent(*ui.att, f, cell);
  return bigAgentAt(f, page, cell);
}

// _PageBars: the "Needs you" mark (with a board that has the screen), then a
// bar per page. r[0] is the mark when `mark`. Hits go to the nearest one.
struct BigBars {
  int n = 0;
  bool mark = false;
  Rect r[20];
  int countX = -1;             // the count before the mark ("3"), -1 for none
};

static inline BigBars bigBars(const BigSpec& S, const BigGrid& g, const PanelUi& ui, int pages) {
  BigBars b;
  if (!g.bars.w || (pages < 2 && !ui.att)) return b;
  if (pages > 19) pages = 19;
  const bool here = ui.needsYou && ui.att;
  const int page = here ? -1 : ui.page;
  const int cur = bk(S, 56), other = bk(S, 30), h = bk(S, 8), pad = bk(S, 6);
  const int mh = bk(S, 10), mw = here ? cur : other;
  const int count = ui.att ? ui.att->count : 0;
  const int countW = ui.att && !here && count > 0 ? (count > 9 ? 2 : 1) * S.label->cellW + bk(S, 7) : 0;
  int total = 0;
  if (ui.att) total += bk(S, 6) + countW + mw + bk(S, 12);
  for (int p = 0; p < pages; p++) total += (p == page ? cur : other) + 2 * pad;
  int x = (S.w - total) / 2;
  if (ui.att) {
    if (countW) b.countX = x + bk(S, 6);
    b.r[b.n++] = Rect{x + bk(S, 6) + countW, g.bars.y + (g.bars.h - mh) / 2, mw, mh};
    b.mark = true;
    x += bk(S, 6) + countW + mw + bk(S, 12);
  }
  for (int p = 0; p < pages; p++) {
    const int w = p == page ? cur : other;
    b.r[b.n++] = Rect{x + pad, g.bars.y + (g.bars.h - h) / 2, w, h};
    x += w + 2 * pad;
  }
  return b;
}

// The bar under x: the nearest one (the row is the touch target).
static inline int bigBarAt(const BigBars& b, int x) {
  int best = -1, bestD = 0;
  for (int i = 0; i < b.n; i++) {
    const int d = abs(x - (b.r[i].x + b.r[i].w / 2));
    if (best < 0 || d < bestD) { best = i; bestD = d; }
  }
  return best;
}

// ---------------------------------------------------------------- the agent's page
// The agent's own screens on the 4": the desk panel's agent page with its pet
// (agent_detail_pets.dart) at 2/3, in the firmware's Roboto Mono sizes. The
// buttons are sharp; only the page has round corners (a question's orange edge).
//
//   card        the head: the pet on its baseline (or, an egg ready to hatch,
//               boxed with "tap to hatch"), the status chip with hearts, level
//               and its bar in the corner, the name and the session across the
//               column; doing now with the command and two subagents in a box, or
//               the status with the account on the right; the last prompt, or
//               the last answer once it has finished; context; where it runs;
//               back, focus terminal, and interrupt, stop, resume or remove
//   permission  the page orange: the head with the pet boxed, "it asks", the
//               question, the command, and every answer as a row (tap one to
//               choose it, tap it again to send); back, "or answer at the
//               terminal" and focus terminal
//   question    dark with an orange edge: the head, the question, its detail,
//               the answers as rows; back, send and focus terminal

namespace big_ui {   // the desk panel's numbers at 2/3
constexpr int kPadX = 24, kPadTop = 21, kPadBot = 20;      // the page's padding (36, 32, 30)
constexpr int kZoneW = 100, kZoneH = 85;                    // the pet's zone (150 x 128)
constexpr int kBoxW = 96, kBoxH = 88, kCtaH = 17;           // a boxed pet (144 x 132), "tap to hatch" (26)
constexpr int kCell = 4;                                    // pet cells (6)
constexpr int kHeadGap = 15;                                // the zone to the column (22)
constexpr int kChipH = 20;                                  // (30)
constexpr int kBtnH = 43, kBtnGap = 9, kSideW = 133;        // buttons (64, 14, 200)
constexpr int kFocusW = 173, kLinkH = 35;                   // focus terminal beside send (260); a permission's back (52)
constexpr int kRowH = 49, kRowGap = 7;                      // a question's answer rows (74, 10)
constexpr int kPermRowH = 43;                               // a permission's answer rows (64)
constexpr int kRadius = 17;                                 // the page's corners (26)
constexpr int kSecGap = 15;                                 // between the parts (22)
constexpr int kLineXS = 14, kLineS = 15, kLineM = 19, kLineL = 21, kLineQ = 29;
}  // namespace big_ui

// A ready egg: its box says "tap to hatch".
static inline bool bigHatchable(const Agent& a) { return a.pet.has && a.pet.stage == DECK_PET_EGG && a.pet.ready; }

// The name: as large as the column takes it (_FitName, 56 down to 34).
static inline const Font& bigNameFont(const Agent& a, int w) {
  return textW(FONT_Q28B, a.title[0] ? a.title : a.id) <= w ? FONT_Q28B : FONT_B20;
}

// The head of every agent screen: the pet, then the column.
struct BigHead {
  Rect zone;                   // the pet's zone, or its box
  bool boxed, hatch;           // boxed (needs you, or a ready egg); hatch: "tap to hatch" under it
  int textX, textW;
  int chipY, nameY, sumY;
  int bottom;
};

static inline BigHead bigHead(const BigSpec& S, const Agent& a, bool boxed) {
  using namespace big_ui;
  BigHead h{};
  h.hatch = bigHatchable(a);
  h.boxed = boxed || h.hatch;
  h.zone = h.boxed ? Rect{kPadX, kPadTop, kBoxW, kBoxH} : Rect{kPadX, kPadTop, kZoneW, kZoneH};
  h.textX = h.zone.x + h.zone.w + kHeadGap;
  h.textW = S.w - kPadX - h.textX;
  h.chipY = kPadTop + 1;
  h.nameY = h.chipY + kChipH + 4;
  h.sumY = h.nameY + bigNameFont(a, h.textW).cellH;
  const int col = h.sumY + kLineS, zone = h.zone.y + h.zone.h;
  h.bottom = col > zone ? col : zone;
  return h;
}

// A row of buttons at the foot.
struct BigButtons {
  int n = 0;
  Hit kind[4];
  Rect r[4];
};

// The card's: back, then the wide one (focus terminal; resume or remove once
// it has exited), then interrupt while it runs, stop while it doesn't. What
// the scope allows: focus and interrupt need act, stop, resume and remove admin.
static inline BigButtons bigCardButtons(const BigSpec& S, const Agent& a, DeckScope scope) {
  using namespace big_ui;
  BigButtons b;
  const bool exited = a.status == Status::Exited, running = a.status == Status::Running;
  const bool act = canAct(scope), admin = scope == DeckScope::Admin;
  const int y = S.h - kPadBot - kBtnH, right = S.w - kPadX;
  b.kind[b.n] = Hit::Back;
  b.r[b.n++] = Rect{kPadX, y, kBtnH, kBtnH};
  Hit wide = Hit::None, side = Hit::None;
  if (!exited) {
    if (act) wide = Hit::Focus;
    if (running && act) side = Hit::Interrupt;
    else if (!running && admin) side = Hit::Stop;
  } else if (admin) {
    wide = a.resumable ? Hit::Resume : Hit::Stop;
    if (a.resumable) side = Hit::Stop;
  }
  const int x = kPadX + kBtnH + kBtnGap, sideX = side != Hit::None ? right - kSideW : right + kBtnGap;
  if (wide != Hit::None) {
    b.kind[b.n] = wide;
    b.r[b.n++] = Rect{x, y, sideX - kBtnGap - x, kBtnH};
  }
  if (side != Hit::None) {
    b.kind[b.n] = side;
    b.r[b.n++] = Rect{sideX, y, kSideW, kBtnH};
  }
  return b;
}

// Where the card's parts go.
struct BigCard {
  BigHead head;
  int nowY;                    // "doing now" (or "status")
  Rect term;                   // the command box while it runs (h 0: none)
  int subs;                    // subagent lines in it (two at most)
  int lastY, textY, lastLines; // the last prompt or answer (lastLines 0: no room)
  int ctxY, whereY;
};

static inline BigCard bigCard(const BigSpec& S, const Agent& a, DeckScope scope) {
  using namespace big_ui;
  BigCard g{};
  g.head = bigHead(S, a, false);
  int y = g.head.bottom + kSecGap;
  g.nowY = y;
  if (a.status == Status::Running) {
    y += kLineXS + 7;
    g.subs = a.subCount < 2 ? a.subCount : 2;
    g.term = Rect{kPadX, y, S.w - 2 * kPadX, 9 + kLineL + g.subs * (5 + kLineM) + 9};
    y += g.term.h;
  } else {
    y += kLineXS + 5 + kLineL;
  }
  y += kSecGap;
  g.lastY = y;
  g.textY = y + kLineXS + 5;
  // The foot, from the buttons up: where it runs, then context.
  const int btnTop = bigCardButtons(S, a, scope).r[0].y;
  g.whereY = btnTop - 11 - kLineXS;
  g.ctxY = g.whereY - 8 - kLineXS;
  g.lastLines = (g.ctxY - 9 - g.textY) / kLineM;
  const int most = a.subCount ? 3 : 5;   // up to five lines, three beside subagents
  if (g.lastLines > most) g.lastLines = most;
  if (g.lastLines < 0) g.lastLines = 0;
  return g;
}

// Where a prompt's parts go.
struct BigPrompt {
  BigHead head;
  bool orange;                 // a permission: the whole page orange
  int askY, titleY, titleLines;
  Rect term;                   // a permission's command box (h 0: none)
  int termLines;
  int detailY, detailLines;    // a question's detail (0 lines: none)
  int n;
  Rect opt[kMaxOptions];
  bool sub;                    // the rows have room for an answer's second line
  BigButtons btn;              // back, then send and focus terminal (focus terminal as a link on orange)
  int orY;                     // a permission's "or answer at the terminal"
};

static inline int bigWrapLines(const Font& f, const char* s, int maxW, int maxLines) {
  Canvas none{nullptr, 0, 0};
  return textWrapped(none, f, 0, 0, s, 0, maxW, maxLines);
}

static inline bool bigPermission(const Agent& a) { return !strcmp(a.waitKind, "permission"); }

static inline BigPrompt bigPrompt(const BigSpec& S, const Agent& a) {
  using namespace big_ui;
  BigPrompt p{};
  const int w = S.w - 2 * kPadX;
  p.orange = bigPermission(a);
  p.head = bigHead(S, a, true);
  int y = p.head.bottom + (p.orange ? 13 : 12);
  p.askY = y;
  y += kLineXS + 4;
  p.titleY = y;
  p.titleLines = bigWrapLines(FONT_Q24B, a.waitTitle[0] ? a.waitTitle : "needs you", w, 2);
  if (p.titleLines < 1) p.titleLines = 1;
  y += p.titleLines * kLineQ;
  // The foot.
  const int foot = S.h - kPadBot;
  p.n = a.optCount;
  if (p.orange) {   // back, "or answer at the terminal", and focus terminal as a link
    p.btn.kind[0] = Hit::Back;
    p.btn.r[0] = Rect{kPadX, foot - kLinkH, kLinkH, kLinkH};
    const int lw = textW(FONT_XS, "FOCUS TERMINAL") + 12;
    p.btn.kind[1] = Hit::Focus;
    p.btn.r[1] = Rect{S.w - kPadX - lw, foot - kLinkH, lw, kLinkH};
    p.btn.n = 2;
    p.orY = foot - kLinkH + (kLinkH - kLineXS) / 2;
  } else {          // back, send (with answers), focus terminal
    const int by = foot - kBtnH, x = kPadX + kBtnH + kBtnGap;
    p.btn.kind[p.btn.n] = Hit::Back;
    p.btn.r[p.btn.n++] = Rect{kPadX, by, kBtnH, kBtnH};
    if (p.n) {
      p.btn.kind[p.btn.n] = Hit::Send;
      p.btn.r[p.btn.n++] = Rect{x, by, S.w - kPadX - kFocusW - kBtnGap - x, kBtnH};
      p.btn.kind[p.btn.n] = Hit::Focus;
      p.btn.r[p.btn.n++] = Rect{S.w - kPadX - kFocusW, by, kFocusW, kBtnH};
    } else {
      p.btn.kind[p.btn.n] = Hit::Focus;
      p.btn.r[p.btn.n++] = Rect{x, by, S.w - kPadX - x, kBtnH};
    }
  }
  const int bottom = p.btn.r[0].y - (p.orange ? 8 : 9);
  // The command (a permission) or the detail (a question) under the question.
  if (p.orange && (a.waitDetail[0] || a.promptCtx[0])) {
    y += 7;
    const int rows = p.n * kPermRowH + (p.n ? (p.n - 1) * kRowGap + 8 : 0);
    const int ctxH = a.promptCtx[0] ? 5 + kLineXS : 0;
    int lines = a.waitDetail[0] ? bigWrapLines(FONT_L, a.waitDetail, w - 24 - 18, 3) : 0;
    while (lines > 1 && y + 18 + lines * 20 + ctxH + rows > bottom) lines--;
    p.termLines = lines;
    p.term = Rect{kPadX, y, w, 18 + lines * 20 + ctxH};
    y += p.term.h;
  } else if (!p.orange && a.waitDetail[0]) {
    y += 4;
    p.detailY = y;
    p.detailLines = bigWrapLines(FONT_S, a.waitDetail, w, 2);
    y += p.detailLines * kLineS;
  }
  y += p.orange ? 8 : 9;
  // The answers right under it; too many for the room, they get thinner.
  const int rowH = p.orange ? kPermRowH : kRowH;
  int pitch = rowH + kRowGap;
  if (p.n && y + p.n * pitch - kRowGap > bottom) pitch = (bottom - y + kRowGap) / p.n;
  const int h = pitch - kRowGap < rowH ? pitch - kRowGap : rowH;
  p.sub = !p.orange && h >= 40;
  for (int i = 0; i < p.n; i++) p.opt[i] = Rect{kPadX, y + i * pitch, w, h};
  return p;
}

static inline int bigDefaultOption(const Agent& a) {
  for (int i = 0; i < a.optCount; i++)
    if (a.opts[i].primary) return i;
  return -1;
}

// ---------------------------------------------------------------- hit-testing and touch

static inline PanelHit bigHit(const BigSpec& S, const PanelUi& ui, const Fleet& f, int x, int y, DeckScope scope) {
  PanelHit h;
  if (ui.screen == Screen::Home) {
    const int pages = bigPages(f);
    const BigGrid g = bigGridFor(S, ui, pages);
    const BigBars bars = bigBars(S, g, ui, pages);
    if (bars.n && g.bars.contains(x, y)) {
      const int k = bigBarAt(bars, x);
      if (bars.mark && k == 0) h.kind = Hit::NeedsYou;
      else { h.kind = Hit::PageBar; h.index = k - (bars.mark ? 1 : 0); }
      return h;
    }
    for (int cell = 0; cell < 16; cell++)
      if (bigCell(g, cell).contains(x, y)) {
        const int i = bigCellAgent(ui, f, ui.page < pages ? ui.page : 0, cell);
        if (i >= 0) { h.kind = Hit::Tile; h.agent = i; }
        return h;
      }
    return h;
  }
  const int i = findAgent(f, ui.openId);
  if (i < 0) return h;
  const Agent& a = f.agents[i];
  h.agent = i;
  if (ui.screen == Screen::Card) {
    const BigButtons b = bigCardButtons(S, a, scope);
    for (int k = 0; k < b.n; k++)
      if (b.r[k].contains(x, y)) { h.kind = b.kind[k]; return h; }
    const BigHead hd = bigHead(S, a, false);
    if (hd.hatch && canAct(scope) && hd.zone.contains(x, y)) { h.kind = Hit::Hatch; return h; }
    return PanelHit();
  }
  const BigPrompt p = bigPrompt(S, a);
  for (int k = 0; k < p.n; k++)
    if (p.opt[k].contains(x, y)) { h.kind = Hit::Option; h.index = k; return h; }
  for (int k = 0; k < p.btn.n; k++)
    if (p.btn.r[k].contains(x, y)) { h.kind = p.btn.kind[k]; return h; }
  return PanelHit();
}

constexpr uint32_t kStopArmMs = 3000;   // "tap again to stop" holds this long

static inline void bigOpen(PanelUi& ui, const Fleet& f, int i) {
  const Agent& a = f.agents[i];
  snprintf(ui.openId, sizeof ui.openId, "%s", a.id);
  ui.screen = a.status == Status::Waiting ? Screen::Prompt : Screen::Card;
  ui.selected = bigDefaultOption(a);
  ui.stopArmedUntil = 0;
  ui.backAfterToast = false;
}

static inline void bigHome(PanelUi& ui) {
  ui.screen = Screen::Home;
  ui.openId[0] = '\0';
  ui.backAfterToast = false;
  ui.stopArmedUntil = 0;
}

static inline DeckCommand bigTap(PanelUi& ui, const Fleet& f, const PanelHit& h, DeckScope scope, uint32_t ms) {
  DeckCommand out;
  switch (h.kind) {
    case Hit::Tile: bigOpen(ui, f, h.agent); break;
    case Hit::Back: bigHome(ui); break;
    case Hit::PageBar: ui.page = h.index; ui.needsYou = false; break;
    case Hit::NeedsYou: ui.needsYou = ui.att != nullptr; break;
    case Hit::Focus:
      if (!canAct(scope)) { uiToast(ui, "view only: needs scope act", ms); break; }
      out = commandFor(DeckCmd::Focus, f.agents[h.agent]);
      uiToast(ui, "focused", ms, 1200);
      break;
    case Hit::Interrupt:
      out = commandFor(DeckCmd::Interrupt, f.agents[h.agent]);
      uiToast(ui, "sent", ms, 1200);
      break;
    case Hit::Resume:
      out = commandFor(DeckCmd::Resume, f.agents[h.agent]);
      uiToast(ui, "resuming", ms, 1200);
      break;
    case Hit::Stop:
      if (f.agents[h.agent].status == Status::Exited || (int32_t)(ui.stopArmedUntil - ms) > 0) {
        out = commandFor(DeckCmd::Dismiss, f.agents[h.agent]);
        uiToast(ui, f.agents[h.agent].status == Status::Exited ? "removed" : "stopping\xE2\x80\xA6", ms, 1500);
        ui.stopArmedUntil = 0;
      } else {
        ui.stopArmedUntil = ms + kStopArmMs;   // first tap: arm
      }
      break;
    case Hit::Hatch:
      if (!kCanHatch) { uiToast(ui, "hatch it on the deck", ms); break; }
      out = commandFor(DeckCmd::Hatch, f.agents[h.agent]);
      uiToast(ui, "hatching\xE2\x80\xA6", ms, 1500);
      break;
    case Hit::Option:   // the first tap chooses an answer, a tap on the chosen one sends it
      if (ui.selected != h.index) { ui.selected = h.index; break; }
      // fall through
    case Hit::Send: {
      const Agent& a = f.agents[h.agent];
      if (ui.selected < 0 || ui.selected >= a.optCount || ui.backAfterToast) break;
      if (!canAct(scope)) { uiToast(ui, "view only: needs scope act", ms); break; }
      out = commandFor(DeckCmd::Answer, a);
      snprintf(out.prompt, sizeof out.prompt, "%s", a.promptId);
      snprintf(out.key, sizeof out.key, "%s", a.opts[ui.selected].key);
      uiToast(ui, "sent", ms, 1000);
      ui.backAfterToast = true;
      break;
    }
    default: break;
  }
  return out;
}

// Home's pages, one step at a time: the "Needs you" screen sits left of the
// first page.
static inline void bigStep(PanelUi& ui, const Fleet& f, bool next) {
  const int pages = bigPages(f);
  if (ui.needsYou) {
    if (next) { ui.needsYou = false; ui.page = 0; }
  } else if (next) {
    if (ui.page < pages - 1) ui.page++;
  } else if (ui.page > 0) {
    ui.page--;
  } else if (ui.att) {
    ui.needsYou = true;
  }
}

static inline int bigSpots() { return 16; }

constexpr float kBigDiagIn = 4.0f;   // the 4" glass: the pages' commit point in pixels

static inline void pagerFollow(PanelUi& ui, const Fleet& f, const BigSpec& S, int dx, int x, uint32_t ms) {
  pagerFollow(ui, bigPages(f), S.w, dx, x, ms);
}

static inline DeckCommand bigTouch(PanelUi& ui, const BigSpec& S, const Fleet& f, bool touching, int x, int y,
                                   DeckScope scope, uint32_t ms) {
  DeckCommand out;
  constexpr int slop = 14, swipeMin = 40;
  if (ui.att) attSync(*ui.att, f, bigSpots(), ms);   // the spots the finger lands on are the ones drawn
  const bool home = ui.screen == Screen::Home;
  if (touching) {
    if (!ui.down) {
      ui.down = true;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ui.lastMs = ms;
      ui.lastX = x;
      ui.velocity = 0;
      // The pages still settling: this finger catches them where they are and moves them on.
      const bool caught = home && ui.slideAnim;
      ui.slide0 = pagerAt(ui);
      ui.slideAnim = false;
      ui.sliding = caught;
      ui.pressed = caught ? PanelHit() : bigHit(S, ui, f, x, y, scope);
      return out;
    }
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
    if (dx * dx + dy * dy > slop * slop) ui.pressed = PanelHit();   // it became a swipe
    // Home's pages follow a clearly sideways finger (page_slide.h).
    if (home && !ui.sliding && adx > slop && swipeAcross(adx, ady)) pagerGrab(ui, x, ms);
    if (ui.sliding) pagerFollow(ui, f, S, dx, x, ms);
    return out;
  }
  if (!ui.down) return out;
  ui.down = false;
  const PanelHit pressed = ui.pressed;
  ui.pressed = PanelHit();
  if (ui.sliding) {                                  // on to the page they settle on
    pagerRelease(ui, bigPages(f), S.w, pagerCommitPx(S.w, S.h, kBigDiagIn), ms);
    return out;
  }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (adx <= slop && ady <= slop) {
    if (pressed.kind != Hit::None) out = bigTap(ui, f, pressed, scope, ms);
  } else if (adx >= swipeMin || ady >= swipeMin) {
    const bool across = swipeAcross(adx, ady);     // only a sideways move pages or goes back
    if (ui.screen != Screen::Home) {
      if (across && dx > 0) bigHome(ui);
    } else if (across) {
      bigStep(ui, f, dx < 0);
    }
  }
  return out;
}

static inline bool bigTick(PanelUi& ui, const Fleet& f, uint32_t ms) {
  const int pages = bigPages(f);
  if (ui.screen != Screen::Home) ui.sliding = ui.slideAnim = false;
  pagerTick(ui, ms);
  if (ui.page >= pages) ui.page = pages - 1;
  if (ui.page < 0) ui.page = 0;
  if (ui.att) attSync(*ui.att, f, bigSpots(), ms);
  else ui.needsYou = false;
  if (ui.screen != Screen::Home) {
    const int i = findAgent(f, ui.openId);
    if (i < 0) bigHome(ui);
    else if (ui.screen == Screen::Prompt && f.agents[i].status != Status::Waiting && !ui.backAfterToast)
      ui.screen = Screen::Card;
    else if (ui.screen == Screen::Card && f.agents[i].status == Status::Waiting) {
      ui.screen = Screen::Prompt;
      ui.selected = bigDefaultOption(f.agents[i]);
    }
  }
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) bigHome(ui);
  if (ui.stopArmedUntil && (int32_t)(ui.stopArmedUntil - ms) <= 0) ui.stopArmedUntil = 0;
  return pagerMoving(ui);                            // the pages slide: draw often
}

static inline void bigResult(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) {
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
  ui.backAfterToast = false;
}

// BOOT: back from a card or a prompt; on home the next page, round to the
// "Needs you" screen after the last one, then the first page again.
static inline void bigButton(PanelUi& ui, const Fleet& f) {
  if (ui.screen != Screen::Home) { bigHome(ui); return; }
  const int pages = bigPages(f);
  if (ui.needsYou) { ui.needsYou = false; ui.page = 0; }
  else if (ui.att && ui.page >= pages - 1) ui.needsYou = true;
  else ui.page = (ui.page + 1) % pages;
}

// ---------------------------------------------------------------- drawing

namespace big_detail {

constexpr uint16_t kRowOutline = rgb565(0x2A, 0x29, 0x25);   // agentsRowOutline
constexpr uint16_t kRowSubOnInk = rgb565(0x3B, 0x3A, 0x36);  // agentsRowSubOnInk

// One corner of a rounded box: the ring between r0 and r1 (a disc when r0 is
// 0) around the whole-pixel centre (cx, cy), in the quarter on the side of
// sx, sy (-1 or 1). The same pixels as fillArc over that quarter's 90
// degrees, without an angle per pixel (pixel centres never sit on the axes).
static inline void fillQuarter(Canvas& c, int cx, int cy, float r0, float r1, int sx, int sy, uint16_t col) {
  if (r1 <= 0) return;
  if (r0 < 0) r0 = 0;
  const int n = (int)ceilf(r1) + 1;
  int y0 = sy < 0 ? cy - n : cy, y1 = sy < 0 ? cy : cy + n;   // rows y0..y1-1
  int x0 = sx < 0 ? cx - n : cx, x1 = sx < 0 ? cx : cx + n;
  if (y0 < c.top) y0 = c.top;
  if (y1 > c.h) y1 = c.h;
  if (x0 < 0) x0 = 0;
  if (x1 > c.w) x1 = c.w;
  for (int y = y0; y < y1; y++) {
    const float dy = y + 0.5f - cy, dy2 = dy * dy;
    for (int x = x0; x < x1; x++) {
      const float dx = x + 0.5f - cx;
      const int a = round_detail::ringCover(sqrtf(dx * dx + dy2), r0, r1);
      if (a) round_detail::blendPx(c, x, y, col, a);
    }
  }
}

static inline void rrect(Canvas& c, const Rect& r, int rad, uint16_t col) {
  if (rad * 2 > r.h) rad = r.h / 2;
  if (rad * 2 > r.w) rad = r.w / 2;
  if (rad <= 0) { c.rect(r.x, r.y, r.w, r.h, col); return; }
  c.rect(r.x + rad, r.y, r.w - 2 * rad, r.h, col);
  c.rect(r.x, r.y + rad, rad, r.h - 2 * rad, col);
  c.rect(r.x + r.w - rad, r.y + rad, rad, r.h - 2 * rad, col);
  fillQuarter(c, r.x + rad, r.y + rad, 0, rad, -1, -1, col);
  fillQuarter(c, r.x + r.w - rad, r.y + rad, 0, rad, 1, -1, col);
  fillQuarter(c, r.x + r.w - rad, r.y + r.h - rad, 0, rad, 1, 1, col);
  fillQuarter(c, r.x + rad, r.y + r.h - rad, 0, rad, -1, 1, col);
}

// A rounded outline `t` px thick, inside the box.
static inline void rframe(Canvas& c, const Rect& r, int rad, int t, uint16_t col) {
  if (rad * 2 > r.h) rad = r.h / 2;
  c.rect(r.x + rad, r.y, r.w - 2 * rad, t, col);
  c.rect(r.x + rad, r.y + r.h - t, r.w - 2 * rad, t, col);
  c.rect(r.x, r.y + rad, t, r.h - 2 * rad, col);
  c.rect(r.x + r.w - t, r.y + rad, t, r.h - 2 * rad, col);
  fillQuarter(c, r.x + rad, r.y + rad, rad - t, rad, -1, -1, col);
  fillQuarter(c, r.x + r.w - rad, r.y + rad, rad - t, rad, 1, -1, col);
  fillQuarter(c, r.x + r.w - rad, r.y + r.h - rad, rad - t, rad, 1, 1, col);
  fillQuarter(c, r.x + rad, r.y + r.h - rad, rad - t, rad, -1, 1, col);
}

// AgentButton: 14 px radius, 2 px outline, the label centred (or left).
static inline void button(Canvas& c, const BigSpec& S, const Rect& r, const char* label, uint16_t fill, uint16_t ink,
                          uint16_t outline, bool left = false) {
  rrect(c, r, bk(S, 14), outline);
  if (fill != outline) rrect(c, Rect{r.x + 2, r.y + 2, r.w - 4, r.h - 4}, bk(S, 14) - 2, fill);
  const int pad = bk(S, 22);
  // The biggest of body / text / label that fits.
  const Font* fonts[] = {S.body, S.text, S.label};
  const Font* f = S.label;
  for (const Font* g : fonts)
    if (textW(*g, label) <= r.w - 2 * pad) { f = g; break; }
  const int w = textW(*f, label) < r.w - 2 * pad ? textW(*f, label) : r.w - 2 * pad;
  const int x = left ? r.x + pad : r.x + (r.w - w) / 2;
  c.text(*f, x, r.y + (r.h - f->cellH) / 2 + 1, label, ink, r.w - 2 * pad);
}

// Diagonal accent stripes sliding across a hungry tile (HungryStripes).
static inline void stripes(Canvas& c, const Rect& r, uint16_t fill, uint16_t accent, uint32_t ms, int period) {
  const uint16_t s = blend565(fill, accent, 2);   // accent at 16 %
  const int shift = (int)((ms / 100) % 6) * period / 6;
  if (period < 2) return;
  const uint16_t v = c.enc(s);
  const int half = period / 2;
  const int y0 = r.y > c.top ? r.y : c.top, y1 = r.y + r.h < c.h ? r.y + r.h : c.h;   // the rows there are
  const int x0 = r.x > 0 ? r.x : 0, x1 = r.x + r.w < c.w ? r.x + r.w : c.w;
  for (int py = y0; py < y1; py++) {
    const int yy = py - r.y;
    // Pixel xx is lit where (xx + o) mod period < period / 2: runs of half a period.
    const int o = ((r.h - yy - shift) % period + period) % period;
    uint16_t* row = c.px + (size_t)py * c.w;
    for (int xx = -o; r.x + xx < x1; xx += period) {
      int a = r.x + xx, b = a + half;
      if (a < x0) a = x0;
      if (b > x1) b = x1;
      if (b > a) Canvas::run(row + a, b - a, v);
    }
  }
}

// ContextRingPainter: `pct` of the border, clockwise from the top centre.
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

// Every cell a pet's look reaches in this pose over its motion (its stroke,
// flames, hat and bubble, and a ready egg's wobble), half-open, in cells from
// its cell (0, 0). Kept for the last few looks: a look is built many times over.
struct PetExtent { int x0, y0, x1, y1; };

static inline PetExtent petExtent(const Agent& a, Pose pose, bool bubble) {
  struct Entry { uint32_t key; PetExtent e; };
  static Entry cache[24];
  static uint8_t next;
  uint32_t key = 2166136261u;
  auto mix = [&](const void* p, size_t n) {
    for (size_t i = 0; i < n; i++) key = (key ^ ((const uint8_t*)p)[i]) * 16777619u;
  };
  const uint8_t more[4] = {a.hat, a.hatColor, (uint8_t)pose, (uint8_t)bubble};
  mix(&a.pet, sizeof a.pet);
  mix(more, sizeof more);
  if (!key) key = 1;
  for (const Entry& en : cache)
    if (en.key == key) return en.e;
  PetExtent e{1 << 20, 1 << 20, -(1 << 20), -(1 << 20)};
  deck_pet_opts_t o{};
  o.pose = petPose(pose);
  o.bubble = bubble;
  o.hat = a.hat;
  o.hat_color = a.hatColor;
  static deck_pet_look_t look;
  for (uint32_t ms = 0; ms < 3000; ms += 150) {   // 20 frames: a few ms on the board, once per look
    o.ms = ms;
    deck_pet_look(&look, &a.pet, &o);
    int bx, by, bw, bh;
    deck_pet_bounds(&look, 1, &bx, &by, &bw, &bh);
    if (bw <= 0 || bh <= 0) continue;
    if (bx < e.x0) e.x0 = bx;
    if (by < e.y0) e.y0 = by;
    if (bx + bw > e.x1) e.x1 = bx + bw;
    if (by + bh > e.y1) e.y1 = by + bh;
  }
  if (e.x1 <= e.x0) e = PetExtent{0, 0, 16, 15};
  e.x0 -= 1;   // a ready egg's wobble: a cell each way; and a cell over the top for frames between those
  e.x1 += 1;
  e.y0 -= 1;
  cache[next] = Entry{key, e};
  next = (uint8_t)((next + 1) % (sizeof cache / sizeof cache[0]));
  return e;
}

// The pet so that all it reaches fits `box`: the biggest whole cell up to
// `most`, standing on the box's foot, centred and pushed in from the sides
// (PetSprite with fit).
static inline void petFit(Canvas& c, const Rect& box, int most, const Agent& a, Pose pose, uint32_t ms, bool bubble) {
  const PetExtent e = petExtent(a, pose, bubble);
  const int ew = e.x1 - e.x0, eh = e.y1 - e.y0;
  int s = most;
  if (box.w / ew < s) s = box.w / ew;
  if (box.h / eh < s) s = box.h / eh;
  if (s < 1) s = 1;
  int x = box.x + box.w / 2 - 8 * s;
  if (x + e.x0 * s < box.x) x = box.x - e.x0 * s;
  if (x + e.x1 * s > box.x + box.w) x = box.x + box.w - e.x1 * s;
  drawPetCells(c, x, box.y + box.h - e.y1 * s, s, a, pose, ms, bubble);
}

// AgentTile, laid out on the deck's 170 px design cell and scaled to the real one.
static inline void tile(Canvas& c, const BigSpec& S, const Theme& T, const Agent& a, const Rect& r, uint32_t ms,
                        int64_t now, bool pressed) {
  Look k = lookOf(T, a);
  const uint16_t fill = pressed ? T.ink : k.fill;
  auto cc = [&](uint16_t v) { return pressed ? T.bg : v; };
  const int side = r.w < r.h ? r.w : r.h;
  const float u = side / 170.f;                     // design px -> board px
  auto d = [&](float v) { return (int)lroundf(v * u); };
  const int ox = r.x + (r.w - side) / 2, oy = r.y + (r.h - side) / 2;   // the FittedBox's square
  SQ_T0(tb);
  c.rect(r.x, r.y, r.w, r.h, fill);
  SQ_ADD(tileBg, tb);
  SQ_T0(ts);
  if (k.stripes && !pressed) stripes(c, r, fill, T.accent, ms, d(24));
  SQ_ADD(other, ts);
  if (!pressed) c.rect(r.x + r.w - d(10) - d(8), r.y + d(10), d(8), d(8), k.chip);   // the tool chip
  SQ_T0(tp);
  if (a.subagents) {                                 // subagents at work: a small body, and a count
    const Pose sp = a.subTool[0] ? poseOfTool(a.subTool) : Pose::Think;
    const int s = S.subScale;
    drawSprite(c, r.x + d(9), r.y + d(8 + 4 * 2) - 4 * s, s, Species::Claude, sp, cc(k.body), fill, ms, false);
    if (a.subagents > 1) {
      char n[8];
      snprintf(n, sizeof n, "\xC3\x97%u", (unsigned)a.subagents);
      c.text(*S.tileMeta, r.x + d(9) + 14 * s + d(3), r.y + d(8) + 9 * s - S.tileMeta->cellH + 3, n, cc(k.meta));
    }
  }
  // The critter: its body where the deck draws it (92 px wide from y 30; the
  // feet at 30 + 12/13 of its height), at the nearest whole scale.
  const int s = S.tileScale;
  if (a.pet.has) {                                   // its pet: all it reaches inside the tile, over the name
    petFit(c, Rect{ox + d(6), oy + d(8), d(158), d(96)}, s, a, poseOf(a, now), ms, true);   // (fit, 158 x 96 at 6, 8)
  } else {
    const float unit = 92.f / 18.f;                  // the deck's critter: px per sprite unit
    const int feet = oy + d(30 + 12 * unit);
    drawCritter(c, ox + side / 2 - 7 * s, feet - 12 * s, s, a, poseOf(a, now), cc(k.body), fill, ms, true);
  }
  SQ_ADD(sprite, tp);
  SQ_T0(tt);
  // Title (mono 16) at 30 + 92 x 13/18 + 14; the status line (12.5) 5 px under it.
  const int titleTop = oy + d(30 + 92.f * 13 / 18 + 14);
  const int textBottom = titleTop + d(16 + 5) + S.tileMeta->cellH + S.tileTitle->cellH;   // with room to spare
  if (titleTop - S.tileTitle->cellH < c.h && textBottom > c.top) {   // (drawn in strips: only where the lines are)
    const int tw = r.w - 2 * d(10);
    const char* title = a.title[0] ? a.title : a.id;
    const int ttw = textW(*S.tileTitle, title) < tw ? textW(*S.tileTitle, title) : (tw / S.tileTitle->cellW) * S.tileTitle->cellW;
    textAt(c, *S.tileTitle, r.x + (r.w - ttw) / 2, titleTop, title, cc(k.label), tw);
    char meta[80];
    metaLine(a, now, meta, sizeof meta);
    const int mw = textW(*S.tileMeta, meta) < tw ? textW(*S.tileMeta, meta) : (tw / S.tileMeta->cellW) * S.tileMeta->cellW;
    textAt(c, *S.tileMeta, r.x + (r.w - mw) / 2, titleTop + d(16 + 5), meta, cc(k.meta), tw);
  }
  SQ_ADD(text, tt);
  if (!pressed) ring(c, r, d(4) < 2 ? 2 : d(4), a.ctxWindow ? ctxPct(a) : 0, k.ring, k.ringTrack);
}

static inline void home(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  const int pages = bigPages(f);
  const int page = ui.page < pages ? ui.page : pages - 1;
  const BigGrid g = bigGridFor(S, ui, pages);
  for (int cell = 0; cell < 16; cell++) {
    const Rect r = bigCell(g, cell);
    if (r.y >= c.h || r.y + r.h <= c.top) continue;   // drawn in strips: not in this one
    const int i = bigCellAgent(ui, f, page, cell);
    if (i >= 0) tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i);
    else {
      c.rect(r.x, r.y, r.w, r.h, T.emptyFill);
      c.frame(r.x, r.y, r.w, r.h, T.emptyOutline);
    }
  }
  if (ui.wifi && g.bars.w)                           // the Wi-Fi (or Bluetooth) mark (round_geom.h), right-aligned with the grid
    (ui.ble ? drawBleMark : drawNetMark)(c, (float)(g.x0 + 4 * g.cell + 3 * g.gap - 18), (float)(g.bars.y + g.bars.h / 2 + 6), T.dim);
  const BigBars bars = bigBars(S, g, ui, pages);
  if (bars.n) {                                      // _PageBars
    bool hot[64] = {};
    for (int i = 0; i < f.count; i++)
      if (f.agents[i].status == Status::Waiting && f.agents[i].slot / 16 < 64) hot[f.agents[i].slot / 16] = true;
    const bool here = ui.needsYou && ui.att;
    if (bars.mark) {                                 // "Needs you": solid orange while you are on it, an orange
      const Rect m = bars.r[0];                      // outline (and the count) while someone is on it
      const int count = ui.att->count;
      const uint16_t edge = here || count > 0 ? T.accent : kRowOutline;
      const int bw = bk(S, 2) < 1 ? 1 : bk(S, 2);
      rrect(c, m, m.h / 2, edge);
      if (!here) rrect(c, Rect{m.x + bw, m.y + bw, m.w - 2 * bw, m.h - 2 * bw}, m.h / 2 - bw, T.bg);
      if (bars.countX >= 0) {
        char n[4];
        snprintf(n, sizeof n, "%d", count > 99 ? 99 : count);
        textAt(c, *S.label, bars.countX, m.y + m.h / 2 - fontPx(*S.label) / 2, n, T.accent);
      }
    }
    for (int k = bars.mark ? 1 : 0; k < bars.n; k++) {
      const int p = k - (bars.mark ? 1 : 0);
      const bool cur = !here && p == page;
      rrect(c, bars.r[k], bk(S, 4), cur ? T.ink : (p < 64 && hot[p]) ? T.accent : kRowOutline);
    }
  }
  if (in.link == LinkState::Lost) {                  // _OfflineChip, 22 px off the bottom
    const char* s = "offline" DECK_DOT "retrying";
    const Font& f2 = *S.label;
    const int w = textW(f2, s) + 2 * bk(S, 18), h = fontPx(f2) + 2 * bk(S, 9);
    const Rect r{(S.w - w) / 2, S.h - bk(S, 22) - h, w, h};
    rrect(c, r, h / 2, T.accent);
    rrect(c, Rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}, h / 2 - 1, T.panel);
    textAt(c, f2, r.x + bk(S, 18), r.y + bk(S, 9), s, T.accent);
  }
}

// "84k", "3.1m" (compactCount).
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

// agentPillLabel.
static inline void pillLabel(const Agent& a, int64_t now, char* out, size_t cap) {
  char t[8];
  fmtAge(a.statusSince ? now - a.statusSince : 0, t, sizeof t);
  switch (a.status) {
    case Status::Running:  snprintf(out, cap, "running" DECK_DOT "%s", t); break;
    case Status::Waiting:
      if (!strcmp(a.waitKind, "question") && a.promptCtx[0]) snprintf(out, cap, "%s", a.promptCtx);
      else snprintf(out, cap, "needs you" DECK_DOT "%s", t);
      break;
    case Status::Idle:     snprintf(out, cap, "%s" DECK_DOT "%s", a.unseen ? "hungry" : "idle", t); break;
    case Status::Error:    snprintf(out, cap, "error"); break;
    case Status::Starting: snprintf(out, cap, "starting" DECK_DOT "%s", t); break;
    case Status::Exited:   snprintf(out, cap, "%s" DECK_DOT "%s", a.lost ? "lost with tmux" : "exited", t); break;
    default:               snprintf(out, cap, "idle" DECK_DOT "%s", t);
  }
}

// ---------------------------------------------------------------- the agent's page
// The design's colours (agent_detail_pets.dart): on the dark page and on orange.
constexpr uint16_t kRule = rgb565(0x3B, 0x3A, 0x36);
constexpr uint16_t kText = rgb565(0xC9, 0xC5, 0xBC);
constexpr uint16_t kMuted = rgb565(0x9A, 0x96, 0x8C);
constexpr uint16_t kOrange2 = rgb565(0xFF, 0x6A, 0x2E);
constexpr uint16_t kLevel = rgb565(0xFF, 0x8A, 0x4C);
constexpr uint16_t kBarTrack = rgb565(0x2E, 0x2D, 0x2A);
constexpr uint16_t kSegOff = rgb565(0x2A, 0x29, 0x25);
constexpr uint16_t kHeart = rgb565(0xE8, 0x45, 0x2B);
constexpr uint16_t kRowLine = rgb565(0x2A, 0x29, 0x25);   // agentsRowOutline

// Text in capitals (the design's small labels), cut to maxW.
static inline int upperAt(Canvas& c, const Font& f, int x, int y, const char* s, uint16_t col, int maxW = -1) {
  char b[128];
  size_t i = 0;
  for (; s[i] && i + 1 < sizeof b; i++) b[i] = (char)toupper((unsigned char)s[i]);
  b[i] = '\0';
  return c.text(f, x, y, b, col, maxW);
}

static inline int upperW(const Font& f, const char* s) {
  char b[128];
  size_t i = 0;
  for (; s[i] && i + 1 < sizeof b; i++) b[i] = (char)toupper((unsigned char)s[i]);
  b[i] = '\0';
  return textW(f, b);
}

// A text line whose line box (lineH) starts at y: the glyph cell centred in it.
static inline int lineAt(Canvas& c, const Font& f, int lineH, int x, int y, const char* s, uint16_t col, int maxW = -1) {
  return c.text(f, x, y + (lineH - f.cellH) / 2, s, col, maxW);
}

// A small label ("DOING NOW") and its dimmer tail (" · BASH"). Returns its width.
static inline int label(Canvas& c, int x, int y, const char* s, uint16_t col, const char* tail = nullptr, uint16_t tailCol = 0) {
  int w = upperAt(c, FONT_XS, x, y, s, col);
  if (tail && tail[0]) {
    char b[64];
    snprintf(b, sizeof b, DECK_DOT "%s", tail);
    w += upperAt(c, FONT_XS, x + w, y, b, tailCol);
  }
  return w;
}

// The status chip: "WORKING · 38M" on ink, "NEEDS YOU" on orange (on the
// orange page, orange on black), "HUNGRY" on orange, "IDLE" in an outline.
static inline int chip(Canvas& c, const Theme& T, int x, int y, const Agent& a, int64_t now, bool onOrange) {
  char t[8], s[40];
  fmtAge(a.statusSince ? now - a.statusSince : 0, t, sizeof t);
  uint16_t fill = T.bg, ink = kMuted, edge = kRule;
  switch (a.status) {
    case Status::Running: snprintf(s, sizeof s, "working" DECK_DOT "%s", t); fill = edge = T.ink; ink = T.bg; break;
    case Status::Waiting:
      snprintf(s, sizeof s, "needs you" DECK_DOT "%s", t);
      if (onOrange) { fill = edge = T.bg; ink = T.accent; }
      else { fill = edge = T.accent; ink = T.bg; }
      break;
    case Status::Error: snprintf(s, sizeof s, "error"); fill = edge = T.accent; ink = T.bg; break;
    case Status::Starting: snprintf(s, sizeof s, "starting" DECK_DOT "%s", t); break;
    case Status::Exited: snprintf(s, sizeof s, "%s" DECK_DOT "%s", a.lost ? "lost" : "exited", t); break;
    default:
      if (a.unseen) { snprintf(s, sizeof s, "hungry" DECK_DOT "%s", t); fill = edge = T.accent; ink = T.bg; }
      else snprintf(s, sizeof s, "idle" DECK_DOT "%s", t);
  }
  const int w = upperW(FONT_XS, s) + 16;
  c.rect(x, y, w, big_ui::kChipH, edge);
  if (fill != edge) c.rect(x + 1, y + 1, w - 2, big_ui::kChipH - 2, fill);
  upperAt(c, FONT_XS, x + 8, y + (big_ui::kChipH - FONT_XS.cellH) / 2, s, ink);
  return w;
}

// A pixel heart (7 x 6 cells of `px`), full or empty.
static inline void heart(Canvas& c, int x, int y, int px, bool full, bool onOrange, uint16_t bg) {
  static const char* const kRows[] = {".rr.rr.", "rrrrrrr", "rrrrrrr", ".rrrrr.", "..rrr..", "...r..."};
  const uint16_t col = onOrange ? (full ? rgb565(0x0A, 0x0A, 0x0A) : blend565(bg, rgb565(0x0A, 0x0A, 0x0A), 4))
                                : (full ? kHeart : kRule);
  for (int r = 0; r < 6; r++)
    for (int k = 0; k < 7; k++)
      if (kRows[r][k] == 'r') c.rect(x + k * px, y + r * px, px, px, col);
}

// Hearts, level and its bar ("♥♥♥♡ LV 14"), "EGG · 84%" and its bar, or the
// "READY TO HATCH" chip: right-aligned at `right`, from y (_petMeta).
static inline void petMeta(Canvas& c, const Theme& T, int right, int y, const Agent& a, bool onOrange) {
  const deck_pet_t& p = a.pet;
  const uint16_t ink = onOrange ? T.bg : T.ink;
  const uint16_t fillCol = onOrange ? T.bg : kLevel, track = onOrange ? blend565(T.accent, T.bg, 4) : kBarTrack;
  const int barW = 100, rowH = FONT_B15.cellH;
  char s[24];
  int pct = p.progress * 100 / 255;
  if (p.stage == DECK_PET_EGG) {
    if (p.ready) {
      const int w = upperW(FONT_XS, "ready to hatch") + 16;
      c.rect(right - w, y, w, big_ui::kChipH, kLevel);
      upperAt(c, FONT_XS, right - w + 8, y + (big_ui::kChipH - FONT_XS.cellH) / 2, "ready to hatch", T.bg);
      return;
    }
    snprintf(s, sizeof s, "EGG" DECK_DOT "%d%%", pct);
    c.text(FONT_B15, right - textW(FONT_B15, s), y, s, ink);
  } else {
    snprintf(s, sizeof s, "LV %u", (unsigned)p.level);
    int x = right - textW(FONT_B15, s);
    c.text(FONT_B15, x, y, s, ink);
    x -= 8;
    for (int i = DECK_PET_HEARTS - 1; i >= 0; i--) {
      x -= 14;
      heart(c, x, y + (rowH - 12) / 2, 2, i < p.hearts, onOrange, T.accent);
      x -= 2;
    }
  }
  const int by = y + rowH + 3;
  c.rect(right - barW, by, barW, 4, track);
  c.rect(right - barW, by, barW * pct / 100, 4, fillCol);
}

// The design's icons on a 16-unit grid: the back chevron (and a square for anything else).
static inline void icon(Canvas& c, Hit kind, int cx, int cy, int size, uint16_t col) {
  const float u = size / 16.f;
  auto r = [&](float x, float y, float w, float h) {
    c.rect(cx - size / 2 + (int)lroundf(x * u), cy - size / 2 + (int)lroundf(y * u), (int)lroundf(w * u) < 1 ? 1 : (int)lroundf(w * u),
           (int)lroundf(h * u) < 1 ? 1 : (int)lroundf(h * u), col);
  };
  if (kind == Hit::Back) {   // a chevron pointing left
    for (int i = 0; i < 5; i++) { r(9 - i, 3 + i, 2, 2); r(9 - i, 11 - i, 2, 2); }
  } else {
    r(4, 4, 8, 8);
  }
}

// A square-cornered button (AgentButton, square): its label in capitals,
// centred; the back button its chevron.
static inline void button(Canvas& c, const Rect& r, const char* text, uint16_t fill, uint16_t ink, uint16_t edge, bool back = false) {
  c.rect(r.x, r.y, r.w, r.h, edge);
  if (fill != edge) c.rect(r.x + 1, r.y + 1, r.w - 2, r.h - 2, fill);
  if (back) {
    icon(c, Hit::Back, r.x + r.w / 2, r.y + r.h / 2, r.h / 2 - 2, ink);
    return;
  }
  const int w = upperW(FONT_M13, text) < r.w - 8 ? upperW(FONT_M13, text) : r.w - 8;
  upperAt(c, FONT_M13, r.x + (r.w - w) / 2, r.y + (r.h - FONT_M13.cellH) / 2, text, ink, r.w - 8);
}

// "↳": a hook down and to the right, for a subagent's line.
static inline int hook(Canvas& c, int x, int y, int lineH, uint16_t col) {
  const int top = y + lineH / 2 - 4;
  c.rect(x + 1, top, 1, 6, col);
  c.rect(x + 1, top + 5, 6, 1, col);
  c.rect(x + 5, top + 3, 1, 5, col);
  return 12;
}

// The context bar (_ContextSegments): 40 segments, 2 px apart.
static inline void segs(Canvas& c, const Theme& T, int x, int y, int w, int pct, uint16_t off) {
  const int n = 40, gap = 2, sw = (w - (n - 1) * gap) / n;
  const int on = (pct * n + 50) / 100;
  for (int i = 0; i < n; i++) c.rect(x + i * (sw + gap), y, sw, 4, i < on ? T.ink : off);
}

// "■ personal": a square in the account's colour, then its name; right-aligned
// at `right`. Returns its width.
static inline int accountName(Canvas& c, const Theme& T, const Font& f, int right, int y, int lineH, const Agent& a) {
  const int nw = textW(f, a.account) < 133 ? textW(f, a.account) : 133;
  const int w = 7 + 7 + nw;
  c.rect(right - w, y + (lineH - 7) / 2, 7, 7, sprite_detail::kHatPalette[a.accountColor < kHatColorCount ? a.accountColor : kHatInk][0]);
  lineAt(c, f, lineH, right - nw, y, a.account, T.ink, 133);
  return w;
}

// The head: the pet (on its baseline, boxed, or boxed with "tap to hatch"),
// the chip, hearts and level, the name and the session.
static inline void head(Canvas& c, const Theme& T, const PanelInput& in, const Agent& a, const BigHead& h, bool onOrange) {
  using namespace big_ui;
  const PanelUi& ui = *in.ui;
  const Pose pose = poseOf(a, in.nowEpoch);
  const Rect& z = h.zone;
  const Look k = lookOf(T, a);
  if (h.hatch) {   // a ready egg: in its box, "tap to hatch" under it
    const bool pressed = ui.pressed.kind == Hit::Hatch;
    c.rect(z.x, z.y, z.w, z.h, kLevel);
    c.rect(z.x + 2, z.y + 2, z.w - 4, z.h - 4, T.panel);
    petFit(c, Rect{z.x + 4, z.y + 4, z.w - 8, z.h - kCtaH - 6}, kCell, a, pose, in.ms, false);
    c.rect(z.x, z.y + z.h - kCtaH, z.w, kCtaH, pressed ? T.ink : kLevel);
    const int tw = upperW(FONT_XS, "tap to hatch");
    upperAt(c, FONT_XS, z.x + (z.w - tw) / 2, z.y + z.h - kCtaH + (kCtaH - FONT_XS.cellH) / 2, "tap to hatch", T.bg);
  } else if (h.boxed) {   // needs you: on a box, its "?" still
    c.rect(z.x, z.y, z.w, z.h, onOrange ? T.bg : T.panel);
    if (a.pet.has) petFit(c, Rect{z.x + 4, z.y + 4, z.w - 8, z.h - 10}, kCell, a, Pose::Wait, 0, true);
    else drawSprite(c, z.x + z.w / 2 - 7 * 4, z.y + z.h - 6 - 12 * 4, 4, a.species, Pose::Wait, T.accent, onOrange ? T.bg : T.panel, 0, true, a.hat, a.hatColor);
  } else if (a.pet.has) {   // on its baseline, the zone's foot; what reaches higher goes into the margin
    petFit(c, Rect{4, 4, 2 * (z.x + 44) - 8, z.y + 81 - 4}, kCell, a, pose, in.ms, true);
  } else {
    drawSprite(c, z.x + 44 - 7 * 4, z.y + 81 - 12 * 4, 4, a.species, pose, k.body == T.bg || k.body == T.dim ? T.ink : k.body, T.bg,
               in.ms, true, a.hat, a.hatColor);
  }
  const uint16_t ink = onOrange ? T.bg : T.ink;
  chip(c, T, h.textX, h.chipY, a, in.nowEpoch, onOrange);
  if (a.pet.has) petMeta(c, T, h.textX + h.textW, h.chipY, a, onOrange);
  c.text(bigNameFont(a, h.textW), h.textX, h.nameY, a.title[0] ? a.title : a.id, ink, h.textW);
  char sum[64];
  snprintf(sum, sizeof sum, "%s", a.aiTitle[0] ? a.aiTitle : a.folder);
  for (char* p = sum; *p; p++) *p = (char)tolower((unsigned char)*p);
  lineAt(c, FONT_S, kLineS, h.textX, h.sumY, sum, onOrange ? blend565(T.accent, T.bg, 11) : kMuted, h.textW);
}

static inline void card(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  using namespace big_ui;
  const BigCard g = bigCard(S, a, in.scope);
  const PanelUi& ui = *in.ui;
  const int w = S.w - 2 * kPadX, right = S.w - kPadX;
  const bool running = a.status == Status::Running;
  char buf[224], t[8];
  head(c, T, in, a, g.head, false);
  if (running) {   // doing now: the command and its subagents
    const bool subsOnly = !a.actTool[0] && !a.actDetail[0] && a.subagents;
    label(c, kPadX, g.nowY, "doing now", kMuted, subsOnly ? "subagents" : a.actTool[0] ? a.actTool : "thinking", T.dim);
    if (a.subagents) {
      snprintf(buf, sizeof buf, "%u subagent%s", (unsigned)a.subagents, a.subagents == 1 ? "" : "s");
      upperAt(c, FONT_XS, right - upperW(FONT_XS, buf), g.nowY, buf, kMuted);
    }
    c.rect(g.term.x, g.term.y, g.term.w, g.term.h, T.panel);
    const int tx = g.term.x + 12, tw = g.term.w - 24;
    const int dx = a.actTool[0] ? lineAt(c, FONT_L, kLineL, tx, g.term.y + 9, "$ ", kOrange2) : 0;
    lineAt(c, FONT_L, kLineL, tx + dx, g.term.y + 9, a.actDetail[0] ? a.actDetail : a.actTool[0] ? a.actTool : "thinking", T.ink, tw - dx);
    for (int i = 0; i < g.subs; i++) {
      const Subagent& sa = a.subs[i];
      const int y = g.term.y + 9 + kLineL + 5 + i * (5 + kLineM);
      int sx = tx + hook(c, tx, y, kLineM, T.dim);
      sx += lineAt(c, FONT_M13, kLineM, sx, y, sa.title[0] ? sa.title : sa.type[0] ? sa.type : "subagent", kText, tx + tw - sx);
      if (sa.title[0] && sa.type[0] && sx < tx + tw) {
        snprintf(buf, sizeof buf, DECK_DOT "%s", sa.type);
        for (char* p = buf; *p; p++) *p = (char)tolower((unsigned char)*p);
        lineAt(c, FONT_M13, kLineM, sx, y, buf, T.dim, tx + tw - sx);
      }
    }
  } else {         // the status, and the account on the right
    fmtAge(a.statusSince ? in.nowEpoch - a.statusSince : 0, t, sizeof t);
    const char* rest = "";
    uint16_t col = T.ink;
    switch (a.status) {
      case Status::Error: snprintf(buf, sizeof buf, "%s", a.errMsg[0] ? a.errMsg : "error"); col = T.accent; break;
      case Status::Starting: snprintf(buf, sizeof buf, "starting the agent"); break;
      case Status::Exited:
        snprintf(buf, sizeof buf, "%s %s ago", a.lost ? "lost with tmux" : "ended", t);
        if (!a.lost && a.resumable) rest = "it can resume";
        break;
      default:
        snprintf(buf, sizeof buf, "finished %s ago", t);
        if (a.unseen) rest = "nobody has looked yet";
    }
    int room = w;
    if (a.account[0]) {
      upperAt(c, FONT_XS, right - upperW(FONT_XS, "account"), g.nowY, "account", kMuted);
      room -= accountName(c, T, FONT_L, right, g.nowY + kLineXS + 5, kLineL, a) + 19;
    }
    label(c, kPadX, g.nowY, "status", kMuted);
    const int y = g.nowY + kLineXS + 5;
    const int mw = lineAt(c, FONT_L, kLineL, kPadX, y, buf, col, room);
    if (rest[0] && mw < room) {
      snprintf(buf, sizeof buf, DECK_DOT "%s", rest);
      lineAt(c, FONT_L, kLineL, kPadX + mw, y, buf, kMuted, room - mw);
    }
  }
  // The last prompt, or the last answer once it has finished; a working agent's account at the row's end.
  if (g.lastLines > 0) {
    t[0] = '\0';
    if (a.lastAt) {
      char age[8];
      fmtAge(in.nowEpoch - a.lastAt, age, sizeof age);
      snprintf(t, sizeof t, "%s", age);
    }
    char ago[16];
    snprintf(ago, sizeof ago, "%s ago", t);
    label(c, kPadX, g.lastY, a.lastIsAnswer ? "last answer" : "last prompt", kMuted, t[0] ? ago : nullptr, kMuted);
    if (running && a.account[0]) accountName(c, T, FONT_S, right, g.lastY + (kLineXS - kLineS) / 2, kLineS, a);
    textWrapped(c, FONT_M13, kPadX, g.textY + (kLineM - FONT_M13.cellH) / 2, a.lastPrompt[0] ? a.lastPrompt : "-", kText, w,
                g.lastLines, kLineM);
  }
  // Context, and where it runs.
  {
    const int lw = label(c, kPadX, g.ctxY, "context", kMuted);
    snprintf(buf, sizeof buf, a.ctxWindow ? "%d%%" : "--", ctxPct(a));
    const int pw = textW(FONT_S, buf);
    lineAt(c, FONT_S, kLineXS, right - pw, g.ctxY, buf, T.ink);
    segs(c, T, kPadX + lw + 9, g.ctxY + (kLineXS - 4) / 2, w - lw - 9 - pw - 9, a.ctxWindow ? ctxPct(a) : 0, kSegOff);
    buf[0] = '\0';
    const char* tool = a.tool == ToolKind::Claude ? "claude code" : a.tool == ToolKind::Codex ? "codex" : a.tool == ToolKind::Gemini ? "gemini" : "";
    const char* parts[4] = {tool, a.model, a.folder, a.branch};
    for (const char* p : parts) {
      if (!p[0]) continue;
      if (buf[0]) strncat(buf, DECK_DOT, sizeof buf - strlen(buf) - 1);
      strncat(buf, p, sizeof buf - strlen(buf) - 1);
    }
    const char* server = in.fleet ? in.fleet->server : "";
    const int sw = server[0] ? textW(FONT_XS, server) : 0;
    c.text(FONT_XS, kPadX, g.whereY, buf, T.dim, w - (sw ? sw + 11 : 0));
    if (sw) c.text(FONT_XS, right - sw, g.whereY, server, T.dim);
  }
  // The buttons: sharp, in a row.
  const BigButtons b = bigCardButtons(S, a, in.scope);
  const bool armed = (int32_t)(ui.stopArmedUntil - in.ms) > 0;
  for (int i = 0; i < b.n; i++) {
    const bool pressed = ui.pressed.kind == b.kind[i];
    const Rect& r = b.r[i];
    switch (b.kind[i]) {
      case Hit::Back: button(c, r, "", pressed ? T.ink : T.bg, pressed ? T.bg : T.ink, pressed ? T.ink : kRule, true); break;
      case Hit::Focus:
        button(c, r, a.unseen && a.status == Status::Idle ? "review in terminal" : "focus terminal", pressed ? T.accent : T.ink, T.bg,
               pressed ? T.accent : T.ink);
        break;
      case Hit::Resume: button(c, r, "resume", pressed ? T.accent : T.ink, T.bg, pressed ? T.accent : T.ink); break;
      case Hit::Interrupt: button(c, r, "interrupt", pressed ? T.ink : T.bg, pressed ? T.bg : T.ink, pressed ? T.ink : kRule); break;
      default: {
        const bool exited = a.status == Status::Exited, hot = pressed || (armed && !exited);
        button(c, r, exited ? "remove" : armed ? "again" : "stop", hot ? T.accent : T.bg, hot ? T.bg : T.ink, hot ? T.accent : kRule);
      }
    }
  }
}

// "Which session strategy?": the first letter up (_pSentence).
static inline void sentence(char* out, size_t cap, const char* s) {
  snprintf(out, cap, "%s", s);
  if (out[0] >= 'a' && out[0] <= 'z') out[0] = (char)(out[0] - 'a' + 'A');
}

static inline void prompt(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  using namespace big_ui;
  const BigPrompt p = bigPrompt(S, a);
  const PanelUi& ui = *in.ui;
  const bool orange = p.orange;
  const uint16_t ink = orange ? T.bg : T.ink, soft = blend565(T.accent, T.bg, 10);   // the dark text at 62 %
  const int w = S.w - 2 * kPadX;
  char buf[224];
  if (orange) c.fill(T.accent);
  else {   // dark, with an orange edge round the page
    c.fill(T.bg);
    rframe(c, Rect{0, 0, S.w, S.h}, kRadius, 2, T.accent);
  }
  head(c, T, in, a, p.head, orange);
  // What it asks.
  label(c, kPadX, p.askY, "it asks", orange ? ink : kMuted, orange ? "permission" : a.promptCtx[0] ? a.promptCtx : a.waitKind[0] ? a.waitKind : "question",
        orange ? soft : T.dim);
  sentence(buf, sizeof buf, a.waitTitle[0] ? a.waitTitle : orange ? "allow this?" : "question");
  textWrapped(c, FONT_Q24B, kPadX, p.titleY + (kLineQ - FONT_Q24B.cellH) / 2, buf, ink, w, p.titleLines, kLineQ);
  if (p.term.h) {   // the command, and where it runs
    c.rect(p.term.x, p.term.y, p.term.w, p.term.h, T.bg);
    int y = p.term.y + 9;
    if (p.termLines) {
      const bool bash = !strncmp(a.promptCtx, "bash", 4) || !strncmp(a.promptCtx, "Bash", 4);
      const int dx = bash ? lineAt(c, FONT_L, 20, p.term.x + 12, y, "$ ", kOrange2) : 0;
      textWrapped(c, FONT_L, p.term.x + 12 + dx, y + (20 - FONT_L.cellH) / 2, a.waitDetail, T.ink, p.term.w - 24 - dx, p.termLines, 20);
      y += p.termLines * 20;
    }
    if (a.promptCtx[0]) lineAt(c, FONT_XS, kLineXS, p.term.x + 12, y + 5, a.promptCtx, kMuted, p.term.w - 24);
  }
  if (p.detailLines)
    textWrapped(c, FONT_S, kPadX, p.detailY + (kLineS - FONT_S.cellH) / 2, a.waitDetail, kMuted, w, p.detailLines, kLineS);
  // Every answer: the chosen one filled; tap it again to send.
  for (int i = 0; i < p.n; i++) {
    const Rect& o = p.opt[i];
    const bool on = ui.selected == i || (ui.pressed.kind == Hit::Option && ui.pressed.index == i);
    if (orange) {   // a permission's: black rows on orange, the chosen one filled
      c.rect(o.x, o.y, o.w, o.h, T.bg);
      if (!on) c.rect(o.x + 2, o.y + 2, o.w - 4, o.h - 4, T.accent);
      snprintf(buf, sizeof buf, "%s  %s", a.opts[i].key, a.opts[i].label);
      lineAt(c, FONT_M13, o.h, o.x + 13, o.y, buf, on ? T.accent : T.bg, o.w - 26);
      continue;
    }
    // A question's: its key, the answer and (with room) what it means, on the panel colour.
    c.rect(o.x, o.y, o.w, o.h, on ? T.ink : kRowLine);
    if (!on) c.rect(o.x + 1, o.y + 1, o.w - 2, o.h - 2, T.panel);
    const uint16_t fg = on ? T.bg : T.ink, sub = on ? kRule : T.dim;
    char ttl[PANEL_OPTION_LABEL];
    snprintf(ttl, sizeof ttl, "%s", a.opts[i].label);
    const char* hint = nullptr;
    char* dash = strstr(ttl, " \xE2\x80\x94 ");
    if (!dash) dash = strstr(ttl, " - ");
    if (dash) { hint = dash + (dash[1] == '-' ? 3 : 5); *dash = '\0'; }
    c.text(FONT_B15, o.x + 13, o.y + (o.h - FONT_B15.cellH) / 2, a.opts[i].key, fg);
    const int tx = o.x + 13 + 15 + 12, tw = o.x + o.w - 13 - tx;
    if (p.sub && hint && hint[0]) {
      const int top = o.y + (o.h - (FONT_M13.cellH + 1 + FONT_XS.cellH)) / 2;
      c.text(FONT_M13, tx, top, ttl, fg, tw);
      c.text(FONT_XS, tx, top + FONT_M13.cellH + 1, hint, sub, tw);
    } else {
      c.text(FONT_M13, tx, o.y + (o.h - FONT_M13.cellH) / 2, ttl, fg, tw);
    }
  }
  // The foot.
  for (int i = 0; i < p.btn.n; i++) {
    const Rect& r = p.btn.r[i];
    const bool pressed = ui.pressed.kind == p.btn.kind[i];
    switch (p.btn.kind[i]) {
      case Hit::Back:
        if (orange) button(c, r, "", pressed ? T.bg : T.accent, pressed ? T.accent : T.bg, T.bg, true);
        else button(c, r, "", pressed ? T.ink : T.bg, pressed ? T.bg : T.ink, pressed ? T.ink : kRule, true);
        break;
      case Hit::Send: {
        const bool chosen = ui.selected >= 0 && ui.selected < a.optCount;
        if (chosen) snprintf(buf, sizeof buf, "send %s", a.opts[ui.selected].key);
        else snprintf(buf, sizeof buf, "send");
        const uint16_t fill = pressed ? T.ink : chosen ? T.accent : T.panel;
        button(c, r, buf, fill, pressed || chosen ? T.bg : T.dim, fill);
        break;
      }
      default:
        if (orange) {   // a link: "FOCUS TERMINAL", underlined
          if (pressed) c.rect(r.x, r.y, r.w, r.h, T.bg);
          const uint16_t col = pressed ? T.accent : T.bg;
          const int tw = upperW(FONT_XS, "focus terminal"), ty = r.y + (r.h - FONT_XS.cellH) / 2;
          upperAt(c, FONT_XS, r.x + (r.w - tw) / 2, ty, "focus terminal", col);
          c.rect(r.x + (r.w - tw) / 2, ty + FONT_XS.cellH, tw, 2, col);
        } else {
          button(c, r, "focus terminal", pressed ? T.ink : T.bg, pressed ? T.bg : T.ink, pressed ? T.ink : kRule);
        }
    }
  }
  if (orange) {
    const int x = p.btn.r[0].x + p.btn.r[0].w + 9;
    upperAt(c, FONT_XS, x, p.orY, "or answer at the terminal", ink, p.btn.r[1].x - 8 - x);
  } else if (!p.n) {
    upperAt(c, FONT_XS, kPadX, p.btn.r[0].y - 9 - kLineXS, "type your answer at the terminal", kMuted, w);
  }
}

// _Notice: a centred critter, a title, a few lines (one of them a command).
static inline void notice(Canvas& c, const BigSpec& S, const Theme& T, Pose pose, const char* title, const char* l1,
                          const char* code, const char* l2, uint32_t ms) {
  const int s = S.noticeScale;
  const Font& tf = *S.title;
  const Font& lf = *S.text;
  const Font& cf = *S.body;
  const int lines = (l1 ? 1 : 0) + (code ? 1 : 0) + (l2 ? 1 : 0);
  int y = S.h / 2 - (13 * s + bk(S, 28) + fontPx(tf) + lines * (bk(S, 12) + fontPx(cf))) / 2;
  drawSprite(c, S.w / 2 - 7 * s, y, s, Species::Claude, pose, T.dim, T.bg, ms, true);
  y += 13 * s + bk(S, 28);
  const int maxW = S.w - 2 * bk(S, 48);
  auto centred = [&](const Font& f, const char* t, uint16_t col) {
    const int w = textW(f, t) < maxW ? textW(f, t) : (maxW / f.cellW) * f.cellW;
    textAt(c, f, (S.w - w) / 2, y, t, col, maxW);
  };
  centred(tf, title, T.ink);
  y += fontPx(tf);
  if (l1) { y += bk(S, 12); centred(lf, l1, T.dim); y += fontPx(cf); }
  if (code) { y += bk(S, 12); centred(cf, code, T.accent); y += fontPx(cf); }
  if (l2) { y += bk(S, 12); centred(lf, l2, T.dim); }
}

// A toast: in the middle; on a notice, low (bottom = true), clear of its text.
static inline void toast(Canvas& c, const BigSpec& S, const Theme& T, const char* s, bool bottom = false) {
  const Font& f = *S.body;
  const int w = textW(f, s) + 2 * bk(S, 24), h = fontPx(f) + 2 * bk(S, 14);
  const Rect r{(S.w - w) / 2, bottom ? S.h - bk(S, 48) - h : (S.h - h) / 2, w, h};
  rrect(c, r, h / 2, T.ink);
  rrect(c, Rect{r.x + 2, r.y + 2, r.w - 4, r.h - 4}, h / 2 - 2, T.panel);
  textAt(c, f, r.x + bk(S, 24), r.y + bk(S, 14), s, T.ink);
}

}  // namespace big_detail

static inline void drawBig(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in) {
  using namespace big_detail;
  SQ_T0(tf);
  c.fill(T.bg);
  SQ_ADD(fill, tf);
  char buf[96];
  const PanelUi* tu = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0 ? in.ui : nullptr;
  switch (in.link) {
    case LinkState::Connecting:
      if (in.net && netNoticeShown(*in.net)) {   // where Wi-Fi or Bluetooth is (net_model.h's words); a problem in orange
        const NetNotice n = netNotice(*in.net);
        if (n.problem || n.code) notice(c, S, T, n.problem ? Pose::Exit : Pose::Think, n.title, nullptr, n.line1, n.line2, in.ms);
        else notice(c, S, T, Pose::Think, n.title, n.line1, nullptr, n.line2, in.ms);
      } else {
        notice(c, S, T, Pose::Think, "connecting to the deck", "plugged into the Mac?", "agentctl link add <port>",
               nullptr, in.ms);
      }
      if (tu) toast(c, S, T, tu->toast, true);
      return;
    case LinkState::Unapproved:
      snprintf(buf, sizeof buf, "agentctl devices approve %s", in.device && in.device[0] ? in.device : "<name>");
      notice(c, S, T, Pose::Wait, "waiting for approval", "on the Mac, run", buf, nullptr, in.ms);
      if (tu) toast(c, S, T, tu->toast, true);
      return;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0) {
    if (in.link == LinkState::Lost) {
      char seen[8];
      fmtAge(in.silentSec, seen, sizeof seen);
      snprintf(buf, sizeof buf, "last seen %s ago" DECK_DOT "retrying\xE2\x80\xA6", seen);
      notice(c, S, T, Pose::Exit, "deck offline", buf, "agentctl serve", nullptr, in.ms);
    } else {
      notice(c, S, T, Pose::Idle, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr,
             "agentctl new", nullptr, in.ms);
    }
    if (tu) toast(c, S, T, tu->toast, true);
    return;
  }
  const PanelUi& ui = *in.ui;
  const int open = ui.screen != Screen::Home ? findAgent(*in.fleet, ui.openId) : -1;
  if (open >= 0 && ui.screen == Screen::Card) card(c, S, T, in, in.fleet->agents[open]);
  else if (open >= 0) prompt(c, S, T, in, in.fleet->agents[open]);
  else home(c, S, T, in);
  if (tu) toast(c, S, T, ui.toast);
}

// The big panels' PanelLayout, with the board's panel (control_panel.h)
// pulled down from the top strip of any screen, as on the square grid.
#include "drawer_slide.h"
#include "layout_square.h"

class BigPanel : public PanelLayout {
 public:
  explicit BigPanel(const BigSpec& s) : S(s), T(deckTheme(false)), G(controlGeom(s.w, s.h, 16, 4.0f, false)) {}
  int width() const override { return S.w; }
  int height() const override { return S.h; }
  bool trueBlack() const override { return false; }
  void draw(Canvas& c, const PanelInput& in) const override {
    if (slideHome(c, in)) return;                   // home's pages under the finger: cut from two pictures
    const ControlUi* u = in.ui && in.ctl ? &in.ui->ctl : nullptr;
    // While the panel moves, the screen and the panel are each drawn once, then slid.
    if (u && (u->pulling || u->animating)) {
      ControlUi full = *u;
      full.open = 1.f;
      full.pulling = full.animating = full.down = false;
      full.pressed = ControlHit::None;
      if (drawer.slide(c, u->open, in.rowsOk, T.ringTrack, [&](Canvas& k) { drawBig(k, S, T, in); },
                       [&](Canvas& k) { controlDraw(k, G, T, *in.ctl, full, in.ms); }))
        return;
    }
    drawer.reset();
    const bool covered = u && u->open >= 0.999f && !u->animating && !u->pulling;
    if (!covered) drawBig(c, S, T, in);              // all the way down, what is under it doesn't show
    if (u && controlShown(*u)) controlDraw(c, G, T, *in.ctl, *u, in.ms);
  }
  const uint16_t* const* frameRows() const override { return drawer.rows(); }
  const FrameSplit* frameSplit() const override { return pages.split(); }
  bool panelTouch(PanelUi& ui, bool down, int x, int y, uint32_t ms) const override {
    const bool was = ui.ctl.down;
    const bool took = controlEdgeTouch(ui.ctl, G, down, x, y, ms, S.h / 8);
    if (took && !was) { ui.down = false; ui.pressed = PanelHit(); }   // the screen lets go of the finger
    return took;
  }
  DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope, uint32_t ms) const override {
    return bigTouch(ui, S, f, down, x, y, scope, ms);
  }
  bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const override {
    const bool panel = controlTick(ui.ctl, ms);
    return bigTick(ui, f, ms) || panel;
  }
  void button(PanelUi& ui, const Fleet& f) const override {
    if (controlShown(ui.ctl)) {                      // BOOT closes the panel first
      ui.ctl = ControlUi();
      return;
    }
    bigButton(ui, f);
  }
  void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const override { bigResult(ui, seq, code, ms); }
  const BigSpec S;
  const Theme T;
  const ControlGeom G;

 private:
  mutable DrawerSlide drawer;
  mutable PageSlide pages;

  // Home's pages while they slide (page_slide.h): each page drawn once (no
  // toast), every frame cut from the two either side of the finger.
  bool slideHome(Canvas& c, const PanelInput& in) const {
    const bool moving = in.ui && in.fleet && in.fleet->count > 0 && pagerMoving(*in.ui) && in.ui->screen == Screen::Home &&
                        (in.link == LinkState::Live || in.link == LinkState::Lost) && !(in.ctl && controlShown(in.ui->ctl));
    if (!moving) {
      pages.reset();
      return false;
    }
    drawer.reset();
    const PanelUi& ui = *in.ui;
    const bool ok = pages.slide(c, pagerAt(ui), pagerCount(ui, bigPages(*in.fleet)), in.splitOk, T.bg, [&](Canvas& k, int i) {
      PanelUi u = ui;
      u.sliding = u.slideAnim = false;
      u.toast[0] = '\0';
      u.pressed = PanelHit();
      pagerGo(u, i);
      PanelInput pi = in;
      pi.ui = &u;
      drawBig(k, S, T, pi);
    });
    if (!ok) return false;
    if (!pages.split() && ui.toast[0] && (int32_t)(ui.toastUntil - in.ms) > 0) big_detail::toast(c, S, T, ui.toast);
    return true;
  }
};
