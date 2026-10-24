// Host renderer: draws the same frames as the board, without the board.
//
//   c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto sim/render.cpp -o out/render
//   out/render ../../docs/fixtures/state.json out/frames [ms] [147|lcd4|amoled18|orbit|dial|selftest28|selftest21]
//
// The State JSON file becomes the Deck Link frames the deck would send
// (sim/fixture.h), and those go through the board's adapter (deck_model.h).
//
// Writes PPM files: hero-<slot>.ppm (each agent as the hero), notice-*.ppm
// (the full-screen states), poses.ppm (every pose of both species) and
// hats.ppm (every hat). With the layout lcd4 or amoled18 it draws the grid
// boards instead: grid.ppm, grid-page2.ppm, card.ppm (a running agent),
// prompt.ppm (a waiting agent with its answers as buttons), prompt-view.ppm
// (the same with scope view), question.ppm and the notices.
// With orbit or dial it draws the round boards (sim/round_shots.h): home
// with 3, 8 and 22 agents, a new waiter, the detail, the prompt and the
// full-screen states. Build with -DPANEL_MAX_AGENTS=64 for those.
// tools/ppm2png.py turns them into PNGs.
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include "layout_147.h"
#include "layout_grid.h"
#include "fixture.h"
#include "round_shots.h"

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

static const char* const kDevice = "waveshare-lcd-147-a1b2c3";

// The grid boards: every screen of one layout.
static int renderGrid(const std::string& dir, const GridLayout& L, const char* device, DeckFleet& deck,
                      uint32_t ms, int64_t now) {
  const Theme T = deckTheme(L.trueBlack);
  std::vector<uint16_t> buf(L.w * L.h);
  Canvas c{buf.data(), L.w, L.h};
  const Fleet& f = deck.fleet;
  PanelUi ui;
  PanelInput in{&f, LinkState::Live, device, true, ms, now, &ui};
  auto shot = [&](const char* name) {
    drawPanel(c, L, T, in);
    writePpm(dir + "/" + name + ".ppm", c);
  };
  shot("grid");

  // A second page: one more agent beyond the first page's slots.
  static DeckFleet more;
  more = deck;
  {
    Agent a = f.agents[0];
    snprintf(a.id, sizeof a.id, "p2more");
    snprintf(a.title, sizeof a.title, "release-notes");
    a.slot = 17;
    a.status = Status::Waiting;
    a.subagents = 0;
    deck_model_detail::upsert(more.fleet, a);
    more.fleet.total++;
  }
  in.fleet = &more.fleet;
  ui.page = 1;
  shot("grid-page2");
  ui.page = 0;
  shot("grid-page1-of-2");
  in.fleet = &f;

  auto open = [&](const char* id) { snprintf(ui.openId, sizeof ui.openId, "%s", id); };
  open("k3f9a2");
  shot("card");
  open("h0m3l4");
  shot("prompt");
  in.act = false;
  shot("prompt-view");
  in.act = true;
  open("4uthfl");
  shot("question");
  open("1nfr4t");
  shot("card-error");
  ui.openId[0] = '\0';
  uiToast(ui, "answered", ms);
  shot("toast");
  ui.toast[0] = '\0';

  in.link = LinkState::Lost;
  shot("offline");
  static Fleet empty;
  snprintf(empty.server, sizeof empty.server, "workstation");
  const struct { const char* name; LinkState link; const Fleet* f; } kNotices[] = {
      {"notice-connecting", LinkState::Connecting, nullptr}, {"notice-unapproved", LinkState::Unapproved, nullptr},
      {"notice-noagents", LinkState::Live, &empty},           {"notice-offline", LinkState::Lost, nullptr},
  };
  for (const auto& n : kNotices) {
    in.link = n.link;
    in.fleet = n.f;
    shot(n.name);
  }
  return 0;
}

int main(int argc, char** argv) {
  if (argc < 3) {
    fprintf(stderr, "usage: render <state.json> <outdir> [ms]\n");
    return 2;
  }
  const std::string dir = argv[2];
  const uint32_t ms = argc > 3 ? (uint32_t)atoi(argv[3]) : 0;

  static DeckFleet deck;
  if (!fixture::load(argv[1], deck)) {
    fprintf(stderr, "cannot read %s\n", argv[1]);
    return 1;
  }
  const Fleet& fleet = deck.fleet;
  // The sample fleet has every statusSince equal to server.time. Move the clock
  // on so the ages are not all "0s" and nobody is still celebrating.
  const int64_t now = (fleet.serverTime ? fleet.serverTime : 1791421323) + 754;
  const std::string layout = argc > 4 ? argv[4] : "147";
  if (layout == "lcd4") return renderGrid(dir, layoutLcd4(), "waveshare-touch-lcd-4-2a5390", deck, ms, now);
  if (layout == "amoled18") return renderGrid(dir, layoutAmoled18(), "waveshare-touch-amoled-18-fea2ac", deck, ms, now);
  if (layout == "orbit") {
    static const OrbitPanel orbit(layoutOrbit28());
    return round_shots::render(dir, orbit, "waveshare-touch-lcd-28c-3c71bf", fleet, ms, now);
  }
  if (layout == "dial") {
    static const DialPanel dial(layoutDial21());
    return round_shots::render(dir, dial, "waveshare-touch-lcd-21-8e04d2", fleet, ms, now);
  }
  if (layout == "selftest28")
    return round_shots::renderSelftest(dir, "waveshare-touch-lcd-28c", "tca9554 ok" DECK_DOT "gt911 0x5D", 18000000);
  if (layout == "selftest21")
    return round_shots::renderSelftest(dir, "waveshare-touch-lcd-21", "tca9554 ok" DECK_DOT "cst820 ok", 16000000);
  if (layout != "147") {
    fprintf(stderr, "unknown layout %s (147, lcd4, amoled18, orbit, dial, selftest28, selftest21)\n", layout.c_str());
    return 2;
  }

  std::vector<uint16_t> buf(kScreenW * kScreenH);
  Canvas c{buf.data(), kScreenW, kScreenH};

  for (int i = 0; i < fleet.count; i++) {
    drawScreen(c, {&fleet, i, LinkState::Live, kDevice, ms, now, false});
    char name[32];
    snprintf(name, sizeof name, "/hero-%02u.ppm", fleet.agents[i].slot);
    writePpm(dir + name, c);
  }

  // What the board picks by itself, then the same with the button hold and the deck offline.
  drawScreen(c, {&fleet, autoHero(fleet), LinkState::Live, kDevice, ms, now, false});
  writePpm(dir + "/auto.ppm", c);
  drawScreen(c, {&fleet, 0, LinkState::Lost, kDevice, ms, now, true});
  writePpm(dir + "/held-lost.ppm", c);

  static Fleet empty;
  snprintf(empty.server, sizeof empty.server, "workstation");
  const struct { const char* name; LinkState link; const Fleet* f; } kNotices[] = {
      {"connecting", LinkState::Connecting, &empty}, {"unapproved", LinkState::Unapproved, &empty},
      {"noagents", LinkState::Live, &empty},         {"offline-empty", LinkState::Lost, &empty},
  };
  for (const auto& n : kNotices) {
    drawScreen(c, {n.f, -1, n.link, kDevice, ms, now, false});
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
