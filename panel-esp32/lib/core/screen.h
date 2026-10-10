#pragma once
// The whole 320 x 172 frame. This is the only file that knows where things
// are on the screen: a hero panel on the left (one agent, with details) and
// a 2 x 4 mini-board of the fleet on the right.
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "model.h"
#include "sprites.h"
#include "view.h"

constexpr int kScreenW = 320, kScreenH = 172;

namespace col {   // the agents-deck web palette
constexpr uint16_t bg = rgb565(0x0A, 0x0A, 0x0A);
constexpr uint16_t ink = rgb565(0xE8, 0xE4, 0xDC);
constexpr uint16_t dim = rgb565(0x7A, 0x77, 0x6F);
constexpr uint16_t accent = rgb565(0xFF, 0x4D, 0x00);
constexpr uint16_t line = rgb565(0x26, 0x26, 0x22);
}  // namespace col

enum class LinkState : uint8_t { NoWifi, Resolving, Connecting, PairError, Live, Lost };

struct ScreenInput {
  const Fleet* fleet;   // while the link is Lost this is the last fleet received
  int hero;             // index into fleet->agents, -1 when there is none
  LinkState link;
  const char* host;     // daemon host name, shown while looking for it
  uint32_t ms;          // animation clock
  int64_t nowEpoch;     // the daemon's time, now
  bool held;            // the button has pinned the hero
};

namespace layout {
constexpr int heroW = 236;                 // hero panel: x 0..235
constexpr int pad = 10;
constexpr int textX = 90;                  // title column, right of the sprite
constexpr int miniX = 240, miniY = 6;
constexpr int cellW = 38, cellH = 40, stepX = 40, stepY = 41;
constexpr int miniCells = 8;
}  // namespace layout

namespace screen_detail {

struct Tone { uint16_t body, cut; };

// Sprite colours by status. A waiting agent sits on an accent background.
static inline Tone toneOf(const Agent& a) {
  switch (a.status) {
    case Status::Waiting: return {col::bg, col::accent};
    case Status::Running: return {col::ink, col::bg};
    case Status::Error:   return {col::accent, col::bg};
    case Status::Idle:    return {a.unseen ? col::accent : col::dim, col::bg};
    default:              return {col::dim, col::bg};
  }
}

static inline void centered(Canvas& c, const Font& f, int y, const char* s, uint16_t color) {
  const int w = utf8Len(s) * f.cellW;
  c.text(f, w < kScreenW ? (kScreenW - w) / 2 : 0, y, s, color, kScreenW);
}

static inline void notice(Canvas& c, Pose pose, uint16_t body, const char* title, const char* hint, uint32_t ms) {
  drawSprite(c, kScreenW / 2 - 28, 26, 4, Species::Claude, pose, body, col::bg, ms, true);
  centered(c, FONT_L, 92, title, col::ink);
  centered(c, FONT_S, 120, hint, col::dim);
}

static inline void dashedFrame(Canvas& c, int x, int y, int w, int h, uint16_t color) {
  for (int i = 0; i < w; i += 5) { c.rect(x + i, y, 2, 1, color); c.rect(x + i, y + h - 1, 2, 1, color); }
  for (int i = 0; i < h; i += 5) { c.rect(x, y + i, 1, 2, color); c.rect(x + w - 1, y + i, 1, 2, color); }
}

static inline void hero(Canvas& c, const ScreenInput& in) {
  using namespace layout;
  const Fleet& f = *in.fleet;
  const Agent& a = f.agents[in.hero];
  const bool waiting = a.status == Status::Waiting;
  const uint16_t fg = waiting ? col::bg : col::ink;
  const uint16_t soft = waiting ? col::bg : col::dim;
  char buf[112];

  if (waiting) c.rect(0, 0, heroW, kScreenH, col::accent);
  if (a.status == Status::Error) c.frame(0, 0, heroW, kScreenH, col::accent, 2);

  const Tone tone = toneOf(a);
  // 4 px right of the text margin: a cap's brim reaches 3 units left of the body.
  drawSprite(c, pad + 4, 8, 4, a.species, poseOf(a, in.nowEpoch), tone.body, tone.cut, in.ms, true, a.hat, a.hatColor);

  // Right of the sprite: name, status, model.
  const int tw = heroW - 4 - textX;
  c.text(FONT_L, textX, 6, a.title[0] ? a.title : a.id, fg, tw);
  statusLine(a, in.nowEpoch, buf, sizeof buf);
  const bool loud = a.status == Status::Running || a.status == Status::Error || (a.status == Status::Idle && a.unseen);
  c.text(FONT_S, textX, 29, buf, waiting ? col::bg : loud ? col::accent : col::dim, tw);
  if (a.subagents) snprintf(buf, sizeof buf, "%s" DECK_DOT "%u sub", a.model, a.subagents);
  else snprintf(buf, sizeof buf, "%s", a.model);
  c.text(FONT_S, textX, 45, buf, soft, tw);

  // Two full-width lines: what it is doing, and what the session is about.
  const int lw = heroW - 2 * pad;
  activityLine(a, buf, sizeof buf);
  if (a.status == Status::Running && a.actTool[0] && a.actDetail[0]) {
    // A long path keeps its file name: cut the start, not the end.
    const int used = c.text(FONT_S, pad, 68, a.actTool, fg, lw) + FONT_S.cellW;
    const bool path = strchr(a.actDetail, '/') && !strchr(a.actDetail, ' ');
    c.text(FONT_S, pad + used, 68, a.actDetail, fg, lw - used, path);
    c.text(FONT_S, pad, 85, a.aiTitle, soft, lw);
  } else if (buf[0]) {
    c.text(FONT_S, pad, 68, buf, fg, lw);
    const bool both = waiting && a.waitDetail[0] && a.waitTitle[0];
    c.text(FONT_S, pad, 85, both ? a.waitTitle : a.aiTitle, soft, lw);
  } else {
    c.text(FONT_S, pad, 68, a.aiTitle, fg, lw);
  }

  // Context window.
  if (a.ctxWindow) {
    const int pct = ctxPct(a), barW = 168;
    c.rect(pad, 111, barW, 6, waiting ? blend565(col::accent, col::bg, 5) : col::line);
    c.rect(pad, 111, barW * pct / 100, 6, waiting ? col::bg : pct >= 85 ? col::accent : col::ink);
    snprintf(buf, sizeof buf, "%d%%", pct);
    c.text(FONT_S, pad + barW + 8, 105, buf, fg);
  }

  // Turns and cost, each only when the daemon reports one (it sends no cost for Codex threads).
  if (a.turns || a.costUsd > 0) {
    const char* unit = a.turns == 1 ? "turn" : "turns";
    if (!a.turns) snprintf(buf, sizeof buf, "$%.2f", (double)a.costUsd);
    else if (a.costUsd <= 0) snprintf(buf, sizeof buf, "%u %s", a.turns, unit);
    else snprintf(buf, sizeof buf, "%u %s" DECK_DOT "$%.2f", a.turns, unit, (double)a.costUsd);
    c.text(FONT_S, pad, 124, buf, soft, lw);
  }

  // Footer: position in the fleet and what the fleet is doing.
  int fx = pad;
  if (in.held) { c.rect(pad, 156, 5, 5, fg); fx += 9; }   // pinned by the button
  if (in.link == LinkState::Lost) {
    c.text(FONT_S, fx, 150, "link lost", waiting ? col::bg : col::accent, lw);
    return;
  }
  const Counts n = countFleet(f);
  int len = snprintf(buf, sizeof buf, "%d/%u", in.hero + 1, (unsigned)f.total);
  if (n.running) len += snprintf(buf + len, sizeof buf - len, DECK_DOT "%u run", n.running);
  if (n.waiting) len += snprintf(buf + len, sizeof buf - len, DECK_DOT "%u wait", n.waiting);
  if (n.error) len += snprintf(buf + len, sizeof buf - len, DECK_DOT "%u err", n.error);
  c.text(FONT_S, fx, 150, buf, soft, lw - (fx - pad));
}

static inline void miniBoard(Canvas& c, const ScreenInput& in) {
  using namespace layout;
  const Fleet& f = *in.fleet;
  c.rect(heroW, 6, 1, kScreenH - 12, col::line);
  const bool overflow = f.total > miniCells;
  for (int i = 0; i < miniCells; i++) {
    const int cx = miniX + (i % 2) * stepX, cy = miniY + (i / 2) * stepY;
    if (overflow && i == miniCells - 1) {
      char more[8];
      snprintf(more, sizeof more, "+%u", (unsigned)(f.total - (miniCells - 1)));
      c.frame(cx, cy, cellW, cellH, col::line);
      c.text(FONT_S, cx + (cellW - utf8Len(more) * FONT_S.cellW) / 2, cy + 12, more, col::dim);
      continue;
    }
    if (i >= f.count) {
      dashedFrame(c, cx, cy, cellW, cellH, col::line);
      continue;
    }
    const Agent& a = f.agents[i];
    if (a.status == Status::Waiting) c.rect(cx, cy, cellW, cellH, col::accent);
    else c.frame(cx, cy, cellW, cellH, a.status == Status::Error ? col::accent : col::line);
    if (i == in.hero) c.frame(cx, cy, cellW, cellH, col::ink, 2);
    const Tone tone = toneOf(a);
    // Room above the head for the hat: up to 5 rows at 2 px.
    // One pixel right of centre, so a cap's brim stays inside the cell.
    drawSprite(c, cx + 6, cy + 8, 2, a.species, poseOf(a, in.nowEpoch), tone.body, tone.cut, in.ms, false,
               a.hat, a.hatColor);
  }
}

}  // namespace screen_detail

static inline void drawScreen(Canvas& c, const ScreenInput& in) {
  using namespace screen_detail;
  c.fill(col::bg);
  char buf[64];
  switch (in.link) {
    case LinkState::NoWifi:
      notice(c, Pose::Exit, col::dim, "no Wi-Fi", "waiting for the network", in.ms);
      return;
    case LinkState::Resolving:
      snprintf(buf, sizeof buf, "%s.local", in.host ? in.host : "?");
      notice(c, Pose::Think, col::ink, "looking for the Mac", buf, in.ms);
      return;
    case LinkState::Connecting:
      snprintf(buf, sizeof buf, "%s.local", in.host ? in.host : "?");
      notice(c, Pose::Run, col::ink, "connecting", buf, in.ms);
      return;
    case LinkState::PairError:
      notice(c, Pose::Err, col::accent, "pairing error", "run tools/make_config.py", in.ms);
      return;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0 || in.hero < 0 || in.hero >= in.fleet->count) {
    if (in.link == LinkState::Lost) notice(c, Pose::Exit, col::dim, "link lost", "reconnecting", in.ms);
    else notice(c, Pose::Idle, col::dim, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : "agentctl new", in.ms);
    return;
  }
  hero(c, in);
  miniBoard(c, in);
}
