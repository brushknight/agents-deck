// Host renderer: draws the same frames as the board, without the board.
//
//   c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto sim/render.cpp -o out/render
//   out/render ../../docs/fixtures/state.json out/frames [ms] [147|lcd4|amoled18|t4s3|freenove28|orbit|dial|spring|spring28|selftest28|selftest21]
//
// The State JSON file becomes the Deck Link frames the deck would send
// (sim/fixture.h), and those go through the board's adapter (deck_model.h).
//
// Writes PPM files: hero-<slot>.ppm (each agent as the hero), notice-*.ppm
// (the full-screen states), poses.ppm (every pose of both species) and
// hats.ppm (every hat). With lcd4, amoled18, t4s3 or freenove28 it draws the square boards'
// screens (home, scrolling or paging, cards, prompts, notices); with orbit or
// dial the round boards'.
// With orbit or dial it draws the round boards (sim/round_shots.h): home
// with 3, 8 and 22 agents, a new waiter, the detail, the prompt and the
// full-screen states. Build with -DPANEL_MAX_AGENTS=64 for those.
// tools/ppm2png.py turns them into PNGs.
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include "layout_147.h"
#include "layout_big.h"
#include "layout_square.h"
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

// The grid boards on Wi-Fi: home with its mark (and the "paired for Wi-Fi"
// toast), then the connecting notice in every Wi-Fi phase. Leaves `in` live.
template <class Shot>
static void wifiShots(Shot shot, PanelInput& in, PanelUi& ui, const Fleet& f, uint32_t ms) {
  static NetView net;
  net = NetView();
  net.via = NetVia::Wifi;
  net.wifiCapable = true;
  snprintf(net.ssid, sizeof net.ssid, "my network");
  in.net = &net;
  in.fleet = &f;
  in.link = LinkState::Live;
  ui.wifi = true;
  shot("home-wifi");
  uiToast(ui, "paired for Wi-Fi", ms);
  shot("home-wifi-toast");
  ui.toast[0] = '\0';
  const struct { const char* name; NetPhase phase; } kWifi[] = {
      {"wifi-joining", NetPhase::Joining},             {"wifi-connecting", NetPhase::Connecting},
      {"wifi-wrong-password", NetPhase::WrongPassword}, {"wifi-no-network", NetPhase::NoNetwork},
      {"wifi-unreachable", NetPhase::DeckUnreachable},  {"wifi-cert-rejected", NetPhase::CertRejected},
  };
  in.link = LinkState::Connecting;
  in.fleet = nullptr;
  for (const auto& w : kWifi) {
    net.phase = w.phase;
    shot(w.name);
  }
  net.phase = NetPhase::Joining;
  uiToast(ui, "trying Wi-Fi\xE2\x80\xA6", ms);
  shot("wifi-joining-toast");
  ui.toast[0] = '\0';
  in.link = LinkState::Live;
  in.fleet = &f;
  in.net = nullptr;
  ui.wifi = false;
}

// The square boards: every screen of one spec.
static int renderSquare(const std::string& dir, const SquareSpec& S, const char* device, DeckFleet& deck, uint32_t ms,
                        int64_t now) {
  const SquarePanel P(S);
  std::vector<uint16_t> buf(S.w * S.h);
  Canvas c{buf.data(), S.w, S.h};
  const Fleet& f = deck.fleet;
  PanelUi ui;
  PanelInput in{&f, LinkState::Live, device, DeckScope::Admin, ms, now, &ui, 0};
  auto shot = [&](const char* name) {
    P.draw(c, in);
    writePpm(dir + "/" + name + ".ppm", c);
  };
  shot("home");

  if (S.home == HomeKind::List) {
    // Mid-scroll, the finger down: the position bar shows.
    ui.scroll = 150;
    ui.dragging = true;
    shot("home-scrolling");
    ui.dragging = false;
    ui.scroll = 2 * rowPitch(S);
    ui.settledMs = ms;
    shot("home-settled");
    ui.scroll = 0;
    ui.settledMs = 0;
  } else {
    // A second page: one more agent beyond the first page's slots.
    static DeckFleet more;
    more = deck;
    Agent a = f.agents[0];
    snprintf(a.id, sizeof a.id, "p2more");
    snprintf(a.title, sizeof a.title, "release-notes");
    a.slot = 17;
    a.status = Status::Waiting;
    a.subagents = 0;
    deck_model_detail::upsert(more.fleet, a);
    more.fleet.total++;
    in.fleet = &more.fleet;
    ui.page = 1;
    shot("home-page2");
    ui.page = 0;
    in.fleet = &f;
  }

  auto open = [&](const char* id, Screen s) {
    snprintf(ui.openId, sizeof ui.openId, "%s", id);
    ui.screen = s;
  };
  open("k3f9a2", Screen::Card);
  shot("card");
  ui.down = true;                                  // stop held half way
  ui.pressed = PanelHit{Hit::Stop, findAgent(f, "k3f9a2"), -1};
  ui.downMs = ms - 500;
  shot("card-stop-held");
  ui.down = false;
  ui.pressed = PanelHit();
  in.scope = DeckScope::Act;
  open("scr4tc", Screen::Card);
  shot("card-idle-act");
  in.scope = DeckScope::View;
  open("k3f9a2", Screen::Card);
  shot("card-view");
  in.scope = DeckScope::Admin;
  open("1nfr4t", Screen::Card);
  shot("card-error");
  open("h0m3l4", Screen::Prompt);
  shot("permission");
  ui.pressed = PanelHit{Hit::Option, findAgent(f, "h0m3l4"), 0};
  shot("permission-pressed");
  ui.pressed = PanelHit();
  open("4uthfl", Screen::Prompt);
  shot("question");
  ui.pressed = PanelHit{Hit::Option, findAgent(f, "4uthfl"), 1};
  shot("question-pressed");
  ui.pressed = PanelHit();
  ui.screen = Screen::Home;
  ui.openId[0] = '\0';
  uiToast(ui, "sent", ms);
  shot("toast");
  ui.toast[0] = '\0';
  wifiShots(shot, in, ui, f, ms);

  in.link = LinkState::Lost;
  shot("home-offline");
  static Fleet empty;
  snprintf(empty.server, sizeof empty.server, "workstation");
  in.silentSec = 120;
  const struct { const char* name; LinkState link; const Fleet* f; } kNotices[] = {
      {"connecting", LinkState::Connecting, nullptr}, {"not-approved", LinkState::Unapproved, nullptr},
      {"no-agents", LinkState::Live, &empty},           {"offline", LinkState::Lost, nullptr},
  };
  for (const auto& n : kNotices) {
    in.link = n.link;
    in.fleet = n.f;
    shot(n.name);
  }
  return 0;
}

// The big panels: every screen at one scale.
static int renderBig(const std::string& dir, const BigSpec& S, const char* device, DeckFleet& deck, uint32_t ms,
                     int64_t now) {
  const BigPanel P(S);
  std::vector<uint16_t> buf(S.w * S.h);
  Canvas c{buf.data(), S.w, S.h};
  const Fleet& f = deck.fleet;
  PanelUi ui;
  PanelInput in{&f, LinkState::Live, device, DeckScope::Admin, ms, now, &ui, 0};
  auto shot = [&](const char* name) {
    P.draw(c, in);
    writePpm(dir + "/" + name + ".ppm", c);
  };
  shot("home");
  {
    static DeckFleet more;
    more = deck;
    Agent a = f.agents[0];
    snprintf(a.id, sizeof a.id, "p2more");
    snprintf(a.title, sizeof a.title, "release-notes");
    a.slot = 17;
    a.status = Status::Waiting;
    a.subagents = 0;
    deck_model_detail::upsert(more.fleet, a);
    more.fleet.total++;
    in.fleet = &more.fleet;
    shot("home-paged");
    ui.page = 1;
    shot("home-page2");
    ui.page = 0;
    in.fleet = &f;
  }
  auto open = [&](const char* id) { bigOpen(ui, f, findAgent(f, id)); };
  open("k3f9a2");
  shot("card");
  ui.pressed = PanelHit{Hit::Focus, findAgent(f, "k3f9a2"), -1};
  shot("card-focus-pressed");
  ui.pressed = PanelHit();
  open("scr4tc");
  shot("card-idle");
  ui.stopArmedUntil = ms + 2000;
  shot("card-stop-armed");
  in.scope = DeckScope::Act;
  open("scr4tc");
  shot("card-idle-act");
  in.scope = DeckScope::View;
  open("k3f9a2");
  shot("card-view");
  in.scope = DeckScope::Admin;
  open("1nfr4t");
  shot("card-error");
  open("h0m3l4");
  shot("permission");
  open("4uthfl");
  shot("question");
  ui.selected = 1;
  shot("question-selected");
  bigHome(ui);
  uiToast(ui, "sent", ms);
  shot("toast");
  ui.toast[0] = '\0';
  wifiShots(shot, in, ui, f, ms);
  {                                                // on Wi-Fi with two pages: the mark beside the bars
    static DeckFleet more;
    more = deck;
    Agent a = f.agents[0];
    snprintf(a.id, sizeof a.id, "p2more");
    a.slot = 17;
    deck_model_detail::upsert(more.fleet, a);
    static NetView net;
    net.via = NetVia::Wifi;
    in.net = &net;
    in.fleet = &more.fleet;
    ui.wifi = true;
    shot("home-wifi-paged");
    ui.wifi = false;
    in.net = nullptr;
    in.fleet = &f;
  }
  in.link = LinkState::Lost;
  shot("home-offline");
  static Fleet empty;
  snprintf(empty.server, sizeof empty.server, "workstation");
  in.silentSec = 120;
  const struct { const char* name; LinkState link; const Fleet* f; } kNotices[] = {
      {"connecting", LinkState::Connecting, nullptr}, {"not-approved", LinkState::Unapproved, nullptr},
      {"no-agents", LinkState::Live, &empty},           {"offline", LinkState::Lost, nullptr},
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
  if (layout == "lcd4") return renderBig(dir, specBig480(), "waveshare-touch-lcd-2a5390", deck, ms, now);
  if (layout == "lcd4-small") return renderSquare(dir, specLcd4(), "waveshare-touch-lcd-2a5390", deck, ms, now);
  if (layout == "amoled18") return renderSquare(dir, specAmoled18(), "waveshare-touch-amol-fea2ac", deck, ms, now);
  if (layout == "t4s3") return renderSquare(dir, specT4S3(), "t4s3-panel-a1b2c3", deck, ms, now);
  if (layout == "freenove28") return renderSquare(dir, specFreenove28(), "freenove-s3-28-c0ffee", deck, ms, now);
  if (layout == "orbit") {
    static const OrbitPanel orbit(layoutOrbit28());
    return round_shots::render(dir, orbit, "waveshare-touch-lcd-28c-3c71bf", fleet, ms, now);
  }
  if (layout == "dial") {
    static const DialPanel dial(layoutDial21());
    return round_shots::render(dir, dial, "waveshare-touch-lcd-21-8e04d2", fleet, ms, now);
  }
  if (layout == "spring" || layout == "spring28") {
    static const SpringPanel s21(layoutSpring21()), s28(layoutSpring28());
    const SpringPanel& spring = layout == "spring" ? s21 : s28;
    const char* dev = layout == "spring" ? "waveshare-touch-lcd-21-8e04d2" : "waveshare-touch-lcd-28c-3c71bf";
    const int r = round_shots::render(dir, spring, dev, fleet, ms, now);
    return r ? r : round_shots::renderSpring(dir, spring, dev, fleet, ms, now);
  }
  if (layout == "selftest28")
    return round_shots::renderSelftest(dir, "waveshare-touch-lcd-28c", "tca9554 ok" DECK_DOT "gt911 0x5D", 16000000);
  if (layout == "selftest21")
    return round_shots::renderSelftest(dir, "waveshare-touch-lcd-21", "tca9554 ok" DECK_DOT "cst820 ok", 16000000);
  if (layout != "147") {
    fprintf(stderr, "unknown layout %s (147, lcd4, amoled18, t4s3, freenove28, orbit, dial, spring, spring28, selftest28, selftest21)\n", layout.c_str());
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
