#pragma once
// A round board's self-test: no Deck Link, just the hardware, for the first
// power-on. Three pages (a "next" pill at the bottom turns them):
//
//   1 colour   colour bars, a grey ramp and a bar sweeping across (tearing and
//              drift show on it), the 480 px circle, and 12 / 3 / 6 / 9 marks
//              so a wrong ROUND_ROTATION shows at once
//   2 text     every font size
//   3 touch    rings to aim at; touch draws a trail with the x / y under the finger
//
// On every page: the touch trail and coordinates, "light" (steps the
// backlight), "chirp" (sounds the buzzer for 80 ms: it must be quiet
// otherwise), the frame and push time, and the build settings (rotation,
// pixel clock, bounce buffer) with what the board found (expander, touch).
//
// The drawing is pure C++ (sim/render.cpp draws it too); roundSelftestSetup()
// and roundSelftestLoop() below run it on the board.
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "font_data.h"
#include "font_round.h"
#include "round_geom.h"
#include "round_rotate.h"
#include "round_ui.h"

constexpr int kSelftestPages = 3, kSelftestDots = 64;
static const uint8_t kSelftestLevels[] = {255, 128, 48, 12};

struct SelftestState {
  int page = 0;
  RPoint dots[kSelftestDots] = {};
  int dotCount = 0, dotNext = 0;
  bool down = false;
  int x = -1, y = -1;                 // the last touch, frame pixels
  uint32_t touches = 0;
  int level = 0;                      // index into kSelftestLevels
  uint32_t chirpUntil = 0;
  uint32_t frameMs = 0, pushMs = 0;   // the last frame's draw and push time
  // What the board says about itself.
  const char* type = "";
  const char* found = "";             // e.g. "tca9554 ok · gt911 0x5D"
  int rotation = 0;
  uint32_t pclkHz = 0;
  int bouncePx = 0;
};

enum class SelftestButton : uint8_t { None, Light, Next, Chirp };

namespace selftest_detail {

struct Btn {
  SelftestButton kind;
  int x, y, w, h;
  const char* label;
};

static inline int buttons(Btn out[3]) {
  out[0] = Btn{SelftestButton::Light, 104, 352, 86, 46, "light"};
  out[1] = Btn{SelftestButton::Next, 197, 352, 86, 46, "next"};
  out[2] = Btn{SelftestButton::Chirp, 290, 352, 86, 46, "chirp"};
  return 3;
}

}  // namespace selftest_detail

static inline SelftestButton selftestButtonAt(int x, int y) {
  selftest_detail::Btn b[3];
  const int n = selftest_detail::buttons(b);
  for (int i = 0; i < n; i++)
    if (x >= b[i].x - 4 && x < b[i].x + b[i].w + 4 && y >= b[i].y - 6 && y < b[i].y + b[i].h + 6) return b[i].kind;
  return SelftestButton::None;
}

// One touch sample. Returns the button pressed (on the finger going down), if any.
static inline SelftestButton selftestTouch(SelftestState& s, bool down, int x, int y, uint32_t ms) {
  SelftestButton pressed = SelftestButton::None;
  if (down) {
    if (!s.down) {
      s.touches++;
      pressed = selftestButtonAt(x, y);
      switch (pressed) {
        case SelftestButton::Next: s.page = (s.page + 1) % kSelftestPages; break;
        case SelftestButton::Light: s.level = (s.level + 1) % (int)sizeof kSelftestLevels; break;
        case SelftestButton::Chirp: s.chirpUntil = ms + 80; break;
        default: break;
      }
    }
    s.x = x;
    s.y = y;
    const RPoint last = s.dotCount ? s.dots[(s.dotNext + kSelftestDots - 1) % kSelftestDots] : RPoint{-100, -100};
    if ((last.x - x) * (last.x - x) + (last.y - y) * (last.y - y) >= 16) {
      s.dots[s.dotNext] = RPoint{x, y};
      s.dotNext = (s.dotNext + 1) % kSelftestDots;
      if (s.dotCount < kSelftestDots) s.dotCount++;
    }
  }
  s.down = down;
  return pressed;
}

static inline uint8_t selftestBrightness(const SelftestState& s) { return kSelftestLevels[s.level]; }
static inline bool selftestBuzzer(const SelftestState& s, uint32_t ms) { return (int32_t)(s.chirpUntil - ms) > 0; }

namespace selftest_detail {

static inline void textC(Canvas& c, const Font& f, int y, const char* s, uint16_t col) {
  textCentered(c, f, 240, y, s, col, 440);
}

static inline void pageColour(Canvas& c, const RoundTheme& T, uint32_t ms) {
  // Bars: white, yellow, cyan, green, magenta, red, blue, black.
  static const uint16_t kBars[] = {rgb565(255, 255, 255), rgb565(255, 255, 0), rgb565(0, 255, 255), rgb565(0, 255, 0),
                                   rgb565(255, 0, 255),   rgb565(255, 0, 0),   rgb565(0, 0, 255),   rgb565(0, 0, 0)};
  const int x0 = 60, w = 45, y0 = 116, h = 100;
  for (int i = 0; i < 8; i++) c.rect(x0 + i * w, y0, w, h, kBars[i]);
  c.frame(x0 - 1, y0 - 1, 8 * w + 2, h + 2, T.t.dim);
  // A grey ramp in 16 steps, and pure red / green / blue ramps.
  for (int i = 0; i < 16; i++) {
    const uint8_t v = (uint8_t)(i * 17);
    c.rect(x0 + i * 22 + (i > 7 ? 8 : 0), y0 + h + 6, 22, 22, rgb565(v, v, v));
    c.rect(x0 + i * 22 + (i > 7 ? 8 : 0), y0 + h + 30, 22, 10, rgb565(v, 0, 0));
    c.rect(x0 + i * 22 + (i > 7 ? 8 : 0), y0 + h + 40, 22, 10, rgb565(0, v, 0));
    c.rect(x0 + i * 22 + (i > 7 ? 8 : 0), y0 + h + 50, 22, 10, rgb565(0, 0, v));
  }
  // A bar sweeping across: tearing and drift show on its edges.
  const int sweep = (int)((ms / 8) % (8 * w));
  c.rect(x0 + sweep, y0, 4, h, T.t.accent);
  textC(c, FONT_S, y0 + h + 66, "the orange bar should move smoothly", T.t.dim);
}

static inline void pageText(Canvas& c, const RoundTheme& T) {
  struct { const Font* f; const char* label; } kFonts[] = {
      {&FONT_XS, "XS 10px  agents deck"},  {&FONT_S, "S 12px  agents deck"},   {&FONT_L, "L 15px  agents deck"},
      {&FONT_XL, "XL 20px  agents"},       {&FONT_XXL, "XXL 27px  agents"},    {&FONT_R30, "R30 30px  ok"},
      {&FONT_R30B, "R30B 30px  ok"},       {&FONT_R35, "R35 35px  ok"},
  };
  int y = 110;
  for (const auto& k : kFonts) {
    textC(c, *k.f, y, k.label, T.t.ink);
    y += k.f->cellH * 85 / 100;   // the big sizes have generous leading
  }
}

static inline void pageTouch(Canvas& c, const RoundTheme& T) {
  for (int r : {60, 120, 180}) strokeCircle(c, 240, 240, (float)r, 1, T.t.line);
  c.rect(239, 70, 2, 340, T.t.line);
  c.rect(70, 239, 340, 2, T.t.line);
  textC(c, FONT_S, 130, "touch anywhere: the trail", T.t.dim);
  textC(c, FONT_S, 148, "should follow the finger", T.t.dim);
}

}  // namespace selftest_detail

static inline void drawSelftest(Canvas& c, const SelftestState& s, uint32_t ms) {
  using namespace selftest_detail;
  const RoundTheme T = roundTheme();
  c.fill(T.t.bg);
  // The visible circle: its edge, and marks every 30 degrees (12, 3, 6, 9 longer, labelled).
  strokeCircle(c, 240, 240, 239, 2, T.t.ink);
  strokeCircle(c, 240, 240, 232, 1, T.t.accent);
  for (int a = 0; a < 360; a += 30) {
    const bool major = a % 90 == 0;
    const RPoint p0 = polar(240, 240, major ? 210.f : 222.f, (float)a), p1 = polar(240, 240, 231, (float)a);
    c.line(p0.x - 1, p0.y - 1, p1.x - 1, p1.y - 1, major ? T.t.accent : T.t.dim, 3);
  }
  static const char* const kHours[] = {"12", "3", "6", "9"};
  for (int i = 0; i < 4; i++) {
    const RPoint p = polar(240, 240, 198, 90.f * i);
    textCentered(c, FONT_L, p.x, p.y - FONT_L.cellH / 2, kHours[i], T.t.accent, 40);
  }

  char buf[96];
  snprintf(buf, sizeof buf, "%s self-test %d/%d", s.type, s.page + 1, kSelftestPages);
  textC(c, FONT_S, 52, buf, T.t.ink);
  snprintf(buf, sizeof buf, "rot %d" DECK_DOT "pclk %u.%u MHz" DECK_DOT "bounce %d", s.rotation,
           (unsigned)(s.pclkHz / 1000000), (unsigned)(s.pclkHz / 100000 % 10), s.bouncePx);
  textC(c, FONT_S, 68, buf, T.t.dim);
  textC(c, FONT_S, 84, s.found, T.t.dim);

  if (s.page == 0) pageColour(c, T, ms);
  else if (s.page == 1) pageText(c, T);
  else pageTouch(c, T);

  // The touch trail, newest brightest, and where the finger is.
  for (int k = 0; k < s.dotCount; k++) {
    const int i = (s.dotNext + kSelftestDots - s.dotCount + k) % kSelftestDots;
    const bool newest = k == s.dotCount - 1;
    fillDisc(c, (float)s.dots[i].x, (float)s.dots[i].y, newest ? 7.f : 4.f, newest ? T.t.accent : T.t.ink);
  }
  if (s.page == 1) {
    // the text page needs the room
  } else if (s.x >= 0) {
    snprintf(buf, sizeof buf, "x %d  y %d  %s  #%u", s.x, s.y, s.down ? "down" : "up", (unsigned)s.touches);
    textC(c, FONT_L, 318, buf, s.down ? T.t.accent : T.t.ink);
  } else {
    textC(c, FONT_L, 318, "no touch yet", T.t.dim);
  }

  // The buttons.
  selftest_detail::Btn b[3];
  const int n = selftest_detail::buttons(b);
  for (int i = 0; i < n; i++) {
    const bool on = (b[i].kind == SelftestButton::Chirp && selftestBuzzer(s, ms));
    if (on || b[i].kind == SelftestButton::Next) fillPill(c, b[i].x, b[i].y, b[i].w, b[i].h, on ? T.t.accent : T.t.ink);
    else strokePill(c, b[i].x, b[i].y, b[i].w, b[i].h, 2, T.t.ink);
    textCentered(c, FONT_L, b[i].x + b[i].w / 2, b[i].y + (b[i].h - FONT_L.cellH) / 2, b[i].label,
                 on || b[i].kind == SelftestButton::Next ? T.t.bg : T.t.ink, b[i].w - 8);
  }
  snprintf(buf, sizeof buf, "light %u" DECK_DOT "draw %u ms" DECK_DOT "push %u ms", (unsigned)selftestBrightness(s),
           (unsigned)s.frameMs, (unsigned)s.pushMs);
  textC(c, FONT_S, 404, buf, T.t.dim);
}

// ---------------------------------------------------------------- on the board
#if defined(ARDUINO)
#include <Arduino.h>
#include <esp_heap_caps.h>

struct SelftestBoard {
  const char* type;
  bool (*begin)();                      // display, touch, backlight
  void (*push)(const uint16_t* frame);
  bool (*touch)(int& x, int& y);        // frame pixels
  void (*brightness)(uint8_t level);
  void (*buzzer)(bool on);
  const char* (*found)();               // what the board found, one line
  uint32_t pclkHz;
  int bouncePx;
};

namespace selftest_app {
static SelftestBoard board;
static SelftestState state;
static uint16_t* frame = nullptr;
static Canvas canvas{nullptr, 0, 0};
static bool buzzing = false;
static uint8_t level = 0;
}  // namespace selftest_app

static inline void roundSelftestSetup(const SelftestBoard& b) {
  using namespace selftest_app;
  board = b;
  Serial.begin(115200);
  frame = (uint16_t*)heap_caps_calloc(480 * 480, 2, MALLOC_CAP_SPIRAM);
  canvas = Canvas{frame, 480, 480};
  const bool up = frame && board.begin();
  state.type = b.type;
  state.found = board.found ? board.found() : "";
  state.rotation = ROUND_ROTATION;
  state.pclkHz = b.pclkHz;
  state.bouncePx = b.bouncePx;
  if (board.buzzer) board.buzzer(false);
  level = selftestBrightness(state);
  if (board.brightness) board.brightness(level);
  Serial.printf("selftest: %s display %s, %s, rotation %d, pclk %u, bounce %d, psram free %u\n", b.type,
                up ? "up" : "FAILED", state.found, ROUND_ROTATION, (unsigned)b.pclkHz, b.bouncePx,
                (unsigned)ESP.getFreePsram());
}

static inline void roundSelftestLoop() {
  using namespace selftest_app;
  const uint32_t ms = millis();
  int x = 0, y = 0;
  const bool down = board.touch && board.touch(x, y);
  if (down || state.down) {
    const SelftestButton pressed = selftestTouch(state, down, x, y, ms);
    if (down) Serial.printf("touch x %d y %d%s\n", x, y, pressed != SelftestButton::None ? " (button)" : "");
  }
  const bool buzz = selftestBuzzer(state, ms);
  if (buzz != buzzing && board.buzzer) { board.buzzer(buzz); buzzing = buzz; }
  if (selftestBrightness(state) != level && board.brightness) {
    level = selftestBrightness(state);
    board.brightness(level);
  }
  if (!frame) { delay(20); return; }
  const uint32_t t0 = millis();
  drawSelftest(canvas, state, ms);
  const uint32_t t1 = millis();
  board.push(frame);
  const uint32_t t2 = millis();
  state.frameMs = t1 - t0;
  state.pushMs = t2 - t1;
  static uint32_t lastNote = 0;
  if (ms - lastNote > 2000) {
    lastNote = ms;
    Serial.printf("selftest: %s, page %d draw %u ms push %u ms light %u touches %u\n", state.found, state.page + 1,
                  (unsigned)state.frameMs, (unsigned)state.pushMs, (unsigned)level, (unsigned)state.touches);
  }
}
#endif  // ARDUINO
