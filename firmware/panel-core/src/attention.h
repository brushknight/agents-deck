#pragma once
// The "Needs you" screen: who is on it and in which spot. The same rules as
// the web dashboard (backend/internal/web/static/attention.js) and the
// Flutter deck (clients/flutter/agents_ui/lib/src/attention.dart):
//
//   - An agent is on it while it works (running, starting), asks you
//     something (waiting), or finished a turn you haven't looked at yet
//     (hungry: idle and unseen), and for 5 s after that. Back within the
//     5 s and it never left.
//   - Spots are sticky: nobody moves when another leaves. A newcomer takes
//     the lowest free spot; one that was here before gets its old spot back
//     when that is free, else a random free one, which it then remembers.
//     When the screen is full the rest wait, oldest first.
//
// No display, no Arduino, no heap: a fixed table of kMaxAgents entries keyed
// by agent id, kept in RAM while the board runs. Agents that leave the fleet
// are forgotten. The clock (ms, wraps) and the random source are passed in.
// attSync() rearranges only when the fleet's ids, order or "wants you"
// change, the screen's size changes, or a 5 s timer runs out; between those
// a frame costs one pass over the fleet for the signature.
#include <stdint.h>
#include <string.h>

#include "model.h"

constexpr uint32_t kAttLingerMs = 5000;
constexpr int kAttMaxSpots = 16;    // the most cells a page has on any layout (4 x 4)

static inline bool wantsYou(const Agent& a) {
  return a.status == Status::Running || a.status == Status::Starting || a.status == Status::Waiting ||
         (a.status == Status::Idle && a.unseen);   // finished, not looked at yet (hungry): stays until you check it
}

struct AttEntry {
  char id[12];
  int8_t spot;        // its spot now, -1 for none (waiting for one, or not on the screen)
  int8_t mem;         // the spot it had last, -1 for none
  bool on;            // on the screen (placed or waiting)
  bool leaving;       // stopped wanting you: leaves at leaveAt
  uint32_t joined;    // when it came on the screen
  uint32_t leaveAt;
};

struct Attention {
  // [0, 1), for the random free spot; the board's hardware RNG. nullptr: the
  // lowest free spot.
  double (*rand)() = nullptr;

  AttEntry e[kMaxAgents];
  uint8_t n = 0;

  // The last arrangement.
  int8_t cap = -1;                  // spots on the screen
  int8_t spotEntry[kAttMaxSpots];   // entry in each spot, -1 for a free one
  int8_t spotAgent[kAttMaxSpots];   // its index in the fleet that was arranged
  uint8_t count = 0;                // agents on the screen, placed or waiting
  uint8_t more = 0;                 // of those, waiting for a spot (the screen is full)
  bool hasWake = false;             // a lingering agent leaves at `wake`
  uint32_t wake = 0;
  uint32_t sig = 0;                 // the fleet that was arranged (attSignature)
};

namespace att_detail {

static inline int find(const Attention& A, const char* id) {
  for (int i = 0; i < A.n; i++)
    if (!strcmp(A.e[i].id, id)) return i;
  return -1;
}

static inline int fleetIndex(const Fleet& f, const char* id) {
  for (int i = 0; i < f.count; i++)
    if (!strcmp(f.agents[i].id, id)) return i;
  return -1;
}

static inline bool due(uint32_t now, uint32_t at) { return (int32_t)(now - at) >= 0; }

}  // namespace att_detail

// What the arrangement depends on: each agent's id, in deck order, and
// whether it wants you.
static inline uint32_t attSignature(const Fleet& f) {
  uint32_t h = 2166136261u;
  auto mix = [&h](uint8_t b) { h = (h ^ b) * 16777619u; };
  mix(f.count);
  for (int i = 0; i < f.count; i++) {
    for (const char* p = f.agents[i].id; *p; p++) mix((uint8_t)*p);
    mix(wantsYou(f.agents[i]) ? 0xA5 : 0x5A);
  }
  return h;
}

// attention.js's arrange(): the screen for this fleet, `cap` spots, at `now`.
static inline void attArrange(Attention& A, const Fleet& f, int cap, uint32_t now) {
  using namespace att_detail;
  if (cap < 0) cap = 0;
  if (cap > kAttMaxSpots) cap = kAttMaxSpots;
  A.cap = (int8_t)cap;
  A.sig = attSignature(f);

  // Forget agents no longer on the deck.
  int k = 0;
  for (int i = 0; i < A.n; i++)
    if (fleetIndex(f, A.e[i].id) >= 0) A.e[k++] = A.e[i];
  A.n = (uint8_t)k;

  // Who is on the screen: members[] holds entry indices, in deck order.
  A.hasWake = false;
  int8_t members[kMaxAgents];
  int8_t memberAt[kMaxAgents];   // the member's index in the fleet
  int nm = 0;
  for (int i = 0; i < f.count; i++) {
    const Agent& a = f.agents[i];
    int j = find(A, a.id);
    if (wantsYou(a)) {
      if (j >= 0) A.e[j].leaving = false;
    } else if (j >= 0 && A.e[j].on) {
      AttEntry& e = A.e[j];
      if (!e.leaving) { e.leaving = true; e.leaveAt = now + kAttLingerMs; }
      if (due(now, e.leaveAt)) { e.leaving = false; e.on = false; e.spot = -1; continue; }
      if (!A.hasWake || (int32_t)(e.leaveAt - A.wake) < 0) { A.wake = e.leaveAt; A.hasWake = true; }
    } else {
      continue;
    }
    if (j < 0) {
      if (A.n >= kMaxAgents) continue;   // can't happen: entries are agents of this fleet
      j = A.n++;
      AttEntry& e = A.e[j];
      memset(&e, 0, sizeof e);
      memcpy(e.id, a.id, sizeof e.id);
      e.id[sizeof e.id - 1] = '\0';
      e.spot = e.mem = -1;
    }
    if (!A.e[j].on) { A.e[j].on = true; A.e[j].joined = now; }
    members[nm] = (int8_t)j;
    memberAt[nm] = (int8_t)i;
    nm++;
  }

  // Spots that still exist keep their agent; the others are placed again.
  for (int s = 0; s < kAttMaxSpots; s++) A.spotEntry[s] = A.spotAgent[s] = -1;
  for (int m = 0; m < nm; m++) {
    AttEntry& e = A.e[members[m]];
    if (e.spot >= 0 && e.spot < cap && A.spotEntry[e.spot] < 0) {
      A.spotEntry[e.spot] = members[m];
      A.spotAgent[e.spot] = memberAt[m];
    } else {
      e.spot = -1;
    }
  }
  int nfree = 0;
  for (int s = 0; s < cap; s++)
    if (A.spotEntry[s] < 0) nfree++;

  // The ones without a spot, oldest first (then deck order): an insertion sort.
  int8_t waiting[kMaxAgents];   // indices into members[]
  int nw = 0;
  for (int m = 0; m < nm; m++) {
    if (A.e[members[m]].spot >= 0) continue;
    int at = nw;
    const uint32_t jm = A.e[members[m]].joined;
    while (at > 0 && (int32_t)(jm - A.e[members[waiting[at - 1]]].joined) < 0) {
      waiting[at] = waiting[at - 1];
      at--;
    }
    waiting[at] = (int8_t)m;
    nw++;
  }
  const int picked = nw < nfree ? nw : nfree;
  auto put = [&](int m, int s) {
    AttEntry& e = A.e[members[m]];
    A.spotEntry[s] = members[m];
    A.spotAgent[s] = memberAt[m];
    e.spot = e.mem = (int8_t)s;
  };
  // Old spots first, so a newcomer doesn't take the spot a returning agent had.
  int8_t rest[kMaxAgents];
  int nr = 0;
  for (int w = 0; w < picked; w++) {
    const int m = waiting[w];
    const int mem = A.e[members[m]].mem;
    if (mem >= 0 && mem < cap && A.spotEntry[mem] < 0) put(m, mem);
    else rest[nr++] = (int8_t)m;
  }
  for (int r = 0; r < nr; r++) {
    int8_t freeSpots[kAttMaxSpots];
    int nf = 0;
    for (int s = 0; s < cap; s++)
      if (A.spotEntry[s] < 0) freeSpots[nf++] = (int8_t)s;
    if (!nf) break;
    int pick = 0;
    if (A.e[members[rest[r]]].mem >= 0 && A.rand) {   // been here before: a random free spot
      pick = (int)(A.rand() * nf);
      if (pick > nf - 1) pick = nf - 1;
      if (pick < 0) pick = 0;
    }
    put(rest[r], freeSpots[pick]);
  }
  A.count = (uint8_t)nm;
  A.more = (uint8_t)(nw - picked);
}

// Arranges again when it is due: the fleet changed (ids, order, who wants
// you), the screen has another size, or a lingering agent's 5 s ran out.
// True when it did. Call it every frame (and before hit-testing a touch).
static inline bool attSync(Attention& A, const Fleet& f, int cap, uint32_t now) {
  if (cap > kAttMaxSpots) cap = kAttMaxSpots;
  if (A.cap == cap && A.sig == attSignature(f) && !(A.hasWake && att_detail::due(now, A.wake))) return false;
  attArrange(A, f, cap, now);
  return true;
}

// The fleet index of the agent in spot `s`, or -1 for a free spot. Checks
// the id, so a fleet that changed since the last arrangement never shows the
// wrong agent (attSync() puts it right on the next frame).
static inline int attAgent(const Attention& A, const Fleet& f, int s) {
  if (s < 0 || s >= A.cap || s >= kAttMaxSpots || A.spotEntry[s] < 0) return -1;
  const char* id = A.e[A.spotEntry[s]].id;
  const int i = A.spotAgent[s];
  if (i >= 0 && i < f.count && !strcmp(f.agents[i].id, id)) return i;
  return att_detail::fleetIndex(f, id);
}

// The spot an agent remembers (-1: none), for the tests and the debug log.
static inline int attMemory(const Attention& A, const char* id) {
  const int j = att_detail::find(A, id);
  return j >= 0 ? A.e[j].mem : -1;
}
