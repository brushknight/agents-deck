// agents deck on the Waveshare ESP32-S3-Touch-LCD-2.8C: "Orbit", every agent
// as a critter on a ring, the one that matters in the middle, on the round
// 480 x 480 screen, with touch.
//
// This file is the board: the ST7701 panel on RGB parallel (its init over a
// 3-wire SPI whose chip select sits on the IO expander), the GT911 touch,
// the TCA9554 IO expander (resets, the LCD's chip select, the buzzer) and
// the backlight PWM. Everything else (Deck Link, the fleet, the layout, the
// touch UI) is panel-core's round_app.h with layout_orbit.h.
//
// Pins, timings and the panel's init sequence are from Waveshare's demo for
// this board (ESP32-S3-Touch-LCD-2.8C-Demo.zip on the board's wiki:
// Arduino/examples/LVGL_Arduino, Display_ST7701.*, Touch_GT911.*,
// TCA9554PWR.*), checked against its ESP-IDF demo.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "layout_orbit.h"
#include "round_app.h"

#define FW_VERSION "0.1.0"

// ---------------------------------------------------------------- pins
constexpr int PIN_SDA = 15, PIN_SCL = 7, PIN_BOOT = 0;
constexpr int PIN_BACKLIGHT = 6, PIN_TOUCH_INT = 16;
constexpr int BL_CHANNEL = 1, BL_FREQ = 20000, BL_BITS = 10;
constexpr uint8_t ADDR_TCA9554 = 0x20, ADDR_GT911 = 0x5D, ADDR_GT911_ALT = 0x14;

// The TCA9554's outputs (EXIO1..8 are bits 0..7).
constexpr uint8_t EXIO_LCD_RST = 1 << 0, EXIO_TP_RST = 1 << 1, EXIO_LCD_CS = 1 << 2, EXIO_SD_CS = 1 << 3,
                  EXIO_BUZZER = 1 << 7;
constexpr uint8_t TCA_OUT = 0x01, TCA_CONFIG = 0x03;

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

// The init goes over 9-bit 3-wire SPI (SCK 2, SDA 1); its chip select is EXIO3,
// driven by hand around lcd->begin().
static Arduino_DataBus* spiBus = new Arduino_SWSPI(GFX_NOT_DEFINED /* DC */, GFX_NOT_DEFINED /* CS */, 2 /* SCK */,
                                                   1 /* MOSI */, GFX_NOT_DEFINED /* MISO */);
// RGB565 on the parallel bus. Waveshare's demo runs the clock at 30 MHz with a
// bounce buffer (arduino-esp32 3.x); without one (2.x, Arduino_GFX 1.5.0) we
// take their ESP-IDF demo's 18 MHz.
static Arduino_ESP32RGBPanel* rgbPanel = new Arduino_ESP32RGBPanel(
    40 /* DE */, 39 /* VSYNC */, 38 /* HSYNC */, 41 /* PCLK */,
    46, 3, 8, 18, 17,            // R0..R4
    14, 13, 12, 11, 10, 9,       // G0..G5
    5, 45, 48, 47, 21,           // B0..B4
    1, 50, 8, 10,                // hsync polarity, front porch, pulse width, back porch
    1, 8, 2, 18,                 // vsync
    0 /* pclk on the rising edge */, 18000000 /* pclk Hz */);
static Arduino_RGB_Display* lcd = new Arduino_RGB_Display(480, 480, rgbPanel, 0, false /* we flush */, spiBus,
                                                          GFX_NOT_DEFINED, waveshare_28c_init_operations,
                                                          sizeof(waveshare_28c_init_operations));
static uint16_t* panelFb = nullptr;
static bool haveTca = false;
static uint8_t exio = 0;               // the expander's output register, as last written
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

static bool tca(uint8_t reg, uint8_t value) {
  const uint8_t d[2] = {reg, value};
  return i2cWrite(ADDR_TCA9554, d, 2);
}

static void exioSet(uint8_t bits, bool high) {
  exio = high ? (uint8_t)(exio | bits) : (uint8_t)(exio & ~bits);
  tca(TCA_OUT, exio);
}

// All expander pins outputs; the buzzer off (it powers up on), LCD and touch
// in reset, then the LCD out of reset.
static void expanderInit() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  delay(120);
  for (int tries = 0; tries < 5 && !haveTca; tries++) {
    haveTca = i2cPresent(ADDR_TCA9554);
    if (!haveTca) delay(50);
  }
  exio = EXIO_LCD_CS | EXIO_SD_CS;     // chip selects high (idle), resets low, buzzer off
  tca(TCA_OUT, exio);
  tca(TCA_CONFIG, 0x00);
  delay(10);
  exioSet(EXIO_LCD_RST, true);
  delay(50);
}

// ---------------------------------------------------------------- GT911
// Reset with INT held low picks address 0x5D.
static void gtReset() {
  pinMode(PIN_TOUCH_INT, OUTPUT);
  digitalWrite(PIN_TOUCH_INT, LOW);
  exioSet(EXIO_TP_RST, false);
  delay(10);
  exioSet(EXIO_TP_RST, true);
  delay(200);
  digitalWrite(PIN_TOUCH_INT, HIGH);
  pinMode(PIN_TOUCH_INT, INPUT);
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
  i2cWrite(gt911, d, 3);
}

// The first touch point, in screen pixels (the panel and the touch share axes).
static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!gt911) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }   // ~60 Hz is plenty
  lastPoll = millis();
  uint8_t status = 0;
  if (!gtRead(0x814E, &status, 1)) return lastDown = false;
  if (!(status & 0x80)) { x = lastX; y = lastY; return lastDown; }        // no new report: as before
  const int n = status & 0x0F;
  if (n > 0 && n <= 5) {
    uint8_t p[6];
    if (gtRead(0x8150, p, sizeof p)) {
      lastX = p[0] | p[1] << 8;
      lastY = p[2] | p[3] << 8;
    }
  }
  gtWrite8(0x814E, 0);
  lastDown = n > 0 && n <= 5;
  x = lastX;
  y = lastY;
  return lastDown;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  expanderInit();
  exioSet(EXIO_LCD_CS, false);
  delay(10);
  const bool ok = lcd->begin();
  exioSet(EXIO_LCD_CS, true);
  if (!ok) return false;
  panelFb = lcd->getFramebuffer();
  gtReset();
  gt911 = i2cPresent(ADDR_GT911) ? ADDR_GT911 : i2cPresent(ADDR_GT911_ALT) ? ADDR_GT911_ALT : 0;
  ledcSetup(BL_CHANNEL, BL_FREQ, BL_BITS);
  ledcAttachPin(PIN_BACKLIGHT, BL_CHANNEL);
  ROUND_LOG("lcd28c: tca9554 %s, gt911 0x%02X, framebuffer %p", haveTca ? "ok" : "MISSING", gt911, panelFb);
  return panelFb != nullptr;
}

// One frame into the panel's own framebuffer (PSRAM), then out of the cache.
static void push(const uint16_t* src) {
  if (!panelFb) return;
  memcpy(panelFb, src, 480 * 480 * 2);
  Cache_WriteBack_Addr((uint32_t)panelFb, 480 * 480 * 2);
}

static void brightness(uint8_t level) { ledcWrite(BL_CHANNEL, (uint32_t)level * ((1 << BL_BITS) - 1) / 255); }

void setup() {
  RoundBoard b{};
  b.type = "waveshare-touch-lcd-28c";
  b.layout = new OrbitPanel(layoutOrbit28());
  b.begin = boardBegin;
  b.push = push;
  b.touch = touchRead;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 50;
  roundAppSetup(b, FW_VERSION);
}

void loop() { roundAppLoop(); }
