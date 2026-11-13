// The panel's logic on the host: the Deck Link kit, the adapter, the view and
// the screen, with stdin as the serial RX and stdout as TX (binary frames
// only). For running the board against a real deck without hardware, e.g.
// from a Go test with the daemon's link.Session on the other end.
//
// tests/run.sh builds it as out/panel_sim (with the kit's C sources).
//   out/panel_sim [--board-id ID] [--seconds N] [--ppm FILE] [--press-at-ms N] [--board 147|tds3] [--answer]
//
// stderr, one line per change:
//   link state=<connecting|unapproved|live|lost> device=<name>
//   fleet count=<n> total=<n> hero=<id or -> status=<word> held=<0|1>
// With --ppm the last frame drawn is written to FILE when it exits.
// --press-at-ms N presses the BOOT button once, N ms after start (with
// --board tds3, the LilyGO T-Display-S3's lower button: the next agent too).
// --answer (tds3) plays a person tapping the waiting hero's primary option
// once the board may act, and logs it:
//   tap x=<x> y=<y>
//   command answer agent=<id> prompt=<id> key=<key> seq=<n>
//   result seq=<n> code=<code>
// With tds3 the link line also says scope=<view|act|admin>.
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
#include "layout_tds3.h"

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

static void onResult(deck_link_t*, uint32_t seq, uint32_t code, const char*) {
  fprintf(stderr, "result seq=%u code=%u\n", (unsigned)seq, (unsigned)code);
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
  bool tds3Board = false, answer = false;
  for (int i = 1; i < argc; i++) {
    const std::string a = argv[i];
    const bool hasValue = i + 1 < argc;
    if (a == "--answer") answer = true;
    else if (a == "--board-id" && hasValue) boardId = argv[++i];
    else if (a == "--seconds" && hasValue) seconds = atol(argv[++i]);
    else if (a == "--ppm" && hasValue) ppm = argv[++i];
    else if (a == "--press-at-ms" && hasValue) pressAt = atol(argv[++i]);
    else if (a == "--board" && hasValue) tds3Board = std::string(argv[++i]) == "tds3";
    else { fprintf(stderr, "panel_sim: unknown flag %s\n", argv[i]); return 2; }
  }
  signal(SIGPIPE, SIG_IGN);
  setvbuf(stderr, nullptr, _IOLBF, 0);

  static deck_link_t link;
  deck_link_config_t cfg;
  memset(&cfg, 0, sizeof cfg);
  if (tds3Board) panelConfig(cfg, boardId, "sim", PanelSpec{"lilygo-t-display-s3", tds3::kW, tds3::kH, false, 2});
  else panelConfig(cfg, boardId, "sim");
  cfg.write = writeOut;
  cfg.millis = millisCb;
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  deck_link_init(&link, &cfg);
  deck_link_begin(&link);

  const int W = tds3Board ? tds3::kW : kScreenW, H = tds3Board ? tds3::kH : kScreenH;
  std::vector<uint16_t> buf(W * H);
  Canvas canvas{buf.data(), W, H};
  tds3::Ui ui;
  bool answered = false;
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
      if (tds3Board) tds3::key(ui, deckFleet.fleet, view, tds3::Key::Next, false, DeckScope::View, ms);
      else pressNext(deckFleet.fleet, view, ms);   // the BOOT button: show the next agent
    }

    // The same as the board's renderNow().
    const LinkState ls = linkStateOf(deck_link_state(&link), deck_link_approved(&link));
    const Fleet* f = deckFleet.have ? &deckFleet.fleet : nullptr;
    const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
    const int hero = f ? resolveHero(*f, view, ms) : -1;
    const DeckScope scope = deckScope(deck_link_scope(&link));
    // --answer: a finger on the waiting hero's primary option, down and up.
    if (tds3Board && answer && !answered && shown == LinkState::Live && hero >= 0 &&
        tds3::promptShown(f->agents[hero], scope, &ui)) {
      tds3::Button b[tds3::kMaxButtons];
      const int n = tds3::buttons(f->agents[hero], &ui, b);
      for (int i = 0; i < n; i++) {
        if (b[i].opt < 0 || !f->agents[hero].opts[b[i].opt].primary) continue;
        const int x = b[i].x + b[i].w / 2, y = b[i].y + b[i].h / 2;
        fprintf(stderr, "tap x=%d y=%d\n", x, y);
        tds3::touch(ui, *f, view, true, x, y, scope, ms);
        const DeckCommand c = tds3::touch(ui, *f, view, false, x, y, scope, ms + 60);
        if (c.cmd == DeckCmd::Answer) {
          const uint32_t seq = deck_answer(&link, c.agent, c.prompt, c.key);
          fprintf(stderr, "command answer agent=%s prompt=%s key=%s seq=%u\n", c.agent, c.prompt, c.key, (unsigned)seq);
        }
        answered = true;
        break;
      }
    }
    if (tds3Board) {
      tds3::noteHero(ui, shown == LinkState::Live || shown == LinkState::Lost ? f : nullptr, hero, ms);
      tds3::draw(canvas, {f, hero, shown, device, ms, deckNowEpoch(deckFleet, ms), view.hold, scope, &ui});
    } else {
      drawScreen(canvas, {f, hero, shown, device, ms, deckNowEpoch(deckFleet, ms), view.hold});
    }

    char line[160];
    snprintf(line, sizeof line, "link state=%s device=%s", linkName(shown), device[0] ? device : "-");
    if (tds3Board && shown == LinkState::Live) {
      static const char* const kScope[] = {"view", "act", "admin"};
      snprintf(line + strlen(line), sizeof line - strlen(line), " scope=%s", kScope[(int)scope]);
    }
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
