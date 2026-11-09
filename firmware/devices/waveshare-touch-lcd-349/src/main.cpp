// The deck on the Waveshare ESP32-S3-Touch-LCD-3.49 (640 x 172, touch); on the battery, hold PWR to switch off.
// -DSTRIP_SELFTEST (env selftest) builds the hardware self-test instead, without Deck Link.
#include <Arduino.h>

#include "board_349.h"
#include "canvas.h"
#include "font_data.h"

#ifdef STRIP_SELFTEST

static uint16_t* frame = nullptr;
static Canvas canvas{nullptr, 0, 0};
static bool lit = true;

static void drawTest(int tx, int ty, bool down) {
  using namespace board349;
  canvas.fill(rgb565(0x20, 0x20, 0x20));
  canvas.rect(0, 0, 48, 48, rgb565(0xFF, 0, 0));
  canvas.rect(kW - 48, 0, 48, 48, rgb565(0, 0xFF, 0));
  canvas.rect(0, kH - 48, 48, 48, rgb565(0, 0, 0xFF));
  canvas.rect(kW - 48, kH - 48, 48, 48, rgb565(0xFF, 0xFF, 0xFF));
  canvas.frame(0, 0, kW, kH, rgb565(0xFF, 0x4D, 0x00), 2);
  canvas.text(FONT_L, 64, 8, "TOP LEFT red", rgb565(0xE8, 0xE4, 0xDC));
  char s[64];
  snprintf(s, sizeof s, "touch %s %d,%d", down ? "down" : "up", tx, ty);
  canvas.text(FONT_L, 64, 70, s, rgb565(0xE8, 0xE4, 0xDC));
  if (down) canvas.rect(tx - 8, ty - 8, 16, 16, rgb565(0xFF, 0xD0, 0));
}

void setup() {
  Serial.begin(115200);
  pinMode(board349::PIN_BOOT, INPUT_PULLUP);
  const bool ok = board349::begin();
  frame = (uint16_t*)heap_caps_malloc(board349::kW * board349::kH * 2, MALLOC_CAP_SPIRAM);
  canvas = Canvas{frame, board349::kW, board349::kH};
  board349::brightness(255);
  Serial.printf("selftest: panel %s, touch %s, tca9554 %s, psram %u free\n", ok ? "ok" : "FAILED",
                board349::haveTouch ? "ok" : "NONE", board349::haveTca ? "ok" : "NONE",
                (unsigned)heap_caps_get_free_size(MALLOC_CAP_SPIRAM));
}

void loop() {
  static bool bootDown = false;
  const bool b = digitalRead(board349::PIN_BOOT) == LOW;
  if (b && !bootDown) {
    lit = !lit;
    board349::brightness(lit ? 255 : 0);
    Serial.printf("backlight %s\n", lit ? "on" : "off");
  }
  bootDown = b;

  // One read per frame, as the deck firmware polls.
  int x = 0, y = 0;
  const bool down = board349::touch(x, y);
  static bool wasDown = false;
  static uint32_t downAt = 0;
  if (down && !wasDown) {
    downAt = millis();
    Serial.printf("touch down %d,%d\n", x, y);
  }
  if (!down && wasDown)
    Serial.printf("touch up %d,%d after %u ms: %u of %u reads were reports\n", x, y, (unsigned)(millis() - downAt),
                  (unsigned)board349::touchReports, (unsigned)board349::touchReads);
  wasDown = down;

  const uint32_t t0 = millis();
  drawTest(x, y, down);
  const uint32_t t1 = millis();
  board349::push(frame);
  const uint32_t t2 = millis();
  static uint32_t lastStat = 0;
  if (millis() - lastStat > 5000) {
    lastStat = millis();
    Serial.printf("draw %u ms, push %u ms\n", (unsigned)(t1 - t0), (unsigned)(t2 - t1));
  }
  delay(2);
}

#else

#include "grid_app.h"
#include "layout_strip.h"

#define FW_VERSION "0.1.0"

constexpr int PIN_BAT_ADC = 4;                // VBAT through a 1:3 divider
constexpr uint32_t kPowerOffHoldMs = 1500;

// Percent on battery power, -1 while a USB host is there. Read every 10 s:
// the voltage sags while the radio sends.
static int batteryPercent() {
  static int pct = -1;
  static uint32_t lastRead = 0;
  if (HWCDC::isPlugged()) return -1;
  if (pct >= 0 && millis() - lastRead < 10000) return pct;
  lastRead = millis();
  const int mv = (int)analogReadMilliVolts(PIN_BAT_ADC) * 3;
  pct = constrain((mv - 3300) * 100 / (4150 - 3300), 0, 100);
  return pct;
}

// PWR held: off (on the battery; on USB the board stays powered). It arms
// once the press that switched the board on has been let go.
static void powerPump() {
  static bool armed = false;
  static uint32_t downAt = 0;
  const bool down = digitalRead(board349::PIN_PWR) == LOW;
  if (!down) { armed = true; downAt = 0; return; }
  if (!armed) return;
  if (!downAt) downAt = millis();
  if (millis() - downAt < kPowerOffHoldMs) return;
  armed = false;
  if (HWCDC::isPlugged()) {
    uiToast(grid_app::ui, "on USB power", millis());
    grid_app::dirty = true;
    return;
  }
  board349::brightness(0);
  board349::powerOff();
}

void setup() {
  board349::earlyPower();                     // on the battery, before anything slow
  pinMode(board349::PIN_PWR, INPUT_PULLUP);
  static const StripPanel layout;
  GridBoard b{};
  b.type = "waveshare-touch-lcd-349";
  b.layout = &layout;
  b.begin = board349::begin;
  b.push = board349::push;
  b.touch = board349::touch;
  b.brightness = board349::brightness;
  b.bootPin = board349::PIN_BOOT;
  b.brightActive = 255;
  b.brightIdle = 60;                          // the backlight is on or off; idle only trims it
  b.battery = batteryPercent;
  gridAppSetup(b, FW_VERSION);
}

void loop() {
  gridAppLoop();
  powerPump();
}

#endif
