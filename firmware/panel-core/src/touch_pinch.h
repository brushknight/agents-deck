#pragma once
// Two-finger touch for boards whose controller reports more than one point
// (the GT911 boards: the 2.8C round, the 4" square). Feed every sample with
// the points down; a pinch in (the fingers' distance shrinking to
// kPinchInRatio of where it began) fires once. While two fingers are down,
// and until every finger has lifted after that, the board must not treat
// the touch as one finger: no tap, no swipe, never an answer.
// Pure C++: the host tests drive it.
#include <math.h>
#include <stdint.h>

constexpr float kPinchInRatio = 0.7f;   // 30 % closer

struct TouchPoints {
  uint8_t n = 0;               // fingers down (0 = lifted)
  int16_t x[2] = {0, 0}, y[2] = {0, 0};
};

struct PinchTracker {
  bool active = false;         // two fingers came down: single-finger handling is off until all lift
  bool fired = false;          // the pinch in already fired in this touch
  float start = 0;             // the fingers' distance when the second came down
};

enum class PinchEvent : uint8_t { None, Begin, In, End };

static inline float pinchDistance(const TouchPoints& t) {
  const float dx = (float)(t.x[0] - t.x[1]), dy = (float)(t.y[0] - t.y[1]);
  return sqrtf(dx * dx + dy * dy);
}

// One sample. Begin: a second finger came down (cancel any one-finger press).
// In: the pinch in, once. End: every finger lifted after a two-finger touch.
static inline PinchEvent pinchFeed(PinchTracker& p, const TouchPoints& t) {
  if (!p.active) {
    if (t.n < 2) return PinchEvent::None;
    p.active = true;
    p.fired = false;
    p.start = pinchDistance(t);
    return PinchEvent::Begin;
  }
  if (t.n == 0) {
    p = PinchTracker();
    return PinchEvent::End;
  }
  if (t.n >= 2 && !p.fired && p.start > 20 && pinchDistance(t) <= p.start * kPinchInRatio) {
    p.fired = true;
    return PinchEvent::In;
  }
  return PinchEvent::None;
}
