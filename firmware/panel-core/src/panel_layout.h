#pragma once
// What every touch layout shares, and the interface a device's firmware
// uses to drive one.
//
// A layout gets, every frame: a Canvas (an RGB565 framebuffer of its own
// width x height) and a PanelInput (the fleet from deck_model.h, the link
// state, the board's name, whether its scope allows answering, the animation
// clock, the deck's time, and the PanelUi below). It draws the whole frame.
//
// Touch: grid_app.h turns touch samples into taps and swipes with Gesture
// and hands them to the layout, which updates PanelUi (the page, the open
// card, a toast) and may return a UiAnswer: an Answer for the board to send
// to the deck. The board then stores the Answer's seq in PanelUi.answerSeq;
// uiResult() turns the deck's Result into a toast.
//
// A device picks its layout by constructing one (e.g. GridPanel with
// layoutLcd4()) and passing it to gridAppSetup() in its GridBoard. Layouts
// live in layout_*.h: layout_147.h (the 1.47" bar, no touch, drawn by its own
// main.cpp), layout_grid.h (the square touch boards). A new layout
// implements PanelLayout; deck_ui.h has the shared tile look and text
// helpers.
//
// Pure C++: runs on the host (tests, sim/render) as on the board.
#include <stdint.h>
#include <stdio.h>

#include "model.h"
#include "view.h"
#include "canvas.h"

constexpr uint32_t kToastMs = 3000;

struct PanelUi {
  int page = 0;
  char openId[12] = "";        // the agent whose card is open, "" for the grid
  char toast[40] = "";
  uint32_t toastUntil = 0;
  uint32_t answerSeq = 0;      // the Answer waiting for its Result
};

static inline void uiToast(PanelUi& ui, const char* text, uint32_t ms) {
  snprintf(ui.toast, sizeof ui.toast, "%s", text);
  ui.toastUntil = ms + kToastMs;
}

// What a tap asks the board to send: an Answer, or nothing.
struct UiAnswer {
  bool send = false;
  char agent[12] = "", prompt[16] = "", key[8] = "";
};

enum class Swipe : uint8_t { Left, Right, Up, Down };

// The Result of the Answer we sent.
static inline void uiResult(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) {
  if (!seq || seq != ui.answerSeq) return;
  ui.answerSeq = 0;
  if (code == 204) { uiToast(ui, "answered", ms); ui.openId[0] = '\0'; }
  else if (code == 403) uiToast(ui, "needs scope act", ms);
  else if (code == 409) uiToast(ui, "the prompt changed", ms);
  else {
    char b[24];
    snprintf(b, sizeof b, "error %u", (unsigned)code);
    uiToast(ui, b, ms);
  }
}

// ---------------------------------------------------------------- gestures

// Turns touch samples into taps and swipes.
struct Gesture {
  bool down = false;
  int x0 = 0, y0 = 0, x = 0, y = 0;
  uint32_t t0 = 0;
};

enum class GestureKind : uint8_t { None, Tap, Swipe };

struct GestureEvent {
  GestureKind kind = GestureKind::None;
  Swipe swipe = Swipe::Left;
  int x = 0, y = 0;
};

constexpr int kTapSlop = 14, kSwipeMin = 40;
constexpr uint32_t kTapMaxMs = 800;

static inline GestureEvent gestureFeed(Gesture& g, bool touching, int x, int y, uint32_t ms) {
  GestureEvent e;
  if (touching) {
    if (!g.down) { g.down = true; g.x0 = g.x = x; g.y0 = g.y = y; g.t0 = ms; }
    else { g.x = x; g.y = y; }
    return e;
  }
  if (!g.down) return e;
  g.down = false;
  const int dx = g.x - g.x0, dy = g.y - g.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (adx <= kTapSlop && ady <= kTapSlop && ms - g.t0 <= kTapMaxMs) {
    e.kind = GestureKind::Tap;
    e.x = g.x0;
    e.y = g.y0;
  } else if (adx >= kSwipeMin || ady >= kSwipeMin) {
    e.kind = GestureKind::Swipe;
    e.swipe = adx >= ady ? (dx < 0 ? Swipe::Left : Swipe::Right) : (dy < 0 ? Swipe::Up : Swipe::Down);
    e.x = g.x0;
    e.y = g.y0;
  }
  return e;
}

struct PanelInput {
  const Fleet* fleet;          // nullptr before the first snapshot
  LinkState link;
  const char* device;          // the board's name on the deck
  bool act;                    // scope act or admin: prompts get buttons
  uint32_t ms;                 // animation clock
  int64_t nowEpoch;            // the deck's time
  const PanelUi* ui;
};


// A layout: draws frames and answers touch. Implementations keep their own
// geometry; every method is pure (no I/O), so the host tests drive them.
class PanelLayout {
 public:
  virtual ~PanelLayout() = default;
  virtual int width() const = 0;
  virtual int height() const = 0;
  virtual bool trueBlack() const = 0;   // an AMOLED: black background, dims when idle
  // The whole frame.
  virtual void draw(Canvas& c, const PanelInput& in) const = 0;
  // Touch and the BOOT button. tap() may ask for an Answer.
  virtual UiAnswer tap(PanelUi& ui, const Fleet& f, int x, int y, bool act, uint32_t ms) const = 0;
  virtual void swipe(PanelUi& ui, const Fleet& f, Swipe s) const = 0;
  virtual void button(PanelUi& ui, const Fleet& f) const = 0;
  // Keeps ui valid for a changed fleet (a page that exists, a card whose agent is there).
  virtual void fix(PanelUi& ui, const Fleet& f) const = 0;
};
