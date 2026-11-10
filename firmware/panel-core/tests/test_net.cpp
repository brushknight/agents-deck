// How a board picks its way to the deck (net_model.h): USB at boot when a
// deck answers there, Wi-Fi when nobody does and it can, back to USB when
// the cable's deck shows up, the retries after each failure; and what the
// round screens say and draw for it.
#define PANEL_MAX_AGENTS 64
#include <assert.h>
#include <stdio.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_big.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "layout_spring.h"
#include "layout_square.h"
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

// A firmware update over Wi-Fi (ota_model.h): what the screen says, how
// often the deck hears, and what a board does at boot after the update.
static void firmwareUpdate() {
  // Progress: every 5 %, or after 3 s when it moved, and always at 100.
  assert(!otaReportDue(0, 0, 4, 1000) && otaReportDue(0, 0, 5, 1000));
  assert(otaReportDue(10, 0, 11, 3000) && !otaReportDue(10, 0, 10, 9000) && otaReportDue(97, 0, 100, 10));

  // At boot.
  OtaMark m;
  assert(otaBootDecision(false, m, "app0") == OtaBoot::Normal);
  assert(otaBootDecision(true, m, "app1") == OtaBoot::PendingBootloader);   // the bootloader rolls back on a reset
  m.pending = 1;
  strcpy(m.next, "app1");
  assert(otaBootDecision(false, m, "app1") == OtaBoot::Normal);             // no old slot named: nothing to go back to
  strcpy(m.prev, "app0");
  assert(otaBootDecision(false, m, "app1") == OtaBoot::PendingApp);
  assert(otaBootDecision(false, m, "app0") == OtaBoot::Normal);             // the bootloader already went back
  m.boots = kOtaMaxBoots - 1;
  assert(otaBootDecision(false, m, "app1") == OtaBoot::PendingApp);
  m.boots = kOtaMaxBoots;
  assert(otaBootDecision(false, m, "app1") == OtaBoot::RevertNow);
  assert(otaBootDecision(true, m, "app1") == OtaBoot::PendingBootloader);   // the bootloader's own state wins
  assert(!otaGiveUp(1000, 1000 + kOtaConfirmMs - 1) && otaGiveUp(1000, 1000 + kOtaConfirmMs));
  assert(otaGiveUp(0xFFFFF000u, 0xFFFFF000u + kOtaConfirmMs));      // across the millis() wrap

  // The board's mark in an image, read in chunks: found across a chunk's end,
  // and only whole ("...-lcd-2" isn't "...-lcd-21").
  {
    static uint8_t img[5000];
    memset(img, 0xFF, sizeof img);
    const char mark[] = "agents-deck-board:waveshare-touch-lcd-21";
    auto reader = [](uint32_t off, uint8_t* to, uint32_t n) {
      memcpy(to, img + off, n);
      return true;
    };
    uint8_t buf[64 + sizeof mark];
    for (size_t at : {(size_t)0, (size_t)30, (size_t)60, (size_t)63, (size_t)64, (size_t)100, (size_t)4000, sizeof img - sizeof mark}) {
      memset(img, 0xFF, sizeof img);
      memcpy(img + at, mark, sizeof mark);
      assert(otaFindMark(mark, sizeof mark, sizeof img, buf, sizeof buf, reader));
      assert(!otaFindMark(mark, sizeof mark, (uint32_t)(at + sizeof mark - 1), buf, sizeof buf, reader));   // cut short
    }
    memset(img, 0xFF, sizeof img);
    memcpy(img + 500, "agents-deck-board:waveshare-touch-lcd-2", 40);   // with its NUL
    assert(!otaFindMark(mark, sizeof mark, sizeof img, buf, sizeof buf, reader));
    const char shorter[] = "agents-deck-board:waveshare-touch-lcd-2";
    memset(img, 0xFF, sizeof img);
    memcpy(img + 900, mark, sizeof mark);   // "...-lcd-21" isn't a mark for "...-lcd-2"
    assert(!otaFindMark(shorter, sizeof shorter, sizeof img, buf, sizeof buf, reader));
    assert(otaFindMark(mark, sizeof mark, sizeof img, buf, sizeof buf, reader));
    assert(!otaFindMark(mark, sizeof mark, sizeof img, buf, sizeof buf, [](uint32_t, uint8_t*, uint32_t) { return false; }));
  }

  // The words: the update wins over the link's notice, on USB too.
  NetView v;
  v.ota.phase = OtaPhase::Downloading;
  v.ota.percent = 42;
  strcpy(v.ota.version, "0.2.1");
  NetNotice n = netNotice(v);
  assert(strcmp(n.title, "updating") == 0 && strcmp(n.line1, "0.2.1 \xC2\xB7 42%") == 0 && !n.problem);
  v.ota.phase = OtaPhase::Failed;
  strcpy(v.ota.error, "the image's SHA-256 doesn't match");
  n = netNotice(v);
  assert(strcmp(n.title, "update failed") == 0 && n.problem && strcmp(n.line2, "kept this firmware") == 0);
  v.ota.error[0] = 0;
  assert(netNotice(v).line1 == nullptr);
  v.ota = OtaView();
  assert(strcmp(netNotice(v).title, "connecting") == 0);

  // Every phase draws on every kind of screen.
  static std::vector<uint16_t> big(480 * 600);
  Canvas c{big.data(), 480, 480};
  const SquarePanel t4s3(specT4S3()), amoled(specAmoled18()), lcd4(specLcd4());
  const BigPanel big480(specBig480());
  DialPanel dial(layoutDial21());
  SpringPanel spring(layoutSpring21());
  PanelUi pu;
  RoundUi ru;
  v.via = NetVia::Wifi;
  strcpy(v.ota.version, "0.2.1");
  for (OtaPhase ph : {OtaPhase::Downloading, OtaPhase::Verifying, OtaPhase::Restarting, OtaPhase::Failed}) {
    v.ota.phase = ph;
    strcpy(v.ota.error, "the deck refused the download, a long reason");
    for (const PanelLayout* L : {(const PanelLayout*)&t4s3, (const PanelLayout*)&amoled, (const PanelLayout*)&lcd4,
                                 (const PanelLayout*)&big480}) {
      Canvas sc{big.data(), L->width(), L->height()};
      PanelInput in{nullptr, LinkState::Connecting, "board", DeckScope::View, 1000, 0, &pu, 0, &v};
      L->draw(sc, in);
    }
    for (const RoundLayout* L : {(const RoundLayout*)&dial, (const RoundLayout*)&spring}) {
      RoundInput in{nullptr, LinkState::Connecting, "board", RoundScope::View, 1000, 0, &ru, 0, &v};
      L->draw(c, in);
    }
  }
}

// Every toast stays inside the round glass: its pill's corners are inside the circle.
static void toasts() {
  for (const char* t : {"sent", "focus", "interrupted", "hold to stop", "stopping\xE2\x80\xA6", "paired for Wi-Fi",
                        "trying Wi-Fi\xE2\x80\xA6", "update installed", "Wi-Fi works", "Wi-Fi forgotten", "Wi-Fi: wrong password",
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
  firmwareUpdate();
  return 0;
}
