// attention.h: the "Needs you" screen's spots. Case for case the web
// dashboard's backend/internal/web/attention_test.mjs.
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>

#include <string>
#include <utility>
#include <vector>

#include "attention.h"

static double never() {
  fprintf(stderr, "no random spot expected\n");
  abort();
}
static double rand09() { return 0.9; }
static double rand0() { return 0.0; }

struct Spec {
  const char* id;
  Status st;
  bool unseen = false;
};

constexpr Status R = Status::Running, W = Status::Waiting, I = Status::Idle, S0 = Status::Starting;

static Fleet ag(std::vector<Spec> spec) {
  static Fleet f;
  memset(&f, 0, sizeof f);
  for (const Spec& s : spec) {
    Agent& a = f.agents[f.count];
    snprintf(a.id, sizeof a.id, "%s", s.id);
    a.slot = f.count;
    a.status = s.st;
    a.unseen = s.unseen;
    f.count++;
  }
  f.total = f.count;
  return f;
}

// The ids in spots [0, n) ("" for a free one).
static std::vector<std::string> spots(const Attention& A, const Fleet& f, int n = -1) {
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
static Attention& fresh(double (*r)() = never) {
  A = Attention();
  A.rand = r;
  return A;
}
static Fleet F;
static void arr(std::vector<Spec> spec, int cap, uint32_t now, double (*r)() = nullptr) {
  if (r) A.rand = r;
  F = ag(spec);
  attArrange(A, F, cap, now);
}

int main() {
  // ---- working and asking agents are on it, the rest are not
  {
    Agent a{};
    for (Status s : {R, S0, W}) { a.status = s; assert(wantsYou(a)); }
    for (Status s : {I, Status::Error, Status::Exited, Status::Unknown}) { a.status = s; assert(!wantsYou(a)); }
    fresh();
    arr({{"a", R}, {"b", I}, {"c", W}, {"d", Status::Error}, {"e", S0}}, 16, 0);
    expect(spots(A, F, 4), {"a", "c", "e", ""}, "working and asking");
    assert(A.count == 3 && A.more == 0 && A.cap == 16);
  }

  // ---- newcomers take the lowest free spot; nobody moves up when one leaves
  {
    fresh();
    arr({{"a", R}, {"b", R}, {"c", R}}, 4, 0);
    expect(spots(A, F), {"a", "b", "c", ""}, "three");
    arr({{"a", R}, {"b", I}, {"c", R}}, 4, 1000);
    expect(spots(A, F), {"a", "b", "c", ""}, "b lingers");
    arr({{"a", R}, {"b", I}, {"c", R}}, 4, 1000 + kAttLingerMs);
    expect(spots(A, F), {"a", "", "c", ""}, "b left, c stays put");
    arr({{"a", R}, {"b", I}, {"c", R}, {"d", W}}, 4, 7000);
    expect(spots(A, F), {"a", "d", "c", ""}, "d takes the lowest free spot");
  }

  // ---- an agent stays 5 seconds after it stops, and wake says when it leaves
  {
    fresh();
    arr({{"a", W}}, 4, 0);
    arr({{"a", I}}, 4, 100);
    expect(spots(A, F, 1), {"a"}, "stays");
    assert(A.hasWake && A.wake == 100 + kAttLingerMs);
    arr({{"a", I}}, 4, 100 + kAttLingerMs - 1);
    expect(spots(A, F, 1), {"a"}, "still there");
    arr({{"a", I}}, 4, 100 + kAttLingerMs);
    expect(spots(A, F, 1), {""}, "gone");
    assert(A.count == 0 && !A.hasWake);
  }

  // ---- back to work within 5 seconds: it never leaves
  {
    fresh();
    arr({{"a", R}, {"b", R}}, 4, 0);
    arr({{"a", I}, {"b", R}}, 4, 1000);
    arr({{"a", W}, {"b", R}}, 4, 4000);
    assert(!A.hasWake);
    arr({{"a", W}, {"b", R}}, 4, 60000);
    expect(spots(A, F, 2), {"a", "b"}, "never left");
    arr({{"a", I}, {"b", R}}, 4, 61000);   // and the 5 seconds start over the next time it stops
    assert(A.hasWake && A.wake == 61000 + kAttLingerMs);
  }

  // ---- a returning agent gets its old spot back when it is free
  {
    fresh();
    arr({{"a", R}, {"b", R}, {"c", R}}, 4, 0);
    arr({{"a", R}, {"b", I}, {"c", R}}, 4, 1);
    arr({{"a", R}, {"b", I}, {"c", R}}, 4, 1 + kAttLingerMs);
    arr({{"a", I}, {"b", I}, {"c", R}}, 4, 10000);
    arr({{"a", I}, {"b", I}, {"c", R}}, 4, 20000);   // a left too: 0 and 1 are free
    arr({{"a", I}, {"b", R}, {"c", R}}, 4, 30000);
    expect(spots(A, F), {"", "b", "c", ""}, "b goes back to 1, not the lowest free 0");
  }

  // ---- its old spot taken: a random free spot, which it then remembers
  {
    fresh();
    arr({{"a", R}, {"b", R}}, 4, 0);
    arr({{"a", I}, {"b", R}}, 4, 1);
    arr({{"a", I}, {"b", R}}, 4, 1 + kAttLingerMs);            // a left spot 0
    arr({{"a", I}, {"b", R}, {"n", R}}, 4, 10000);             // n takes 0
    expect(spots(A, F), {"n", "b", "", ""}, "n takes 0");
    // free spots are 2 and 3; rand 0.9 picks the last, so not simply the first
    arr({{"a", R}, {"b", R}, {"n", R}}, 4, 11000, rand09);
    expect(spots(A, F), {"n", "b", "", "a"}, "a at random");
    assert(attMemory(A, "a") == 3);
    // leave and come back once more: spot 3 again, no random pick
    A.rand = never;
    arr({{"a", I}, {"b", R}, {"n", R}}, 4, 12000);
    arr({{"a", I}, {"b", R}, {"n", R}}, 4, 12000 + kAttLingerMs);
    arr({{"a", R}, {"b", R}, {"n", R}}, 4, 20000);
    expect(spots(A, F), {"n", "b", "", "a"}, "a back at 3");
  }

  // ---- a full screen: the rest wait with a count, oldest first
  {
    fresh();
    arr({{"a", R}, {"b", R}}, 2, 0);
    arr({{"a", R}, {"b", R}, {"c", R}}, 2, 1000);
    arr({{"a", R}, {"b", R}, {"c", R}, {"d", W}}, 2, 2000);
    expect(spots(A, F), {"a", "b"}, "full");
    assert(A.more == 2 && A.count == 4);
    // d sits earlier in deck order, but c has waited longer
    arr({{"d", W}, {"a", I}, {"b", R}, {"c", R}}, 2, 3000);
    arr({{"d", W}, {"a", I}, {"b", R}, {"c", R}}, 2, 3000 + kAttLingerMs);
    expect(spots(A, F), {"c", "b"}, "c (first-timer) takes the freed spot");
    assert(A.more == 1);
  }

  // ---- a waiting agent that stops working while it waits for a spot drops out after 5 seconds
  {
    fresh();
    arr({{"a", R}, {"b", R}}, 1, 0);
    assert(A.more == 1);
    arr({{"a", R}, {"b", I}}, 1, 1000);
    assert(A.more == 1);
    arr({{"a", R}, {"b", I}}, 1, 1000 + kAttLingerMs);
    assert(A.more == 0 && A.count == 1);
  }

  // ---- the grid shrinks: agents in spots that are gone are placed again, the others keep theirs
  {
    fresh();
    arr({{"a", R}, {"b", R}, {"c", R}, {"d", R}, {"e", R}}, 16, 0);
    arr({{"a", R}, {"b", I}, {"c", I}, {"d", R}, {"e", R}}, 16, 1);
    arr({{"a", R}, {"b", I}, {"c", I}, {"d", R}, {"e", R}}, 16, 1 + kAttLingerMs);
    expect(spots(A, F, 5), {"a", "", "", "d", "e"}, "b and c left");
    // 16 -> 4 spots: e (spot 4) is gone; free are 1 and 2, rand 0 picks 1
    arr({{"a", R}, {"b", I}, {"c", I}, {"d", R}, {"e", R}}, 4, 10000, rand0);
    expect(spots(A, F), {"a", "e", "", "d"}, "e placed again");
    assert(attMemory(A, "e") == 1);
    // and back to 16: nobody moves
    A.rand = never;
    arr({{"a", R}, {"b", I}, {"c", I}, {"d", R}, {"e", R}}, 16, 11000);
    expect(spots(A, F, 5), {"a", "e", "", "d", ""}, "nobody moves");
  }

  // ---- remembered spots come back, and newcomers do not take them (the web's
  // "after a reload"; a board keeps them in RAM while it runs)
  {
    fresh();
    const std::pair<const char*, int> memory[] = {{"a", 5}, {"b", 0}, {"gone", 3}};
    for (const auto& m : memory) {
      AttEntry& e = A.e[A.n++];
      memset(&e, 0, sizeof e);
      snprintf(e.id, sizeof e.id, "%s", m.first);
      e.spot = -1;
      e.mem = (int8_t)m.second;
    }
    arr({{"n", R}, {"a", R}, {"b", W}}, 16, 0);
    const auto s = spots(A, F);
    assert(s[5] == "a" && s[0] == "b");
    assert(s[1] == "n");   // n took the lowest spot nobody remembers
    assert(attMemory(A, "gone") == -1 && A.n == 3);   // ids no longer on the deck are forgotten
  }

  // ---- agents that leave the deck leave the screen at once
  {
    fresh();
    arr({{"a", R}, {"b", R}}, 4, 0);
    arr({{"b", R}}, 4, 10);
    expect(spots(A, F), {"", "b", "", ""}, "a left the deck");
    assert(A.count == 1);
  }

  // ---- no room at all
  {
    fresh();
    arr({{"a", R}}, 0, 0);
    assert(spots(A, F).empty() && A.more == 1);
  }

  // ---- finished and not looked at yet (hungry): stays until you check it
  {
    Agent a{};
    a.status = I;
    a.unseen = true;
    assert(wantsYou(a));
    a.unseen = false;
    assert(!wantsYou(a));
    fresh();
    arr({{"a", R}, {"b", R}}, 16, 0);
    arr({{"a", I, true}, {"b", R}}, 16, 1000);
    arr({{"a", I, true}, {"b", R}}, 16, 10 * 60 * 1000);   // long after it finished, still in its spot
    expect(spots(A, F, 2), {"a", "b"}, "hungry stays");
    const uint32_t t = 10 * 60 * 1000;
    arr({{"a", I, false}, {"b", R}}, 16, t + 1000);        // checked: it leaves 5 seconds later
    expect(spots(A, F, 2), {"a", "b"}, "checked, lingering");
    arr({{"a", I, false}, {"b", R}}, 16, t + 1000 + kAttLingerMs);
    expect(spots(A, F, 2), {"", "b"}, "checked, gone; b does not move");
  }

  // ---- the board's side: attSync rearranges only when something changed
  {
    fresh();
    F = ag({{"a", R}, {"b", I}});
    assert(attSync(A, F, 16, 0));               // the first time
    assert(!attSync(A, F, 16, 50));             // nothing changed
    F.agents[0].ctxUsed = 1234;                 // a field the screen doesn't place by
    assert(!attSync(A, F, 16, 100));
    F.agents[1].status = W;                     // b asks
    assert(attSync(A, F, 16, 150) && A.count == 2);
    F.agents[0].status = I;                     // a stops: lingers, a timer
    assert(attSync(A, F, 16, 200) && A.hasWake && A.wake == 200 + kAttLingerMs);
    assert(!attSync(A, F, 16, 200 + kAttLingerMs - 1));
    assert(attSync(A, F, 16, 200 + kAttLingerMs) && A.count == 1 && attAgent(A, F, 0) == -1);
    assert(attSync(A, F, 8, 6000) && A.cap == 8);   // another size
    // The clock wraps: a timer set just before it still runs out 5 s later.
    fresh();
    F = ag({{"a", R}});
    const uint32_t t0 = 0xFFFFFFFFu - 1000;
    attSync(A, F, 4, t0);
    F.agents[0].status = I;
    attSync(A, F, 4, t0 + 10);
    assert(!attSync(A, F, 4, t0 + 2000) && attAgent(A, F, 0) == 0);
    assert(attSync(A, F, 4, t0 + 10 + kAttLingerMs) && attAgent(A, F, 0) == -1);
    // A fleet that changed since the last arrangement never shows the wrong agent.
    fresh();
    F = ag({{"a", R}, {"b", R}});
    attSync(A, F, 4, 0);
    Fleet g = ag({{"x", I}, {"b", R}, {"a", R}});
    assert(attAgent(A, g, 0) == 2 && attAgent(A, g, 1) == 1);
  }

  // ---- a full fleet: the table holds every agent, no more
  {
    fresh(rand09);
    std::vector<Spec> all;
    static char ids[kMaxAgents][12];
    for (int i = 0; i < kMaxAgents; i++) {
      snprintf(ids[i], sizeof ids[i], "a%d", i);
      all.push_back({ids[i], R});
    }
    arr(all, 16, 0);
    assert(A.count == kMaxAgents && A.more == kMaxAgents - 16 && A.n == kMaxAgents);
    for (int s = 0; s < 16; s++) assert(attAgent(A, F, s) == s);
  }

  printf("attention: ok (%zu bytes of state)\n", sizeof(Attention));
  return 0;
}
