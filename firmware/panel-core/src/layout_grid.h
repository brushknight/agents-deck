#pragma once
// The deck as a grid of tiles, for the square-ish touch boards: the same
// tile as the Flutter desk panel and the web dashboard (critter with its pose
// and hat, title, a status line, the context gauge along the border, hungry
// stripes, the subagent corner critter, the tool chip), paged, with an
// agent's card on a tap.
//
// One layout, sized per screen: layoutLcd4() is the 480 x 480 4" board (a
// 4 x 4 grid, tiles at their deck slot, page bars like the desk panel),
// layoutAmoled18() the 368 x 448 AMOLED (a 2 x 2 grid of bigger tiles in slot
// order, a header and page dots). Pixel art scales by whole numbers; text
// uses the Roboto Mono sizes of font_data.h.
//
// Everything here is pure drawing and pure UI state, so it runs on the host
// (tests, sim/render) exactly as on the board. No Arduino code.
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "deck_ui.h"
#include "model.h"
#include "panel_layout.h"
#include "sprites.h"
#include "view.h"

// ---------------------------------------------------------------- layout

struct GridLayout {
  int w, h;
  int cols, rows;
  bool bySlot;                 // tiles at their deck slot (gaps stay empty); else in slot order
  bool trueBlack;
  int pad, gap;
  int cornerR;                 // the glass's rounded corners (0 = square); content keeps clear, the host renders the mask
  int headerH, footerH;        // above and below the grid (0 = none); the footer holds the page marks
  int barsH;                   // 4x4 only: page bars under the grid when there is more than one page
  const Font* title;           // tile title
  const Font* meta;            // tile status line
  int critterScale, subScale;
  const Font* hudFont;         // header and footer text
  // The card.
  int cardPad, cardCritter;
  const Font* cardTitle;
  const Font* cardStatus;
  const Font* cardText;
  const Font* cardSmall;
  int btnH, btnGap;
  // Notices.
  int noticeScale;
  const Font* noticeTitle;
  const Font* noticeText;
};

// 480 x 480, 4 x 4, like the desk panel.
static inline GridLayout layoutLcd4() {
  GridLayout L{};
  L.w = 480; L.h = 480; L.cols = 4; L.rows = 4; L.bySlot = true; L.trueBlack = false;
  L.pad = 8; L.gap = 8; L.cornerR = 0; L.headerH = 0; L.footerH = 0; L.barsH = 30;
  L.title = &FONT_S; L.meta = &FONT_XS; L.critterScale = 3; L.subScale = 1; L.hudFont = &FONT_S;
  L.cardPad = 16; L.cardCritter = 6;
  L.cardTitle = &FONT_XL; L.cardStatus = &FONT_L; L.cardText = &FONT_L; L.cardSmall = &FONT_S;
  L.btnH = 46; L.btnGap = 8;
  L.noticeScale = 8; L.noticeTitle = &FONT_XXL; L.noticeText = &FONT_L;
  return L;
}

// 368 x 448 AMOLED, 2 x 2 bigger tiles, a header and page dots.
static inline GridLayout layoutAmoled18() {
  GridLayout L{};
  L.w = 368; L.h = 448; L.cols = 2; L.rows = 2; L.bySlot = false; L.trueBlack = true;
  // The 1.8" AMOLED's glass has rounded corners. Waveshare publishes no
  // radius; about 3.5 mm at 0.08 mm per pixel gives 44 px. Header and footer
  // sit inside it, the tiles below and above it.
  L.pad = 8; L.gap = 8; L.cornerR = 44; L.headerH = 40; L.footerH = 34; L.barsH = 0;
  L.title = &FONT_L; L.meta = &FONT_S; L.critterScale = 5; L.subScale = 2; L.hudFont = &FONT_S;
  L.cardPad = 18; L.cardCritter = 5;
  L.cardTitle = &FONT_XL; L.cardStatus = &FONT_S; L.cardText = &FONT_L; L.cardSmall = &FONT_S;
  L.btnH = 44; L.btnGap = 8;
  L.noticeScale = 7; L.noticeTitle = &FONT_XL; L.noticeText = &FONT_S;
  return L;
}

struct Rect {
  int x, y, w, h;
  bool contains(int px, int py) const { return px >= x && px < x + w && py >= y && py < y + h; }
};

static inline int perPage(const GridLayout& L) { return L.cols * L.rows; }

static inline int pageCount(const GridLayout& L, const Fleet& f) {
  if (f.count == 0) return 1;
  if (L.bySlot) return f.agents[f.count - 1].slot / perPage(L) + 1;   // agents are ordered by slot
  return (f.count + perPage(L) - 1) / perPage(L);
}

// Where the cells go: the space between header and footer (and the page
// bars, when shown), square cells as large as fit, centred.
struct GridGeom {
  int x0, y0, cellW, cellH;
  Rect bars;                   // page bars or dots; w = 0 when there are none
};

static inline GridGeom gridGeom(const GridLayout& L, int pages) {
  GridGeom g{};
  const int barsH = L.barsH && pages > 1 ? L.barsH : 0;
  const int top = L.headerH + L.pad, bottom = L.h - L.footerH - L.pad - barsH;
  const int availW = L.w - 2 * L.pad, availH = bottom - top;
  g.cellW = (availW - L.gap * (L.cols - 1)) / L.cols;
  g.cellH = (availH - L.gap * (L.rows - 1)) / L.rows;
  if (L.bySlot && pages < 2 && g.cellH > g.cellW) g.cellH = g.cellW;   // square with one page, like the deck
  if (L.bySlot && g.cellW > g.cellH && pages < 2) g.cellW = g.cellH;
  const int gridW = g.cellW * L.cols + L.gap * (L.cols - 1), gridH = g.cellH * L.rows + L.gap * (L.rows - 1);
  g.x0 = (L.w - gridW) / 2;
  g.y0 = top + (availH - gridH) / 2;
  if (barsH) g.bars = Rect{0, bottom, L.w, barsH};
  else if (L.footerH) g.bars = Rect{0, L.h - L.footerH, L.w, L.footerH};
  else g.bars = Rect{0, 0, 0, 0};
  return g;
}

static inline Rect cellRect(const GridLayout& L, const GridGeom& g, int cell) {
  const int col = cell % L.cols, row = cell / L.cols;
  return Rect{g.x0 + col * (g.cellW + L.gap), g.y0 + row * (g.cellH + L.gap), g.cellW, g.cellH};
}

// The cell under a point, -1 for none (gaps count as none).
static inline int cellAt(const GridLayout& L, const GridGeom& g, int x, int y) {
  for (int i = 0; i < perPage(L); i++)
    if (cellRect(L, g, i).contains(x, y)) return i;
  return -1;
}

// The agent in a cell of a page: index into f.agents, or -1 for a free cell.
static inline int agentAt(const GridLayout& L, const Fleet& f, int page, int cell) {
  if (cell < 0 || cell >= perPage(L)) return -1;
  if (L.bySlot) {
    const int slot = page * perPage(L) + cell;
    for (int i = 0; i < f.count; i++)
      if (f.agents[i].slot == slot) return i;
    return -1;
  }
  const int i = page * perPage(L) + cell;
  return i < f.count ? i : -1;
}

static inline int pageOf(const GridLayout& L, const Fleet& f, int agent) {
  return L.bySlot ? f.agents[agent].slot / perPage(L) : agent / perPage(L);
}

// ---------------------------------------------------------------- UI state

// Keeps the UI valid for the fleet: a page that exists, a card whose agent is still there.
static inline void uiFix(PanelUi& ui, const GridLayout& L, const Fleet& f) {
  const int pages = pageCount(L, f);
  if (ui.page >= pages) ui.page = pages - 1;
  if (ui.page < 0) ui.page = 0;
  if (ui.openId[0] && findAgent(f, ui.openId) < 0) ui.openId[0] = '\0';
}

static inline void uiPage(PanelUi& ui, const GridLayout& L, const Fleet& f, int step) {
  const int pages = pageCount(L, f);
  ui.page = (ui.page + step + pages) % pages;
}

// The BOOT button: back from a card, otherwise the next page.
static inline void uiButton(PanelUi& ui, const GridLayout& L, const Fleet& f) {
  if (ui.openId[0]) ui.openId[0] = '\0';
  else uiPage(ui, L, f, 1);
}

// The card's parts, for drawing and for hit-testing.
struct CardGeom {
  Rect back;                   // "‹ back"
  Rect buttons[kMaxOptions];
  int buttonCount;             // 0 when the options are shown without buttons
  int optionsTop;              // where the buttons (or the options list) start
};

static inline CardGeom cardGeom(const GridLayout& L, const Agent& a, bool act) {
  CardGeom g{};
  g.back = Rect{0, 0, 140, L.cardPad + L.cardSmall->cellH + 14};
  const int n = a.status == Status::Waiting ? a.optCount : 0;
  const int colsN = n > 3 ? 2 : 1, rowsN = (n + colsN - 1) / colsN;
  const int bw = (L.w - 2 * L.cardPad - (colsN - 1) * L.btnGap) / colsN;
  g.optionsTop = L.h - L.cardPad - rowsN * L.btnH - (rowsN > 0 ? (rowsN - 1) * L.btnGap : 0);
  if (!act) {
    // The same rows, as plain text: shorter.
    g.optionsTop = L.h - L.cardPad - n * (L.cardSmall->cellH + 4) - (n ? 2 * L.cardSmall->cellH + 8 : 0);
    return g;
  }
  g.buttonCount = n;
  for (int i = 0; i < n; i++) {
    const int col = i % colsN, row = i / colsN;
    g.buttons[i] = Rect{L.cardPad + col * (bw + L.btnGap), g.optionsTop + row * (L.btnH + L.btnGap), bw, L.btnH};
  }
  return g;
}

// A tap at (x, y). `act`: the deck gave this board scope act or admin.
static inline UiAnswer uiTap(PanelUi& ui, const GridLayout& L, const Fleet& f, int x, int y, bool act, uint32_t ms) {
  UiAnswer out;
  uiFix(ui, L, f);
  if (ui.openId[0]) {
    const int i = findAgent(f, ui.openId);
    const Agent& a = f.agents[i];
    const CardGeom g = cardGeom(L, a, act);
    if (g.back.contains(x, y)) { ui.openId[0] = '\0'; return out; }
    for (int b = 0; b < g.buttonCount; b++) {
      if (!g.buttons[b].contains(x, y)) continue;
      out.send = true;
      snprintf(out.agent, sizeof out.agent, "%s", a.id);
      snprintf(out.prompt, sizeof out.prompt, "%s", a.promptId);
      snprintf(out.key, sizeof out.key, "%s", a.opts[b].key);
      uiToast(ui, "sending\xE2\x80\xA6", ms);
      return out;
    }
    return out;
  }
  const int pages = pageCount(L, f);
  const GridGeom g = gridGeom(L, pages);
  if (g.bars.w && g.bars.contains(x, y) && pages > 1) {   // a page bar or dot: go there
    ui.page = x * pages / L.w;
    return out;
  }
  const int i = agentAt(L, f, ui.page, cellAt(L, g, x, y));
  if (i >= 0) snprintf(ui.openId, sizeof ui.openId, "%s", f.agents[i].id);
  return out;
}

// Left or up: the next page. Right or down: the previous one, or back from a card.
static inline void uiSwipe(PanelUi& ui, const GridLayout& L, const Fleet& f, Swipe s) {
  uiFix(ui, L, f);
  if (ui.openId[0]) {
    if (s == Swipe::Right || s == Swipe::Down) ui.openId[0] = '\0';
    return;
  }
  const int pages = pageCount(L, f);
  if (s == Swipe::Left || s == Swipe::Up) { if (ui.page < pages - 1) ui.page++; }
  else if (ui.page > 0) ui.page--;
}

// ---------------------------------------------------------------- drawing

namespace grid_detail {

// Diagonal accent stripes sliding across a hungry tile (HungryStripes).
static inline void stripes(Canvas& c, const Rect& r, uint16_t fill, uint16_t accent, uint32_t ms, int period) {
  const uint16_t s = blend565(fill, accent, 3);
  const int shift = (int)((ms / 100) % 6) * period / 6;
  for (int yy = 0; yy < r.h; yy++)
    for (int xx = 0; xx < r.w; xx++) {
      const int u = ((xx + yy - shift) % period + period) % period;
      if (u < period / 2) {
        const int px = r.x + xx, py = r.y + yy;
        if (px >= 0 && px < c.w && py >= 0 && py < c.h) c.px[py * c.w + px] = s;
      }
    }
}

// The tile's border as a context gauge: `pct` of the perimeter, clockwise
// from the top centre, over a faint full-length track (ContextRingPainter).
static inline void ring(Canvas& c, const Rect& r, int t, int pct, uint16_t color, uint16_t track) {
  c.frame(r.x, r.y, r.w, r.h, track, t);
  if (pct <= 0) return;
  const int W = r.w - t, H = r.h - t;              // the path along the stroke's top-left corners
  const int total = 2 * (W + H);
  int left = total * (pct > 100 ? 100 : pct) / 100;
  // Segments: top centre → top right, down, bottom right → left, up, top left → centre.
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

static inline void tile(Canvas& c, const GridLayout& L, const Theme& T, const Agent& a, const Rect& r,
                        uint32_t ms, int64_t now) {
  const Look k = lookOf(T, a);
  const int side = r.w < r.h ? r.w : r.h;
  const int u = side * 1000 / 170;                 // the deck's 170 px design cell, in thousandths
  auto d = [&](int v) { const int p = v * u / 1000; return p < 1 ? 1 : p; };
  c.rect(r.x, r.y, r.w, r.h, k.fill);
  if (k.stripes) stripes(c, r, k.fill, T.accent, ms, d(24));

  // The tool chip, top right.
  const int chip = d(8);
  c.rect(r.x + r.w - d(10) - chip, r.y + d(10), chip, chip, k.chip);

  // Working subagents: a small critter top left, with a count.
  if (a.subagents) {
    const Pose sp = a.subTool[0] ? poseOfTool(a.subTool) : Pose::Think;
    drawSprite(c, r.x + d(9), r.y + d(8), L.subScale, Species::Claude, sp, k.body, k.fill, ms, false);
    if (a.subagents > 1) {
      char n[8];
      snprintf(n, sizeof n, "\xC3\x97%u", (unsigned)a.subagents);
      c.text(*L.meta, r.x + d(9) + 14 * L.subScale + 2, r.y + d(8) + 9 * L.subScale - L.meta->cellH + 2, n, k.meta);
    }
  }

  // The critter, the title and the status line, centred as a block.
  const int s = L.critterScale;
  const int spriteH = 13 * s, gapA = d(10), gapB = d(3);
  const int block = spriteH + gapA + L.title->cellH + gapB + L.meta->cellH;
  int y = r.y + (r.h - block) / 2 + s;             // a hat rises one unit above the box
  if (y < r.y + 2 * s) y = r.y + 2 * s;
  drawSprite(c, r.x + r.w / 2 - 7 * s, y, s, a.species, poseOf(a, now), k.body, k.fill, ms, true, a.hat, a.hatColor);
  y += spriteH + gapA;
  const int tw = r.w - 2 * d(10);
  textCentered(c, *L.title, r.x + r.w / 2, y, a.title[0] ? a.title : a.id, k.label, tw);
  y += L.title->cellH + gapB;
  char meta[80];
  metaLine(a, now, meta, sizeof meta);
  textCentered(c, *L.meta, r.x + r.w / 2, y, meta, k.meta, tw);

  ring(c, r, d(4) < 2 ? 2 : d(4), a.ctxWindow ? ctxPct(a) : 0, k.ring, k.ringTrack);
}

static inline void emptyCell(Canvas& c, const Theme& T, const Rect& r) {
  c.rect(r.x, r.y, r.w, r.h, T.emptyFill);
  c.frame(r.x, r.y, r.w, r.h, T.emptyOutline);
}

static inline void pill(Canvas& c, const Font& f, const Theme& T, int cx, int y, const char* s, uint16_t border) {
  const int w = textW(f, s) + 28, h = f.cellH + 12;
  const int x = cx - w / 2;
  c.rect(x, y, w, h, T.panel);
  c.frame(x, y, w, h, border, 2);
  c.text(f, x + 14, y + 6, s, border == T.accent ? T.accent : T.ink);
}

// Page bars (4x4) or dots (the AMOLED): the current page bright, another
// page orange when an agent there needs you.
static inline void pageMarks(Canvas& c, const GridLayout& L, const Theme& T, const Fleet& f, int page, int pages,
                             const Rect& area) {
  if (pages < 2 || !area.w) return;
  bool hot[64] = {};
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].status == Status::Waiting) {
      const int p = pageOf(L, f, i);
      if (p < 64) hot[p] = true;
    }
  const bool bars = L.barsH > 0;
  const int cur = bars ? 56 : 22, other = bars ? 30 : 8, h = 8, gap = 12;
  int total = 0;
  for (int p = 0; p < pages; p++) total += (p == page ? cur : other) + (p ? gap : 0);
  int x = (L.w - total) / 2;
  const int y = area.y + (area.h - h) / 2;
  for (int p = 0; p < pages; p++) {
    const int w = p == page ? cur : other;
    const uint16_t col = p == page ? T.ink : (p < 64 && hot[p]) ? T.accent : T.ringTrack;
    c.rect(x + 1, y, w - 2, h, col);                // rounded ends: one pixel off the corners
    c.rect(x, y + 1, w, h - 2, col);
    x += w + gap;
  }
}

static inline void notice(Canvas& c, const GridLayout& L, const Theme& T, Pose pose, uint16_t body,
                          const char* title, const char* line1, const char* code, const char* line2, uint32_t ms) {
  const int s = L.noticeScale;
  int y = L.h / 2 - (13 * s + 24 + L.noticeTitle->cellH + 3 * (L.noticeText->cellH + 8)) / 2;
  drawSprite(c, L.w / 2 - 7 * s, y + s, s, Species::Claude, pose, body, T.bg, ms, true);
  y += 13 * s + 24;
  textCentered(c, *L.noticeTitle, L.w / 2, y, title, T.ink, L.w - 24);
  y += L.noticeTitle->cellH + 12;
  if (line1) { textCentered(c, *L.noticeText, L.w / 2, y, line1, T.dim, L.w - 24); y += L.noticeText->cellH + 8; }
  if (code) { textCentered(c, *L.noticeText, L.w / 2, y, code, T.accent, L.w - 24); y += L.noticeText->cellH + 8; }
  if (line2) textCentered(c, *L.noticeText, L.w / 2, y, line2, T.accent, L.w - 24);
}

static inline void header(Canvas& c, const GridLayout& L, const Theme& T, const Fleet& f, const char* server) {
  if (!L.headerH) return;
  const Counts n = countFleet(f);
  char buf[64];
  const Font& F = *L.hudFont;
  const int y = (L.headerH - F.cellH) / 2 + 2;
  const int inset = L.pad + 4 + L.cornerR / 3;
  int x = inset;
  if (n.waiting) {
    snprintf(buf, sizeof buf, "%u waiting", (unsigned)n.waiting);
    x += c.text(F, x, y, buf, T.accent) + F.cellW * 2;
  }
  snprintf(buf, sizeof buf, "%u running", (unsigned)n.running);
  c.text(F, x, y, buf, n.running ? T.ink : T.dim);
  if (server && server[0]) {
    const int w = textW(F, server) > L.w / 2 ? L.w / 2 : textW(F, server);
    c.text(F, L.w - inset - w, y, server, T.dim, L.w / 2);
  }
}

static inline void grid(Canvas& c, const GridLayout& L, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const int pages = pageCount(L, f);
  const int page = in.ui ? (in.ui->page < pages ? in.ui->page : pages - 1) : 0;
  const GridGeom g = gridGeom(L, pages);
  header(c, L, T, f, f.server);
  for (int cell = 0; cell < perPage(L); cell++) {
    const Rect r = cellRect(L, g, cell);
    const int i = agentAt(L, f, page, cell);
    if (i >= 0) tile(c, L, T, f.agents[i], r, in.ms, in.nowEpoch);
    else emptyCell(c, T, r);
  }
  pageMarks(c, L, T, f, page, pages, g.bars);
  if (L.footerH && in.link == LinkState::Lost)
    c.text(*L.hudFont, L.pad + 4 + L.cornerR / 2, L.h - L.footerH + (L.footerH - L.hudFont->cellH) / 2, "deck offline", T.accent);
  else if (in.link == LinkState::Lost)
    pill(c, *L.hudFont, T, L.w / 2, L.h - L.pad - L.hudFont->cellH - 20, "deck offline" DECK_DOT "retrying", T.accent);
}

static inline void button(Canvas& c, const Font& f, const Rect& r, const char* key, const char* label, uint16_t fill,
                          uint16_t ink, uint16_t outline) {
  c.rect(r.x + 1, r.y, r.w - 2, r.h, fill);
  c.rect(r.x, r.y + 1, r.w, r.h - 2, fill);
  c.frame(r.x, r.y, r.w, r.h, outline, 2);
  char buf[64];
  snprintf(buf, sizeof buf, "%s  %s", key, label);
  c.text(f, r.x + 14, r.y + (r.h - f.cellH) / 2, buf, ink, r.w - 24);
}

// An agent's card: the 1.47" hero, laid out for a big screen, with the
// prompt's answers as buttons when the board may act.
static inline void card(Canvas& c, const GridLayout& L, const Theme& T, const PanelInput& in, const Agent& a, int index) {
  const bool waiting = a.status == Status::Waiting;
  const uint16_t bgc = waiting ? T.accent : T.bg;
  const uint16_t fg = waiting ? T.bg : T.ink;
  const uint16_t soft = waiting ? T.bg : T.dim;
  c.fill(bgc);
  if (a.status == Status::Error) c.frame(0, 0, L.w, L.h, T.accent, 3);
  const int P = L.cardPad;
  char buf[224];

  // Back, and the position in the fleet.
  const Font& sm = *L.cardSmall;
  c.text(sm, P, P, "\xE2\x80\xB9 back", fg);
  snprintf(buf, sizeof buf, "%d/%u", index + 1, (unsigned)in.fleet->total);
  c.text(sm, L.w - P - textW(sm, buf), P, buf, soft);

  // The critter, then name, status and model to its right.
  const int s = L.cardCritter;
  const int top = P + sm.cellH + 14;
  const Look k = lookOf(T, a);
  const uint16_t body = waiting ? T.bg : (a.status == Status::Running || hungry(a)) ? T.accent
                                         : a.status == Status::Error ? T.ink : k.body == T.dim ? T.dim : T.ink;
  drawSprite(c, P + 3 * s / 2, top + s, s, a.species, poseOf(a, in.nowEpoch), body, bgc, in.ms, true, a.hat, a.hatColor);
  const int tx = P + 3 * s / 2 + 18 * s + 10, tw = L.w - tx - P;
  int y = top + 2;
  c.text(*L.cardTitle, tx, y, a.title[0] ? a.title : a.id, fg, tw);
  y += L.cardTitle->cellH + 4;
  statusLine(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Running || a.status == Status::Error || hungry(a);
  c.text(*L.cardStatus, tx, y, buf, waiting ? T.bg : loud ? T.accent : T.dim, tw);
  y += L.cardStatus->cellH + 4;
  if (a.subagents) snprintf(buf, sizeof buf, "%s" DECK_DOT "%u sub", a.model[0] ? a.model : "-", a.subagents);
  else snprintf(buf, sizeof buf, "%s", a.model);
  c.text(sm, tx, y, buf, soft, tw);

  y = top + 13 * s + 18;
  const int lw = L.w - 2 * P;
  const Font& tx1 = *L.cardText;
  const bool act = in.act;
  const CardGeom g = cardGeom(L, a, act);

  if (waiting) {
    // What it asks, then the answers.
    const char* q = a.waitTitle[0] ? a.waitTitle : "needs you";
    y += textWrapped(c, tx1, P, y, q, fg, lw, 2) * tx1.cellH + 6;
    const int room = (g.optionsTop - 10 - y) / (sm.cellH + 2);
    if (a.waitDetail[0] && room > 0) textWrapped(c, sm, P, y, a.waitDetail, fg, lw, room > 4 ? 4 : room, sm.cellH + 2);
    if (a.optCount == 0) {
      c.text(sm, P, L.h - P - sm.cellH, "answer in the terminal", fg, lw);
    } else if (act) {
      for (int b = 0; b < g.buttonCount; b++) {
        const Option& o = a.opts[b];
        const Font& bf = g.buttons[b].w > 30 * L.cardText->cellW ? *L.cardText : *L.cardSmall;
        if (o.primary) button(c, bf, g.buttons[b], o.key, o.label, T.bg, T.ink, T.bg);
        else button(c, bf, g.buttons[b], o.key, o.label, T.accent, T.bg, T.bg);
      }
    } else {
      int oy = g.optionsTop;
      for (int b = 0; b < a.optCount; b++) {
        snprintf(buf, sizeof buf, "%s %s  %s", a.opts[b].primary ? "\xE2\x80\xBA" : " ", a.opts[b].key, a.opts[b].label);
        c.text(sm, P, oy, buf, T.bg, lw);
        oy += sm.cellH + 4;
      }
      snprintf(buf, sizeof buf, "view only: agentctl devices scope %s act", in.device && in.device[0] ? in.device : "<name>");
      textWrapped(c, sm, P, oy + 4, buf, T.bg, lw, 2);
    }
  } else {
    // What it is doing, what the session is about.
    activityLine(a, buf, sizeof buf);
    if (a.status == Status::Running && a.actTool[0] && a.actDetail[0]) {
      const int used = c.text(tx1, P, y, a.actTool, fg, lw) + tx1.cellW;
      const bool path = strchr(a.actDetail, '/') && !strchr(a.actDetail, ' ');
      c.text(tx1, P + used, y, a.actDetail, fg, lw - used, path);
    } else {
      c.text(tx1, P, y, buf[0] ? buf : a.aiTitle, fg, lw);
    }
    y += tx1.cellH + 6;
    if (a.aiTitle[0]) y += textWrapped(c, sm, P, y, a.aiTitle, soft, lw, 2) * sm.cellH;
    y += 14;

    if (a.ctxWindow) {
      const int pct = ctxPct(a), barW = lw - 6 * sm.cellW;
      c.rect(P, y + sm.cellH / 2 - 4, barW, 8, T.line);
      c.rect(P, y + sm.cellH / 2 - 4, barW * pct / 100, 8, pct >= 85 ? T.accent : T.ink);
      snprintf(buf, sizeof buf, "%d%%", pct);
      c.text(sm, P + barW + 10, y, buf, fg);
      y += sm.cellH + 10;
    }
    if (a.turns || a.costUsd > 0) {
      const char* unit = a.turns == 1 ? "turn" : "turns";
      if (!a.turns) snprintf(buf, sizeof buf, "$%.2f", (double)a.costUsd);
      else if (a.costUsd <= 0) snprintf(buf, sizeof buf, "%u %s", a.turns, unit);
      else snprintf(buf, sizeof buf, "%u %s" DECK_DOT "$%.2f", a.turns, unit, (double)a.costUsd);
      c.text(sm, P, y, buf, soft, lw);
      y += sm.cellH + 6;
    }
    if (a.folder[0] || a.branch[0]) {
      if (a.folder[0] && a.branch[0]) snprintf(buf, sizeof buf, "%s" DECK_DOT "%s", a.folder, a.branch);
      else snprintf(buf, sizeof buf, "%s", a.folder[0] ? a.folder : a.branch);
      c.text(sm, P, L.h - P - sm.cellH, buf, soft, lw);
    }
  }
}

// The rounded glass: everything outside the corner arcs is black (what the
// panel shows anyway; the host renders it so pictures look like the board).
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

}  // namespace grid_detail

// The whole frame for a grid board.
static inline void drawPanel(Canvas& c, const GridLayout& L, const Theme& T, const PanelInput& in) {
  using namespace grid_detail;
  c.fill(T.bg);
  switch (in.link) {
    case LinkState::Connecting:
      notice(c, L, T, Pose::Run, T.ink, "connecting", "waiting for the deck on USB", nullptr,
             "agentctl link add <port>", in.ms);
      return;
    case LinkState::Unapproved:
      notice(c, L, T, Pose::Wait, T.accent, "waiting for approval", "on the Mac, run",
             "agentctl devices approve", in.device && in.device[0] ? in.device : "<name>", in.ms);
      return;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0) {
    if (in.link == LinkState::Lost) notice(c, L, T, Pose::Exit, T.dim, "deck offline", "waiting for agentctl", nullptr, nullptr, in.ms);
    else notice(c, L, T, Pose::Idle, T.dim, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr,
                nullptr, "agentctl new", in.ms);
    return;
  }
  const int open = in.ui && in.ui->openId[0] ? findAgent(*in.fleet, in.ui->openId) : -1;
  if (open >= 0) card(c, L, T, in, in.fleet->agents[open], open);
  else grid(c, L, T, in);
  if (in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0)
    pill(c, *L.hudFont, T, L.w / 2, L.h / 2 - L.hudFont->cellH, in.ui->toast, T.ink);
  if (L.cornerR) cornerMask(c, L.cornerR);
}

// The grid as a PanelLayout, for grid_app.h.
class GridPanel : public PanelLayout {
 public:
  explicit GridPanel(const GridLayout& l) : L(l), T(deckTheme(l.trueBlack)) {}
  int width() const override { return L.w; }
  int height() const override { return L.h; }
  bool trueBlack() const override { return L.trueBlack; }
  void draw(Canvas& c, const PanelInput& in) const override { drawPanel(c, L, T, in); }
  UiAnswer tap(PanelUi& ui, const Fleet& f, int x, int y, bool act, uint32_t ms) const override {
    return uiTap(ui, L, f, x, y, act, ms);
  }
  void swipe(PanelUi& ui, const Fleet& f, Swipe s) const override { uiSwipe(ui, L, f, s); }
  void button(PanelUi& ui, const Fleet& f) const override { uiButton(ui, L, f); }
  void fix(PanelUi& ui, const Fleet& f) const override { uiFix(ui, L, f); }
  const GridLayout L;
  const Theme T;
};
