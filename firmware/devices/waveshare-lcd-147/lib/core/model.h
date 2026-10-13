#pragma once
// The fleet as the board keeps it: fixed-size, no heap, only the fields the
// screen uses. deck_model.h fills it from Deck Link (docs/link/protocol.md).
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#include "timeutil.h"

constexpr int kMaxAgents = 16;

enum class Status : uint8_t { Starting, Running, Waiting, Idle, Error, Exited, Unknown };
enum class Species : uint8_t { Claude, Codex };   // Codex is drawn as the onigiri

// Pixel hats: one per working folder, chosen by the daemon. The order is the
// protocol's (docs/protocol.md) and sprites.h draws by the same index.
static const char* const kHatShapes[] = {"hard hat", "cap", "beanie", "helmet", "wizard", "beret", "chef",
                                         "crown", "headphones", "propeller", "top hat", "cowboy", "bandana"};
static const char* const kHatColors[] = {"teal", "blue", "yellow", "green", "purple", "pink", "sky", "ink"};
constexpr int kHatShapeCount = 13, kHatColorCount = 8;
constexpr uint8_t kHatInk = 7;

// What the board knows of the Deck Link connection, for the screen.
enum class LinkState : uint8_t {
  Connecting,   // Hello sent, no Welcome yet
  Unapproved,   // Welcome with approved = false: agentctl devices approve <name>
  Live,
  Lost,         // was live, nothing from the deck for 15 s ("deck offline")
};

struct Agent {
  char id[12];              // Deck Link ids are at most 11 bytes (link.options)
  uint8_t slot;
  char title[40];
  char aiTitle[56];
  Species species;
  Status status;
  int64_t statusSince;      // epoch seconds, 0 when unknown
  char model[16];
  char actTool[16];         // empty when the agent has no current tool
  char actDetail[72];
  char waitKind[12];
  char waitTitle[32];
  char waitDetail[72];
  char errMsg[56];
  uint32_t ctxUsed, ctxWindow;
  float costUsd;
  uint16_t turns;
  bool unseen, external;
  uint8_t subagents;
  uint8_t hat;              // 0 = no hat, else 1 + index into kHatShapes
  uint8_t hatColor;         // index into kHatColors
};

struct Fleet {
  char server[32];
  int64_t serverTime;       // epoch seconds: the deck's clock (deck_model.h keeps it current)
  uint8_t count;            // agents kept, at most kMaxAgents
  uint16_t total;           // agents on the deck
  Agent agents[kMaxAgents]; // ordered by slot
};

// Bounded copy that never cuts inside a UTF-8 sequence.
static inline void copyUtf8(char* dst, size_t cap, const char* src) {
  if (!cap) return;
  size_t n = src ? strlen(src) : 0;
  if (n >= cap) {
    n = cap - 1;
    while (n > 0 && ((unsigned char)src[n] & 0xC0) == 0x80) n--;
  }
  if (n) memcpy(dst, src, n);
  dst[n] = '\0';
}

// copyUtf8, then newlines and tabs become spaces: every field is drawn on one line.
static inline void copyLine(char* dst, size_t cap, const char* src) {
  copyUtf8(dst, cap, src);
  for (char* p = dst; *p; p++)
    if ((unsigned char)*p < 0x20) *p = ' ';
}
