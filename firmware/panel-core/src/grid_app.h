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
  bool (*touch)(int& x, int& y);           // true while a finger is down, at (x, y) in frame pixels
  void (*brightness)(uint8_t level);       // 0..255
  int bootPin;                             // the BOOT button (active low), -1 for none
  uint8_t brightActive, brightIdle;
  const char* boardId;                     // Hello.board_id; nullptr: DeckLink::chipId() (the MAC, "AA:BB:...")
};

namespace grid_app {

constexpr size_t kRxBuffer = 32768;

static GridBoard board;
static DeckLink deck;
static DeckFleet* fleet = nullptr;         // in PSRAM: up to PANEL_MAX_AGENTS agents, twice
static uint16_t* frame = nullptr;          // in PSRAM
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

static void onFrame(deck_link_t* ctx, const deck_frame_t* f) {
  if (ctx != link()) return;               // the other link (USB while on Wi-Fi): not what the screen shows
  lastHeardMs = millis();
  if (deckApply(*fleet, *f, millis())) dirty = true;
  if (fleet->newWaiting) fleet->newWaiting = false;
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
    default: return;
  }
  PANEL_LOG("command %d for %s, seq %u", (int)c.cmd, c.agent, (unsigned)ui.cmdSeq);
}

static void onLink(deck_link_t*) { dirty = true; }

#ifdef PANEL_DEBUG
static uint32_t dbgDrawUs = 0, dbgPushUs = 0, dbgFrames = 0, dbgPanelFrames = 0;   // the debug report: where a frame's time goes
#endif

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
  PanelInput in{f, notice ? LinkState::Connecting : shown, deviceName, scope(), ms, deckNowEpoch(*fleet, ms), &ui, (ms - lastHeardMs) / 1000, &net, &ci};
  in.rowsOk = board.pushRows != nullptr;
#ifdef PANEL_DEBUG
  const uint32_t t0 = micros();
  board.layout->draw(canvas, in);
  const uint32_t t1 = micros();
  if (const uint16_t* const* rows = board.pushRows ? board.layout->frameRows() : nullptr) board.pushRows(rows);
  else board.push(frame);
  dbgDrawUs += t1 - t0;
  dbgPushUs += micros() - t1;
  dbgFrames++;
  if (controlShown(ui.ctl)) dbgPanelFrames++;
#else
  board.layout->draw(canvas, in);
  if (const uint16_t* const* rows = board.pushRows ? board.layout->frameRows() : nullptr) board.pushRows(rows);
  else board.push(frame);
#endif
}

// Bright while something happens or someone touched it in the last 30 s;
// dim when every agent is idle.
static void updateBrightness() {
  const uint32_t ms = millis();
  bool active = ms - lastActiveMs < 30000 || linkState() != LinkState::Live || !fleet->have;
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
    default: break;
  }
}

static void touchPump() {
  int x = 0, y = 0;
  const bool down = board.touch && board.touch(x, y);
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
  frame = (uint16_t*)heap_caps_calloc((size_t)W * H, 2, MALLOC_CAP_SPIRAM);
  // The "Needs you" screen (attention.h): the deck's spots as this board draws them, in PSRAM.
  void* am = heap_caps_calloc(1, sizeof(Attention), MALLOC_CAP_SPIRAM);
  if (!am) am = calloc(1, sizeof(Attention));
  if (am) {
    ui.att = new (am) Attention();
  }
  canvas = Canvas{frame, W, H};

  const bool shown = frame && board.begin();
  canvas.be = shown && board.frameSwapped && board.frameSwapped();
  PANEL_LOG("panel: %s, display %s, fleet %u B, frame %s, psram free %u", b.type, shown ? "up" : "FAILED",
            (unsigned)sizeof(DeckFleet), frame ? "ok" : "none", (unsigned)ESP.getFreePsram());
  if (board.brightness) board.brightness(board.brightActive);

  DeckLinkConfig cfg = {};
#ifdef PANEL_DEBUG
  squareClock = [] { return (uint32_t)micros(); };
#endif
  boardId = b.boardId ? b.boardId : DeckLink::chipId();
  panelConfig(cfg, boardId, firmware, PanelSpec{b.type, (uint32_t)W, (uint32_t)H, true});
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
