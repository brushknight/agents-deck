// A round board on the host: the Deck Link kit, the adapter, the round state
// machine (round_view.h) and a layout (Orbit, Dial or the spring), with stdin as the
// serial RX and stdout as TX (binary frames only). For running the round
// boards' logic against a real deck without hardware, e.g. from a Go test
// with the daemon's link.Session on the other end
// (backend/internal/link/round_test.go).
//
//   out/round_sim [--layout orbit|dial|spring|spring28] [--board-id ID] [--type T] [--seconds N]
//                 [--answer] [--ppm FILE]
//
// --answer plays a person answering the featured prompt: once the board is
// live with scope act and a waiting agent is featured, it taps the middle
// (which opens the prompt), then the first pill.
//
// stderr, one line per event:
//   link state=<connecting|unapproved|live|lost> device=<name> scope=<view|act|admin>
//   featured id=<id> status=<word>
//   screen=<home|detail|prompt> agent=<id>
//   tap x=<x> y=<y>
//   command <answer|focus|interrupt|dismiss> agent=<id> prompt=<id> key=<key> seq=<n>
//   result seq=<n> code=<code>
// With --ppm the last frame drawn is written to FILE when it exits.
#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <time.h>
#include <unistd.h>

#include <fstream>
#include <string>
#include <vector>

#include "deck_model.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "layout_spring.h"

static DeckFleet deckFleet;
static RoundUi ui;
static char device[40] = "";

static uint32_t nowMs() {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (uint32_t)((uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u);
}

static size_t writeOut(const uint8_t* d, size_t n, void*) {
  size_t done = 0;
  while (done < n) {
    const ssize_t w = write(STDOUT_FILENO, d + done, n - done);
    if (w < 0 && errno == EINTR) continue;
    if (w <= 0) break;
    done += (size_t)w;
  }
  return done;
}

static uint32_t millisCb(void*) { return nowMs(); }

static void onFrame(deck_link_t*, const deck_frame_t* f) {
  deckApply(deckFleet, *f, nowMs());
  deckFleet.newWaiting = false;   // round_view.h spots new waiters itself
}

static void onWelcome(deck_link_t*, const char* name, agentsdeck_link_v1_Scope, bool) {
  snprintf(device, sizeof device, "%s", name);
}

static void onResult(deck_link_t*, uint32_t seq, uint32_t code, const char*) {
  fprintf(stderr, "result seq=%u code=%u\n", (unsigned)seq, (unsigned)code);
  roundResult(ui, seq, code, nowMs());
}

static const char* linkName(LinkState s) {
  switch (s) {
    case LinkState::Connecting: return "connecting";
    case LinkState::Unapproved: return "unapproved";
    case LinkState::Live:       return "live";
    default:                    return "lost";
  }
}

static const char* statusWord(Status s) {
  static const char* const k[] = {"starting", "running", "waiting", "idle", "error", "exited", "unknown"};
  return k[(int)s];
}

static const char* screenName(RoundScreen s) {
  return s == RoundScreen::Home ? "home" : s == RoundScreen::Detail ? "detail" : "prompt";
}

static const char* scopeName(RoundScope s) {
  return s == RoundScope::Admin ? "admin" : s == RoundScope::Act ? "act" : "view";
}

static RoundScope scopeOf(agentsdeck_link_v1_Scope s) {
  switch (s) {
    case agentsdeck_link_v1_Scope_SCOPE_ADMIN: return RoundScope::Admin;
    case agentsdeck_link_v1_Scope_SCOPE_ACT:   return RoundScope::Act;
    default:                                   return RoundScope::View;
  }
}

static uint32_t send(deck_link_t* link, const RoundCommand& c) {
  uint32_t seq = 0;
  const char* name = "";
  switch (c.cmd) {
    case RoundCmd::Answer:    seq = deck_answer(link, c.agent, c.prompt, c.key); name = "answer"; break;
    case RoundCmd::Focus:     seq = deck_focus(link, c.agent); name = "focus"; break;
    case RoundCmd::Interrupt: seq = deck_interrupt(link, c.agent); name = "interrupt"; break;
    case RoundCmd::Dismiss:   seq = deck_dismiss(link, c.agent); name = "dismiss"; break;
    default: return 0;
  }
  fprintf(stderr, "command %s agent=%s prompt=%s key=%s seq=%u\n", name, c.agent, c.prompt[0] ? c.prompt : "-",
          c.key[0] ? c.key : "-", (unsigned)seq);
  return seq;
}

static void writePpm(const char* path, const Canvas& c) {
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
  const char* boardId = "sim-round-0001";
  const char* type = "waveshare-touch-lcd-28c";
  const char* ppm = nullptr;
  std::string layoutName = "orbit";
  long seconds = 0;
  bool answer = false;
  for (int i = 1; i < argc; i++) {
    const std::string a = argv[i];
    const bool hasValue = i + 1 < argc;
    if (a == "--answer") answer = true;
    else if (a == "--board-id" && hasValue) boardId = argv[++i];
    else if (a == "--type" && hasValue) type = argv[++i];
    else if (a == "--layout" && hasValue) layoutName = argv[++i];
    else if (a == "--seconds" && hasValue) seconds = atol(argv[++i]);
    else if (a == "--ppm" && hasValue) ppm = argv[++i];
    else { fprintf(stderr, "round_sim: unknown flag %s\n", argv[i]); return 2; }
  }
  static const OrbitPanel orbit(layoutOrbit28());
  static const DialPanel dial(layoutDial21());
  static const SpringPanel spring(layoutSpring21()), spring28(layoutSpring28());
  const RoundLayout& L = layoutName == "dial"       ? (const RoundLayout&)dial
                         : layoutName == "spring"   ? (const RoundLayout&)spring
                         : layoutName == "spring28" ? (const RoundLayout&)spring28
                                                    : (const RoundLayout&)orbit;
  signal(SIGPIPE, SIG_IGN);
  setvbuf(stderr, nullptr, _IOLBF, 0);

  static deck_link_t link;
  deck_link_config_t cfg;
  memset(&cfg, 0, sizeof cfg);
  panelConfig(cfg, boardId, "sim", PanelSpec{type, 480, 480, true});
  cfg.input_count = 1;
  cfg.inputs[0] = agentsdeck_link_v1_Input_INPUT_TOUCH;
  cfg.write = writeOut;
  cfg.millis = millisCb;
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  deck_link_init(&link, &cfg);
  deck_link_begin(&link);

  std::vector<uint16_t> buf(480 * 480);
  Canvas canvas{buf.data(), 480, 480};
  const uint32_t start = nowMs();
  ui.lastTouchMs = start;
  std::string lastLink, lastFeatured, lastScreen;
  // --answer: 0 waiting for a featured waiter, 1 tapped the middle, 2 tapped a pill.
  int step = 0;
  uint32_t stepAt = 0;
  for (;;) {
    const uint32_t ms = nowMs();
    if (seconds > 0 && ms - start >= (uint32_t)seconds * 1000u) break;
    struct pollfd pfd = {STDIN_FILENO, POLLIN, 0};
    if (poll(&pfd, 1, 20) > 0) {
      uint8_t in[1024];
      const ssize_t n = read(STDIN_FILENO, in, sizeof in);
      if (n == 0) break;
      if (n > 0) deck_link_feed(&link, in, (size_t)n);
    }
    deck_link_poll(&link);

    const LinkState ls = linkStateOf(deck_link_state(&link), deck_link_approved(&link));
    const Fleet* f = deckFleet.have ? &deckFleet.fleet : nullptr;
    const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
    const RoundScope scope = scopeOf(deck_link_scope(&link));
    const int64_t now = deckNowEpoch(deckFleet, ms);
    if (f) {
      roundSync(ui, L, *f, ms);
      roundTick(ui, L, *f, ms);
      ui.wake = false;
      roundAnimate(ui, *f, scope, ms);
    }

    // A person answering: tap the middle, then the first pill.
    if (answer && f && shown == LinkState::Live && canAct(scope) && ms - stepAt > 300) {
      auto tapAt = [&](int x, int y) {
        fprintf(stderr, "tap x=%d y=%d\n", x, y);
        roundTouch(ui, L, *f, true, x, y, scope, now, ms);
        const RoundCommand c = roundTouch(ui, L, *f, false, 0, 0, scope, now, ms + 50);
        const uint32_t seq = send(&link, c);
        if (seq) ui.cmdSeq = seq;
        stepAt = ms;
      };
      const int fi = roundFeatured(ui, *f);
      if (step == 0 && ui.screen == RoundScreen::Home && fi >= 0 && f->agents[fi].status == Status::Waiting) {
        tapAt(240, 240);
        step = 1;
      } else if (step == 1 && ui.screen == RoundScreen::Prompt) {
        const int i = findAgent(*f, ui.openId);
        Pill p[4];
        if (i >= 0 && promptPills(f->agents[i], ui.optFirst, p) > 0) {
          tapAt(p[0].x + p[0].w / 2, p[0].y + p[0].h / 2);
          step = 2;
        }
      }
    }

    RoundInput in{f, shown, device, scope, ms, now, &ui, 0};
    in.partial = true;   // as the board: the canvas keeps the last frame
    L.draw(canvas, in);

    char line[160];
    snprintf(line, sizeof line, "link state=%s device=%s scope=%s", linkName(shown), device[0] ? device : "-",
             scopeName(scope));
    if (line != lastLink) { fprintf(stderr, "%s\n", line); lastLink = line; }
    if (f) {
      const int fi = roundFeatured(ui, *f);
      snprintf(line, sizeof line, "featured id=%s status=%s", fi >= 0 ? f->agents[fi].id : "-",
               fi >= 0 ? statusWord(f->agents[fi].status) : "-");
      if (line != lastFeatured) { fprintf(stderr, "%s\n", line); lastFeatured = line; }
      snprintf(line, sizeof line, "screen=%s agent=%s", screenName(ui.screen), ui.openId[0] ? ui.openId : "-");
      if (line != lastScreen) { fprintf(stderr, "%s\n", line); lastScreen = line; }
    }
  }
  if (ppm) writePpm(ppm, canvas);
  return 0;
}
