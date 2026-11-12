#pragma once
// Bluetooth LE as a way to the deck, the part without a radio: when the
// board advertises, when Bluetooth carries the Deck Link session, and the
// gesture that forgets the bond. Pure C++ (no Arduino): ble_link.h and
// net_link.h drive it on the board, tests/test_ble.cpp on the host.
//
// The rules (docs/link/protocol.md, "Bluetooth"):
//   - USB wins while a deck answers there (and for the first seconds after
//     boot, while the board listens for one).
//   - Otherwise the deck's preference (LinkConfig, kept in NVS) decides:
//       auto  Wi-Fi when its session is up, else Bluetooth
//       wifi  Wi-Fi only; Bluetooth only while the board has no Wi-Fi
//             network or certificate yet (so it can still be set up)
//       ble   Bluetooth only; Wi-Fi stays off
//   - One session at a time: Bluetooth carries it only when the above picks
//     it, and gives way (the board drops the central) as soon as another
//     link does.
//   - A board with no bond advertises to anyone (setup) and pairs with a
//     passkey on its screen. A bonded board always advertises (slower while
//     Bluetooth isn't wanted) so its deck can find it again, and takes only
//     its bonded deck: anyone else is dropped unless it encrypts with the
//     bond's keys within seconds.
//   - Introduction: a central that has just made (or used) the bond keeps
//     its link for kBleIntroMs even while another link carries the session,
//     so the deck can finish adding the board: it checks the board id over
//     the encrypted link and hears NetStatus.ble CONNECTED on the board's
//     USB or Wi-Fi session. Then, if Bluetooth still isn't wanted, the board
//     lets it go.
//   - Holding BOOT (or the screen, while it shows a notice) for 5 s asks
//     "forget Bluetooth?"; pressing again within 10 s forgets the bond and
//     the board is back in setup.
#include <stdint.h>

enum class NetUplink : uint8_t { Auto, Wifi, Ble };

// What the radio is doing, for the screen and NetStatus.
enum class BlePhase : uint8_t {
  Off,        // no Bluetooth in this firmware, or stopped (a firmware update)
  Setup,      // no bond: open to anyone, pairs with a passkey
  Bonded,     // bonded, no deck connected
  Pairing,    // a central is connected and pairing: the passkey is on the screen
  Connected,  // the bonded deck is connected over an encrypted, authenticated link
};

struct BleView {
  bool capable = false;          // this firmware has Bluetooth
  BlePhase phase = BlePhase::Off;
  bool carrying = false;         // Bluetooth carries the Deck Link session now
  uint32_t passkey = 0;          // shown while phase == Pairing
  bool forgetAsk = false;        // "forget Bluetooth?" is on the screen
  char name[12] = "";            // "deck-1a2b3c"
};

// Open: connectable, anyone may pair (setup). Bonded: connectable, but only
// the bonded deck gets in (ble_link.h refuses pairing and drops anyone who
// doesn't encrypt with the bond's keys). Standby: the same, less often,
// while another link carries the session: the deck can still reach the
// board (to add it, check it, or take over when that link goes).
enum class BleAdvertise : uint8_t { Off, Open, Bonded, Standby };

// How long a fresh central keeps its link while Bluetooth isn't wanted.
constexpr uint32_t kBleIntroMs = 20000;

struct BleDecision {
  bool wanted;              // Bluetooth may carry the session now
  BleAdvertise advertise;
  bool dropCentral;         // a bonded central is connected but another link has the session
};

// The central is being introduced: secure for less than kBleIntroMs, and
// Bluetooth hasn't carried the session on this connection.
static inline bool bleIntroducing(bool centralSecure, uint32_t secureForMs, bool carried) {
  return centralSecure && !carried && secureForMs < kBleIntroMs;
}

// usbHolds: a deck answers on USB (or the board is still listening there
// after boot). wifiUp: the Wi-Fi session is up. wifiPossible: the board has
// a network and a certificate (and the preference allows Wi-Fi).
// introducing: bleIntroducing() for the connected central.
static inline BleDecision bleDecide(NetUplink pref, bool usbHolds, bool wifiUp, bool wifiPossible, bool bonded,
                                    bool centralSecure, bool introducing = false) {
  bool wanted = false;
  if (!usbHolds) {
    switch (pref) {
      case NetUplink::Ble:  wanted = true; break;
      case NetUplink::Wifi: wanted = !wifiPossible; break;
      default:              wanted = !wifiUp; break;
    }
  }
  BleDecision d;
  d.wanted = wanted;
  d.advertise = !bonded ? BleAdvertise::Open : wanted ? BleAdvertise::Bonded : BleAdvertise::Standby;
  d.dropCentral = bonded && centralSecure && !wanted && !introducing;
  return d;
}

// The preference as the deck sends it (agentsdeck_link_v1_Uplink: 0 auto, 1 wifi, 2 ble).
static inline NetUplink uplinkOf(uint32_t v) {
  return v == 1 ? NetUplink::Wifi : v == 2 ? NetUplink::Ble : NetUplink::Auto;
}

// Does the Wi-Fi side run at all under this preference?
static inline bool uplinkAllowsWifi(NetUplink pref) { return pref != NetUplink::Ble; }

// ---------------------------------------------------------------- forgetting the bond

struct BleForget {
  uint32_t holdMs = 5000;      // hold this long to be asked
  uint32_t confirmMs = 10000;  // then press again within this long
  bool down = false;
  uint32_t downAt = 0;
  bool askedThisPress = false;
  bool asking = false;
  uint32_t askedAt = 0;
};

enum class ForgetEvent : uint8_t { None, Ask, Forget, Cancel };

// One sample of "pressed or not": BOOT, or a finger on a notice screen (the
// caller ORs the two). Call it every loop, pressed or not, so the question
// times out.
// bonded: there is a bond to forget (otherwise nothing happens).
static inline ForgetEvent bleForgetInput(BleForget& g, bool down, bool bonded, uint32_t ms) {
  ForgetEvent ev = ForgetEvent::None;
  if (g.asking && (uint32_t)(ms - g.askedAt) >= g.confirmMs) {
    g.asking = false;
    ev = ForgetEvent::Cancel;
  }
  if (down && !g.down) {   // pressed
    g.down = true;
    g.downAt = ms;
    g.askedThisPress = false;
    if (g.asking) {
      g.asking = false;
      g.askedThisPress = true;   // this press is spent: don't ask again while held
      return bonded ? ForgetEvent::Forget : ForgetEvent::Cancel;
    }
    return ev;
  }
  if (!down) {
    g.down = false;
    return ev;
  }
  if (bonded && !g.asking && !g.askedThisPress && (uint32_t)(ms - g.downAt) >= g.holdMs) {
    g.asking = true;
    g.askedAt = ms;
    g.askedThisPress = true;
    return ForgetEvent::Ask;
  }
  return ev;
}
