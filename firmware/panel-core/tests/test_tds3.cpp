// The LilyGO T-Display-S3's screen and input (layout_tds3.h): every frame
// stays inside the canvas, and touch, buttons and results do what the
// layout's header says.
#include <assert.h>
#include <string.h>
#include <string>
#include <vector>
// Another layout first: tds3 must build next to the others (one hungry(), one centered()).
#include "layout_square.h"
#include "layout_tds3.h"
#include "../sim/fixture.h"

using namespace tds3;

static const uint16_t GUARD = 0xDEAD;
static const int PAD = 2 * kW;

static int find(const Fleet& f, const char* title) {
  for (int i = 0; i < f.count; i++)
    if (!strcmp(f.agents[i].title, title)) return i;
  return -1;
}

int main() {
  static DeckFleet d;
  assert(fixture::load(FIXTURE, d));
  const Fleet& f = d.fleet;
  const int permission = find(f, "infra-network");   // waiting: yes / yes, and don't ask… / no
  const int question = find(f, "auth-flow");         // waiting: three long answers
  assert(permission >= 0 && question >= 0);
  const int running = autoHero(f) == permission ? find(f, "checkout-api") : autoHero(f);
  assert(running >= 0 && f.agents[running].status != Status::Waiting);

  std::vector<uint16_t> mem(kW * kH + 2 * PAD, GUARD);
  Canvas c{mem.data() + PAD, kW, kH};
  auto guards = [&] {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[PAD + kW * kH + i] != GUARD) return false;
    return true;
  };

  // ---- drawing: every hero, both scopes, with and without a toast and a pressed button
  {
    Ui ui;
    for (int hero = 0; hero < f.count; hero++)
      for (DeckScope s : {DeckScope::View, DeckScope::Act})
        for (uint32_t ms : {0u, 650u, 1300u})
          for (bool withToast : {false, true}) {
            ui.down = withToast;
            ui.pressed = withToast ? kHitButton + 1 : -1;
            if (withToast) toast(ui, "the prompt changed", ms);
            else ui.toast[0] = '\0';
            draw(c, {&f, hero, LinkState::Live, "m", ms, f.serverTime + 754, hero % 2 == 0, s, &ui});
            assert(guards());
          }
    // Every link state and the empty cases.
    static Fleet empty;
    for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Live, LinkState::Lost}) {
      draw(c, {&f, 0, ls, "lilygo-t-display-s3-a1b2c3", 100, f.serverTime, false, DeckScope::Act, nullptr});
      draw(c, {&empty, -1, ls, "x", 100, 0, false, DeckScope::Act, nullptr});
      draw(c, {nullptr, -1, ls, nullptr, 100, 0, false, DeckScope::View, nullptr});
      draw(c, {&f, 99, ls, "a-very-long-device-name-that-does-not-fit-on-the-screen-at-all", 100, 0, true,
               DeckScope::Act, nullptr});
      assert(guards());
    }
  }

  // ---- Wi-Fi: every phase's notice, and the mark in the footer once live
  {
    NetView net;
    net.via = NetVia::Wifi;
    net.wifiCapable = true;
    snprintf(net.ssid, sizeof net.ssid, "a-network-with-a-long-name-0123");
    for (NetPhase ph : {NetPhase::Off, NetPhase::Joining, NetPhase::WrongPassword, NetPhase::NoNetwork,
                        NetPhase::WifiFailed, NetPhase::DeckUnreachable, NetPhase::CertRejected})
      for (LinkState ls : {LinkState::Connecting, LinkState::Live, LinkState::Lost}) {
        net.phase = ph;
        Input in{&f, running, ls, "m", 300, f.serverTime, true, DeckScope::Act, nullptr};
        in.net = &net;
        draw(c, in);
        assert(guards());
      }
    // Live over Wi-Fi the footer ends in the mark; over USB it doesn't.
    auto markPx = [&]() {
      int n = 0;
      for (int y = 140; y < 165; y++)
        for (int x = layout::heroW - 40; x < layout::heroW - 4; x++) n += c.px[y * kW + x] == col::dim;
      return n;
    };
    net.phase = NetPhase::Off;
    Input in{&f, running, LinkState::Live, "m", 300, f.serverTime, false, DeckScope::Act, nullptr};
    in.net = &net;
    draw(c, in);
    assert(markPx() > 10);
    NetView usb;
    in.net = &usb;
    draw(c, in);
    assert(markPx() == 0);
  }

  // ---- the prompt view: a waiting hero with scope act, and only then
  {
    Ui ui;
    const Agent& p = f.agents[permission];
    assert(promptView(p, DeckScope::Act) && !promptView(p, DeckScope::View));
    assert(!promptView(f.agents[running], DeckScope::Act));
    Button b[kMaxButtons];
    assert(buttons(p, &ui, b) == 3);
    for (int i = 0; i < 3; i++) {
      assert(b[i].opt == i && b[i].y >= kStripY && b[i].y + b[i].h <= kH);
      assert(b[i].x >= 0 && b[i].x + b[i].w <= kW);
      if (i) assert(b[i].x >= b[i - 1].x + b[i - 1].w);           // side by side, no overlap
    }
    // The strip is orange from edge to edge, and the hero's question sits above it.
    draw(c, {&f, permission, LinkState::Live, "m", 0, f.serverTime, false, DeckScope::Act, &ui});
    assert(c.px[(kStripY + 1) * kW + kW - 8] == col::accent);   // inside the marching frame
    assert(c.px[(kStripY + 1) * kW + 8] == col::accent);
  }

  // ---- six options page through "+N"
  {
    static Fleet six;
    six = f;
    Agent& a = six.agents[permission];
    a.optCount = 6;
    for (int i = 0; i < 6; i++) {
      snprintf(a.opts[i].key, sizeof a.opts[i].key, "%d", i + 1);
      snprintf(a.opts[i].label, sizeof a.opts[i].label, "option number %d with a long label", i + 1);
      a.opts[i].primary = i == 0;
    }
    Ui ui;
    ViewState v;
    pressPin(six, v, 0, permission);
    Button b[kMaxButtons];
    assert(buttons(a, &ui, b) == 3 && b[0].opt == 0 && b[1].opt == 1 && b[2].opt == -1);
    // Tap "+N": the next two.
    const int mx = b[2].x + b[2].w / 2, my = b[2].y + b[2].h / 2;
    DeckCommand cmd = touch(ui, six, v, true, mx, my, DeckScope::Act, 10);
    cmd = touch(ui, six, v, false, 0, 0, DeckScope::Act, 60);
    assert(cmd.cmd == DeckCmd::None);
    assert(buttons(a, &ui, b) == 3 && b[0].opt == 2 && b[1].opt == 3);
    draw(c, {&six, permission, LinkState::Live, "m", 0, six.serverTime, true, DeckScope::Act, &ui});
    assert(guards());
    touch(ui, six, v, true, mx, my, DeckScope::Act, 100);
    touch(ui, six, v, false, 0, 0, DeckScope::Act, 150);
    assert(buttons(a, &ui, b) == 3 && b[0].opt == 4 && b[1].opt == 5);
    touch(ui, six, v, true, mx, my, DeckScope::Act, 200);
    touch(ui, six, v, false, 0, 0, DeckScope::Act, 250);
    assert(buttons(a, &ui, b) == 3 && b[0].opt == 0);              // wraps
    // Another prompt starts from its first option.
    snprintf(a.promptId, sizeof a.promptId, "p-other");
    assert(buttons(a, &ui, b) == 3 && b[0].opt == 0);
  }

  // ---- touch: answer an option
  {
    Ui ui;
    ViewState v;
    pressPin(f, v, 0, permission);
    const Agent& p = f.agents[permission];
    Button b[kMaxButtons];
    buttons(p, &ui, b);
    const int bx = b[2].x + 10, by = b[2].y + 10;                  // "no"
    DeckCommand cmd = touch(ui, f, v, true, bx, by, DeckScope::Act, 1000);
    assert(cmd.cmd == DeckCmd::None && ui.pressed == kHitButton + 2);
    cmd = touch(ui, f, v, true, bx + 3, by + 2, DeckScope::Act, 1030);   // a little wobble is still a tap
    cmd = touch(ui, f, v, false, 0, 0, DeckScope::Act, 1080);
    assert(cmd.cmd == DeckCmd::Answer && !strcmp(cmd.agent, p.id));
    assert(!strcmp(cmd.prompt, p.promptId) && !strcmp(cmd.key, p.opts[2].key));
    assert(!strcmp(ui.toast, "sent"));

    // With view scope the same place is the hero, and nothing is sent.
    Ui vu;
    cmd = touch(vu, f, v, true, bx, 60, DeckScope::View, 2000);
    cmd = touch(vu, f, v, false, 0, 0, DeckScope::View, 2050);
    assert(cmd.cmd == DeckCmd::None && strstr(vu.toast, "scope act"));

    // A press that slides off its button sends nothing.
    Ui su;
    touch(su, f, v, true, bx, by, DeckScope::Act, 3000);
    touch(su, f, v, true, bx, by - 30, DeckScope::Act, 3050);
    cmd = touch(su, f, v, false, 0, 0, DeckScope::Act, 3100);
    assert(cmd.cmd == DeckCmd::None);
  }

  // ---- touch: the agent hides its prompt, then focuses; a hold shows the prompt again
  {
    Ui ui;
    ViewState v;
    pressPin(f, v, 0, permission);
    const Agent& p = f.agents[permission];
    const int lowCell = layout::miniY + 3 * layout::stepY + 10;        // the mini-board's last row
    assert(hitTest(f, permission, &ui, DeckScope::Act, layout::miniX + 10, lowCell) >= kHitButton);   // a button there
    DeckCommand cmd = touch(ui, f, v, true, 40, 40, DeckScope::Act, 100);
    cmd = touch(ui, f, v, false, 0, 0, DeckScope::Act, 150);
    assert(cmd.cmd == DeckCmd::None && !promptShown(p, DeckScope::Act, &ui));   // hidden, nothing sent
    assert(strstr(ui.toast, "hold"));
    assert(hitTest(f, permission, &ui, DeckScope::Act, layout::miniX + 10, lowCell) == 6);  // the whole mini-board
    draw(c, {&f, permission, LinkState::Live, "m", 200, f.serverTime, true, DeckScope::Act, &ui});
    assert(guards());
    // Hidden, the lower button's hold focuses instead of answering.
    assert(key(ui, f, v, Key::Next, true, DeckScope::Act, 210).cmd == DeckCmd::Focus);
    // A tap now focuses, as on any agent.
    cmd = touch(ui, f, v, true, 40, 40, DeckScope::Act, 300);
    cmd = touch(ui, f, v, false, 0, 0, DeckScope::Act, 350);
    assert(cmd.cmd == DeckCmd::Focus && !strcmp(cmd.agent, p.id) && !promptShown(p, DeckScope::Act, &ui));
    // A hold brings the prompt back while the finger is still down; the release sends nothing.
    touch(ui, f, v, true, 40, 40, DeckScope::Act, 400);
    touch(ui, f, v, true, 42, 41, DeckScope::Act, 400 + kTouchHoldMs - 10);
    assert(!promptShown(p, DeckScope::Act, &ui));
    touch(ui, f, v, true, 42, 41, DeckScope::Act, 400 + kTouchHoldMs);
    assert(promptShown(p, DeckScope::Act, &ui));
    assert(touch(ui, f, v, false, 0, 0, DeckScope::Act, 400 + kTouchHoldMs + 50).cmd == DeckCmd::None);
    // A new prompt from the same agent shows by itself.
    touch(ui, f, v, true, 40, 40, DeckScope::Act, 2000);
    touch(ui, f, v, false, 0, 0, DeckScope::Act, 2050);
    static Fleet next;
    next = f;
    snprintf(next.agents[permission].promptId, sizeof next.agents[permission].promptId, "p-new");
    assert(!promptShown(p, DeckScope::Act, &ui) && promptShown(next.agents[permission], DeckScope::Act, &ui));

    pressPin(f, v, 200, running);
    cmd = touch(ui, f, v, true, 120, 140, DeckScope::Act, 300);
    cmd = touch(ui, f, v, false, 0, 0, DeckScope::Act, 350);
    assert(cmd.cmd == DeckCmd::Focus && !strcmp(cmd.agent, f.agents[running].id));
  }

  // ---- touch: mini-board cells and swipes
  {
    Ui ui;
    ViewState v;
    pressPin(f, v, 0, running);
    // Cell 3 (second row, right column) of the full mini-board.
    const int cx = layout::miniX + layout::stepX + 10, cy = layout::miniY + layout::stepY + 10;
    assert(hitTest(f, running, &ui, DeckScope::Act, cx, cy) == 3);
    touch(ui, f, v, true, cx, cy, DeckScope::Act, 100);
    touch(ui, f, v, false, 0, 0, DeckScope::Act, 150);
    assert(resolveHero(f, v, 151) == 3 && v.hold);
    // In the prompt view only the top two rows are cells; below is the strip.
    assert(hitTest(f, permission, &ui, DeckScope::Act, cx, cy) == 3);
    assert(hitTest(f, permission, &ui, DeckScope::Act, cx, layout::miniY + 3 * layout::stepY + 10) != 7);
    // Swipe left: the next agent; right: the one before.
    pressPin(f, v, 200, 2);
    touch(ui, f, v, true, 200, 80, DeckScope::Act, 300);
    touch(ui, f, v, true, 140, 84, DeckScope::Act, 330);
    touch(ui, f, v, true, 100, 86, DeckScope::Act, 360);
    assert(touch(ui, f, v, false, 0, 0, DeckScope::Act, 400).cmd == DeckCmd::None);
    assert(resolveHero(f, v, 401) == 3);
    touch(ui, f, v, true, 60, 80, DeckScope::Act, 500);
    touch(ui, f, v, true, 160, 80, DeckScope::Act, 540);
    touch(ui, f, v, false, 0, 0, DeckScope::Act, 580);
    assert(resolveHero(f, v, 581) == 2);
    // A mostly vertical drag is neither a swipe nor a tap.
    touch(ui, f, v, true, 100, 20, DeckScope::Act, 600);
    touch(ui, f, v, true, 130, 150, DeckScope::Act, 650);
    assert(touch(ui, f, v, false, 0, 0, DeckScope::Act, 700).cmd == DeckCmd::None);
    assert(resolveHero(f, v, 701) == 2);
    // A release with no press before it does nothing.
    Ui idle;
    assert(touch(idle, f, v, false, 0, 0, DeckScope::Act, 800).cmd == DeckCmd::None);
  }

  // ---- buttons
  {
    Ui ui;
    ViewState v;
    pressPin(f, v, 0, 2);
    assert(key(ui, f, v, Key::Next, false, DeckScope::View, 10).cmd == DeckCmd::None);
    assert(resolveHero(f, v, 11) == 3);
    key(ui, f, v, Key::Prev, false, DeckScope::View, 20);
    assert(resolveHero(f, v, 21) == 2);
    // Hold the lower button on a waiting agent: its primary answer.
    pressPin(f, v, 30, permission);
    DeckCommand cmd = key(ui, f, v, Key::Next, true, DeckScope::Act, 40);
    const Agent& p = f.agents[permission];
    assert(cmd.cmd == DeckCmd::Answer && !strcmp(cmd.key, p.opts[primaryOpt(p)].key) && p.opts[primaryOpt(p)].primary);
    // On a working agent: focus. Hold BOOT: interrupt. Neither with view scope.
    pressPin(f, v, 50, running);
    assert(key(ui, f, v, Key::Next, true, DeckScope::Act, 60).cmd == DeckCmd::Focus);
    assert(key(ui, f, v, Key::Prev, true, DeckScope::Act, 70).cmd == DeckCmd::Interrupt);
    assert(key(ui, f, v, Key::Prev, true, DeckScope::View, 80).cmd == DeckCmd::None);
    assert(strstr(ui.toast, "scope act"));
    static Fleet empty;
    ViewState ev;
    assert(key(ui, empty, ev, Key::Next, true, DeckScope::Act, 90).cmd == DeckCmd::None);
  }

  // ---- results
  {
    Ui ui;
    ui.cmdSeq = 7;
    toast(ui, "sent", 0);
    result(ui, 6, 409, 10);                    // not ours
    assert(!strcmp(ui.toast, "sent") && ui.cmdSeq == 7);
    result(ui, 7, 200, 10);
    assert(!strcmp(ui.toast, "sent") && ui.cmdSeq == 0);
    ui.cmdSeq = 8;
    result(ui, 8, 409, 20);
    assert(!strcmp(ui.toast, "the prompt changed") && toastShown(ui, 21));
    ui.cmdSeq = 9;
    result(ui, 9, 403, 30);
    assert(!strcmp(ui.toast, "not allowed: scope"));
    ui.cmdSeq = 10;
    result(ui, 10, 500, 40);
    assert(!strcmp(ui.toast, "failed (500)") && !toastShown(ui, 40 + 2600));
  }

  // ---- motion: the marching frame moves, a new hero slides in from its side
  {
    Ui ui;
    ViewState v;
    pressPin(f, v, 0, running);
    // The frame round a waiting hero changes over time; a working one has none.
    std::vector<uint16_t> a0, a1;
    draw(c, {&f, permission, LinkState::Live, "m", 0, f.serverTime, false, DeckScope::View, &ui});
    a0.assign(c.px, c.px + 3 * kW);                           // the top rows: the frame, no sprite
    draw(c, {&f, permission, LinkState::Live, "m", 3 * kAntsStepMs, f.serverTime, false, DeckScope::View, &ui});
    a1.assign(c.px, c.px + 3 * kW);
    assert(a0 != a1);
    for (int x = 0; x < layout::heroW; x++) {
      const uint16_t p = c.px[x];
      assert(p == col::accent || p == col::bg);               // dashes are dark on orange
    }
    draw(c, {&f, running, LinkState::Live, "m", 0, f.serverTime, false, DeckScope::View, &ui});
    a0.assign(c.px, c.px + kW);
    draw(c, {&f, running, LinkState::Live, "m", 3 * kAntsStepMs, f.serverTime, false, DeckScope::View, &ui});
    a1.assign(c.px, c.px + kW);
    assert(a0 == a1);

    // noteHero: the first hero just appears; a change slides, the way it was asked.
    noteHero(ui, &f, running, 1000);
    assert(slideOffset(&ui, 1000) == 0);
    touch(ui, f, v, true, 200, 80, DeckScope::Act, 1100);     // swipe left: the next agent
    touch(ui, f, v, true, 120, 80, DeckScope::Act, 1130);
    touch(ui, f, v, false, 0, 0, DeckScope::Act, 1160);
    assert(ui.pendingDir == 1);
    const int next = resolveHero(f, v, 1200);
    noteHero(ui, &f, next, 1200);
    assert(ui.pendingDir == 0 && slideOffset(&ui, 1200) == kSlidePx);   // starts a full slide to the right
    const int mid = slideOffset(&ui, 1200 + kSlideMs / 2);
    assert(mid > 0 && mid < kSlidePx / 2);                    // eased: most of the way already
    assert(slideOffset(&ui, 1200 + kSlideMs) == 0);
    // Mid-slide every frame stays inside, hero and prompt alike.
    for (int hero : {running, permission, question})
      for (uint32_t t = 0; t <= kSlideMs; t += 20) {
        noteHero(ui, &f, hero, 5000 + hero * 1000);
        draw(c, {&f, hero, LinkState::Live, "m", 5000 + hero * 1000 + t, f.serverTime, false, DeckScope::Act, &ui});
        assert(guards());
      }
    // BOOT goes back: the hero comes from the left.
    key(ui, f, v, Key::Prev, false, DeckScope::Act, 9000);
    noteHero(ui, &f, resolveHero(f, v, 9000), 9000);
    assert(slideOffset(&ui, 9000) == -kSlidePx);
    // Nothing shown: nothing slides.
    Ui none;
    noteHero(none, &f, 0, 0);
    noteHero(none, nullptr, -1, 10);
    noteHero(none, &f, 1, 20);
    assert(slideOffset(&none, 20) == 0);
  }

  // ---- the toast slides up and back down; a new hero's context bar grows
  {
    Ui ui;
    auto inkRows = [&](int x) {                               // how many of the bottom 30 rows are the toast
      int n = 0;
      for (int y = kH - 30; y < kH; y++) n += c.px[y * kW + x] == col::ink;
      return n;
    };
    toast(ui, "focus", 1000, 1600);
    noteHero(ui, &f, running, 0);
    draw(c, {&f, running, LinkState::Live, "m", 1000, f.serverTime, false, DeckScope::Act, &ui});
    const int start = inkRows(2);
    draw(c, {&f, running, LinkState::Live, "m", 1000 + kToastSlideMs, f.serverTime, false, DeckScope::Act, &ui});
    const int full = inkRows(2);
    draw(c, {&f, running, LinkState::Live, "m", 1000 + 1600 - 20, f.serverTime, false, DeckScope::Act, &ui});
    const int leaving = inkRows(2);
    assert(start == 0 && full == 30 && leaving > 0 && leaving < 30);
    assert(guards());

    // The bar: almost empty as the hero arrives, full width later.
    auto barPx = [&]() {
      int n = 0;
      for (int x = layout::pad; x < layout::pad + 150; x++) n += c.px[103 * kW + x] == col::ink;
      return n;
    };
    Ui bu;
    noteHero(bu, &f, running, 5000);
    draw(c, {&f, running, LinkState::Live, "m", 5000, f.serverTime, false, DeckScope::Act, &bu});
    const int b0 = barPx();
    draw(c, {&f, running, LinkState::Live, "m", 5000 + kBarGrowMs, f.serverTime, false, DeckScope::Act, &bu});
    const int b1 = barPx();
    assert(b0 == 0 && b1 == 150 * ctxPct(f.agents[running]) / 100);
  }

  // ---- hungry: the web's stripes, sliding, on the hero and on its cell
  {
    int h = -1;
    for (int i = 0; i < f.count; i++)
      if (f.agents[i].status == Status::Idle && f.agents[i].unseen) h = i;
    assert(h >= 0);
    Ui ui;
    std::vector<uint16_t> s0, s1;
    draw(c, {&f, h, LinkState::Live, "m", 0, f.serverTime, false, DeckScope::Act, &ui});
    assert(guards());
    const uint16_t tint = blend565(col::bg, col::accent, kStripeAlpha);
    int tinted = 0;
    for (int x = 0; x < layout::heroW; x++) tinted += c.px[160 * kW + x] == tint;   // a row under the text
    assert(tinted > layout::heroW / 4 && tinted < layout::heroW * 3 / 4);         // about half: stripes
    s0.assign(c.px + 160 * kW, c.px + 161 * kW);
    draw(c, {&f, h, LinkState::Live, "m", kStripeStepMs, f.serverTime, false, DeckScope::Act, &ui});
    s1.assign(c.px + 160 * kW, c.px + 161 * kW);
    assert(s0 != s1);                                                              // they slide
    // Its cell, when it's not the hero.
    const int other = h == 0 ? 1 : 0;
    draw(c, {&f, other, LinkState::Live, "m", 0, f.serverTime, false, DeckScope::Act, &ui});
    if (h < 8) {
      const int cx = layout::miniX + (h % 2) * layout::stepX, cy = layout::miniY + (h / 2) * layout::stepY;
      int t = 0;
      for (int x = cx + 2; x < cx + layout::cellW - 2; x++) t += c.px[(cy + 3) * kW + x] == tint;
      assert(t > 0);
    }
  }

  // ---- a press is for what was under the finger: never for its replacement
  {
    ViewState v;
    static Fleet g;
    g = f;
    pressPin(g, v, 0, permission);
    Button b[kMaxButtons];
    Ui ui;
    buttons(g.agents[permission], &ui, b);
    const int bx = b[0].x + 10, by = b[0].y + 10;
    // Answered elsewhere while the finger is down: no options any more. No crash, nothing sent.
    touch(ui, g, v, true, bx, by, DeckScope::Act, 100);
    g.agents[permission].status = Status::Running;
    g.agents[permission].optCount = 0;
    assert(buttons(g.agents[permission], &ui, b) == 0);
    assert(touch(ui, g, v, false, 0, 0, DeckScope::Act, 150).cmd == DeckCmd::None);
    // A new prompt from the same agent under the finger: not answered blind.
    g = f;
    Ui u2;
    touch(u2, g, v, true, bx, by, DeckScope::Act, 200);
    snprintf(g.agents[permission].promptId, sizeof g.agents[permission].promptId, "p-next");
    assert(touch(u2, g, v, false, 0, 0, DeckScope::Act, 250).cmd == DeckCmd::None);
    assert(!strcmp(u2.toast, "the prompt changed"));
    // Another agent became the hero: a tap on "the agent" doesn't focus the new one.
    g = f;
    Ui u3;
    touch(u3, g, v, true, 40, 40, DeckScope::Act, 300);
    pressPin(g, v, 310, running);
    assert(touch(u3, g, v, false, 0, 0, DeckScope::Act, 350).cmd == DeckCmd::None);
  }

  // ---- offline, a waiting hero is the hero (with "deck offline"), not live-looking buttons
  {
    Ui ui;
    draw(c, {&f, permission, LinkState::Lost, "m", 0, f.serverTime, false, DeckScope::Act, &ui});
    int dark = 0;                                             // the strip's dark buttons are gone
    for (int x = 20; x < 100; x++) dark += c.px[(kButtonY + 20) * kW + x] == col::bg;
    assert(dark == 0);
    assert(guards());
  }

  // ---- the "+N" button counts what is left, then goes back
  {
    static Fleet six;
    six = f;
    Agent& a = six.agents[permission];
    a.optCount = 5;
    for (int i = 0; i < 5; i++) {
      snprintf(a.opts[i].key, sizeof a.opts[i].key, "%d", i + 1);
      snprintf(a.opts[i].label, sizeof a.opts[i].label, "o%d", i + 1);
    }
    Ui ui;
    ViewState v;
    pressPin(six, v, 0, permission);
    Button b[kMaxButtons];
    // Page 1: o1 o2 "+3 more"; page 2: o3 o4 "+1 more"; page 3: o5 "back", then page 1 again.
    for (int page = 0; page < 3; page++) {
      assert(optFirstFor(&ui, a) == page * 2);
      const int n = buttons(a, &ui, b);
      assert(b[n - 1].opt == -1 && n == (page < 2 ? 3 : 2));
      draw(c, {&six, permission, LinkState::Live, "m", 0, six.serverTime, false, DeckScope::Act, &ui});
      assert(guards());
      touch(ui, six, v, true, b[n - 1].x + 5, b[n - 1].y + 5, DeckScope::Act, 1000 + page * 100);
      touch(ui, six, v, false, 0, 0, DeckScope::Act, 1050 + page * 100);
    }
    assert(optFirstFor(&ui, a) == 0);
  }

  // ---- no agents: the clock, two walkers, then a nap; the notices move
  {
    static Fleet empty;
    memset(&empty, 0, sizeof empty);
    snprintf(empty.server, sizeof empty.server, "workstation");
    Ui ui;
    noteLink(ui, LinkState::Live, 1000);
    auto clockPx = [&]() {
      int n = 0;
      for (int y = 6; y < 38; y++)
        for (int x = 100; x < 220; x++) n += c.px[y * kW + x] == col::ink;
      return n;
    };
    Input in{&empty, -1, LinkState::Live, "m", 1000, 0, false, DeckScope::Act, &ui};
    in.clockMin = 21 * 60 + 14;
    draw(c, in);
    assert(clockPx() > 50 && guards());
    in.clockMin = -1;
    draw(c, in);
    assert(clockPx() == 0);
    // The walkers move, think, celebrate, and sleep after kNapAfterMs; never off the screen.
    std::vector<uint16_t> f0, f1;
    for (uint32_t t = 0; t < kNapAfterMs + 20000; t += 1700) {
      in.ms = 1000 + t;
      in.clockMin = 600;
      in.clockUtc = t % 2;
      draw(c, in);
      assert(guards());
    }
    in.ms = 1000 + 2000;
    draw(c, in);
    f0.assign(c.px + 60 * kW, c.px + 106 * kW);
    in.ms = 1000 + 4000;
    draw(c, in);
    f1.assign(c.px + 60 * kW, c.px + 106 * kW);
    assert(f0 != f1);                                         // they walked
    assert(busy(in));
    in.ms = 1000 + kNapAfterMs + 10;
    assert(!busy(in));                                        // asleep: the slow clock is enough
    assert(detail::walker(kNapAfterMs, 200, true).pose == Pose::Idle);
    assert(detail::walker(9500, 200, false).pose == Pose::Think);
    assert(detail::walker(11500, 200, false).pose == Pose::Celebrate);
    for (uint32_t t = 0; t < 600000; t += 997) {
      const int x = detail::walker(t, 200, false).x;
      assert(x >= 0 && x <= 200);
    }
    // Connecting: the critter runs across; waiting for approval: it looks around.
    noteLink(ui, LinkState::Connecting, 5000);
    Input cn{nullptr, -1, LinkState::Connecting, "m", 5000, 0, false, DeckScope::View, &ui};
    draw(c, cn);
    f0.assign(c.px, c.px + 70 * kW);
    cn.ms = 6500;
    draw(c, cn);
    f1.assign(c.px, c.px + 70 * kW);
    assert(f0 != f1 && busy(cn) && guards());
    noteLink(ui, LinkState::Unapproved, 7000);
    Input un{nullptr, -1, LinkState::Unapproved, "dev", 7000, 0, false, DeckScope::View, &ui};
    draw(c, un);
    un.ms = 8300;
    draw(c, un);
    assert(guards() && busy(un));
    // Offline with nothing to show: it nods off.
    noteLink(ui, LinkState::Lost, 9000);
    Input lo{nullptr, -1, LinkState::Lost, "dev", 9000, 0, false, DeckScope::View, &ui};
    assert(busy(lo));
    lo.ms = 9000 + kDozeAfterMs + 1;
    draw(c, lo);
    assert(guards() && !busy(lo));
    // The empty screen says how it reaches the deck: "USB", or the Wi-Fi mark.
    auto cornerPx = [&]() {
      int n = 0;
      for (int y = 4; y < 32; y++)
        for (int x = kW - 40; x < kW; x++) n += c.px[y * kW + x] == col::dim;
      return n;
    };
    Input ew{&empty, -1, LinkState::Live, "m", 20000, 0, false, DeckScope::Act, &ui};
    draw(c, ew);
    const int usbPx = cornerPx();
    NetView wifi;
    wifi.via = NetVia::Wifi;
    ew.net = &wifi;
    draw(c, ew);
    assert(usbPx > 10 && cornerPx() > 10 && guards());
    // A toast shows on the empty screen too, across the whole width ("trying Wi-Fi…").
    toast(ui, "trying Wi-Fi\xE2\x80\xA6", 20000);
    Input em{&empty, -1, LinkState::Live, "m", 20000 + kToastSlideMs, 0, false, DeckScope::Act, &ui};
    draw(c, em);
    assert(c.px[(kH - 3) * kW + kW - 5] == col::ink && guards());
    // noteLink keeps its time while the state stays.
    noteLink(ui, LinkState::Lost, 20000);
    assert(ui.linkSince == 9000);
  }

  // ---- busy: idle agents only need the slow clock; work, waiting, stripes or motion want frames
  {
    static Fleet idle;
    idle = f;
    for (int i = 0; i < idle.count; i++) { idle.agents[i].status = Status::Idle; idle.agents[i].unseen = false; }
    Ui ui;
    ui.heroSince = 0;
    Input in{&idle, 0, LinkState::Live, "m", 100000, 0, false, DeckScope::Act, &ui};
    assert(!busy(in));
    idle.agents[3].status = Status::Running;
    assert(busy(in));
    idle.agents[3].status = Status::Idle;
    idle.agents[3].unseen = true;                             // hungry: its stripes slide
    assert(busy(in));
    idle.agents[3].unseen = false;
    toast(ui, "focus", 100000);
    assert(busy(in));
  }

  // ---- long Cyrillic everywhere, prompt and hero, stays inside
  {
    std::string longRu;
    for (int i = 0; i < 40; i++) longRu += "\xD0\x96\xD1\x8B";
    static Fleet ru;
    ru = f;
    Agent& a = ru.agents[permission];
    snprintf(a.title, sizeof a.title, "%s", longRu.c_str());
    copyUtf8(a.waitTitle, sizeof a.waitTitle, longRu.c_str());
    copyUtf8(a.waitDetail, sizeof a.waitDetail, longRu.c_str());
    for (int i = 0; i < a.optCount; i++) copyUtf8(a.opts[i].label, sizeof a.opts[i].label, longRu.c_str());
    Ui ui;
    draw(c, {&ru, permission, LinkState::Live, "m", 0, ru.serverTime, true, DeckScope::Act, &ui});
    draw(c, {&ru, permission, LinkState::Lost, "m", 0, ru.serverTime, true, DeckScope::View, &ui});
    assert(guards());
  }

  printf("tds3: ok\n");
  return 0;
}
