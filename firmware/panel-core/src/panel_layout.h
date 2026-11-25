#pragma once
// The interface between a touch board's firmware (grid_app.h) and its
// layout, and the UI state they share. The square boards' layout,
// SquarePanel in layout_square.h, implements it; the round boards have their
// own (RoundLayout in round_view.h).
//
// Every frame the firmware calls tick() (animations: a list settling on a
// row, its pages settling after a sideways drag, a toast ending) and draw() with a PanelInput: the fleet from
// deck_model.h, the link state, the board's name, its scope, the animation
// clock and the deck's time. Touch arrives as raw samples (finger down at
// x, y, or lifted) through touch(), which turns them into taps, drags,
// scrolls and holds and returns a DeckCommand for the board to send (an
// Answer, Focus, Interrupt or Dismiss). The board keeps the command's seq in
// PanelUi::cmdSeq and passes the deck's Result to result(). BOOT is button().
// On Wi-Fi (net_link.h) PanelInput.net says how the board is connected (the
// connecting notice then says where Wi-Fi is), and PanelUi::wifi puts the
// Wi-Fi mark on home.
//
// A device picks its layout by constructing it, e.g.
// `static const SquarePanel layout(specLcd4());`, and putting it in its
// GridBoard (grid_app.h). Pure C++: runs on the host (tests, sim/render).
#include <stdint.h>
#include <stdio.h>

#include "attention.h"
#include "canvas.h"
#include "deck_cmd.h"
#include "model.h"
#include "net_model.h"

constexpr uint32_t kToastMs = 3000;

// ---------------------------------------------------------------- profiling

// The debug build's clock (grid_app.h sets it) and where a frame's drawing
// time goes, in microseconds since the last report. Unset: nothing measured.
static uint32_t (*squareClock)() __attribute__((unused)) = nullptr;
struct SquareProfile { uint32_t fill, tileBg, sprite, text, other; };
static SquareProfile squareProf __attribute__((unused)){};
#define SQ_T0(v) const uint32_t v = squareClock ? squareClock() : 0
#define SQ_ADD(field, v) do { if (squareClock) squareProf.field += squareClock() - (v); } while (0)


struct Rect {
  int x, y, w, h;
  bool contains(int px, int py) const { return px >= x && px < x + w && py >= y && py < y + h; }
  bool intersects(const Rect& o) const { return x < o.x + o.w && o.x < x + w && y < o.y + o.h && o.y < y + h; }
};

enum class Swipe : uint8_t { Left, Right, Up, Down };

// A move is sideways (a page slide, back from a card) only when it is clearly
// more across than up or down: |dx| > 1.3 |dy|. A diagonal that goes up (or
// down) about as much as across is not a page slide.
static inline bool swipeAcross(int adx, int ady) { return adx * 10 > ady * 13; }

// The screens of a square board.
enum class Screen : uint8_t { Home, Card, Prompt };

// What lies under a point.
// NeedsYou: the "Needs you" screen's mark among the page bars.
// ConfirmYes, ConfirmNo: the two buttons of "interrupt?" over a card.
enum class Hit : uint8_t { None, Tile, PageBar, Focus, Interrupt, Stop, Option, Send, Resume, NeedsYou, ConfirmYes, ConfirmNo, Hatch, Back };

struct PanelHit {
  Hit kind = Hit::None;
  int agent = -1;    // Tile: index into Fleet.agents
  int index = -1;    // PageBar: the page; Option: index into Agent.opts
  bool operator==(const PanelHit& o) const { return kind == o.kind && agent == o.agent && index == o.index; }
};

// ---------------------------------------------------------------- the board's panel
// The board's own panel, pulled down from the top of home (control_panel.h).
// What it shows: the firmware fills it (grid_app.h); nullptr in PanelInput
// means a board without the panel.
struct ControlInfo {
  const char* name = "";       // the board's name on the deck
  const char* bleName = "";    // "deck-" and six characters; empty without Bluetooth
  const char* board = "";      // the board type (firmware/devices/<board>)
  const char* firmware = "";
  const char* id = "";
  const char* ssid = "";       // the network it has, empty for none
  NetVia via = NetVia::Usb;
  bool linked = false;         // a deck answers
  NetUplink uplink = NetUplink::Auto;
  bool wifiCapable = false, wifiOn = false, wifiUp = false;
  bool bleCapable = false, bleOn = false, bleBonded = false;
  uint32_t upSec = 0;
  bool imu = false;            // a motion sensor: the magic trick tile
  bool magicOn = false;        // the trick is armed
};

// What a touch on the panel asks the firmware to do.
enum class ControlAct : uint8_t { None, Wifi, Ble, Restart, Magic };

enum class ControlHit : uint8_t { None, Wifi, Ble, Restart, Magic };

constexpr uint32_t kControlSettleMs = 220, kControlHoldMs = 1000;
constexpr int kControlSlop = 6;
// The panel stays under the finger (1: it moves as far as the finger does).
constexpr float kControlPullGain = 1.0f;
// Let go after the finger moved this far and the panel carries on: opening
// from shut, or closing from open (a flick decides on its own). A distance on
// the glass, the same on every board (ControlGeom::commitPx turns it into pixels).
constexpr float kControlCommitMm = 5.f;

struct ControlUi {
  float open = 0;              // 0 shut .. 1 fully down
  float from = 0, to = 0;
  uint32_t animStart = 0;
  bool animating = false;
  // The finger.
  bool down = false, pulling = false, moved = false;
  int y0 = 0, lastY = 0;
  float open0 = 0;
  float velocity = 0;          // of `open`, per ms
  uint32_t downMs = 0, lastMs = 0;
  ControlHit pressed = ControlHit::None;
  bool holdFired = false;
  ControlAct act = ControlAct::None;   // for the firmware: it takes it and clears it
  // Grid homes (controlEdgeTouch): the finger that may become a pull. 0: none
  // yet, 1: it went down in the top strip at (ex, ey), 2: it is the screen's.
  uint8_t edge = 0;
  int ex = 0, ey = 0;
};

static inline bool controlShown(const ControlUi& u) { return u.open > 0.001f || u.animating || u.pulling; }

// ---------------------------------------------------------------- motion (fun_motion.h)


struct FunUi {
  bool magic = false;          // the trick is on (from the drawer) until it is switched off there
  bool taken = false;          // a knock took the agents (the next knock gives them back)
  bool spent = false;          // taken and given back once: nothing more until it is switched on again
  // Its part, from the deck (Settings, Magic trick; the Magic frame).
  uint8_t role = 0;            // 0 none, 1 giver, 2 receiver
  uint8_t arrive = 1, leave = 3;   // corners its tiles fly in from and out to: 0 top-left, 1 top-right, 2 bottom-left, 3 bottom-right
  // The giver: its agents are taken; when they left and came back (the flights).
  bool given = false;
  uint32_t giveAt = 0, backAt = 0;
  // What it showed when they were taken: the frame, and where each tile was.
  const uint16_t* pic = nullptr;
  int16_t picX[16] = {}, picY[16] = {};
  int picN = 0;
  uint32_t revealAt = 0;       // the knock that took them: the tiles fly in
  uint32_t returnAt = 0;       // the knock that gave them back: the tiles fly out
  bool stashDue = false;       // tell the deck (Stash): take every agent, or give them back
  uint32_t sickAt = 0;         // shaken on a card: the critter is sick
  char sickId[12] = "";
  // Detection (funDetect).
  bool seeded = false, wasHigh = false;
  float hist[8] = {};          // the last samples' size (g), for the knock: how fast it changed
  uint32_t histMs[8] = {};
  int hn = 0;
  uint32_t lastMs = 0;
  float base = 1.f, gx = 0, gy = 0, gz = 1.f;   // gravity, smoothed (g): the sphere leans with it
  uint32_t peaks[8] = {};
  int np = 0;
  uint32_t lastKnock = 0, lastShake = 0;
  uint32_t lastMove = 0, burstAt = 0, prevMoveEnd = 0;   // movement bursts: a knock comes out of stillness
  uint32_t lastTouch = 0;      // a finger on the glass (grid_app)
};

struct PanelUi {
  Screen screen = Screen::Home;
  char openId[12] = "";        // the card's or the prompt's agent
  bool confirm = false;        // square boards: "interrupt?" over the card, before it is sent
  int page = 0;                // grid homes: the page

  // The "Needs you" screen (attention.h), one step left of the first page:
  // the deck's spots in this screen's cells (nullptr: a board without the
  // screen), and whether home shows it now.
  Attention* att = nullptr;
  bool needsYou = false;

  // List homes: the scroll offset in pixels, and its settle animation.
  float scroll = 0;
  float scrollFrom = 0, scrollTo = 0;
  uint32_t animStart = 0;
  bool animating = false;
  uint32_t settledMs = 0;      // when the list last came to rest (the position bar fades after it)

  // List homes with the "Needs you" screen: the two are pages side by side,
  // and a sideways drag slides them. slide is where the pages are, 0 = "Needs
  // you" on screen, 1 = the list; it counts only while a finger slides them
  // (sliding) or they settle (slideAnim). At rest needsYou alone says which
  // page shows, so code that sets it lands on that page at once.
  bool sliding = false;
  bool slideAnim = false;
  float slide = 1;
  float slide0 = 1;            // where the pages were when the finger went down
  float slideFrom = 1, slideTo = 1;
  uint32_t slideStart = 0;

  // The finger.
  bool down = false;
  bool dragging = false;       // moving the list (up and down; sliding is the sideways one)
  int x0 = 0, y0 = 0, x = 0, y = 0;
  uint32_t downMs = 0;
  float scroll0 = 0;           // the list's offset when the finger went down
  float velocity = 0;          // px per ms, smoothed (the list's, or the pages' while they slide)
  int lastX = 0, lastY = 0;
  uint32_t lastMs = 0;
  PanelHit pressed;            // drawn pressed until the finger moves or lifts
  bool holdFired = false;      // the stop hold already sent its Dismiss
  bool caught = false;         // the finger stopped a moving list: not a tap
  int selected = -1;           // big panels: the prompt's chosen option (sent with "send")
  uint32_t stopArmedUntil = 0; // big panels: "tap again to stop" until then

  // On Wi-Fi or Bluetooth (the firmware sets it from net_link.h): home shows
  // a small mark, and a grid home keeps a row at the bottom for it. ble: the
  // mark is Bluetooth's.
  bool wifi = false;
  bool ble = false;

  // Feedback.
  char toast[48] = "";
  uint32_t toastUntil = 0;
  bool backAfterToast = false; // answered: home when "sent" ends
  uint32_t cmdSeq = 0;         // the command waiting for its Result

  ControlUi ctl;               // the board's panel (control_panel.h)
  FunUi fun;                   // the motion tricks (boards with a motion sensor)
};

static inline void uiToast(PanelUi& ui, const char* text, uint32_t ms, uint32_t forMs = kToastMs) {
  snprintf(ui.toast, sizeof ui.toast, "%s", text);
  ui.toastUntil = ms + forMs;
}

struct PanelInput {
  const Fleet* fleet;          // nullptr before the first snapshot
  LinkState link;
  const char* device;          // the board's name on the deck
  DeckScope scope;
  uint32_t ms;                 // animation clock (millis)
  int64_t nowEpoch;            // the deck's time
  const PanelUi* ui;
  uint32_t silentSec;          // seconds since the deck last said anything ("last seen")
  const NetView* net = nullptr;  // USB or Wi-Fi (nullptr: a USB-only board)
  const ControlInfo* ctl = nullptr;   // the board's panel (nullptr: none)
  bool rowsOk = false;         // the board can send a frame as rows from anywhere (frameRows)
  bool splitOk = false;        // the board can send a frame as two pictures side by side (frameSplit)
};

// A moving frame made of two pictures: pixel x of a row comes from `a` at
// x + shift while x < w - shift, then from `b` at x - (w - shift). A null
// picture is `fill` (past the first or the last page).
struct FrameSplit {
  const uint16_t* a;
  const uint16_t* b;
  int shift;
  uint16_t fill;   // as the canvas encodes it
};

class PanelLayout {
 public:
  virtual ~PanelLayout() = default;
  virtual int width() const = 0;
  virtual int height() const = 0;
  virtual bool trueBlack() const = 0;   // an AMOLED: black background
  virtual void draw(Canvas& c, const PanelInput& in) const = 0;
  // One touch sample; returns what to send.
  virtual DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope,
                            uint32_t ms) const = 0;
  // Animations and timeouts; true while something moves (the board then draws often).
  virtual bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const = 0;
  virtual void button(PanelUi& ui, const Fleet& f) const = 0;
  // The deck's Result for ui.cmdSeq.
  virtual void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const = 0;
  // The board's panel takes this touch sample (pulling it down, or on it); the
  // firmware then gives it to nothing else. Boards without the panel: never.
  virtual bool panelTouch(PanelUi&, bool, int, int, uint32_t) const { return false; }
  // After draw(): the frame as one pointer per row, when the layout made it
  // from pictures it keeps (and in.rowsOk), so the board sends those rows and
  // the canvas wasn't drawn; nullptr: send the canvas.
  virtual const uint16_t* const* frameRows() const { return nullptr; }
  // After draw(): the frame as two pictures side by side (home's pages
  // sliding, page_slide.h), when in.splitOk; nullptr: send the canvas or the rows.
  virtual const FrameSplit* frameSplit() const { return nullptr; }
  // An accelerometer sample in g (boards with a motion sensor): knocks and shakes.
  virtual void motion(PanelUi&, float, float, float, uint32_t) const {}
  // The agent tiles on screen now (the magic trick's giver): indexes into the
  // fleet and their places, at most max; 0 where this layout can't say.
  virtual int shownTiles(const PanelUi&, const Fleet&, int*, int16_t*, int16_t*, int) const { return 0; }
};
