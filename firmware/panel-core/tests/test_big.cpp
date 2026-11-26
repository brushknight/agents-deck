// layout_big.h: the Flutter desk panel at 2/3 for the 4" board. Geometry,
// hit-testing, the card's buttons (stop arms on the first tap), the shared
// prompt layout (choose, then send), and every screen inside the frame.
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_big.h"
#include "layout_square.h"

static Agent mk(const char* id, uint8_t slot, Status st) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.status = st;
  return a;
}

static Fleet fleetOf(std::initializer_list<Agent> as) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (const Agent& a : as) f.agents[f.count++] = a;
  f.total = f.count;
  return f;
}

static DeckCommand tap(const BigSpec& S, PanelUi& ui, const Fleet& f, const Rect& r, DeckScope sc, uint32_t ms) {
  const int x = r.x + r.w / 2, y = r.y + r.h / 2;
  bigTick(ui, f, ms);                                // pages that slid before have settled
  bigTouch(ui, S, f, true, x, y, sc, ms);
  bigTouch(ui, S, f, true, x, y, sc, ms + 40);
  return bigTouch(ui, S, f, false, 0, 0, sc, ms + 60);
}

int main() {
  const BigSpec S = specBig480();

  // ---- the scale: the desk panel's numbers times 2/3
  assert(bk(S, 720) == 480 && bk(S, 8) == 5 && bk(S, 68) == 45 && bk(S, 36) == 24);
  assert(fontPx(*S.title) == 20 && fontPx(*S.heading) == 15 && fontPx(*S.body) == 13);
  assert(18 * S.cardScale == bk(S, 54));                       // the card's critter: 54 px -> 36 px exactly

  // ---- the grid: DeckGrid's padding and gap at 2/3, square cells, bars with more pages
  {
    const BigGrid one = bigGrid(S, 1), two = bigGrid(S, 2);
    assert(one.gap == 5 && one.cell == 113 && one.cellH == 113 && !one.bars.w);
    assert(two.cell == 113 && two.cellH < 113 && two.bars.w == S.w && two.bars.h == bk(S, 34));
    const Rect last = bigCell(one, 15);
    assert(last.x + last.w <= S.w - 5 && last.y + last.h <= S.h - 5);
    for (int cell = 0; cell < 16; cell++) {
      const Rect r = bigCell(two, cell);
      assert(r.y + r.h <= two.bars.y);
    }
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("w", 5, Status::Waiting), mk("z", 20, Status::Running)});
    assert(bigPages(f) == 2);
    PanelUi ui;
    const BigGrid g = bigGrid(S, 2);
    PanelHit h = bigHit(S, ui, f, bigCell(g, 1).x + 10, bigCell(g, 1).y + 10, DeckScope::Act);
    assert(h.kind == Hit::None);                                // a free cell
    h = bigHit(S, ui, f, bigCell(g, 5).x + 10, bigCell(g, 5).y + 10, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 1);
    // A page bar, a swipe, BOOT.
    tap(S, ui, f, Rect{S.w - 20, g.bars.y, 10, g.bars.h}, DeckScope::Act, 0);
    assert(ui.page == 1);
    assert(bigHit(S, ui, f, bigCell(g, 4).x + 5, bigCell(g, 4).y + 5, DeckScope::Act).agent == 2);
    bigTouch(ui, S, f, true, 300, 200, DeckScope::Act, 100);
    bigTouch(ui, S, f, true, 400, 205, DeckScope::Act, 200);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 210);
    assert(ui.page == 0);
    bigButton(ui, f);
    assert(ui.page == 1);
    bigButton(ui, f);
    assert(ui.page == 0);
    // Tap a waiting tile: its prompt, with the primary option chosen.
    f.agents[1].optCount = 2;
    snprintf(f.agents[1].opts[0].key, sizeof f.agents[1].opts[0].key, "1");
    snprintf(f.agents[1].opts[1].key, sizeof f.agents[1].opts[1].key, "2");
    f.agents[1].opts[1].primary = true;
    tap(S, ui, f, bigCell(bigGrid(S, 2), 5), DeckScope::Act, 1000);
    assert(ui.screen == Screen::Prompt && !strcmp(ui.openId, "w") && ui.selected == 1);
    // Swipe right: back.
    bigTouch(ui, S, f, true, 100, 200, DeckScope::Act, 2000);
    bigTouch(ui, S, f, true, 250, 210, DeckScope::Act, 2100);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 2110);
    assert(ui.screen == Screen::Home);
  }

  // ---- the card's buttons, as the desk panel's agent page has them: back, the wide one, then
  // interrupt (running) or stop; by status and scope
  {
    using namespace big_ui;
    Agent run = mk("r", 0, Status::Running), idle = mk("i", 1, Status::Idle), ex = mk("x", 2, Status::Exited);
    const int foot = S.h - kPadBot, right = S.w - kPadX;
    BigButtons b = bigCardButtons(S, run, DeckScope::View);
    assert(b.n == 1 && b.kind[0] == Hit::Back && b.r[0].x == kPadX && b.r[0].w == kBtnH && b.r[0].y + b.r[0].h == foot);   // view scope: only back
    b = bigCardButtons(S, run, DeckScope::Act);
    assert(b.n == 3 && b.kind[0] == Hit::Back && b.kind[1] == Hit::Focus && b.kind[2] == Hit::Interrupt);
    assert(b.r[1].x == kPadX + kBtnH + kBtnGap && b.r[1].x + b.r[1].w + kBtnGap == b.r[2].x);
    assert(b.r[2].w == kSideW && b.r[2].x + b.r[2].w == right && b.r[1].h == kBtnH);
    b = bigCardButtons(S, run, DeckScope::Admin);
    assert(b.n == 3 && b.kind[2] == Hit::Interrupt);                                    // no stop while it runs
    b = bigCardButtons(S, idle, DeckScope::Act);
    assert(b.n == 2 && b.kind[1] == Hit::Focus && b.r[1].x + b.r[1].w == right);         // stop needs admin
    b = bigCardButtons(S, idle, DeckScope::Admin);
    assert(b.n == 3 && b.kind[1] == Hit::Focus && b.kind[2] == Hit::Stop);
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 2 && b.kind[1] == Hit::Stop && b.r[1].x + b.r[1].w == right);         // remove, wide
    ex.resumable = true;
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 3 && b.kind[1] == Hit::Resume && b.kind[2] == Hit::Stop);
    {   // back: a tap goes home
      Fleet f = fleetOf({run});
      PanelUi ui;
      bigOpen(ui, f, 0);
      b = bigCardButtons(S, f.agents[0], DeckScope::View);
      const DeckCommand c = tap(S, ui, f, b.r[0], DeckScope::View, 0);
      assert(c.cmd == DeckCmd::None && ui.screen == Screen::Home);
    }
    // The card's parts stack without overlap, from the head down to the buttons.
    for (Agent* a : {&run, &idle, &ex}) {
      for (int subs : {0, 3}) {
        a->subCount = (uint8_t)subs;
        const BigCard g = bigCard(S, *a, DeckScope::Admin);
        assert(g.head.chipY >= kPadTop && g.head.nameY > g.head.chipY + kChipH && g.head.sumY + kLineS <= g.head.bottom);
        assert(g.nowY >= g.head.bottom && g.lastY > g.nowY && (g.term.h == 0 || g.term.y + g.term.h < g.lastY));
        assert(g.lastLines >= 2 && g.lastLines <= (subs ? 3 : 5) && g.textY + g.lastLines * kLineM <= g.ctxY);
        assert(g.ctxY + kLineXS <= g.whereY && g.whereY + kLineXS <= bigCardButtons(S, *a, DeckScope::Admin).r[0].y);
        assert(g.head.textX + g.head.textW == right);
      }
      a->subCount = 0;
    }

    // Stop: the first tap arms it ("again"), the second sends Dismiss; armed,
    // it lapses after 3 s.
    Fleet f = fleetOf({idle});
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Card);
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    DeckCommand c = tap(S, ui, f, b.r[2], DeckScope::Admin, 0);
    assert(c.cmd == DeckCmd::None && ui.stopArmedUntil == 60 + kStopArmMs);
    c = tap(S, ui, f, b.r[2], DeckScope::Admin, 1000);
    assert(c.cmd == DeckCmd::Dismiss && !strcmp(c.agent, "i") && ui.stopArmedUntil == 0);
    tap(S, ui, f, b.r[2], DeckScope::Admin, 5000);
    bigTick(ui, f, 5000 + 60 + kStopArmMs + 1);
    assert(ui.stopArmedUntil == 0);
    c = tap(S, ui, f, b.r[2], DeckScope::Admin, 9000);
    assert(c.cmd == DeckCmd::None);                                                  // armed again, not sent
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 9500);
    assert(c.cmd == DeckCmd::Focus);
    // Interrupt while running.
    f.agents[0].status = Status::Running;
    b = bigCardButtons(S, f.agents[0], DeckScope::Act);
    c = tap(S, ui, f, b.r[2], DeckScope::Act, 10000);
    assert(c.cmd == DeckCmd::Interrupt);
    // Resume an exited agent.
    f.agents[0].status = Status::Exited;
    f.agents[0].resumable = true;
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 11000);
    assert(c.cmd == DeckCmd::Resume);
    c = tap(S, ui, f, b.r[2], DeckScope::Admin, 12000);                              // remove: one tap
    assert(c.cmd == DeckCmd::Dismiss);

    // An egg ready to hatch: its box says "tap to hatch"; a tap asks the deck
    // (HatchPet) or, on a Deck Link without it, says where to hatch it.
    f.agents[0].status = Status::Idle;
    f.agents[0].pet.has = true;
    f.agents[0].pet.ready = f.agents[0].pet.cracked = true;
    const BigHead g = bigCard(S, f.agents[0], DeckScope::Act).head;
    assert(g.hatch && g.boxed && g.zone.w == kBoxW && g.zone.h == kBoxH);
    assert(bigHit(S, ui, f, g.zone.x + 5, g.zone.y + 5, DeckScope::View).kind == Hit::None);   // view scope: no
    c = tap(S, ui, f, g.zone, DeckScope::Act, 13000);
    if (kCanHatch) assert(c.cmd == DeckCmd::Hatch && !strcmp(c.agent, "i"));
    else assert(c.cmd == DeckCmd::None && strstr(ui.toast, "hatch"));
    f.agents[0].pet.ready = false;                                                   // not ready: no box, no hatch
    assert(!bigCard(S, f.agents[0], DeckScope::Act).head.hatch);
    assert(bigHit(S, ui, f, g.zone.x + 5, g.zone.y + 5, DeckScope::Act).kind == Hit::None);
  }

  // ---- the prompt: a permission on orange, a question dark with an orange edge; tap an answer to
  // choose it, tap it again (or send) to send
  {
    using namespace big_ui;
    Fleet f = fleetOf({mk("w", 0, Status::Waiting)});
    Agent& w = f.agents[0];
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    snprintf(w.waitTitle, sizeof w.waitTitle, "which session strategy should the login flow use?");
    snprintf(w.promptId, sizeof w.promptId, "p9");
    w.optCount = 3;
    for (int i = 0; i < 3; i++) {
      snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
      snprintf(w.opts[i].label, sizeof w.opts[i].label, "option %d \xE2\x80\x94 hint %d", i + 1, i + 1);
    }
    auto btn = [](const BigPrompt& p, Hit k) {
      for (int i = 0; i < p.btn.n; i++)
        if (p.btn.kind[i] == k) return p.btn.r[i];
      return Rect{-100, -100, 1, 1};
    };
    for (const char* kind : {"question", "permission"}) {
      snprintf(w.waitKind, sizeof w.waitKind, "%s", kind);
      const bool perm = !strcmp(kind, "permission");
      snprintf(w.waitDetail, sizeof w.waitDetail, "%s", perm ? "terraform apply" : "");
      const BigPrompt p = bigPrompt(S, w);
      assert(p.orange == perm && p.head.boxed && p.n == 3 && p.titleLines == 2 && (p.term.h == 0) == !perm);
      assert(p.titleY > p.askY && p.askY >= p.head.bottom);
      if (p.term.h) assert(p.term.y >= p.titleY + 2 * kLineQ && p.term.y + p.term.h <= p.opt[0].y);
      for (int i = 0; i < 3; i++) assert(p.opt[i].h >= kLineL + 4 && (i == 0 || p.opt[i].y >= p.opt[i - 1].y + p.opt[i - 1].h));
      assert(p.sub == !perm && p.opt[2].y + p.opt[2].h <= p.btn.r[0].y);
      assert(p.btn.kind[0] == Hit::Back && p.btn.r[0].x == kPadX && p.btn.r[0].y + p.btn.r[0].h == S.h - kPadBot);
      assert(btn(p, Hit::Focus).x + btn(p, Hit::Focus).w == S.w - kPadX && (btn(p, Hit::Send).x >= 0) == !perm);
    }
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    w.waitDetail[0] = '\0';
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Prompt && ui.selected == -1);    // no primary: nothing chosen
    BigPrompt p = bigPrompt(S, w);
    DeckCommand c = tap(S, ui, f, btn(p, Hit::Send), DeckScope::Act, 50);
    assert(c.cmd == DeckCmd::None);                              // send: nothing chosen yet
    c = tap(S, ui, f, p.opt[2], DeckScope::Act, 100);
    assert(c.cmd == DeckCmd::None && ui.selected == 2);          // chosen
    c = tap(S, ui, f, p.opt[2], DeckScope::View, 200);           // view scope: a note, nothing sent
    assert(c.cmd == DeckCmd::None && strstr(ui.toast, "view only"));
    c = tap(S, ui, f, p.opt[2], DeckScope::Act, 300);            // again: sent
    assert(c.cmd == DeckCmd::Answer && !strcmp(c.prompt, "p9") && !strcmp(c.key, "3"));
    assert(ui.backAfterToast);
    assert(tap(S, ui, f, p.opt[2], DeckScope::Act, 400).cmd == DeckCmd::None);   // once
    bigTick(ui, f, 2000);
    assert(ui.screen == Screen::Home);
    // Send sends the chosen one; a refused answer keeps the prompt; focus focuses; back goes home.
    bigOpen(ui, f, 0);
    tap(S, ui, f, p.opt[0], DeckScope::Act, 3000);
    c = tap(S, ui, f, btn(p, Hit::Send), DeckScope::Act, 3100);
    assert(c.cmd == DeckCmd::Answer && !strcmp(c.key, "1"));
    ui.cmdSeq = 5;
    bigResult(ui, 5, 409, 3200);
    assert(!ui.backAfterToast);
    bigTick(ui, f, 9000);
    assert(ui.screen == Screen::Prompt);
    c = tap(S, ui, f, btn(p, Hit::Focus), DeckScope::Act, 9100);
    assert(c.cmd == DeckCmd::Focus);
    tap(S, ui, f, p.btn.r[0], DeckScope::Act, 9200);
    assert(ui.screen == Screen::Home);
    // Six long answers still fit above the buttons (thinner rows, one line each).
    w.optCount = 6;
    for (int i = 3; i < 6; i++) snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
    p = bigPrompt(S, w);
    assert(p.n == 6 && p.opt[0].y > p.titleY && p.opt[5].y + p.opt[5].h <= p.btn.r[0].y);
    for (int i = 0; i < 6; i++) assert(p.opt[i].h >= kLineL + 4);
    // Free text: no answers, focus terminal is the wide button.
    w.optCount = 0;
    p = bigPrompt(S, w);
    assert(p.btn.n == 2 && p.btn.kind[1] == Hit::Focus && p.btn.r[1].x + p.btn.r[1].w == S.w - kPadX);
  }

  // ---- the last prompt, or the last answer once it has finished; the account
  {
    agentsdeck_link_v1_Agent in = agentsdeck_link_v1_Agent_init_zero;
    snprintf(in.id, sizeof in.id, "a1");
    snprintf(in.last_prompt, sizeof in.last_prompt, "fix it");
    snprintf(in.last_answer, sizeof in.last_answer, "fixed it");
    in.last_answer_at_ms = 1700000000000;
    in.last_active_ms = 1700000100000;
    in.status = agentsdeck_link_v1_Status_STATUS_RUNNING;
    Agent a;
    agentFromLink(in, a);
    assert(!a.lastIsAnswer && !strcmp(a.lastPrompt, "fix it") && a.lastAt == 1700000100 && !a.account[0]);
    in.status = agentsdeck_link_v1_Status_STATUS_IDLE;
    in.has_account = true;
    snprintf(in.account.name, sizeof in.account.name, "personal");
    snprintf(in.account.color, sizeof in.account.color, "teal");
    agentFromLink(in, a);
    assert(a.lastIsAnswer && !strcmp(a.lastPrompt, "fixed it") && a.lastAt == 1700000000);
    assert(!strcmp(a.account, "personal") && a.accountColor == 0);
    in.last_answer[0] = '\0';                                     // no answer yet: the prompt
    agentFromLink(in, a);
    assert(!a.lastIsAnswer && !strcmp(a.lastPrompt, "fix it"));
  }

  // ---- a pet fits its tile: all it can reach in its pose, over its motion, inside the tile
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    const BigGrid g = bigGrid(S, 1);
    for (int stage = 0; stage < 7; stage++) {
      for (Pose pose : {Pose::Idle, Pose::Wait, Pose::Bash, Pose::Celebrate, Pose::Hungry, Pose::Delegate}) {
        Agent a = d.fleet.agents[0];
        a.pet.has = true;
        a.pet.stage = (uint8_t)stage;
        a.pet.level = (uint8_t)(stage * 8);
        a.hat = 3;
        a.pet.hat = stage >= 2 ? 2 : 0;
        const big_detail::PetExtent e = big_detail::petExtent(a, pose, true);
        const Rect cell = bigCell(g, 0);
        const float u = cell.w / 170.f;
        const int bw = (int)lroundf(158 * u), bh = (int)lroundf(96 * u);
        int s = S.tileScale;
        if (bw / (e.x1 - e.x0) < s) s = bw / (e.x1 - e.x0);
        if (bh / (e.y1 - e.y0) < s) s = bh / (e.y1 - e.y0);
        assert(s >= 1 && (e.y1 - e.y0) * s <= bh && (e.x1 - e.x0) * s <= bw);
      }
    }
  }

  // ---- every screen at every scope and link state stays inside the frame
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    const BigPanel P(S);
    const int PAD = 2 * S.w;
    const uint16_t GUARD = 0xDEAD;
    std::vector<uint16_t> mem(S.w * S.h + 2 * PAD, GUARD);
    Canvas c{mem.data() + PAD, S.w, S.h};
    auto guards = [&] {
      for (int i = 0; i < PAD; i++)
        if (mem[i] != GUARD || mem[PAD + S.w * S.h + i] != GUARD) return false;
      return true;
    };
    for (LinkState ls : {LinkState::Connecting, LinkState::Unapproved, LinkState::Live, LinkState::Lost})
      for (DeckScope sc : {DeckScope::View, DeckScope::Act, DeckScope::Admin}) {
        PanelUi ui;
        PanelInput in{&d.fleet, ls, "waveshare-touch-a-very-long-device-name-0123456789", sc, 300, d.fleet.serverTime + 60,
                      &ui, 125};
        P.draw(c, in);
        assert(guards());
        in.fleet = nullptr;
        P.draw(c, in);
        assert(guards());
        in.fleet = &d.fleet;
        for (int i = 0; i < d.fleet.count; i++) {
          bigOpen(ui, d.fleet, i);
          for (Screen s : {Screen::Card, Screen::Prompt}) {
            ui.screen = s;
            ui.stopArmedUntil = 1000;
            for (uint32_t ms : {0u, 450u}) {
              in.ms = ms;
              P.draw(c, in);
              assert(guards());
            }
          }
        }
      }
  }
  // ---- drawn in strips (grid_app.h, a board with stripRows): the same picture
  // as drawn whole, and nothing written outside the strip
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    const BigPanel P(S);
    std::vector<uint16_t> whole(S.w * S.h);
    for (int n : {24, 7}) {                          // strips that divide the frame, and some that don't
      const int PAD = 2 * S.w;
      const uint16_t GUARD = 0xBEEF;
      std::vector<uint16_t> mem(S.w * n + 2 * PAD);
      std::vector<uint16_t> joined(S.w * S.h);
      auto check = [&](const PanelInput& in) {
        Canvas cw{whole.data(), S.w, S.h};
        P.draw(cw, in);
        for (int y0 = 0; y0 < S.h; y0 += n) {
          const int y1 = y0 + n < S.h ? y0 + n : S.h;
          std::fill(mem.begin(), mem.end(), GUARD);
          uint16_t* strip = mem.data() + PAD;
          Canvas sc{strip - (ptrdiff_t)y0 * S.w, S.w, y1};
          sc.top = y0;
          P.draw(sc, in);
          for (int i = 0; i < PAD; i++) assert(mem[i] == GUARD && mem[PAD + S.w * n + i] == GUARD);
          memcpy(joined.data() + (size_t)y0 * S.w, strip, (size_t)(y1 - y0) * S.w * 2);
        }
        assert(memcmp(whole.data(), joined.data(), whole.size() * 2) == 0);
      };
      PanelUi ui;
      PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Admin, 450, d.fleet.serverTime + 60, &ui, 0};
      check(in);                                     // home, hungry stripes and all
      in.link = LinkState::Lost;
      check(in);
      in.link = LinkState::Live;
      for (int i = 0; i < d.fleet.count; i++) {
        bigOpen(ui, d.fleet, i);
        for (Screen s : {Screen::Card, Screen::Prompt}) {
          ui.screen = s;
          check(in);
        }
      }
      ui = PanelUi();
      in.link = LinkState::Connecting;
      check(in);
    }
  }
  // ---- the stripes come out as the per-pixel rule says (pixel xx, yy is lit
  // where (xx + h - yy - shift) mod period < period / 2), clipped to the canvas
  {
    const Theme T = deckTheme(false);
    for (uint32_t ms : {0u, 100u, 350u})
      for (int period : {16, 17}) {
        std::vector<uint16_t> a(S.w * S.h, 0), b(S.w * S.h, 0);
        Canvas ca{a.data(), S.w, S.h};
        const Rect r{-5, S.h - 70, 113, 110};          // past the left and bottom edges
        big_detail::stripes(ca, r, T.panel, T.accent, ms, period);
        const uint16_t s = blend565(T.panel, T.accent, 2);
        const int shift = (int)((ms / 100) % 6) * period / 6;
        for (int yy = 0; yy < r.h; yy++)
          for (int xx = 0; xx < r.w; xx++) {
            const int u = ((xx + (r.h - yy) - shift) % period + period) % period;
            const int px = r.x + xx, py = r.y + yy;
            if (u < period / 2 && px >= 0 && px < S.w && py >= 0 && py < S.h) b[py * S.w + px] = s;
          }
        assert(a == b);
      }
  }
  // ---- rounded boxes: each corner as fillArc draws its quarter, pixel for pixel
  {
    const Theme T = deckTheme(false);
    std::vector<uint16_t> a(S.w * S.h), b(S.w * S.h);
    for (int rad : {0, 1, 3, 4, 9, 14, 30})
      for (int t : {1, 2, 5})
        for (const Rect& r : {Rect{10, 10, 100, 60}, Rect{-7, 400, 61, 120}, Rect{300, -3, 33, 21}}) {
          std::fill(a.begin(), a.end(), T.bg);
          std::fill(b.begin(), b.end(), T.bg);
          Canvas ca{a.data(), S.w, S.h}, cb{b.data(), S.w, S.h};
          big_detail::rrect(ca, r, rad, T.accent);
          big_detail::rframe(ca, Rect{r.x + 120, r.y, r.w, r.h}, rad, t, T.ink);
          // The old way: fillArc over each corner's 90 degrees.
          auto oldRrect = [&](const Rect& q, int rd, uint16_t col) {
            if (rd * 2 > q.h) rd = q.h / 2;
            if (rd * 2 > q.w) rd = q.w / 2;
            if (rd <= 0) { cb.rect(q.x, q.y, q.w, q.h, col); return; }
            cb.rect(q.x + rd, q.y, q.w - 2 * rd, q.h, col);
            cb.rect(q.x, q.y + rd, rd, q.h - 2 * rd, col);
            cb.rect(q.x + q.w - rd, q.y + rd, rd, q.h - 2 * rd, col);
            fillArc(cb, q.x + rd, q.y + rd, 0, rd, 270, 90, col);
            fillArc(cb, q.x + q.w - rd, q.y + rd, 0, rd, 0, 90, col);
            fillArc(cb, q.x + q.w - rd, q.y + q.h - rd, 0, rd, 90, 90, col);
            fillArc(cb, q.x + rd, q.y + q.h - rd, 0, rd, 180, 90, col);
          };
          auto oldRframe = [&](const Rect& q, int rd, int tt, uint16_t col) {
            if (rd * 2 > q.h) rd = q.h / 2;
            cb.rect(q.x + rd, q.y, q.w - 2 * rd, tt, col);
            cb.rect(q.x + rd, q.y + q.h - tt, q.w - 2 * rd, tt, col);
            cb.rect(q.x, q.y + rd, tt, q.h - 2 * rd, col);
            cb.rect(q.x + q.w - tt, q.y + rd, tt, q.h - 2 * rd, col);
            fillArc(cb, q.x + rd, q.y + rd, rd - tt, rd, 270, 90, col);
            fillArc(cb, q.x + q.w - rd, q.y + rd, rd - tt, rd, 0, 90, col);
            fillArc(cb, q.x + q.w - rd, q.y + q.h - rd, rd - tt, rd, 90, 90, col);
            fillArc(cb, q.x + rd, q.y + q.h - rd, rd - tt, rd, 180, 90, col);
          };
          oldRrect(r, rad, T.accent);
          oldRframe(Rect{r.x + 120, r.y, r.w, r.h}, rad, t, T.ink);
          assert(a == b);
        }
  }
  // ---- the 4"'s card is the big panel's (the desk panel's detail, its "focus terminal" button), not
  // the small boards' card that a 480 x 480 SquareSpec would draw
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> big(S.w * S.h), small(S.w * S.h);
    Canvas cb{big.data(), S.w, S.h}, cs{small.data(), S.w, S.h};
    PanelUi ui;
    const int i = findAgent(d.fleet, "k3f9a2");
    assert(i >= 0);
    bigOpen(ui, d.fleet, i);
    PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0};
    const BigPanel B(S);
    B.draw(cb, in);
    SquarePanel(specLcd4()).draw(cs, in);
    const BigButtons b = bigCardButtons(S, d.fleet.agents[i], DeckScope::Act);
    assert(b.n == 3 && b.kind[0] == Hit::Back && b.kind[1] == Hit::Focus);
    const Theme T = deckTheme(false);
    // The focus button's face, beside its label, is filled; the small card has its round buttons elsewhere.
    const uint16_t face = big[(b.r[1].y + 6) * S.w + b.r[1].x + 12];
    assert(face != T.bg && face == big[(b.r[1].y + b.r[1].h - 8) * S.w + b.r[1].x + b.r[1].w - 14]);
    assert(memcmp(big.data(), small.data(), big.size() * 2) != 0);
    assert(B.width() == 480 && B.height() == 480);
  }

  // ---- on Wi-Fi: the bars' row stays with one page (the mark's), hits follow the drawing, every notice fits
  {
    const BigGrid w = bigGrid(S, 1, true);
    assert(w.bars.w == S.w && w.bars.h == bk(S, 34) && w.cell == w.cellH);
    for (int cell = 0; cell < 16; cell++) assert(bigCell(w, cell).y + bigCell(w, cell).h <= w.bars.y);
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("b", 15, Status::Running)});
    PanelUi ui;
    ui.wifi = true;
    PanelHit h = bigHit(S, ui, f, bigCell(w, 15).x + 3, bigCell(w, 15).y + 3, DeckScope::Act);
    assert(h.kind == Hit::Tile && h.agent == 1);
    h = bigHit(S, ui, f, S.w / 2, w.bars.y + w.bars.h / 2, DeckScope::Act);
    assert(h.kind == Hit::None);                                 // no page bars with one page
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d));
    std::vector<uint16_t> buf(S.w * S.h);
    Canvas c{buf.data(), S.w, S.h};
    NetView net;
    net.via = NetVia::Wifi;
    snprintf(net.ssid, sizeof net.ssid, "%s", "a-network-name-of-32-characters!");
    PanelInput in{&d.fleet, LinkState::Live, "x", DeckScope::Act, 0, d.fleet.serverTime, &ui, 0, &net};
    BigPanel(S).draw(c, in);
    uiToast(ui, "paired for Wi-Fi", 0);
    in.link = LinkState::Connecting;
    for (NetPhase ph : {NetPhase::Joining, NetPhase::Connecting, NetPhase::WrongPassword, NetPhase::NoNetwork,
                        NetPhase::WifiFailed, NetPhase::DeckUnreachable, NetPhase::CertRejected}) {
      net.phase = ph;
      BigPanel(S).draw(c, in);
    }
  }
  // ---- the "Needs you" screen: one step left of the first page, a page of
  // spots, the mark at the left of the bars; taps there act as on any page
  {
    static Attention att;
    att = Attention();
    Fleet f = fleetOf({mk("a", 0, Status::Idle), mk("w", 5, Status::Waiting), mk("r", 9, Status::Running),
                       mk("z", 20, Status::Running)});
    f.agents[1].attnSlot = 1, f.agents[2].attnSlot = 2, f.agents[3].attnSlot = 3;   // the deck's spots
    PanelUi ui;
    ui.att = &att;
    bigTick(ui, f, 0);
    assert(att.cap == 16 && att.count == 3);
    assert(!ui.needsYou && ui.page == 0);                         // it boots on the first page
    // One page and the screen: the bars' row stays, and its cells match a page's.
    Fleet one = fleetOf({mk("a", 0, Status::Running)});
    const BigGrid g1 = bigGridFor(S, ui, bigPages(one));
    assert(g1.bars.w == S.w && g1.cell == g1.cellH);
    const BigBars b1 = bigBars(S, g1, ui, 1);
    assert(b1.n == 2 && b1.mark && b1.r[0].x + b1.r[0].w < b1.r[1].x);
    // Swipe right on the first page: the screen, spot n in cell n.
    const BigGrid g = bigGridFor(S, ui, 2);
    bigTouch(ui, S, f, true, 100, 200, DeckScope::Act, 100);
    bigTouch(ui, S, f, true, 250, 205, DeckScope::Act, 150);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 160);
    assert(ui.needsYou && ui.screen == Screen::Home);
    assert(bigHit(S, ui, f, bigCell(g, 0).x + 5, bigCell(g, 0).y + 5, DeckScope::Act).agent == 1);   // w
    assert(bigHit(S, ui, f, bigCell(g, 1).x + 5, bigCell(g, 1).y + 5, DeckScope::Act).agent == 2);   // r
    assert(bigHit(S, ui, f, bigCell(g, 2).x + 5, bigCell(g, 2).y + 5, DeckScope::Act).agent == 3);   // z (page 2)
    assert(bigHit(S, ui, f, bigCell(g, 3).x + 5, bigCell(g, 3).y + 5, DeckScope::Act).kind == Hit::None);
    // Further right: nothing.
    bigStep(ui, f, false);
    assert(ui.needsYou);
    // Up, or a diagonal that goes mostly up: not a page change (only |dx| > 1.3 |dy| is).
    bigTick(ui, f, 500);                                          // (the slide there has settled)
    bigTouch(ui, S, f, true, 200, 400, DeckScope::Act, 570);
    bigTouch(ui, S, f, true, 260, 300, DeckScope::Act, 580);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 590);
    assert(ui.needsYou && !ui.slideAnim);
    bigTouch(ui, S, f, true, 300, 400, DeckScope::Act, 591);
    bigTouch(ui, S, f, true, 220, 330, DeckScope::Act, 595);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 599);
    assert(ui.needsYou && !ui.slideAnim);
    // A tile there opens its prompt, and back is the screen again.
    tap(S, ui, f, bigCell(g, 0), DeckScope::Act, 600);
    assert(ui.screen == Screen::Prompt && !strcmp(ui.openId, "w"));
    bigButton(ui, f);
    assert(ui.screen == Screen::Home && ui.needsYou);
    // r stops and the deck lets it go: its cell is free, nobody moves up.
    f.agents[2].status = Status::Idle;
    f.agents[2].attnSlot = 0;
    bigTick(ui, f, 1000);
    assert(attAgent(att, f, 1) == -1 && attAgent(att, f, 2) == 3);
    // The deck puts z in spot 16: the last cell.
    f.agents[3].attnSlot = 16;
    bigTick(ui, f, 1100);
    assert(attAgent(att, f, 2) == -1 && attAgent(att, f, 15) == 3);
    f.agents[3].attnSlot = 3;
    bigTick(ui, f, 1200);
    // The mark: here, the widest; tap a page bar to leave, tap the mark to come back.
    BigBars b = bigBars(S, g, ui, 2);
    assert(b.n == 3 && b.r[0].w == bk(S, 56) && b.r[1].w == bk(S, 30) && b.countX < 0);
    tap(S, ui, f, Rect{b.r[2].x, g.bars.y, b.r[2].w, g.bars.h}, DeckScope::Act, 7000);
    assert(!ui.needsYou && ui.page == 1);
    b = bigBars(S, g, ui, 2);
    assert(b.countX >= 0 && b.r[0].w == bk(S, 30));               // the count before the mark, off the screen
    assert(bigHit(S, ui, f, b.r[0].x + b.r[0].w / 2, g.bars.y + 3, DeckScope::Act).kind == Hit::NeedsYou);
    tap(S, ui, f, Rect{b.r[0].x, g.bars.y, b.r[0].w, g.bars.h}, DeckScope::Act, 7100);
    assert(ui.needsYou);
    // Swipe left: the first page, never further.
    bigTouch(ui, S, f, true, 300, 200, DeckScope::Act, 7200);
    bigTouch(ui, S, f, true, 150, 205, DeckScope::Act, 7250);
    bigTouch(ui, S, f, false, 0, 0, DeckScope::Act, 7260);
    assert(!ui.needsYou && ui.page == 0);
    // BOOT goes round: page 1, page 2, the screen, page 1.
    bigButton(ui, f);
    assert(!ui.needsYou && ui.page == 1);
    bigButton(ui, f);
    assert(ui.needsYou);
    bigButton(ui, f);
    assert(!ui.needsYou && ui.page == 0);
    // Drawn: the screen's cells as a page's, the mark solid orange; the first page with the outline.
    std::vector<uint16_t> buf(S.w * S.h);
    Canvas c{buf.data(), S.w, S.h};
    const Theme T = deckTheme(false);
    PanelInput in{&f, LinkState::Live, "x", DeckScope::Act, 8000, 0, &ui, 0};
    bigTick(ui, f, 8000);                                         // at rest
    ui.needsYou = true;
    BigPanel(S).draw(c, in);
    b = bigBars(S, g, ui, 2);
    assert(buf[(b.r[0].y + b.r[0].h / 2) * S.w + b.r[0].x + b.r[0].w / 2] == T.accent);
    assert(buf[(b.r[1].y + b.r[1].h / 2) * S.w + b.r[1].x + b.r[1].w / 2] != T.ink);   // no page is current
    const Rect free3 = bigCell(g, 3);
    assert(buf[(free3.y + free3.h / 2) * S.w + free3.x + free3.w / 2] == T.emptyFill);
    ui.needsYou = false;
    BigPanel(S).draw(c, in);
    b = bigBars(S, g, ui, 2);
    assert(buf[(b.r[0].y + b.r[0].h / 2) * S.w + b.r[0].x + b.r[0].w / 2] == T.bg);    // an outline
    assert(buf[(b.r[0].y + b.r[0].h / 2) * S.w + b.r[0].x] == T.accent);               // in orange: someone is there
    assert(buf[(b.r[1].y + b.r[1].h / 2) * S.w + b.r[1].x + b.r[1].w / 2] == T.ink);    // page 1 is current
    // Without the screen (no Attention), nothing changes: no mark, no row with one page.
    PanelUi plain;
    assert(!bigBars(S, bigGridFor(S, plain, 1), plain, 1).n && !bigGridFor(S, plain, 1).bars.w);
    plain.needsYou = true;
    bigTick(plain, f, 0);
    assert(!plain.needsYou);
  }
  printf("ok  big layout\n");
  return 0;
}
