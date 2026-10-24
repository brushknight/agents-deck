#pragma once
// What Waveshare's two round ESP32-S3 boards share: the ESP32-S3-Touch-LCD-2.1
// and -2.8C have the same wiring but for the touch chip and the panel's init.
//
//   - TCA9554 IO expander (I2C 0x20): EXIO1 LCD reset, EXIO2 touch reset,
//     EXIO3 the ST7701's SPI chip select, EXIO4 SD chip select, EXIO8 buzzer
//     (it powers up on: the first thing we do is turn it off).
//   - ST7701 480 x 480 on a 16-bit RGB bus, initialised over 9-bit 3-wire
//     SPI (SCK 2, SDA 1, CS on EXIO3).
//   - Backlight PWM on GPIO 6. I2C on SDA 15 / SCL 7. BOOT on GPIO 0.
//
// Pins and timings: Waveshare's demos for both boards (the wiki's demo zips).
//
// Build settings (platformio.ini build_flags):
//   ROUND_ROTATION  0 | 90 | 180 | 270   the image turned clockwise, and the touch with it
//   ROUND_PCLK_HZ   the RGB pixel clock (the board's main.cpp sets its default)
//   ROUND_BOUNCE_PX RGB bounce buffer in pixels (arduino-esp32 3.x only; 0 = none)
//
// The RGB panel: on arduino-esp32 2.x (ESP-IDF 4.4) through Arduino_GFX's
// Arduino_ESP32RGBPanel, like the square boards; on 3.x (ESP-IDF 5) through
// esp_lcd directly, which is what can add a bounce buffer (Arduino_GFX
// doesn't expose one). The ST7701's init goes through Arduino_GFX's
// software SPI either way. Arduino only.
#if defined(ARDUINO)
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "round_rotate.h"

#if defined(ESP_ARDUINO_VERSION_MAJOR) && ESP_ARDUINO_VERSION_MAJOR >= 3
#define WS_ROUND_IDF5 1
#include <esp_lcd_panel_ops.h>
#include <esp_lcd_panel_rgb.h>
#else
#define WS_ROUND_IDF5 0
#endif

#ifndef ROUND_BOUNCE_PX
#define ROUND_BOUNCE_PX 0
#endif

namespace ws_round {

constexpr int kSize = 480;
constexpr int PIN_SDA = 15, PIN_SCL = 7, PIN_BOOT = 0, PIN_BACKLIGHT = 6, PIN_TOUCH_INT = 16;
constexpr int PIN_SPI_SCK = 2, PIN_SPI_SDA = 1;
constexpr int BL_CHANNEL = 1, BL_FREQ = 20000, BL_BITS = 10;
constexpr uint8_t ADDR_TCA9554 = 0x20;
constexpr uint8_t TCA_OUT = 0x01, TCA_CONFIG = 0x03;
// The expander's outputs (EXIO1..8 are bits 0..7).
constexpr uint8_t EXIO_LCD_RST = 1 << 0, EXIO_TP_RST = 1 << 1, EXIO_LCD_CS = 1 << 2, EXIO_SD_CS = 1 << 3,
                  EXIO_BUZZER = 1 << 7;

// One board's panel: its ST7701 init (Arduino_GFX batch operations) and RGB timing.
struct Panel {
  const uint8_t* init;
  size_t initLen;
  uint16_t hsyncPulse, hsyncBack, hsyncFront;
  uint16_t vsyncPulse, vsyncBack, vsyncFront;
  uint32_t pclkHz;
};

static uint16_t* fb = nullptr;                   // the panel's framebuffer (PSRAM)
static bool haveTca = false;
static uint8_t exio = 0;                  // the expander's output register, as last written

// ---------------------------------------------------------------- I2C

static inline bool i2cWrite(uint8_t addr, const uint8_t* data, size_t n) {
  Wire.beginTransmission(addr);
  Wire.write(data, n);
  return Wire.endTransmission() == 0;
}

static inline bool i2cPresent(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

static inline bool tca(uint8_t reg, uint8_t value) {
  const uint8_t d[2] = {reg, value};
  return i2cWrite(ADDR_TCA9554, d, 2);
}

static inline void exioSet(uint8_t bits, bool high) {
  exio = high ? (uint8_t)(exio | bits) : (uint8_t)(exio & ~bits);
  tca(TCA_OUT, exio);
}

// All expander pins outputs: chip selects idle high, resets low, the buzzer
// off; then the LCD out of reset.
static inline void expanderBegin() {
  Wire.begin(PIN_SDA, PIN_SCL, 400000);
  delay(120);
  for (int tries = 0; tries < 5 && !haveTca; tries++) {
    haveTca = i2cPresent(ADDR_TCA9554);
    if (!haveTca) delay(50);
  }
  exio = EXIO_LCD_CS | EXIO_SD_CS;
  tca(TCA_OUT, exio);                     // the output register first, so the buzzer never sounds
  tca(TCA_CONFIG, 0x00);
  delay(10);
  exioSet(EXIO_LCD_RST, true);
  delay(50);
}

static inline void buzzer(bool on) { exioSet(EXIO_BUZZER, on); }

// ---------------------------------------------------------------- display

#if WS_ROUND_IDF5
static esp_lcd_panel_handle_t rgb = nullptr;

static inline uint16_t* rgbBegin(const Panel& p) {
  esp_lcd_rgb_panel_config_t c = {};
  c.clk_src = LCD_CLK_SRC_DEFAULT;
  c.timings.pclk_hz = p.pclkHz;
  c.timings.h_res = kSize;
  c.timings.v_res = kSize;
  c.timings.hsync_pulse_width = p.hsyncPulse;
  c.timings.hsync_back_porch = p.hsyncBack;
  c.timings.hsync_front_porch = p.hsyncFront;
  c.timings.vsync_pulse_width = p.vsyncPulse;
  c.timings.vsync_back_porch = p.vsyncBack;
  c.timings.vsync_front_porch = p.vsyncFront;
  c.timings.flags.pclk_active_neg = 0;    // latch on the rising edge, as the demos
  c.data_width = 16;
  c.bits_per_pixel = 16;
  c.num_fbs = 1;
  c.bounce_buffer_size_px = ROUND_BOUNCE_PX;
  c.dma_burst_size = 64;
  c.hsync_gpio_num = 38;
  c.vsync_gpio_num = 39;
  c.de_gpio_num = 40;
  c.pclk_gpio_num = 41;
  c.disp_gpio_num = -1;
  static const int kData[16] = {5, 45, 48, 47, 21, 14, 13, 12, 11, 10, 9, 46, 3, 8, 18, 17};   // B0-4 G0-5 R0-4
  for (int i = 0; i < 16; i++) c.data_gpio_nums[i] = kData[i];
  c.flags.fb_in_psram = 1;
  if (esp_lcd_new_rgb_panel(&c, &rgb) != ESP_OK) return nullptr;
  if (esp_lcd_panel_reset(rgb) != ESP_OK || esp_lcd_panel_init(rgb) != ESP_OK) return nullptr;
  void* buf = nullptr;
  if (esp_lcd_rgb_panel_get_frame_buffer(rgb, 1, &buf) != ESP_OK) return nullptr;
  return (uint16_t*)buf;
}
#else
static inline uint16_t* rgbBegin(const Panel& p) {
  static Arduino_ESP32RGBPanel* panel = new Arduino_ESP32RGBPanel(
      40 /* DE */, 39 /* VSYNC */, 38 /* HSYNC */, 41 /* PCLK */,
      46, 3, 8, 18, 17,                   // R0..R4
      14, 13, 12, 11, 10, 9,              // G0..G5
      5, 45, 48, 47, 21,                  // B0..B4
      1, p.hsyncFront, p.hsyncPulse, p.hsyncBack,
      1, p.vsyncFront, p.vsyncPulse, p.vsyncBack,
      0 /* pclk on the rising edge */, (int32_t)p.pclkHz);
  panel->begin((int32_t)p.pclkHz);
  return panel->getFrameBuffer(kSize, kSize);
}
#endif

// The ST7701's init over software SPI (its chip select held low on the
// expander meanwhile), then the RGB bus. False when there is no framebuffer.
static inline bool displayBegin(const Panel& p) {
  static Arduino_DataBus* spi = new Arduino_SWSPI(GFX_NOT_DEFINED /* DC */, GFX_NOT_DEFINED /* CS */, PIN_SPI_SCK,
                                                  PIN_SPI_SDA, GFX_NOT_DEFINED /* MISO */);
  exioSet(EXIO_LCD_CS, false);
  delay(10);
  spi->begin();
  spi->sendCommand(0x01);                 // software reset, as Arduino_RGB_Display::begin()
  delay(120);
  spi->batchOperation(p.init, p.initLen);
  exioSet(EXIO_LCD_CS, true);
  fb = rgbBegin(p);
  if (fb) memset(fb, 0, (size_t)kSize * kSize * 2);
  return fb != nullptr;
}

// One frame onto the panel, turned by ROUND_ROTATION, then out of the cache.
static inline void push(const uint16_t* src) {
  if (!fb) return;
  rotateFrame(ROUND_ROTATION, kSize, src, fb);
  Cache_WriteBack_Addr((uint32_t)fb, kSize * kSize * 2);
}

// A touch in the panel's axes, in frame pixels.
static inline void touchToFrame(int& x, int& y) {
  int fx, fy;
  rotateToFrame(ROUND_ROTATION, kSize, x, y, fx, fy);
  x = fx < 0 ? 0 : fx >= kSize ? kSize - 1 : fx;
  y = fy < 0 ? 0 : fy >= kSize ? kSize - 1 : fy;
}

// ---------------------------------------------------------------- backlight

static inline void backlightBegin() {
#if WS_ROUND_IDF5
  ledcAttach(PIN_BACKLIGHT, BL_FREQ, BL_BITS);
#else
  ledcSetup(BL_CHANNEL, BL_FREQ, BL_BITS);
  ledcAttachPin(PIN_BACKLIGHT, BL_CHANNEL);
#endif
}

static inline void brightness(uint8_t level) {
  const uint32_t duty = (uint32_t)level * ((1u << BL_BITS) - 1) / 255;
#if WS_ROUND_IDF5
  ledcWrite(PIN_BACKLIGHT, duty);
#else
  ledcWrite(BL_CHANNEL, duty);
#endif
}

}  // namespace ws_round

#endif  // ARDUINO
