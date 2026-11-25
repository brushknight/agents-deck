#pragma once
// The firmware of a grid board, minus the board: Deck Link on the native USB
// port (or Wi-Fi, net_link.h), the fleet, the touch UI and the frame loop. A
// device's main.cpp fills a GridBoard with its display, touch and backlight
// drivers and calls gridAppSetup() and gridAppLoop(). Arduino only (the rest
// of panel-core runs on the host too). round_app.h is the same for the round
// boards, and wires net_link the same way.
#if defined(ARDUINO)
#include <Arduino.h>
#include <DeckLink.h>
#include <esp_heap_caps.h>

#include <new>

#include "deck_model.h"
#include "panel_layout.h"

// Bring-up builds (-DPANEL_DEBUG) print one-line notes on the USB port, between
// Deck Link frames (the deck ignores text). Release builds print nothing.
#ifdef PANEL_DEBUG
#define PANEL_LOG(...) do { Serial.printf(__VA_ARGS__); Serial.print("\n"); } while (0)
#define NET_LOG(...) PANEL_LOG(__VA_ARGS__)
#else
#define PANEL_LOG(...) do {} while (0)
#endif
#include "net_link.h"

struct GridBoard {
  const char* type;                        // Hello.type, e.g. "waveshare-touch-lcd-4"
  const PanelLayout* layout;               // e.g. a static SquarePanel(specLcd4())
  bool (*begin)();                         // display, touch, backlight; false: the display didn't start
  void (*push)(const uint16_t* frame);     // a whole RGB565 frame of the layout's width x height
  void (*pushRows)(const uint16_t* const* rows);   // the same, one pointer per row (nullptr: the board can't)
  bool (*frameSwapped)();                  // after begin(): the board takes frames byte-swapped (Canvas::be)
  // The motion sensor's next sample, in g, and when it was taken (false: none
  // waiting; nullptr: no sensor): fun_motion.h.
  bool (*accel)(float& x, float& y, float& z, uint32_t& ms);
  bool (*touch)(int& x, int& y);           // true while a finger is down, at (x, y) in frame pixels
  void (*brightness)(uint8_t level);       // 0..255
  int bootPin;                             // the BOOT button (active low), -1 for none
  uint8_t brightActive, brightIdle;
  const char* boardId;                     // Hello.board_id; nullptr: DeckLink::chipId() (the MAC, "AA:BB:...")
  // A board that keeps the frames itself (nullptr: grid_app draws into one
  // frame of its own and calls push): the frame to draw the next picture
  // into; push() then gets that same pointer back.
  uint16_t* (*frameBegin)();
  // A board that takes a frame in strips (0: whole frames): the frame is drawn
  // stripRows rows at a time into internal RAM, which is much faster to draw
  // into than PSRAM, and each strip goes to pushStrip (rows y0..y0+rows-1;
  // `last`: the frame is complete). While the board's panel slides, whole
  // frames again (its pictures are whole frames, frameRows).
  int stripRows;
  void (*pushStrip)(const uint16_t* px, int y0, int rows, bool last);
  // A board that composes a frame from two pictures side by side as it sends
  // it (home's pages sliding, page_slide.h; nullptr: the layout composes it on the canvas).
  void (*pushSplit)(const FrameSplit& s);
};

namespace grid_app {

constexpr size_t kRxBuffer = 32768;

static GridBoard board;
static DeckLink deck;
static DeckFleet* fleet = nullptr;         // in PSRAM: up to PANEL_MAX_AGENTS agents, twice
static uint16_t* frame = nullptr;          // in PSRAM
static uint16_t* strip = nullptr;          // in internal RAM: board.stripRows rows (nullptr: whole frames)
static Canvas canvas{nullptr, 0, 0};
static PanelUi ui;
static char deviceName[40] = "";
static bool dirty = true;
static bool moving = false;               // the layout animates: draw at full rate
static uint32_t lastActiveMs = 0;
static uint32_t lastHeardMs = 0;          // the last frame from the deck
static const char* firmwareVersion = "";
static const char* boardId = "";
static uint32_t restartAt = 0;            // the panel's restart: when to do it (0: never)

// The link the screen follows: USB, or Wi-Fi once that is up (net_link.h).
static deck_link_t* link() { return net_link::active(); }

static LinkState linkState() {
  if (net_link::wifiPending()) return LinkState::Connecting;   // the notice says where Wi-Fi is
  return linkStateOf(deck_link_state(link()), deck_link_approved(link()));
}

static DeckScope scope() {
  switch (deck_link_scope(link())) {
    case agentsdeck_link_v1_Scope_SCOPE_ADMIN: return DeckScope::Admin;
    case agentsdeck_link_v1_Scope_SCOPE_ACT:   return DeckScope::Act;
    default:                                   return DeckScope::View;
  }
}

static void onMagic(const agentsdeck_link_v1_Magic& m);

static void onFrame(deck_link_t* ctx, const deck_frame_t* f) {
  if (ctx != link()) return;               // the other link (USB while on Wi-Fi): not what the screen shows
  lastHeardMs = millis();
  if (f->which_body == agentsdeck_link_v1_Frame_magic_tag) onMagic(f->body.magic);   // before the agent changes it brings
  if (deckApply(*fleet, *f, millis())) dirty = true;
  if (fleet->newWaiting) fleet->newWaiting = false;
}

// Its part in the magic trick (Settings on the deck). The giver, taken:
// keep the frame it shows and where its tiles are, to fly them out (the
// deck's removals follow this frame). Given back: fly the new ones in.
static uint16_t* giverPic = nullptr;
static void onMagic(const agentsdeck_link_v1_Magic& m) {
  FunUi& f = ui.fun;
  const uint32_t ms = millis() ? millis() : 1;
  f.role = (uint8_t)m.role;
  f.arrive = (uint8_t)m.arrive;
  f.leave = (uint8_t)m.leave;
  if (f.role != 2 && f.magic) {            // no longer the receiver: the trick ends here
    f.magic = f.taken = false;
    f.revealAt = f.returnAt = 0;
  }
  if (f.role != 1) {
    f.given = false;
    f.giveAt = f.backAt = 0;
  } else if (m.taken && !f.given) {
    int idx[16];
    f.picN = board.layout->shownTiles(ui, fleet->fleet, idx, f.picX, f.picY, 16);
    const size_t n = (size_t)canvas.w * canvas.h;
    if (!giverPic) giverPic = (uint16_t*)heap_caps_malloc(n * 2, MALLOC_CAP_SPIRAM);
    if (giverPic && canvas.px) memcpy(giverPic, canvas.px, n * 2);
    f.pic = giverPic;
    f.given = true;
    f.giveAt = ms;
    f.backAt = 0;
  } else if (!m.taken && f.given) {
    f.given = false;
    f.backAt = ms;
    f.giveAt = 0;
  }
  dirty = true;
}

static void onWelcome(deck_link_t* ctx, const char* name, agentsdeck_link_v1_Scope, bool approved) {
  // A deck on USB takes over at once (and pairs the board for Wi-Fi when it needs to).
  if (ctx == deck.core()) net_link::usbWelcome(approved, name);
  else net_link::netWelcome(ctx, approved, name);
  if (ctx == link()) snprintf(deviceName, sizeof deviceName, "%s", name);
  dirty = true;
}

static void onResult(deck_link_t* ctx, uint32_t seq, uint32_t code, const char*) {
  net_link::result(seq, code);
  if (ctx != link()) return;
  board.layout->result(ui, seq, code, millis());
  dirty = true;
}

static void onCertIssued(deck_link_t*, uint32_t seq, const agentsdeck_link_v1_CertIssued* c) {
  net_link::certIssued(seq, c);
}

// The screen's link changed (USB, Wi-Fi, Bluetooth): start the fleet and the screen
// over (home); the new link sends a snapshot. A toast stays.
static void onSwitched(NetVia via) {
  fleet->~DeckFleet();
  new (fleet) DeckFleet();
  PanelUi fresh;
  fresh.att = ui.att;                      // the "Needs you" spots stay (the agents are the same)
  memcpy(fresh.toast, ui.toast, sizeof fresh.toast);
  fresh.toastUntil = ui.toastUntil;
  fresh.ctl = ui.ctl;                      // the board's panel stays open (its switches move the link)
  ui = fresh;
  dirty = true;
  PANEL_LOG("panel: now on %s", via == NetVia::Wifi ? "Wi-Fi" : via == NetVia::Ble ? "Bluetooth" : "USB");
}

static void onToast(const char* text) {
  uiToast(ui, text, millis(), 2500);
  dirty = true;
}

static void send(const DeckCommand& c) {
  switch (c.cmd) {
    case DeckCmd::Answer:    ui.cmdSeq = deck_answer(link(), c.agent, c.prompt, c.key); break;
    case DeckCmd::Focus:     ui.cmdSeq = deck_focus(link(), c.agent); break;
    case DeckCmd::Interrupt: ui.cmdSeq = deck_interrupt(link(), c.agent); break;
    case DeckCmd::Dismiss:   ui.cmdSeq = deck_dismiss(link(), c.agent); break;
    case DeckCmd::Resume:    ui.cmdSeq = deck_resume(link(), c.agent); break;
#ifdef agentsdeck_link_v1_Frame_hatch_pet_tag
    case DeckCmd::Hatch:     ui.cmdSeq = deck_hatch_pet(link(), c.agent); break;
#endif
    default: return;
  }
  PANEL_LOG("command %d for %s, seq %u", (int)c.cmd, c.agent, (unsigned)ui.cmdSeq);
}

static void onLink(deck_link_t*) { dirty = true; }

#ifdef PANEL_DEBUG
static uint32_t dbgDrawUs = 0, dbgPushUs = 0, dbgFrames = 0, dbgPanelFrames = 0;   // the debug report: where a frame's time goes
#endif

// The frame: drawn in strips when the board takes them, else whole.
static void drawAndSend(const PanelInput& in) {
#ifdef PANEL_DEBUG
#define GRID_TIME(acc, stmt) do { const uint32_t t_ = micros(); stmt; acc += micros() - t_; } while (0)
#else
#define GRID_TIME(acc, stmt) stmt
#endif
  // The panel's pictures, and the pages' while they slide, are whole frames.
  const bool slides = ui.ctl.pulling || ui.ctl.animating || ui.sliding || ui.slideAnim;
  if (strip && !slides) {
    const int W = canvas.w, H = canvas.h, n = board.stripRows;
    for (int y0 = 0; y0 < H; y0 += n) {
      const int y1 = y0 + n < H ? y0 + n : H;
      Canvas sc{strip - (ptrdiff_t)y0 * W, W, y1};   // rows y0..y1-1 are the strip
      sc.top = y0;
      sc.be = canvas.be;
      GRID_TIME(dbgDrawUs, board.layout->draw(sc, in));
      GRID_TIME(dbgPushUs, board.pushStrip(strip, y0, y1 - y0, y1 == H));
    }
    return;
  }
  if (board.frameBegin) {
    if (uint16_t* f = board.frameBegin()) canvas.px = frame = f;
  }
  GRID_TIME(dbgDrawUs, board.layout->draw(canvas, in));
  if (const FrameSplit* sp = board.pushSplit ? board.layout->frameSplit() : nullptr) GRID_TIME(dbgPushUs, board.pushSplit(*sp));
  else if (const uint16_t* const* rows = board.pushRows ? board.layout->frameRows() : nullptr) GRID_TIME(dbgPushUs, board.pushRows(rows));
  else GRID_TIME(dbgPushUs, board.push(frame));
#undef GRID_TIME
}

static void render() {
  const uint32_t ms = millis();
  const LinkState ls = linkState();
  const Fleet* f = fleet->have ? &fleet->fleet : nullptr;
  // (Before the first snapshot the fleet is empty, and the board's panel can still be pulled down.)
  moving = board.layout->tick(ui, f ? *f : fleet->fleet, ms);
  // Approved but no snapshot yet: still connecting as far as the screen goes.
  const LinkState shown = ls == LinkState::Live && !f ? LinkState::Connecting : ls;
  static NetView net;
  net = net_link::view();
  // Home's Wi-Fi or Bluetooth mark (and the grid's row for it; touch follows the last frame).
  ui.wifi = net.via == NetVia::Wifi || (net.via == NetVia::Ble && net.ble.carrying);
  ui.ble = net.via == NetVia::Ble;
  // A firmware update, a passkey or "forget Bluetooth?": its notice (net_model.h's words) instead of the agents.
  const bool notice = net.ota.phase != OtaPhase::None || netOverlay(net);
  canvas.be = board.frameSwapped && board.frameSwapped();   // the board may fall back to plain byte order
  // The board's own panel (control_panel.h), when it is pulled down.
  static ControlInfo ci;
  ci.name = deviceName;
  ci.bleName = net.ble.name;
  ci.board = board.type;
  ci.firmware = firmwareVersion;
  ci.id = boardId;
  ci.ssid = net.ssid;
  ci.via = net.via == NetVia::Ble || net.ble.carrying ? NetVia::Ble : net.via;
  ci.linked = ls == LinkState::Live;
  ci.uplink = net.uplink;
  ci.wifiCapable = net.wifiCapable;
  ci.wifiOn = net_link::wifiSwitch();
  ci.wifiUp = net.via == NetVia::Wifi && net.phase == NetPhase::Online;
  ci.bleCapable = net.ble.capable;
  ci.bleOn = net_link::bleSwitch();
  ci.bleBonded = net_link::bleBonded();
  ci.upSec = ms / 1000;
  ci.imu = ui.fun.role == 2 && board.accel != nullptr;   // the Magic trick tile: when the deck made it the receiver
  ci.magicOn = ui.fun.magic;
  PanelInput in{f, notice ? LinkState::Connecting : shown, deviceName, scope(), ms, deckNowEpoch(*fleet, ms), &ui, (ms - lastHeardMs) / 1000, &net, &ci};
  in.rowsOk = board.pushRows != nullptr;
  in.splitOk = board.pushSplit != nullptr;
#ifdef PANEL_DEBUG
  dbgFrames++;
  if (controlShown(ui.ctl)) dbgPanelFrames++;
#endif
  drawAndSend(in);
}

// Bright while something happens or someone touched it in the last 30 s;
// dim when every agent is idle.
static void updateBrightness() {
  const uint32_t ms = millis();
  bool active = ms - lastActiveMs < 30000 || linkState() != LinkState::Live || !fleet->have ||
                ui.fun.magic;   // the magic trick is worked by knocks, not touches: it stays bright
  if (!active) {
    const Counts n = countFleet(fleet->fleet);
    active = n.running || n.waiting || n.error || n.hungry;
  }
  static int last = -1;
  const int level = active ? board.brightActive : board.brightIdle;
  if (level != last && board.brightness) { board.brightness((uint8_t)level); last = level; }
}

// What a touch on the board's panel asked for.
static void panelAct() {
  const ControlAct a = ui.ctl.act;
  ui.ctl.act = ControlAct::None;
  switch (a) {
    case ControlAct::Wifi: net_link::setWifiSwitch(!net_link::wifiSwitch()); break;
    case ControlAct::Ble:  net_link::setBleSwitch(!net_link::bleSwitch()); break;
    case ControlAct::Restart: restartAt = millis() + 400; break;   // "restarting" shows first
    case ControlAct::Magic:
      if (ui.fun.magic) {                      // off: the agents are back where they were
        if (ui.fun.taken) ui.fun.stashDue = true;
        ui.fun.magic = ui.fun.taken = false;
        ui.fun.revealAt = ui.fun.returnAt = 0;
      }
      else {
        ui.fun.magic = true;
        ui.fun.spent = false;
        ui.ctl = ControlUi();                  // on: the drawer gets out of the way, the grid is empty
      }
      break;
    default: break;
  }
}

static void touchPump() {
  int x = 0, y = 0;
  const bool down = board.touch && board.touch(x, y);
  if (down) ui.fun.lastTouch = millis();   // a finger on the glass is not a knock
  const NetView nv = net_link::view();
  const bool onNotice = !fleet->have || linkState() != LinkState::Live || nv.ota.phase != OtaPhase::None || netOverlay(nv);
  const uint32_t ms = millis();
  // The board's panel first: pulled down from the top edge, it works on every screen (home, cards, prompts, notices).
  if (!netOverlay(nv) && board.layout->panelTouch(ui, down, x, y, ms)) {
    net_link::touchPressed(false);
    lastActiveMs = ms;
    panelAct();
    dirty = true;
    return;
  }
  net_link::touchPressed(down && onNotice);   // a long press on a notice can forget the Bluetooth bond
  if (!down && !ui.down) return;
  lastActiveMs = ms;
  if (onNotice) {
    ui.down = false;
    return;
  }
  send(board.layout->touch(ui, fleet->fleet, down, x, y, scope(), ms));
  dirty = true;
}

static void buttonPump() {
  if (board.bootPin < 0) return;
  static bool down = false;
  static uint32_t changedAt = 0;
  const bool level = digitalRead(board.bootPin) == LOW;
  const uint32_t now = millis();
  net_link::bootPressed(down);                            // held 5 s: "forget Bluetooth?" (ble_model.h)
  if (level == down || now - changedAt < 30) return;      // debounce
  down = level;
  changedAt = now;
  if (down && controlShown(ui.ctl)) {                   // BOOT closes the board's panel
    ui.ctl = ControlUi();
    lastActiveMs = now;
    dirty = true;
  } else if (down && fleet->have) {
    board.layout->button(ui, fleet->fleet);
    lastActiveMs = now;
    dirty = true;
  }
}

}  // namespace grid_app

static inline void gridAppSetup(const GridBoard& b, const char* firmware) {
  using namespace grid_app;
  board = b;
  firmwareVersion = firmware;
  ota::begin();                             // an update waiting for its confirm? (before anything that could crash)
  const int W = b.layout->width(), H = b.layout->height();
  // The USB serial driver drops what doesn't fit in its RX queue (256 bytes
  // by default), and a frame takes ~50 ms to draw: a snapshot or a busy deck
  // would lose bytes. Room for well over a frame time of traffic.
  Serial.setRxBufferSize(kRxBuffer);
  Serial.begin(115200);                                     // native USB: the rate is ignored
  if (board.bootPin >= 0) pinMode(board.bootPin, INPUT_PULLUP);

  void* fm = heap_caps_calloc(1, sizeof(DeckFleet), MALLOC_CAP_SPIRAM);
  if (!fm) fm = calloc(1, sizeof(DeckFleet));
  fleet = new (fm) DeckFleet();
  // A board that keeps its frames has none here until begin().
  if (!b.frameBegin) frame = (uint16_t*)heap_caps_calloc((size_t)W * H, 2, MALLOC_CAP_SPIRAM);
  if (b.stripRows > 0 && b.pushStrip && b.frameBegin)
    strip = (uint16_t*)heap_caps_malloc((size_t)W * b.stripRows * 2, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
  // The "Needs you" screen (attention.h): the deck's spots as this board draws them, in PSRAM.
  void* am = heap_caps_calloc(1, sizeof(Attention), MALLOC_CAP_SPIRAM);
  if (!am) am = calloc(1, sizeof(Attention));
  if (am) {
    ui.att = new (am) Attention();
  }
  canvas = Canvas{frame, W, H};

  const bool shown = (frame || b.frameBegin) && board.begin();
  if (shown && b.frameBegin) canvas.px = frame = board.frameBegin();
  canvas.be = shown && board.frameSwapped && board.frameSwapped();
  PANEL_LOG("panel: %s, display %s, fleet %u B, frame %s, strips %s, psram free %u", b.type, shown ? "up" : "FAILED",
            (unsigned)sizeof(DeckFleet), frame ? "ok" : "none", strip ? "yes" : "no", (unsigned)ESP.getFreePsram());
  if (board.brightness) board.brightness(board.brightActive);

  DeckLinkConfig cfg = {};
#ifdef PANEL_DEBUG
  squareClock = [] { return (uint32_t)micros(); };
#endif
  boardId = b.boardId ? b.boardId : DeckLink::chipId();
  panelConfig(cfg, boardId, firmware, PanelSpec{b.type, (uint32_t)W, (uint32_t)H, true, b.accel != nullptr});
  cfg.on_frame = onFrame;
  cfg.on_welcome = onWelcome;
  cfg.on_result = onResult;
  cfg.on_online = onLink;
  cfg.on_offline = onLink;
  cfg.on_cert_issued = onCertIssued;
  cfg.on_wifi_config = net_link::wifiConfigHook();   // nullptr without Wi-Fi: the core answers 501
  cfg.on_firmware_offer = net_link::firmwareOfferHook();   // nullptr without Wi-Fi: the core refuses
  cfg.on_link_config = net_link::linkConfigHook();         // nullptr without Bluetooth: the core answers 501
  deck.begin(Serial, cfg);
  net_link::begin(deck.core(), cfg, net_link::Hooks{onSwitched, onToast});
  lastActiveMs = lastHeardMs = millis();
  if (shown) render();
}

static inline void gridAppLoop() {
  using namespace grid_app;
  // Drain what arrived while the last frame was drawn (each loop() reads up to 4 KB).
  for (int i = 0; i < 16; i++) {
    deck.loop();
    if (Serial.available() <= 0) break;
  }
  net_link::loop();                        // Wi-Fi: the plan, the TLS link and its frames
  if (restartAt && (int32_t)(millis() - restartAt) >= 0) ESP.restart();   // the board's panel asked
  touchPump();
  buttonPump();
  // The motion sensor: every sample since the last loop (the board reads it
  // every 2 ms on its own, so a short knock is not missed while a frame draws).
  if (board.accel) {
    float ax, ay, az;
    uint32_t at;
    for (int i = 0; i < 64 && board.accel(ax, ay, az, at); i++) {
      board.layout->motion(ui, ax, ay, az, at);
      if (ui.fun.revealAt || ui.fun.returnAt || ui.fun.sickAt) dirty = true;
    }
  }
  // The magic trick took the agents on this screen, or gave them back: the
  // deck leaves them out of every other screen's state meanwhile (POST /v1/stash).
  // The deck is told again each time the link comes up, and every 5 s while
  // (debug builds only): a word lost while the link was down must not leave the
  // wall panel's page empty.
  if (board.accel && ui.fun.role == 2) {   // the receiver
    static int told = -1;                  // what the deck last heard: 1 taken, 0 given back
    static uint32_t toldAt = 0;
    static bool wasLive = false;
    // USB when it is up (the deck closes a Wi-Fi link while USB carries the board), else the active link.
    deck_link_t* via = deck_link_state(deck.core()) == DECK_LINK_ONLINE ? deck.core() : link();
    const bool live = deck_link_state(via) == DECK_LINK_ONLINE && deck_link_approved(via);
    if (live && !wasLive) told = -1;
    wasLive = live;
    const int want = ui.fun.taken ? 1 : 0;
    ui.fun.stashDue = false;
    if (live && (told != want || millis() - toldAt > 5000)) {
      if (want) deck_stash_all(via);
      else deck_stash(via, nullptr, 0);
      if (told != want) PANEL_LOG("magic: %s", want ? "took the agents" : "gave them back");
      told = want;
      toldAt = millis();
    }
  }
  // The giver: tell the deck which agents it shows (what a take takes), when
  // that changes and every 5 s.
  if (ui.fun.role == 1 && !ui.fun.given) {
    static char sent[16][12];
    static int sentN = -1;
    static uint32_t sentAt = 0, lookedAt = 0;
    if (millis() - lookedAt > 300 && fleet->have && linkState() == LinkState::Live) {
      lookedAt = millis();
      int idx[16];
      int16_t xs[16], ys[16];
      const int n = board.layout->shownTiles(ui, fleet->fleet, idx, xs, ys, 16);
      bool same = n == sentN;
      for (int i = 0; same && i < n; i++) same = strcmp(sent[i], fleet->fleet.agents[idx[i]].id) == 0;
      if (n > 0 && (!same || millis() - sentAt > 5000)) {
        const char* ids[16];
        for (int i = 0; i < n; i++) {
          snprintf(sent[i], sizeof sent[i], "%s", fleet->fleet.agents[idx[i]].id);
          ids[i] = sent[i];
        }
        deck_magic_view(link(), ids, (size_t)n);
        sentN = n;
        sentAt = millis();
      }
    }
  }
  static uint32_t lastFrame = 0;
#ifdef PANEL_DEBUG
  static uint32_t lastNote = 0, frames = 0, drawMs = 0;
  if (millis() - lastNote > 5000) {
    lastNote = millis();
    const deck_link_stats_t* st = deck_link_stats(deck.core());
    const NetView nv = net_link::view();
    PANEL_LOG("status: via %s phase %d link %d approved %d agents %u frames %u avg %u ms rx %u bad %u overflow %u "
              "heap %u (min %u, largest %u) psram %u",
              nv.via == NetVia::Wifi ? "wifi" : nv.via == NetVia::Ble ? "ble" : "usb", (int)nv.phase, (int)deck_link_state(link()),
              (int)deck_link_approved(link()), fleet->have ? (unsigned)fleet->fleet.count : 0u, (unsigned)frames,
              frames ? (unsigned)(drawMs / frames) : 0u, (unsigned)st->rx_frames, (unsigned)st->rx_bad,
              (unsigned)st->rx_overflow, (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL),
              (unsigned)heap_caps_get_minimum_free_size(MALLOC_CAP_INTERNAL),
              (unsigned)heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL), (unsigned)ESP.getFreePsram());
    if (dbgFrames) PANEL_LOG("frame: %u frames in 5 s (%u with the panel shown), draw %u ms, send %u ms on average", (unsigned)dbgFrames,
                             (unsigned)dbgPanelFrames, (unsigned)(dbgDrawUs / dbgFrames / 1000), (unsigned)(dbgPushUs / dbgFrames / 1000));
#ifdef QSPI_FAST_REPORT
    QSPI_FAST_REPORT();
#endif
    if (dbgFrames) PANEL_LOG("draw: per frame fill %u us, tile backgrounds %u us, stripes %u us, critters %u us, tile text %u us",
                             (unsigned)(squareProf.fill / dbgFrames), (unsigned)(squareProf.tileBg / dbgFrames), (unsigned)(squareProf.other / dbgFrames),
                             (unsigned)(squareProf.sprite / dbgFrames), (unsigned)(squareProf.text / dbgFrames));
    squareProf = SquareProfile{};
    if (board.accel) PANEL_LOG("motion: gravity x %.2f y %.2f z %.2f g", ui.fun.gx, ui.fun.gy, ui.fun.gz);
    dbgDrawUs = dbgPushUs = dbgFrames = dbgPanelFrames = 0;
    frames = drawMs = 0;
  }
#endif
  // Critters animate: a frame every 100 ms; at once after a touch or new
  // state, and as fast as it draws while the list scrolls.
  if (frame && (dirty || moving || millis() - lastFrame >= 100)) {
    dirty = false;
    lastFrame = millis();
    updateBrightness();
#ifdef PANEL_DEBUG
    const uint32_t t0 = millis();
    render();
    drawMs += millis() - t0;
    frames++;
#else
    render();
#endif
  }
  delay(2);
}

#endif  // ARDUINO
