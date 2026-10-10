// agents-deck panel for the Waveshare ESP32-S3-LCD-1.47B.
//
// The board is a read-only client of the agents-deck daemon (`agentctl
// serve` on the Mac): it opens the panel's event stream over TLS and draws
// the fleet. It never answers a prompt and sends nothing but that one GET.
//
// Everything that can run without hardware lives in lib/core and is tested on
// the Mac. This file is the I/O: Wi-Fi, mDNS, TLS with a pinned certificate,
// the display, the LED, the button and a small debug channel on USB serial.
#include <Arduino.h>
#include <Arduino_GFX_Library.h>
#include <ArduinoJson.h>
#include <ESPmDNS.h>
#include <Preferences.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>

#include "config.h"
#include "screen.h"
#include "sse.h"

#define FW_VERSION "agents-deck-esp32 0.4.0"

#ifndef PANEL_HOSTNAME
#define PANEL_HOSTNAME "agents-deck-panel"
#endif

// ---------------------------------------------------------------- hardware
constexpr int PIN_LCD_DC = 41, PIN_LCD_CS = 42, PIN_LCD_SCK = 40, PIN_LCD_MOSI = 45;
constexpr int PIN_LCD_RST = 39, PIN_LCD_BL = 46, PIN_RGB = 38, PIN_BOOT = 0;
constexpr uint8_t BL_ACTIVE = 220, BL_IDLE = 45;

Arduino_DataBus* bus = new Arduino_ESP32SPI(PIN_LCD_DC, PIN_LCD_CS, PIN_LCD_SCK, PIN_LCD_MOSI, GFX_NOT_DEFINED);
Arduino_GFX* lcd = new Arduino_ST7789(bus, PIN_LCD_RST, 1 /* landscape */, true /* IPS */, 172, 320, 34, 0, 34, 0);

static uint16_t frame[kScreenW * kScreenH];
static Canvas canvas{frame, kScreenW, kScreenH};

// ---------------------------------------------------------------- state
static Fleet fleet, incoming;             // the daemon's last State, and the one being parsed
static bool haveFleet = false;
static uint32_t stateAtMs = 0;            // millis() when `fleet` arrived
static ViewState view;
static DynamicJsonDocument* doc = nullptr;

enum class Net : uint8_t { Wifi, Resolve, Connect, Stream, Wait };
static Net net = Net::Wifi;
// Not named `link`: that is a POSIX function and the names collide.
static LinkState linkState = LinkState::NoWifi;
static bool pairError = false;            // sticky until a stream opens
static WiFiClientSecure client;
static SseScanner scanner;
static IPAddress deckIp;
static uint32_t nextTryMs = 0, backoffMs = 1000, lastByteMs = 0;
static uint32_t statesSeen = 0, parseErrors = 0, pinFailures = 0, reconnects = 0;
static String wifiSsid, wifiPass;
static const char* wifiSource = "none";

static const uint8_t kPinnedCert[32] = DECK_FP;

// The board has no clock of its own: "now" is the daemon's time when the last
// State arrived, plus the milliseconds since.
static int64_t nowEpoch() {
  return haveFleet && fleet.serverTime ? fleet.serverTime + (millis() - stateAtMs) / 1000 : 0;
}

// What is known to be current: nothing while the link is down.
static Counts liveCounts() {
  if (!haveFleet || linkState != LinkState::Live) return Counts{0, 0, 0, 0};
  return countFleet(fleet);
}

// ---------------------------------------------------------------- drawing
static void renderNow() {
  const uint32_t ms = millis();
  const int hero = haveFleet ? resolveHero(fleet, view, ms) : -1;
  drawScreen(canvas, {haveFleet ? &fleet : nullptr, hero, linkState, DECK_HOST, ms, nowEpoch(), view.hold});
  lcd->draw16bitRGBBitmap(0, 0, frame, kScreenW, kScreenH);
}

static void setLed(uint8_t r, uint8_t g, uint8_t b) {
  static uint32_t last = 0xFFFFFFFF;
  const uint32_t v = (uint32_t)r << 16 | (uint32_t)g << 8 | b;
  if (v == last) return;
  last = v;
  neopixelWrite(PIN_RGB, r, g, b);
}

// Orange flashes when somebody needs the user, a slow blue breath while
// agents work, dark otherwise.
static void updateLed() {
  const uint32_t ms = millis();
  const Counts n = liveCounts();
  if (n.waiting || n.error) {
    if ((ms / 125) % 2) setLed(0, 0, 0); else setLed(60, 18, 0);
  } else if (n.running) {
    const uint32_t t = ms % 3000;
    const uint32_t tri = t < 1500 ? t : 3000 - t;          // 0..1500..0
    setLed(0, (uint8_t)(1 + tri * 5 / 1500), (uint8_t)(4 + tri * 36 / 1500));
  } else {
    setLed(0, 0, 0);
  }
}

static void updateBacklight() {
  // Notices are always bright; a quiet or empty board is dim.
  bool active = linkState == LinkState::NoWifi || linkState == LinkState::PairError ||
                (!haveFleet && linkState != LinkState::Live);
  if (!active) {
    const Counts n = liveCounts();
    active = n.running || n.waiting || n.error || n.hungry;
  }
  static int last = -1;
  const int level = active ? BL_ACTIVE : BL_IDLE;
  if (level != last) { analogWrite(PIN_LCD_BL, level); last = level; }
}

// ---------------------------------------------------------------- socket
// Buffered bytes from the TLS socket. The SSE scanner and the JSON parser
// take turns reading from it.
namespace rx {
static uint8_t buf[1024];
static size_t len = 0, pos = 0;

static void clear() { len = pos = 0; }

// Next byte, or -1 when none arrives within waitMs (0 = do not wait).
static int get(uint32_t waitMs) {
  if (pos < len) return buf[pos++];
  const uint32_t t0 = millis();
  for (;;) {
    int n = client.available();
    if (n > 0) {
      n = client.read(buf, n > (int)sizeof buf ? sizeof buf : (size_t)n);
      if (n > 0) {
        len = (size_t)n;
        pos = 1;
        lastByteMs = millis();
        return buf[0];
      }
    }
    if (!waitMs || !client.connected() || millis() - t0 > waitMs) return -1;
    delay(1);
  }
}

// Reads one line (without CR LF) into out. False on timeout.
static bool line(char* out, size_t cap, uint32_t waitMs) {
  size_t n = 0;
  for (;;) {
    const int ch = get(waitMs);
    if (ch < 0) return false;
    if (ch == '\n') break;
    if (ch != '\r' && n < cap - 1) out[n++] = (char)ch;
  }
  out[n] = '\0';
  return true;
}
}  // namespace rx

// What ArduinoJson needs to read a document straight from the socket.
struct SocketReader {
  int read() { return rx::get(3000); }
  size_t readBytes(char* out, size_t n) {
    size_t i = 0;
    for (; i < n; i++) {
      const int ch = rx::get(3000);
      if (ch < 0) break;
      out[i] = (char)ch;
    }
    return i;
  }
};

// ---------------------------------------------------------------- network
static void setLink(LinkState s) {
  if (s == LinkState::Live) pairError = false;
  if (pairError) { linkState = LinkState::PairError; return; }
  // While the board retries in the background, the last fleet stays on screen.
  if (haveFleet && (s == LinkState::Resolving || s == LinkState::Connecting)) s = LinkState::Lost;
  linkState = s;
}

static void retryLater() {
  client.stop();
  rx::clear();
  net = Net::Wait;
  nextTryMs = millis() + backoffMs;
  backoffMs = backoffMs * 2 > 10000 ? 10000 : backoffMs * 2;
  reconnects++;
  setLink(LinkState::Connecting);
}

static void pairingFailed() {
  client.stop();
  rx::clear();
  pairError = true;
  linkState = LinkState::PairError;
  net = Net::Wait;
  nextTryMs = millis() + 10000;
}

static void onState() {
  if (hasNewWaiting(fleet, incoming)) view.hold = false;   // never hide a new prompt behind a hold
  fleet = incoming;
  haveFleet = true;
  stateAtMs = millis();
  statesSeen++;
  setLink(LinkState::Live);
}

static void connectDeck() {
  setLink(LinkState::Connecting);
  renderNow();                                            // the handshake blocks for a second or two
  client.stop();
  rx::clear();
  client.setInsecure();                                   // trust is the pinned fingerprint below, not a CA
  client.setHandshakeTimeout(8);
  if (!client.connect(deckIp, DECK_PORT, 5000)) { retryLater(); return; }

  // Check the certificate before a single byte of the token leaves the board.
  uint8_t fp[32];
  if (!client.getFingerprintSHA256(fp) || memcmp(fp, kPinnedCert, sizeof fp) != 0) {
    pinFailures++;
    pairingFailed();
    return;
  }

  // HTTP/1.0 on purpose: the daemon then streams without chunked encoding.
  client.printf("GET /v1/events HTTP/1.0\r\nHost: %s.local:%d\r\nAuthorization: Bearer %s\r\n"
                "Accept: text/event-stream\r\n\r\n", DECK_HOST, DECK_PORT, DECK_TOKEN);

  char ln[160];
  if (!rx::line(ln, sizeof ln, 5000)) { retryLater(); return; }
  const char* sp = strchr(ln, ' ');
  const int code = sp ? atoi(sp + 1) : 0;
  if (code == 401 || code == 403) { pairingFailed(); return; }
  if (code != 200) { retryLater(); return; }
  do {                                                    // skip the response headers
    if (!rx::line(ln, sizeof ln, 5000)) { retryLater(); return; }
  } while (ln[0]);

  scanner.reset();
  lastByteMs = millis();
  backoffMs = 1000;
  net = Net::Stream;
}

static void netPump() {
  const uint32_t now = millis();

  if (WiFi.status() != WL_CONNECTED) {
    if (net != Net::Wifi) { client.stop(); rx::clear(); net = Net::Wifi; }
    linkState = LinkState::NoWifi;
    static uint32_t lastKick = 0;
    if (wifiSsid.length() && now - lastKick > 15000) {
      lastKick = now;
      WiFi.disconnect();
      WiFi.begin(wifiSsid.c_str(), wifiPass.c_str());
    }
    return;
  }

  switch (net) {
    case Net::Wifi:
      MDNS.end();
      MDNS.begin(PANEL_HOSTNAME);
      net = Net::Resolve;
      nextTryMs = now;
      setLink(LinkState::Resolving);
      break;

    case Net::Wait:
      if ((int32_t)(now - nextTryMs) >= 0) net = Net::Resolve;
      break;

    case Net::Resolve:
      if ((int32_t)(now - nextTryMs) < 0) break;
      setLink(LinkState::Resolving);
      renderNow();                                        // the query blocks for up to 2 s
      if (!deckIp.fromString(DECK_HOST)) deckIp = MDNS.queryHost(String(DECK_HOST), 2000);
      if (deckIp == IPAddress(0, 0, 0, 0)) { nextTryMs = millis() + 5000; break; }
      net = Net::Connect;
      break;

    case Net::Connect:
      connectDeck();
      break;

    case Net::Stream: {
      // One state is a few kilobytes. The cap keeps the screen and the button alive
      // if the daemon ever floods the stream.
      for (int budget = 65536; budget > 0; budget--) {
        const int ch = rx::get(0);
        if (ch < 0) break;
        if (scanner.feed((char)ch) != SseEvent::DataStart) continue;
        SocketReader reader;
        if (parseState(reader, incoming, *doc)) onState(); else parseErrors++;
      }
      if (!client.connected() && !client.available()) { retryLater(); break; }
      if (millis() - lastByteMs > 40000) retryLater();    // the daemon pings every 15 s
      break;
    }
  }
}

// ---------------------------------------------------------------- button
static void buttonPump() {
  static bool down = false;
  static uint32_t changedAt = 0;
  const bool level = digitalRead(PIN_BOOT) == LOW;
  const uint32_t now = millis();
  if (level == down || now - changedAt < 30) return;      // debounce
  down = level;
  changedAt = now;
  if (down && haveFleet) {
    pressNext(fleet, view, now);
    renderNow();
  }
}

// ---------------------------------------------------------------- debug serial
static const char* linkName() {
  switch (linkState) {
    case LinkState::NoWifi:     return "no_wifi";
    case LinkState::Resolving:  return "resolving";
    case LinkState::Connecting: return "connecting";
    case LinkState::PairError:  return "pair_error";
    case LinkState::Live:       return "live";
    default:                    return "lost";
  }
}

static void printStatus() {
  StaticJsonDocument<896> s;
  s["fw"] = FW_VERSION;
  s["wifi"] = WiFi.status() == WL_CONNECTED ? "connected" : "down";
  s["wifi_src"] = wifiSource;
  s["ip"] = WiFi.localIP().toString();
  s["rssi"] = WiFi.RSSI();
  s["deck_ip"] = deckIp.toString();
  s["link"] = linkName();
  s["agents"] = haveFleet ? fleet.count : 0;
  s["total"] = haveFleet ? fleet.total : 0;
  const int hero = haveFleet ? resolveHero(fleet, view, millis()) : -1;
  s["hero"] = hero >= 0 ? fleet.agents[hero].title : "";
  s["hold"] = view.hold;
  s["server"] = haveFleet ? fleet.server : "";
  s["states"] = statesSeen;
  s["parse_errors"] = parseErrors;
  s["pin_failures"] = pinFailures;
  s["reconnects"] = reconnects;
  s["heap"] = ESP.getFreeHeap();
  s["min_heap"] = ESP.getMinFreeHeap();
  s["uptime_s"] = millis() / 1000;
  serializeJson(s, Serial);
  Serial.println();
}

// One JSON object per line: {"cmd":"status"} or {"cmd":"frame"}. Anything
// that is not a known command is ignored, so a stray write from another tool
// on this port does no harm.
static void command(char* s) {
  char* w = s;                                            // drop spaces: {"cmd": "frame"} is fine too
  for (char* r = s; *r; r++)
    if (*r != ' ') *w++ = *r;
  *w = '\0';
  if (strstr(s, "\"cmd\":\"frame\"")) {
    Serial.printf("{\"frame_bytes\":%u}\n", (unsigned)sizeof frame);
    Serial.write((const uint8_t*)frame, sizeof frame);
    Serial.println();
  } else if (strstr(s, "\"cmd\":\"status\"")) {
    printStatus();
  }
}

static void serialPump() {
  static char line[96];
  static size_t n = 0;
  static bool tooLong = false;
  while (Serial.available()) {
    const char ch = (char)Serial.read();
    if (ch == '\n') {
      if (!tooLong) { line[n] = '\0'; command(line); }
      n = 0;
      tooLong = false;
    } else if (ch != '\r') {
      if (n < sizeof line - 1) line[n++] = ch; else tooLong = true;
    }
  }
}

// ---------------------------------------------------------------- arduino
void setup() {
  Serial.begin(115200);
  pinMode(PIN_BOOT, INPUT_PULLUP);
  pinMode(PIN_LCD_BL, OUTPUT);
  analogWrite(PIN_LCD_BL, BL_ACTIVE);
  setLed(0, 0, 0);

  if (!lcd->begin()) {
    for (;;) { Serial.println("{\"error\":\"display_init_failed\"}"); delay(1000); }
  }
  doc = new DynamicJsonDocument(32768);

  wifiSsid = WIFI_SSID;
  wifiPass = WIFI_PASS;
  if (wifiSsid.length()) wifiSource = "config";
#ifdef WIFI_NVS_NAMESPACE
  // A board that had another firmware may already keep its Wi-Fi in NVS
  // ("ssid" and "pass"). Those win; config.h is the fallback.
  Preferences nvs;
  if (nvs.begin(WIFI_NVS_NAMESPACE, true)) {
    if (nvs.isKey("ssid")) { wifiSsid = nvs.getString("ssid", wifiSsid); wifiSource = "nvs"; }
    if (nvs.isKey("pass")) wifiPass = nvs.getString("pass", wifiPass);
    nvs.end();
  }
#endif

  WiFi.mode(WIFI_STA);
  WiFi.setHostname(PANEL_HOSTNAME);
  WiFi.setAutoReconnect(true);
  WiFi.setSleep(false);                                   // USB powered: prefer a prompt stream
  if (wifiSsid.length()) WiFi.begin(wifiSsid.c_str(), wifiPass.c_str());

  renderNow();
  Serial.println("{\"ready\":\"" FW_VERSION "\"}");
}

void loop() {
  serialPump();
  buttonPump();
  netPump();
  updateLed();

  static uint32_t lastFrame = 0;
  if (millis() - lastFrame >= 200) {
    lastFrame = millis();
    updateBacklight();
    renderNow();
  }
  delay(2);
}
