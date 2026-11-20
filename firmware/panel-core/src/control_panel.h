#pragma once
// The board's own panel on the small list boards (the 1.8" AMOLED and the
// T4-S3): pulled down from the top edge of home, over whatever home shows.
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
  Rect wifi, ble, link, restart;
  int factsY, factPitch, facts;
  int handleY;
  int commitPx;                // kControlCommitMm on this glass
  const Font* name;
  const Font* label;
  const Font* small;
};

// diagIn: the glass's diagonal in inches (0: unknown, then a fifth of the height).
static inline ControlGeom controlGeom(int w, int h, int m, float diagIn = 0) {
  ControlGeom g{};
  const float ppi = diagIn > 0 ? sqrtf((float)(w * w + h * h)) / diagIn : 0;
  g.commitPx = ppi > 0 ? (int)lroundf(kControlCommitMm / 25.4f * ppi) : h / 5;
  const bool big = h >= 560;   // the T4-S3
  g.w = w; g.h = h; g.m = m;
  g.pad = big ? 14 : 12;
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
  g.link = Rect{m, y, w - 2 * m, big ? 62 : 46};
  y += g.link.h + (big ? 10 : 6);
  g.handleY = h - m - (big ? 10 : 8);
  const int btnH = big ? 68 : 52;
  g.restart = Rect{m, g.handleY - (big ? 14 : 10) - btnH, w - 2 * m, btnH};
  g.factsY = y;
  g.factPitch = g.small->cellH + (big ? 12 : 5);
  g.facts = (g.restart.y - (big ? 8 : 4) - y) / g.factPitch;
  if (g.facts > 5) g.facts = 5;
  if (g.facts < 0) g.facts = 0;
  return g;
}

static inline ControlHit controlHit(const ControlGeom& g, int x, int y) {
  if (g.wifi.contains(x, y)) return ControlHit::Wifi;
  if (g.ble.contains(x, y)) return ControlHit::Ble;
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
  }
  u.pressed = ControlHit::None;
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
static const char* const kIconBle[] = {"...#...", "...##..", "#..#.#.", ".#.##..", "..##...", ".#.##..", "#..#.#.", "...##..", "...#..."};

static inline void pixels(Canvas& c, const char* const* rows, int n, int x, int y, int cell, uint16_t col) {
  for (int r = 0; r < n; r++)
    for (int k = 0; rows[r][k]; k++)
      if (rows[r][k] == '#') c.rect(x + k * cell, y + r * cell, cell, cell, col);
}

static inline void fmtUp(uint32_t sec, char* out, size_t cap) {
  if (sec < 60) snprintf(out, cap, "%u s", (unsigned)sec);
  else if (sec < 3600) snprintf(out, cap, "%u min", (unsigned)(sec / 60));
  else snprintf(out, cap, "%u h %u min", (unsigned)(sec / 3600), (unsigned)(sec / 60 % 60));
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

  // How it reaches the deck.
  {
    const Rect& r = g.link;
    roundRect(c, r.x, r.y + oy, r.w, r.h, g.r, T.panel);
    const char* via = info.via == NetVia::Wifi ? "Wi-Fi" : info.via == NetVia::Ble ? "Bluetooth" : "USB";
    char line[40];
    if (info.linked) snprintf(line, sizeof line, "on %s", via);
    else snprintf(line, sizeof line, "looking for the deck");
    const int ty = r.y + oy + (r.h - g.label->cellH) / 2 + 1;
    // The link's own mark, orange while a deck answers on it.
    const int cell = g.h >= 560 ? 3 : 3;
    const char* const* icon = info.via == NetVia::Wifi ? kIconWifi : info.via == NetVia::Ble ? kIconBle : kIconUsb;
    const int rows = info.via == NetVia::Wifi ? 7 : info.via == NetVia::Ble ? 9 : 12;
    const int iw = (int)strlen(icon[0]) * cell, ih = rows * cell;
    pixels(c, icon, rows, r.x + g.pad, r.y + oy + (r.h - ih) / 2, cell, info.linked ? T.accent : T.dim);
    const int dot = iw - 10;
    const char* pref = info.uplink == NetUplink::Wifi ? "uplink Wi-Fi" : info.uplink == NetUplink::Ble ? "uplink BT" : "uplink auto";
    const int pw = textW(*g.small, pref);
    c.text(*g.label, r.x + g.pad + dot + 22, ty, line, T.ink, r.w - 2 * g.pad - dot - 10 - pw - 8);
    c.text(*g.small, r.x + r.w - g.pad - pw, r.y + oy + (r.h - g.small->cellH) / 2 + 1, pref, T.dim);
  }

  // Facts, as many as fit.
  {
    char up[24];
    fmtUp(info.upSec, up, sizeof up);
    const char* keys[5] = {"firmware", "id", "board", "network", "up"};
    const char* vals[5] = {info.firmware, info.id, info.board, info.ssid[0] ? info.ssid : "none", up};
    for (int i = 0; i < g.facts; i++) {
      const int y = g.factsY + i * g.factPitch + oy;
      c.text(*g.small, x0, y, keys[i], T.dim);
      const int room = g.w - 2 * x0 - textW(*g.small, keys[i]) - 12;
      const int vw = textW(*g.small, vals[i]);
      const int shown = vw > room ? room / g.small->cellW * g.small->cellW : vw;
      c.text(*g.small, g.w - x0 - shown, y, vals[i], T.ink, room, true);
      c.rect(x0, y + g.factPitch - 3, g.w - 2 * x0, 1, T.line);
    }
  }

  // Restart: the fill grows while it is held.
  {
    const Rect& r = g.restart;
    const bool held = u.down && u.pressed == ControlHit::Restart;
    int permille = 0;
    if (held) {
      const uint32_t t = ms - u.downMs;
      permille = t >= kControlHoldMs ? 1000 : (int)(t * 1000 / kControlHoldMs);
    }
    roundRect(c, r.x, r.y + oy, r.w, r.h, r.h / 2, T.panel);
    if (permille > 0) {
      const int fw = r.h + (r.w - r.h) * permille / 1000;
      roundRect(c, r.x, r.y + oy, fw, r.h, r.h / 2, T.accent);
    }
    roundFrame(c, r.x, r.y + oy, r.w, r.h, r.h / 2, 2, held ? T.accent : T.ringTrack);
    const char* text = u.holdFired ? "restarting" : held ? "keep holding" : "hold to restart";
    textCentered(c, *g.label, r.x + r.w / 2, r.y + oy + (r.h - g.label->cellH) / 2 + 1, text,
                 permille > 500 ? T.bg : T.ink, r.w - 2 * g.pad);
  }

  // The handle.
  fillPill(c, (g.w - 56) / 2, g.handleY + oy, 56, 6, T.dim);
}
