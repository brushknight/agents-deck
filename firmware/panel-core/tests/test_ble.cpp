// Bluetooth on the boards (ble_model.h, net_model.h): which link carries the
// session under each preference, when the board advertises, the gesture that
// forgets the bond, and what every layout says and draws for it.
#define PANEL_MAX_AGENTS 64
#include <assert.h>
#include <stdio.h>

#include <vector>

#include "../sim/fixture.h"
#include "ble_model.h"
#include "layout_big.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "layout_spring.h"
#include "layout_square.h"
#include "net_model.h"

static void decide() {
  // USB holds: Bluetooth never carries, whatever the preference; a bond stays
  // findable (standby) so the deck can add or check it.
  for (NetUplink p : {NetUplink::Auto, NetUplink::Wifi, NetUplink::Ble}) {
    BleDecision d = bleDecide(p, true, false, true, true, false);
    assert(!d.wanted && d.advertise == BleAdvertise::Standby && !d.dropCentral);
    d = bleDecide(p, true, false, true, true, true);   // the bonded deck is connected: let it go
    assert(!d.wanted && d.dropCentral);
    d = bleDecide(p, true, false, true, true, true, true);   // ...unless it is being introduced
    assert(!d.wanted && !d.dropCentral);
    // No bond: always open for setup, even on USB.
    d = bleDecide(p, true, false, true, false, false);
    assert(d.advertise == BleAdvertise::Open && !d.dropCentral);
  }
  // auto: Bluetooth while Wi-Fi isn't up; gives way once it is.
  BleDecision d = bleDecide(NetUplink::Auto, false, false, true, true, false);
  assert(d.wanted && d.advertise == BleAdvertise::Bonded);
  d = bleDecide(NetUplink::Auto, false, true, true, true, true);
  assert(!d.wanted && d.advertise == BleAdvertise::Standby && d.dropCentral);
  // wifi: Bluetooth only while the board can't use Wi-Fi yet (setting it up).
  d = bleDecide(NetUplink::Wifi, false, false, true, true, false);
  assert(!d.wanted && d.advertise == BleAdvertise::Standby);
  d = bleDecide(NetUplink::Wifi, false, false, false, true, true);
  assert(d.wanted && !d.dropCentral);
  // ble: Bluetooth even with Wi-Fi up (net_link.h keeps Wi-Fi off then anyway).
  d = bleDecide(NetUplink::Ble, false, true, true, true, true);
  assert(d.wanted && d.advertise == BleAdvertise::Bonded && !d.dropCentral);
  assert(!uplinkAllowsWifi(NetUplink::Ble) && uplinkAllowsWifi(NetUplink::Auto) && uplinkAllowsWifi(NetUplink::Wifi));
  assert(uplinkOf(0) == NetUplink::Auto && uplinkOf(1) == NetUplink::Wifi && uplinkOf(2) == NetUplink::Ble &&
         uplinkOf(9) == NetUplink::Auto);
}

// Adding a board over Bluetooth while USB (or Wi-Fi) carries its session:
// the freshly paired central keeps its link for the introduction, then goes;
// the bond keeps advertising so the deck finds it again.
static void introduction() {
  assert(!bleIntroducing(false, 0, false));                  // nobody secure: nothing to introduce
  assert(bleIntroducing(true, 0, false));                    // just paired (or encrypted with the bond)
  assert(bleIntroducing(true, kBleIntroMs - 1, false));
  assert(!bleIntroducing(true, kBleIntroMs, false));         // the window is over
  assert(!bleIntroducing(true, 10, true));                   // it carried the session: no window after
  // On USB, the passkey typed: the board is bonded now, the deck reads Info
  // and checks on USB, the link stays.
  uint32_t secureFor = 0;
  for (; secureFor < kBleIntroMs; secureFor += 500) {
    BleDecision d = bleDecide(NetUplink::Auto, true, false, true, true, true, bleIntroducing(true, secureFor, false));
    assert(!d.wanted && !d.dropCentral);
  }
  // Then it goes, and the board waits on standby.
  BleDecision d = bleDecide(NetUplink::Auto, true, false, true, true, true, bleIntroducing(true, secureFor, false));
  assert(!d.wanted && d.dropCentral);
  d = bleDecide(NetUplink::Auto, true, false, true, true, false);
  assert(d.advertise == BleAdvertise::Standby);
  // The same on Wi-Fi under auto.
  d = bleDecide(NetUplink::Auto, false, true, true, true, true, true);
  assert(!d.wanted && !d.dropCentral);
  // The deck sets ble and the cable goes: Bluetooth is wanted, advertised
  // quickly, and the deck's link (secure, not new) is kept and carries.
  d = bleDecide(NetUplink::Ble, false, false, true, true, true, false);
  assert(d.wanted && d.advertise == BleAdvertise::Bonded && !d.dropCentral);
  // Back to auto with Wi-Fi up: Bluetooth gives way at once (it carried).
  d = bleDecide(NetUplink::Auto, false, true, true, true, true, bleIntroducing(true, 100, true));
  assert(!d.wanted && d.dropCentral && d.advertise == BleAdvertise::Standby);
  // Wi-Fi goes: Bluetooth is wanted again, and findable.
  d = bleDecide(NetUplink::Auto, false, false, true, true, false);
  assert(d.wanted && d.advertise == BleAdvertise::Bonded);
}

static void usbHolds() {
  NetPlan p;
  netBegin(p, 1000, true, "");
  assert(netUsbHolds(p, 1000) && netUsbHolds(p, 4999) && !netUsbHolds(p, 5000));   // the boot window
  netStep(p, 6000, true, false);                                                    // a deck on USB
  assert(netUsbHolds(p, 6000 + p.usbQuietMs - 1) && !netUsbHolds(p, 6000 + p.usbQuietMs));
}

static void forgetGesture() {
  BleForget g;
  uint32_t t = 100;
  // No bond: holding does nothing.
  assert(bleForgetInput(g, true, false, t) == ForgetEvent::None);
  assert(bleForgetInput(g, true, false, t + 6000) == ForgetEvent::None && !g.asking);
  assert(bleForgetInput(g, false, false, t + 6100) == ForgetEvent::None);
  // Held 5 s: asked once; released; pressed again: forgotten.
  t = 10000;
  assert(bleForgetInput(g, true, true, t) == ForgetEvent::None);
  assert(bleForgetInput(g, true, true, t + 4999) == ForgetEvent::None);
  assert(bleForgetInput(g, true, true, t + 5000) == ForgetEvent::Ask && g.asking);
  assert(bleForgetInput(g, true, true, t + 9000) == ForgetEvent::None);   // still held: no second question
  assert(bleForgetInput(g, false, true, t + 9100) == ForgetEvent::None && g.asking);
  assert(bleForgetInput(g, true, true, t + 9200) == ForgetEvent::Forget && !g.asking);
  // Held through: the confirming press doesn't ask again.
  assert(bleForgetInput(g, true, true, t + 20000) == ForgetEvent::None && !g.asking);
  assert(bleForgetInput(g, false, true, t + 20100) == ForgetEvent::None);
  // Asked, then nothing for 10 s: the question goes.
  t = 50000;
  bleForgetInput(g, true, true, t);
  assert(bleForgetInput(g, true, true, t + 5000) == ForgetEvent::Ask);
  bleForgetInput(g, false, true, t + 5100);
  assert(bleForgetInput(g, false, true, t + 15000) == ForgetEvent::Cancel && !g.asking);
  assert(bleForgetInput(g, true, true, t + 15100) == ForgetEvent::None);   // a press now is just a press
  bleForgetInput(g, false, true, t + 15200);
  // A short press never asks.
  assert(bleForgetInput(g, true, true, t + 30000) == ForgetEvent::None);
  assert(bleForgetInput(g, false, true, t + 30500) == ForgetEvent::None && !g.asking);
  // Across the millis() wrap.
  t = 0xFFFFF000u;
  bleForgetInput(g, true, true, t);
  assert(bleForgetInput(g, true, true, t + 5000) == ForgetEvent::Ask);
}

static void words() {
  NetView v;
  v.ble.capable = true;
  snprintf(v.ble.name, sizeof v.ble.name, "deck-1a2b3c");
  assert(!netOverlay(v) && !netNoticeShown(v));

  // The passkey covers everything, on USB too, with the name the computer lists.
  v.ble.phase = BlePhase::Pairing;
  v.ble.passkey = 42917;
  NetNotice n = netNotice(v);
  assert(netOverlay(v) && netNoticeShown(v));
  assert(!strcmp(n.title, "pair deck-1a2b3c") && !strcmp(n.line1, "042917") && n.code && !n.problem);
  v.ota.phase = OtaPhase::Downloading;   // even over an update's notice
  assert(!strcmp(netNotice(v).line1, "042917"));
  v.ota = OtaView();

  // "Forget Bluetooth?"
  v.ble.phase = BlePhase::Bonded;
  v.ble.forgetAsk = true;
  n = netNotice(v);
  assert(netOverlay(v) && !strcmp(n.title, "forget Bluetooth?") && !strcmp(n.line2, "deck-1a2b3c") && n.problem);
  v.ble.forgetAsk = false;

  // Waiting over Bluetooth: setup, bonded, connected.
  v.via = NetVia::Ble;
  assert(netNoticeShown(v) && !netOverlay(v));
  v.ble.phase = BlePhase::Setup;
  n = netNotice(v);
  assert(!strcmp(n.title, "Bluetooth setup") && !strcmp(n.line1, "deck-1a2b3c") && !n.problem);
  v.ble.phase = BlePhase::Bonded;
  n = netNotice(v);
  assert(!strcmp(n.title, "waiting for the deck") && !strcmp(n.line1, "over Bluetooth"));
  v.ble.phase = BlePhase::Connected;
  assert(!strcmp(netNotice(v).line2, "over Bluetooth"));

  // The words outlive the view they came from.
  {
    NetView t = v;
    t.ble.phase = BlePhase::Pairing;
    t.ble.passkey = 7;
    n = netNotice(t);
  }
  assert(!strcmp(n.line1, "000007") && !strcmp(n.title, "pair deck-1a2b3c"));
}

static int countColor(const Canvas& c, int x0, int y0, int x1, int y1, uint16_t col) {
  int k = 0;
  for (int y = y0; y < y1; y++)
    for (int x = x0; x < x1; x++) k += c.px[y * c.w + x] == col;
  return k;
}

static void drawing() {
  static DeckFleet d;
  assert(fixture::load(FIXTURE, d));
  Fleet f = d.fleet;
  static std::vector<uint16_t> buf(480 * 600);
  NetView v;
  v.ble.capable = true;
  snprintf(v.ble.name, sizeof v.ble.name, "deck-1a2b3c");

  // Every Bluetooth notice draws on every kind of screen; the passkey in the accent.
  const SquarePanel t4s3(specT4S3()), amoled(specAmoled18()), lcd4(specLcd4());
  const BigPanel big480(specBig480());
  PanelUi pu;
  const uint16_t accent = deckTheme(true).accent;   // the same on every theme
  for (const PanelLayout* L : {(const PanelLayout*)&t4s3, (const PanelLayout*)&amoled, (const PanelLayout*)&lcd4,
                               (const PanelLayout*)&big480}) {
    Canvas c{buf.data(), L->width(), L->height()};
    v.via = NetVia::Ble;
    for (BlePhase ph : {BlePhase::Setup, BlePhase::Bonded, BlePhase::Connected, BlePhase::Pairing}) {
      v.ble.phase = ph;
      v.ble.passkey = 42917;
      PanelInput in{nullptr, LinkState::Connecting, "board", DeckScope::View, 1000, 0, &pu, 0, &v};
      L->draw(c, in);
      const int lit = countColor(c, 0, 0, c.w, c.h, accent);
      if (ph == BlePhase::Pairing) assert(lit > 40);   // the six digits
    }
    // Home with the Bluetooth mark: different pixels from the Wi-Fi mark.
    v.ble.phase = BlePhase::Connected;
    v.ble.carrying = true;
    pu = PanelUi();
    pu.wifi = true;
    PanelInput live{&f, LinkState::Live, "board", DeckScope::Act, 1000, f.serverTime, &pu, 0, &v};
    pu.ble = true;
    L->draw(c, live);
    std::vector<uint16_t> ble(buf.begin(), buf.begin() + c.w * c.h);
    pu.ble = false;
    L->draw(c, live);
    assert(!std::equal(ble.begin(), ble.end(), buf.begin()));
    v.ble.carrying = false;
    pu = PanelUi();
  }

  // The round boards: the mark under home's middle, and the notices.
  Canvas c{buf.data(), 480, 480};
  const RoundTheme RT = roundTheme();
  DialPanel dial(layoutDial21());
  OrbitPanel orbit(layoutOrbit28());
  SpringPanel spring21(layoutSpring21()), spring28(layoutSpring28());
  RoundUi ui;
  for (const RoundLayout* L : {(const RoundLayout*)&dial, (const RoundLayout*)&orbit, (const RoundLayout*)&spring21,
                               (const RoundLayout*)&spring28}) {
    roundSync(ui, *L, f, 1000);
    ui.screen = RoundScreen::Home;
    v.via = NetVia::Ble;
    v.ble.phase = BlePhase::Connected;
    v.ble.carrying = true;
    RoundInput in{&f, LinkState::Live, "board", RoundScope::Act, 1000, f.serverTime, &ui, 0, &v};
    L->draw(c, in);
    const int base = L->spring() ? L->spring()->netBase : kNetBadgeY;
    int marks = countColor(c, 220, base - 22, 260, base + 4, RT.t.dim);
    if (!L->spring()) marks += countColor(c, 220, base - 22, 260, base + 4, RT.t.bg);
    assert(marks > 10);
    v.ble.carrying = false;
    for (BlePhase ph : {BlePhase::Setup, BlePhase::Bonded, BlePhase::Connected, BlePhase::Pairing}) {
      v.ble.phase = ph;
      RoundInput ni{nullptr, LinkState::Connecting, "board", RoundScope::View, 1000, 0, &ui, 0, &v};
      L->draw(c, ni);
      if (ph == BlePhase::Pairing) assert(countColor(c, 0, 300, 480, 345, RT.t.accent) > 40);
    }
  }
}

int main() {
  decide();
  introduction();
  usbHolds();
  forgetGesture();
  words();
  drawing();
  printf("ble: ok\n");
  return 0;
}
