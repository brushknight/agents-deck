#pragma once
// How a board reaches the deck: USB serial or Wi-Fi, and which one to use.
// Pure C++ (no Arduino): net_link.h drives it on the board, tests/test_net.cpp
// on the host.
//
// The rule (docs/link/protocol.md, "Pairing and Wi-Fi"):
//   - At boot the board listens on USB for a few seconds. A deck that answers
//     there (a Welcome) wins: the board stays on USB.
//   - Nobody on USB, and the board has a Wi-Fi network and a certificate:
//     it joins the network and connects to the deck over TLS.
//   - A deck that shows up on USB later (the cable plugged into the Mac)
//     takes over at once: USB is preferred, so pairing and recovery always
//     work over the cable.
//   - On USB, a deck silent for a while (the Mac asleep, the daemon stopped)
//     lets the board try Wi-Fi; it comes back to USB when the deck does.
#include <stdint.h>
#include <string.h>

enum class NetVia : uint8_t { Usb, Wifi };

// What the Wi-Fi side is doing, for the screen and NetStatus.
enum class NetPhase : uint8_t {
  Off,              // Wi-Fi not in use
  Joining,          // joining the network
  Connecting,       // joined; reaching the deck over TLS
  Online,           // a Deck Link session over Wi-Fi
  WrongPassword,    // the network refused the password
  NoNetwork,        // the network wasn't found
  WifiFailed,       // another Wi-Fi failure
  DeckUnreachable,  // joined, but the deck didn't answer
  CertRejected,     // the deck refused the certificate: pair again over USB
};

// What the screen needs to know.
struct NetView {
  NetVia via = NetVia::Usb;
  NetPhase phase = NetPhase::Off;
  bool wifiCapable = false;   // this firmware has Wi-Fi (TLS 1.3)
  char ssid[33] = "";
};

// What net_link.h should do next.
enum class NetAction : uint8_t { None, StartWifi, ConnectDeck, BackToUsb };

struct NetPlan {
  // Timing (ms).
  uint32_t bootListenMs = 4000;     // listen on USB this long at boot
  uint32_t usbQuietMs = 30000;      // a deck silent on USB this long: try Wi-Fi
  uint32_t retryNoNetworkMs = 15000;
  uint32_t retryWrongPasswordMs = 60000;
  uint32_t retryDeckMinMs = 3000, retryDeckMaxMs = 60000;

  NetView view;
  uint32_t bootAt = 0;
  bool usbEver = false;             // a deck has answered on USB since boot
  uint32_t usbLastLive = 0;         // the last moment the USB deck was there
  uint32_t nextTryAt = 0;
  uint32_t deckBackoff = 0;
};

static inline void netBegin(NetPlan& p, uint32_t now, bool wifiCapable, const char* ssid) {
  p.bootAt = now;
  p.usbEver = false;
  p.usbLastLive = p.nextTryAt = p.deckBackoff = 0;
  p.view = NetView();
  p.view.wifiCapable = wifiCapable;
  if (ssid) {
    strncpy(p.view.ssid, ssid, sizeof p.view.ssid - 1);
    p.view.ssid[sizeof p.view.ssid - 1] = '\0';
  }
}

// Wi-Fi credentials set or forgotten (over USB).
static inline void netSetSsid(NetPlan& p, const char* ssid) {
  strncpy(p.view.ssid, ssid ? ssid : "", sizeof p.view.ssid - 1);
  p.view.ssid[sizeof p.view.ssid - 1] = '\0';
  p.nextTryAt = 0;
  if (p.view.phase == NetPhase::WrongPassword || p.view.phase == NetPhase::NoNetwork) p.view.phase = NetPhase::Off;
}

static inline bool netFailed(NetPhase ph) {
  return ph == NetPhase::WrongPassword || ph == NetPhase::NoNetwork || ph == NetPhase::WifiFailed ||
         ph == NetPhase::DeckUnreachable || ph == NetPhase::CertRejected;
}

// Called every loop. usbLive: the USB deck has said Welcome and isn't
// silent. canWifi: credentials, a certificate and TLS 1.3.
static inline NetAction netStep(NetPlan& p, uint32_t now, bool usbLive, bool canWifi) {
  if (usbLive) {
    p.usbEver = true;
    p.usbLastLive = now;
    if (p.view.via == NetVia::Wifi) {
      p.view.via = NetVia::Usb;
      p.view.phase = NetPhase::Off;
      p.deckBackoff = 0;
      return NetAction::BackToUsb;
    }
    if (p.view.phase == NetPhase::CertRejected) p.view.phase = NetPhase::Off;   // re-paired over USB
    return NetAction::None;
  }
  if (p.view.via == NetVia::Usb) {
    if (!canWifi) return NetAction::None;
    const bool quiet = p.usbEver ? now - p.usbLastLive >= p.usbQuietMs : now - p.bootAt >= p.bootListenMs;
    if (!quiet || (int32_t)(now - p.nextTryAt) < 0) return NetAction::None;
    p.view.via = NetVia::Wifi;
    p.view.phase = NetPhase::Joining;
    return NetAction::StartWifi;
  }
  // On Wi-Fi: retry after a failure, except a refused certificate (only USB fixes that).
  if (!canWifi) {
    p.view.via = NetVia::Usb;
    p.view.phase = NetPhase::Off;
    return NetAction::BackToUsb;
  }
  if (netFailed(p.view.phase) && p.view.phase != NetPhase::CertRejected && (int32_t)(now - p.nextTryAt) >= 0) {
    const bool rejoin = p.view.phase != NetPhase::DeckUnreachable;
    p.view.phase = rejoin ? NetPhase::Joining : NetPhase::Connecting;
    return rejoin ? NetAction::StartWifi : NetAction::ConnectDeck;
  }
  return NetAction::None;
}

// The network is joined: reach the deck.
static inline NetAction netJoined(NetPlan& p) {
  if (p.view.via != NetVia::Wifi) return NetAction::None;
  p.view.phase = NetPhase::Connecting;
  return NetAction::ConnectDeck;
}

// Joining failed (or the network dropped): why, and when to try again.
static inline void netWifiFailed(NetPlan& p, uint32_t now, NetPhase why) {
  if (p.view.via != NetVia::Wifi) return;
  p.view.phase = why;
  p.nextTryAt = now + (why == NetPhase::WrongPassword ? p.retryWrongPasswordMs : p.retryNoNetworkMs);
}

// The TLS connection to the deck: up, refused (certificate), or not reached.
static inline void netDeckUp(NetPlan& p) {
  if (p.view.via != NetVia::Wifi) return;
  p.view.phase = NetPhase::Online;
  p.deckBackoff = 0;
}

static inline void netDeckFailed(NetPlan& p, uint32_t now, bool certRejected) {
  if (p.view.via != NetVia::Wifi) return;
  if (certRejected) {
    p.view.phase = NetPhase::CertRejected;
    return;
  }
  p.view.phase = NetPhase::DeckUnreachable;
  p.deckBackoff = p.deckBackoff ? p.deckBackoff * 2 : p.retryDeckMinMs;
  if (p.deckBackoff > p.retryDeckMaxMs) p.deckBackoff = p.retryDeckMaxMs;
  p.nextTryAt = now + p.deckBackoff;
}

// ---------------------------------------------------------------- words

// The notice for a board that isn't live yet: a title and up to two lines
// (nullptr when not needed). For USB with Wi-Fi off it is the usual
// "connecting / waiting for the deck / on USB".
struct NetNotice {
  const char* title;
  const char* line1;
  const char* line2;
  bool problem;   // drawn as a failure
};

static inline NetNotice netNotice(const NetView& v) {
  if (v.via == NetVia::Usb) return {"connecting", "waiting for the deck", "on USB", false};
  switch (v.phase) {
    case NetPhase::Joining:         return {"joining Wi-Fi", v.ssid[0] ? v.ssid : nullptr, nullptr, false};
    case NetPhase::WrongPassword:   return {"Wi-Fi failed", "wrong password", "set it again over USB", true};
    case NetPhase::NoNetwork:       return {"Wi-Fi failed", "network not found", v.ssid[0] ? v.ssid : nullptr, true};
    case NetPhase::WifiFailed:      return {"Wi-Fi failed", "retrying\xE2\x80\xA6", nullptr, true};
    case NetPhase::DeckUnreachable: return {"deck not reachable", "over Wi-Fi", "retrying\xE2\x80\xA6", true};
    case NetPhase::CertRejected:    return {"certificate", "rejected", "pair again over USB", true};
    default:                        return {"connecting", "to the deck", "over Wi-Fi", false};
  }
}
