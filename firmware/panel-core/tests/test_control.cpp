// The board's own panel (control_panel.h) on the list boards: pulled down
// from the header, its switches, the restart hold, and closing it.
#include <assert.h>
#include <stdio.h>

#include <initializer_list>

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
  drag(L, s, 180, 10, 60, 60, ms);   // 50 px: under a fifth of the way
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

  // The 4" grid has no panel.
  const SquarePanel G4(specLcd4());
  PanelUi g;
  assert(!G4.panelTouch(g, true, 240, 4, ms));

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
