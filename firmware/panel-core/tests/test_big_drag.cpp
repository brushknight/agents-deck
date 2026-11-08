// A drag must never turn a swipe or stale fleet index into an agent action.
#include <assert.h>
#include <string.h>
#include <vector>
#include "layout_big.h"

static Fleet sample() {
  Fleet f{};
  f.count = f.total = 3;
  for (int i = 0; i < 3; ++i) {
    snprintf(f.agents[i].id, sizeof f.agents[i].id, "a%d", i);
    f.agents[i].slot = i == 2 ? 20 : i;
    f.agents[i].status = Status::Idle;
  }
  return f;
}

static void press(PanelUi& ui, const Fleet& f, int cell, DeckScope scope, uint32_t ms) {
  const Rect r = bigCell(bigGrid(specBig480(), bigPages(f), ui.wifi), cell);
  assert(bigTouch(ui, specBig480(), f, true, r.x + 30, r.y + 30, scope, ms).cmd == DeckCmd::None);
}

static DeckCommand release(PanelUi& ui, const Fleet& f, DeckScope scope, uint32_t ms) {
  return bigTouch(ui, specBig480(), f, false, 0, 0, scope, ms);
}

int main() {
  const BigSpec s = specBig480();
  Fleet f = sample();
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 599);
    assert(!ui.tileMoving);
    press(ui, f, 0, DeckScope::Act, 600);
    assert(ui.tileMoving);
    press(ui, f, 5, DeckScope::Act, 700);
    // The source index changes while the finger is down.
    const Agent tmp = f.agents[0]; f.agents[0] = f.agents[1]; f.agents[1] = tmp;
    DeckCommand c = release(ui, f, DeckScope::Act, 750);
    assert(c.cmd == DeckCmd::Move && !strcmp(c.agent, "a0") && c.slot == 5);
    assert(f.agents[1].slot == 0); // Only the daemon changes the fleet.
    assert(release(ui, f, DeckScope::Act, 760).cmd == DeckCmd::None);
  }
  f = sample();
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 50);
    assert(release(ui, f, DeckScope::Act, 60).cmd == DeckCmd::None);
    assert(ui.screen == Screen::Card && !strcmp(ui.openId, "a0"));
  }
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 600);
    assert(release(ui, f, DeckScope::Act, 650).cmd == DeckCmd::None);
    assert(ui.screen == Screen::Home); // A hold on the same cell does not open a card.
  }
  for (DeckScope scope : {DeckScope::View, DeckScope::Act}) {
    PanelUi ui;
    press(ui, f, 0, scope, 0);
    press(ui, f, 0, scope, 600);
    assert(ui.tileMoving == (scope == DeckScope::Act));
    press(ui, f, 1, scope, 700);
    assert(release(ui, f, DeckScope::View, 750).cmd == DeckCmd::None); // Scope revoked mid-drag.
  }
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 600);
    press(ui, f, 1, DeckScope::Act, 700);
    Fleet gone = f;
    snprintf(gone.agents[0].id, sizeof gone.agents[0].id, "other");
    assert(release(ui, gone, DeckScope::Act, 750).cmd == DeckCmd::None);
  }
  {
    PanelUi ui;
    ui.page = 1; ui.wifi = true;
    press(ui, f, 4, DeckScope::Admin, 0); // Agent a2 at slot 20.
    press(ui, f, 4, DeckScope::Admin, 600);
    press(ui, f, 15, DeckScope::Admin, 700);
    assert(release(ui, f, DeckScope::Admin, 750).slot == 31);
  }
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 600);
    press(ui, f, 1, DeckScope::Act, 700); // Occupied slot: daemon handles the swap.
    const DeckCommand c = release(ui, f, DeckScope::Act, 750);
    assert(c.cmd == DeckCmd::Move && c.slot == 1);
  }
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 600);
    bigTouch(ui, s, f, true, 0, 0, DeckScope::Act, 700);
    assert(release(ui, f, DeckScope::Act, 750).cmd == DeckCmd::None); // Outside grid.
  }
  {
    PanelUi ui;
    press(ui, f, 2, DeckScope::Act, 0); // Empty cell cannot start a move.
    press(ui, f, 2, DeckScope::Act, 600);
    assert(!ui.tileMoving);
  }
  {
    PanelUi ui;
    bigTouch(ui, s, f, true, 400, 50, DeckScope::Act, 0);
    bigTouch(ui, s, f, true, 200, 50, DeckScope::Act, 100);
    bigTouch(ui, s, f, true, 200, 50, DeckScope::Act, 800);
    assert(!ui.tileMoving); // Waiting after a quick move cannot turn a swipe into a drag.
    assert(release(ui, f, DeckScope::Act, 900).cmd == DeckCmd::None && ui.page == 1);
  }
  {
    PanelUi ui;
    press(ui, f, 0, DeckScope::Act, 0);
    press(ui, f, 0, DeckScope::Act, 600);
    press(ui, f, 5, DeckScope::Act, 700);
    std::vector<uint16_t> px(480 * 480);
    Canvas canvas{px.data(), 480, 480};
    const BigPanel layout(s);
    PanelInput in{&f, LinkState::Live, "test", DeckScope::Act, 700, 0, &ui, 0};
    layout.draw(canvas, in); // Source and destination feedback stays within the framebuffer.
    ui.page = 1;
    assert(release(ui, f, DeckScope::Act, 750).cmd == DeckCmd::None); // Page changed externally.
  }
}
