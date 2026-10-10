#include <assert.h>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>
#include "screen.h"

static const uint16_t GUARD = 0xDEAD;
static const int PAD = 2 * kScreenW;

int main() {
  std::ifstream fi(FIXTURE);
  std::stringstream ss;
  ss << fi.rdbuf();
  std::string text = ss.str();
  DynamicJsonDocument doc(65536);
  static Fleet f;
  assert(parseState(text, f, doc));

  std::vector<uint16_t> mem(kScreenW * kScreenH + 2 * PAD, GUARD);
  Canvas c{mem.data() + PAD, kScreenW, kScreenH};
  auto guards = [&] {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[PAD + kScreenW * kScreenH + i] != GUARD) return false;
    return true;
  };

  // Every agent as the hero, across the animation clock and the clock of ages.
  for (int hero = 0; hero < f.count; hero++)
    for (uint32_t ms = 0; ms < 2400; ms += 130)
      for (int64_t age : {0, 3, 754, 400000}) {
        drawScreen(c, {&f, hero, LinkState::Live, "MacBook-Pro", ms, f.serverTime + age, hero % 2 == 0});
        assert(guards());
        // The gap between the divider and the mini-board stays empty: the hero panel never spills.
        for (int y = 0; y < kScreenH; y++)
          for (int x = layout::heroW + 1; x < layout::miniX; x++) assert(c.px[y * kScreenW + x] == col::bg);
        // A waiting hero fills its panel; any other leaves it dark. The probe
        // sits inside the 2-pixel frame that an error hero draws.
        const bool waiting = f.agents[hero].status == Status::Waiting;
        assert((c.px[(kScreenH - 5) * kScreenW + layout::heroW - 6] == col::accent) == waiting);
      }

  // Every link state, with a fleet, with an empty fleet, with no fleet and with a bad hero index.
  static Fleet empty;
  for (LinkState ls : {LinkState::NoWifi, LinkState::Resolving, LinkState::Connecting, LinkState::PairError,
                       LinkState::Live, LinkState::Lost}) {
    drawScreen(c, {&f, 0, ls, "MacBook-Pro", 100, f.serverTime, false});
    drawScreen(c, {&empty, -1, ls, "MacBook-Pro", 100, 0, false});
    drawScreen(c, {nullptr, -1, ls, nullptr, 100, 0, false});
    drawScreen(c, {&f, 99, ls, "a-very-long-host-name-that-does-not-fit-on-the-screen-at-all", 100, 0, true});
    assert(guards());
  }

  // Long Cyrillic text in every field still stays inside the frame.
  {
    std::string longRu;
    for (int i = 0; i < 40; i++) longRu += "\xD0\x96\xD1\x8B";
    std::string t = "{\"server\":{\"name\":\"m\"},\"agents\":[{\"id\":\"ru\",\"slot\":0,\"title\":\"" + longRu +
                    "\",\"aiTitle\":\"" + longRu + "\",\"status\":\"running\",\"modelLabel\":\"" + longRu +
                    "\",\"activity\":{\"tool\":\"Edit\",\"detail\":\"/a/" + longRu + "\"},\"context\":{\"used\":5,\"window\":4},"
                    "\"turns\":65535,\"costUsd\":123456.78,\"subagents\":[{\"id\":\"s\"}]}]}";
    static Fleet ru;
    assert(parseState(t, ru, doc));
    drawScreen(c, {&ru, 0, LinkState::Live, "m", 0, 100, true});
    assert(guards());
    for (int y = 0; y < kScreenH; y++)
      for (int x = layout::heroW + 1; x < layout::miniX; x++) assert(c.px[y * kScreenW + x] == col::bg);
  }
  // Every pose with every hat, on both species, at both scales, also half off the canvas.
  for (int sp = 0; sp < 2; sp++)
    for (int pose = 0; pose <= (int)Pose::Hungry; pose++)
      for (int hat = 0; hat <= kHatShapeCount; hat++)
        for (uint32_t ms : {0u, 300u, 650u, 950u}) {
          const uint8_t color = (uint8_t)((hat + pose) % kHatColorCount);
          drawSprite(c, 40, 40, 4, (Species)sp, (Pose)pose, col::ink, col::bg, ms, true, (uint8_t)hat, color);
          drawSprite(c, -20, -15, 4, (Species)sp, (Pose)pose, col::ink, col::bg, ms, true, (uint8_t)hat, color);
          drawSprite(c, kScreenW - 20, kScreenH - 12, 2, (Species)sp, (Pose)pose, col::ink, col::bg, ms, false, (uint8_t)hat, color);
        }
  drawSprite(c, 10, 10, 4, Species::Claude, Pose::Idle, col::ink, col::bg, 0, true, 200, 200);   // out-of-range hat and colour
  assert(guards());

  // A hat changes the picture, and only above the legs.
  {
    c.fill(col::bg);
    drawSprite(c, 40, 40, 4, Species::Claude, Pose::Wait, col::ink, col::bg, 0, false);
    std::vector<uint16_t> bare(c.px, c.px + kScreenW * kScreenH);
    drawSprite(c, 40, 40, 4, Species::Claude, Pose::Wait, col::ink, col::bg, 0, false, 1, 0);
    int changed = 0;
    for (int i = 0; i < kScreenW * kScreenH; i++) changed += c.px[i] != bare[i];
    assert(changed > 0);
    for (int y = 40 + 4 * 4; y < kScreenH; y++)               // rows from the body's top down are untouched
      for (int x = 0; x < kScreenW; x++) assert(c.px[y * kScreenW + x] == bare[y * kScreenW + x]);
  }

  return 0;
}
