#pragma once
// The LilyGO T-Display-S3's screen, 320 x 170: the 1.47" layout (layout_147.h)
// with bigger text for the 1.9" glass, and what its touch screen and two
// buttons do. Everything here runs on the host (tests/test_tds3.cpp).
//
//   touch   tap a mini-board cell: show that agent (held for 30 s)
//           swipe left / right: the next / the previous agent
//           tap an option of a waiting agent: answer it
//           tap the agent anywhere else: hide the prompt (to look at other
//           agents), or focus its terminal when no prompt is shown (never answers)
//           hold the agent: show its hidden prompt again
//   lower button (GPIO 14)  press: the next agent · hold 1 s: answer the
//                            primary option of the prompt on screen, or focus
//   BOOT                    press: the previous agent · hold 1 s: interrupt
//
// A waiting agent with scope act turns the screen into its prompt: the
// question across the hero, the mini-board's top half, and the options as
// big buttons along the bottom. With view scope the board stays read-only.
//
// Motion: whoever waits gets a frame of marching dashes (its mini-board cell,
// and the hero or the whole prompt); a finished agent you haven't looked at
// yet ("hungry") gets the web's sliding orange stripes; and a new hero slides
// in from the side it came from. The firmware draws about every 40 ms.
//
// With no agents the screen keeps a little life: the time in big digits and
// two critters walking along the bottom, stopping to think or celebrate, who
// fall asleep after a few quiet minutes. The notices move too: the critter
// runs while connecting, looks around while waiting for approval, and nods
// off when the deck goes quiet.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "deck_cmd.h"
#include "deck_ui.h"
#include "font_big.h"
#include "layout_147.h"
#include "net_model.h"
#include "round_geom.h"

namespace tds3 {

constexpr int kW = 320, kH = 170;

// Text, Roboto Mono: bold for what matters (the name, what the agent does,
// the question, the answers), regular for the rest; 12 x 27 for the line
// to read from afar and the footer, 9 x 21 otherwise.
static const Font& kBold = FONT_B15;     // 9 x 21, 700
static const Font& kReg = FONT_L;        // 9 x 21, 500
static const Font& kBigBold = FONT_B20;  // 12 x 27, 700
static const Font& kBigReg = FONT_XL;    // 12 x 27, 500

// Touch.
constexpr int kTapSlop = 14;            // px a tap may move
constexpr int kSwipeMin = 48;           // px a swipe travels sideways
constexpr uint32_t kTouchHoldMs = 600;  // a finger held this long on the agent shows its hidden prompt
constexpr uint32_t kToastMs = 1600;
constexpr int kMaxButtons = 3;          // option buttons on screen; more page through "+N"

// The prompt view.
constexpr int kStripY = 108;          // the buttons' strip: y 108..169, full width
constexpr int kButtonY = 112, kButtonH = 54;
constexpr int kPromptMiniRows = 2;    // mini-board rows left above the strip

// Motion.
constexpr uint32_t kSlideMs = 220;        // a new hero slides in over this long
constexpr int kSlidePx = 72;              // from this far to the side
constexpr int kAntsDash = 6, kAntsGap = 4;
constexpr uint32_t kAntsStepMs = 60;      // the dashes move a pixel this often
constexpr uint32_t kToastSlideMs = 150;   // a toast slides up in, and down out
constexpr uint32_t kBarGrowMs = 350;      // a new hero's context bar grows to its value
// Hungry stripes (app.css .tile.hungry): orange at about 13 %, sliding one
// period in 6 steps of 100 ms.
constexpr uint8_t kStripeAlpha = 2;       // of 15
constexpr uint32_t kStripeStepMs = 100;
// The empty screen's walkers.
constexpr uint32_t kWalkCycleMs = 12000;  // walk 9 s, think 2 s, celebrate 1 s
constexpr uint32_t kWalkPxPerS = 28;
constexpr uint32_t kNapAfterMs = 180000;  // no agents this long: they fall asleep
constexpr uint32_t kDozeAfterMs = 6000;   // deck offline this long: the critter nods off

enum class Key : uint8_t { Next, Prev };    // GPIO 14, BOOT

struct Ui {
  // The finger.
  bool down = false;
  int x0 = 0, y0 = 0, x = 0, y = 0;
  int pressed = -1;          // the hit drawn pressed while the finger stays on it
  uint32_t downMs = 0;
  bool holdFired = false;    // the hold already acted: the release does nothing
  // The agent and prompt under the finger when it went down: a release acts on
  // them only, never on whatever replaced them meanwhile.
  char pressAgent[12] = "";
  char pressPrompt[16] = "";
  // A prompt the user hid with a tap: this agent's, this prompt. A new prompt shows.
  char hiddenAgent[12] = "";
  char hiddenPrompt[16] = "";
  // The prompt's buttons page through the options from here.
  char optPrompt[16] = "";
  int optFirst = 0;
  // Feedback.
  char toast[40] = "";
  uint32_t toastStart = 0, toastUntil = 0;
  uint32_t cmdSeq = 0;       // the command waiting for its Result (sent() sets it)
  DeckCmd cmdKind = DeckCmd::None;   // and what it was, for the words when it fails
  // The hero's slide (noteHero): +1 comes in from the right, -1 from the left.
  char heroId[12] = "";
  int pendingDir = 0;        // what a swipe, a tap or a button asked for
  int slideDir = 0;
  uint32_t slideStart = 0;
  uint32_t heroSince = 0;    // when the hero shown last changed: its context bar grows from there
  // The link state shown, and since when (noteLink): the notices' and the empty screen's clock.
  LinkState shownLink = LinkState::Connecting;
  uint32_t linkSince = 0;
};

// Before drawing: notes the link state shown, for the notices' motion.
static inline void noteLink(Ui& ui, LinkState shown, uint32_t ms) {
  if (shown != ui.shownLink || !ui.linkSince) {
    ui.shownLink = shown;
    ui.linkSince = ms ? ms : 1;
  }
}

// Before drawing: notes the hero shown, and starts a slide when it changed.
static inline void noteHero(Ui& ui, const Fleet* f, int hero, uint32_t ms) {
  const char* id = f && hero >= 0 && hero < f->count ? f->agents[hero].id : "";
  if (strcmp(id, ui.heroId) != 0) {
    if (ui.heroId[0] && id[0]) {
      ui.slideDir = ui.pendingDir ? ui.pendingDir : 1;
      ui.slideStart = ms;
    }
    snprintf(ui.heroId, sizeof ui.heroId, "%s", id);
    ui.heroSince = ms;
  }
  ui.pendingDir = 0;
}

// How far the hero is still shifted, in px (0 when it has arrived).
static inline int slideOffset(const Ui* ui, uint32_t ms) {
  if (!ui || !ui->slideDir) return 0;
  const uint32_t t = ms - ui->slideStart;
  if (t >= kSlideMs) return 0;
  const float rest = 1.0f - (float)t / kSlideMs;      // ease out: fast, then settling
  return (int)(ui->slideDir * kSlidePx * rest * rest * rest);
}

struct Input {
  const Fleet* fleet;        // while the link is Lost this is the last fleet received
  int hero;                  // index into fleet->agents, -1 when there is none
  LinkState link;
  const char* device;        // the board's name on the deck, shown until approved
  uint32_t ms;
  int64_t nowEpoch;
  bool held;                 // a button or a touch has pinned the hero
  DeckScope scope;
  const Ui* ui;              // may be null: no toast, nothing pressed
  int clockMin = -1;         // local time, minutes past midnight; -1: no clock
  bool clockUtc = false;     // the time is UTC (no time zone set): say so
  const NetView* net = nullptr;   // the link: USB or Wi-Fi, and how Wi-Fi is going
};

static inline bool onWifi(const Input& in) { return in.net && in.net->via == NetVia::Wifi; }

static inline void toast(Ui& ui, const char* text, uint32_t ms, uint32_t forMs = kToastMs) {
  snprintf(ui.toast, sizeof ui.toast, "%s", text);
  ui.toastStart = ms;
  ui.toastUntil = ms + forMs;
}

static inline bool toastShown(const Ui& ui, uint32_t ms) {
  return ui.toast[0] && (int32_t)(ui.toastUntil - ms) > 0;
}

// A waiting hero the board may answer.
static inline bool promptView(const Agent& a, DeckScope s) {
  return canAct(s) && a.status == Status::Waiting && a.optCount > 0;
}

static inline bool promptHidden(const Ui* ui, const Agent& a) {
  return ui && ui->hiddenAgent[0] && !strcmp(ui->hiddenAgent, a.id) && !strcmp(ui->hiddenPrompt, a.promptId);
}

// The prompt view: a waiting hero the board may answer, unless the user hid it.
static inline bool promptShown(const Agent& a, DeckScope s, const Ui* ui) {
  return promptView(a, s) && !promptHidden(ui, a);
}

// ---------------------------------------------------------------- geometry
// Hit ids: 0..7 a mini-board cell, 10 + k button k, 20 the hero, -1 nothing.
constexpr int kHitHero = 20, kHitButton = 10;

struct Button {
  int x, y, w, h;
  int opt;                   // index into Agent::opts, -1 = "+N" (the next options)
};

static inline int optFirstFor(const Ui* ui, const Agent& a) {
  if (!ui || strcmp(ui->optPrompt, a.promptId) != 0) return 0;
  return ui->optFirst < a.optCount ? ui->optFirst : 0;
}

// The option buttons of the prompt view, left to right. Returns how many.
static inline int buttons(const Agent& a, const Ui* ui, Button* out) {
  const int first = optFirstFor(ui, a);
  const bool more = a.optCount > kMaxButtons;
  int n = more ? kMaxButtons - 1 : a.optCount;          // paging: two options and "+N"
  if (n > a.optCount - first) n = a.optCount - first;
  const int total = n + (more ? 1 : 0);
  if (total <= 0) return 0;                              // no options (any more)
  const int x0 = 6, gap = 6, span = kW - 2 * x0;
  const int w = (span - gap * (total - 1)) / total;
  for (int i = 0; i < n; i++) out[i] = Button{x0 + i * (w + gap), kButtonY, w, kButtonH, first + i};
  if (more) out[n] = Button{x0 + n * (w + gap), kButtonY, w, kButtonH, -1};
  return total;
}

// Mini-board cells drawn: all 8, or the top rows above the prompt's strip.
static inline int miniCellsShown(bool prompt) { return prompt ? 2 * kPromptMiniRows : layout::miniCells; }

static inline bool inside(int x, int y, int rx, int ry, int rw, int rh) {
  return x >= rx && x < rx + rw && y >= ry && y < ry + rh;
}

static inline int hitTest(const Fleet& f, int hero, const Ui* ui, DeckScope scope, int x, int y) {
  using namespace layout;
  const bool prompt = hero >= 0 && hero < f.count && promptShown(f.agents[hero], scope, ui);
  if (prompt && y >= kStripY) {
    Button b[kMaxButtons];
    const int n = buttons(f.agents[hero], ui, b);
    for (int i = 0; i < n; i++)
      if (inside(x, y, b[i].x, b[i].y, b[i].w, b[i].h)) return kHitButton + i;
    return -1;                                       // the strip between buttons
  }
  if (x < heroW) return hero >= 0 ? kHitHero : -1;
  const int cells = miniCellsShown(prompt);
  for (int i = 0; i < cells; i++) {
    const int cx = miniX + (i % 2) * stepX, cy = miniY + (i / 2) * stepY;
    if (inside(x, y, cx - 1, cy - 1, stepX, stepY)) return i;   // the gap between cells counts
  }
  return -1;
}

// ---------------------------------------------------------------- drawing
namespace detail {

using screen_detail::Tone;
using screen_detail::toneOf;

// Text that fits: whole tokens appended while there is room for them.
static inline void addToken(char* buf, size_t cap, int& len, int room, const char* tok) {
  const int add = (len ? 3 : 0) + (int)utf8Len(tok);    // " · " counts as three cells
  if (len + add > room) return;
  len += snprintf(buf + len, cap - len, "%s%s", len ? DECK_DOT : "", tok);
}

using screen_detail::centered;

// A full-screen message: the critter (dx px off centre), a title, one or two lines.
static inline void notice(Canvas& c, Pose pose, uint16_t body, const char* title, const char* hint, uint32_t ms,
                          const char* hint2 = nullptr, int dx = 0) {
  drawSprite(c, c.w / 2 - 36 + dx, 14, 4, Species::Claude, pose, body, col::bg, ms, true);
  centered(c, kBigBold, 78, title, col::ink);
  centered(c, kReg, 112, hint, col::dim);
  if (hint2) centered(c, kBold, 138, hint2, col::ink);
}

// 0 .. span .. 0 over periodMs: a walk there and back.
static inline int pingpong(uint32_t t, int span, uint32_t periodMs) {
  const uint32_t p = t % periodMs, half = periodMs / 2;
  return (int)(p < half ? (uint64_t)span * p / half : (uint64_t)span * (periodMs - p) / half);
}

// One walker of the empty screen, `t` ms in: where it is and what it does.
struct Walker { int x; Pose pose; };

static inline Walker walker(uint32_t t, int span, bool asleep) {
  const uint32_t cycles = t / kWalkCycleMs, in = t % kWalkCycleMs;
  const uint32_t walked = cycles * 9000 + (in < 9000 ? in : 9000);     // ms spent walking so far
  const uint32_t leg = (uint32_t)span * 2 * 1000 / kWalkPxPerS;        // there and back, in ms
  Walker w{pingpong(walked, span, leg ? leg : 1), Pose::Run};
  if (asleep) w.pose = Pose::Idle;
  else if (in >= 9000 && in < 11000) w.pose = Pose::Think;
  else if (in >= 11000) w.pose = Pose::Celebrate;
  return w;
}

// No agents: the time, two critters walking along the bottom, and what to do.
static inline void emptyScene(Canvas& c, const Input& in, uint32_t since) {
  const bool asleep = since >= kNapAfterMs;
  char buf[48];
  if (in.clockMin >= 0) {
    snprintf(buf, sizeof buf, "%02d:%02d", in.clockMin / 60 % 24, in.clockMin % 60);
    const int w = 5 * FONT_XXL.cellW, x = (c.w - w) / 2;
    c.text(FONT_XXL, x, 4, buf, col::ink);
    if (in.clockUtc) c.text(kReg, x + w + 6, 16, "UTC", col::dim);
  }
  // How the board reaches the deck, top right: the Wi-Fi mark, or "USB".
  if (onWifi(in)) drawNetMark(c, c.w - 22, 26, col::dim);
  else c.text(kReg, c.w - 4 - 3 * kReg.cellW, 8, "USB", col::dim);
  // The ground, and the two critters on it: Claude's block and the Codex onigiri.
  const int ground = 106, span = c.w - 2 * 12 - 56;
  c.rect(12, ground, c.w - 24, 1, col::line);
  const Walker a = walker(since, span, asleep), b = walker(since + 6000, span, asleep);
  drawSprite(c, 12 + a.x, ground - 44, 3, Species::Claude, a.pose, col::ink, col::bg, in.ms, true);
  drawSprite(c, 12 + span - b.x, ground - 44, 3, Species::Codex, b.pose, col::dim, col::bg, in.ms + 250, true);
  const char* where = in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr;
  if (where) snprintf(buf, sizeof buf, "no agents" DECK_DOT "%s", where);
  else snprintf(buf, sizeof buf, "no agents");
  centered(c, kBold, 116, buf, col::ink);
  centered(c, kReg, 142, "agentctl new", col::dim);
}

// Up to two lines of `s` in w px, broken at a space when one is near.
static inline void wrap2(Canvas& c, const Font& f, int x, int y, int w, const char* s, uint16_t color) {
  const int room = w / f.cellW;
  if (room <= 0) return;
  const int n = utf8Len(s);
  if (n <= room) { c.text(f, x, y, s, color, w); return; }
  // Byte offset of the break: the last space within the first line, else a hard cut.
  const char* p = s;
  const char* cut = nullptr;
  const char* space = nullptr;
  for (int i = 0; i < room && *p; i++) {
    if (*p == ' ') space = p;
    utf8Next(p);
    cut = p;
  }
  const char* end = space && space - s > 0 ? space : cut;
  char first[PANEL_OPTION_LABEL];
  const size_t len = (size_t)(end - s) < sizeof first ? (size_t)(end - s) : sizeof first - 1;
  memcpy(first, s, len);
  first[len] = '\0';
  c.text(f, x, y, first, color, w);
  while (*end == ' ') end++;
  c.text(f, x, y + f.cellH, end, color, w);
}

// Marching dashes along the inside of a rectangle, t px thick, moving
// clockwise. Each dash takes the colour that stands out from what is under
// it: dark on orange, orange on anything else.
static inline void ants(Canvas& c, int x, int y, int w, int h, int t, uint32_t ms) {
  const int period = kAntsDash + kAntsGap;
  const int off = (int)((ms / kAntsStepMs) % period);
  auto dot = [&](int px, int py, int dw, int dh, int pos) {
    if ((pos + period - off) % period >= kAntsDash) return;
    if (px < 0 || py < c.top || px >= c.w || py >= c.h) return;
    const uint16_t under = c.px[py * c.w + px];
    c.rect(px, py, dw, dh, under == col::accent ? col::bg : col::accent);
  };
  int pos = 0;
  for (int i = 0; i < w; i++) dot(x + i, y, 1, t, pos++);                 // top, left to right
  for (int i = 1; i < h; i++) dot(x + w - t, y + i, t, 1, pos++);         // right, down
  for (int i = w - 2; i >= 0; i--) dot(x + i, y + h - t, 1, t, pos++);    // bottom, right to left
  for (int i = h - 2; i > 0; i--) dot(x, y + i, t, 1, pos++);             // left, up
}

// Diagonal stripes ("/"), half of each period tinted orange, sliding right.
static inline void stripes(Canvas& c, int x, int y, int w, int h, int period, uint32_t ms) {
  const int off = (int)((ms / kStripeStepMs) % 6) * period / 6;
  for (int yy = y < c.top ? c.top : y; yy < y + h && yy < c.h; yy++)
    for (int xx = x < 0 ? 0 : x; xx < x + w && xx < c.w; xx++)
      if (((xx - x) + (yy - y) - off + 6 * period) % period < period / 2) {
        uint16_t& px = c.px[yy * c.w + xx];
        px = blend565(px, col::accent, kStripeAlpha);
      }
}

// Shifts a rectangle of the canvas dx px sideways; what it leaves is `fill`.
static inline void shift(Canvas& c, int x, int y, int w, int h, int dx, uint16_t fill) {
  if (!dx) return;
  if (dx >= w || -dx >= w) { c.rect(x, y, w, h, fill); return; }
  for (int yy = y; yy < y + h && yy < c.h; yy++) {
    if (yy < c.top) continue;
    uint16_t* row = c.px + yy * c.w + x;
    if (dx > 0) {
      memmove(row + dx, row, (size_t)(w - dx) * sizeof *row);
      for (int i = 0; i < dx; i++) row[i] = fill;
    } else {
      memmove(row, row - dx, (size_t)(w + dx) * sizeof *row);
      for (int i = w + dx; i < w; i++) row[i] = fill;
    }
  }
}

static inline void miniBoard(Canvas& c, const Fleet& f, int hero, int64_t nowEpoch, uint32_t ms, int cells,
                             int pressed) {
  using namespace layout;
  const int rows = (cells + 1) / 2;
  c.rect(heroW, 6, 1, rows * stepY - 6, col::line);
  const bool overflow = f.total > (unsigned)cells;
  for (int i = 0; i < cells; i++) {
    const int cx = miniX + (i % 2) * stepX, cy = miniY + (i / 2) * stepY;
    if (overflow && i == cells - 1) {
      char more[8];
      snprintf(more, sizeof more, "+%u", (unsigned)(f.total - (cells - 1)));
      c.frame(cx, cy, cellW, cellH, pressed == i ? col::ink : col::line);
      c.text(kBold, cx + (cellW - (int)utf8Len(more) * kBold.cellW) / 2, cy + 10, more, col::dim);
      continue;
    }
    if (i >= f.count) {
      screen_detail::dashedFrame(c, cx, cy, cellW, cellH, col::line);
      continue;
    }
    const Agent& a = f.agents[i];
    if (a.status == Status::Waiting) c.rect(cx, cy, cellW, cellH, col::accent);
    else c.frame(cx, cy, cellW, cellH, a.status == Status::Error || hungry(a) ? col::accent : col::line);
    if (hungry(a)) stripes(c, cx + 1, cy + 1, cellW - 2, cellH - 2, 12, ms);
    if (i == hero || i == pressed) c.frame(cx, cy, cellW, cellH, col::ink, 2);
    else if (a.status == Status::Waiting) ants(c, cx, cy, cellW, cellH, 2, ms);
    const Tone tone = toneOf(a);
    drawSprite(c, cx + 6, cy + 8, 2, a.species, poseOf(a, nowEpoch), tone.body, tone.cut, ms, false, a.hat,
               a.hatColor);
  }
}

// Sprite, name and status: the hero's top left, in both views.
static inline void heroHead(Canvas& c, const Agent& a, const Input& in, uint16_t fg) {
  using namespace layout;
  char buf[112];
  const bool waiting = a.status == Status::Waiting;
  const Tone tone = toneOf(a);
  drawSprite(c, pad + 4, 8, 4, a.species, poseOf(a, in.nowEpoch), tone.body, tone.cut, in.ms, true, a.hat, a.hatColor);
  const int tw = heroW - 4 - textX;
  c.text(kBold, textX, 6, a.title[0] ? a.title : a.id, fg, tw);
  statusLine(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Running || a.status == Status::Error || hungry(a);
  c.text(kReg, textX, 31, buf, waiting ? col::bg : loud ? col::accent : col::ink, tw);
}

static inline void hero(Canvas& c, const Input& in) {
  using namespace layout;
  const Fleet& f = *in.fleet;
  const Agent& a = f.agents[in.hero];
  const bool waiting = a.status == Status::Waiting;
  const uint16_t fg = waiting ? col::bg : col::ink;
  const int lw = heroW - 2 * pad;
  char buf[112];

  if (waiting) c.rect(0, 0, heroW, c.h, col::accent);
  if (a.status == Status::Error) c.frame(0, 0, heroW, c.h, col::accent, 2);
  if (hungry(a)) stripes(c, 0, 0, heroW, c.h, 24, in.ms);
  heroHead(c, a, in, fg);

  // What it is doing or asking, one line, big.
  activityLine(a, buf, sizeof buf);
  if (a.status == Status::Running && a.actTool[0] && a.actDetail[0]) {
    // A long path keeps its file name: cut the start, not the end.
    const int used = c.text(kBigBold, pad, 62, a.actTool, fg, lw) + kBigBold.cellW;
    const bool path = strchr(a.actDetail, '/') && !strchr(a.actDetail, ' ');
    c.text(kBigBold, pad + used, 62, a.actDetail, fg, lw - used, path);
  } else {
    c.text(kBigBold, pad, 62, buf[0] ? buf : a.aiTitle, fg, lw);
  }

  // Context window: a new hero's bar grows to its value.
  if (a.ctxWindow) {
    int pct = ctxPct(a);
    const int barW = 150;
    if (in.ui) {
      const uint32_t t = in.ms - in.ui->heroSince;
      if (t < kBarGrowMs) {
        const float rest = 1.0f - (float)t / kBarGrowMs;
        pct = (int)(pct * (1.0f - rest * rest * rest));
      }
    }
    c.rect(pad, 100, barW, 8, waiting ? blend565(col::accent, col::bg, 5) : col::line);
    c.rect(pad, 100, barW * pct / 100, 8, waiting ? col::bg : pct >= 85 ? col::accent : col::ink);
    snprintf(buf, sizeof buf, "%d%%", pct);
    c.text(kReg, pad + barW + 8, 93, buf, fg);
  }

  // Footer, big: position in the fleet, then what needs the user, then what runs.
  int fx = pad;
  if (in.held) { c.rect(pad, 147, 7, 7, fg); fx += 13; }
  if (in.link == LinkState::Lost) {
    c.text(kBigReg, fx, 136, "deck offline", waiting ? col::bg : col::accent, lw - (fx - pad));
    return;
  }
  const Counts n = countFleet(f);
  const int markW = onWifi(in) ? 36 : 0;                     // the Wi-Fi mark at the footer's end
  const int footRoom = (lw - (fx - pad) - markW) / kBigReg.cellW;
  int len = snprintf(buf, sizeof buf, "%d/%u", in.hero + 1, (unsigned)f.total);
  char tok[16];
  if (n.waiting) { snprintf(tok, sizeof tok, "%u wait", n.waiting); addToken(buf, sizeof buf, len, footRoom, tok); }
  if (n.error) { snprintf(tok, sizeof tok, "%u err", n.error); addToken(buf, sizeof buf, len, footRoom, tok); }
  if (n.running) { snprintf(tok, sizeof tok, "%u run", n.running); addToken(buf, sizeof buf, len, footRoom, tok); }
  c.text(kBigReg, fx, 136, buf, waiting ? col::bg : col::ink, lw - (fx - pad) - markW);
  if (markW) drawNetMark(c, heroW - pad - 16, 158, waiting ? col::bg : col::dim);
}

// The waiting hero the board may answer: the question, and the options along the bottom.
static inline void prompt(Canvas& c, const Input& in) {
  using namespace layout;
  const Agent& a = in.fleet->agents[in.hero];
  const int lw = heroW - 2 * pad;
  c.rect(0, 0, heroW, kStripY, col::accent);
  c.rect(0, kStripY, c.w, c.h - kStripY, col::accent);
  heroHead(c, a, in, col::bg);

  // The question, then what it is about (a command, a file), each one line.
  const char* first = a.waitTitle[0] ? a.waitTitle : a.waitDetail[0] ? a.waitDetail : "needs input";
  c.text(kBold, pad, 64, first, col::bg, lw);
  if (a.waitTitle[0] && a.waitDetail[0]) c.text(kReg, pad, 86, a.waitDetail, col::bg, lw);

  Button b[kMaxButtons];
  const int n = buttons(a, in.ui, b);
  const int pressed = in.ui && in.ui->down ? in.ui->pressed - kHitButton : -1;
  for (int i = 0; i < n; i++) {
    const bool primary = b[i].opt >= 0 && a.opts[b[i].opt].primary;
    const bool down = i == pressed;
    const uint16_t fill = down ? col::dim : primary ? col::ink : col::bg;
    const uint16_t ink = down || primary ? col::bg : col::ink;
    c.rect(b[i].x, b[i].y, b[i].w, b[i].h, fill);
    const int tx = b[i].x + 5, tw = b[i].w - 10;
    if (b[i].opt < 0) {                                  // the options after these, or back to the first
      char more[24];
      const int left = a.optCount - (optFirstFor(in.ui, a) + n - 1);
      if (left > 0) snprintf(more, sizeof more, "+%d more", left);
      else snprintf(more, sizeof more, "back");
      c.text(kBold, tx, b[i].y + (b[i].h - kBold.cellH) / 2, more, ink, tw);
    } else {
      const char* label = a.opts[b[i].opt].label[0] ? a.opts[b[i].opt].label : a.opts[b[i].opt].key;
      const bool two = (int)utf8Len(label) > tw / kBold.cellW;
      wrap2(c, kBold, tx, b[i].y + (two ? 6 : (b[i].h - kBold.cellH) / 2), tw, label, ink);
    }
  }
}

// Up from the bottom edge as it starts, back down as it ends; w px wide from the left.
static inline void toastBox(Canvas& c, const Ui& ui, uint32_t ms, int w = layout::heroW) {
  const char* text = ui.toast;
  const uint32_t in = ms - ui.toastStart, out = ui.toastUntil - ms;
  const uint32_t edge = in < out ? in : out;
  const int h = 30;
  int shown = h;
  if (edge < kToastSlideMs) {
    const float f = 1.0f - (float)edge / kToastSlideMs;
    shown = (int)(h * (1.0f - f * f));                    // ease out
  }
  if (shown <= 0) return;
  const int y = c.h - shown;
  c.rect(0, y, w, h, col::ink);
  const int tw = utf8Len(text) * kBold.cellW;
  c.text(kBold, tw < w ? (w - tw) / 2 : 0, y + 5, text, col::bg, w);
}

}  // namespace detail

// Everything but the toast. Returns true when it drew the hero (the mini-board beside it).
static inline bool drawBody(Canvas& c, const Input& in);

static inline void draw(Canvas& c, const Input& in) {
  const bool withBoard = drawBody(c, in);
  // A toast on any screen ("trying Wi-Fi…" can come while nothing else is shown).
  if (in.ui && toastShown(*in.ui, in.ms)) detail::toastBox(c, *in.ui, in.ms, withBoard ? layout::heroW : c.w);
}

static inline bool drawBody(Canvas& c, const Input& in) {
  using namespace detail;
  c.fill(col::bg);
  const uint32_t since = in.ui && in.ui->linkSince ? in.ms - in.ui->linkSince : in.ms;   // in this link state
  switch (in.link) {
    case LinkState::Connecting:      // the critter runs from side to side while it waits for the deck
      if (onWifi(in)) {              // joining, finding the deck, or why not (net_model.h's words)
        const NetNotice n = netNotice(*in.net);
        notice(c, n.problem ? Pose::Err : Pose::Run, n.problem ? col::accent : col::ink, n.title,
               n.line1 ? n.line1 : "", in.ms, n.line2, n.problem ? 0 : pingpong(since, 160, 6000) - 80);
      } else {
        notice(c, Pose::Run, col::ink, "connecting", "waiting for the deck on USB", in.ms, "agentctl link add <port>",
               pingpong(since, 160, 6000) - 80);
      }
      return false;
    case LinkState::Unapproved:      // and looks one way, then the other
      notice(c, Pose::Wait, col::accent, "waiting for approval", "agentctl devices approve", in.ms,
             in.device && in.device[0] ? in.device : "<name>", (since / 1200) % 2 ? 10 : -10);
      return false;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0 || in.hero < 0 || in.hero >= in.fleet->count) {
    if (in.link == LinkState::Lost)
      notice(c, since >= kDozeAfterMs ? Pose::Idle : Pose::Exit, col::dim, "deck offline", "waiting for agentctl", in.ms);
    else emptyScene(c, in, since);
    return false;
  }
  const Agent& h = in.fleet->agents[in.hero];
  // The prompt only while the deck is there to take an answer: offline, the hero says so.
  const bool isPrompt = in.link == LinkState::Live && promptShown(h, in.scope, in.ui);
  const bool waiting = h.status == Status::Waiting;
  if (isPrompt) prompt(c, in);
  else hero(c, in);
  // A new hero slides in; the mini-board stays put.
  const int dx = slideOffset(in.ui, in.ms);
  const uint16_t under = waiting ? col::accent : col::bg;
  if (isPrompt) {
    shift(c, 0, 0, layout::heroW, kStripY, dx, under);
    shift(c, 0, kStripY, c.w, c.h - kStripY, dx, under);
  } else {
    shift(c, 0, 0, layout::heroW, c.h, dx, under);
  }
  // Someone waits: the frame marches round the prompt, or round the hero.
  if (isPrompt) ants(c, 0, 0, c.w, c.h, 3, in.ms);
  else if (waiting) ants(c, 0, 0, layout::heroW, c.h, 3, in.ms);
  const int pressed = in.ui && in.ui->down && in.ui->pressed >= 0 && in.ui->pressed < kHitButton ? in.ui->pressed : -1;
  miniBoard(c, *in.fleet, in.hero, in.nowEpoch, in.ms, miniCellsShown(isPrompt), pressed);
  return true;
}

// True while something on screen moves fast enough to want a frame every
// 40 ms: a slide, a toast, a growing bar, a critter at work, a marching frame,
// stripes, the empty screen's walkers or a notice's critter. A board of idle
// agents only needs the 200 ms clock (their critters doze).
static inline bool busy(const Input& in) {
  const uint32_t since = in.ui && in.ui->linkSince ? in.ms - in.ui->linkSince : 0;
  if (in.link == LinkState::Connecting || in.link == LinkState::Unapproved) return true;
  if (!in.fleet || in.fleet->count == 0 || in.hero < 0 || in.hero >= in.fleet->count)
    return since < (in.link == LinkState::Lost ? kDozeAfterMs : kNapAfterMs);
  if (in.ui && (slideOffset(in.ui, in.ms) || toastShown(*in.ui, in.ms) || in.ms - in.ui->heroSince < kBarGrowMs))
    return true;
  auto lively = [](const Agent& a) { return a.status != Status::Idle && a.status != Status::Exited ? true : hungry(a); };
  if (lively(in.fleet->agents[in.hero])) return true;
  for (int i = 0; i < in.fleet->count && i < layout::miniCells; i++)
    if (lively(in.fleet->agents[i])) return true;
  return false;
}

// ---------------------------------------------------------------- input
static inline DeckCommand answerCmd(const Agent& a, int opt) {
  DeckCommand c = commandFor(DeckCmd::Answer, a);
  snprintf(c.prompt, sizeof c.prompt, "%s", a.promptId);
  snprintf(c.key, sizeof c.key, "%s", a.opts[opt].key);
  return c;
}

static inline int primaryOpt(const Agent& a) {
  for (int i = 0; i < a.optCount; i++)
    if (a.opts[i].primary) return i;
  return a.optCount ? 0 : -1;
}

static inline DeckCommand denied(Ui& ui, uint32_t ms) {
  toast(ui, "view only: needs scope act", ms, 2500);
  return DeckCommand();
}

// A sample from the touch screen (down, with where) or its release (down false).
// Returns the command to send, if any.
static inline DeckCommand touch(Ui& ui, const Fleet& f, ViewState& v, bool down, int x, int y, DeckScope scope,
                                uint32_t ms) {
  const int hero = resolveHero(f, v, ms);
  if (down) {
    if (!ui.down) {
      ui.down = true;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ms;
      ui.holdFired = false;
      ui.pressed = hitTest(f, hero, &ui, scope, x, y);
      snprintf(ui.pressAgent, sizeof ui.pressAgent, "%s", hero >= 0 ? f.agents[hero].id : "");
      snprintf(ui.pressPrompt, sizeof ui.pressPrompt, "%s", hero >= 0 ? f.agents[hero].promptId : "");
    } else {
      ui.x = x;
      ui.y = y;
      if (abs(x - ui.x0) > kTapSlop || abs(y - ui.y0) > kTapSlop) ui.pressed = -1;   // a swipe, not a press
      // Held on the agent whose prompt is hidden: show it again, at once.
      if (!ui.holdFired && ui.pressed == kHitHero && ms - ui.downMs >= kTouchHoldMs && hero >= 0 &&
          promptView(f.agents[hero], scope) && promptHidden(&ui, f.agents[hero])) {
        ui.hiddenAgent[0] = ui.hiddenPrompt[0] = '\0';
        ui.holdFired = true;
        ui.pressed = -1;
      }
    }
    return DeckCommand();
  }
  if (!ui.down) return DeckCommand();
  ui.down = false;
  if (ui.holdFired) { ui.pressed = -1; return DeckCommand(); }
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int hit = ui.pressed;
  ui.pressed = -1;

  if (abs(dx) >= kSwipeMin && abs(dx) > 2 * abs(dy)) {      // swipe: the board follows the finger
    if (dx < 0) pressNext(f, v, ms);
    else pressPrev(f, v, ms);
    ui.pendingDir = dx < 0 ? 1 : -1;                         // the new hero comes from where the finger went
    return DeckCommand();
  }
  if (hit < 0) return DeckCommand();

  if (hit < kHitButton) {                                    // a mini-board cell
    const bool prompt = hero >= 0 && promptShown(f.agents[hero], scope, &ui);
    const int cells = miniCellsShown(prompt);
    if (f.total > (unsigned)cells && hit == cells - 1) { pressNext(f, v, ms); ui.pendingDir = 1; }   // "+N"
    else { ui.pendingDir = hit < hero ? -1 : 1; pressPin(f, v, ms, hit); }
    return DeckCommand();
  }
  if (hero < 0) return DeckCommand();
  const Agent& a = f.agents[hero];
  // The hero or its prompt changed under the finger (a new waiter, an answer
  // from elsewhere): the press was for something no longer on screen.
  if (strcmp(a.id, ui.pressAgent) != 0 || strcmp(a.promptId, ui.pressPrompt) != 0) {
    if (hit >= kHitButton && hit != kHitHero) toast(ui, "the prompt changed", ms, 2000);
    return DeckCommand();
  }
  if (hit == kHitHero) {
    if (promptShown(a, scope, &ui)) {                        // put the prompt away, to look around
      snprintf(ui.hiddenAgent, sizeof ui.hiddenAgent, "%s", a.id);
      snprintf(ui.hiddenPrompt, sizeof ui.hiddenPrompt, "%s", a.promptId);
      toast(ui, "hold the agent to answer", ms, 2000);
      return DeckCommand();
    }
    if (!canAct(scope)) return denied(ui, ms);
    toast(ui, "focus", ms);
    return commandFor(DeckCmd::Focus, a);
  }
  // A button of the prompt view (only there with scope act).
  if (!promptShown(a, scope, &ui)) return DeckCommand();
  Button b[kMaxButtons];
  const int n = buttons(a, &ui, b);
  const int i = hit - kHitButton;
  if (i >= n) return DeckCommand();
  if (b[i].opt < 0) {                                        // "+N": the next options
    snprintf(ui.optPrompt, sizeof ui.optPrompt, "%s", a.promptId);
    const int shown = n - 1;
    ui.optFirst = optFirstFor(&ui, a) + shown;
    if (ui.optFirst >= a.optCount) ui.optFirst = 0;
    return DeckCommand();
  }
  toast(ui, "sent", ms);
  return answerCmd(a, b[i].opt);
}

// A button: a short press (longPress false, on release) or a hold that just
// passed kStopHoldMs (longPress true, while still down).
static inline DeckCommand key(Ui& ui, const Fleet& f, ViewState& v, Key k, bool longPress, DeckScope scope,
                              uint32_t ms) {
  if (!longPress) {
    if (k == Key::Next) pressNext(f, v, ms);
    else pressPrev(f, v, ms);
    ui.pendingDir = k == Key::Next ? 1 : -1;
    return DeckCommand();
  }
  const int hero = resolveHero(f, v, ms);
  if (hero < 0) return DeckCommand();
  if (!canAct(scope)) return denied(ui, ms);
  const Agent& a = f.agents[hero];
  if (k == Key::Prev) {
    toast(ui, "interrupted", ms);
    return commandFor(DeckCmd::Interrupt, a);
  }
  // Answer only a prompt that is on screen: a hidden one is never answered blind.
  const int opt = promptShown(a, scope, &ui) ? primaryOpt(a) : -1;
  if (opt >= 0) {
    toast(ui, "sent", ms);
    return answerCmd(a, opt);
  }
  toast(ui, "focus", ms);
  return commandFor(DeckCmd::Focus, a);
}

// The firmware sent c to the deck as seq (0: not sent).
static inline void sent(Ui& ui, const DeckCommand& c, uint32_t seq) {
  ui.cmdSeq = seq;
  ui.cmdKind = c.cmd;
}

// The deck's Result for a command. Only failures change what the toast says.
static inline void result(Ui& ui, uint32_t seq, uint32_t code, uint32_t ms) {
  if (!seq || seq != ui.cmdSeq) return;
  ui.cmdSeq = 0;
  if (code >= 200 && code < 300) return;
  if (code == 403) toast(ui, "not allowed: scope", ms, 2500);
  else if (code == 409) toast(ui, "the prompt changed", ms, 2500);
  // The deck couldn't do it on this computer (e.g. no way to raise that terminal).
  else if (ui.cmdKind == DeckCmd::Focus) toast(ui, "can't open its terminal", ms, 2500);
  else if (ui.cmdKind == DeckCmd::Interrupt) toast(ui, "couldn't interrupt", ms, 2500);
  else {
    char b[24];
    snprintf(b, sizeof b, "failed (%u)", (unsigned)code);
    toast(ui, b, ms, 2500);
  }
}

}  // namespace tds3
