#pragma once
// The "Needs you" screen: who is on it and in which spot. The deck decides
// both (docs/protocol.md, "The Needs you grid"): every agent carries its spot
// in Agent.attnSlot (Deck Link's attention_slot), 1..16 on a 4 x 4 grid, or 0
// when it isn't there. So every board, the panel and the browser show the
// same agents in the same spots. The board only draws them:
//
//   - A 4 x 4 screen (16 cells) puts spot n in cell n.
//   - A screen with fewer cells (the 2 x 4 mini-board, the 2 x 2 and 2 x 3
//     list screens) shows the agents in order of their spot, lowest first,
//     in the cells it has. The ones that don't fit aren't shown; the count
//     on the mark still includes them. Newcomers take the lowest free spot,
//     so the first spots fill first.
//   - An older deck sends no spots (attnSlot is always 0). When no agent has
//     one but some need you, they fill the cells in deck order instead: no
//     5 s stay and no remembered spots, but never an empty screen.
//
// No display, no Arduino, no heap. attSync() works the screen out from the
// fleet (one pass, every frame and before hit-testing a touch); attAgent()
// says who is in a cell.
#include <stdint.h>
#include <string.h>

#include "model.h"

constexpr int kAttMaxSpots = 16;    // the deck's grid, and the most cells a page has on any layout (4 x 4)

// The deck's rule, for the count and for an older deck: working (running,
// starting), asking (waiting), or finished and not looked at yet (hungry).
static inline bool wantsYou(const Agent& a) {
  return a.status == Status::Running || a.status == Status::Starting || a.status == Status::Waiting ||
         (a.status == Status::Idle && a.unseen);
}

struct Attention {
  int8_t cap = -1;                     // cells on the screen
  int8_t cellAgent[kAttMaxSpots];      // fleet index of the agent in each cell, -1 for a free one
  char cellId[kAttMaxSpots][12];       // …and its id, so a fleet that changed never shows the wrong agent
  uint8_t count = 0;                   // agents on the screen, with the ones not shown (the mark's count)
  bool fromDeck = true;                // false: the deck sent no spots, they were filled in deck order
};

// Works the screen out for this fleet and `cap` cells. True when it changed.
// `now` is unused (the deck keeps the time); it stays for the callers.
static inline bool attSync(Attention& A, const Fleet& f, int cap, uint32_t now) {
  (void)now;
  if (cap < 0) cap = 0;
  if (cap > kAttMaxSpots) cap = kAttMaxSpots;
  int8_t cell[kAttMaxSpots];
  for (int s = 0; s < kAttMaxSpots; s++) cell[s] = -1;

  // The deck's spots, and who needs you without one (the grid is full).
  int8_t bySpot[kAttMaxSpots];
  for (int s = 0; s < kAttMaxSpots; s++) bySpot[s] = -1;
  int placed = 0, wanting = 0, waiting = 0;
  for (int i = 0; i < f.count; i++) {
    const Agent& a = f.agents[i];
    const int s = a.attnSlot;
    if (s >= 1 && s <= kAttMaxSpots && bySpot[s - 1] < 0) {
      bySpot[s - 1] = (int8_t)i;
      placed++;
    } else if (wantsYou(a)) {
      waiting++;
    }
    if (wantsYou(a)) wanting++;
  }
  const bool fromDeck = placed > 0 || wanting == 0;
  int count = 0;
  if (fromDeck) {
    if (cap >= kAttMaxSpots) {
      for (int s = 0; s < kAttMaxSpots; s++) cell[s] = bySpot[s];
    } else {
      int k = 0;
      for (int s = 0; s < kAttMaxSpots && k < cap; s++)
        if (bySpot[s] >= 0) cell[k++] = bySpot[s];
    }
    count = placed + waiting;
  } else {
    int k = 0;
    for (int i = 0; i < f.count; i++)   // the fleet is in deck (slot) order
      if (wantsYou(f.agents[i]) && k < cap) cell[k++] = (int8_t)i;
    count = wanting;
  }
  if (count > 255) count = 255;

  bool changed = A.cap != cap || A.count != count || A.fromDeck != fromDeck;
  for (int s = 0; s < kAttMaxSpots; s++) {
    const char* id = cell[s] >= 0 ? f.agents[cell[s]].id : "";
    if (A.cellAgent[s] != cell[s] || strncmp(A.cellId[s], id, sizeof A.cellId[s])) changed = true;
    A.cellAgent[s] = cell[s];
    strncpy(A.cellId[s], id, sizeof A.cellId[s] - 1);
    A.cellId[s][sizeof A.cellId[s] - 1] = '\0';
  }
  A.cap = (int8_t)cap;
  A.count = (uint8_t)count;
  A.fromDeck = fromDeck;
  return changed;
}

// The fleet index of the agent in cell `s`, or -1 for a free one. Checks the
// id, so a fleet that changed since the last attSync() never shows the wrong
// agent (the next frame puts it right).
static inline int attAgent(const Attention& A, const Fleet& f, int s) {
  if (s < 0 || s >= A.cap || s >= kAttMaxSpots || A.cellAgent[s] < 0) return -1;
  const int i = A.cellAgent[s];
  if (i < f.count && !strcmp(f.agents[i].id, A.cellId[s])) return i;
  for (int j = 0; j < f.count; j++)
    if (!strcmp(f.agents[j].id, A.cellId[s])) return j;
  return -1;
}
