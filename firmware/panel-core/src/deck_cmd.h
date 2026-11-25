#pragma once
// What a board may do and what it asks the deck for, shared by every touch
// layout (square and round).
#include <stdint.h>
#include <stdio.h>

#include "deck_link.h"
#include "model.h"

// What the board may do (Welcome.scope).
enum class DeckScope : uint8_t { View, Act, Admin };

static inline bool canAct(DeckScope s) { return s != DeckScope::View; }

// What the board should send.
enum class DeckCmd : uint8_t { None, Answer, Focus, Interrupt, Dismiss, Resume, Hatch };

// Whether this firmware's Deck Link can ask the deck to hatch a ready egg
// (HatchPet, Frame field 70): the generated code names its tag once
// link.proto has it.
#ifdef agentsdeck_link_v1_Frame_hatch_pet_tag
constexpr bool kCanHatch = true;
#else
constexpr bool kCanHatch = false;
#endif

struct DeckCommand {
  DeckCmd cmd = DeckCmd::None;
  char agent[12] = "", prompt[16] = "", key[8] = "";
};

static inline DeckCommand commandFor(DeckCmd cmd, const Agent& a) {
  DeckCommand c;
  c.cmd = cmd;
  snprintf(c.agent, sizeof c.agent, "%s", a.id);
  return c;
}

// Stop (Dismiss) fires after the button is held this long.
constexpr uint32_t kStopHoldMs = 1000;
