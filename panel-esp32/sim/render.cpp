// Host renderer: draws the same frames as the board, without the board.
//
//   clang++ -std=c++17 -Ilib/core -I.pio/libdeps/waveshare-s3-147b/ArduinoJson/src sim/render.cpp -o out/render
//   out/render ../docs/fixtures/state.json out/frames
//
// Writes PPM files: hero-<slot>.ppm (each agent as the hero), notice-*.ppm
// (the full-screen states), poses.ppm (every pose of both species) and
// hats.ppm (every hat).
// tools/ppm2png.py turns them into PNG sheets.
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include "screen.h"

static void writePpm(const std::string& path, const Canvas& c) {
  std::ofstream out(path, std::ios::binary);
  out << "P6\n" << c.w << " " << c.h << "\n255\n";
  for (int i = 0; i < c.w * c.h; i++) {
    const uint16_t p = c.px[i];
    const unsigned char rgb[3] = {(unsigned char)(((p >> 11) & 31) * 255 / 31), (unsigned char)(((p >> 5) & 63) * 255 / 63),
                                  (unsigned char)((p & 31) * 255 / 31)};
    out.write((const char*)rgb, 3);
  }
}

int main(int argc, char** argv) {
  if (argc < 3) {
    fprintf(stderr, "usage: render <state.json> <outdir> [ms]\n");
    return 2;
  }
  const std::string dir = argv[2];
  const uint32_t ms = argc > 3 ? (uint32_t)atoi(argv[3]) : 0;

  std::ifstream fi(argv[1]);
  std::stringstream ss;
  ss << fi.rdbuf();
  std::string text = ss.str();
  DynamicJsonDocument doc(65536);
  static Fleet fleet;
  if (!parseState(text, fleet, doc)) {
    fprintf(stderr, "cannot parse %s\n", argv[1]);
    return 1;
  }
  // The sample fleet has every statusSince equal to server.time. Move the clock
  // on so the ages are not all "0s" and nobody is still celebrating.
  const int64_t now = (fleet.serverTime ? fleet.serverTime : 1791421323) + 754;

  std::vector<uint16_t> buf(kScreenW * kScreenH);
  Canvas c{buf.data(), kScreenW, kScreenH};

  for (int i = 0; i < fleet.count; i++) {
    drawScreen(c, {&fleet, i, LinkState::Live, "MacBook-Pro", ms, now, false});
    char name[32];
    snprintf(name, sizeof name, "/hero-%02u.ppm", fleet.agents[i].slot);
    writePpm(dir + name, c);
  }

  // What the board picks by itself, then the same with the button hold and a lost link.
  drawScreen(c, {&fleet, autoHero(fleet), LinkState::Live, "MacBook-Pro", ms, now, false});
  writePpm(dir + "/auto.ppm", c);
  drawScreen(c, {&fleet, 0, LinkState::Lost, "MacBook-Pro", ms, now, true});
  writePpm(dir + "/held-lost.ppm", c);

  static Fleet empty;
  snprintf(empty.server, sizeof empty.server, "MacBook-Pro");
  const struct { const char* name; LinkState link; const Fleet* f; } kNotices[] = {
      {"nowifi", LinkState::NoWifi, &empty},       {"resolving", LinkState::Resolving, &empty},
      {"connecting", LinkState::Connecting, &empty}, {"pairerror", LinkState::PairError, &empty},
      {"noagents", LinkState::Live, &empty},        {"lost-empty", LinkState::Lost, &empty},
  };
  for (const auto& n : kNotices) {
    drawScreen(c, {n.f, -1, n.link, "MacBook-Pro", ms, now, false});
    writePpm(dir + "/notice-" + n.name + ".ppm", c);
  }

  // Every pose, both species: scale 4 with the bubble, and scale 2 as on the mini-board.
  static const char* const kNames[] = {"think", "read", "write", "bash", "web", "plan", "delegate", "compact",
                                       "run", "wait", "err", "idle", "start", "exit", "celebrate", "hungry"};
  const int cols = 4, cw = 120, ch = 76;
  std::vector<uint16_t> big(cols * cw * 8 * ch);
  Canvas p{big.data(), cols * cw, 8 * ch};
  p.fill(col::bg);
  for (int sp = 0; sp < 2; sp++)
    for (int i = 0; i < 16; i++) {
      const int x = (i % cols) * cw, y = (sp * 4 + i / cols) * ch;
      drawSprite(p, x + 4, y + 2, 4, (Species)sp, (Pose)i, col::ink, col::bg, ms, true);
      drawSprite(p, x + 84, y + 14, 2, (Species)sp, (Pose)i, col::accent, col::bg, ms, false);
      p.text(FONT_S, x + 4, y + 56, kNames[i], col::dim);
    }
  writePpm(dir + "/poses.ppm", p);

  // Every hat on both species, colours in turn: scale 4 and the mini-board's scale 2.
  const int hw = 132, hh = 84;
  std::vector<uint16_t> hatBuf(5 * hw * 6 * hh);
  Canvas hc{hatBuf.data(), 5 * hw, 6 * hh};
  hc.fill(col::bg);
  for (int sp = 0; sp < 2; sp++)
    for (int i = 0; i < kHatShapeCount; i++) {
      const int x = (i % 5) * hw, y = (sp * 3 + i / 5) * hh;
      drawSprite(hc, x + 16, y + 10, 4, (Species)sp, i % 2 ? Pose::Run : Pose::Read, col::ink, col::bg, ms, false,
                 (uint8_t)(i + 1), (uint8_t)(i % kHatColorCount));
      drawSprite(hc, x + 92, y + 24, 2, (Species)sp, Pose::Exit, col::dim, col::bg, ms, false,
                 (uint8_t)(i + 1), (uint8_t)(i % kHatColorCount));
      hc.text(FONT_S, x + 4, y + 64, kHatShapes[i], col::dim);
    }
  writePpm(dir + "/hats.ppm", hc);
  return 0;
}
