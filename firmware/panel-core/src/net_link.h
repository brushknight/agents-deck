#pragma once
// A touch board's way to the deck: USB serial, or Wi-Fi once it is paired
// (docs/link/protocol.md, "Pairing and Wi-Fi"). Shared by every board that
// runs panel-core; round_app.h uses it. Arduino on ESP32 only.
//
//   - Pairing: on USB, approved, and the deck doesn't hold this board's
//     certificate (Welcome.cert_serial): a key on the chip, a CSR, the
//     certificate back; all of it kept in NVS ("decklink").
//   - Wi-Fi credentials come over USB (WifiConfig) and go to NVS; the board
//     tries them once at once, staying on USB, and reports how it went.
//   - Which link: net_model.h (USB at boot if a deck answers there, else
//     Wi-Fi; USB again as soon as its deck shows up).
//   - Over Wi-Fi: join, find the deck (mDNS for its .local name, then its
//     addresses), TLS 1.3 with the certificate (deck_tls.h, in a task of its
//     own so the screen keeps drawing), and a second Deck Link core on it.
//
// Builds without TLS 1.3 (arduino-esp32 2.0.x) get the USB-only part:
// active() is always the USB link and the deck hears "no Wi-Fi" (501).
#if defined(ARDUINO)
#include <Arduino.h>

#include "deck_link.h"
#include "deck_net_config.h"
#include "deck_pair.h"
#include "deck_store.h"
#include "deck_tls.h"
#include "net_model.h"

#if DECK_LINK_HAS_TLS13
#include <ESPmDNS.h>
#include <WiFi.h>
#endif

#ifndef NET_LOG
#define NET_LOG(...) do {} while (0)
#endif

namespace net_link {

struct Hooks {
  void (*switched)(NetVia via);       // the link the screen follows changed: start the fleet over
  void (*toast)(const char* text);    // a short note on the screen
};

static NetPlan plan;
static Hooks hooks;
static deck_link_t* usb = nullptr;    // the USB core (DeckLink::core())
static deck_kv_t kv;
static bool kvOk = false;
static deck_cert_t cert;              // about 2 KB: static, not on a stack
static deck_wifi_t wifi;
static deck_pairing_t pairing;
static uint32_t lastPairAt = 0;
static char boardName[40] = "";
static bool statusDue = false;        // send NetStatus from loop(): after the Result the deck waits for

static bool hasCert() { return deck_cert_valid(&cert); }
static bool canWifi() { return DECK_LINK_HAS_TLS13 && kvOk && wifi.ssid[0] && hasCert(); }

#if DECK_LINK_HAS_TLS13
static deck_link_t wcore;             // the Wi-Fi core
static deck_tls_t tls;
static bool tlsUp = false;
static uint32_t tlsSince = 0;

// The connect task's result (main loop reads it once the task is done).
enum class Conn : uint8_t { Idle, Running, Done };
static volatile Conn conn = Conn::Idle;
static volatile deck_tls_err_t connErr = DECK_TLS_OK;
static volatile bool connAbandon = false;   // USB took over while it ran
static char connHost[DECK_HOST_SIZE] = "";

// Wi-Fi events (from the Wi-Fi task).
static volatile bool evJoined = false, evLost = false;
static volatile uint8_t evReason = 0;
static bool wifiOn = false;
static bool mdnsOn = false;

// A test of new credentials while on USB.
static bool testing = false;
static uint32_t testUntil = 0;
static char testedIp[16] = "";
static int8_t testedRssi = 0;
static agentsdeck_link_v1_WifiState testResult = agentsdeck_link_v1_WifiState_WIFI_UNSPECIFIED;
#endif

// ---------------------------------------------------------------- NetStatus

static deck_link_t* active() {
#if DECK_LINK_HAS_TLS13
  if (plan.view.via == NetVia::Wifi && tlsUp) return &wcore;
#endif
  return usb;
}

static NetView view() { return plan.view; }

// Over Wi-Fi but not (yet) talking to the deck: the screen shows where it is.
static bool wifiPending() {
#if DECK_LINK_HAS_TLS13
  return plan.view.via == NetVia::Wifi && !tlsUp;
#else
  return false;
#endif
}

static void sendStatus() {
  deck_link_t* c = active();
  if (!c || !deck_link_online(c)) return;
  agentsdeck_link_v1_NetStatus s = agentsdeck_link_v1_NetStatus_init_zero;
  s.transport = c == usb ? agentsdeck_link_v1_Transport_TRANSPORT_SERIAL : agentsdeck_link_v1_Transport_TRANSPORT_WIFI;
  s.has_cert = hasCert();
  snprintf(s.ssid, sizeof s.ssid, "%s", wifi.ssid);
#if DECK_LINK_HAS_TLS13
  if (!wifi.ssid[0]) {
    s.wifi = agentsdeck_link_v1_WifiState_WIFI_NOT_SET;
  } else if (testing) {
    s.wifi = agentsdeck_link_v1_WifiState_WIFI_JOINING;
  } else if (plan.view.via == NetVia::Usb) {
    s.wifi = testResult != agentsdeck_link_v1_WifiState_WIFI_UNSPECIFIED ? testResult
                                                                         : agentsdeck_link_v1_WifiState_WIFI_IDLE;
    if (s.wifi == agentsdeck_link_v1_WifiState_WIFI_IDLE) {
      snprintf(s.ip, sizeof s.ip, "%s", testedIp);
      s.rssi = testedRssi;
    }
  } else {
    switch (plan.view.phase) {
      case NetPhase::Joining:         s.wifi = agentsdeck_link_v1_WifiState_WIFI_JOINING; break;
      case NetPhase::WrongPassword:   s.wifi = agentsdeck_link_v1_WifiState_WIFI_WRONG_PASSWORD; break;
      case NetPhase::NoNetwork:       s.wifi = agentsdeck_link_v1_WifiState_WIFI_NO_NETWORK; break;
      case NetPhase::WifiFailed:      s.wifi = agentsdeck_link_v1_WifiState_WIFI_FAILED; break;
      case NetPhase::DeckUnreachable: s.wifi = agentsdeck_link_v1_WifiState_WIFI_DECK_UNREACHABLE; break;
      case NetPhase::CertRejected:    s.wifi = agentsdeck_link_v1_WifiState_WIFI_CERT_REJECTED; break;
      default:                        s.wifi = agentsdeck_link_v1_WifiState_WIFI_CONNECTED; break;
    }
    if (WiFi.isConnected()) {
      snprintf(s.ip, sizeof s.ip, "%s", WiFi.localIP().toString().c_str());
      s.rssi = WiFi.RSSI();
    }
  }
#else
  s.wifi = agentsdeck_link_v1_WifiState_WIFI_UNSUPPORTED;
#endif
  deck_net_status(c, &s);
}

// ---------------------------------------------------------------- Wi-Fi

#if DECK_LINK_HAS_TLS13
static NetPhase phaseOf(uint8_t reason) {
  switch (reason) {
    case WIFI_REASON_4WAY_HANDSHAKE_TIMEOUT:
    case WIFI_REASON_HANDSHAKE_TIMEOUT:
    case WIFI_REASON_AUTH_FAIL:
    case WIFI_REASON_AUTH_EXPIRE:
      return NetPhase::WrongPassword;
    case WIFI_REASON_NO_AP_FOUND:
    case WIFI_REASON_NO_AP_FOUND_W_COMPATIBLE_SECURITY:
    case WIFI_REASON_NO_AP_FOUND_IN_AUTHMODE_THRESHOLD:
    case WIFI_REASON_NO_AP_FOUND_IN_RSSI_THRESHOLD:
      return NetPhase::NoNetwork;
    default:
      return NetPhase::WifiFailed;
  }
}

static agentsdeck_link_v1_WifiState stateOf(NetPhase p) {
  switch (p) {
    case NetPhase::WrongPassword: return agentsdeck_link_v1_WifiState_WIFI_WRONG_PASSWORD;
    case NetPhase::NoNetwork:     return agentsdeck_link_v1_WifiState_WIFI_NO_NETWORK;
    default:                      return agentsdeck_link_v1_WifiState_WIFI_FAILED;
  }
}

static void onWifiEvent(arduino_event_id_t event, arduino_event_info_t info) {
  if (event == ARDUINO_EVENT_WIFI_STA_GOT_IP) {
    evJoined = true;
  } else if (event == ARDUINO_EVENT_WIFI_STA_DISCONNECTED) {
    evReason = info.wifi_sta_disconnected.reason;
    evLost = true;
  }
}

static void wifiStart() {
  evJoined = evLost = false;
  if (!wifiOn) {
    WiFi.persistent(false);         // the credentials live in our NVS namespace, not the driver's
    WiFi.mode(WIFI_STA);
    WiFi.setAutoReconnect(false);   // net_model.h decides when to try again
    WiFi.setSleep(true);
    wifiOn = true;
  } else {
    WiFi.disconnect(false, false);
  }
  NET_LOG("net: joining %s", wifi.ssid);
  WiFi.begin(wifi.ssid, wifi.password[0] ? wifi.password : nullptr);
}

static void wifiStop() {
  if (!wifiOn) return;
  if (mdnsOn) {
    MDNS.end();
    mdnsOn = false;
  }
  WiFi.disconnect(true, false);
  WiFi.mode(WIFI_OFF);
  wifiOn = false;
  evJoined = evLost = false;
  NET_LOG("net: Wi-Fi off");
}

// ---------------------------------------------------------------- TLS

// Where a host is: an address as is; a .local name through mDNS. "" when not found.
static void resolve(const char* host, char* out, size_t cap) {
  out[0] = '\0';
  const size_t n = strlen(host);
  if (n > 6 && strcasecmp(host + n - 6, ".local") == 0) {
    char label[DECK_HOST_SIZE];
    snprintf(label, sizeof label, "%.*s", (int)(n - 6), host);
    if (!mdnsOn) mdnsOn = MDNS.begin(boardName[0] ? boardName : "agents-deck-board");
    IPAddress ip = MDNS.queryHost(label, 2500);
    if (ip != IPAddress((uint32_t)0)) snprintf(out, cap, "%s", ip.toString().c_str());
    return;
  }
  snprintf(out, cap, "%s", host);
}

static void connectTask(void*) {
  deck_tls_err_t e = DECK_TLS_CONNECT;
  for (uint8_t i = 0; i < cert.host_count && !connAbandon; i++) {
    char addr[48];
    resolve(cert.hosts[i], addr, sizeof addr);
    if (!addr[0]) {
      NET_LOG("net: %s didn't resolve", cert.hosts[i]);
      e = DECK_TLS_RESOLVE;
      continue;
    }
    e = deck_tls_open(&tls, &cert, cert.hosts[i], addr, 6000);
    NET_LOG("net: %s (%s): %s, mbedtls -0x%04x, alert %d", cert.hosts[i], addr, deck_tls_strerror(e),
            (unsigned)-tls.mbedtls_error, tls.alert);
    if (e == DECK_TLS_OK) {
      snprintf(connHost, sizeof connHost, "%s", cert.hosts[i]);
      break;
    }
    deck_tls_close(&tls);
    if (e == DECK_TLS_CERT_REJECTED) break;
  }
  connErr = e;
  conn = Conn::Done;
  vTaskDelete(nullptr);
}

static void connectStart() {
  if (conn == Conn::Running) return;
  connAbandon = false;
  conn = Conn::Running;
  // mbedTLS's handshake wants a deep stack; core 0, beside the Wi-Fi driver.
  if (xTaskCreatePinnedToCore(connectTask, "deck-tls", 12288, nullptr, 1, nullptr, 0) != pdPASS) {
    connErr = DECK_TLS_CONNECT;
    conn = Conn::Done;
  }
}

static void tlsDown() {
  if (!tlsUp) return;
  deck_tls_close(&tls);
  tlsUp = false;
}
#endif

// ---------------------------------------------------------------- the plan

static void apply(NetAction a) {
#if DECK_LINK_HAS_TLS13
  switch (a) {
    case NetAction::StartWifi:
      testing = false;
      wifiStart();
      break;
    case NetAction::ConnectDeck:
      connectStart();
      break;
    case NetAction::BackToUsb:
      NET_LOG("net: back to USB");
      if (conn == Conn::Running) connAbandon = true;
      tlsDown();
      wifiStop();
      if (hooks.switched) hooks.switched(NetVia::Usb);
      break;
    default:
      break;
  }
#else
  (void)a;
#endif
}

// ---------------------------------------------------------------- the USB link's callbacks

// Every Welcome on the USB link: USB wins, and pairing when needed.
static void usbWelcome(bool approved, const char* name) {
  if (name && name[0]) snprintf(boardName, sizeof boardName, "%s", name);
  apply(netStep(plan, millis(), true, canWifi()));
  if (approved && DECK_LINK_HAS_TLS13 && kvOk && !pairing.seq && deck_cert_needed(&cert, deck_link_cert_serial(usb)) &&
      (!lastPairAt || millis() - lastPairAt > 30000)) {
    lastPairAt = millis();
    const uint32_t seq = deck_pair_request(usb, &pairing, boardName);
    NET_LOG("net: pairing, seq %u", (unsigned)seq);
    (void)seq;
  }
  sendStatus();
}

static void certIssued(uint32_t seq, const agentsdeck_link_v1_CertIssued* in) {
  deck_cert_t next;
  if (!deck_pair_accept(&pairing, seq, in, &next)) return;
  if (!deck_store_save_cert(&kv, &next)) {
    NET_LOG("net: can't save the certificate");
    return;
  }
  cert = next;
  memset(&next, 0, sizeof next);
  plan.view.phase = plan.view.phase == NetPhase::CertRejected ? NetPhase::Off : plan.view.phase;
  NET_LOG("net: paired as %s, certificate %s", cert.device_name, cert.cert_serial);
  if (hooks.toast) hooks.toast("paired for Wi-Fi");
  sendStatus();
}

static void result(uint32_t seq, uint32_t code) {
  if (pairing.seq && seq == pairing.seq) {
    NET_LOG("net: pairing refused (%u)", (unsigned)code);
    deck_pair_cancel(&pairing);
  }
}

#if DECK_LINK_HAS_TLS13
static uint32_t wifiConfig(deck_link_t* c, const char* ssid, const char* password, bool forget) {
  if (c != usb) return 403;   // credentials come over the cable only
  if (!kvOk) return 500;
  if (forget) {
    if (!deck_store_clear_wifi(&kv)) return 500;
    memset(&wifi, 0, sizeof wifi);
    netSetSsid(plan, "");
    testResult = agentsdeck_link_v1_WifiState_WIFI_UNSPECIFIED;
    testedIp[0] = '\0';
    if (hooks.toast) hooks.toast("Wi-Fi forgotten");
    statusDue = true;
    return 204;
  }
  const size_t sl = strlen(ssid), pl = strlen(password);
  if (sl == 0 || sl > 32 || (pl > 0 && pl < 8) || pl > 64) return 400;
  deck_wifi_t w;
  memset(&w, 0, sizeof w);
  memcpy(w.ssid, ssid, sl);
  memcpy(w.password, password, pl);
  const bool ok = deck_store_save_wifi(&kv, &w);
  if (ok) wifi = w;
  memset(&w, 0, sizeof w);
  if (!ok) return 500;
  netSetSsid(plan, wifi.ssid);
  // Try it now, staying on USB, and tell the deck how it went.
  testResult = agentsdeck_link_v1_WifiState_WIFI_UNSPECIFIED;
  testedIp[0] = '\0';
  testing = true;
  testUntil = millis() + 20000;
  wifiStart();
  if (hooks.toast) hooks.toast("trying Wi-Fi\xE2\x80\xA6");
  statusDue = true;
  return 204;
}

static void testDone(agentsdeck_link_v1_WifiState r) {
  testing = false;
  testResult = r == agentsdeck_link_v1_WifiState_WIFI_CONNECTED ? agentsdeck_link_v1_WifiState_WIFI_UNSPECIFIED : r;
  if (r == agentsdeck_link_v1_WifiState_WIFI_CONNECTED) {
    snprintf(testedIp, sizeof testedIp, "%s", WiFi.localIP().toString().c_str());
    testedRssi = (int8_t)WiFi.RSSI();
  }
  wifiStop();
  if (hooks.toast)
    hooks.toast(r == agentsdeck_link_v1_WifiState_WIFI_CONNECTED       ? "Wi-Fi works"
                : r == agentsdeck_link_v1_WifiState_WIFI_WRONG_PASSWORD ? "Wi-Fi: wrong password"
                : r == agentsdeck_link_v1_WifiState_WIFI_NO_NETWORK     ? "Wi-Fi: network not found"
                                                                        : "Wi-Fi failed");
  NET_LOG("net: test %s", testedIp[0] ? testedIp : "failed");
  sendStatus();
}
#endif

// ---------------------------------------------------------------- setup and loop

// The USB link is up (deck.begin done). cfg is its configuration: the Wi-Fi
// link gets the same board, subscription and callbacks.
static void begin(deck_link_t* usbCore, const deck_link_config_t& cfg, const Hooks& h) {
  usb = usbCore;
  hooks = h;
  kvOk = deck_kv_nvs_open(&kv, "decklink");
  if (kvOk) {
    (void)deck_store_load_cert(&kv, &cert);
    (void)deck_store_load_wifi(&kv, &wifi);
  }
  netBegin(plan, millis(), DECK_LINK_HAS_TLS13, wifi.ssid);
  NET_LOG("net: nvs %s, certificate %s, Wi-Fi %s, TLS 1.3 %s", kvOk ? "ok" : "FAILED", hasCert() ? cert.cert_serial : "none",
          wifi.ssid[0] ? "set" : "not set", DECK_LINK_HAS_TLS13 ? "yes" : "no");
#if DECK_LINK_HAS_TLS13
  deck_link_config_t w = cfg;
  w.write = deck_tls_write;
  w.millis = [](void*) -> uint32_t { return (uint32_t)millis(); };
  w.user = &tls;
  w.on_wifi_config = nullptr;   // never over Wi-Fi
  deck_link_init(&wcore, &w);
  WiFi.onEvent(onWifiEvent);
#else
  (void)cfg;
#endif
}

// The callback for the USB core's on_wifi_config (nullptr: no Wi-Fi here, the core answers 501).
static uint32_t (*wifiConfigHook())(deck_link_t*, const char*, const char*, bool) {
#if DECK_LINK_HAS_TLS13
  return wifiConfig;
#else
  return nullptr;
#endif
}

// Runs the plan, the Wi-Fi events and the TLS link. Call it every loop.
static void loop() {
  const uint32_t now = millis();
  if (statusDue) {
    statusDue = false;
    sendStatus();
  }
#if DECK_LINK_HAS_TLS13
  // A credentials test on USB.
  if (testing) {
    if (evJoined) testDone(agentsdeck_link_v1_WifiState_WIFI_CONNECTED);
    else if (evLost) testDone(stateOf(phaseOf(evReason)));
    else if ((int32_t)(now - testUntil) >= 0) testDone(agentsdeck_link_v1_WifiState_WIFI_NO_NETWORK);
    return;
  }
  const NetView before = plan.view;
  if (plan.view.via == NetVia::Wifi && evLost) {
    evLost = false;
    NET_LOG("net: Wi-Fi lost, reason %u", (unsigned)evReason);
    if (conn == Conn::Running) connAbandon = true;
    tlsDown();
    netWifiFailed(plan, now, phaseOf(evReason));
  }
  if (plan.view.via == NetVia::Wifi && evJoined) {
    evJoined = false;
    NET_LOG("net: joined, %s", WiFi.localIP().toString().c_str());
    apply(netJoined(plan));
  }
  if (conn == Conn::Done) {
    conn = Conn::Idle;
    if (connAbandon || plan.view.via != NetVia::Wifi) {
      deck_tls_close(&tls);
    } else if (connErr == DECK_TLS_OK) {
      tlsUp = true;
      tlsSince = now;
      netDeckUp(plan);
      if (hooks.switched) hooks.switched(NetVia::Wifi);
      deck_link_begin(&wcore);   // Hello over TLS
    } else {
      netDeckFailed(plan, now, connErr == DECK_TLS_CERT_REJECTED);
    }
  }
  if (tlsUp) {
    uint8_t buf[512];
    for (int i = 0; i < 16; i++) {
      const int n = deck_tls_read(&tls, buf, sizeof buf);
      if (n > 0) {
        deck_link_feed(&wcore, buf, (size_t)n);
        continue;
      }
      if (n < 0) {
        NET_LOG("net: TLS closed: %s", deck_tls_strerror((deck_tls_err_t)-n));
        tlsDown();
        netDeckFailed(plan, now, -n == DECK_TLS_CERT_REJECTED);
      }
      break;
    }
    if (tlsUp) {
      deck_link_poll(&wcore);
      // Silent for 20 s on Wi-Fi: the connection is gone; start over.
      if (deck_link_state(&wcore) == DECK_LINK_OFFLINE && now - tlsSince > 20000) {
        tlsDown();
        netDeckFailed(plan, now, false);
      }
    }
  }
  apply(netStep(plan, now, deck_link_state(usb) == DECK_LINK_ONLINE, canWifi()));
  if (before.via != plan.view.via || before.phase != plan.view.phase) sendStatus();
#else
  (void)now;
#endif
}

// A Welcome on the Wi-Fi link: the deck hears how the board is connected.
static void wifiWelcome() { sendStatus(); }

}  // namespace net_link

#endif  // ARDUINO
