// The board's own panel (control_panel.h) on the list boards: pulled down
// from the header, its switches, the restart hold, and closing it.
#include <assert.h>
#include <stdio.h>

#include <initializer_list>

#include "layout_big.h"
#include "layout_square.h"

static Fleet fleet;   // empty: the panel doesn't need agents

// A finger from (x, y0) to (x, y1) in `steps` samples, 10 ms apart, then lifted.
static void drag(const SquarePanel& L, PanelUi& ui, int x, int y0, int y1, int steps, uint32_t& ms) {
  for (int i = 0; i <= steps; i++) {
    const int y = y0 + (y1 - y0) * i / steps;
    if (!L.panelTouch(ui, true, x, y, ms)) L.touch(ui, fleet, true, x, y, DeckScope::Act, ms);
    ms += 10;
  }
  if (!L.panelTouch(ui, false, x, y1, ms)) L.touch(ui, fleet, false, x, y1, DeckScope::Act, ms);
}

static void settle(const SquarePanel& L, PanelUi& ui, uint32_t& ms) {
  for (int i = 0; i < 40; i++) { L.tick(ui, fleet, ms); ms += 10; }
}

static void tap(const SquarePanel& L, PanelUi& ui, const Rect& r, uint32_t& ms) {
  const int x = r.x + r.w / 2, y = r.y + r.h / 2;
  assert(L.panelTouch(ui, true, x, y, ms));
  ms += 60;
  assert(L.panelTouch(ui, false, x, y, ms));
  ms += 10;
}

// A grid board's touch as grid_app.h's touchPump passes it on: the panel
// first, the screen when the panel didn't take it.
static Fleet gridFleet() {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (int i = 0; i < 2; i++) {
    Agent& a = f.agents[f.count++];
    snprintf(a.id, sizeof a.id, "g%d", i);
    snprintf(a.title, sizeof a.title, "t-g%d", i);
    a.slot = (uint8_t)(i == 0 ? 0 : 17);           // the first page's first tile, and one on page 2
    a.status = Status::Idle;
  }
  f.total = f.count;
  return f;
}

static void feed(const PanelLayout& L, PanelUi& ui, const Fleet& f, bool down, int x, int y, uint32_t ms) {
  if (!L.panelTouch(ui, down, x, y, ms)) L.touch(ui, f, down, x, y, DeckScope::Act, ms);
}

static void stroke(const PanelLayout& L, PanelUi& ui, const Fleet& f, int x0, int y0, int x1, int y1, int steps, uint32_t& ms) {
  for (int i = 0; i <= steps; i++) {
    feed(L, ui, f, true, x0 + (x1 - x0) * i / steps, y0 + (y1 - y0) * i / steps, ms);
    ms += 10;
  }
  feed(L, ui, f, false, x1, y1, ms);
  ms += 10;
}

static void gridSettle(const PanelLayout& L, PanelUi& ui, const Fleet& f, uint32_t& ms) {
  for (int i = 0; i < 40; i++) { L.tick(ui, f, ms); ms += 10; }
}

static void gridChecks(const PanelLayout& L, uint32_t& ms) {
  const Fleet f = gridFleet();
  const int W = L.width(), H = L.height();
  // Where the first tile is: the first cell of the first page (both grids start near the top-left).
  int tx = -1, ty = -1;
  for (int y = 2; y < H / 8 && tx < 0; y += 2)
    for (int x = 2; x < W / 4 && tx < 0; x += 2) {
      PanelUi probe;
      feed(L, probe, f, true, x, y, ms);
      feed(L, probe, f, false, x, y, ms + 50);
      if (probe.screen != Screen::Home) { tx = x + 10; ty = y; }
    }
  assert(tx >= 0 && ty < H / 8);                   // a tile reaches into the top strip
  {
    PanelUi ui;                                    // a tap there opens the tile, the panel stays shut
    stroke(L, ui, f, tx, ty, tx + 2, ty + 3, 3, ms);
    assert(ui.screen != Screen::Home && !controlShown(ui.ctl));
  }
  {
    PanelUi ui;                                    // a pull down from there opens the panel
    stroke(L, ui, f, tx, ty, tx + 10, ty + 300, 20, ms);
    gridSettle(L, ui, f, ms);
    assert(ui.ctl.open == 1.f && ui.screen == Screen::Home && !ui.down);
    L.button(ui, f);                               // BOOT closes it
    assert(!controlShown(ui.ctl));
    stroke(L, ui, f, tx, ty, tx, ty + 300, 20, ms);
    gridSettle(L, ui, f, ms);
    assert(ui.ctl.open == 1.f);
    stroke(L, ui, f, W / 2, H - 60, W / 2, 80, 10, ms);   // and a swipe up on it
    gridSettle(L, ui, f, ms);
    assert(!controlShown(ui.ctl) && ui.page == 0);  // the swipe was the panel's: home's page stays
  }
  {
    PanelUi ui;                                    // sideways from the top strip: the next page, no panel
    stroke(L, ui, f, W - 40, ty, 60, ty + 30, 10, ms);
    assert(ui.page == 1 && !controlShown(ui.ctl));
    // A pull that starts lower down is the screen's (not a page change either: it goes down).
    stroke(L, ui, f, W / 2, H / 2, W / 2, H - 20, 10, ms);
    assert(!controlShown(ui.ctl) && ui.page == 1);
  }
  // While it moves, the frame is the cached panel over the cached home: the
  // same picture as drawing both, row for row; and as rows to send.
  {
    static uint16_t a[480 * 480], b[480 * 480];
    Canvas ca{a, W, H}, cb{b, W, H};
    ControlInfo info;
    info.name = "test-board";
    info.board = "board";
    PanelUi u;
    u.ctl.animating = true;
    u.ctl.open = 0.4f;
    PanelInput in{&f, LinkState::Live, "test-board", DeckScope::Act, ms, 0, &u, 0, nullptr, &info};
    L.draw(ca, in);                                // the first moving frame draws the pictures
    assert(L.frameRows() == nullptr);
    u.ctl.open = 0.6f;
    L.draw(ca, in);                                // then cut from them
    u.ctl.animating = false;
    L.draw(cb, in);                                // the same frame drawn the slow way
    int diff = 0;
    for (int i = 0; i < W * H; i++) diff += a[i] != b[i];
    assert(diff < W * 3);                          // the edge line may sit a row apart
    u.ctl.animating = true;
    in.rowsOk = true;
    L.draw(ca, in);
    const uint16_t* const* rows = L.frameRows();
    assert(rows);
    diff = 0;
    for (int y = 0; y < H; y++)
      for (int x = 0; x < W; x++) diff += rows[y][x] != b[y * W + x];
    assert(diff < W * 3);
  }
}

int main() {
  const SquarePanel L(specAmoled18());
  const ControlGeom& G = L.G;
  uint32_t ms = 1000;

  // A pull from the header opens it; on release past half it settles open.
  PanelUi ui;
  drag(L, ui, 180, 10, 300, 20, ms);
  settle(L, ui, ms);
  assert(ui.ctl.open == 1.f && !ui.ctl.animating);
  assert(controlShown(ui.ctl));

  // A short, slow pull springs back shut (a quick one is a flick: it opens).
  PanelUi s;
  drag(L, s, 180, 10, 50, 60, ms);   // 40 px: under 5 mm (63 px on this glass)
  settle(L, s, ms);
  assert(s.ctl.open == 0.f && !controlShown(s.ctl));

  // A drag that starts below the header is the list's, not the panel's.
  PanelUi l;
  assert(!L.panelTouch(l, true, 180, L.S.headerH + 40, ms));
  assert(l.ctl.open == 0.f);

  // The switches: a tap asks the firmware to flip one, once.
  tap(L, ui, G.wifi, ms);
  assert(ui.ctl.act == ControlAct::Wifi);
  ui.ctl.act = ControlAct::None;
  tap(L, ui, G.ble, ms);
  assert(ui.ctl.act == ControlAct::Ble);
  ui.ctl.act = ControlAct::None;

  // Restart: a short press does nothing, a 1 s hold asks once.
  tap(L, ui, G.restart, ms);
  assert(ui.ctl.act == ControlAct::None);
  const int rx = G.restart.x + G.restart.w / 2, ry = G.restart.y + G.restart.h / 2;
  assert(L.panelTouch(ui, true, rx, ry, ms));
  for (int i = 0; i < 12; i++) { ms += 100; L.panelTouch(ui, true, rx, ry, ms); }
  assert(ui.ctl.act == ControlAct::Restart);
  L.panelTouch(ui, false, rx, ry, ms);

  // A swipe up on the open panel closes it.
  ui.ctl.act = ControlAct::None;
  drag(L, ui, 180, 400, 80, 10, ms);
  settle(L, ui, ms);
  assert(ui.ctl.open == 0.f && !controlShown(ui.ctl));

  // BOOT closes an open panel before it does anything else.
  drag(L, ui, 180, 10, 400, 20, ms);
  settle(L, ui, ms);
  assert(controlShown(ui.ctl));
  L.button(ui, fleet);
  assert(!controlShown(ui.ctl));

  // The grids (the 4" big layout, the square grid): the panel comes from the
  // top strip too, but a tile there still takes a tap, and a sideways swipe
  // that starts there still turns the page.
  {
    const SquarePanel G4(specLcd4());
    const BigPanel B4(specBig480());
    gridChecks(G4, ms);
    gridChecks(B4, ms);
  }

  // Drawn on both list boards, open and halfway, without leaving the canvas.
  for (const SquareSpec& sp : {specAmoled18(), specT4S3()}) {
    const SquarePanel P(sp);
    static uint16_t px[450 * 600];
    Canvas c{px, sp.w, sp.h};
    ControlInfo info;
    info.name = "test-board";
    info.bleName = "deck-000000";
    info.board = "board";
    info.firmware = "0.0.0";
    info.id = "02:00:00:00:00:01";
    info.wifiCapable = info.bleCapable = info.wifiOn = info.bleOn = true;
    for (float open : {1.f, 0.5f}) {
      PanelUi u;
      u.ctl.open = open;
      PanelInput in{&fleet, LinkState::Live, "test-board", DeckScope::Act, ms, 0, &u, 0, nullptr, &info};
      P.draw(c, in);
    }
  }
  // While it moves, the frame is the cached panel over the cached home: the
  // same picture as drawing both, row for row.
  {
    const SquareSpec sp = specT4S3();
    const SquarePanel P(sp), Q(sp);
    static uint16_t a[450 * 600], b[450 * 600];
    Canvas ca{a, sp.w, sp.h}, cb{b, sp.w, sp.h};
    ControlInfo info;
    info.name = "test-board";
    info.board = "board";
    PanelUi u;
    u.ctl.animating = true;
    u.ctl.open = 0.4f;
    PanelInput in{&fleet, LinkState::Live, "test-board", DeckScope::Act, ms, 0, &u, 0, nullptr, &info};
    P.draw(ca, in);                    // made from the cache
    u.ctl.animating = false;           // the same frame drawn the slow way, for comparison
    Q.draw(cb, in);
    int diff = 0;
    for (int i = 0; i < sp.w * sp.h; i++) diff += a[i] != b[i];
    assert(diff < sp.w * 3);           // the edge line may sit a row apart
  }
  printf("control panel: ok\n");
  return 0;
}
