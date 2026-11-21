#pragma once
// The board's own panel: pulled down from the top edge of any screen, over
// whatever the screen shows (the list boards from their header, the 4" from
// the top strip of its grid, controlEdgeTouch).
//
//   header   the board's name on the deck; under it its Bluetooth name and type
//   switches Wi-Fi and Bluetooth, two tiles: bright when on, outlined when off.
//            A tap flips one (the firmware keeps it across restarts)
//   link     how the board reaches the deck now, and the deck's preference
//   facts    firmware, id, what the deck shows, how long the board has been up
//   restart  held for 1 s, like stop on an agent's card
//   handle   swipe up (or BOOT) to close
//
// It follows the finger while it is pulled and settles open or shut on
// release. Pure C++ (no Arduino): included by layout_square.h after its
// drawing helpers, drawn by the tests and sim/render exactly as on a board.


// The panel's numbers for a screen of w x h.
struct ControlGeom {
  int w, h, m, pad, gap, r;
  int headY, nameH;
  Rect wifi, ble, magic, restart;   // a 2 x 2 grid; magic: h 0 on a board without a motion sensor
  Rect link;                        // the small line: how it reaches the deck
  int versionY;                     // the firmware version, at the bottom
  int handleY;
  int commitPx;                // kControlCommitMm on this glass
  const Font* name;
  const Font* label;
  const Font* small;
};

// diagIn: the glass's diagonal in inches (0: unknown, then a fifth of the height).
static inline ControlGeom controlGeom(int w, int h, int m, float diagIn = 0, bool imu = false) {
  ControlGeom g{};
  const float ppi = diagIn > 0 ? sqrtf((float)(w * w + h * h)) / diagIn : 0;
  g.commitPx = ppi > 0 ? (int)lroundf(kControlCommitMm / 25.4f * ppi) : h / 5;
  const bool big = h >= 560;   // the T4-S3
  g.w = w; g.h = h; g.m = m;
  g.pad = big ? 14 : 10;
  g.gap = 8;
  g.r = big ? 12 : 10;
  g.name = big ? &FONT_Q28B : &FONT_Q24B;
  g.label = big ? &FONT_Q24B : &FONT_Q22;
  g.small = big ? &FONT_Q16 : &FONT_L;
  g.headY = m + (big ? 18 : 12);
  g.nameH = g.name->cellH + g.small->cellH + 2;
  int y = g.headY + g.nameH + (big ? 14 : 8);
  const int tileW = (w - 2 * m - g.gap) / 2, tileH = big ? 150 : 108;
  g.wifi = Rect{m, y, tileW, tileH};
  g.ble = Rect{m + tileW + g.gap, y, tileW, tileH};
  y += tileH + g.gap;
  // The second row: the magic trick and restart, the same size; restart alone
  // (at the left) on a board without a motion sensor.
  if (imu) {
    g.magic = Rect{m, y, tileW, tileH};
    g.restart = Rect{m + tileW + g.gap, y, tileW, tileH};
  } else {
    g.restart = Rect{m, y, tileW, tileH};
  }
  y += tileH + (big ? 16 : 10);
  g.link = Rect{m, y, w - 2 * m, big ? 40 : 32};
  g.handleY = h - m - (big ? 10 : 8);
  g.versionY = g.handleY - (big ? 12 : 8) - g.small->cellH;
  return g;
}

static inline ControlHit controlHit(const ControlGeom& g, int x, int y) {
  if (g.wifi.contains(x, y)) return ControlHit::Wifi;
  if (g.ble.contains(x, y)) return ControlHit::Ble;
  if (g.magic.h && g.magic.contains(x, y)) return ControlHit::Magic;
  if (g.restart.contains(x, y)) return ControlHit::Restart;
  return ControlHit::None;
}

static inline void controlSettle(ControlUi& u, bool open, uint32_t ms) {
  u.from = u.open;
  u.to = open ? 1.f : 0.f;
  u.animStart = ms;
  u.animating = u.from != u.to;
  if (!u.animating) u.open = u.to;
}

static inline void controlClose(ControlUi& u, uint32_t ms) {
  u.down = u.pulling = false;
  u.pressed = ControlHit::None;
  controlSettle(u, false, ms);
}

// One touch sample while the panel is shown, or while a pull from the top
// edge may begin (`mayBegin`: the finger went down in the top strip of
// home). True: the panel took the touch (home must not see it).
static inline bool controlTouch(ControlUi& u, const ControlGeom& g, bool touching, int x, int y,
                                uint32_t ms, bool mayBegin) {
  if (touching) {
    if (!u.down) {
      if (!controlShown(u) && !mayBegin) return false;
      u.down = true;
      u.pulling = u.moved = u.holdFired = false;
      u.animating = false;
      u.y0 = u.lastY = y;
      u.open0 = u.open;
      u.velocity = 0;
      u.downMs = u.lastMs = ms;
      u.pressed = u.open > 0.99f ? controlHit(g, x, y) : ControlHit::None;
      return true;
    }
    const int dy = y - u.y0;
    if (!u.pulling && (dy > kControlSlop || dy < -kControlSlop)) {
      u.pulling = u.moved = true;
      u.pressed = ControlHit::None;
    }
    if (u.pulling) {
      float o = u.open0 + (float)dy * kControlPullGain / (float)g.h;
      if (o < 0) o = 0;
      if (o > 1) o = 1;
      const uint32_t dt = ms - u.lastMs;
      if (dt > 0) u.velocity = 0.6f * u.velocity + 0.4f * ((float)(y - u.lastY) * kControlPullGain / (float)g.h / (float)dt);
      u.open = o;
    } else if (u.pressed == ControlHit::Restart && !u.holdFired && ms - u.downMs >= kControlHoldMs) {
      u.holdFired = true;
      u.act = ControlAct::Restart;
    }
    u.lastY = y;
    u.lastMs = ms;
    return true;
  }
  if (!u.down) return controlShown(u);
  u.down = false;
  if (u.pulling) {
    u.pulling = false;
    const bool fromOpen = u.open0 >= 0.5f;
    const float moved = (u.open - u.open0) * (float)g.h / kControlPullGain;   // finger travel in pixels, down positive
    const bool open = u.velocity > 0.0008f    ? true
                      : u.velocity < -0.0008f ? false
                      : fromOpen              ? moved > -(float)g.commitPx
                                              : moved > (float)g.commitPx;
    controlSettle(u, open, ms);
  } else {
    if (u.open < 0.99f) controlSettle(u, false, ms);   // a tap in the top strip: nothing to open
    else if (u.pressed == ControlHit::Wifi) u.act = ControlAct::Wifi;
    else if (u.pressed == ControlHit::Ble) u.act = ControlAct::Ble;
    else if (u.pressed == ControlHit::Magic) u.act = ControlAct::Magic;
  }
  u.pressed = ControlHit::None;
  return true;
}

// A grid home has tiles right up to the top edge, so a touch there stays the
// screen's (a tap opens the tile) until it moves down, more down than across,
// by kControlEdgeSlop: then the panel takes the finger from where it went down
// and follows it. Up or sideways first: it stays the screen's (a page swipe).
// True: the panel took the touch (the screen must forget the finger).
constexpr int kControlEdgeSlop = 12;
static inline bool controlEdgeTouch(ControlUi& u, const ControlGeom& g, bool touching, int x, int y, uint32_t ms,
                                    int strip) {
  if (u.down || controlShown(u)) {
    u.edge = 2;
    return controlTouch(u, g, touching, x, y, ms, false);
  }
  if (!touching) {
    u.edge = 0;
    return false;
  }
  if (u.edge == 0) {
    u.edge = y < strip ? 1 : 2;
    u.ex = x;
    u.ey = y;
    return false;
  }
  if (u.edge != 1) return false;
  const int dx = x - u.ex, dy = y - u.ey, adx = dx < 0 ? -dx : dx;
  if (dy < -kControlEdgeSlop || (adx > kControlEdgeSlop && swipeAcross(adx, dy > 0 ? dy : 0))) {
    u.edge = 2;                                    // up or sideways: the screen's
    return false;
  }
  if (dy <= kControlEdgeSlop || swipeAcross(adx, dy)) return false;   // not far enough yet
  controlTouch(u, g, true, u.ex, u.ey, ms, true);  // down where the finger went down, then here
  controlTouch(u, g, true, x, y, ms, false);
  return true;
}

// The settle animation; true while it moves or the restart hold fills.
static inline bool controlTick(ControlUi& u, uint32_t ms) {
  if (u.animating) {
    const uint32_t t = ms - u.animStart;
    if (t >= kControlSettleMs) {
      u.open = u.to;
      u.animating = false;
    } else {
      const float k = (float)t / (float)kControlSettleMs, e = 1 - (1 - k) * (1 - k) * (1 - k);
      u.open = u.from + (u.to - u.from) * e;
    }
    return true;
  }
  return u.pulling || (u.down && u.pressed == ControlHit::Restart);
}

namespace control_detail {

static const char* const kIconWifi[] = {"..#####..", ".#.....#.", "#..###..#", "..#...#..", "....#....", ".........", "....#...."};
// The USB mark: the trident with a round end, a square end and a dot at the root.
static const char* const kIconUsb[] = {".....#.....", "....###....", "...#####...", ".....#..###", ".##..#..###", ".##..#...#.", "..#..#...#.",
                                       "...#.#..#..", "....###....", ".....#.....", "....###....", "....###...."};
// A wand with a spark at its tip.
static const char* const kIconWand[] = {"......#..", "....#.#.#", ".....###.", "....##.##", ".....###.", "....##.#.", "...##....", "..##.....", ".##......", "##......."};
// A turning arrow: restart.
static const char* const kIconRestart[] = {"..####.#.", ".#....##.", "#....###.", "#........", "#........", "#.......#", ".#.....#.", "..#####.."};
static const char* const kIconBle[] = {"...#...", "...##..", "#..#.#.", ".#.##..", "..##...", ".#.##..", "#..#.#.", "...##..", "...#..."};

static inline void pixels(Canvas& c, const char* const* rows, int n, int x, int y, int cell, uint16_t col) {
  for (int r = 0; r < n; r++)
    for (int k = 0; rows[r][k]; k++)
      if (rows[r][k] == '#') c.rect(x + k * cell, y + r * cell, cell, cell, col);
}

// A switch tile: bright when on, an outline when off; pressed, the other way round.
static inline void switchTile(Canvas& c, const ControlGeom& g, const Theme& T, const Rect& r, int oy, const char* const* icon,
                              int iconRows, const char* label, bool capable, bool on, bool pressed, const char* status) {
  using square_detail::roundFrame;
  using square_detail::roundRect;
  const bool lit = capable && (on != pressed);
  const int y = r.y + oy;
  const uint16_t ink = lit ? T.bg : capable ? T.ink : T.dim, sub = lit ? T.line : T.dim;
  if (lit) roundRect(c, r.x, y, r.w, r.h, g.r, T.ink);
  else {
    roundRect(c, r.x, y, r.w, r.h, g.r, T.panel);
    roundFrame(c, r.x, y, r.w, r.h, g.r, 2, T.ringTrack);
  }
  const int cell = g.h >= 560 ? 4 : 3;
  pixels(c, icon, iconRows, r.x + g.pad, y + g.pad, cell, ink);
  const char* state = !capable ? "none" : on ? "on" : "off";
  c.text(*g.small, r.x + r.w - g.pad - textW(*g.small, state), y + g.pad, state, sub);
  const int ty = y + r.h - g.pad - g.small->cellH - g.label->cellH + 2;
  c.text(*g.label, r.x + g.pad, ty, label, ink, r.w - 2 * g.pad);
  c.text(*g.small, r.x + g.pad, ty + g.label->cellH - 2, status, sub, r.w - 2 * g.pad);
}

}  // namespace control_detail

// The panel, `open` of the way down, over what is on the canvas.
static inline void controlDraw(Canvas& c, const ControlGeom& g, const Theme& T, const ControlInfo& info, const ControlUi& u,
                               uint32_t ms) {
  using namespace control_detail;
  using square_detail::roundFrame;
  using square_detail::roundRect;
  if (!controlShown(u)) return;
  const int oy = -(int)((1.f - u.open) * (float)g.h + 0.5f);   // the panel's top edge
  c.rect(0, oy, g.w, g.h, T.bg);
  // A line under the panel while it is on its way, so its edge reads over home.
  if (u.open < 0.999f) c.rect(0, oy + g.h, g.w, 2, T.ringTrack);

  // Header.
  const int x0 = g.m + g.pad;
  c.text(*g.name, x0, g.headY + oy, info.name[0] ? info.name : "this board", T.ink, g.w - 2 * x0);
  // Under it: what the board is called over Bluetooth, the name the Mac shows when adding it.
  c.text(*g.small, x0, g.headY + oy + g.name->cellH + 2, info.bleName[0] ? info.bleName : info.board, T.dim, g.w - 2 * x0);

  // The switches.
  char st[48];
  if (!info.wifiCapable) snprintf(st, sizeof st, "not in this build");
  else if (!info.wifiOn) snprintf(st, sizeof st, "tap to turn on");
  else if (!info.ssid[0]) snprintf(st, sizeof st, "no network yet");
  else snprintf(st, sizeof st, "%s", info.ssid);
  switchTile(c, g, T, g.wifi, oy, kIconWifi, 7, "Wi-Fi", info.wifiCapable, info.wifiOn, u.pressed == ControlHit::Wifi, st);
  if (!info.bleCapable) snprintf(st, sizeof st, "not in this build");
  else if (!info.bleOn) snprintf(st, sizeof st, "tap to turn on");
  else if (info.via == NetVia::Ble && info.linked) snprintf(st, sizeof st, "in use");
  else snprintf(st, sizeof st, "%s", info.bleBonded ? "paired" : "not paired");
  switchTile(c, g, T, g.ble, oy, kIconBle, 9, "Bluetooth", info.bleCapable, info.bleOn, u.pressed == ControlHit::Ble, st);

  // The magic trick (a board with a motion sensor): a switch like the radios.
  if (g.magic.h)
    switchTile(c, g, T, g.magic, oy, kIconWand, 10, "Magic trick", true, info.magicOn, u.pressed == ControlHit::Magic,
               info.magicOn ? "knock to reveal" : "tap to arm");

  // Restart: a tile held for 1 s; a bar along its bottom fills while it is held.
  {
    const Rect& r = g.restart;
    const int y = r.y + oy;
    const bool held = u.down && u.pressed == ControlHit::Restart;
    int permille = 0;
    if (held) {
      const uint32_t t = ms - u.downMs;
      permille = t >= kControlHoldMs ? 1000 : (int)(t * 1000 / kControlHoldMs);
    }
    roundRect(c, r.x, y, r.w, r.h, g.r, T.panel);
    roundFrame(c, r.x, y, r.w, r.h, g.r, 2, held || u.holdFired ? T.accent : T.ringTrack);
    const int cell = g.h >= 560 ? 4 : 3;
    pixels(c, kIconRestart, 8, r.x + g.pad, y + g.pad, cell, held ? T.accent : T.ink);
    const int ty = y + r.h - g.pad - g.small->cellH - g.label->cellH + 2;
    c.text(*g.label, r.x + g.pad, ty, "Restart", T.ink, r.w - 2 * g.pad);
    c.text(*g.small, r.x + g.pad, ty + g.label->cellH - 2, u.holdFired ? "restarting" : held ? "keep holding" : "hold 1 s", T.dim,
           r.w - 2 * g.pad);
    if (permille > 0) c.rect(r.x + g.r, y + r.h - 6, (r.w - 2 * g.r) * permille / 1000, 4, T.accent);
  }

  // How it reaches the deck: a small line, the link's mark orange while a deck answers on it.
  {
    const Rect& r = g.link;
    const int cy = r.y + oy + r.h / 2;
    const char* const* icon = info.via == NetVia::Wifi ? kIconWifi : info.via == NetVia::Ble ? kIconBle : kIconUsb;
    const int rows = info.via == NetVia::Wifi ? 7 : info.via == NetVia::Ble ? 9 : 12;
    const int cell = 2, iw = (int)strlen(icon[0]) * cell;
    pixels(c, icon, rows, r.x + g.pad, cy - rows * cell / 2, cell, info.linked ? T.accent : T.dim);
    const char* via = info.via == NetVia::Wifi ? "Wi-Fi" : info.via == NetVia::Ble ? "Bluetooth" : "USB";
    char line[40];
    if (info.linked) snprintf(line, sizeof line, "on %s", via);
    else snprintf(line, sizeof line, "looking for the deck");
    const char* pref = info.uplink == NetUplink::Wifi ? "uplink Wi-Fi" : info.uplink == NetUplink::Ble ? "uplink BT" : "uplink auto";
    const int pw = textW(*g.small, pref), tx = r.x + g.pad + iw + 12;
    c.text(*g.small, tx, cy - g.small->cellH / 2, line, T.ink, r.x + r.w - g.pad - pw - 12 - tx);
    c.text(*g.small, r.x + r.w - g.pad - pw, cy - g.small->cellH / 2, pref, T.dim);
  }

  // The firmware version, at the bottom.
  {
    char v[48];
    snprintf(v, sizeof v, "firmware %s", info.firmware[0] ? info.firmware : "?");
    textCentered(c, *g.small, g.w / 2, g.versionY + oy, v, T.dim, g.w - 2 * g.m);
  }

  // The handle.
  fillPill(c, (g.w - 56) / 2, g.handleY + oy, 56, 6, T.dim);
}
