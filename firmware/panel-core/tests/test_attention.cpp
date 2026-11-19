// attention.h: the "Needs you" screen draws the deck's spots (attention_slot).
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>

#include <string>
#include <vector>

#include "attention.h"

struct Spec {
  const char* id;
  Status st;
  int spot = 0;          // the deck's attention_slot
  bool unseen = false;
};

constexpr Status R = Status::Running, W = Status::Waiting, I = Status::Idle, S0 = Status::Starting;

static Fleet F;
static Fleet& ag(std::vector<Spec> spec) {
  memset(&F, 0, sizeof F);
  for (const Spec& s : spec) {
    Agent& a = F.agents[F.count];
    snprintf(a.id, sizeof a.id, "%s", s.id);
    a.slot = F.count;
    a.status = s.st;
    a.attnSlot = (uint8_t)s.spot;
    a.unseen = s.unseen;
    F.count++;
  }
  F.total = F.count;
  return F;
}

// The ids in cells [0, n) ("" for a free one).
static std::vector<std::string> cells(const Attention& A, const Fleet& f, int n = -1) {
  if (n < 0) n = A.cap;
  std::vector<std::string> out;
  for (int s = 0; s < n; s++) {
    const int i = attAgent(A, f, s);
    out.push_back(i >= 0 ? f.agents[i].id : "");
  }
  return out;
}

static void expect(const std::vector<std::string>& got, std::vector<std::string> want, const char* what) {
  if (got == want) return;
  fprintf(stderr, "FAIL %s: got [", what);
  for (auto& s : got) fprintf(stderr, "'%s' ", s.c_str());
  fprintf(stderr, "] want [");
  for (auto& s : want) fprintf(stderr, "'%s' ", s.c_str());
  fprintf(stderr, "]\n");
  abort();
}

static Attention A;

int main() {
  // ---- the deck's rule, for the count and an older deck
  {
    Agent a{};
    for (Status s : {R, S0, W}) { a.status = s; assert(wantsYou(a)); }
    for (Status s : {I, Status::Error, Status::Exited, Status::Unknown}) { a.status = s; assert(!wantsYou(a)); }
    a.status = I;
    a.unseen = true;
    assert(wantsYou(a));   // hungry
  }

  // ---- a 4 x 4 screen: spot n in cell n, free spots stay free
  {
    A = Attention();
    assert(attSync(A, ag({{"a", R, 3}, {"b", W, 16}, {"c", I, 1}, {"d", I}}), 16, 0));
    expect(cells(A, F), {"c", "", "a", "", "", "", "", "", "", "", "", "", "", "", "", "b"}, "4x4");
    assert(A.count == 3 && A.fromDeck && A.cap == 16);
    assert(!attSync(A, F, 16, 1));   // nothing changed
    // The deck moves one and lets another go: the cells follow.
    assert(attSync(A, ag({{"a", R, 2}, {"b", W, 16}, {"c", I, 0}, {"d", I}}), 16, 2));
    expect(cells(A, F, 4), {"", "a", "", ""}, "moved and gone");
    assert(A.count == 2);
  }

  // ---- fewer cells: by spot, lowest first; the count still has everyone
  {
    A = Attention();
    attSync(A, ag({{"j", R, 10}, {"c", R, 3}, {"a", W, 1}, {"p", R, 16}}), 4, 0);
    expect(cells(A, F), {"a", "c", "j", "p"}, "2x2");
    attSync(A, F, 2, 0);
    expect(cells(A, F), {"a", "c"}, "two cells");
    assert(A.count == 4 && A.cap == 2);
    attSync(A, F, 8, 0);
    expect(cells(A, F), {"a", "c", "j", "p", "", "", "", ""}, "2x4");
  }

  // ---- the ones waiting for a spot (the deck's grid is full) count, but
  // aren't shown (a board keeps kMaxAgents agents: 16 here)
  {
    A = Attention();
    std::vector<Spec> s;
    static char ids[kMaxAgents][8];
    for (int i = 0; i < kMaxAgents; i++) {
      snprintf(ids[i], sizeof ids[i], "a%d", i);
      s.push_back({ids[i], R, i < kMaxAgents - 2 ? i + 1 : 0});
    }
    attSync(A, ag(s), 16, 0);
    assert(A.count == kMaxAgents && attAgent(A, F, 0) == 0 && attAgent(A, F, kMaxAgents - 3) == kMaxAgents - 3);
    assert(attAgent(A, F, kMaxAgents - 2) == -1);
  }

  // ---- bad or doubled spots are ignored
  {
    A = Attention();
    attSync(A, ag({{"a", R, 2}, {"b", R, 2}, {"c", R, 17}}), 16, 0);
    expect(cells(A, F, 3), {"", "a", ""}, "doubled");
  }

  // ---- an older deck sends no spots: those who need you, in deck order
  {
    A = Attention();
    attSync(A, ag({{"i", I}, {"a", R}, {"h", I, 0, true}, {"z", W}}), 16, 0);
    assert(!A.fromDeck && A.count == 3);
    expect(cells(A, F, 4), {"a", "h", "z", ""}, "older deck");
    attSync(A, F, 2, 0);
    expect(cells(A, F), {"a", "h"}, "older deck, two cells");
    // Nobody needs you: an empty screen.
    attSync(A, ag({{"i", I}, {"e", Status::Error}}), 16, 0);
    assert(A.fromDeck && A.count == 0 && attAgent(A, F, 0) == -1);
  }

  // ---- a fleet that changed since the last sync never shows the wrong agent
  {
    A = Attention();
    attSync(A, ag({{"a", R, 1}, {"b", R, 2}}), 16, 0);
    ag({{"b", R, 2}, {"a", R, 1}});   // reordered, not synced yet
    assert(attAgent(A, F, 0) == 1 && attAgent(A, F, 1) == 0);
    ag({{"b", R, 2}});                // a is gone
    assert(attAgent(A, F, 0) == -1 && attAgent(A, F, 1) == 0);
    assert(attAgent(A, F, -1) == -1 && attAgent(A, F, 16) == -1);
  }

  printf("attention: all tests passed\n");
  return 0;
}
