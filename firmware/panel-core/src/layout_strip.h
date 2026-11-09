#pragma once
// The 640 x 172 strip's layout (Waveshare ESP32-S3-Touch-LCD-3.49): one row of tiles, the card and prompt
// side by side. Tiles, toasts, taps and results are the square boards' (layout_square.h).
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "layout_square.h"

namespace strip {

constexpr int W = 640, H = 172, M = 8;
constexpr int kHeaderH = 28;
constexpr int kTileW = 121, kTileH = 134, kGap = 6, kPitch = kTileW + kGap;
constexpr int kVisible = 5;                                   // tiles on screen
constexpr int kTilesX = (W - kVisible * kTileW - (kVisible - 1) * kGap) / 2;
constexpr int kBarY = H - 5, kBarH = 3;                       // the position bar

// The card.
constexpr int kCardScale = 3, kBtnR = 26, kBtnGap = 14, kBtnCy = 36;
constexpr int kBtnRing = 10;                                  // the stop hold's ring outside the button
// The prompt: the question's column, then the answers'.
constexpr int kQuestionW = 296, kOptX = M + kQuestionW + 12, kOptW = W - M - kOptX;
constexpr int kOptMaxH = 50, kOptGap = 6, kOptR = 10;

// The numbers the square helpers (tile, toast, pills) read.
static inline SquareSpec squareSpec() {
  SquareSpec s{};
  s.name = "strip";
  s.w = W; s.h = H; s.cornerR = 0; s.m = M; s.trueBlack = false;
  s.label = &FONT_S; s.body = &FONT_L; s.question = &FONT_L; s.value = &FONT_XL; s.title = &FONT_XL;
  s.home = HomeKind::List; s.cols = kVisible; s.rows = 0; s.pad = 0; s.gap = kGap; s.barsH = 0;
  s.tileW = kTileW; s.tileH = kTileH; s.headerH = kHeaderH;
  s.tileTitle = &FONT_L; s.tileMeta = &FONT_S; s.hud = &FONT_S;
  s.tileScale = 3; s.subScale = 1;
  s.cardScale = kCardScale; s.promptScale = 2; s.noticeScale = 5;
  s.ctxPitch = 6; s.ctxTickW = 2; s.ctxTickH = 10; s.ctxFillH = 8;
  s.btnR = kBtnR; s.btnGap = kBtnGap; s.btnFromBottom = 0;
  s.optH = kOptMaxH; s.optGap = kOptGap; s.optR = kOptR;
  return s;
}

// ---------------------------------------------------------------- the home

static inline float maxScroll(const Fleet& f) {
  return f.count > kVisible ? (float)((f.count - kVisible) * kPitch) : 0.f;
}

static inline Rect tileRect(int i, float scroll) {
  return Rect{kTilesX + i * kPitch - (int)lroundf(scroll), kHeaderH, kTileW, kTileH};
}

// Where a released row comes to rest: the tile nearest to where its momentum
// would carry it (a flick, faster than 0.5 px/ms, carries on for a quarter second).
static inline float settleTarget(const Fleet& f, float scroll, float velocity) {
  const float v = velocity > 0.5f || velocity < -0.5f ? velocity : 0.f;
  float t = scroll + v * 250.f;
  const float mx = maxScroll(f);
  if (t < 0) t = 0;
  if (t > mx) t = mx;
  t = roundf(t / kPitch) * kPitch;
  return t > mx ? mx : t;
}

// ---------------------------------------------------------------- card and prompt geometry

static inline CardButtons cardButtons(const Agent& a, DeckScope scope) {
  CardButtons b;
  if (!canAct(scope)) return b;
  b.kind[b.n++] = IconKind::Focus;
  if (a.status == Status::Running) b.kind[b.n++] = IconKind::Interrupt;
  if (scope == DeckScope::Admin) b.kind[b.n++] = IconKind::Stop;
  for (int i = 0; i < b.n; i++) b.cx[i] = W - M - kBtnRing - kBtnR - (b.n - 1 - i) * (2 * kBtnR + kBtnGap);
  b.cy = kBtnCy;
  return b;
}

struct StripPrompt {
  int questionY, questionLines;
  Rect box;                    // a permission's command; h = 0 for none
  int boxLines;
  int n, cols;                 // options shown, in one or two columns
  Rect opt[kMaxOptions];
  bool hints;                  // room for the options' hint lines
};

static inline int questionLineH() { return FONT_L.cellH - 1; }
static inline int boxLineH() { return FONT_S.cellH - 1; }

static inline StripPrompt promptGeom(const Agent& a) {
  StripPrompt g{};
  g.n = a.optCount;
  g.cols = g.n > 3 ? 2 : 1;
  const int rows = (g.n + g.cols - 1) / g.cols;
  int optH = rows ? (H - 2 * M - (rows - 1) * kOptGap) / rows : 0;
  if (optH > kOptMaxH) optH = kOptMaxH;
  const int colW = (kOptW - (g.cols - 1) * kOptGap) / g.cols;
  const int top = rows ? (H - rows * optH - (rows - 1) * kOptGap) / 2 : M;
  for (int i = 0; i < g.n; i++) {
    const int r = i / g.cols, col = i % g.cols;
    g.opt[i] = Rect{kOptX + col * (colW + kOptGap), top + r * (optH + kOptGap), colW, optH};
  }
  g.hints = g.cols == 1 && optH >= FONT_L.cellH + FONT_XS.cellH + 6;

  // The question under the header; a permission keeps two lines of its command
  // at the bottom, which is what the answer approves.
  g.questionY = 38;
  int bottom = H - M;
  if (isPermission(a) && a.waitDetail[0]) {
    const int lines = wrapLines(FONT_S, a.waitDetail, kQuestionW - 20, 2);
    g.boxLines = lines < 1 ? 1 : lines;
    g.box = Rect{M, 0, kQuestionW, g.boxLines * boxLineH() + 12};
    g.box.y = bottom - g.box.h;
    bottom = g.box.y - 6;
  }
  int qMax = (bottom - g.questionY) / questionLineH();
  if (qMax < 1) qMax = 1;
  g.questionLines = wrapLines(FONT_L, a.waitTitle[0] ? a.waitTitle : "needs you", kQuestionW, qMax);
  if (g.questionLines < 1) g.questionLines = 1;
  return g;
}

// ---------------------------------------------------------------- hit-testing

static inline PanelHit hit(const PanelUi& ui, const Fleet& f, int x, int y, DeckScope scope) {
  PanelHit h;
  if (ui.screen == Screen::Home) {
    if (y < kHeaderH) return h;
    for (int i = 0; i < f.count; i++)
      if (tileRect(i, ui.scroll).contains(x, y)) { h.kind = Hit::Tile; h.agent = i; return h; }
    return h;
  }
  const int i = findAgent(f, ui.openId);
  if (i < 0) return h;
  const Agent& a = f.agents[i];
  if (ui.screen == Screen::Card) {
    const CardButtons b = cardButtons(a, scope);
    for (int k = 0; k < b.n; k++) {
      const int dx = x - b.cx[k], dy = y - b.cy;
      if (dx * dx + dy * dy <= (kBtnR + 4) * (kBtnR + 4)) {
        h.kind = b.kind[k] == IconKind::Focus ? Hit::Focus : b.kind[k] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
        h.agent = i;
        return h;
      }
    }
    return h;
  }
  const StripPrompt g = promptGeom(a);
  for (int k = 0; k < g.n; k++)
    if (g.opt[k].contains(x, y)) { h.kind = Hit::Option; h.agent = i; h.index = k; return h; }
  return h;
}

// ---------------------------------------------------------------- the state machine

// Keeps the UI valid for a changed fleet.
static inline void fix(PanelUi& ui, const Fleet& f) {
  if (ui.screen != Screen::Home) {
    const int i = findAgent(f, ui.openId);
    if (i < 0) squareHome(ui);
    else if (ui.screen == Screen::Prompt && f.agents[i].status != Status::Waiting && !ui.backAfterToast)
      ui.screen = Screen::Card;                    // answered elsewhere: show the card
    else if (ui.screen == Screen::Card && f.agents[i].status == Status::Waiting)
      ui.screen = Screen::Prompt;                  // it started asking
  }
  if (!ui.dragging && !ui.animating) {
    const float mx = maxScroll(f);
    if (ui.scroll > mx) ui.scroll = mx;
    if (ui.scroll < 0) ui.scroll = 0;
  }
}

static inline void settleFrom(PanelUi& ui, const Fleet& f, float velocity, uint32_t ms) {
  ui.scrollFrom = ui.scroll;
  ui.scrollTo = settleTarget(f, ui.scroll, velocity);
  ui.animStart = ms;
  ui.animating = true;
}

static inline DeckCommand touch(PanelUi& ui, const SquareSpec& S, const Fleet& f, bool touching, int x, int y,
                                DeckScope scope, uint32_t ms) {
  DeckCommand out;
  const bool home = ui.screen == Screen::Home;
  if (touching) {
    if (!ui.down) {
      ui.down = true;
      ui.dragging = false;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ui.lastMs = ms;
      ui.lastY = x;                                // the row scrolls along x
      ui.velocity = 0;
      ui.holdFired = false;
      ui.caught = home && ui.animating;
      if (ui.caught) {                             // catch the moving row
        ui.animating = false;
        ui.settledMs = ms ? ms : 1;
        ui.pressed = PanelHit();
      } else {
        ui.pressed = hit(ui, f, x, y, scope);
      }
      ui.scroll0 = ui.scroll;
      if (ui.pressed.kind == Hit::Stop && scope != DeckScope::Admin) ui.pressed = PanelHit();
      return out;
    }
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > kSlop * kSlop) ui.pressed = PanelHit();   // it became a drag
    if (home && !ui.dragging && (dx > kSlop || dx < -kSlop) && (dx < 0 ? -dx : dx) >= (dy < 0 ? -dy : dy))
      ui.dragging = true;
    if (ui.dragging) {
      const float mx = maxScroll(f);
      float s = ui.scroll0 - dx;
      if (s < 0) s *= 0.35f;                       // a little give past the ends
      if (s > mx) s = mx + (s - mx) * 0.35f;
      ui.scroll = s;
      const uint32_t dt = ms - ui.lastMs;
      if (dt > 0) {
        const float v = -(float)(x - ui.lastY) / dt;
        ui.velocity = ui.velocity * 0.4f + v * 0.6f;
        ui.lastY = x;
        ui.lastMs = ms;
      }
    }
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
    if (ms - ui.lastMs > 80) ui.velocity = 0;     // it stopped before letting go
    settleFrom(ui, f, ui.velocity, ms);
    return out;
  }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (ui.caught) {                                 // it only stopped the row: rest on a tile
    settleFrom(ui, f, 0, ms);
  } else if (adx <= kSlop && ady <= kSlop) {
    if (pressed.kind != Hit::None || (uint32_t)(ms - ui.downMs) <= kTapMaxMs)
      out = squareTap(ui, S, f, pressed.kind != Hit::None ? pressed : hit(ui, f, ui.x0, ui.y0, scope), scope, ms);
  } else if (!home && dx >= kSwipeMin && adx >= ady) {
    squareHome(ui);                                // swipe right: back
  }
  return out;
}

static inline bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) {
  fix(ui, f);
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

// BOOT: back from a card or a prompt; at home, the next five tiles (then the first).
static inline void button(PanelUi& ui, const Fleet& f) {
  if (ui.screen != Screen::Home) { squareHome(ui); return; }
  const float mx = maxScroll(f);
  ui.animating = false;
  ui.scroll = ui.scroll >= mx - 1 ? 0 : fminf(ui.scroll + kVisible * kPitch, mx);
}

// ---------------------------------------------------------------- drawing

namespace draw {
using namespace square_detail;

// A battery outline filled to `pct`, its right edge at x, its top at y; accent when low.
static inline void battery(Canvas& c, const Theme& T, int x, int y, int pct) {
  constexpr int bw = 24, bh = 12;
  const uint16_t col = pct <= 15 ? T.accent : T.dim;
  const int bx = x - bw - 3;
  c.frame(bx, y, bw, bh, col, 2);
  c.rect(bx + bw, y + 3, 3, bh - 6, col);
  const int inner = (bw - 6) * (pct < 0 ? 0 : pct > 100 ? 100 : pct) / 100;
  if (inner > 0) c.rect(bx + 3, y + 3, inner, bh - 6, col);
}

static inline void header(Canvas& c, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  c.rect(0, 0, W, kHeaderH, T.bg);
  const int ty = (kHeaderH - FONT_L.cellH) / 2 + 1;
  c.text(FONT_L, M + 2, ty, "agents", T.ink);
  if (in.ui->wifi || (in.net && in.net->wifiCapable && in.net->ssid[0])) wifiMark(c, T, M + 2 + textW(FONT_L, "agents") + 22, ty + FONT_L.cellH * 3 / 4);
  int right = W - M;
  if (in.battery >= 0) {
    char pct[16];
    snprintf(pct, sizeof pct, "%d%%", in.battery);
    battery(c, T, right, (kHeaderH - 12) / 2, in.battery);
    right -= 36 + textW(FONT_XS, pct);
    c.text(FONT_XS, right + 2, (kHeaderH - FONT_XS.cellH) / 2 + 1, pct, in.battery <= 15 ? T.accent : T.dim);
    right -= 14;
  }
  char buf[48];
  const Counts n = countFleet(f);
  if (in.link == LinkState::Lost) snprintf(buf, sizeof buf, "offline");
  else snprintf(buf, sizeof buf, "%u wait" DECK_DOT "%u run", (unsigned)n.waiting, (unsigned)n.running);
  c.text(FONT_S, right - textW(FONT_S, buf), (kHeaderH - FONT_S.cellH) / 2 + 1, buf,
         in.link == LinkState::Lost || n.waiting ? T.accent : T.dim);
}

static inline void home(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < f.count; i++) {
    const Rect r = tileRect(i, ui.scroll);
    if (r.x + r.w <= 0 || r.x >= W) continue;
    tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, true, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i,
         kHeaderH);
  }
  header(c, T, in);
  // The position bar while the row moves, fading a second after it settles.
  const int alpha = posBarAlpha(ui, in.ms);
  const float mx = maxScroll(f);
  if (alpha > 0 && mx > 0) {
    const int span = W - 2 * M;
    int len = span * kVisible / f.count;
    if (len < 32) len = 32;
    const float s = ui.scroll < 0 ? 0 : ui.scroll > mx ? mx : ui.scroll;
    const int x = M + (int)((span - len) * s / mx);
    const uint16_t col = blend565(T.bg, blend565(T.bg, T.ink, 10), (uint8_t)alpha);
    roundRect(c, x, kBarY, len, kBarH, 1, col);
  }
}

// The critter on the left, a title and up to three lines beside it.
static inline void notice(Canvas& c, const Theme& T, Pose pose, uint16_t body, const char* title, const char* l1,
                          uint16_t c1, const char* l2, uint16_t c2, const char* l3, uint16_t c3, uint32_t ms) {
  constexpr int s = 5, tx = 40 + 18 * s + 30, tw = W - tx - M;
  drawSprite(c, 40, (H - 13 * s) / 2, s, Species::Claude, pose, body, T.bg, ms, true);
  const char* ls[3] = {l1, l2, l3};
  const uint16_t cs[3] = {c1, c2, c3};
  const int lines = (l1 ? 1 : 0) + (l2 ? 1 : 0) + (l3 ? 1 : 0);
  const int pitch = FONT_L.cellH + 4;
  int y = (H - (FONT_XL.cellH + 8 + lines * pitch)) / 2;
  c.text(FONT_XL, tx, y, title, T.ink, tw);
  y += FONT_XL.cellH + 8;
  for (int i = 0; i < 3; i++) {
    if (!ls[i]) continue;
    c.text(FONT_L, tx, y, ls[i], cs[i], tw);
    y += pitch;
  }
}

static inline void card(Canvas& c, const Theme& T, const PanelInput& in, const Agent& a) {
  char buf[224];
  c.fill(T.bg);
  const CardButtons b = cardButtons(a, in.scope);
  const int textRight = b.n ? b.cx[0] - kBtnR - 16 : W - M;

  // Header: the critter; beside it the name over the pill.
  const Look k = lookOf(T, a);
  const uint16_t body = a.status == Status::Running || hungry(a) ? T.accent : k.body == T.bg ? T.ink : k.body;
  drawSprite(c, M, 12, kCardScale, a.species, poseOf(a, in.nowEpoch), body, T.bg, in.ms, true, a.hat, a.hatColor);
  const int tx = M + 18 * kCardScale + 10;
  c.text(FONT_XL, tx, 6, a.title[0] ? a.title : a.id, T.ink, textRight - tx);
  pillLabel(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Error || hungry(a);
  pillText(c, FONT_S, tx, 36, buf, loud ? T.accent : T.ink, T.bg, textRight - tx);

  // Doing now, and how many subagents work for it on the right.
  const int lw = W - 2 * M;
  int y = 72;                                     // clear of the stop hold's ring
  int cw = 0;
  if (a.subagents) {
    subCountLabel(a, buf, sizeof buf);
    cw = textW(FONT_XS, buf);
    c.text(FONT_XS, W - M - cw, y, buf, T.dim);
  }
  c.text(FONT_XS, M, y, "doing now", T.dim, lw - cw - 8);
  doingNow(a, buf, sizeof buf);
  c.text(FONT_L, M, y + FONT_XS.cellH, buf, T.ink, lw);

  // Context on the left half: label, numbers, percent, the dotted bar.
  y = 112;
  const int half = W / 2 - M - 12;
  const int pct = ctxPct(a);
  const int cx = M + c.text(FONT_S, M, y, "context", T.ink) + 8;
  if (a.ctxWindow) {
    char u[16], w[16];
    compact(a.ctxUsed, u, sizeof u);
    compact(a.ctxWindow, w, sizeof w);
    snprintf(buf, sizeof buf, "%s / %s", u, w);
  } else {
    snprintf(buf, sizeof buf, "--");
  }
  c.text(FONT_XS, cx, y + 2, buf, T.dim);
  snprintf(buf, sizeof buf, a.ctxWindow ? "%d%%" : "--", pct);
  c.text(FONT_S, M + half - textW(FONT_S, buf), y, buf, T.ink);
  {
    constexpr int pitch = 6, tickW = 2, tickH = 10, fillH = 8;
    const int by = y + FONT_S.cellH + 6;
    const int ticks = (half + pitch - tickW) / pitch;
    const int fillW = a.ctxWindow ? half * pct / 100 : 0;
    for (int i = 0; i < ticks; i++) {
      const int x = M + i * pitch;
      if (x + tickW <= M + fillW) continue;
      c.rect(x, by, tickW, tickH, T.dim);
    }
    if (fillW) c.rect(M, by + (tickH - fillH) / 2, fillW, fillH, pct >= 85 ? T.accent : T.ink);
  }

  // Tokens, cost, turns on the right half.
  const int x0 = W / 2 + 12, colW = (W - M - x0) / 3;
  const char* labels[3] = {"tokens", "cost", "turns"};
  char vals[3][16];
  compact(a.tokensOut, vals[0], sizeof vals[0]);
  if (a.costUsd > 0) snprintf(vals[1], sizeof vals[1], "~$%.2f", (double)a.costUsd);
  else snprintf(vals[1], sizeof vals[1], "\xE2\x80\x94");
  snprintf(vals[2], sizeof vals[2], "%u", a.turns);
  for (int i = 0; i < 3; i++) {
    c.text(FONT_XS, x0 + i * colW, y, labels[i], T.dim, colW - 4);
    c.text(FONT_XL, x0 + i * colW, y + FONT_XS.cellH, vals[i], T.ink, colW - 4);
  }

  // The buttons.
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < b.n; i++) {
    const Hit hk = b.kind[i] == IconKind::Focus ? Hit::Focus : b.kind[i] == IconKind::Interrupt ? Hit::Interrupt : Hit::Stop;
    const bool pressed = ui.pressed.kind == hk;
    iconButton(c, b.cx[i], b.cy, kBtnR, b.kind[i], pressed, hk == Hit::Stop ? holdPermille(ui, in.ms) : 0, T.bg, T.ink,
               T.accent, T.line);
  }
}

static inline void prompt(Canvas& c, const Theme& T, const PanelInput& in, const Agent& a) {
  const bool perm = isPermission(a);
  const uint16_t bg = perm ? T.accent : T.bg;
  const uint16_t fg = perm ? T.bg : T.ink;
  c.fill(bg);
  if (!perm) c.frame(0, 0, W, H, T.accent, 3);
  const StripPrompt g = promptGeom(a);

  // Header: ×2 critter, the name, the age in a pill at the column's end.
  drawSprite(c, M, 6, 2, a.species, Pose::Wait, perm ? T.bg : T.dim, bg, in.ms, false, a.hat, a.hatColor);
  char age[8];
  fmtAge(a.statusSince ? in.nowEpoch - a.statusSince : 0, age, sizeof age);
  const int pw = textW(FONT_S, age) + 24;
  const int px = M + kQuestionW - pw;
  const int nameX = M + 14 * 2 + 8;
  c.text(FONT_S, nameX, 10, a.title[0] ? a.title : a.id, fg, px - 8 - nameX);
  pillText(c, FONT_S, px, 9, age, perm ? T.bg : T.accent, perm ? T.ink : T.bg, pw);

  textWrapped(c, FONT_L, M, g.questionY, a.waitTitle[0] ? a.waitTitle : "needs you", fg, kQuestionW, g.questionLines,
              questionLineH());
  if (g.box.h) {
    roundRect(c, g.box.x, g.box.y, g.box.w, g.box.h, 8, T.bg);
    textWrapped(c, FONT_S, g.box.x + 10, g.box.y + 6, a.waitDetail, T.ink, g.box.w - 20, g.boxLines, boxLineH());
  }

  if (!g.n) {
    textCentered(c, FONT_L, kOptX + kOptW / 2, (H - FONT_L.cellH) / 2, "answer in the terminal", fg, kOptW);
    return;
  }
  const PanelUi& ui = *in.ui;
  const bool narrow = g.cols > 1;
  const Font& tf = narrow ? FONT_S : FONT_L;
  const Font& kf = narrow ? FONT_L : FONT_XL;
  for (int i = 0; i < g.n; i++) {
    const Rect& r = g.opt[i];
    const Option& o = a.opts[i];
    const bool pressed = ui.pressed.kind == Hit::Option && ui.pressed.index == i;
    uint16_t fill, edge, num, ttl, hnt;
    if (pressed) {
      fill = edge = T.ink;
      num = ttl = hnt = T.bg;
    } else if (perm) {
      fill = o.primary ? T.bg : T.accent;
      edge = T.bg;
      num = ttl = o.primary ? T.accent : T.bg;
      hnt = o.primary ? T.dim : blend565(T.accent, T.bg, 9);
    } else {
      fill = T.panel;
      edge = T.ringTrack;
      num = ttl = T.ink;
      hnt = T.dim;
    }
    roundRect(c, r.x, r.y, r.w, r.h, kOptR, edge);
    roundRect(c, r.x + 2, r.y + 2, r.w - 4, r.h - 4, kOptR - 2, fill);
    if (perm && !pressed && !o.primary) roundRect(c, r.x + 3, r.y + 3, r.w - 6, r.h - 6, kOptR - 3, fill);
    const int pad = narrow ? 8 : 10, numW = kf.cellW + (narrow ? 8 : 14);
    c.text(kf, r.x + pad, r.y + (r.h - kf.cellH) / 2 + 1, o.key, num, numW);
    char title[96], hint[96];
    splitOption(o.label, title, sizeof title, hint, sizeof hint);
    const int tx = r.x + pad + numW, tw = r.w - (tx - r.x) - pad;
    const int lines = narrow ? wrapLines(tf, title, tw, 2) : 1;
    if (lines > 1) {                             // a narrow cell: the title on two lines
      const int lh = tf.cellH - 2;
      textWrapped(c, tf, tx, r.y + (r.h - 2 * lh) / 2, title, ttl, tw, 2, lh);
    } else if (g.hints && hint[0]) {
      const int th = tf.cellH - 2 + FONT_XS.cellH;
      const int ty = r.y + (r.h - th) / 2;
      c.text(tf, tx, ty, title, ttl, tw);
      c.text(FONT_XS, tx, ty + tf.cellH - 2, hint, hnt, tw);
    } else {
      c.text(tf, tx, r.y + (r.h - tf.cellH) / 2 + 1, title, ttl, tw);
    }
  }
}

}  // namespace draw

// The whole frame.
static inline void drawStrip(Canvas& c, const SquareSpec& S, const Theme& T, const PanelInput& in) {
  using draw::notice;
  c.fill(T.bg);
  char buf[96];
  const char* name = in.device && in.device[0] ? in.device : "<name>";
  const PanelUi* tu = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0 ? in.ui : nullptr;
  if (in.link == LinkState::Connecting) {
    if (in.net && in.net->via == NetVia::Wifi) {   // where Wi-Fi is (net_model.h's words)
      const NetNotice n = netNotice(*in.net);
      notice(c, T, n.problem ? Pose::Exit : Pose::Think, T.dim, n.title, n.line1, n.problem ? T.accent : T.dim, n.line2,
             T.dim, nullptr, T.dim, in.ms);
    } else {
      notice(c, T, Pose::Think, T.dim, "connecting to the deck", "plugged into the Mac?", T.dim, "agentctl link add",
             T.ink, "/dev/cu.usbmodem\xE2\x80\xA6", T.dim, in.ms);
    }
  } else if (in.link == LinkState::Unapproved) {
    snprintf(buf, sizeof buf, "agentctl devices approve %s", name);
    if (textW(FONT_L, buf) <= W - 160 - M)
      notice(c, T, Pose::Wait, T.ink, "waiting for approval", "on the Mac:", T.dim, buf, T.accent, nullptr, T.ink, in.ms);
    else
      notice(c, T, Pose::Wait, T.ink, "waiting for approval", "on the Mac:", T.dim, "agentctl devices approve", T.ink,
             name, T.accent, in.ms);
  } else if (!in.fleet || in.fleet->count == 0) {
    if (in.link == LinkState::Lost) {
      char seen[16];
      fmtAge(in.silentSec, seen, sizeof seen);
      snprintf(buf, sizeof buf, "last seen %s ago" DECK_DOT "retrying\xE2\x80\xA6", seen);
      notice(c, T, Pose::Exit, T.dim, "deck offline", buf, T.dim, "agentctl serve", T.ink, nullptr, T.ink, in.ms);
    } else {
      notice(c, T, Pose::Idle, T.dim, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr, T.dim,
             "agentctl new", T.ink, nullptr, T.ink, in.ms);
    }
  } else {
    const PanelUi& ui = *in.ui;
    const int open = ui.screen != Screen::Home ? findAgent(*in.fleet, ui.openId) : -1;
    if (open >= 0 && ui.screen == Screen::Card) draw::card(c, T, in, in.fleet->agents[open]);
    else if (open >= 0) draw::prompt(c, T, in, in.fleet->agents[open]);
    else draw::home(c, S, T, in);
  }
  if (tu) square_detail::toastPill(c, S, T, tu->toast);
}

}  // namespace strip

// The strip's PanelLayout.
class StripPanel : public PanelLayout {
 public:
  StripPanel() : S(strip::squareSpec()), T(deckTheme(false)) {}
  int width() const override { return strip::W; }
  int height() const override { return strip::H; }
  bool trueBlack() const override { return false; }
  void draw(Canvas& c, const PanelInput& in) const override { strip::drawStrip(c, S, T, in); }
  DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope, uint32_t ms) const override {
    return strip::touch(ui, S, f, down, x, y, scope, ms);
  }
  bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const override { return strip::tick(ui, f, ms); }
  void button(PanelUi& ui, const Fleet& f) const override { strip::button(ui, f); }
  void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const override { squareResult(ui, seq, code, ms); }
  const SquareSpec S;
  const Theme T;
};
