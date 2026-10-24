// The panel's logic on the host: the Deck Link kit, the adapter, the view and
// the screen, with stdin as the serial RX and stdout as TX (binary frames
// only). For running the board against a real deck without hardware, e.g.
// from a Go test with the daemon's link.Session on the other end.
//
// tests/run.sh builds it as out/panel_sim (with the kit's C sources).
//   out/panel_sim [--board-id ID] [--seconds N] [--ppm FILE] [--press-at-ms N]
//
// stderr, one line per change:
//   link state=<connecting|unapproved|live|lost> device=<name>
//   fleet count=<n> total=<n> hero=<id or -> status=<word> held=<0|1>
// With --ppm the last frame drawn is written to FILE when it exits.
// --press-at-ms N presses the BOOT button once, N ms after start.
#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <time.h>
#include <unistd.h>

#include <fstream>
#include <string>
#include <vector>

#include "deck_model.h"
#include "layout_147.h"

static DeckFleet deckFleet;
static ViewState view;
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
  if (deckFleet.newWaiting) {
    deckFleet.newWaiting = false;
    view.hold = false;
  }
}

static void onWelcome(deck_link_t*, const char* name, agentsdeck_link_v1_Scope, bool) {
  snprintf(device, sizeof device, "%s", name);
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
  const char* boardId = "sim-panel-0001";
  const char* ppm = nullptr;
  long seconds = 0, pressAt = -1;
  for (int i = 1; i + 1 < argc; i += 2) {
    const std::string a = argv[i];
    if (a == "--board-id") boardId = argv[i + 1];
    else if (a == "--seconds") seconds = atol(argv[i + 1]);
    else if (a == "--ppm") ppm = argv[i + 1];
    else if (a == "--press-at-ms") pressAt = atol(argv[i + 1]);
    else { fprintf(stderr, "panel_sim: unknown flag %s\n", argv[i]); return 2; }
  }
  signal(SIGPIPE, SIG_IGN);
  setvbuf(stderr, nullptr, _IOLBF, 0);

  static deck_link_t link;
  deck_link_config_t cfg;
  memset(&cfg, 0, sizeof cfg);
  panelConfig(cfg, boardId, "sim");
  cfg.write = writeOut;
  cfg.millis = millisCb;
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  deck_link_init(&link, &cfg);
  deck_link_begin(&link);

  std::vector<uint16_t> buf(kScreenW * kScreenH);
  Canvas canvas{buf.data(), kScreenW, kScreenH};
  const uint32_t start = nowMs();
  std::string lastLink, lastFleet;
  bool pressed = false;
  for (;;) {
    const uint32_t ms = nowMs();
    if (seconds > 0 && ms - start >= (uint32_t)seconds * 1000u) break;
    struct pollfd pfd = {STDIN_FILENO, POLLIN, 0};
    if (poll(&pfd, 1, 50) > 0) {
      uint8_t in[512];
      const ssize_t n = read(STDIN_FILENO, in, sizeof in);
      if (n == 0) break;
      if (n > 0) deck_link_feed(&link, in, (size_t)n);
    }
    deck_link_poll(&link);
    if (!pressed && pressAt >= 0 && ms - start >= (uint32_t)pressAt && deckFleet.have) {
      pressed = true;
      pressNext(deckFleet.fleet, view, ms);   // the BOOT button: show the next agent
    }

    // The same as the board's renderNow().
    const LinkState ls = linkStateOf(deck_link_state(&link), deck_link_approved(&link));
    const Fleet* f = deckFleet.have ? &deckFleet.fleet : nullptr;
    const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
    const int hero = f ? resolveHero(*f, view, ms) : -1;
    drawScreen(canvas, {f, hero, shown, device, ms, deckNowEpoch(deckFleet, ms), view.hold});

    char line[160];
    snprintf(line, sizeof line, "link state=%s device=%s", linkName(shown), device[0] ? device : "-");
    if (line != lastLink) { fprintf(stderr, "%s\n", line); lastLink = line; }
    if (f) {
      snprintf(line, sizeof line, "fleet count=%u total=%u hero=%s status=%s held=%d", (unsigned)f->count,
               (unsigned)f->total, hero >= 0 ? f->agents[hero].id : "-", hero >= 0 ? statusWord(f->agents[hero].status) : "-",
               view.hold ? 1 : 0);
      if (line != lastFleet) { fprintf(stderr, "%s\n", line); lastFleet = line; }
    }
  }
  if (ppm) writePpm(ppm, canvas);
  return 0;
}
