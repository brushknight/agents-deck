#pragma once
// The square boards' layout: one layout system with per-board numbers
// (SquareSpec), after the owner's approved designs ("small square boards",
// boards 22-26 of the design canvas):
//
//   home    the 4": the deck's 4 x 4 grid by slot, paged, as built
//           the AMOLED: 2 x 2 tiles in one vertical list that scrolls with
//           momentum and settles on a row, a position bar while it moves
//   card    header (critter, name, status pill), doing now, context with the
//           dotted bar, tokens / cost / turns, the last prompt, round buttons:
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
//
// Back from a card or a prompt: swipe right, or BOOT. A tile's icons (tool
// chip, subagent critter) keep to a band above the critter, so they never
// meet the name.
//
// Pure C++ (no Arduino): the tests and sim/render draw exactly what the
// boards draw.
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "deck_cmd.h"
#include "deck_ui.h"
#include "font_data.h"
#include "font_round.h"
#include "font_square.h"
#include "model.h"
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
};

// The 1.8" AMOLED, 368 x 448, rounded glass.
static inline SquareSpec specAmoled18() {
  SquareSpec s{};
  s.name = "amoled18";
  s.w = 368; s.h = 448; s.cornerR = 44; s.m = 16; s.trueBlack = true;
  s.label = &FONT_XL; s.body = &FONT_Q22; s.question = &FONT_Q24B; s.value = &FONT_Q28B; s.title = &FONT_Q28B;
  s.home = HomeKind::List; s.cols = 2; s.rows = 0; s.pad = 8; s.gap = 8; s.barsH = 0;
  s.tileW = 172; s.tileH = 192; s.headerH = 50;
  s.tileTitle = &FONT_XL; s.tileMeta = &FONT_XL; s.hud = &FONT_XL;
  s.tileScale = 4; s.subScale = 2;
  s.cardScale = 4; s.promptScale = 2; s.noticeScale = 5;
  s.ctxPitch = 8; s.ctxTickW = 2; s.ctxTickH = 12; s.ctxFillH = 10;
  s.btnR = 36; s.btnGap = 26; s.btnFromBottom = 54;
  s.optH = 76; s.optGap = 8; s.optR = 12;
  s.posX = 4; s.posW = 6; s.posTop = 90; s.posBottom = 48;
  return s;
}

// The 4" LCD, 480 x 480: the grid as built.
static inline SquareSpec specLcd4() {
  SquareSpec s{};
  s.name = "lcd4";
  s.w = 480; s.h = 480; s.cornerR = 0; s.m = 24; s.trueBlack = false;
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

static inline PanelHit squareHit(const SquareSpec& S, const PanelUi& ui, const Fleet& f, int x, int y, DeckScope scope) {
  PanelHit h;
  if (ui.screen == Screen::Home) {
    if (S.home == HomeKind::Grid) {
      const int pages = pageCount(S, f);
      const GridGeom g = gridGeom(S, pages, ui.wifi);
      if (pages > 1 && g.bars.contains(x, y)) { h.kind = Hit::PageBar; h.index = x * pages / S.w; return h; }
      const int i = agentAt(S, f, ui.page < pages ? ui.page : 0, cellAt(S, g, x, y));
      if (i >= 0) { h.kind = Hit::Tile; h.agent = i; }
      return h;
    }
    if (y < S.headerH) return h;
    for (int i = 0; i < f.count; i++)
      if (listTile(S, i, ui.scroll).contains(x, y)) { h.kind = Hit::Tile; h.agent = i; return h; }
    return h;
  }
  const int i = findAgent(f, ui.openId);
  if (i < 0) return h;
  const Agent& a = f.agents[i];
  if (ui.screen == Screen::Card) {
    const CardButtons b = cardButtons(S, a, scope);
    for (int k = 0; k < b.n; k++) {
      const int dx = x - b.cx[k], dy = y - b.cy;
      if (dx * dx + dy * dy <= (S.btnR + 4) * (S.btnR + 4)) {
        h.kind = b.kind[k] == IconKind::Focus ? Hit::Focus : b.kind[k] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
        h.agent = i;
        return h;
      }
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
}

static inline DeckCommand squareTap(PanelUi& ui, const SquareSpec& S, const Fleet& f, const PanelHit& h, DeckScope scope,
                                    uint32_t ms) {
  DeckCommand out;
  switch (h.kind) {
    case Hit::Tile: squareOpen(ui, f, h.agent); break;
    case Hit::PageBar: ui.page = h.index; break;
    case Hit::Focus:
      out = commandFor(DeckCmd::Focus, f.agents[h.agent]);
      uiToast(ui, "focus", ms, 1200);
      break;
    case Hit::Interrupt:
      out = commandFor(DeckCmd::Interrupt, f.agents[h.agent]);
      uiToast(ui, "interrupted", ms, 1200);
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
  if (ui.screen != Screen::Home) {
    if (s == Swipe::Right) squareHome(ui);
    return;
  }
  if (S.home == HomeKind::Grid) {
    const int pages = pageCount(S, f);
    if ((s == Swipe::Left || s == Swipe::Up) && ui.page < pages - 1) ui.page++;
    else if ((s == Swipe::Right || s == Swipe::Down) && ui.page > 0) ui.page--;
  }
}

static inline DeckCommand squareTouch(PanelUi& ui, const SquareSpec& S, const Fleet& f, bool touching, int x, int y,
                                      DeckScope scope, uint32_t ms) {
  DeckCommand out;
  const bool list = S.home == HomeKind::List && ui.screen == Screen::Home;
  if (touching) {
    if (!ui.down) {
      ui.down = true;
      ui.dragging = false;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ui.lastMs = ms;
      ui.lastY = y;
      ui.velocity = 0;
      ui.holdFired = false;
      ui.caught = list && ui.animating;
      if (ui.caught) {                             // catch the moving list
        ui.animating = false;
        ui.settledMs = ms ? ms : 1;
        ui.pressed = PanelHit();
      } else {
        ui.pressed = squareHit(S, ui, f, x, y, scope);
      }
      ui.scroll0 = ui.scroll;
      if (ui.pressed.kind == Hit::Stop && scope != DeckScope::Admin) ui.pressed = PanelHit();
      return out;
    }
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > kSlop * kSlop) ui.pressed = PanelHit();   // it became a drag
    if (list && !ui.dragging && (dy > kSlop || dy < -kSlop) && (dy < 0 ? -dy : dy) >= (dx < 0 ? -dx : dx))
      ui.dragging = true;
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
  if (ui.dragging) {
    ui.dragging = false;
    if (ms - ui.lastMs > 80) ui.velocity = 0;       // it stopped before letting go
    ui.scrollFrom = ui.scroll;
    ui.scrollTo = settleTarget(S, f, ui.scroll, ui.velocity);
    ui.animStart = ms;
    ui.animating = true;
    return out;
  }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (ui.caught) {                                 // it only stopped the list: rest on a row
    ui.scrollFrom = ui.scroll;
    ui.scrollTo = settleTarget(S, f, ui.scroll, 0);
    ui.animStart = ms;
    ui.animating = true;
  } else if (adx <= kSlop && ady <= kSlop) {
    if (pressed.kind != Hit::None || (uint32_t)(ms - ui.downMs) <= kTapMaxMs)
      out = squareTap(ui, S, f, pressed.kind != Hit::None ? pressed : squareHit(S, ui, f, ui.x0, ui.y0, scope), scope, ms);
  } else if (adx >= kSwipeMin || ady >= kSwipeMin) {
    squareSwipe(ui, S, f, adx >= ady ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down));
  }
  return out;
}

// Animations and timeouts. True while something moves.
static inline bool squareTick(PanelUi& ui, const SquareSpec& S, const Fleet& f, uint32_t ms) {
  squareFix(ui, S, f);
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) squareHome(ui);
  if (ui.animating) {
    const uint32_t t = ms - ui.animStart;
    if (t >= kSettleMs) {
      ui.scroll = ui.scrollTo;
      ui.animating = false;
      ui.settledMs = ms ? ms : 1;
    } else {
      const float p = (float)t / kSettleMs, e = 1 - (1 - p) * (1 - p) * (1 - p);   // ease out
      ui.scroll = ui.scrollFrom + (ui.scrollTo - ui.scrollFrom) * e;
    }
  }
  return ui.dragging || ui.animating || posBarAlpha(ui, ms) > 0 || (ui.down && ui.pressed.kind == Hit::Stop);
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

// BOOT: back from a card or a prompt; on a grid, the next page.
static inline void squareButton(PanelUi& ui, const SquareSpec& S, const Fleet& f) {
  if (ui.screen != Screen::Home) { squareHome(ui); return; }
  if (S.home == HomeKind::Grid) {
    const int pages = pageCount(S, f);
    ui.page = (ui.page + 1) % pages;
  }
}

static inline int holdPermille(const PanelUi& ui, uint32_t ms) {
  if (!ui.down || ui.pressed.kind != Hit::Stop) return 0;
  const uint32_t t = ms - ui.downMs;
  return t >= kStopHoldMs ? 1000 : (int)(t * 1000 / kStopHoldMs);
}

// ---------------------------------------------------------------- drawing

namespace square_detail {

// A rounded rectangle, filled or as a `t`-pixel outline.
static inline void roundRect(Canvas& c, int x, int y, int w, int h, int r, uint16_t col) {
  if (r * 2 > h) r = h / 2;
  if (r * 2 > w) r = w / 2;
  c.rect(x + r, y, w - 2 * r, h, col);
  c.rect(x, y + r, r, h - 2 * r, col);
  c.rect(x + w - r, y + r, r, h - 2 * r, col);
  fillArc(c, x + r, y + r, 0, r, 270, 90, col);
  fillArc(c, x + w - r, y + r, 0, r, 0, 90, col);
  fillArc(c, x + w - r, y + h - r, 0, r, 90, 90, col);
  fillArc(c, x + r, y + h - r, 0, r, 180, 90, col);
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
  for (int yy = 0; yy < r.h; yy++) {
    const int py = r.y + yy;
    if (py < clipTop || py < 0 || py >= c.h) continue;
    for (int xx = 0; xx < r.w; xx++) {
      const int u = ((xx + yy - shift) % period + period) % period;
      const int px = r.x + xx;
      if (u < period / 2 && px >= 0 && px < c.w && c.px[py * c.w + px] == fill) c.px[py * c.w + px] = s;
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
  if (list) p.bar = Rect{r.x + 10, r.y + r.h - 14, r.w - 20, 4};
  return p;
}

static inline void tile(Canvas& c, const SquareSpec& S, const Theme& T, const Agent& a, const Rect& r, uint32_t ms,
                        int64_t now, bool list, bool pressed, int clipTop) {
  Look k = lookOf(T, a);
  if (pressed) { k.fill = T.ink; k.body = k.label = k.meta = k.chip = T.bg; k.stripes = false; }
  const TileParts p = tileParts(S, r, list);
  if (list) square_detail::roundRect(c, r.x, r.y, r.w, r.h, 8, k.fill);
  else c.rect(r.x, r.y, r.w, r.h, k.fill);
  if (k.stripes) stripes(c, r, k.fill, T.accent, ms, list ? 32 : 24, clipTop);
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
  drawSprite(c, p.critter.x, p.critter.y, p.scale, a.species, poseOf(a, now), k.body, k.fill, ms, true, a.hat, a.hatColor);
  textCentered(c, *S.tileTitle, r.x + r.w / 2, p.name.y, a.title[0] ? a.title : a.id, k.label, p.name.w);
  char meta[80];
  metaLine(a, now, meta, sizeof meta);
  textCentered(c, *S.tileMeta, r.x + r.w / 2, p.meta.y, meta, k.meta, p.meta.w);
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
// The Wi-Fi mark (round_geom.h's), centred on cx, its base at `base`.
static inline void wifiMark(Canvas& c, const Theme& T, int cx, int base) { drawNetMark(c, (float)cx, (float)base, T.dim); }

static inline void listHeader(Canvas& c, const SquareSpec& S, const Theme& T, const Fleet& f, LinkState link,
                              bool wifi) {
  c.rect(0, 0, S.w, S.headerH, T.bg);
  const Font& hf = *S.question;
  const int x0 = S.m + S.cornerR / 4;
  const int ty = (S.headerH - hf.cellH) / 2 + 2;
  c.text(hf, x0, ty, "agents", T.ink);
  // On Wi-Fi the mark follows the title, its base about on the title's baseline.
  if (wifi) wifiMark(c, T, x0 + textW(hf, "agents") + 26, ty + hf.cellH * 3 / 4);
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

static inline void listHome(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < f.count; i++) {
    const Rect r = listTile(S, i, ui.scroll);
    if (r.y + r.h <= S.headerH || r.y >= S.h) continue;
    tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, true, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i,
         S.headerH);
  }
  listHeader(c, S, T, f, in.link, ui.wifi);
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
    roundRect(c, S.posX, y, S.posW, len, S.posW / 2, col);
  }
}

static inline void pageBars(Canvas& c, const SquareSpec& S, const Theme& T, const Fleet& f, int page, int pages,
                            const Rect& area) {
  if (pages < 2 || !area.w) return;
  bool hot[64] = {};
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].status == Status::Waiting && f.agents[i].slot / perPage(S) < 64) hot[f.agents[i].slot / perPage(S)] = true;
  const int cur = 56, other = 30, h = 8, gap = 12;
  int total = 0;
  for (int p = 0; p < pages; p++) total += (p == page ? cur : other) + (p ? gap : 0);
  int x = (S.w - total) / 2;
  const int y = area.y + (area.h - h) / 2;
  for (int p = 0; p < pages; p++) {
    const int w = p == page ? cur : other;
    fillPill(c, x, y, w, h, p == page ? T.ink : (p < 64 && hot[p]) ? T.accent : T.ringTrack);
    x += w + gap;
  }
}

static inline void gridHome(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  const int pages = pageCount(S, f);
  const int page = ui.page < pages ? ui.page : pages - 1;
  const GridGeom g = gridGeom(S, pages, ui.wifi);
  for (int cell = 0; cell < perPage(S); cell++) {
    const Rect r = cellRect(S, g, cell);
    const int i = agentAt(S, f, page, cell);
    if (i >= 0) tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, false, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i, 0);
    else emptyCell(c, T, r);
  }
  pageBars(c, S, T, f, page, pages, g.bars);
  if (ui.wifi && g.bars.w) wifiMark(c, T, g.x0 + S.cols * g.cellW + (S.cols - 1) * S.gap - 18, g.bars.y + g.bars.h / 2 + 6);
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

static inline void card(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  const int m = S.m, s = S.cardScale;
  char buf[224];
  c.fill(T.bg);
  // Header: the critter at (m, 20), the name with its baseline at 20 + 8s, the pill under it.
  const Look k = lookOf(T, a);
  const uint16_t body = a.status == Status::Running || hungry(a) ? T.accent : k.body == T.bg ? T.ink : k.body;
  drawSprite(c, m, 20, s, a.species, poseOf(a, in.nowEpoch), body, T.bg, in.ms, true, a.hat, a.hatColor);
  const Font& nf = *S.title;
  const int tx = m + 18 * s + 14, tw = S.w - tx - m;
  const int nameTop = 20 + 8 * s - nf.cellH * 3 / 4;
  c.text(nf, tx, nameTop, a.title[0] ? a.title : a.id, T.ink, tw);
  pillLabel(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Error || hungry(a);
  pillText(c, *S.label, tx, nameTop + nf.cellH, buf, loud ? T.accent : T.ink, T.bg, tw);
  int y = nameTop + nf.cellH + S.label->cellH + 14;
  if (y < 20 + 13 * s + 8) y = 20 + 13 * s + 8;

  const Font& lf = *S.label;
  const Font& bf = *S.body;
  const int lw = S.w - 2 * m;
  // Doing now.
  c.text(lf, m, y, "doing now", T.dim, lw);
  y += lf.cellH - 4;
  doingNow(a, buf, sizeof buf);
  c.text(bf, m, y, buf, T.ink, lw);
  y += bf.cellH + 2;

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

  // Tokens, cost, turns.
  {
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
  const int ruleY = b.n ? b.cy - S.btnR - 10 : S.h - m;
  c.text(lf, m, y, "last prompt", T.dim, lw);
  y += lf.cellH - 4;
  const int line = bf.cellH - 2;
  const int lines = (ruleY - 4 - y) / line;
  if (lines > 0) textWrapped(c, bf, m, y, a.lastPrompt[0] ? a.lastPrompt : "\xE2\x80\x94", T.ink, lw, lines, line);

  // The buttons.
  if (b.n) {
    c.rect(m, ruleY, lw, 1, T.line);
    const PanelUi& ui = *in.ui;
    for (int i = 0; i < b.n; i++) {
      const Hit hk = b.kind[i] == IconKind::Focus ? Hit::Focus : b.kind[i] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
      const bool pressed = ui.pressed.kind == hk;
      iconButton(c, b.cx[i], b.cy, S.btnR, b.kind[i], pressed, hk == Hit::Stop ? holdPermille(ui, in.ms) : 0, T.bg, T.ink,
                 T.accent, T.line);
    }
  }
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
  drawSprite(c, m + (S.cornerR ? 6 : 0), hy, ps, a.species, Pose::Wait, perm ? T.bg : T.dim, bg, in.ms, false, a.hat,
             a.hatColor);
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
  c.fill(T.bg);
  char buf[96];
  const char* name = in.device && in.device[0] ? in.device : "<name>";
  const PanelUi* tu = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0 ? in.ui : nullptr;
  switch (in.link) {
    case LinkState::Connecting:
      if (in.net && in.net->via == NetVia::Wifi) {   // where Wi-Fi is (net_model.h's words)
        const NetNotice n = netNotice(*in.net);
        notice(c, S, T, n.problem ? Pose::Exit : Pose::Think, T.dim, n.title, n.line1, n.problem ? T.accent : T.dim,
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
  if (open >= 0 && ui.screen == Screen::Card) card(c, S, T, in, in.fleet->agents[open]);
  else if (open >= 0) prompt(c, S, T, in, in.fleet->agents[open]);
  else if (S.home == HomeKind::List) listHome(c, S, T, in);
  else gridHome(c, S, T, in);
  if (tu) toastPill(c, S, T, ui.toast);
  if (S.cornerR) cornerMask(c, S.cornerR);
}

// The square boards' PanelLayout.
class SquarePanel : public PanelLayout {
 public:
  explicit SquarePanel(const SquareSpec& s) : S(s), T(deckTheme(s.trueBlack)) {}
  int width() const override { return S.w; }
  int height() const override { return S.h; }
  bool trueBlack() const override { return S.trueBlack; }
  void draw(Canvas& c, const PanelInput& in) const override { drawSquare(c, S, T, in); }
  DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope, uint32_t ms) const override {
    return squareTouch(ui, S, f, down, x, y, scope, ms);
  }
  bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const override { return squareTick(ui, S, f, ms); }
  void button(PanelUi& ui, const Fleet& f) const override { squareButton(ui, S, f); }
  void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const override { squareResult(ui, seq, code, ms); }
  const SquareSpec S;
  const Theme T;
};
