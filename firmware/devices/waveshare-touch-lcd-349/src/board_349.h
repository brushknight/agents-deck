#pragma once
// Waveshare ESP32-S3-Touch-LCD-3.49 drivers; pins and init order from Waveshare's demos.
// The panel runs on its own defaults: Waveshare sends only sleep-out and display-on.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp_heap_caps.h>

#ifndef STRIP_ROTATION
#define STRIP_ROTATION 90     // 90 | 270: the landscape frame turned clockwise into the panel
#endif

namespace board349 {

constexpr int kW = 640, kH = 172;           // the landscape frame
constexpr int kPanelCols = 172, kPanelRows = 640;

constexpr int PIN_QSPI_CS = 9, PIN_QSPI_SCK = 10, PIN_QSPI_D0 = 11, PIN_QSPI_D1 = 12, PIN_QSPI_D2 = 13,
              PIN_QSPI_D3 = 14;
constexpr int PIN_LCD_RST = 21, PIN_LCD_BL = 8;
constexpr int PIN_TP_SDA = 17, PIN_TP_SCL = 18;
constexpr int PIN_SDA = 47, PIN_SCL = 48;
constexpr int PIN_BOOT = 0, PIN_PWR = 16;
constexpr uint8_t ADDR_TOUCH = 0x3B, ADDR_TCA9554 = 0x20;
constexpr uint8_t EXIO_BL_EN = 1 << 1;      // the backlight driver's enable (AP3032 CTRL)
constexpr uint8_t EXIO_BAT_HOLD = 1 << 6;   // high keeps the board on when it runs on the battery

constexpr int kStripRows = 32;              // panel rows per transfer (640 / 32 strips)

static Arduino_ESP32QSPI* bus = nullptr;
static uint16_t* strip = nullptr;           // internal RAM: kStripRows panel rows
static bool haveTca = false, haveTouch = false;

static bool i2cPresent(TwoWire& w, uint8_t addr) {
  w.beginTransmission(addr);
  return w.endTransmission() == 0;
}

static uint8_t exioOut = EXIO_BAT_HOLD | EXIO_BL_EN;

static void tcaWrite(uint8_t reg, uint8_t value) {
  Wire.beginTransmission(ADDR_TCA9554);
  Wire.write(reg);
  Wire.write(value);
  Wire.endTransmission();
}

static void exio(uint8_t bit, bool high) {
  exioOut = high ? (exioOut | bit) : (exioOut & ~bit);
  if (haveTca) tcaWrite(0x01, exioOut);
}

// Keep the battery supply latched on (a no-op on USB power) and the backlight driver enabled.
static void expanderBegin() {
  haveTca = i2cPresent(Wire, ADDR_TCA9554);
  if (!haveTca) return;
  tcaWrite(0x01, exioOut);                                  // output port
  tcaWrite(0x03, (uint8_t)~(EXIO_BAT_HOLD | EXIO_BL_EN));   // configuration: 0 is output
}

// Lets the board switch off on the battery. On USB power it stays on.
[[maybe_unused]] static void powerOff() { exio(EXIO_BAT_HOLD, false); }

constexpr uint32_t kBlFreq = 50000;         // Waveshare's; the backlight driver ignores slow PWM
constexpr uint8_t kBlChannel = 7;

static void backlightBegin() {
#if ESP_ARDUINO_VERSION_MAJOR >= 3
  ledcAttach(PIN_LCD_BL, kBlFreq, 8);
#else
  ledcSetup(kBlChannel, kBlFreq, 8);
  ledcAttachPin(PIN_LCD_BL, kBlChannel);
#endif
}

// On or off: BL_EN switches the backlight driver. IO8's PWM (inverted) only
// trims it a little on this board.
static void brightness(uint8_t level) {
  exio(EXIO_BL_EN, level > 0);
  const uint32_t duty = 255 - level;
#if ESP_ARDUINO_VERSION_MAJOR >= 3
  ledcWrite(PIN_LCD_BL, duty);
#else
  ledcWrite(kBlChannel, duty);
#endif
}

static bool panelBegin() {
  bus = new Arduino_ESP32QSPI(PIN_QSPI_CS, PIN_QSPI_SCK, PIN_QSPI_D0, PIN_QSPI_D1, PIN_QSPI_D2, PIN_QSPI_D3);
  if (!bus->begin(40000000)) return false;
  strip = (uint16_t*)heap_caps_malloc(kPanelCols * kStripRows * 2, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
  if (!strip) return false;

  pinMode(PIN_LCD_RST, OUTPUT);
  digitalWrite(PIN_LCD_RST, HIGH);
  delay(30);
  digitalWrite(PIN_LCD_RST, LOW);
  delay(250);
  digitalWrite(PIN_LCD_RST, HIGH);
  delay(30);

  bus->beginWrite();
  bus->writeCommand(0x11);                  // sleep out
  bus->endWrite();
  delay(100);
  bus->beginWrite();
  bus->writeC8D8(0x36, 0x00);               // MADCTL: the panel's own orientation
  bus->writeC8D8(0x3A, 0x55);               // RGB565
  bus->writeC8D16D16(0x2A, 0, kPanelCols - 1);
  bus->writeCommand(0x29);                  // display on
  bus->endWrite();
  delay(100);
  return true;
}

// In QSPI mode the panel ignores the row window, so a frame goes out whole from 0x2C.
// The inner loop walks a frame row so the PSRAM reads run in order.
static void push(const uint16_t* frame) {
  if (!bus || !strip) return;
  bus->beginWrite();
  bus->writeCommand(0x2C);
  for (int r0 = 0; r0 < kPanelRows; r0 += kStripRows) {
    for (int c = 0; c < kPanelCols; c++) {
#if STRIP_ROTATION == 270
      const uint16_t* src = frame + c * kW + (kPanelRows - 1 - r0);
      for (int k = 0; k < kStripRows; k++) strip[k * kPanelCols + c] = *src--;
#else
      const uint16_t* src = frame + (kH - 1 - c) * kW + r0;
      for (int k = 0; k < kStripRows; k++) strip[k * kPanelCols + c] = *src++;
#endif
    }
    bus->writePixels(strip, kPanelCols * kStripRows);
  }
  bus->endWrite();
}

// False also for the filler bytes the chip returns between reports: it never
// reports a lift, it only stops reporting.
static bool touchRaw(int& rx, int& ry, int& n) {
  static const uint8_t cmd[11] = {0xB5, 0xAB, 0xA5, 0x5A, 0x00, 0x00, 0x00, 0x0E, 0x00, 0x00, 0x00};
  Wire1.beginTransmission(ADDR_TOUCH);
  Wire1.write(cmd, sizeof cmd);
  if (Wire1.endTransmission(false) != 0) return false;
  uint8_t d[8] = {0};
  if (Wire1.requestFrom(ADDR_TOUCH, (uint8_t)sizeof d) != sizeof d) return false;
  for (auto& b : d) b = Wire1.read();
  n = d[1];
  rx = (d[2] & 0x0F) << 8 | d[3];
  ry = (d[4] & 0x0F) << 8 | d[5];
  return d[0] == 0 && n > 0 && n < 5 && rx <= kPanelRows && ry <= kPanelCols;
}

// Reads since the finger went down, and how many were reports (bring-up notes).
static uint32_t touchReads = 0, touchReports = 0;

// Up only after a few filler reads in a row and some time without a report:
// one filler read between two slow polls must not end a drag or a hold.
static bool touch(int& x, int& y) {
  constexpr int kLiftReads = 3;
  constexpr uint32_t kLiftMs = 60;
  static bool lastDown = false;
  static int lastX = 0, lastY = 0, bad = 0;
  static uint32_t lastPoll = 0, lastReport = 0;
  if (!haveTouch) return false;
  if (millis() - lastPoll < 15) { x = lastX; y = lastY; return lastDown; }
  lastPoll = millis();
  int rx, ry, n;
  const bool report = touchRaw(rx, ry, n);
  if (lastDown) touchReads++;
  if (report) {
    if (!lastDown) touchReads = 1, touchReports = 0;
    touchReports++;
    bad = 0;
    lastReport = lastPoll;
    const int pc = constrain(ry, 0, kPanelCols - 1);              // panel column
    const int pr = constrain(kPanelRows - rx, 0, kPanelRows - 1); // panel row
#if STRIP_ROTATION == 270
    lastX = kPanelRows - 1 - pr;
    lastY = pc;
#else
    lastX = pr;
    lastY = kH - 1 - pc;
#endif
    lastDown = true;
  } else if (lastDown && ++bad >= kLiftReads && lastPoll - lastReport >= kLiftMs) {
    lastDown = false;
  }
  x = lastX;
  y = lastY;
  return lastDown;
}

// The battery latch, as early as possible (begin() does it too).
static void earlyPower() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  expanderBegin();
}

static bool begin() {
  earlyPower();
  Wire1.begin(PIN_TP_SDA, PIN_TP_SCL, 300000);
  haveTouch = i2cPresent(Wire1, ADDR_TOUCH);
  backlightBegin();
  brightness(0);
  return panelBegin();
}

}  // namespace board349
