// agents deck on the Seeed SenseCAP Indicator: the deck as a 4 x 4 grid of
// tiles on the 480 x 480 screen, with touch.
//
// This file is the board: the ST7701S panel on RGB parallel, the FT6336 touch,
// and the TCA9535 IO expander that holds the panel's chip select and the
// resets. Everything else (Deck Link, the fleet, the layout, the touch UI) is
// panel-core's grid_app.h.
//
// Pins, timing and the panel's init sequence are from Seeed's BSP
// (github.com/Seeed-Solution/SenseCAP_Indicator_ESP32, components/bsp:
// sensecap_indicator_board.c and lcd_panel_config.c, lcd_panel_st7701s_init).
//
// Seeed shipped two panels: "GX" needs the ST7701S init over 3-wire SPI and
// has its touch at 0x48; "DX" comes up on its own and has its touch at 0x38.
// The touch address tells them apart.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "grid_app.h"
#include "layout_big.h"

#define FW_VERSION "0.1.0"

// ---------------------------------------------------------------- pins
constexpr int kW = 480, kH = 480;
constexpr int PIN_SDA = 39, PIN_SCL = 40, PIN_USER = 38, PIN_BL = 45;
constexpr int PIN_LCD_SCK = 41, PIN_LCD_MOSI = 48;
constexpr uint8_t ADDR_TCA9535 = 0x20, ADDR_TCA9535_ALT = 0x39;
constexpr uint8_t ADDR_TOUCH_GX = 0x48, ADDR_TOUCH_DX = 0x38;

// TCA9535 lines (16 bits: port 0 is IO0-7, port 1 is IO8-15).
constexpr uint16_t EXP_LCD_CS = 1 << 4, EXP_LCD_RST = 1 << 5, EXP_TP_RST = 1 << 7;
constexpr uint16_t EXP_RP2040_RST = 1 << 8, EXP_BMP_PWR = 1 << 10;
constexpr uint16_t EXP_OUTPUTS = EXP_LCD_CS | EXP_LCD_RST | EXP_TP_RST | EXP_RP2040_RST | EXP_BMP_PWR;

// Touch orientation, relative to the panel's scan order. Seeed's BSP inverts Y.
#ifndef TOUCH_INVERT_X
#define TOUCH_INVERT_X 0
#endif
#ifndef TOUCH_INVERT_Y
#define TOUCH_INVERT_Y 1
#endif

// ---------------------------------------------------------------- display
// RGB565 on the parallel bus; porches as in the BSP. The panel scans straight
// from PSRAM (arduino-esp32 2.0.17 has no bounce buffer), and at the BSP's
// 18 MHz the frame copies starve it and the picture shakes: LCD_PCLK_HZ is lower.
// No init bus here: the ST7701S init goes out by hand below, because its chip
// select sits on the IO expander.
#ifndef LCD_PCLK_HZ
#define LCD_PCLK_HZ 12000000
#endif
static Arduino_ESP32RGBPanel* rgbPanel = new Arduino_ESP32RGBPanel(
    18 /* DE */, 17 /* VSYNC */, 16 /* HSYNC */, 21 /* PCLK */,
    4, 3, 2, 1, 0,               // R0..R4
    10, 9, 8, 7, 6, 5,           // G0..G5
    15, 14, 13, 12, 11,          // B0..B4
    1, 10, 8, 50,                // hsync polarity, front porch, pulse width, back porch
    1, 10, 8, 20,                // vsync
    0 /* pclk on the rising edge */, LCD_PCLK_HZ);
static Arduino_RGB_Display* lcd = new Arduino_RGB_Display(
    kW, kH, rgbPanel, 0, false /* we flush */, nullptr, GFX_NOT_DEFINED, nullptr, 0);
static uint16_t* panelFb = nullptr;
static uint8_t expAddr = 0, touchAddr = 0;
static uint16_t expOut = 0;

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

static bool i2cRead(uint8_t addr, uint8_t reg, uint8_t* out, size_t n) {
  Wire.beginTransmission(addr);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(addr, (uint8_t)n) != n) return false;
  for (size_t i = 0; i < n; i++) out[i] = Wire.read();
  return true;
}

// ---------------------------------------------------------------- TCA9535
static bool expWrite16(uint8_t reg, uint16_t v) {
  const uint8_t d[3] = {reg, (uint8_t)v, (uint8_t)(v >> 8)};
  return i2cWrite(expAddr, d, 3);
}

static void expSet(uint16_t lines, bool high) {
  expOut = high ? expOut | lines : expOut & ~lines;
  expWrite16(0x02, expOut);
}

// Outputs high first (CS idle, nothing in reset, RP2040 running), then out of
// input mode; the rest stay inputs.
static bool expanderInit() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  for (int tries = 0; tries < 5 && !expAddr; tries++) {
    expAddr = i2cPresent(ADDR_TCA9535) ? ADDR_TCA9535 : i2cPresent(ADDR_TCA9535_ALT) ? ADDR_TCA9535_ALT : 0;
    if (!expAddr) delay(50);
  }
  if (!expAddr) return false;
  expOut = EXP_OUTPUTS;
  expWrite16(0x02, expOut);
  expWrite16(0x06, (uint16_t)~EXP_OUTPUTS);
  expSet(EXP_TP_RST, false);                         // touch out of reset, as the BSP does
  delay(10);
  expSet(EXP_TP_RST, true);
  delay(50);
  return true;
}

// ---------------------------------------------------------------- ST7701S
// 3-wire SPI, 9 bits a word: the first bit says command (0) or data (1).
// SCK and MOSI are GPIOs, CS is on the expander, so CS stays low for the
// whole sequence and the words are framed by their bit count.
static void lcdWord(uint16_t w) {
  for (int b = 8; b >= 0; b--) {
    digitalWrite(PIN_LCD_MOSI, (w >> b) & 1);
    digitalWrite(PIN_LCD_SCK, HIGH);
    delayMicroseconds(1);
    digitalWrite(PIN_LCD_SCK, LOW);
    delayMicroseconds(1);
  }
}

static void lcdCmd(uint8_t c, std::initializer_list<uint8_t> data = {}) {
  lcdWord(c);
  for (uint8_t d : data) lcdWord(0x100 | d);
}

static void st7701sInit() {
  pinMode(PIN_LCD_SCK, OUTPUT);
  pinMode(PIN_LCD_MOSI, OUTPUT);
  digitalWrite(PIN_LCD_SCK, LOW);
  expSet(EXP_LCD_RST, false);
  delay(10);
  expSet(EXP_LCD_RST, true);
  delay(120);
  expSet(EXP_LCD_CS, false);

  lcdCmd(0xFF, {0x77, 0x01, 0x00, 0x00, 0x10});      // command bank 2, page 0
  lcdCmd(0xC0, {0x3B, 0x00});
  lcdCmd(0xC1, {0x0D, 0x02});
  lcdCmd(0xC2, {0x31, 0x05});
  lcdCmd(0xC7, {0x04});
  lcdCmd(0xCD, {0x08});
  lcdCmd(0xB0, {0x00, 0x11, 0x18, 0x0E, 0x11, 0x06, 0x07, 0x08, 0x07, 0x22, 0x04, 0x12, 0x0F, 0xAA, 0x31, 0x18});
  lcdCmd(0xB1, {0x00, 0x11, 0x19, 0x0E, 0x12, 0x07, 0x08, 0x08, 0x08, 0x22, 0x04, 0x11, 0x11, 0xA9, 0x32, 0x18});
  lcdCmd(0xFF, {0x77, 0x01, 0x00, 0x00, 0x11});      // page 1: power
  lcdCmd(0xB0, {0x60});
  lcdCmd(0xB1, {0x32});
  lcdCmd(0xB2, {0x07});
  lcdCmd(0xB3, {0x80});
  lcdCmd(0xB5, {0x49});
  lcdCmd(0xB7, {0x85});
  lcdCmd(0xB8, {0x21});
  lcdCmd(0xC1, {0x78});
  lcdCmd(0xC2, {0x78});
  delay(20);
  lcdCmd(0xE0, {0x00, 0x1B, 0x02});
  lcdCmd(0xE1, {0x08, 0xA0, 0x00, 0x00, 0x07, 0xA0, 0x00, 0x00, 0x00, 0x44, 0x44});
  lcdCmd(0xE2, {0x11, 0x11, 0x44, 0x44, 0xED, 0xA0, 0x00, 0x00, 0xEC, 0xA0, 0x00, 0x00});
  lcdCmd(0xE3, {0x00, 0x00, 0x11, 0x11});
  lcdCmd(0xE4, {0x44, 0x44});
  lcdCmd(0xE5, {0x0A, 0xE9, 0xD8, 0xA0, 0x0C, 0xEB, 0xD8, 0xA0, 0x0E, 0xED, 0xD8, 0xA0, 0x10, 0xEF, 0xD8, 0xA0});
  lcdCmd(0xE6, {0x00, 0x00, 0x11, 0x11});
  lcdCmd(0xE7, {0x44, 0x44});
  lcdCmd(0xE8, {0x09, 0xE8, 0xD8, 0xA0, 0x0B, 0xEA, 0xD8, 0xA0, 0x0D, 0xEC, 0xD8, 0xA0, 0x0F, 0xEE, 0xD8, 0xA0});
  lcdCmd(0xEB, {0x02, 0x00, 0xE4, 0xE4, 0x88, 0x00, 0x40});
  lcdCmd(0xEC, {0x3C, 0x00});
  lcdCmd(0xED, {0xAB, 0x89, 0x76, 0x54, 0x02, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x20, 0x45, 0x67, 0x98, 0xBA});
  lcdCmd(0x36, {0x10});
  lcdCmd(0xFF, {0x77, 0x01, 0x00, 0x00, 0x13});
  lcdCmd(0xE5, {0xE4});
  lcdCmd(0xFF, {0x77, 0x01, 0x00, 0x00, 0x00});      // back to the normal command set
  lcdCmd(0x3A, {0x60});                              // RGB666 (the bus carries 565 in the top bits)
  lcdCmd(0x21);                                      // inversion on
  lcdCmd(0x11);                                      // sleep out
  delay(120);
  lcdCmd(0x29);                                      // display on
  delay(20);
  expSet(EXP_LCD_CS, true);
}

// ---------------------------------------------------------------- FT6336
static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!touchAddr) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }   // ~60 Hz is plenty
  lastPoll = millis();
  uint8_t p[5];                                       // TD_STATUS, P1_XH, P1_XL, P1_YH, P1_YL
  if (!i2cRead(touchAddr, 0x02, p, sizeof p)) return lastDown = false;
  const int n = p[0] & 0x0F;
  lastDown = n > 0 && n <= 2;
  if (lastDown) {
    lastX = (p[1] & 0x0F) << 8 | p[2];
    lastY = (p[3] & 0x0F) << 8 | p[4];
  }
  x = lastX;
  y = lastY;
  return lastDown;
}

static bool touch(int& x, int& y) {
  if (!touchRead(x, y)) return false;
  if (TOUCH_INVERT_X) x = kW - 1 - x;
  if (TOUCH_INVERT_Y) y = kH - 1 - y;
  x = constrain(x, 0, kW - 1);
  y = constrain(y, 0, kH - 1);
  return true;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  const bool exp = expanderInit();
  touchAddr = i2cPresent(ADDR_TOUCH_GX) ? ADDR_TOUCH_GX : i2cPresent(ADDR_TOUCH_DX) ? ADDR_TOUCH_DX : 0;
  if (exp && touchAddr != ADDR_TOUCH_DX) st7701sInit();   // the DX panel needs no init
  if (!lcd->begin()) return false;
  panelFb = lcd->getFramebuffer();
  PANEL_LOG("indicator: expander 0x%02X, touch 0x%02X (%s), framebuffer %p", expAddr, touchAddr,
            touchAddr == ADDR_TOUCH_DX ? "dx" : "gx", panelFb);
  return panelFb != nullptr;
}

// One frame into the panel's own framebuffer (PSRAM), then out of the cache.
static void push(const uint16_t* src) {
  if (!panelFb) return;
  memcpy(panelFb, src, kW * kH * 2);
  Cache_WriteBack_Addr((uint32_t)panelFb, kW * kH * 2);
}

static void brightness(uint8_t level) {
  static bool attached = false;
  if (!attached) {
    ledcSetup(0, 20000, 8);
    ledcAttachPin(PIN_BL, 0);
    attached = true;
  }
  ledcWrite(0, level);
}

void setup() {
  GridBoard b{};
  b.type = "sensecap-indicator";
  static const BigPanel layout(specBig480());   // the desk panel's UI at 2/3
  b.layout = &layout;
  b.begin = boardBegin;
  b.push = push;
  b.touch = touch;
  b.brightness = brightness;
  b.bootPin = PIN_USER;                          // the button on top; GPIO0 is a data line here
  b.brightActive = 255;
  b.brightIdle = 60;
  gridAppSetup(b, FW_VERSION);
}

void loop() { gridAppLoop(); }
