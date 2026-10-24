// agents deck on the Waveshare ESP32-S3-Touch-LCD-4: the deck as a 4 x 4 grid
// of tiles on the 480 x 480 screen, with touch.
//
// This file is the board: the ST7701 panel on RGB parallel, the GT911 touch,
// and the CH32V003 that the board uses as IO expander (resets, power, the
// backlight PWM). Everything else (Deck Link, the fleet, the layout, the
// touch UI) is panel-core's grid_app.h.
//
// Pins and the panel's init sequence are from Waveshare's BSP for this board
// (esp32_s3_touch_lcd_4 3.0.0 on the ESP component registry) and their
// Arduino demos (github.com/waveshareteam/ESP32-S3-Touch-LCD-4).
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "grid_app.h"
#include "layout_grid.h"

#define FW_VERSION "0.1.0"

// ---------------------------------------------------------------- pins
constexpr int kW = 480, kH = 480;
constexpr int PIN_SDA = 15, PIN_SCL = 7, PIN_BOOT = 0;
constexpr uint8_t ADDR_EXPANDER = 0x24;        // CH32V003 (board V4); V1-V3 have a TCA9554 at 0x20
constexpr uint8_t ADDR_TCA9554 = 0x20;
constexpr uint8_t ADDR_GT911 = 0x5D, ADDR_GT911_ALT = 0x14;

// The CH32V003's registers (custom_io_expander_ch32v003 in the BSP).
constexpr uint8_t CH32_DIR = 0x02, CH32_OUT = 0x03, CH32_PWM = 0x05;
constexpr uint8_t CH32_TP_RST = 1 << 1, CH32_LCD_RST = 1 << 3, CH32_SYS_EN = 1 << 5;

// ---------------------------------------------------------------- display
// ST7701 init over 3-wire SPI (CS 42, SCK 2, SDA 1), then RGB565 on the
// parallel bus. Timing as in Waveshare's Arduino demo.
static Arduino_DataBus* spiBus = new Arduino_SWSPI(GFX_NOT_DEFINED, 42 /* CS */, 2 /* SCK */, 1 /* MOSI */, GFX_NOT_DEFINED);
static Arduino_ESP32RGBPanel* rgbPanel = new Arduino_ESP32RGBPanel(
    40 /* DE */, 39 /* VSYNC */, 38 /* HSYNC */, 41 /* PCLK */,
    46, 3, 8, 18, 17,            // R0..R4
    14, 13, 12, 11, 10, 9,       // G0..G5
    5, 45, 48, 47, 21,           // B0..B4
    1, 10, 8, 50,                // hsync polarity, front porch, pulse width, back porch
    1, 10, 8, 20);               // vsync
static Arduino_RGB_Display* lcd = new Arduino_RGB_Display(
    480, 480, rgbPanel, 0, false /* we flush */, spiBus, GFX_NOT_DEFINED,
    st7701_type1_init_operations, sizeof(st7701_type1_init_operations));
static uint16_t* panelFb = nullptr;
static bool haveCh32 = false, haveTca = false;
static uint8_t gt911 = 0;

// ---------------------------------------------------------------- I2C
static bool i2cWrite(uint8_t addr, const uint8_t* data, size_t n) {
  Wire.beginTransmission(addr);
  Wire.write(data, n);
  return Wire.endTransmission() == 0;
}

static bool i2cPresent(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

static bool ch32(uint8_t reg, uint8_t value) {
  const uint8_t d[2] = {reg, value};
  return i2cWrite(ADDR_EXPANDER, d, 2);
}

static bool tca(uint8_t reg, uint8_t value) {
  const uint8_t d[2] = {reg, value};
  return i2cWrite(ADDR_TCA9554, d, 2);
}

// A stuck slave can hold SDA low after a reset: clock it free (as the BSP does).
static void i2cRecover() {
  pinMode(PIN_SDA, INPUT_PULLUP);
  pinMode(PIN_SCL, OUTPUT);
  for (int i = 0; i < 9; i++) {
    digitalWrite(PIN_SCL, LOW);
    delayMicroseconds(5);
    digitalWrite(PIN_SCL, HIGH);
    delayMicroseconds(5);
  }
  pinMode(PIN_SCL, INPUT_PULLUP);
}

// Power, LCD and touch out of reset.
static void expanderInit() {
  i2cRecover();
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  for (int tries = 0; tries < 5 && !haveCh32 && !haveTca; tries++) {
    haveCh32 = i2cPresent(ADDR_EXPANDER);
    haveTca = !haveCh32 && i2cPresent(ADDR_TCA9554);
    if (!haveCh32 && !haveTca) delay(50);
  }
  if (haveCh32) {                                   // V4
    ch32(CH32_DIR, 0xFF);
    ch32(CH32_OUT, CH32_SYS_EN);                    // keep power, hold LCD and touch in reset
    delay(20);
    ch32(CH32_OUT, CH32_SYS_EN | CH32_LCD_RST | CH32_TP_RST);
    delay(200);
  } else if (haveTca) {                             // V1-V3: EXIO0 touch reset, EXIO1 backlight, EXIO2 LCD reset
    tca(0x03, 0x00);                                // all outputs
    tca(0x01, 0x00);
    delay(20);
    tca(0x01, 0x07);
    delay(200);
  }
}

// ---------------------------------------------------------------- GT911
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
  i2cWrite(gt911, d, 3);
}

static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!gt911) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }   // ~60 Hz is plenty
  lastPoll = millis();
  uint8_t status = 0;
  if (!gtRead(0x814E, &status, 1)) return lastDown = false;
  if (!(status & 0x80)) { x = lastX; y = lastY; return lastDown; }        // no new report: same as before
  const int n = status & 0x0F;
  if (n > 0) {
    uint8_t p[6];
    if (gtRead(0x8150, p, sizeof p)) {
      lastX = p[0] | p[1] << 8;
      lastY = p[2] | p[3] << 8;
    }
  }
  gtWrite8(0x814E, 0);
  lastDown = n > 0;
  x = lastX;
  y = lastY;
  return lastDown;
}

// Touch in frame pixels: turned 180 degrees like the image.
static bool touch(int& x, int& y) {
  if (!touchRead(x, y)) return false;
  x = kW - 1 - x;
  y = kH - 1 - y;
  return true;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  expanderInit();
  if (!lcd->begin()) return false;
  panelFb = lcd->getFramebuffer();
  gt911 = i2cPresent(ADDR_GT911) ? ADDR_GT911 : i2cPresent(ADDR_GT911_ALT) ? ADDR_GT911_ALT : 0;
  PANEL_LOG("lcd4: expander %s, gt911 0x%02X, framebuffer %p", haveCh32 ? "ch32v003" : haveTca ? "tca9554" : "NONE",
            gt911, panelFb);
  return panelFb != nullptr;
}

// The panel is mounted upside down relative to its scan order: the board
// is upright with the image turned 180 degrees, i.e. the frame reversed.
// The touch reports in the panel's own orientation, so it turns the same way.
// One frame into the panel's own framebuffer (PSRAM), turned, then out of the cache.
static void push(const uint16_t* src) {
  if (!panelFb) return;
  const uint32_t* in = (const uint32_t*)src;
  uint32_t* out = (uint32_t*)panelFb + kW * kH / 2 - 1;
  for (int i = 0; i < kW * kH / 2; i++, out--) {
    const uint32_t two = in[i];
    *out = two >> 16 | two << 16;                // two pixels at a time, swapped
  }
  Cache_WriteBack_Addr((uint32_t)panelFb, kW * kH * 2);
}

// Backlight: the CH32V003's PWM, inverted (0 is brightest, as in the BSP);
// V1-V3 can only switch it, with EXIO1.
static void brightness(uint8_t level) {
  if (haveCh32) ch32(CH32_PWM, (uint8_t)(255 - level));
  else if (haveTca) tca(0x01, level ? 0x07 : 0x05);
}

void setup() {
  GridBoard b{};
  b.type = "waveshare-touch-lcd-4";
  static const GridPanel layout(layoutLcd4());
  b.layout = &layout;
  b.begin = boardBegin;
  b.push = push;
  b.touch = touch;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 60;
  gridAppSetup(b, FW_VERSION);
}

void loop() { gridAppLoop(); }
