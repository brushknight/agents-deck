#pragma once
// The fleet as the board keeps it: fixed-size, no heap, only the fields the
// screen uses. parseState() reads one State document of the agents-deck
// protocol (docs/protocol.md).
#include <ArduinoJson.h>
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

struct Agent {
  char id[8];
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
  int64_t serverTime;       // epoch seconds from server.time
  uint8_t count;            // agents kept, at most kMaxAgents
  uint16_t total;           // agents in the document
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

static inline Status parseStatus(const char* s) {
  if (!s) return Status::Unknown;
  if (!strcmp(s, "starting")) return Status::Starting;
  if (!strcmp(s, "running")) return Status::Running;
  if (!strcmp(s, "waiting")) return Status::Waiting;
  if (!strcmp(s, "idle")) return Status::Idle;
  if (!strcmp(s, "error")) return Status::Error;
  if (!strcmp(s, "exited")) return Status::Exited;
  return Status::Unknown;
}

namespace model_detail {

inline const JsonDocument& stateFilter() {
  // 768 bytes was too small: ArduinoJson then drops the last keys without an
  // error, and "context" silently disappeared from every agent.
  static StaticJsonDocument<2048> f;
  if (f.isNull()) {
    f["server"]["name"] = true;
    f["server"]["time"] = true;
    JsonObject a = f["agents"].createNestedObject();
    for (const char* k : {"id", "slot", "title", "aiTitle", "tool", "status", "statusSince",
                          "modelLabel", "costUsd", "turns", "unseen", "external"})
      a[k] = true;
    a["activity"]["tool"] = true;
    a["activity"]["detail"] = true;
    a["waiting"]["kind"] = true;
    a["waiting"]["title"] = true;
    a["waiting"]["detail"] = true;
    a["error"]["message"] = true;
    a["context"]["used"] = true;
    a["context"]["window"] = true;
    a["subagents"][0]["id"] = true;
    a["hat"]["shape"] = true;
    a["hat"]["color"] = true;
  }
  return f;
}

inline void readAgent(JsonObjectConst o, Agent& a) {
  memset(&a, 0, sizeof a);
  copyLine(a.id, sizeof a.id, o["id"] | "");
  a.slot = (uint8_t)(o["slot"] | 0);
  copyLine(a.title, sizeof a.title, o["title"] | "");
  copyLine(a.aiTitle, sizeof a.aiTitle, o["aiTitle"] | "");
  a.species = strcmp(o["tool"] | "", "codex") == 0 ? Species::Codex : Species::Claude;
  a.status = parseStatus(o["status"] | "");
  a.statusSince = parseIso8601(o["statusSince"] | "");
  copyLine(a.model, sizeof a.model, o["modelLabel"] | "");
  copyLine(a.actTool, sizeof a.actTool, o["activity"]["tool"] | "");
  copyLine(a.actDetail, sizeof a.actDetail, o["activity"]["detail"] | "");
  copyLine(a.waitKind, sizeof a.waitKind, o["waiting"]["kind"] | "");
  copyLine(a.waitTitle, sizeof a.waitTitle, o["waiting"]["title"] | "");
  copyLine(a.waitDetail, sizeof a.waitDetail, o["waiting"]["detail"] | "");
  copyLine(a.errMsg, sizeof a.errMsg, o["error"]["message"] | "");
  a.ctxUsed = o["context"]["used"] | 0u;
  a.ctxWindow = o["context"]["window"] | 0u;
  a.costUsd = o["costUsd"] | 0.0f;
  a.turns = (uint16_t)(o["turns"] | 0);
  a.unseen = o["unseen"] | false;
  a.external = o["external"] | false;
  const size_t subs = o["subagents"].size();
  a.subagents = subs > 255 ? 255 : (uint8_t)subs;
  // An unknown shape means no hat; an unknown colour falls back to ink, as in the web UI.
  const char* shape = o["hat"]["shape"] | "";
  const char* color = o["hat"]["color"] | "";
  a.hatColor = kHatInk;
  for (int i = 0; i < kHatShapeCount; i++)
    if (!strcmp(shape, kHatShapes[i])) a.hat = (uint8_t)(i + 1);
  for (int i = 0; i < kHatColorCount; i++)
    if (!strcmp(color, kHatColors[i])) a.hatColor = (uint8_t)i;
}

}  // namespace model_detail

// Reads exactly one JSON object from `in` (anything ArduinoJson can read: a
// stream, a string, a custom reader). `scratch` is reused between calls.
// Returns false and leaves `out` untouched when the document is not a State.
template <class Input>
bool parseState(Input& in, Fleet& out, JsonDocument& scratch) {
  scratch.clear();
  const DeserializationError err =
      deserializeJson(scratch, in, DeserializationOption::Filter(model_detail::stateFilter()));
  if (err) return false;
  JsonArrayConst arr = scratch["agents"];
  if (arr.isNull()) return false;

  memset(&out, 0, sizeof out);
  copyLine(out.server, sizeof out.server, scratch["server"]["name"] | "");
  out.serverTime = parseIso8601(scratch["server"]["time"] | "");

  // The protocol orders agents by slot. Do not rely on it: keep the lowest
  // kMaxAgents slots, in order.
  Agent a;
  for (JsonObjectConst o : arr) {
    out.total++;
    model_detail::readAgent(o, a);
    int pos = out.count;
    while (pos > 0 && out.agents[pos - 1].slot > a.slot) pos--;
    if (pos >= kMaxAgents) continue;
    const int last = out.count < kMaxAgents ? out.count : kMaxAgents - 1;
    for (int i = last; i > pos; i--) out.agents[i] = out.agents[i - 1];
    out.agents[pos] = a;
    if (out.count < kMaxAgents) out.count++;
  }
  return true;
}
