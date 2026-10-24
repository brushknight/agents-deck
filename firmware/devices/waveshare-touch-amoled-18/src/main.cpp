// agents deck on the Waveshare ESP32-S3-Touch-AMOLED-1.8: the deck as a
// 2 x 2 grid of big tiles on the 368 x 448 AMOLED, with touch.
//
// This file is the board: the CO5300 panel on QSPI, the CST820 (or, on the
// first revision, FT3168) touch, and the TCA9554 IO expander that resets the
// panel and switches its supply. Everything else (Deck Link, the fleet, the
// layout, the touch UI) is panel-core's grid_app.h.
//
// Pins and init order are from Waveshare's BSP for this board
// (esp32_s3_touch_amoled_1_8 2.0.3 on the ESP component registry) and their
// Arduino demos (github.com/waveshareteam/ESP32-S3-Touch-AMOLED-1.8). The
// AXP2101 PMU is left at its power-on defaults, as Waveshare's code does.
//
// Two builds: arduino-esp32 2.0.17 (../platformio.ini, USB only) and 3.x
// (idf5/platformio.ini, Wi-Fi with TLS 1.3); only Arduino_GFX's CO5300
// constructor differs between the two GFX versions they use.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>

#include "grid_app.h"
#include "layout_square.h"

#define FW_VERSION "0.1.0"

// ---------------------------------------------------------------- pins
constexpr int PIN_SDA = 15, PIN_SCL = 14, PIN_BOOT = 0;
constexpr int PIN_QSPI_CS = 12, PIN_QSPI_SCK = 11, PIN_QSPI_D0 = 4, PIN_QSPI_D1 = 5, PIN_QSPI_D2 = 6, PIN_QSPI_D3 = 7;
constexpr uint8_t ADDR_TCA9554 = 0x20;
constexpr uint8_t ADDR_CST820 = 0x15, ADDR_FT3168 = 0x38;
// TCA9554: EXIO0 LCD reset, EXIO1 panel supply (DSI_PWR_EN), EXIO2 touch reset.
constexpr uint8_t EXIO_LCD_RST = 1 << 0, EXIO_LCD_PWR = 1 << 1, EXIO_TP_RST = 1 << 2;

static Arduino_DataBus* bus = new Arduino_ESP32QSPI(PIN_QSPI_CS, PIN_QSPI_SCK, PIN_QSPI_D0, PIN_QSPI_D1, PIN_QSPI_D2, PIN_QSPI_D3);
static Arduino_CO5300* lcd = nullptr;
static uint8_t touchAddr = 0;
static bool haveTca = false;

// ---------------------------------------------------------------- I2C
static bool i2cPresent(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

static bool tca(uint8_t reg, uint8_t value) {
  Wire.beginTransmission(ADDR_TCA9554);
  Wire.write(reg);
  Wire.write(value);
  return Wire.endTransmission() == 0;
}

// Panel supply on, panel and touch through a reset.
static void expanderInit() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  haveTca = i2cPresent(ADDR_TCA9554);
  if (!haveTca) return;
  tca(0x03, (uint8_t)~(EXIO_LCD_RST | EXIO_LCD_PWR | EXIO_TP_RST));   // those three as outputs
  tca(0x01, 0x00);
  delay(20);
  tca(0x01, EXIO_LCD_RST | EXIO_LCD_PWR | EXIO_TP_RST);
  delay(150);
}

// ---------------------------------------------------------------- touch
// CST820 (driven as a CST816) and FT3168 share the report layout from
// register 0x02: count, then X and Y as 12-bit big-endian.
static bool touchRead(int& x, int& y) {
  static bool lastDown = false;
  static int lastX = 0, lastY = 0;
  static uint32_t lastPoll = 0;
  if (!touchAddr) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }
  lastPoll = millis();
  Wire.beginTransmission(touchAddr);
  Wire.write(0x02);
  if (Wire.endTransmission(false) != 0 || Wire.requestFrom(touchAddr, (uint8_t)5) != 5) {
    lastDown = false;                 // the CST820 sleeps (and NACKs) when nobody touches it
    return false;
  }
  uint8_t d[5];
  for (int i = 0; i < 5; i++) d[i] = Wire.read();
  const int n = d[0] & 0x0F;
  if (n > 0) {
    lastX = (d[1] & 0x0F) << 8 | d[2];
    lastY = (d[3] & 0x0F) << 8 | d[4];
  }
  lastDown = n > 0;
  x = lastX;
  y = lastY;
  return lastDown;
}

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  expanderInit();
  touchAddr = i2cPresent(ADDR_CST820) ? ADDR_CST820 : i2cPresent(ADDR_FT3168) ? ADDR_FT3168 : 0;
  // The CO5300 boards (CST820 touch) start 16 columns in; the first SH8601
  // revision (FT3168) at 0. GFX 1.5.x takes an `ips` flag after the rotation; 1.6 doesn't.
  const uint8_t colOffset = touchAddr == ADDR_FT3168 ? 0 : 16;
#if defined(ESP_ARDUINO_VERSION_MAJOR) && ESP_ARDUINO_VERSION_MAJOR >= 3
  lcd = new Arduino_CO5300(bus, GFX_NOT_DEFINED, 0, 368, 448, colOffset, 0, 0, 0);
#else
  lcd = new Arduino_CO5300(bus, GFX_NOT_DEFINED, 0, false, 368, 448, colOffset, 0, 0, 0);
#endif
  const bool ok = lcd->begin(40000000);
  PANEL_LOG("amoled18: tca9554 %s, touch 0x%02X, col offset %u, display %s", haveTca ? "ok" : "NONE", touchAddr,
            colOffset, ok ? "ok" : "FAILED");
  return ok;
}

static void push(const uint16_t* frame) {
  if (lcd) lcd->draw16bitRGBBitmap(0, 0, (uint16_t*)frame, 368, 448);
}

static void brightness(uint8_t level) {
  if (lcd) lcd->setBrightness(level);
}

void setup() {
  static const SquarePanel layout(specAmoled18());
  GridBoard b{};
  b.type = "waveshare-touch-amoled-18";
  b.layout = &layout;
  b.begin = boardBegin;
  b.push = push;
  b.touch = touchRead;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 40;          // an AMOLED dims well, and black pixels cost nothing
  gridAppSetup(b, FW_VERSION);
}

void loop() { gridAppLoop(); }
