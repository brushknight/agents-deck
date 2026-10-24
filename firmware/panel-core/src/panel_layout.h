#pragma once
// The interface between a touch board's firmware (grid_app.h) and its
// layout, and the UI state they share. The square boards' layout,
// SquarePanel in layout_square.h, implements it; the round boards have their
// own (RoundLayout in round_view.h).
//
// Every frame the firmware calls tick() (animations: a list settling on a
// row, a toast ending) and draw() with a PanelInput: the fleet from
// deck_model.h, the link state, the board's name, its scope, the animation
// clock and the deck's time. Touch arrives as raw samples (finger down at
// x, y, or lifted) through touch(), which turns them into taps, drags,
// scrolls and holds and returns a DeckCommand for the board to send (an
// Answer, Focus, Interrupt or Dismiss). The board keeps the command's seq in
// PanelUi::cmdSeq and passes the deck's Result to result(). BOOT is button().
//
// A device picks its layout by constructing it, e.g.
// `static const SquarePanel layout(specLcd4());`, and putting it in its
// GridBoard (grid_app.h). Pure C++: runs on the host (tests, sim/render).
#include <stdint.h>
#include <stdio.h>

#include "canvas.h"
#include "deck_cmd.h"
#include "model.h"

constexpr uint32_t kToastMs = 3000;

enum class Swipe : uint8_t { Left, Right, Up, Down };

// The screens of a square board.
enum class Screen : uint8_t { Home, Card, Prompt };

// What lies under a point.
enum class Hit : uint8_t { None, Tile, PageBar, Focus, Interrupt, Stop, Option };

struct PanelHit {
  Hit kind = Hit::None;
  int agent = -1;    // Tile: index into Fleet.agents
  int index = -1;    // PageBar: the page; Option: index into Agent.opts
  bool operator==(const PanelHit& o) const { return kind == o.kind && agent == o.agent && index == o.index; }
};

struct PanelUi {
  Screen screen = Screen::Home;
  char openId[12] = "";        // the card's or the prompt's agent
  int page = 0;                // grid homes: the page

  // List homes: the scroll offset in pixels, and its settle animation.
  float scroll = 0;
  float scrollFrom = 0, scrollTo = 0;
  uint32_t animStart = 0;
  bool animating = false;
  uint32_t settledMs = 0;      // when the list last came to rest (the position bar fades after it)

  // The finger.
  bool down = false;
  bool dragging = false;       // moving the list
  int x0 = 0, y0 = 0, x = 0, y = 0;
  uint32_t downMs = 0;
  float scroll0 = 0;           // the list's offset when the finger went down
  float velocity = 0;          // px per ms, smoothed
  int lastY = 0;
  uint32_t lastMs = 0;
  PanelHit pressed;            // drawn pressed until the finger moves or lifts
  bool holdFired = false;      // the stop hold already sent its Dismiss
  bool caught = false;         // the finger stopped a moving list: not a tap

  // Feedback.
  char toast[48] = "";
  uint32_t toastUntil = 0;
  bool backAfterToast = false; // answered: home when "sent" ends
  uint32_t cmdSeq = 0;         // the command waiting for its Result
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
};
