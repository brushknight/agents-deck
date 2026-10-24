// agents deck on the Waveshare ESP32-S3-Touch-LCD-2.1: "Dial", one agent at
// a time on the round 480 x 480 screen with the whole fleet as dots round
// it, with touch.
//
// This file is the board: the ST7701 panel on RGB parallel (its init over a
// 3-wire SPI whose chip select sits on the IO expander), the CST820 touch,
// the TCA9554 IO expander (resets, the LCD's chip select, the buzzer) and
// the backlight PWM. Everything else (Deck Link, the fleet, the layout, the
// touch UI) is panel-core's round_app.h with layout_dial.h.
//
// Pins, timings and the panel's init sequence are from Waveshare's demo for
// this board (ESP32-S3-Touch-LCD-2.1-Demo.zip on the board's wiki:
// Arduino/examples/LVGL_Arduino, Display_ST7701.*, Touch_CST820.*,
// TCA9554PWR.*), checked against its ESP-IDF demo.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "layout_dial.h"
#include "round_app.h"

#define FW_VERSION "0.1.0"

// ---------------------------------------------------------------- pins
constexpr int PIN_SDA = 15, PIN_SCL = 7, PIN_BOOT = 0;
constexpr int PIN_BACKLIGHT = 6, PIN_TOUCH_INT = 16;
constexpr int BL_CHANNEL = 1, BL_FREQ = 20000, BL_BITS = 10;
constexpr uint8_t ADDR_TCA9554 = 0x20, ADDR_CST820 = 0x15;

// The TCA9554's outputs (EXIO1..8 are bits 0..7).
constexpr uint8_t EXIO_LCD_RST = 1 << 0, EXIO_TP_RST = 1 << 1, EXIO_LCD_CS = 1 << 2, EXIO_SD_CS = 1 << 3,
                  EXIO_BUZZER = 1 << 7;
constexpr uint8_t TCA_OUT = 0x01, TCA_CONFIG = 0x03;

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

// The init goes over 9-bit 3-wire SPI (SCK 2, SDA 1); its chip select is EXIO3,
// driven by hand around lcd->begin().
static Arduino_DataBus* spiBus = new Arduino_SWSPI(GFX_NOT_DEFINED /* DC */, GFX_NOT_DEFINED /* CS */, 2 /* SCK */,
                                                   1 /* MOSI */, GFX_NOT_DEFINED /* MISO */);
// RGB565 on the parallel bus, with the porches and the 16 MHz clock of
// Waveshare's Arduino demo (their ESP-IDF demo runs 18 MHz).
static Arduino_ESP32RGBPanel* rgbPanel = new Arduino_ESP32RGBPanel(
    40 /* DE */, 39 /* VSYNC */, 38 /* HSYNC */, 41 /* PCLK */,
    46, 3, 8, 18, 17,            // R0..R4
    14, 13, 12, 11, 10, 9,       // G0..G5
    5, 45, 48, 47, 21,           // B0..B4
    1, 50, 8, 10,                // hsync polarity, front porch, pulse width, back porch
    1, 8, 3, 8,                  // vsync
    0 /* pclk on the rising edge */, 16000000 /* pclk Hz */);
static Arduino_RGB_Display* lcd = new Arduino_RGB_Display(480, 480, rgbPanel, 0, false /* we flush */, spiBus,
                                                          GFX_NOT_DEFINED, waveshare_21_init_operations,
                                                          sizeof(waveshare_21_init_operations));
static uint16_t* panelFb = nullptr;
static bool haveTca = false;
static uint8_t exio = 0;               // the expander's output register, as last written
static bool cst820 = false;

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

// ---------------------------------------------------------------- CST820
static void cstWrite8(uint8_t reg, uint8_t v) {
  const uint8_t d[2] = {reg, v};
  i2cWrite(ADDR_CST820, d, 2);
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
  pinMode(PIN_TOUCH_INT, INPUT_PULLUP);
  exioSet(EXIO_TP_RST, false);
  delay(10);
  exioSet(EXIO_TP_RST, true);
  delay(50);
  cst820 = i2cPresent(ADDR_CST820);
  if (cst820) cstWrite8(0xFE, 0xFF);   // auto sleep off
}

// One touch point, in screen pixels (the panel and the touch share axes).
static bool touchRead(int& x, int& y) {
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

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  expanderInit();
  exioSet(EXIO_LCD_CS, false);
  delay(10);
  const bool ok = lcd->begin();
  exioSet(EXIO_LCD_CS, true);
  if (!ok) return false;
  panelFb = lcd->getFramebuffer();
  cstInit();
  ledcSetup(BL_CHANNEL, BL_FREQ, BL_BITS);
  ledcAttachPin(PIN_BACKLIGHT, BL_CHANNEL);
  ROUND_LOG("lcd21: tca9554 %s, cst820 %s, framebuffer %p", haveTca ? "ok" : "MISSING", cst820 ? "ok" : "MISSING",
            panelFb);
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
  b.type = "waveshare-touch-lcd-21";
  b.layout = new DialPanel(layoutDial21());
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
