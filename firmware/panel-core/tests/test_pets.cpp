// Agent pets on the panels (sprites.h drawPet, drawCritter, petLine): the
// adapter keeps the pet, no pet draws exactly the critter, a pet stays in the
// room a critter's box gives it, stands on a dark plate on orange, and every
// layout draws its screens with pets inside the frame; the square card and
// the detail screens show the pet's name, stage and level.
#define PANEL_MAX_AGENTS 64
#define PANEL_OPTION_LABEL 96
#include <assert.h>

#include <vector>

#include "../sim/fixture.h"
#include "layout_147.h"
#include "layout_big.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "layout_spring.h"
#include "layout_square.h"

static const uint16_t kGuard = 0xDEAD;

// A canvas with a guard band round it: nothing may be drawn outside.
struct Framed {
  int w, h, pad;
  std::vector<uint16_t> mem;
  Canvas c;
  Framed(int w_, int h_) : w(w_), h(h_), pad(2 * w_), mem((size_t)w_ * h_ + 4 * w_, kGuard), c{mem.data() + 2 * w_, w_, h_} {}
  void check() const {
    for (int i = 0; i < pad; i++) assert(mem[i] == kGuard && mem[mem.size() - 1 - i] == kGuard);
  }
};

static deck_pet_t pet(int stage, int mood, const char* name = "Pip", uint16_t level = 7) {
  deck_pet_t p{};
  p.has = true;
  p.stage = (uint8_t)stage;
  p.mood = (uint8_t)mood;
  p.level = level;
  snprintf(p.name, sizeof p.name, "%s", name);
  return p;
}

// Every agent of the sample fleet gets a pet: the stages and moods in turn.
static void givePets(Fleet& f) {
  for (int i = 0; i < f.count; i++) f.agents[i].pet = pet(i % 7, (i / 7) % 4, "Biscuit", (uint16_t)(i * 6));
}

static void testAdapter() {
  agentsdeck_link_v1_Agent in = agentsdeck_link_v1_Agent_init_zero;
  snprintf(in.id, sizeof in.id, "a1");
  Agent a;
  agentFromLink(in, a);
  assert(!a.pet.has);   // pets off: no pet, the critter as before
  in.has_pet = true;
  snprintf(in.pet.name, sizeof in.pet.name, "Momo");
  snprintf(in.pet.stage, sizeof in.pet.stage, "kid");
  snprintf(in.pet.mood, sizeof in.pet.mood, "hungry");
  in.pet.level = 6;
  in.pet.hearts = 2;
  agentFromLink(in, a);
  assert(a.pet.has && !strcmp(a.pet.name, "Momo") && a.pet.stage == DECK_PET_KID && a.pet.mood == DECK_PET_HUNGRY);
  assert(a.pet.level == 6 && a.pet.hearts == 2);
}

static void testLine() {
  Agent a{};
  char buf[48];
  petLine(a, buf, sizeof buf);
  assert(!buf[0]);
  a.pet = pet(DECK_PET_KID, DECK_PET_FED, "Pip", 7);
  petLine(a, buf, sizeof buf);
  assert(!strcmp(buf, "Pip" DECK_DOT "Kid" DECK_DOT "level 7"));
  a.pet = pet(DECK_PET_GROWN, DECK_PET_ASLEEP, "Juniper", 12);
  petLine(a, buf, sizeof buf);
  assert(!strcmp(buf, "Juniper" DECK_DOT "Grown-up" DECK_DOT "level 12"));
  a.pet = pet(DECK_PET_EGG, DECK_PET_FED, "", 0);
  petLine(a, buf, sizeof buf);
  assert(!strcmp(buf, "Egg"));
}

// No pet: drawCritter is drawSprite, pixel for pixel.
static void testNoPet() {
  constexpr int W = 120, H = 100;
  std::vector<uint16_t> p1(W * H), p2(W * H);
  Canvas c1{p1.data(), W, H}, c2{p2.data(), W, H};
  Agent a{};
  for (int sp = 0; sp < 2; sp++)
    for (int pose = 0; pose <= (int)Pose::Celebrate; pose++)
      for (uint8_t hat : {0, 2, 9, 13}) {
        a.species = (Species)sp;
        a.hat = hat;
        a.hatColor = 3;
        for (uint32_t ms : {0u, 333u, 1250u}) {
          c1.fill(0x0841);
          c2.fill(0x0841);
          drawSprite(c1, 30, 30, 3, a.species, (Pose)pose, 0xE71C, 0x0841, ms, true, a.hat, a.hatColor);
          drawCritter(c2, 30, 30, 3, a, (Pose)pose, 0xE71C, 0x0841, ms, true);
          assert(p1 == p2);
        }
      }
}

// A pet stays in the room it is given: cells -4..19 x -4..15 at unit (-1, -5),
// plus its rock and bob; it is drawn in its own colours; on orange it stands on a dark plate.
static void testRoom() {
  constexpr int W = 200, H = 200, X = 60, Y = 60;
  constexpr uint16_t kBg = 0x0000, kAccent = rgb565(0xFF, 0x4D, 0x00);
  std::vector<uint16_t> px(W * H);
  Canvas c{px.data(), W, H};
  Agent a{};
  constexpr uint16_t kGrey = rgb565(0x40, 0x40, 0x40), kDark = rgb565(0x0A, 0x0A, 0x0A);
  // Draws it on bg; returns its dark pixels, checking that all it drew is in its room.
  auto draw = [&](int s, uint16_t bg, uint32_t ms) {
    c.fill(bg);
    drawCritter(c, X, Y, s, a, Pose::Run, 0xFFFF, bg, ms, true);
    const int half = s / 2 > 0 ? s / 2 : 1;
    int drawn = 0, dark = 0;
    for (int y = 0; y < H; y++)
      for (int x = 0; x < W; x++) {
        const uint16_t p = px[y * W + x];
        if (p == bg) continue;
        drawn++;
        dark += p == kDark;
        assert(x >= X - 5 * s - half && x < X + 19 * s + half);
        assert(y >= Y - 5 * s - half && y < Y + 15 * s);
      }
    assert(drawn > 30 * s * s);
    return dark;
  };
  for (int s = 1; s <= 4; s++)
    for (int stage = 0; stage < 7; stage++)
      for (int mood = 0; mood < 4; mood++)
        for (uint8_t hat : {0, 2, 11, 13})
          for (Status st : {Status::Running, Status::Idle, Status::Waiting})
            for (uint32_t ms : {0u, 300u, 700u}) {
              a.pet = pet(stage, mood);
              a.pet.ready = stage == 0 && mood == 0;
              a.hat = hat;
              a.status = st;
              const int onGrey = draw(s, kGrey, ms);
              assert(draw(s, kAccent, ms) > onGrey);   // the dark plate on orange
              draw(s, kBg, ms);
            }
  // The pet's own orange, not the critter's colour.
  a.pet = pet(DECK_PET_GROWN, DECK_PET_FED);
  a.status = Status::Idle;
  c.fill(kBg);
  drawCritter(c, X, Y, 2, a, Pose::Idle, 0xFFFF, kBg, 0, true);
  int body = 0, white = 0;
  for (uint16_t p : px) { body += p == kAccent; white += p == 0xFFFF; }
  assert(body > 100 && white == 0);
}

// The spring's critter box holds a pet (the carousel redraws only that box).
static void testSpringBox() {
  std::vector<uint16_t> px(480 * 480);
  Canvas c{px.data(), 480, 480};
  Agent a{};
  for (int s = 1; s <= 6; s++)
    for (int stage = 0; stage < 7; stage++)
      for (int mood = 0; mood < 4; mood++)
        for (uint32_t ms : {0u, 700u}) {
          a.pet = pet(stage, mood);
          a.pet.ready = stage == 0;
          a.hat = 2;   // the cap: its brim reaches furthest left
          a.status = Status::Waiting;
          c.fill(0);
          agentCritter(c, a, 240, 240, s, 0xFFFF, rgb565(0xFF, 0x4D, 0x00), ms, 0, false);
          const RoundRect b = spring_draw::critterBox(240, 240, s, false, true);
          for (int y = 0; y < 480; y++)
            for (int x = 0; x < 480; x++)
              if (px[y * 480 + x]) assert(x >= b.x && x < b.x + b.w && y >= b.y && y < b.y + b.h);
        }
}

// Every layout's screens with a pet on every agent, inside the frame.
static void testLayouts() {
  static DeckFleet d;
  assert(fixture::load(FIXTURE, d));
  static Fleet f;
  f = d.fleet;
  givePets(f);
  const int64_t now = f.serverTime + 754;

  for (const SquareSpec& S : {specAmoled18(), specT4S3(), specLcd4()}) {
    Framed fr(S.w, S.h);
    PanelUi ui;
    PanelInput in{&f, LinkState::Live, "x", DeckScope::Admin, 0, now, &ui, 0};
    const SquarePanel P(S);
    P.draw(fr.c, in);
    fr.check();
    for (int i = 0; i < f.count; i++) {
      const Agent& a = f.agents[i];
      snprintf(ui.openId, sizeof ui.openId, "%s", a.id);
      for (Screen s : {Screen::Card, Screen::Prompt})
        for (uint32_t ms : {0u, 700u}) {
          ui.screen = s;
          in.ms = ms;
          P.draw(fr.c, in);
          fr.check();
        }
      // The card: the pet's line under the pill, above "doing now", and drawn.
      const square_detail::CardGeom g = square_detail::cardGeom(S, a, DeckScope::Admin);
      assert(g.pet.h > 0 && g.pet.y >= g.pill.y + g.pill.h && g.pet.y + g.pet.h < g.doingY);
      ui.screen = Screen::Card;
      P.draw(fr.c, in);
      int ink = 0;
      for (int y = g.pet.y; y < g.pet.y + g.pet.h; y++)
        for (int x = g.pet.x; x < g.pet.x + 40; x++) ink += fr.c.px[y * S.w + x] != fr.c.px[g.pet.y * S.w + S.w - 3];
      assert(ink > 0);
    }
    ui.screen = Screen::Home;
  }

  {
    const BigSpec S = specBig480();
    Framed fr(S.w, S.h);
    PanelUi ui;
    PanelInput in{&f, LinkState::Live, "x", DeckScope::Admin, 0, now, &ui, 0};
    const BigPanel P(S);
    P.draw(fr.c, in);
    fr.check();
    for (int i = 0; i < f.count; i++) {
      snprintf(ui.openId, sizeof ui.openId, "%s", f.agents[i].id);
      for (Screen s : {Screen::Card, Screen::Prompt}) {
        ui.screen = s;
        P.draw(fr.c, in);
        fr.check();
      }
    }
  }

  {
    Framed fr(kScreenW, kScreenH);
    for (int hero = 0; hero < f.count; hero++)
      for (uint32_t ms : {0u, 700u}) {
        drawScreen(fr.c, {&f, hero, LinkState::Live, "x", ms, now, false});
        fr.check();
      }
  }

  static const SpringPanel spring(layoutSpring28());
  static const OrbitPanel orbit(layoutOrbit28());
  static const DialPanel dial(layoutDial21());
  for (const RoundLayout* L : {(const RoundLayout*)&spring, (const RoundLayout*)&orbit, (const RoundLayout*)&dial}) {
    Framed fr(480, 480);
    RoundUi ui;
    RoundInput in{&f, LinkState::Live, "x", RoundScope::Admin, 0, now, &ui, 0};
    roundSync(ui, *L, f, 0);
    L->draw(fr.c, in);
    fr.check();
    for (int i = 0; i < f.count; i++) {
      roundOpen(ui, f, i, RoundScreen::Detail);
      for (uint32_t ms : {0u, 700u}) {
        in.ms = ms;
        L->draw(fr.c, in);
        fr.check();
      }
      roundHome(ui);
    }
  }
}

int main() {
  testAdapter();
  testLine();
  testNoPet();
  testRoom();
  testSpringBox();
  testLayouts();
  printf("pets ok\n");
  return 0;
}
