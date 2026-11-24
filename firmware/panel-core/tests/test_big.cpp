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

  // ---- the card's buttons: squares in a centred row at the foot, by status and scope
  {
    Agent run = mk("r", 0, Status::Running), idle = mk("i", 1, Status::Idle), ex = mk("x", 2, Status::Exited);
    BigButtons b = bigCardButtons(S, run, DeckScope::View);
    assert(b.n == 1 && b.kind[0] == Hit::Back && b.r[0].x == (S.w - big_ui::kAct) / 2);   // view scope: only back
    b = bigCardButtons(S, run, DeckScope::Act);
    assert(b.n == 3 && b.kind[0] == Hit::Focus && b.kind[1] == Hit::Interrupt && b.kind[2] == Hit::Back);
    assert(b.r[0].w == big_ui::kAct && b.r[0].h == big_ui::kAct && b.r[1].x == b.r[0].x + big_ui::kAct + big_ui::kActGap);
    assert(b.r[0].y + b.r[0].h == S.h - big_ui::kM && b.r[0].x - 0 == S.w - (b.r[2].x + b.r[2].w));   // centred
    b = bigCardButtons(S, run, DeckScope::Admin);
    assert(b.n == 4 && b.kind[2] == Hit::Stop && b.kind[3] == Hit::Back && b.r[0].x >= big_ui::kM);
    b = bigCardButtons(S, idle, DeckScope::Act);
    assert(b.n == 2 && b.kind[0] == Hit::Focus && b.kind[1] == Hit::Back);   // stop needs admin
    b = bigCardButtons(S, idle, DeckScope::Admin);
    assert(b.n == 3 && b.kind[1] == Hit::Stop);
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 2 && b.kind[0] == Hit::Stop && b.kind[1] == Hit::Back);             // remove
    ex.resumable = true;
    b = bigCardButtons(S, ex, DeckScope::Admin);
    assert(b.n == 3 && b.kind[0] == Hit::Resume && b.kind[1] == Hit::Stop);
    {   // back: a tap goes home
      Fleet f = fleetOf({run});
      PanelUi ui;
      bigOpen(ui, f, 0);
      b = bigCardButtons(S, f.agents[0], DeckScope::View);
      const DeckCommand c = tap(S, ui, f, b.r[0], DeckScope::View, 0);
      assert(c.cmd == DeckCmd::None && ui.screen == Screen::Home);
    }
    // The card's parts stack without overlap: the column ends on the zone's foot.
    for (const Agent* a : {&run, &idle, &ex}) {
      const BigCard g = bigCard(S, *a, DeckScope::Admin);
      assert(g.chipY >= big_ui::kM && g.barY + 3 <= g.zone.y + g.zone.h);
      assert(g.nowY >= g.zone.y + g.zone.h && g.lastY > g.nowY && g.ctxY < g.whereY);
      assert(g.whereY + big_ui::kLineXS <= bigCardButtons(S, *a, DeckScope::Admin).r[0].y);
    }

    // Stop: the first tap arms it ("again"), the second sends Dismiss; armed,
    // it lapses after 3 s.
    Fleet f = fleetOf({idle});
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Card);
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    DeckCommand c = tap(S, ui, f, b.r[1], DeckScope::Admin, 0);
    assert(c.cmd == DeckCmd::None && ui.stopArmedUntil == 60 + kStopArmMs);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 1000);
    assert(c.cmd == DeckCmd::Dismiss && !strcmp(c.agent, "i") && ui.stopArmedUntil == 0);
    tap(S, ui, f, b.r[1], DeckScope::Admin, 5000);
    bigTick(ui, f, 5000 + 60 + kStopArmMs + 1);
    assert(ui.stopArmedUntil == 0);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 9000);
    assert(c.cmd == DeckCmd::None);                                                  // armed again, not sent
    c = tap(S, ui, f, b.r[0], DeckScope::Admin, 9500);
    assert(c.cmd == DeckCmd::Focus);
    // Interrupt while running.
    f.agents[0].status = Status::Running;
    b = bigCardButtons(S, f.agents[0], DeckScope::Act);
    c = tap(S, ui, f, b.r[1], DeckScope::Act, 10000);
    assert(c.cmd == DeckCmd::Interrupt);
    // Resume an exited agent.
    f.agents[0].status = Status::Exited;
    f.agents[0].resumable = true;
    b = bigCardButtons(S, f.agents[0], DeckScope::Admin);
    c = tap(S, ui, f, b.r[0], DeckScope::Admin, 11000);
    assert(c.cmd == DeckCmd::Resume);
    c = tap(S, ui, f, b.r[1], DeckScope::Admin, 12000);                              // remove: one tap
    assert(c.cmd == DeckCmd::Dismiss);

    // An egg ready to hatch: its box says "tap to hatch"; a tap asks the deck
    // (HatchPet) or, on a Deck Link without it, says where to hatch it.
    f.agents[0].status = Status::Idle;
    f.agents[0].pet.has = true;
    f.agents[0].pet.ready = f.agents[0].pet.cracked = true;
    const BigCard g = bigCard(S, f.agents[0], DeckScope::Act);
    assert(g.box && g.zone.h == big_ui::kZoneH + big_ui::kCtaH);
    assert(bigHit(S, ui, f, g.zone.x + 5, g.zone.y + 5, DeckScope::View).kind == Hit::None);   // view scope: no
    c = tap(S, ui, f, g.zone, DeckScope::Act, 13000);
    if (kCanHatch) assert(c.cmd == DeckCmd::Hatch && !strcmp(c.agent, "i"));
    else assert(c.cmd == DeckCmd::None && strstr(ui.toast, "hatch"));
    f.agents[0].pet.ready = false;                                                   // not ready: no box, no hatch
    assert(!bigCard(S, f.agents[0], DeckScope::Act).box);
    assert(bigHit(S, ui, f, g.zone.x + 5, g.zone.y + 5, DeckScope::Act).kind == Hit::None);
  }

  // ---- the prompt: the whole screen orange; tap an answer to choose it, tap it again to send
  {
    Fleet f = fleetOf({mk("w", 0, Status::Waiting)});
    Agent& w = f.agents[0];
    snprintf(w.waitKind, sizeof w.waitKind, "question");
    snprintf(w.waitTitle, sizeof w.waitTitle, "which session strategy should the login flow use?");
    snprintf(w.promptId, sizeof w.promptId, "p9");
    w.optCount = 3;
    for (int i = 0; i < 3; i++) {
      snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
      snprintf(w.opts[i].label, sizeof w.opts[i].label, "option %d - hint %d", i + 1, i + 1);
    }
    for (const char* kind : {"question", "permission"}) {
      snprintf(w.waitKind, sizeof w.waitKind, "%s", kind);
      snprintf(w.waitDetail, sizeof w.waitDetail, "%s", !strcmp(kind, "permission") ? "terraform apply" : "");
      const BigPrompt p = bigPrompt(S, w);
      assert(p.n == 3 && p.titleLines == 2 && (p.term.h == 0) == (w.waitDetail[0] == 0));
      assert(p.titleY > p.askY && p.askY >= p.pet.y + p.pet.h);
      if (p.term.h) assert(p.term.y >= p.titleY + 2 * big_ui::kLineQ && p.term.y + p.term.h <= p.opt[0].y);
      for (int i = 0; i < 3; i++) assert(p.opt[i].h >= big_ui::kLineL + 4 && (i == 0 || p.opt[i].y >= p.opt[i - 1].y + p.opt[i - 1].h));
      assert(p.opt[2].y + p.opt[2].h <= p.terminal.y && p.terminal.y + p.terminal.h == S.h - big_ui::kM);
    }
    PanelUi ui;
    bigOpen(ui, f, 0);
    assert(ui.screen == Screen::Prompt && ui.selected == -1);    // no primary: nothing chosen
    BigPrompt p = bigPrompt(S, w);
    DeckCommand c = tap(S, ui, f, p.opt[2], DeckScope::Act, 100);
    assert(c.cmd == DeckCmd::None && ui.selected == 2);          // chosen
    c = tap(S, ui, f, p.opt[2], DeckScope::View, 200);           // view scope: a note, nothing sent
    assert(c.cmd == DeckCmd::None && strstr(ui.toast, "view only"));
    c = tap(S, ui, f, p.opt[2], DeckScope::Act, 300);            // again: sent
    assert(c.cmd == DeckCmd::Answer && !strcmp(c.prompt, "p9") && !strcmp(c.key, "3"));
    assert(ui.backAfterToast);
    assert(tap(S, ui, f, p.opt[2], DeckScope::Act, 400).cmd == DeckCmd::None);   // once
    bigTick(ui, f, 2000);
    assert(ui.screen == Screen::Home);
    // A refused answer keeps the prompt; focus focuses.
    bigOpen(ui, f, 0);
    tap(S, ui, f, p.opt[0], DeckScope::Act, 3000);
    tap(S, ui, f, p.opt[0], DeckScope::Act, 3100);
    ui.cmdSeq = 5;
    bigResult(ui, 5, 409, 3200);
    assert(!ui.backAfterToast);
    bigTick(ui, f, 9000);
    assert(ui.screen == Screen::Prompt);
    c = tap(S, ui, f, p.terminal, DeckScope::Act, 9100);
    assert(c.cmd == DeckCmd::Focus);
    // Six long answers still fit above focus (thinner rows).
    w.optCount = 6;
    for (int i = 3; i < 6; i++) snprintf(w.opts[i].key, sizeof w.opts[i].key, "%d", i + 1);
    p = bigPrompt(S, w);
    assert(p.n == 6 && p.opt[0].y > p.titleY && p.opt[5].y + p.opt[5].h <= p.terminal.y);
    for (int i = 0; i < 6; i++) assert(p.opt[i].h >= big_ui::kLineL + 4);
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
    assert(b.n == 3 && b.kind[0] == Hit::Focus && b.kind[2] == Hit::Back);
    const Theme T = deckTheme(false);
    // The focus button's face, beside its label, is filled; the small card has its round buttons elsewhere.
    const uint16_t face = big[(b.r[0].y + 6) * S.w + b.r[0].x + 12];
    assert(face != T.bg && face == big[(b.r[0].y + b.r[0].h - 8) * S.w + b.r[0].x + b.r[0].w - 14]);
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
