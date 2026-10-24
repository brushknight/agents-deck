// agents deck on the Waveshare ESP32-S3-Touch-LCD-2.1: "Dial", one agent at
// a time on the round 480 x 480 screen with the whole fleet as dots round
// it, with touch.
//
// This file is what is particular to the board: the CST820 touch,
// its ST7701 init and RGB timing. The wiring both round boards share (the
// TCA9554 expander, the RGB bus, the backlight) is panel-core's
// waveshare_round.h. Everything else (Deck Link, the fleet, the layout, the
// touch UI) is panel-core's round_app.h with layout_dial.h.
//
// Pins, timings and the panel's init sequence are from Waveshare's demo for
// this board (ESP32-S3-Touch-LCD-2.1-Demo.zip on the board's wiki:
// Arduino/examples/LVGL_Arduino, Display_ST7701.*, Touch_CST820.*,
// TCA9554PWR.*), checked against its ESP-IDF demo.
//
// Built with -DROUND_SELFTEST (env selftest) it runs the hardware self-test
// instead (round_selftest.h): no Deck Link.
#include <Arduino.h>

#include "layout_dial.h"
#include "round_app.h"
#include "round_selftest.h"
#include "waveshare_round.h"

#define FW_VERSION "0.2.0"

// 16 MHz as in Waveshare's Arduino demo (their ESP-IDF demo runs 18 MHz).
#ifndef ROUND_PCLK_HZ
#define ROUND_PCLK_HZ 16000000
#endif

constexpr uint8_t ADDR_CST820 = 0x15;
static bool cst820 = false;

// ---------------------------------------------------------------- display
// The ST7701's init, verbatim from Waveshare's demo for this board (their
// Arduino and ESP-IDF demos agree byte for byte). Arduino_GFX 1.5.0's
// st7701_type7_init_operations is close but skips INVOFF and waits less after
// Sleep Out. DELAY takes a byte, so 480 ms is 255 + 225.
static const uint8_t waveshare_21_init_operations[] = {
    BEGIN_WRITE,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x10,
    WRITE_C8_D16, 0xC0, 0x3B, 0x00,
    WRITE_C8_D16, 0xC1, 0x0B, 0x02,
    WRITE_C8_D16, 0xC2, 0x07, 0x02,
    WRITE_C8_D8, 0xCC, 0x10,
    WRITE_C8_D8, 0xCD, 0x08,
    WRITE_COMMAND_8, 0xB0, WRITE_BYTES, 16,
    0x00, 0x11, 0x16, 0x0E, 0x11, 0x06, 0x05, 0x09, 0x08, 0x21, 0x06, 0x13, 0x10, 0x29, 0x31, 0x18,
    WRITE_COMMAND_8, 0xB1, WRITE_BYTES, 16,
    0x00, 0x11, 0x16, 0x0E, 0x11, 0x07, 0x05, 0x09, 0x09, 0x21, 0x05, 0x13, 0x11, 0x2A, 0x31, 0x18,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x11,
    WRITE_C8_D8, 0xB0, 0x6D,
    WRITE_C8_D8, 0xB1, 0x37,
    WRITE_C8_D8, 0xB2, 0x81,
    WRITE_C8_D8, 0xB3, 0x80,
    WRITE_C8_D8, 0xB5, 0x43,
    WRITE_C8_D8, 0xB7, 0x85,
    WRITE_C8_D8, 0xB8, 0x20,
    WRITE_C8_D8, 0xC1, 0x78,
    WRITE_C8_D8, 0xC2, 0x78,
    WRITE_C8_D8, 0xD0, 0x88,
    WRITE_COMMAND_8, 0xE0, WRITE_BYTES, 3, 0x00, 0x00, 0x02,
    WRITE_COMMAND_8, 0xE1, WRITE_BYTES, 11, 0x03, 0xA0, 0x00, 0x00, 0x04, 0xA0, 0x00, 0x00, 0x00, 0x20, 0x20,
    WRITE_COMMAND_8, 0xE2, WRITE_BYTES, 13,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0xE3, WRITE_BYTES, 4, 0x00, 0x00, 0x11, 0x00,
    WRITE_C8_D16, 0xE4, 0x22, 0x00,
    WRITE_COMMAND_8, 0xE5, WRITE_BYTES, 16,
    0x05, 0xEC, 0xA0, 0xA0, 0x07, 0xEE, 0xA0, 0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0xE6, WRITE_BYTES, 4, 0x00, 0x00, 0x11, 0x00,
    WRITE_C8_D16, 0xE7, 0x22, 0x00,
    WRITE_COMMAND_8, 0xE8, WRITE_BYTES, 16,
    0x06, 0xED, 0xA0, 0xA0, 0x08, 0xEF, 0xA0, 0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0xEB, WRITE_BYTES, 7, 0x00, 0x00, 0x40, 0x40, 0x00, 0x00, 0x00,
    WRITE_COMMAND_8, 0xED, WRITE_BYTES, 16,
    0xFF, 0xFF, 0xFF, 0xBA, 0x0A, 0xBF, 0x45, 0xFF, 0xFF, 0x54, 0xFB, 0xA0, 0xAB, 0xFF, 0xFF, 0xFF,
    WRITE_COMMAND_8, 0xEF, WRITE_BYTES, 6, 0x10, 0x0D, 0x04, 0x08, 0x3F, 0x1F,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x13,
    WRITE_C8_D8, 0xEF, 0x08,
    WRITE_COMMAND_8, 0xFF, WRITE_BYTES, 5, 0x77, 0x01, 0x00, 0x00, 0x00,
    WRITE_C8_D8, 0x36, 0x00,
    WRITE_C8_D8, 0x3A, 0x66,
    WRITE_COMMAND_8, 0x11,
    END_WRITE,
    DELAY, 255,
    DELAY, 225,
    BEGIN_WRITE,
    WRITE_COMMAND_8, 0x20,
    END_WRITE,
    DELAY, 120,
    BEGIN_WRITE,
    WRITE_COMMAND_8, 0x29,
    END_WRITE,
};

// ---------------------------------------------------------------- CST820
static void cstWrite8(uint8_t reg, uint8_t v) {
  const uint8_t d[2] = {reg, v};
  ws_round::i2cWrite(ADDR_CST820, d, 2);
}

static bool cstRead(uint8_t reg, uint8_t* out, size_t n) {
  Wire.beginTransmission(ADDR_CST820);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(ADDR_CST820, (uint8_t)n) != n) return false;
  for (size_t i = 0; i < n; i++) out[i] = Wire.read();
  return true;
}

// Reset, then keep it awake: it sleeps after a while and misses the first touch.
static void cstInit() {
  pinMode(ws_round::PIN_TOUCH_INT, INPUT_PULLUP);
  ws_round::exioSet(ws_round::EXIO_TP_RST, false);
  delay(10);
  ws_round::exioSet(ws_round::EXIO_TP_RST, true);
  delay(50);
  cst820 = ws_round::i2cPresent(ADDR_CST820);
  if (cst820) cstWrite8(0xFE, 0xFF);   // auto sleep off
}

// One touch point, in screen pixels (the panel and the touch share axes).
static bool touchPanel(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!cst820) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }   // ~60 Hz is plenty
  lastPoll = millis();
  uint8_t p[6];   // gesture, points, x high, x low, y high, y low
  if (!cstRead(0x01, p, sizeof p)) return lastDown = false;
  lastDown = p[1] > 0;
  if (lastDown) {
    lastX = (p[2] & 0x0F) << 8 | p[3];
    lastY = (p[4] & 0x0F) << 8 | p[5];
  }
  x = lastX;
  y = lastY;
  return lastDown;
}

// A touch in frame pixels (turned back by ROUND_ROTATION).
static bool touchRead(int& x, int& y) {
  if (!touchPanel(x, y)) return false;
  ws_round::touchToFrame(x, y);
  return true;
}

// ---------------------------------------------------------------- the board
[[maybe_unused]] static const char* found() {
  static char line[48];
  snprintf(line, sizeof line, "tca9554 %s" DECK_DOT "cst820 %s", ws_round::haveTca ? "ok" : "MISSING",
           cst820 ? "ok" : "MISSING");
  return line;
}

static bool boardBegin() {
  ws_round::expanderBegin();
  const ws_round::Panel panel{waveshare_21_init_operations, sizeof waveshare_21_init_operations, 8, 10, 50, 3, 8, 8, ROUND_PCLK_HZ};
  const bool ok = ws_round::displayBegin(panel);
  cstInit();
  ws_round::backlightBegin();
  ROUND_LOG("lcd21: %s, framebuffer %p", found(), ws_round::fb);
  return ok;
}

#ifdef ROUND_SELFTEST
void setup() {
  SelftestBoard b{};
  b.type = "waveshare-touch-lcd-21";
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
  b.type = "waveshare-touch-lcd-21";
  b.layout = new DialPanel(layoutDial21());
  b.begin = boardBegin;
  b.push = ws_round::push;
  b.touch = touchRead;
  b.brightness = ws_round::brightness;
  b.bootPin = ws_round::PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 50;
  roundAppSetup(b, FW_VERSION);
}

void loop() { roundAppLoop(); }
#endif
