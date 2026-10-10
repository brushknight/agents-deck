#include <assert.h>
#include <fstream>
#include <sstream>
#include <string>
#include "view.h"

static Agent mk(const char* id, Status st, int64_t since = 1000, bool unseen = false) {
  Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  a.status = st;
  a.statusSince = since;
  a.unseen = unseen;
  return a;
}

static Fleet fleetOf(std::initializer_list<Agent> as) {
  Fleet f;
  memset(&f, 0, sizeof f);
  for (const Agent& a : as) { f.agents[f.count] = a; f.agents[f.count].slot = f.count; f.count++; }
  f.total = f.count;
  return f;
}

static Pose poseFor(const char* tool) {
  Agent a = mk("x", Status::Running);
  snprintf(a.actTool, sizeof a.actTool, "%s", tool);
  return poseOf(a, 2000);
}

static std::string status(const Agent& a, int64_t now) {
  char b[40];
  statusLine(a, now, b, sizeof b);
  return b;
}

static std::string activity(const Agent& a) {
  char b[96];
  activityLine(a, b, sizeof b);
  return b;
}

int main() {
  // rank
  assert(rankOf(mk("a", Status::Waiting)) > rankOf(mk("a", Status::Error)));
  assert(rankOf(mk("a", Status::Error)) > rankOf(mk("a", Status::Idle, 1, true)));
  assert(rankOf(mk("a", Status::Idle, 1, true)) > rankOf(mk("a", Status::Running)));
  assert(rankOf(mk("a", Status::Running)) > rankOf(mk("a", Status::Starting)));
  assert(rankOf(mk("a", Status::Starting)) > rankOf(mk("a", Status::Idle)));
  assert(rankOf(mk("a", Status::Idle)) == rankOf(mk("a", Status::Exited)));

  // auto hero
  {
    Fleet none = fleetOf({});
    assert(autoHero(none) == -1);
    Fleet f = fleetOf({mk("run", Status::Running, 500), mk("w1", Status::Waiting, 100),
                       mk("idle", Status::Idle, 900), mk("w2", Status::Waiting, 200)});
    assert(autoHero(f) == 3);                        // the later of the two waiting agents
    Fleet g = fleetOf({mk("i1", Status::Idle, 100), mk("i2", Status::Idle, 300), mk("i3", Status::Idle, 200)});
    assert(autoHero(g) == 1);
  }

  // The fixture: slots 1 and 6 are waiting.
  {
    std::ifstream fi(FIXTURE);
    std::stringstream ss;
    ss << fi.rdbuf();
    std::string text = ss.str();
    DynamicJsonDocument doc(65536);
    static Fleet f;
    assert(parseState(text, f, doc));
    const int h = autoHero(f);
    assert(f.agents[h].status == Status::Waiting);
    Counts c = countFleet(f);
    assert(c.waiting == 2 && c.error == 1 && c.hungry == 1 && c.running == 5);
  }

  // button hold
  {
    Fleet f = fleetOf({mk("a", Status::Running), mk("b", Status::Waiting), mk("c", Status::Idle)});
    ViewState v;
    assert(resolveHero(f, v, 0) == 1);
    pressNext(f, v, 1000);                           // from b to c
    assert(v.hold && resolveHero(f, v, 1001) == 2);
    pressNext(f, v, 2000);                           // wraps to a
    assert(resolveHero(f, v, 2001) == 0);
    assert(resolveHero(f, v, 2000 + kHoldMs - 1) == 0);
    assert(resolveHero(f, v, 2000 + kHoldMs) == 1 && !v.hold);   // hold expired: auto again

    pressNext(f, v, 5000);                           // hold c
    Fleet gone = fleetOf({mk("a", Status::Running), mk("b", Status::Waiting)});
    assert(resolveHero(gone, v, 5001) == 1 && !v.hold);          // held agent vanished

    Fleet empty = fleetOf({});
    pressNext(empty, v, 6000);
    assert(!v.hold && resolveHero(empty, v, 6000) == -1);

    // millis() wrap: a hold set just before the wrap still lasts kHoldMs.
    ViewState w;
    pressNext(f, w, 0xFFFFFF00u);
    assert(resolveHero(f, w, 0x00000010u) == 2);
    assert(resolveHero(f, w, 0xFFFFFF00u + kHoldMs) == 1);
  }

  // new waiting
  {
    Fleet prev = fleetOf({mk("a", Status::Running), mk("b", Status::Waiting)});
    Fleet same = fleetOf({mk("a", Status::Running), mk("b", Status::Waiting)});
    Fleet turned = fleetOf({mk("a", Status::Waiting), mk("b", Status::Waiting)});
    Fleet added = fleetOf({mk("a", Status::Running), mk("b", Status::Waiting), mk("n", Status::Waiting)});
    Fleet calm = fleetOf({mk("a", Status::Running), mk("b", Status::Idle)});
    assert(!hasNewWaiting(prev, same));
    assert(hasNewWaiting(prev, turned));
    assert(hasNewWaiting(prev, added));
    assert(!hasNewWaiting(prev, calm));
  }

  // pose
  assert(poseFor("Edit") == Pose::Write && poseFor("MultiEdit") == Pose::Write);
  assert(poseFor("Grep") == Pose::Read && poseFor("LS") == Pose::Read);
  assert(poseFor("Bash") == Pose::Bash && poseFor("WebFetch") == Pose::Web);
  assert(poseFor("TodoWrite") == Pose::Plan && poseFor("Task") == Pose::Delegate);
  assert(poseFor("Compact") == Pose::Compact && poseFor("SomethingNew") == Pose::Run);
  assert(poseFor("") == Pose::Think);
  assert(poseOf(mk("a", Status::Waiting), 2000) == Pose::Wait);
  assert(poseOf(mk("a", Status::Error), 2000) == Pose::Err);
  assert(poseOf(mk("a", Status::Starting), 2000) == Pose::Start);
  assert(poseOf(mk("a", Status::Exited), 2000) == Pose::Exit);
  assert(poseOf(mk("a", Status::Idle, 1000), 2000) == Pose::Idle);
  assert(poseOf(mk("a", Status::Idle, 1000, true), 2000) == Pose::Hungry);
  assert(poseOf(mk("a", Status::Idle, 1997, true), 2000) == Pose::Celebrate);   // first 6 s after a turn
  assert(poseOf(mk("a", Status::Idle, 1994, true), 2000) == Pose::Hungry);
  assert(poseOf(mk("a", Status::Idle, 0), 3) == Pose::Idle);                    // unknown time: no party

  // context percent
  {
    Agent a = mk("a", Status::Running);
    assert(ctxPct(a) == 0);
    a.ctxUsed = 410000; a.ctxWindow = 1000000;
    assert(ctxPct(a) == 41);
    a.ctxUsed = 3000000;
    assert(ctxPct(a) == 100);
    a.ctxUsed = 4000000000u; a.ctxWindow = 4000000000u;       // no 32-bit overflow
    assert(ctxPct(a) == 100);
  }

  // text
  assert(status(mk("a", Status::Running, 1000), 1000 + 12 * 60) == "running \xC2\xB7 12m");
  assert(status(mk("a", Status::Waiting, 1000), 1045) == "needs you \xC2\xB7 45s");
  assert(status(mk("a", Status::Idle, 1000, true), 1000 + 7200) == "hungry \xC2\xB7 2h");
  assert(status(mk("a", Status::Idle, 1000), 1060) == "idle \xC2\xB7 1m");
  assert(status(mk("a", Status::Error, 0), 1060) == "error");
  assert(status(mk("a", Status::Starting, 1000), 1060) == "starting");
  {
    Agent a = mk("a", Status::Running);
    assert(activity(a) == "thinking");
    snprintf(a.actTool, sizeof a.actTool, "Edit");
    assert(activity(a) == "Edit");
    snprintf(a.actDetail, sizeof a.actDetail, "src/a.ts");
    assert(activity(a) == "Edit src/a.ts");

    Agent w = mk("w", Status::Waiting);
    assert(activity(w) == "needs input");
    snprintf(w.waitTitle, sizeof w.waitTitle, "run this command?");
    assert(activity(w) == "run this command?");
    snprintf(w.waitDetail, sizeof w.waitDetail, "terraform apply");
    assert(activity(w) == "terraform apply");

    Agent e = mk("e", Status::Error);
    assert(activity(e) == "error");
    snprintf(e.errMsg, sizeof e.errMsg, "api 529 overloaded");
    assert(activity(e) == "api 529 overloaded");
    assert(activity(mk("i", Status::Idle)) == "");
  }
  return 0;
}
