// agents deck on the Freenove ESP32-S3 Display 2.8" (FNK0104B): the 240 x 320
// LCD (touch) with the 1.8" AMOLED's UI at half size (layout_square.h,
// specFreenove28()): tiles in one scrolling list, the card with its icon
// buttons, the prompts, swipe right to go back.
//
// This file is the board: the ILI9341 on SPI, the FT6336U touch on I2C and the
// backlight. Everything else (Deck Link over USB or Wi-Fi, the fleet, the
// layout, the touch UI) is panel-core's grid_app.h.
//
// Pins and the panel's settings (BGR, inverted, no reset line) are Freenove's
// (github.com/Freenove/Freenove_ESP32_S3_Display: the FNK0104AB TFT_eSPI setup
// and the touch sketches). The ES8311 codec on the same I2C bus, the WS2812 and
// the SD slot are left alone.
//
// Build with -DFREENOVE28_TURNED to turn the picture and touch 180 degrees
// (the board standing with its USB-C at the top), and with
// -DFREENOVE28_NOT_INVERTED for a panel that shows the colours inverted.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>

#include "grid_app.h"
#include "layout_square.h"

#define FW_VERSION "1.0.0"

// ---------------------------------------------------------------- pins
constexpr int PIN_LCD_CS = 10, PIN_LCD_MOSI = 11, PIN_LCD_SCK = 12, PIN_LCD_MISO = 13, PIN_LCD_DC = 46;
constexpr int PIN_LCD_BL = 45;                     // through a BSS138: high is on
constexpr int PIN_SDA = 16, PIN_SCL = 15, PIN_TP_RST = 18, PIN_BOOT = 0;
constexpr uint8_t ADDR_FT6336 = 0x38;
constexpr int W = 240, H = 320;
constexpr int32_t kSpiHz = 40000000;               // Freenove's rate

#ifdef FREENOVE28_TURNED
constexpr bool kTurned = true;
#else
constexpr bool kTurned = false;
#endif
#ifdef FREENOVE28_NOT_INVERTED
constexpr bool kInverted = false;
#else
constexpr bool kInverted = true;
#endif

// FSPI on its own IO_MUX pins (10-13).
static Arduino_DataBus* bus = new Arduino_ESP32SPI(PIN_LCD_DC, PIN_LCD_CS, PIN_LCD_SCK, PIN_LCD_MOSI, PIN_LCD_MISO, FSPI);
// No reset line: the panel comes up with power. ips = true keeps the inversion
// on, as Freenove's setup has it.
static Arduino_ILI9341* lcd = new Arduino_ILI9341(bus, GFX_NOT_DEFINED, kTurned ? 2 : 0, kInverted);
static bool haveTouch = false;

// ---------------------------------------------------------------- touch
// The FT6336U: register 0xA8 is the vendor id (0x11), 0xA6 the firmware.
static bool ftRead(uint8_t reg, uint8_t* buf, uint8_t n) {
  Wire.beginTransmission(ADDR_FT6336);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0 || Wire.requestFrom(ADDR_FT6336, n) != n) return false;
  for (uint8_t i = 0; i < n; i++) buf[i] = Wire.read();
  return true;
}

static bool touchInit() {
  pinMode(PIN_TP_RST, OUTPUT);
  digitalWrite(PIN_TP_RST, LOW);
  delay(10);
  digitalWrite(PIN_TP_RST, HIGH);
  delay(300);                                      // the controller's start-up
  uint8_t vendor = 0, fw = 0;
  const bool ok = ftRead(0xA8, &vendor, 1) && ftRead(0xA6, &fw, 1);
  PANEL_LOG("freenove28: ft6336 vendor %02x firmware %02x", vendor, fw);
  return ok;
}

// From register 0x02: the number of points, then point 0 (event and x high,
// x low, id and y high, y low). Its coordinates are the panel's own, portrait.
static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!haveTouch) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }
  lastPoll = millis();
  uint8_t b[5];
  if (!ftRead(0x02, b, sizeof b)) {
    lastDown = false;
    return false;
  }
  const int points = b[0] & 0x0F;
  bool down = false;
  if (points >= 1 && points <= 2) {
    int px = (b[1] & 0x0F) << 8 | b[2], py = (b[3] & 0x0F) << 8 | b[4];
    if (kTurned) { px = W - 1 - px; py = H - 1 - py; }
    lastX = px < 0 ? 0 : px >= W ? W - 1 : px;
    lastY = py < 0 ? 0 : py >= H ? H - 1 : py;
    down = true;
  }
  lastDown = down;
  x = lastX;
  y = lastY;
  return down;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  haveTouch = touchInit();
  const bool ok = lcd->begin(kSpiHz);
  PANEL_LOG("freenove28: touch %s, display %s, %s", haveTouch ? "ok" : "NONE", ok ? "ok" : "FAILED",
            kTurned ? "turned 180" : "upright");
  return ok;
}

static void push(const uint16_t* frame) { lcd->draw16bitRGBBitmap(0, 0, (uint16_t*)frame, W, H); }

static void brightness(uint8_t level) { analogWrite(PIN_LCD_BL, level); }

void setup() {
  pinMode(PIN_LCD_BL, OUTPUT);
  static const SquarePanel layout(specFreenove28());
  GridBoard b{};
  b.type = "freenove-s3-28";
  b.layout = &layout;
  b.begin = boardBegin;
  b.push = push;
  b.touch = touchRead;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 50;
  b.boardId = nullptr;          // the MAC
  gridAppSetup(b, FW_VERSION);
}

void loop() { gridAppLoop(); }
