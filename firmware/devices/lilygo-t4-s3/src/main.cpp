// agents deck on the LilyGO T4-S3: the 2.41" AMOLED (450 x 600, touch) with
// the same UI as the 1.8" AMOLED (layout_square.h, specT4S3()): tiles in one
// scrolling list, the card with its icon buttons, the prompts, swipe right
// to go back.
//
// This file is the board: the RM690B0 panel on QSPI, the CST226SE touch on
// I2C, and the panel's power switch. Everything else (Deck Link over USB or
// Wi-Fi, the fleet, the layout, the touch UI) is panel-core's grid_app.h.
//
// Pins, the panel's init and the touch protocol are LilyGO's
// (github.com/Xinyuan-LilyGO/LilyGo-AMOLED-Series, BOARD_AMOLED_241, and
// SensorLib's TouchClassCST226), as in the earlier firmware (../t4s3-panel).
// The SY6970 charger is left at its power-on defaults, as there.
//
// The panel is mounted turned 180 degrees (as the earlier t4s3-flip build), so the picture
// and touch are turned by default; build with -DT4S3_UPRIGHT for LilyGO's
// own orientation.
#include <Arduino.h>
// Private made public for qspi_fast.h, which sends frames on Arduino_GFX's own SPI device.
#define private public
#include <Arduino_GFX_Library.h>
#undef private
#include <Wire.h>
#include <esp_mac.h>

#include "qspi_fast.h"   // before grid_app.h: its debug report reads the send's numbers
#include "grid_app.h"
#include "layout_square.h"

#define FW_VERSION "2.0.0"   // after the LVGL firmware's 1.0.0

// ---------------------------------------------------------------- pins
constexpr int PIN_SDA = 6, PIN_SCL = 7, PIN_TP_RST = 17, PIN_BOOT = 0;
constexpr int PIN_QSPI_CS = 11, PIN_QSPI_SCK = 15, PIN_QSPI_D0 = 14, PIN_QSPI_D1 = 10, PIN_QSPI_D2 = 16,
              PIN_QSPI_D3 = 12;
constexpr int PIN_LCD_RST = 13, PIN_LCD_POWER = 9;   // the panel's power switch, active high
constexpr uint8_t ADDR_CST226 = 0x5A;
constexpr int W = 450, H = 600;

#ifdef T4S3_UPRIGHT
constexpr bool kTurned = false;
#else
constexpr bool kTurned = true;
#endif

static Arduino_DataBus* bus = new Arduino_ESP32QSPI(PIN_QSPI_CS, PIN_QSPI_SCK, PIN_QSPI_D0, PIN_QSPI_D1, PIN_QSPI_D2, PIN_QSPI_D3);
// The panel's RAM is 482 columns wide; the glass shows 450 of them, 16 in
// from either side, so turned 180 degrees the offset is the same.
static Arduino_RM690B0* lcd = nullptr;
static bool haveTouch = false;
static bool fastSend = false;
#ifndef LCD_QSPI_HZ
#define LCD_QSPI_HZ 40000000
#endif
static char boardId[16];

// ---------------------------------------------------------------- touch
static bool cstWrite(uint8_t a, uint8_t b) {
  Wire.beginTransmission(ADDR_CST226);
  Wire.write(a);
  Wire.write(b);
  return Wire.endTransmission() == 0;
}

// Reset, then ask for the firmware check code ("CACA" in the top half) to
// know it's there.
static bool touchInit() {
  pinMode(PIN_TP_RST, OUTPUT);
  digitalWrite(PIN_TP_RST, LOW);
  delay(100);
  digitalWrite(PIN_TP_RST, HIGH);
  delay(100);
  bool ok = cstWrite(0xD1, 0x01);                  // command mode
  delay(10);
  uint8_t code[4] = {0};
  Wire.beginTransmission(ADDR_CST226);
  Wire.write(0xD1);
  Wire.write(0xFC);
  ok = ok && Wire.endTransmission(false) == 0 && Wire.requestFrom(ADDR_CST226, (uint8_t)4) == 4;
  for (int i = 0; ok && i < 4; i++) code[i] = Wire.read();
  cstWrite(0xD1, 0x09);                            // back to reporting touches
  PANEL_LOG("t4s3: cst226 check code %02x%02x%02x%02x", code[3], code[2], code[1], code[0]);
  return ok && code[3] == 0xCA && code[2] == 0xCA;
}

// A report is 28 bytes from register 0x00: byte 5 the number of points, byte
// 6 0xAB in a valid report, point 0 in bytes 1-3 (12-bit x and y).
static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!haveTouch) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }
  lastPoll = millis();
  uint8_t b[28];
  Wire.beginTransmission(ADDR_CST226);
  Wire.write(0x00);
  if (Wire.endTransmission(false) != 0 || Wire.requestFrom(ADDR_CST226, (uint8_t)sizeof b) != sizeof b) {
    lastDown = false;
    return false;
  }
  for (uint8_t& v : b) v = Wire.read();
  bool down = false;
  if (b[6] == 0xAB && b[0] != 0xAB && b[5] != 0x80) {
    const int points = b[5] & 0x7F;
    if (points > 5) cstWrite(0x00, 0xAB);          // clear a bad report
    else if (points > 0) {
      int px = (b[1] << 4) | (b[3] >> 4), py = (b[2] << 4) | (b[3] & 0x0F);
      if (kTurned) { px = W - 1 - px; py = H - 1 - py; }   // the touch layer doesn't turn with the picture
      lastX = px < 0 ? 0 : px >= W ? W - 1 : px;
      lastY = py < 0 ? 0 : py >= H ? H - 1 : py;
      down = true;
    }
  }
  lastDown = down;
  x = lastX;
  y = lastY;
  return down;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  pinMode(PIN_LCD_POWER, OUTPUT);
  digitalWrite(PIN_LCD_POWER, HIGH);
  delay(20);
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  haveTouch = touchInit();
  lcd = new Arduino_RM690B0(bus, PIN_LCD_RST, kTurned ? 2 : 0, W, H, 16, 0, 16, 0);
  const bool ok = lcd->begin(LCD_QSPI_HZ);
  fastSend = ok && qspi_fast::begin();
  qspi_fast::preswapped = fastSend;        // the canvas draws in the panel's byte order (frameSwapped)
  PANEL_LOG("t4s3: QSPI at %d kHz, fast send %s", qspi_fast::actualKhz((Arduino_ESP32QSPI*)bus), fastSend ? "on" : "off");
  PANEL_LOG("t4s3: touch %s, display %s, %s", haveTouch ? "ok" : "NONE", ok ? "ok" : "FAILED",
            kTurned ? "turned 180" : "upright");
  return ok;
}

// A whole frame: two buffers in turn (qspi_fast.h), else Arduino_GFX's own send.
static void push(const uint16_t* frame) {
  if (!lcd) return;
  if (fastSend && qspi_fast::broken) {      // the fast send failed: the old one from now on, in plain byte order
    fastSend = false;
    qspi_fast::preswapped = false;
  }
  if (!fastSend) {
    lcd->draw16bitRGBBitmap(0, 0, (uint16_t*)frame, W, H);
    return;
  }
  lcd->startWrite();
  lcd->writeAddrWindow(0, 0, W, H);
  qspi_fast::pixels((Arduino_ESP32QSPI*)bus, frame, (uint32_t)W * H);
  lcd->endWrite();
}

// A frame as rows (the panel sliding over home: control_panel.h).
static void pushRows(const uint16_t* const* rows) {
  if (!lcd) return;
  if (fastSend && qspi_fast::broken) {
    fastSend = false;
    qspi_fast::preswapped = false;
  }
  if (!fastSend) {                         // row by row, the slow way
    for (int y = 0; y < H; y++) lcd->draw16bitRGBBitmap(0, y, (uint16_t*)rows[y], W, 1);
    return;
  }
  lcd->startWrite();
  lcd->writeAddrWindow(0, 0, W, H);
  qspi_fast::rows((Arduino_ESP32QSPI*)bus, rows, W, H);
  lcd->endWrite();
}

// The panel's own brightness (0x51), on the bus: Arduino_GFX 1.5.0's
// RM690B0 has no setBrightness().
static void brightness(uint8_t level) {
  if (!lcd) return;
  bus->beginWrite();
  bus->writeC8D8(0x51, level);
  bus->endWrite();
}

void setup() {
  // The board id the earlier firmware (../t4s3-panel) reported: the MAC as
  // 12 lowercase hex digits, so the deck sees the same device (its approval,
  // scope and name carry over).
  uint8_t mac[6];
  esp_read_mac(mac, ESP_MAC_WIFI_STA);
  snprintf(boardId, sizeof boardId, "%02x%02x%02x%02x%02x%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);

  static const SquarePanel layout(specT4S3());
  GridBoard b{};
  b.type = "t4s3-panel";      // the earlier firmware's type: the same device on the deck
  b.layout = &layout;
  b.begin = boardBegin;
  b.push = push;
  b.pushRows = pushRows;
  b.frameSwapped = [] { return fastSend; };
  b.touch = touchRead;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 40;          // an AMOLED dims well, and black pixels cost nothing
  b.boardId = boardId;
  gridAppSetup(b, FW_VERSION);
}

void loop() { gridAppLoop(); }
