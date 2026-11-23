// The hats: still while the agent rests, stepping through their frames while
// it works, each at its own pace.
#include <assert.h>
#include <string.h>
#include <vector>
#include "sprites.h"

static const int W = 120, H = 100, S = 4, OX = 30, OY = 40;
static const uint16_t BODY = 0xFFFF, CUT = 0x0001;

// One sprite without its bubble, on an empty canvas.
static std::vector<uint16_t> shot(Species sp, Pose pose, uint32_t ms, int hat, int color) {
  std::vector<uint16_t> mem(W * H);
  Canvas c{mem.data(), W, H};
  c.fill(CUT);
  drawSprite(c, OX, OY, S, sp, pose, BODY, CUT, ms, false, (uint8_t)hat, (uint8_t)color);
  return mem;
}

static uint32_t hashOf(const std::vector<uint16_t>& px) {
  uint32_t h = 2166136261u;
  for (uint16_t v : px) { h = (h ^ (v & 0xFF)) * 16777619u; h = (h ^ (v >> 8)) * 16777619u; }
  return h;
}

// The colour of the cell at (ux, uy) in the sprite's units.
static uint16_t cellAt(const std::vector<uint16_t>& px, int ux, int uy) {
  return px[(OY + (uy + 4) * S + S / 2) * W + OX + ux * S + S / 2];
}

int main() {
  const int propeller = 10, bandana = 13;                  // 1 + the index into kHatShapes
  assert(strcmp(kHatShapes[propeller - 1], "propeller") == 0 && strcmp(kHatShapes[bandana - 1], "bandana") == 0);
  const Pose kWorking[] = {Pose::Run, Pose::Think, Pose::Read, Pose::Write, Pose::Bash,
                           Pose::Web, Pose::Plan, Pose::Delegate, Pose::Compact};
  const Pose kResting[] = {Pose::Wait, Pose::Err, Pose::Idle, Pose::Start, Pose::Exit, Pose::Celebrate, Pose::Hungry};

  for (Species sp : {Species::Claude, Species::Codex}) {
    // The propeller turns one frame every 120 ms while the agent works. The
    // body holds still over these 120 ms in every pose, so only the hat can differ.
    for (Pose pose : kWorking) {
      assert(shot(sp, pose, 0, propeller, 2) != shot(sp, pose, 120, propeller, 2));
      assert(shot(sp, pose, 0, propeller, 2) == shot(sp, pose, 119, propeller, 2));
      assert(shot(sp, pose, 0, 0, 0) == shot(sp, pose, 120, 0, 0));                  // bare-headed: nothing moves
    }
    // It stays still on an agent that is not working.
    for (Pose pose : kResting) assert(shot(sp, pose, 0, propeller, 2) == shot(sp, pose, 120, propeller, 2));

    // Four frames: the blades swap sides in two steps, then the cycle starts over.
    const Pose p = Pose::Plan;                             // nothing but the hat moves in this pose
    const auto f0 = shot(sp, p, 0, propeller, 2), f1 = shot(sp, p, 120, propeller, 2);
    const auto f2 = shot(sp, p, 240, propeller, 2), f3 = shot(sp, p, 360, propeller, 2);
    assert(f0 != f1 && f0 != f2 && f0 != f3 && f1 != f2 && f1 != f3 && f2 != f3);
    assert(shot(sp, p, 480, propeller, 2) == f0);
    const bool bot = sp == Species::Codex;
    const int left = bot ? 4 : 3, right = bot ? 9 : 9;     // a cell of each blade
    const uint16_t light = rgb565(0xF7, 0xF4, 0xEC), main = sprite_detail::kHatPalette[2][0];
    assert(cellAt(f0, left, -4) == light && cellAt(f0, right, -4) == main);
    assert(cellAt(f2, left, -4) == main && cellAt(f2, right, -4) == light);
    assert(cellAt(f1, left, -4) == CUT && cellAt(f3, left, -4) == CUT);   // edge on: the blades are short

    // Every hat moves while working, at its own pace, and comes round again.
    for (int hat = 1; hat <= kHatShapeCount; hat++) {
      const sprite_detail::HatArt& art = (bot ? sprite_detail::kOniHats : sprite_detail::kHats)[hat - 1];
      assert(art.ms >= 100 && art.frames >= 2 && art.frames <= sprite_detail::kHatFramesMax);
      const auto first = shot(sp, p, 0, hat, 1);
      bool moved = false;
      for (int i = 1; i < art.frames; i++) moved = moved || shot(sp, p, (uint32_t)i * art.ms, hat, 1) != first;
      assert(moved);
      assert(shot(sp, p, (uint32_t)art.ms * art.frames, hat, 1) == first);
      assert(shot(sp, p, art.ms - 1u, hat, 1) == first);
      assert(shot(sp, Pose::Idle, 0, hat, 1) == shot(sp, Pose::Idle, art.ms, hat, 1));
    }
  }

  // A resting agent's hat is the picture the boards drew before hats moved:
  // hashes of that picture (Wait, no bubble, colour = hat % 8). The bandana
  // is not in the list: its knot moved to the left, as in the dashboard.
  static const uint32_t kStill[2][kHatShapeCount - 1] = {
      {0x4DD098C5, 0xB5123365, 0x4357A905, 0xEB2DC385, 0xB5BBF145, 0x3A528105,
       0xBE22A005, 0x72A55345, 0xC6215105, 0x74776865, 0xA376B585, 0x00174B85},
      {0x2F35B0A5, 0xFDF49285, 0x513A4BE5, 0xA9E304A5, 0x0D4F57A5, 0x661C57A5,
       0xA1E817E5, 0x4AD154A5, 0x57207B45, 0x24C66D45, 0x860F7AA5, 0xB25C2025},
  };
  for (int sp = 0; sp < 2; sp++)
    for (int hat = 1; hat < bandana; hat++)
      for (uint32_t ms : {0u, 120u, 777u})
        assert(hashOf(shot((Species)sp, Pose::Wait, ms, hat, hat % kHatColorCount)) == kStill[sp][hat - 1]);
  {
    const uint16_t shade = sprite_detail::kHatPalette[0][1];
    const auto b = shot(Species::Claude, Pose::Wait, 0, bandana, 0);
    assert(cellAt(b, -1, 0) == shade && cellAt(b, 13, -1) == CUT);
  }

  // Any time, any pose, also half off the canvas: the frame index stays in range.
  std::vector<uint16_t> mem(W * H);
  Canvas c{mem.data(), W, H};
  for (int sp = 0; sp < 2; sp++)
    for (int pose = 0; pose <= (int)Pose::Hungry; pose++)
      for (int hat = 1; hat <= kHatShapeCount; hat++)
        for (uint32_t ms : {0u, 1u, 119u, 120u, 4321u, 0x7FFFFFFFu, 0xFFFFFFFFu}) {
          drawSprite(c, OX, OY, S, (Species)sp, (Pose)pose, BODY, CUT, ms, true, (uint8_t)hat, 3);
          drawSprite(c, -10, -6, 2, (Species)sp, (Pose)pose, BODY, CUT, ms, true, (uint8_t)hat, 3);
        }
  return 0;
}
