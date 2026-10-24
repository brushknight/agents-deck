// How a board picks its way to the deck (net_model.h): USB at boot when a
// deck answers there, Wi-Fi when nobody does and it can, back to USB when
// the cable's deck shows up, the retries after each failure; and what the
// round screens say and draw for it.
#define PANEL_MAX_AGENTS 64
#include <assert.h>
#include <stdio.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "layout_spring.h"
#include "net_model.h"

static void bootChoice() {
  // A deck answers on USB within the listening window: USB, Wi-Fi never starts.
  NetPlan p;
  netBegin(p, 1000, true, "lab-net");
  assert(netStep(p, 1500, false, true) == NetAction::None);
  assert(netStep(p, 2000, true, true) == NetAction::None);
  assert(p.view.via == NetVia::Usb && p.view.phase == NetPhase::Off);
  assert(netStep(p, 9000, true, true) == NetAction::None);

  // Nobody on USB: Wi-Fi after the window, not before.
  netBegin(p, 1000, true, "lab-net");
  assert(netStep(p, 4999, false, true) == NetAction::None);
  assert(netStep(p, 5000, false, true) == NetAction::StartWifi);
  assert(p.view.via == NetVia::Wifi && p.view.phase == NetPhase::Joining);
  assert(netStep(p, 5001, false, true) == NetAction::None);   // started once
  assert(netJoined(p) == NetAction::ConnectDeck && p.view.phase == NetPhase::Connecting);
  netDeckUp(p);
  assert(p.view.phase == NetPhase::Online);

  // No credentials or no certificate (or no TLS 1.3): stays on USB.
  netBegin(p, 1000, false, "");
  assert(netStep(p, 60000, false, false) == NetAction::None && p.view.via == NetVia::Usb);
}

static void usbWinsLater() {
  NetPlan p;
  netBegin(p, 0, true, "lab-net");
  assert(netStep(p, 5000, false, true) == NetAction::StartWifi);
  netJoined(p);
  netDeckUp(p);
  // The cable goes into the Mac and the deck says Welcome there: back to USB at once.
  assert(netStep(p, 20000, true, true) == NetAction::BackToUsb);
  assert(p.view.via == NetVia::Usb && p.view.phase == NetPhase::Off);
  // The USB deck goes quiet (Mac asleep): Wi-Fi only after usbQuietMs.
  assert(netStep(p, 21000, false, true) == NetAction::None);
  assert(netStep(p, 20000 + p.usbQuietMs - 1, false, true) == NetAction::None);
  assert(netStep(p, 20000 + p.usbQuietMs, false, true) == NetAction::StartWifi);
  // Credentials forgotten while on Wi-Fi: back to USB.
  assert(netStep(p, 60000, false, false) == NetAction::BackToUsb && p.view.via == NetVia::Usb);
}

static void retries() {
  NetPlan p;
  netBegin(p, 0, true, "lab-net");
  assert(netStep(p, 5000, false, true) == NetAction::StartWifi);
  netWifiFailed(p, 6000, NetPhase::WrongPassword);
  assert(p.view.phase == NetPhase::WrongPassword);
  assert(netStep(p, 6000 + p.retryWrongPasswordMs - 1, false, true) == NetAction::None);
  assert(netStep(p, 6000 + p.retryWrongPasswordMs, false, true) == NetAction::StartWifi);
  netWifiFailed(p, 70000, NetPhase::NoNetwork);
  assert(netStep(p, 70000 + p.retryNoNetworkMs, false, true) == NetAction::StartWifi);
  // The deck doesn't answer: back off 3 s, 6 s, 12 s … up to a minute, without rejoining.
  netJoined(p);
  uint32_t t = 100000, last = 0;
  for (int i = 0; i < 8; i++) {
    netDeckFailed(p, t, false);
    assert(p.view.phase == NetPhase::DeckUnreachable);
    const uint32_t wait = p.nextTryAt - t;
    assert(wait >= last && wait <= p.retryDeckMaxMs);
    last = wait;
    assert(netStep(p, p.nextTryAt - 1, false, true) == NetAction::None);
    t = p.nextTryAt;
    assert(netStep(p, t, false, true) == NetAction::ConnectDeck);
  }
  assert(last == p.retryDeckMaxMs);
  netDeckUp(p);
  assert(p.deckBackoff == 0);
  // A refused certificate: no retries over Wi-Fi; USB fixes it (pairing again).
  netDeckFailed(p, t, true);
  assert(p.view.phase == NetPhase::CertRejected);
  assert(netStep(p, t + 3600000, false, true) == NetAction::None);
  assert(netStep(p, t + 3600001, true, true) == NetAction::BackToUsb && p.view.phase == NetPhase::Off);
  // New credentials (over USB) clear a wrong password.
  p.view.phase = NetPhase::WrongPassword;
  netSetSsid(p, "other-net");
  assert(p.view.phase == NetPhase::Off && strcmp(p.view.ssid, "other-net") == 0);
}

static void words() {
  NetView v;
  NetNotice n = netNotice(v);
  assert(!strcmp(n.title, "connecting") && !strcmp(n.line2, "on USB") && !n.problem);
  v.via = NetVia::Wifi;
  snprintf(v.ssid, sizeof v.ssid, "lab-net");
  v.phase = NetPhase::Joining;
  n = netNotice(v);
  assert(!strcmp(n.title, "joining Wi-Fi") && !strcmp(n.line1, "lab-net"));
  v.phase = NetPhase::Connecting;
  assert(!strcmp(netNotice(v).line2, "over Wi-Fi"));
  v.phase = NetPhase::WrongPassword;
  assert(netNotice(v).problem && !strcmp(netNotice(v).line1, "wrong password"));
  v.phase = NetPhase::CertRejected;
  assert(!strcmp(netNotice(v).line2, "pair again over USB"));
}

static std::vector<uint16_t> frame(480 * 480);

static int countColor(const Canvas& c, int x0, int y0, int x1, int y1, uint16_t col) {
  int n = 0;
  for (int y = y0; y < y1; y++)
    for (int x = x0; x < x1; x++) n += c.px[y * c.w + x] == col;
  return n;
}

static void drawing() {
  static DeckFleet d;
  assert(fixture::load(FIXTURE, d));
  Fleet f = d.fleet;
  RoundUi ui;
  NetView v;
  v.via = NetVia::Wifi;
  v.phase = NetPhase::Online;
  Canvas c{frame.data(), 480, 480};
  const RoundTheme T = roundTheme();
  DialPanel dial(layoutDial21());
  OrbitPanel orbit(layoutOrbit28());
  SpringPanel spring21(layoutSpring21()), spring28(layoutSpring28());
  for (const RoundLayout* L : {(const RoundLayout*)&dial, (const RoundLayout*)&orbit, (const RoundLayout*)&spring21,
                               (const RoundLayout*)&spring28}) {
    roundSync(ui, *L, f, 1000);
    ui.screen = RoundScreen::Home;
    RoundInput in{&f, LinkState::Live, "board", RoundScope::Act, 1000, f.serverTime, &ui, 0, &v};
    L->draw(c, in);
    // The spring has the mark under its context bar; the others at kNetBadgeY.
    const int base = L->spring() ? L->spring()->netBase : kNetBadgeY;
    int marks = countColor(c, 220, base - 22, 260, base + 4, T.t.dim);
    if (!L->spring()) marks += countColor(c, 220, base - 22, 260, base + 4, T.t.bg);
    assert(marks > 20);
    // On USB: no mark (the same pixels as without any NetView).
    v.via = NetVia::Usb;
    L->draw(c, in);
    std::vector<uint16_t> usb(frame);
    in.net = nullptr;
    L->draw(c, in);
    assert(usb == frame);
    v.via = NetVia::Wifi;
    // Every notice fits and draws.
    for (NetPhase ph : {NetPhase::Joining, NetPhase::Connecting, NetPhase::WrongPassword, NetPhase::NoNetwork,
                        NetPhase::WifiFailed, NetPhase::DeckUnreachable, NetPhase::CertRejected}) {
      v.phase = ph;
      RoundInput ni{nullptr, LinkState::Connecting, "board", RoundScope::View, 1000, 0, &ui, 0, &v};
      L->draw(c, ni);
    }
    v.phase = NetPhase::Online;
  }
}

// Every toast stays inside the round glass: its pill's corners are inside the circle.
static void toasts() {
  for (const char* t : {"sent", "focus", "interrupted", "hold to stop", "stopping\xE2\x80\xA6", "paired for Wi-Fi",
                        "trying Wi-Fi\xE2\x80\xA6", "Wi-Fi works", "Wi-Fi forgotten", "Wi-Fi: wrong password",
                        "Wi-Fi: network not found", "Wi-Fi failed", "the prompt changed", "not allowed: scope", "network not found", "wrong password",
                        "a very long toast that cannot fit anywhere at all"}) {
    // Dial and Orbit: no higher than 300; the spring: under its context bar.
    for (int top : {kToastTop, springDial21().barY + 8, springOrbit28().barY + 8}) {
      const ToastBox b = toastBox(t, top);
      for (int x : {b.x, b.x + b.w})
        for (int y : {b.y, b.y + b.h}) {
          // The pill's ends are round: test the corners of its straight part (inset by the radius).
          const float px = x == b.x ? x + b.h / 2.f : x - b.h / 2.f;
          const float dx = px - 240, dy = y - 240;
          const float r = sqrtf(dx * dx + dy * dy);
          assert(r <= 240 - 4);
        }
      assert(b.w > 0 && b.y >= top && b.font);
      // The real toasts (not the made-up long ones) are never cut.
      if (strlen(t) <= 18) assert(b.w == textW(*b.font, t) + 40);
    }
  }
}

int main() {
  toasts();
  bootChoice();
  usbWinsLater();
  retries();
  words();
  drawing();
  return 0;
}
