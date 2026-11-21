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
//
// Two builds: arduino-esp32 2.0.17 (../platformio.ini, USB only) and 3.x
// (idf5/platformio.ini: Wi-Fi with TLS 1.3, and an RGB bounce buffer so the
// panel's DMA reads internal RAM rather than the PSRAM that Wi-Fi, TLS and
// the CPU share).
#include <Arduino.h>
// The debug build on arduino-esp32 3.x reports the display and the restarts
// (LCD4_DIAG, below): it reads the panel handle Arduino_GFX keeps private.
#if defined(PANEL_DEBUG) && defined(ESP_ARDUINO_VERSION_MAJOR) && ESP_ARDUINO_VERSION_MAJOR >= 3
#define LCD4_DIAG 1
#define private public
#include <Arduino_GFX_Library.h>
#undef private
#include <esp_system.h>
#else
#include <Arduino_GFX_Library.h>
#endif
#include <Wire.h>
#include <esp32s3/rom/cache.h>

#include "grid_app.h"
#include "layout_big.h"

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
// parallel bus. Timing as in Waveshare's Arduino demo; the pixel clock is
// Arduino_GFX's default with octal PSRAM, 12 MHz. On arduino-esp32 3.x the
// panel can scan out of two bounce buffers in internal RAM (LCD4_BOUNCE_PX
// pixels each, a whole number of lines that divides the frame; 0 scans
// straight from PSRAM as 2.0.17 does). The bounce buffer needs the display
// settings in idf5/platformio.ini's custom_sdkconfig (see there).
#if defined(ESP_ARDUINO_VERSION_MAJOR) && ESP_ARDUINO_VERSION_MAJOR >= 3
#ifndef LCD4_BOUNCE_PX
#define LCD4_BOUNCE_PX 0
#endif
static_assert(LCD4_BOUNCE_PX % 480 == 0 && (LCD4_BOUNCE_PX == 0 || (480 * 480) % LCD4_BOUNCE_PX == 0),
              "LCD4_BOUNCE_PX: whole lines that divide the frame");
#define LCD4_RGB_EXTRA , 0 /* pclk on the rising edge */, GFX_NOT_DEFINED, false, 0, 0, LCD4_BOUNCE_PX
// With a bounce buffer the firmware fills it itself (LCD4_SCAN, below): the
// panel scans the frames the layout draws, with no frame buffer of its own.
#if LCD4_BOUNCE_PX > 0
#define LCD4_SCAN 1
#include <esp_lcd_panel_ops.h>
#include <esp_lcd_panel_rgb.h>
#endif
#else
#undef LCD4_BOUNCE_PX
#define LCD4_BOUNCE_PX 0
#define LCD4_RGB_EXTRA
#endif
static Arduino_DataBus* spiBus = new Arduino_SWSPI(GFX_NOT_DEFINED, 42 /* CS */, 2 /* SCK */, 1 /* MOSI */, GFX_NOT_DEFINED);
static Arduino_ESP32RGBPanel* rgbPanel = new Arduino_ESP32RGBPanel(
    40 /* DE */, 39 /* VSYNC */, 38 /* HSYNC */, 41 /* PCLK */,
    46, 3, 8, 18, 17,            // R0..R4
    14, 13, 12, 11, 10, 9,       // G0..G5
    5, 45, 48, 47, 21,           // B0..B4
    1, 10, 8, 50,                // hsync polarity, front porch, pulse width, back porch
    1, 10, 8, 20                 // vsync
    LCD4_RGB_EXTRA);
static Arduino_RGB_Display* lcd = new Arduino_RGB_Display(
    480, 480, rgbPanel, 0, false /* we flush */, spiBus, GFX_NOT_DEFINED,
    st7701_type1_init_operations, sizeof(st7701_type1_init_operations));
static uint16_t* panelFb = nullptr;
static bool haveCh32 = false, haveTca = false;
static uint8_t gt911 = 0;

#ifdef LCD4_SCAN
#ifdef LCD4_DIAG
bool diagOnVsync(esp_lcd_panel_handle_t, const esp_lcd_rgb_panel_event_data_t*, void*);   // (diagnostics, below)
#endif
// The frames, three of them in PSRAM, and the panel scans one while the next
// is drawn into another: nothing is copied into a frame buffer, so a frame
// costs its drawing only. The RGB peripheral reads two bounce buffers of ten
// lines in internal RAM; the LCD interrupt refills each from the frame on
// screen (fill(), below), turned 180 degrees on the way (the panel is mounted
// upside down). A new frame goes on screen at the start of a panel frame, so
// a picture is never shown half drawn.
//
// The frame on screen is a table of row pointers: the rows of one of the three
// frames, or rows that a layout keeps (pushRows: the board's panel sliding
// over the screen, cut from two pictures with nothing drawn or copied).
//
// Frames are drawn in strips of internal RAM (grid_app.h) and copied in by
// rows: a row whose pixels are the same as that frame's last time (a hash of
// each row, per frame) is not copied, so a frame where only a few critters
// moved writes only their rows to PSRAM.
namespace scan {
constexpr int kSlots = 3;
static uint16_t* frames[kSlots];                       // PSRAM
static const uint16_t* rows[kSlots][kH];               // what each slot shows, row by row (internal RAM)
static uint32_t hashes[kSlots][kH];                    // each frame's rows as last copied in
static bool hashed[kSlots];                            // false: the frame was drawn whole, its hashes are stale
static volatile int onScreen = -1, next = -1;          // the slots the interrupt shows and shows next
static int drawing = -1;                               // the slot being drawn into
static portMUX_TYPE lock = portMUX_INITIALIZER_UNLOCKED;
static esp_lcd_panel_handle_t panel = nullptr;

// A slot neither on screen nor waiting to be (there always is one of three).
static int freeSlot() {
  portENTER_CRITICAL(&lock);
  const int a = next, b = onScreen;
  portEXIT_CRITICAL(&lock);
  for (int k = 0; k < kSlots; k++)
    if (k != a && k != b) return k;
  return 0;
}

static void show(int k) {
  portENTER_CRITICAL(&lock);
  next = k;
  portEXIT_CRITICAL(&lock);
  drawing = -1;
}

static void slotRows(int k) {
  for (int y = 0; y < kH; y++) rows[k][y] = frames[k] + (size_t)y * kW;
}

// The interrupt: `len` bytes of the panel from pixel `pos` (whole lines), from
// the frame on screen, the last row first and each row backwards.
static IRAM_ATTR bool fill(esp_lcd_panel_handle_t, void* buf, int pos, int len, void*) {
  if (pos == 0) {                                      // a new panel frame: the next picture, if there is one
    portENTER_CRITICAL_ISR(&lock);
    if (next >= 0) {
      onScreen = next;
      next = -1;
    }
    portEXIT_CRITICAL_ISR(&lock);
  }
  uint32_t* out = (uint32_t*)buf;
  const int k = onScreen, y0 = pos / kW, n = len / 2 / kW;
  if (k < 0) {
    for (int i = 0; i < len / 4; i++) out[i] = 0;
    return false;
  }
  for (int y = y0; y < y0 + n; y++) {
    const uint32_t* in = (const uint32_t*)rows[k][kH - 1 - y] + kW / 2;
    for (int i = 0; i < kW / 2; i += 4, out += 4) {
      const uint32_t a = in[-1], b = in[-2], c = in[-3], d = in[-4];
      in -= 4;
      out[0] = a >> 16 | a << 16;
      out[1] = b >> 16 | b << 16;
      out[2] = c >> 16 | c << 16;
      out[3] = d >> 16 | d << 16;
    }
  }
  return false;
}

static uint32_t rowHash(const uint16_t* px) {
  const uint32_t* w = (const uint32_t*)px;
  uint32_t h = 2166136261u;
  for (int i = 0; i < kW / 2; i++) h = (h ^ w[i]) * 16777619u;
  return h;
}

// grid_app.h: a strip of the next frame, from internal RAM.
static void pushStrip(const uint16_t* px, int y0, int n, bool last) {
  if (y0 == 0 || drawing < 0) {
    drawing = freeSlot();
    slotRows(drawing);                                 // (a slide may have pointed its rows elsewhere)
    if (!hashed[drawing]) {
      for (int y = 0; y < kH; y++) hashes[drawing][y] = 0;
      hashed[drawing] = true;
    }
  }
  const int k = drawing;
  for (int y = y0; y < y0 + n; y++, px += kW) {
    const uint32_t h = rowHash(px) | 1;                // never 0: 0 is "not copied yet"
    if (hashes[k][y] == h) continue;
    hashes[k][y] = h;
    memcpy(frames[k] + (size_t)y * kW, px, kW * 2);
  }
  if (last) show(k);
}

// A whole frame: drawn into a free slot, which then needs every row next time it is drawn in strips.
static uint16_t* frameBegin() {
  if (!panel) return nullptr;
  drawing = freeSlot();
  slotRows(drawing);
  hashed[drawing] = false;
  return frames[drawing];
}

static void push(const uint16_t* frame) {
  if (drawing >= 0 && frame == frames[drawing]) show(drawing);
}

// Rows a layout keeps (the board's panel sliding): shown as they are.
static void pushRows(const uint16_t* const* r) {
  const int k = drawing >= 0 ? drawing : freeSlot();
  for (int y = 0; y < kH; y++) rows[k][y] = r[y];
  hashed[k] = false;
  show(k);
}

// The panel: Arduino_GFX's settings for this board (Arduino_ESP32RGBPanel at
// 12 MHz, timings and pins above), with no frame buffer and fill() as the
// bounce buffers' source.
static bool begin() {
  for (int k = 0; k < kSlots; k++) {
    frames[k] = (uint16_t*)heap_caps_calloc((size_t)kW * kH, 2, MALLOC_CAP_SPIRAM);
    if (!frames[k]) return false;
    slotRows(k);
  }
  esp_lcd_rgb_panel_config_t cfg = {};
  cfg.clk_src = LCD_CLK_SRC_DEFAULT;
  cfg.timings.pclk_hz = 12000000;
  cfg.timings.h_res = kW;
  cfg.timings.v_res = kH;
  cfg.timings.hsync_front_porch = 10;
  cfg.timings.hsync_pulse_width = 8;
  cfg.timings.hsync_back_porch = 50;
  cfg.timings.vsync_front_porch = 10;
  cfg.timings.vsync_pulse_width = 8;
  cfg.timings.vsync_back_porch = 20;
  cfg.timings.flags.hsync_idle_low = 0;              // polarity 1
  cfg.timings.flags.vsync_idle_low = 0;
  cfg.timings.flags.de_idle_high = 0;
  cfg.timings.flags.pclk_active_neg = 0;             // pclk on the rising edge
  cfg.timings.flags.pclk_idle_high = 0;
  cfg.data_width = 16;
  cfg.bits_per_pixel = 16;
  cfg.num_fbs = 0;
  cfg.bounce_buffer_size_px = LCD4_BOUNCE_PX;
  cfg.dma_burst_size = 64;
  cfg.hsync_gpio_num = 38;
  cfg.vsync_gpio_num = 39;
  cfg.de_gpio_num = 40;
  cfg.pclk_gpio_num = 41;
  cfg.disp_gpio_num = -1;
  const int data[16] = {5, 45, 48, 47, 21,             // B0..B4
                        14, 13, 12, 11, 10, 9,         // G0..G5
                        46, 3, 8, 18, 17};             // R0..R4
  for (int i = 0; i < 16; i++) cfg.data_gpio_nums[i] = data[i];
  cfg.flags.disp_active_low = 1;
  cfg.flags.no_fb = 1;
  if (esp_lcd_new_rgb_panel(&cfg, &panel) != ESP_OK) return false;
  esp_lcd_rgb_panel_event_callbacks_t cbs = {};
  cbs.on_bounce_empty = fill;
#ifdef LCD4_DIAG
  cbs.on_vsync = diagOnVsync;
#endif
  if (esp_lcd_rgb_panel_register_event_callbacks(panel, &cbs, nullptr) != ESP_OK) return false;
  if (esp_lcd_panel_reset(panel) != ESP_OK || esp_lcd_panel_init(panel) != ESP_OK) return false;
  return true;
}
}  // namespace scan
#endif

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

// ---------------------------------------------------------------- diagnostics
// Every 5 s on the USB port: how often the board has started since power-on
// and why (esp_reset_reason of the last 8 starts, oldest first: 1 power-on,
// 3 software, 4 panic, 5 and 6 and 7 watchdogs, 9 brownout, 11 USB), and how
// many frames the panel took (about 42 a second while it is being fed).
#ifdef LCD4_DIAG
RTC_NOINIT_ATTR static uint32_t diagMagic;
RTC_NOINIT_ATTR static uint32_t diagBoots;
RTC_NOINIT_ATTR static uint8_t diagWhy[8];
static volatile uint32_t diagVsyncs = 0;
IRAM_ATTR bool diagOnVsync(esp_lcd_panel_handle_t, const esp_lcd_rgb_panel_event_data_t*, void*) {
  diagVsyncs = diagVsyncs + 1;
  return false;
}
static void diagBegin() {
  if (diagMagic != 0x4C434434) {             // power-on: the RTC memory holds nothing
    diagMagic = 0x4C434434;
    diagBoots = 0;
    memset(diagWhy, 0, sizeof diagWhy);
  }
  if (diagBoots >= 8) memmove(diagWhy, diagWhy + 1, 7);
  diagWhy[diagBoots < 8 ? diagBoots : 7] = (uint8_t)esp_reset_reason();
  diagBoots++;
#ifndef LCD4_SCAN                                    // (scan::begin registers it with its own callback)
  if (rgbPanel->_panel_handle) {
    esp_lcd_rgb_panel_event_callbacks_t cbs = {};
    cbs.on_vsync = diagOnVsync;
    esp_lcd_rgb_panel_register_event_callbacks(rgbPanel->_panel_handle, &cbs, nullptr);
  }
#endif
}
static void diagLoop() {
  static uint32_t last = 0;
  if (millis() - last < 5000) return;
  last = millis();
  const uint32_t v = diagVsyncs;
  diagVsyncs = 0;
  PANEL_LOG("lcd4: starts %u why %u %u %u %u %u %u %u %u, panel frames %u in 5 s, up %u s, expander %s, bounce %d px",
            (unsigned)diagBoots, diagWhy[0], diagWhy[1], diagWhy[2], diagWhy[3], diagWhy[4], diagWhy[5], diagWhy[6], diagWhy[7],
            (unsigned)v, (unsigned)(millis() / 1000), haveCh32 ? "ch32v003" : haveTca ? "tca9554" : "NONE", (int)LCD4_BOUNCE_PX);
}
#endif

// ---------------------------------------------------------------- the board
static bool boardBegin() {
  expanderInit();
#ifdef LCD4_SCAN
  // The ST7701's own setup over 3-wire SPI, as Arduino_RGB_Display::begin does it; then the panel.
  if (!spiBus->begin()) return false;
  spiBus->sendCommand(0x01);                         // software reset
  delay(120);
  spiBus->batchOperation(st7701_type1_init_operations, sizeof(st7701_type1_init_operations));
  if (!scan::begin()) return false;
  panelFb = scan::frames[0];
#else
  if (!lcd->begin()) return false;
  panelFb = lcd->getFramebuffer();
#endif
  gt911 = i2cPresent(ADDR_GT911) ? ADDR_GT911 : i2cPresent(ADDR_GT911_ALT) ? ADDR_GT911_ALT : 0;
  PANEL_LOG("lcd4: expander %s, gt911 0x%02X, framebuffer %p", haveCh32 ? "ch32v003" : haveTca ? "tca9554" : "NONE",
            gt911, panelFb);
#ifdef LCD4_DIAG
  diagBegin();
#endif
  return panelFb != nullptr;
}

// The panel is mounted upside down relative to its scan order: the board
// is upright with the image turned 180 degrees, i.e. the frame reversed.
// The touch reports in the panel's own orientation, so it turns the same way.
// One frame into the panel's own framebuffer (PSRAM), turned. Without a
// bounce buffer the panel's DMA reads PSRAM itself, so the frame goes out of
// the CPU's cache; with one, the LCD interrupt copies the framebuffer through
// that same cache, so there is nothing to write back, and a write-back of the
// whole frame would only hold those copies up (as on the round boards).
[[maybe_unused]] static void push(const uint16_t* src) {
  if (!panelFb) return;
  const uint32_t* in = (const uint32_t*)src;
  uint32_t* out = (uint32_t*)panelFb + kW * kH / 2 - 1;
  for (int i = 0; i < kW * kH / 2; i++, out--) {
    const uint32_t two = in[i];
    *out = two >> 16 | two << 16;                // two pixels at a time, swapped
  }
#if LCD4_BOUNCE_PX == 0
  Cache_WriteBack_Addr((uint32_t)panelFb, kW * kH * 2);
#endif
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
  static const BigPanel layout(specBig480());   // the desk panel's UI at 2/3
  b.layout = &layout;
  b.begin = boardBegin;
#ifdef LCD4_SCAN
  b.push = scan::push;
  b.pushRows = scan::pushRows;
  b.frameBegin = scan::frameBegin;
  b.stripRows = 24;                                   // 23 KB of internal RAM
  b.pushStrip = scan::pushStrip;
#else
  b.push = push;
#endif
  b.touch = touch;
  b.brightness = brightness;
  b.bootPin = PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 60;
  gridAppSetup(b, FW_VERSION);
}

void loop() {
  gridAppLoop();
#ifdef LCD4_DIAG
  diagLoop();
#endif
}
