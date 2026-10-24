// agents deck on the Waveshare ESP32-S3-Touch-LCD-2.8C: the spring carousel
// on the round 480 x 480 screen (one turn of nine agents round the selected
// one), with touch. Built with -DROUND_ORBIT_CLASSIC it shows the earlier
// Orbit home instead: every agent on a ring, the one that matters in the middle.
//
// This file is what is particular to the board: the GT911 touch,
// its ST7701 init and RGB timing. The wiring both round boards share (the
// TCA9554 expander, the RGB bus, the backlight) is panel-core's
// waveshare_round.h. Everything else (Deck Link, the fleet, the layout, the
// touch UI) is panel-core's round_app.h with layout_spring.h.
//
// Pins, timings and the panel's init sequence are from Waveshare's demo for
// this board (ESP32-S3-Touch-LCD-2.8C-Demo.zip on the board's wiki:
// Arduino/examples/LVGL_Arduino, Display_ST7701.*, Touch_GT911.*,
// TCA9554PWR.*), checked against its ESP-IDF demo.
//
// Built with -DROUND_SELFTEST (env selftest) it runs the hardware self-test
// instead (round_selftest.h): no Deck Link.
#include <Arduino.h>

#include "layout_orbit.h"
#include "layout_spring.h"
#include "round_app.h"
#include "round_selftest.h"
#include "waveshare_round.h"

#define FW_VERSION "0.3.0"

// Waveshare's Arduino demo runs 30 MHz with a bounce buffer; their ESP-IDF demo
// runs 18 MHz without one, which is the default here.
#ifndef ROUND_PCLK_HZ
#define ROUND_PCLK_HZ 18000000
#endif

constexpr uint8_t ADDR_GT911 = 0x5D, ADDR_GT911_ALT = 0x14;
static uint8_t gt911 = 0;

// ---------------------------------------------------------------- display
// The ST7701's init, verbatim from Waveshare's demo for this board. Arduino_GFX
// 1.5.0's TL028WVC01_init_operations is the same panel but sets VCOM (B1) to
// 0x2D where Waveshare uses 0x3E.
static const uint8_t waveshare_28c_init_operations[] = {
    BEGIN_WRITE,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x13,
    WRITE_C8_D8, 0xEF, 0x08,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x10,
    WRITE_C8_D16, 0xC0, 0x3B, 0x00,
    WRITE_C8_D16, 0xC1, 0x10, 0x0C,
    WRITE_C8_D16, 0xC2, 0x07, 0x0A,
    WRITE_C8_D8, 0xC7, 0x00,
    WRITE_C8_D8, 0xCC, 0x10,
    WRITE_C8_D8, 0xCD, 0x08,
    WRITE_COMMAND_8, 0xB0, WRITE_BYTES, 16,
    0x05, 0x12, 0x98, 0x0E, 0x0F, 0x07, 0x07, 0x09, 0x09, 0x23, 0x05, 0x52, 0x0F, 0x67, 0x2C, 0x11,
    WRITE_COMMAND_8, 0xB1, WRITE_BYTES, 16,
    0x0B, 0x11, 0x97, 0x0C, 0x12, 0x06, 0x06, 0x08, 0x08, 0x22, 0x03, 0x51, 0x11, 0x66, 0x2B, 0x0F,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x11,
    WRITE_C8_D8, 0xB0, 0x5D,
    WRITE_C8_D8, 0xB1, 0x3E,
    WRITE_C8_D8, 0xB2, 0x81,
    WRITE_C8_D8, 0xB3, 0x80,
    WRITE_C8_D8, 0xB5, 0x4E,
    WRITE_C8_D8, 0xB7, 0x85,
    WRITE_C8_D8, 0xB8, 0x20,
    WRITE_C8_D8, 0xC1, 0x78,
    WRITE_C8_D8, 0xC2, 0x78,
    WRITE_C8_D8, 0xD0, 0x88,
    WRITE_COMMAND_8, 0xE0, WRITE_BYTES, 3, 0x00, 0x00, 0x02,
    WRITE_COMMAND_8, 0xE1, WRITE_BYTES, 11, 0x06, 0x30, 0x08, 0x30, 0x05, 0x30, 0x07, 0x30, 0x00, 0x33, 0x33,
    WRITE_COMMAND_8, 0xE2, WRITE_BYTES, 12, 0x11, 0x11, 0x33, 0x33, 0xF4, 0x00, 0x00, 0x00, 0xF4, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0xE3, WRITE_BYTES, 4, 0x00, 0x00, 0x11, 0x11,
    WRITE_C8_D16, 0xE4, 0x44, 0x44,
    WRITE_COMMAND_8, 0xE5, WRITE_BYTES, 16,
    0x0D, 0xF5, 0x30, 0xF0, 0x0F, 0xF7, 0x30, 0xF0, 0x09, 0xF1, 0x30, 0xF0, 0x0B, 0xF3, 0x30, 0xF0,
    WRITE_COMMAND_8, 0xE6, WRITE_BYTES, 4, 0x00, 0x00, 0x11, 0x11,
    WRITE_C8_D16, 0xE7, 0x44, 0x44,
    WRITE_COMMAND_8, 0xE8, WRITE_BYTES, 16,
    0x0C, 0xF4, 0x30, 0xF0, 0x0E, 0xF6, 0x30, 0xF0, 0x08, 0xF0, 0x30, 0xF0, 0x0A, 0xF2, 0x30, 0xF0,
    WRITE_C8_D16, 0xE9, 0x36, 0x01,
    WRITE_COMMAND_8, 0xEB, WRITE_BYTES, 7, 0x00, 0x01, 0xE4, 0xE4, 0x44, 0x88, 0x40,
    WRITE_COMMAND_8, 0xED, WRITE_BYTES, 16,
    0xFF, 0x10, 0xAF, 0x76, 0x54, 0x2B, 0xCF, 0xFF, 0xFF, 0xFC, 0xB2, 0x45, 0x67, 0xFA, 0x01, 0xFF,
    WRITE_COMMAND_8, 0xEF, WRITE_BYTES, 6, 0x08, 0x08, 0x08, 0x45, 0x3F, 0x54,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0x11,
    END_WRITE,
    DELAY, 120,
    BEGIN_WRITE,
    WRITE_C8_D8, 0x3A, 0x66,
    WRITE_C8_D8, 0x36, 0x00,
    WRITE_C8_D8, 0x35, 0x00,
    WRITE_COMMAND_8, 0x29,
    END_WRITE,
};

// ---------------------------------------------------------------- GT911
// Reset with INT held low picks address 0x5D.
static void gtReset() {
  pinMode(ws_round::PIN_TOUCH_INT, OUTPUT);
  digitalWrite(ws_round::PIN_TOUCH_INT, LOW);
  ws_round::exioSet(ws_round::EXIO_TP_RST, false);
  delay(10);
  ws_round::exioSet(ws_round::EXIO_TP_RST, true);
  delay(200);
  digitalWrite(ws_round::PIN_TOUCH_INT, HIGH);
  pinMode(ws_round::PIN_TOUCH_INT, INPUT);
  delay(50);
}

static bool gtRead(uint16_t reg, uint8_t* out, size_t n) {
  Wire.beginTransmission(gt911);
  Wire.write((uint8_t)(reg >> 8));
  Wire.write((uint8_t)reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(gt911, (uint8_t)n) != n) return false;
  for (size_t i = 0; i < n; i++) out[i] = Wire.read();
  return true;
}

static void gtWrite8(uint16_t reg, uint8_t v) {
  const uint8_t d[3] = {(uint8_t)(reg >> 8), (uint8_t)reg, v};
  ws_round::i2cWrite(gt911, d, 3);
}

// The first touch point, in screen pixels (the panel and the touch share axes).
// Up to two touch points, in the panel's pixels: the GT911 reports how many
// in 0x814E and 8 bytes per point from 0x814F (id, x lo/hi, y lo/hi, size).
static void touchPanelPoints(TouchPoints& t) {
  static TouchPoints last;
  static uint32_t lastPoll = 0;
  if (!gt911) { t = TouchPoints(); return; }
  if (millis() - lastPoll < 8) { t = last; return; }
  lastPoll = millis();
  uint8_t status = 0;
  if (!gtRead(0x814E, &status, 1)) { last = TouchPoints(); t = last; return; }
  if (!(status & 0x80)) { t = last; return; }                  // no new report: as before
  const int n = status & 0x0F;
  TouchPoints now;
  if (n > 0 && n <= 5) {
    uint8_t p[16];
    const int take = n >= 2 ? 2 : 1;
    if (gtRead(0x814F, p, (size_t)take * 8)) {
      now.n = (uint8_t)take;
      for (int i = 0; i < take; i++) {
        now.x[i] = (int16_t)(p[i * 8 + 1] | p[i * 8 + 2] << 8);
        now.y[i] = (int16_t)(p[i * 8 + 3] | p[i * 8 + 4] << 8);
      }
    } else {
      now = last;
    }
  }
  gtWrite8(0x814E, 0);
  last = now;
  t = now;
}

static bool touchPanel(int& x, int& y) {
  TouchPoints t;
  touchPanelPoints(t);
  x = t.x[0];
  y = t.y[0];
  return t.n > 0;
}

// A touch in frame pixels (turned back by ROUND_ROTATION).
static bool touchRead(int& x, int& y) {
  if (!touchPanel(x, y)) return false;
  ws_round::touchToFrame(x, y);
  return true;
}

// Both points in frame pixels: the Orbit goes home on a pinch in.
static void touchReadPoints(TouchPoints& t) {
  touchPanelPoints(t);
  for (int i = 0; i < t.n && i < 2; i++) {
    int x = t.x[i], y = t.y[i];
    ws_round::touchToFrame(x, y);
    t.x[i] = (int16_t)x;
    t.y[i] = (int16_t)y;
  }
}

// ---------------------------------------------------------------- the board
[[maybe_unused]] static const char* found() {
  static char line[48];
  snprintf(line, sizeof line, "tca9554 %s" DECK_DOT "gt911 %s", ws_round::haveTca ? "ok" : "MISSING",
           gt911 == ADDR_GT911 ? "0x5D" : gt911 == ADDR_GT911_ALT ? "0x14" : "MISSING");
  return line;
}

static bool boardBegin() {
  ws_round::expanderBegin();
  const ws_round::Panel panel{waveshare_28c_init_operations, sizeof waveshare_28c_init_operations, 8, 10, 50, 2, 18, 8, ROUND_PCLK_HZ};
  const bool ok = ws_round::displayBegin(panel);
  gtReset();
  gt911 = ws_round::i2cPresent(ADDR_GT911) ? ADDR_GT911 : ws_round::i2cPresent(ADDR_GT911_ALT) ? ADDR_GT911_ALT : 0;
  ws_round::backlightBegin();
  ROUND_LOG("lcd28c: %s, framebuffer %p", found(), ws_round::fb);
  return ok;
}

#ifdef ROUND_SELFTEST
void setup() {
  SelftestBoard b{};
  b.type = "waveshare-touch-lcd-28c";
  b.begin = boardBegin;
  b.push = ws_round::push;
  b.touch = touchRead;
  b.brightness = ws_round::brightness;
  b.buzzer = ws_round::buzzer;
  b.found = found;
  b.pclkHz = ROUND_PCLK_HZ;
  b.bouncePx = ROUND_BOUNCE_PX;
  roundSelftestSetup(b);
}

void loop() { roundSelftestLoop(); }
#else
void setup() {
  RoundBoard b{};
  b.type = "waveshare-touch-lcd-28c";
#ifdef ROUND_ORBIT_CLASSIC
  b.layout = new OrbitPanel(layoutOrbit28());
#else
  b.layout = new SpringPanel(layoutSpring28());
#endif
  b.begin = boardBegin;
  b.push = ws_round::push;
  b.pushDamage = ws_round::pushDamage;
  b.pushStrip = ws_round::pushStrip;
  b.touch = touchRead;
  b.touchPoints = touchReadPoints;   // the GT911 reports two fingers: pinch in to go home
  b.brightness = ws_round::brightness;
  b.bootPin = ws_round::PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 50;
  roundAppSetup(b, FW_VERSION);
}

void loop() { roundAppLoop(); }
#endif
