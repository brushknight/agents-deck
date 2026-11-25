#pragma once
// The big panel layout: the Flutter desk panel's UI (clients/flutter/
// agents_ui: AgentsApp, AgentTile, DeckGrid, agent_detail.dart) drawn at a
// scale. The desk panel is a 4" 720 x 720 screen; a board of the same size
// at 480 x 480 gets every Flutter size times 2/3 (specBig480()), with the
// nearest bitmap fonts and whole-number critter scales. A 720 x 720 board
// would use k = 1 and larger fonts.
//
//   home    the deck's 4 x 4 grid of square tiles by slot, paged, with the
//           page bars under it (AgentTile, DeckGrid, _PageBars)
//   card    the agent: its pet (or critter) on its baseline, the status
//           chip, hearts and level, the name, the session and the level bar;
//           doing now with the command and the subagents; the last prompt;
//           context and where it runs; and square buttons in a row: focus,
//           interrupt while running, otherwise stop ("again" arms it for
//           3 s) or resume and remove once exited, and back to the deck. An egg ready to hatch sits
//           in a box that says "tap to hatch" (HatchPet).
//   prompt  the whole screen orange: the pet boxed with its "?", what it
//           asks, the command, and every answer as a row; tap one to choose
//           it, tap it again to send; focus at the foot
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

// ---------------------------------------------------------------- the card's parts
// The agent's own screens on the 4" (the boards' pass 3 design): sharp
// corners, the firmware's Roboto Mono sizes, the pet on its baseline (no line drawn).
//
//   card    the pet in its zone (or, an egg ready to hatch, in a box with
//           "tap to hatch"), the status chip, hearts and level, the name, the
//           session's title and the level bar; doing now with the command and
//           the subagents in a box (or the status); the last prompt; context
//           and where it runs; focus, interrupt and stop as squares in a row
//   prompt  the whole screen orange: the pet boxed with its "?", the name,
//           the chip, hearts and level; what it asks, the command, and every
//           answer as a row (tap one to choose it, tap it again to send); focus

namespace big_ui {   // the design's numbers
constexpr int kM = 18, kGap = 10;
constexpr int kZoneW = 116, kZoneH = 92, kCell = 5;   // the pet's zone and its scale
constexpr int kBoxCell = 4, kCtaH = 20;                // the ready egg's box
constexpr int kAskBox = 64, kAskCell = 3;              // the boxed pet on a prompt
constexpr int kAct = 62, kActGap = 10;                 // the square buttons
constexpr int kChipH = 18, kLineXS = 13, kLineS = 15, kLineM = 17, kLineL = 19, kLineB20 = 25, kLineQ = 29;
}  // namespace big_ui

// The button row: squares in a centred row at the foot.
struct BigButtons {
  int n = 0;
  Hit kind[4];
  Rect r[4];
};

static inline Rect bigCardInner(const BigSpec& S) {
  return Rect{big_ui::kM, big_ui::kM, S.w - 2 * big_ui::kM, S.h - 2 * big_ui::kM};
}

// A ready egg: its box says "tap to hatch".
static inline bool bigHatchable(const Agent& a) { return a.pet.has && a.pet.stage == DECK_PET_EGG && a.pet.ready; }

static inline BigButtons bigCardButtons(const BigSpec& S, const Agent& a, DeckScope scope, int size = big_ui::kAct) {
  BigButtons b;
  const bool exited = a.status == Status::Exited, running = a.status == Status::Running;
  // What the scope allows: focus and interrupt need act, stop / remove / resume admin.
  const bool act = canAct(scope), admin = scope == DeckScope::Admin;
  if (act && !exited) {
    b.kind[b.n++] = Hit::Focus;
    if (running) b.kind[b.n++] = Hit::Interrupt;
    if (admin) b.kind[b.n++] = Hit::Stop;
  } else if (act && admin) {
    if (a.resumable) b.kind[b.n++] = Hit::Resume;
    b.kind[b.n++] = Hit::Stop;
  }
  b.kind[b.n++] = Hit::Back;   // back to the deck, as the menu bar app's agent page has it
  const int w = b.n * size + (b.n - 1) * big_ui::kActGap;
  for (int i = 0; i < b.n; i++)
    b.r[i] = Rect{(S.w - w) / 2 + i * (size + big_ui::kActGap), S.h - big_ui::kM - size, size, size};
  return b;
}

// Where the card's parts go.
struct BigCard {
  Rect zone;                   // the pet's zone, or the ready egg's box
  bool box;                    // the box (with "tap to hatch")
  int textX, textW;            // the column beside it
  int chipY, nameY, sumY, barY;
  int nowY;                    // "doing now" (or "status")
  Rect term;                   // the command box (h 0: none)
  int subs;                    // subagent lines in it
  int lastY, lastLines;        // the last prompt (lastLines 0: no room)
  int ctxY, whereY;
};

static inline BigCard bigCard(const BigSpec& S, const Agent& a, DeckScope scope) {
  using namespace big_ui;
  BigCard g{};
  g.box = bigHatchable(a);
  const int zh = g.box ? kZoneH + kCtaH : kZoneH;
  g.zone = Rect{kM, kM, kZoneW, zh};
  g.textX = kM + kZoneW + kGap;
  g.textW = S.w - kM - g.textX;
  // The column, from its foot (the zone's) up: the level bar, the summary, the name, the chip.
  const int foot = g.zone.y + g.zone.h - 2;
  g.barY = foot - 3;
  g.sumY = g.barY - 3 - kLineS;
  g.nameY = g.sumY - 3 - kLineB20;
  g.chipY = g.nameY - 3 - kChipH;
  int y = g.zone.y + g.zone.h + kGap;
  g.nowY = y;
  y += kLineXS + 3;
  if (a.status == Status::Running) {
    g.subs = a.subCount;
    if (g.subs > 3) g.subs = 3;
    g.term = Rect{kM, y, S.w - 2 * kM, 6 + kLineL + g.subs * (2 + kLineM) + 6};
    y += g.term.h;
  } else {
    y += kLineL;
  }
  y += kGap;
  g.lastY = y;
  // The foot: the buttons, where it runs, context.
  const BigButtons b = bigCardButtons(S, a, scope);
  const int btnTop = b.n ? b.r[0].y : S.h - kM;
  g.whereY = btnTop - (b.n ? kGap : 0) - kLineXS;
  g.ctxY = g.whereY + 5 - kGap - kLineXS;
  g.lastLines = (g.ctxY - kGap - (g.lastY + kLineXS + 2)) / kLineM;
  if (g.lastLines > 4) g.lastLines = 4;
  if (g.lastLines < 0) g.lastLines = 0;
  return g;
}

// Where the prompt's parts go.
struct BigPrompt {
  Rect pet;                    // the boxed pet
  int nameY, chipY;
  int askY, titleY, titleLines;
  Rect term;                   // the command box (h 0: none)
  int termLines;
  int n;
  Rect opt[kMaxOptions];
  Rect terminal;               // focus
  int orY;                     // "or at the terminal"
};

static inline int bigWrapLines(const Font& f, const char* s, int maxW, int maxLines) {
  Canvas none{nullptr, 0, 0};
  return textWrapped(none, f, 0, 0, s, 0, maxW, maxLines);
}

static inline BigPrompt bigPrompt(const BigSpec& S, const Agent& a) {
  using namespace big_ui;
  BigPrompt p{};
  const int w = S.w - 2 * kM;
  p.pet = Rect{kM, kM, kAskBox, kAskBox};
  const int block = kLineB20 + 3 + kChipH;
  p.nameY = kM + (kAskBox - block) / 2;
  p.chipY = p.nameY + kLineB20 + 3;
  int y = kM + kAskBox + kGap;
  p.askY = y;
  y += kLineXS + 2;
  p.titleY = y;
  p.titleLines = bigWrapLines(FONT_Q24B, a.waitTitle[0] ? a.waitTitle : "needs you", w, 2);
  if (p.titleLines < 1) p.titleLines = 1;
  y += p.titleLines * kLineQ + kGap;
  // The focus square at the foot, the answers above it.
  const int act = kAct - 8;
  p.terminal = Rect{0, S.h - kM - act, act, act};
  p.orY = p.terminal.y + (act - kLineXS) / 2;
  p.n = a.optCount;
  const int rowH = 2 + 6 + kLineL + 6 + 2, gap = kGap - 2;
  const int bottom = p.terminal.y - kGap;
  int optTop = bottom - p.n * rowH - (p.n > 0 ? (p.n - 1) * gap : 0);
  // The command box between the question and the answers.
  if (a.waitDetail[0] || a.promptCtx[0]) {
    int lines = bigWrapLines(FONT_L, a.waitDetail[0] ? a.waitDetail : " ", w - 16, 3);
    const int ctxH = a.promptCtx[0] ? kLineXS + 2 : 0;
    while (lines > 1 && y + 12 + lines * kLineL + ctxH + kGap > optTop) lines--;
    if (y + 12 + lines * kLineL + ctxH + kGap <= optTop || !p.n) {
      p.termLines = a.waitDetail[0] ? lines : 0;
      p.term = Rect{kM, y, w, 12 + p.termLines * kLineL + ctxH};
      y += p.term.h + kGap;
    }
  }
  // The answers right under it; too many for the room, they get thinner.
  optTop = y;
  int pitch = rowH + gap;
  if (p.n && optTop + p.n * pitch - gap > bottom) pitch = (bottom - optTop + gap) / p.n;
  for (int i = 0; i < p.n; i++)
    p.opt[i] = Rect{kM, optTop + i * pitch, w, pitch - gap < rowH ? pitch - gap : rowH};
  // "or at the terminal" and the square, centred together.
  const int orW = textW(FONT_XS, "OR AT THE TERMINAL");
  const int total = orW + 12 + act;
  p.terminal.x = (S.w - total) / 2 + orW + 12;
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
    if (bigHatchable(a) && canAct(scope) && bigCard(S, a, scope).zone.contains(x, y)) { h.kind = Hit::Hatch; return h; }
    return PanelHit();
  }
  const BigPrompt p = bigPrompt(S, a);
  for (int k = 0; k < p.n; k++)
    if (p.opt[k].contains(x, y)) { h.kind = Hit::Option; h.index = k; return h; }
  if (p.terminal.contains(x, y)) { h.kind = Hit::Focus; return h; }
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
  const float unit = 92.f / 18.f;                    // the deck's critter: px per sprite unit
  const int feet = oy + d(30 + 12 * unit);
  drawCritter(c, ox + side / 2 - 7 * s, feet - 12 * s, s, a, poseOf(a, now), cc(k.body), fill, ms, true);
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

// ---------------------------------------------------------------- the card and the prompt
// The design's colours (mock3/boards3.css): on the dark screen and on orange.
constexpr uint16_t kRule = rgb565(0x3B, 0x3A, 0x36);
constexpr uint16_t kText = rgb565(0xC9, 0xC5, 0xBC);
constexpr uint16_t kMuted = rgb565(0x9A, 0x96, 0x8C);
constexpr uint16_t kOrange2 = rgb565(0xFF, 0x6A, 0x2E);
constexpr uint16_t kLevel = rgb565(0xFF, 0x8A, 0x4C);
constexpr uint16_t kBarTrack = rgb565(0x2E, 0x2D, 0x2A);
constexpr uint16_t kSegOff = rgb565(0x2A, 0x29, 0x25);
constexpr uint16_t kHeart = rgb565(0xE8, 0x45, 0x2B);

// Text in capitals (the design's small labels), cut to maxW.
static inline int upperAt(Canvas& c, const Font& f, int x, int y, const char* s, uint16_t col, int maxW = -1) {
  char b[128];
  size_t i = 0;
  for (; s[i] && i + 1 < sizeof b; i++) b[i] = (char)toupper((unsigned char)s[i]);
  b[i] = '\0';
  return c.text(f, x, y, b, col, maxW);
}

// A text line whose line box (lineH) starts at y: the glyph cell centred in it.
static inline int lineAt(Canvas& c, const Font& f, int lineH, int x, int y, const char* s, uint16_t col, int maxW = -1) {
  return c.text(f, x, y + (lineH - f.cellH) / 2, s, col, maxW);
}

// The status chip: "WORKING · 38M" on ink, "NEEDS YOU" in orange on black,
// "HUNGRY" on orange, "IDLE" in an outline. Returns its width.
static inline int chip(Canvas& c, const Theme& T, int x, int y, const Agent& a, int64_t now, bool onOrange) {
  char t[8], s[40];
  fmtAge(a.statusSince ? now - a.statusSince : 0, t, sizeof t);
  uint16_t fill = T.bg, ink = kMuted, edge = kRule;
  switch (a.status) {
    case Status::Running: snprintf(s, sizeof s, "working" DECK_DOT "%s", t); fill = edge = T.ink; ink = T.bg; break;
    case Status::Waiting: snprintf(s, sizeof s, "needs you" DECK_DOT "%s", t); fill = edge = T.bg; ink = T.accent; break;
    case Status::Error: snprintf(s, sizeof s, "error"); fill = edge = T.accent; ink = T.bg; break;
    case Status::Starting: snprintf(s, sizeof s, "starting" DECK_DOT "%s", t); break;
    case Status::Exited: snprintf(s, sizeof s, "%s" DECK_DOT "%s", a.lost ? "lost" : "exited", t); break;
    default:
      if (a.unseen) { snprintf(s, sizeof s, "hungry" DECK_DOT "%s", t); fill = edge = T.accent; ink = T.bg; }
      else snprintf(s, sizeof s, "idle" DECK_DOT "%s", t);
  }
  if (onOrange && a.status != Status::Waiting) { fill = edge = T.bg; ink = T.accent; }
  const int w = textW(FONT_XS, s) + 12;
  c.rect(x, y, w, big_ui::kChipH, edge);
  if (fill != edge) c.rect(x + 1, y + 1, w - 2, big_ui::kChipH - 2, fill);
  upperAt(c, FONT_XS, x + 6, y + (big_ui::kChipH - FONT_XS.cellH) / 2, s, ink);
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

// Hearts and level ("♥♥♥♡ LV 14"), "EGG · 84%", or the "READY TO HATCH" chip,
// right-aligned at `right`.
static inline void petMeta(Canvas& c, const Theme& T, int right, int y, const Agent& a, int px, bool onOrange, uint16_t bg) {
  const deck_pet_t& p = a.pet;
  const uint16_t ink = onOrange ? T.bg : T.ink;
  char s[24];
  if (p.stage == DECK_PET_EGG) {
    if (p.ready) {
      const int w = textW(FONT_XS, "READY TO HATCH") + 12;
      c.rect(right - w, y, w, big_ui::kChipH, kLevel);
      upperAt(c, FONT_XS, right - w + 6, y + (big_ui::kChipH - FONT_XS.cellH) / 2, "ready to hatch", T.bg);
    } else {
      snprintf(s, sizeof s, "egg" DECK_DOT "%d%%", p.progress * 100 / 255);
      upperAt(c, FONT_XS, right - textW(FONT_XS, s), y + (big_ui::kChipH - FONT_XS.cellH) / 2, s, ink);
    }
    return;
  }
  snprintf(s, sizeof s, "LV %u", (unsigned)p.level);
  int x = right - textW(FONT_XS, s);
  c.text(FONT_XS, x, y + (big_ui::kChipH - FONT_XS.cellH) / 2, s, ink);
  x -= 8;
  const int hw = 7 * px, hh = 6 * px;
  for (int i = DECK_PET_HEARTS - 1; i >= 0; i--) {
    x -= hw;
    heart(c, x, y + (big_ui::kChipH - hh) / 2, px, i < p.hearts, onOrange, bg);
    x -= 2;
  }
}

// The pet standing on its zone's baseline at groundY (cell `s`); with
// pets off the critter, in body on cut.
static inline void petZone(Canvas& c, const Rect& z, int s, const Agent& a, Pose pose, uint32_t ms, bool bubble, int groundY,
                           uint16_t body, uint16_t cut) {
  if (a.pet.has) drawPetCells(c, z.x + z.w / 2 - 9 * s, groundY - petBottom(a) * s, s, a, pose, ms, bubble);
  else drawSprite(c, z.x + z.w / 2 - 7 * s, groundY - 12 * s, s, a.species, pose, body, cut, ms, bubble, a.hat, a.hatColor);
}

// The design's icons on a 16-unit grid: focus (a terminal), interrupt (pause), stop.
static inline void icon(Canvas& c, Hit kind, int cx, int cy, int size, uint16_t col) {
  const float u = size / 16.f;
  auto r = [&](float x, float y, float w, float h) {
    c.rect(cx - size / 2 + (int)lroundf(x * u), cy - size / 2 + (int)lroundf(y * u), (int)lroundf(w * u) < 1 ? 1 : (int)lroundf(w * u),
           (int)lroundf(h * u) < 1 ? 1 : (int)lroundf(h * u), col);
  };
  switch (kind) {
    case Hit::Focus: {
      const float t = 1.6f;
      r(1.5f - t / 2, 2.5f - t / 2, 13 + t, t);
      r(1.5f - t / 2, 13.5f - t / 2, 13 + t, t);
      r(1.5f - t / 2, 2.5f - t / 2, t, 11 + t);
      r(14.5f - t / 2, 2.5f - t / 2, t, 11 + t);
      for (int i = 0; i < 3; i++) r(4 + i * 0.85f, 6 + i * 0.7f - 0.6f, 1.4f, 1.4f);   // ">"
      for (int i = 0; i < 2; i++) r(4 + (1 - i) * 0.85f, 8.1f + i * 0.7f, 1.4f, 1.4f);
      r(8, 10.5f - 0.8f, 4, 1.6f);                                                   // "_"
      break;
    }
    case Hit::Interrupt: r(4, 3, 3, 10); r(9, 3, 3, 10); break;
    case Hit::Resume: r(5, 3, 2, 10); r(7, 4, 2, 8); r(9, 5, 2, 6); r(11, 7, 1, 2); break;
    case Hit::Back:   // a chevron pointing left
      for (int i = 0; i < 5; i++) { r(9 - i, 3 + i, 2, 2); r(9 - i, 11 - i, 2, 2); }
      break;
    default: r(4, 4, 8, 8);
  }
}

// A square button: its icon over its label in capitals.
static inline void act(Canvas& c, const Theme& T, const Rect& r, Hit kind, const char* label, bool pri, bool stop,
                       bool pressed, bool onOrange) {
  uint16_t fill = onOrange ? T.accent : T.bg, edge = onOrange ? T.bg : kRule, ink = onOrange ? T.bg : T.ink;
  if (pri) { fill = edge = onOrange ? T.bg : T.ink; ink = onOrange ? T.accent : T.bg; }
  if (stop && !onOrange) { edge = ink = T.accent; }
  if (pressed) { fill = edge = T.accent; ink = T.bg; }
  c.rect(r.x, r.y, r.w, r.h, edge);
  c.rect(r.x + 2, r.y + 2, r.w - 4, r.h - 4, fill);
  const int is = (int)lroundf(r.w * 0.34f), lh = FONT_XS.cellH;
  const int block = is + 3 + lh, top = r.y + (r.h - block) / 2;
  icon(c, kind, r.x + r.w / 2, top + is / 2, is, ink);
  char b[24];
  snprintf(b, sizeof b, "%s", label);
  for (char* p = b; *p; p++) *p = (char)toupper((unsigned char)*p);
  const int w = textW(FONT_XS, b) < r.w - 4 ? textW(FONT_XS, b) : r.w - 4;
  c.text(FONT_XS, r.x + (r.w - w) / 2, top + is + 3, b, ink, r.w - 4);
}

// "↳": a hook down and to the right, for a subagent's line.
static inline int hook(Canvas& c, int x, int y, int lineH, uint16_t col) {
  const int top = y + lineH / 2 - 4;
  c.rect(x + 1, top, 1, 6, col);
  c.rect(x + 1, top + 5, 6, 1, col);
  c.rect(x + 5, top + 3, 1, 5, col);
  return 12;
}

// The context bar: n segments, 2 px apart.
static inline void segs(Canvas& c, const Theme& T, int x, int y, int w, int pct, uint16_t off) {
  const int n = 40, gap = 2, sw = (w - (n - 1) * gap) / n;
  const int on = (pct * n + 50) / 100;
  for (int i = 0; i < n; i++) c.rect(x + i * (sw + gap), y, sw, 6, i < on ? T.ink : off);
}

static inline void card(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  using namespace big_ui;
  const BigCard g = bigCard(S, a, in.scope);
  const PanelUi& ui = *in.ui;
  const Pose pose = poseOf(a, in.nowEpoch);
  char buf[224];
  // The pet: on its baseline, or (an egg ready to hatch) in its box with "tap to hatch".
  if (g.box) {
    const bool pressed = ui.pressed.kind == Hit::Hatch;
    c.rect(g.zone.x, g.zone.y, g.zone.w, g.zone.h, kLevel);
    c.rect(g.zone.x + 2, g.zone.y + 2, g.zone.w - 4, g.zone.h - 4, T.panel);
    petZone(c, g.zone, kBoxCell, a, pose, in.ms, false, g.zone.y + g.zone.h - kCtaH - 2 * kBoxCell, T.ink, T.panel);
    c.rect(g.zone.x, g.zone.y + g.zone.h - kCtaH, g.zone.w, kCtaH, pressed ? T.ink : kLevel);
    const int tw = textW(FONT_XS, "TAP TO HATCH");
    upperAt(c, FONT_XS, g.zone.x + (g.zone.w - tw) / 2, g.zone.y + g.zone.h - kCtaH + (kCtaH - FONT_XS.cellH) / 2, "tap to hatch", T.bg);
  } else {   // with pets off the critter, at the pet's size
    const Look k = lookOf(T, a);
    petZone(c, g.zone, a.pet.has ? kCell : kCell - 1, a, pose, in.ms, true, g.zone.y + g.zone.h - 2 - kCell,
            k.body == T.bg || k.body == T.dim ? T.ink : k.body, T.bg);
  }
  // The column: chip, hearts and level; the name; the session; the level bar.
  chip(c, T, g.textX, g.chipY, a, in.nowEpoch, false);
  if (a.pet.has) petMeta(c, T, g.textX + g.textW, g.chipY, a, 2, false, T.bg);
  lineAt(c, FONT_B20, kLineB20, g.textX, g.nameY, a.title[0] ? a.title : a.id, T.ink, g.textW);
  lineAt(c, FONT_S, kLineS, g.textX, g.sumY, a.aiTitle[0] ? a.aiTitle : a.folder, kMuted, g.textW);
  if (a.pet.has) {   // how far it is to its next level
    c.rect(g.textX, g.barY, g.textW, 3, kBarTrack);
    c.rect(g.textX, g.barY, g.textW * a.pet.progress / 255, 3, kLevel);
  }
  // Doing now: the command and the subagents; or the status.
  const int w = S.w - 2 * kM;
  if (a.status == Status::Running) {
    int x = upperAt(c, FONT_XS, kM, g.nowY, "doing now", kMuted) + kM;
    if (a.actTool[0]) {
      snprintf(buf, sizeof buf, DECK_DOT "%s", a.actTool);
      upperAt(c, FONT_XS, x, g.nowY, buf, T.dim);
    }
    if (a.subagents) {
      snprintf(buf, sizeof buf, "%u subagent%s", (unsigned)a.subagents, a.subagents == 1 ? "" : "s");
      char up[40];
      size_t i = 0;
      for (; buf[i] && i + 1 < sizeof up; i++) up[i] = (char)toupper((unsigned char)buf[i]);
      up[i] = '\0';
      c.text(FONT_XS, kM + w - textW(FONT_XS, up), g.nowY, up, kMuted);
    }
    c.rect(g.term.x, g.term.y, g.term.w, g.term.h, T.panel);
    const int tx = g.term.x + 8, tw = g.term.w - 16;
    const int dx = lineAt(c, FONT_L, kLineL, tx, g.term.y + 6, "$ ", kOrange2);
    lineAt(c, FONT_L, kLineL, tx + dx, g.term.y + 6, a.actDetail[0] ? a.actDetail : a.actTool[0] ? a.actTool : "thinking", T.ink, tw - dx);
    for (int i = 0; i < g.subs; i++) {
      const Subagent& sa = a.subs[i];
      const int y = g.term.y + 6 + kLineL + 2 + i * (2 + kLineM);
      int sx = tx + hook(c, tx, y, kLineM, T.dim);
      sx += lineAt(c, FONT_M13, kLineM, sx, y, sa.title[0] ? sa.title : "subagent", kText, tx + tw - sx);
      if (sa.type[0] && sx < tx + tw) {
        snprintf(buf, sizeof buf, DECK_DOT "%s", sa.type);
        lineAt(c, FONT_M13, kLineM, sx, y, buf, T.dim, tx + tw - sx);
      }
    }
  } else {
    upperAt(c, FONT_XS, kM, g.nowY, a.status == Status::Error ? "error" : "status", kMuted);
    char t[8];
    fmtAge(a.statusSince ? in.nowEpoch - a.statusSince : 0, t, sizeof t);
    uint16_t col = T.ink;
    switch (a.status) {
      case Status::Error: snprintf(buf, sizeof buf, "%s", a.errMsg[0] ? a.errMsg : "error"); col = T.accent; break;
      case Status::Starting: snprintf(buf, sizeof buf, "starting\xE2\x80\xA6"); break;
      case Status::Exited: snprintf(buf, sizeof buf, "%s %s ago", a.lost ? "lost with tmux" : "exited", t); break;
      default: snprintf(buf, sizeof buf, "finished %s ago%s", t, a.unseen ? DECK_DOT "nobody looked" : "");
    }
    lineAt(c, FONT_L, kLineL, kM, g.nowY + kLineXS + 3, buf, col, w);
  }
  // The last prompt.
  if (g.lastLines > 0) {
    upperAt(c, FONT_XS, kM, g.lastY, "last prompt", kMuted);
    textWrapped(c, FONT_M13, kM, g.lastY + kLineXS + 2 + (kLineM - FONT_M13.cellH) / 2,
                a.lastPrompt[0] ? a.lastPrompt : "\xE2\x80\x94", kText, w, g.lastLines, kLineM);
  }
  // Context, and where it runs.
  {
    const int lw = upperAt(c, FONT_XS, kM, g.ctxY, "context", kMuted);
    snprintf(buf, sizeof buf, a.ctxWindow ? "%d%%" : "--", ctxPct(a));
    const int pw = textW(FONT_XS, buf);
    c.text(FONT_XS, kM + w - pw, g.ctxY, buf, T.ink);
    segs(c, T, kM + lw + 8, g.ctxY + (kLineXS - 6) / 2, w - lw - 8 - pw - 8, a.ctxWindow ? ctxPct(a) : 0, kSegOff);
    buf[0] = '\0';
    const char* tool = a.tool == ToolKind::Claude ? "claude code" : a.tool == ToolKind::Codex ? "codex" : a.tool == ToolKind::Gemini ? "gemini" : "";
    const char* parts[4] = {tool, a.model, a.folder, a.branch};
    for (const char* p : parts) {
      if (!p[0]) continue;
      if (buf[0]) strncat(buf, DECK_DOT, sizeof buf - strlen(buf) - 1);
      strncat(buf, p, sizeof buf - strlen(buf) - 1);
    }
    c.text(FONT_XS, kM, g.whereY, buf, T.dim, w);
  }
  // The buttons.
  const BigButtons b = bigCardButtons(S, a, in.scope);
  const bool armed = (int32_t)(ui.stopArmedUntil - in.ms) > 0;
  for (int i = 0; i < b.n; i++) {
    const bool pressed = ui.pressed.kind == b.kind[i];
    switch (b.kind[i]) {
      case Hit::Focus: act(c, T, b.r[i], Hit::Focus, "focus", true, false, pressed, false); break;
      case Hit::Interrupt: act(c, T, b.r[i], Hit::Interrupt, "interrupt", false, false, pressed, false); break;
      case Hit::Resume: act(c, T, b.r[i], Hit::Resume, "resume", false, false, pressed, false); break;
      case Hit::Back: act(c, T, b.r[i], Hit::Back, "back", false, false, pressed, false); break;
      default: {
        const bool exited = a.status == Status::Exited;
        act(c, T, b.r[i], Hit::Stop, exited ? "remove" : armed ? "again" : "stop", false, true, pressed || (armed && !exited), false);
      }
    }
  }
}

static inline void prompt(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  using namespace big_ui;
  const BigPrompt p = bigPrompt(S, a);
  const PanelUi& ui = *in.ui;
  const uint16_t ink = T.bg, soft = blend565(T.accent, T.bg, 11);   // the dark text at 70 %
  const int w = S.w - 2 * kM;
  c.fill(T.accent);
  // The pet in its black box, its "?" steady.
  c.rect(p.pet.x, p.pet.y, p.pet.w, p.pet.h, T.bg);
  petZone(c, p.pet, a.pet.has ? kAskCell : 2, a, Pose::Wait, 0, true, p.pet.y + p.pet.h - 2 * kAskCell, T.accent, T.bg);
  const int tx = p.pet.x + p.pet.w + kGap;
  lineAt(c, FONT_B20, kLineB20, tx, p.nameY, a.title[0] ? a.title : a.id, ink, S.w - kM - tx);
  const int cw = chip(c, T, tx, p.chipY, a, in.nowEpoch, true);
  if (a.pet.has && a.pet.stage != DECK_PET_EGG) {
    char lv[12];
    snprintf(lv, sizeof lv, "LV %u", (unsigned)a.pet.level);
    int x = tx + cw + 8;
    for (int i = 0; i < DECK_PET_HEARTS; i++, x += 7 + 2) heart(c, x, p.chipY + (kChipH - 6) / 2, 1, i < a.pet.hearts, true, T.accent);
    c.text(FONT_XS, x + 6, p.chipY + (kChipH - FONT_XS.cellH) / 2, lv, ink);
  }
  // What it asks.
  int x = upperAt(c, FONT_XS, kM, p.askY, "it asks", ink) + kM;
  if (a.waitKind[0]) {
    char k[20];
    snprintf(k, sizeof k, DECK_DOT "%s", a.waitKind);
    upperAt(c, FONT_XS, x, p.askY, k, soft);
  }
  textWrapped(c, FONT_Q24B, kM, p.titleY + (kLineQ - FONT_Q24B.cellH) / 2, a.waitTitle[0] ? a.waitTitle : "needs you", ink, w,
              p.titleLines, kLineQ);
  // The command, and where it runs.
  if (p.term.h) {
    c.rect(p.term.x, p.term.y, p.term.w, p.term.h, T.bg);
    int y = p.term.y + 6;
    if (p.termLines) {
      const int dx = lineAt(c, FONT_L, kLineL, p.term.x + 8, y, "$ ", kOrange2);
      textWrapped(c, FONT_L, p.term.x + 8 + dx, y + (kLineL - FONT_L.cellH) / 2, a.waitDetail, T.ink, p.term.w - 16 - dx,
                  p.termLines, kLineL);
      y += p.termLines * kLineL;
    }
    if (a.promptCtx[0]) c.text(FONT_XS, p.term.x + 8, y + 2, a.promptCtx, T.dim, p.term.w - 16);
  }
  // Every answer: the chosen one filled; tap it again to send.
  for (int i = 0; i < p.n; i++) {
    const Rect& o = p.opt[i];
    const bool on = ui.selected == i, down = ui.pressed.kind == Hit::Option && ui.pressed.index == i;
    c.rect(o.x, o.y, o.w, o.h, T.bg);
    if (!on && !down) c.rect(o.x + 2, o.y + 2, o.w - 4, o.h - 4, T.accent);
    const uint16_t fg = on || down ? T.accent : ink, key = on || down ? blend565(T.bg, T.accent, 11) : soft;
    const int ty = o.y + (o.h - kLineL) / 2;
    const int kw = lineAt(c, FONT_L, kLineL, o.x + 10, ty, a.opts[i].key, key);
    lineAt(c, FONT_L, kLineL, o.x + 10 + kw + 8, ty, a.opts[i].label, fg, o.w - 20 - kw - 8);
  }
  if (!p.n) upperAt(c, FONT_XS, kM, p.terminal.y - kGap - kLineXS, "free text: type it at the terminal", soft, w);
  // Or at the terminal.
  upperAt(c, FONT_XS, p.terminal.x - 12 - textW(FONT_XS, "OR AT THE TERMINAL"), p.orY, "or at the terminal", soft);
  act(c, T, p.terminal, Hit::Focus, "focus", true, false, ui.pressed.kind == Hit::Focus, true);
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
