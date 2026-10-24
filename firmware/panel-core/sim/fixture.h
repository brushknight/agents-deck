#pragma once
// Host only: reads a State JSON file of the web API (docs/fixtures/state.json)
// and turns it into the Deck Link frames the deck would send for it
// (SnapshotBegin, one AgentChanged per agent, SnapshotEnd), the way
// backend/internal/link/convert.go does with TILE | CARD | PROMPT | NUMBERS.
// The tests and sim/render.cpp feed those frames through deck_model.h, so the
// screen is drawn from exactly what the board would receive.
//
// A small JSON reader is enough for a fixture we control; it is not used on
// the board.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <fstream>
#include <map>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "deck_model.h"
#include "timeutil.h"

namespace fixture {

struct Json {
  enum Kind { Null, Bool, Num, Str, Arr, Obj } kind = Null;
  bool b = false;
  double n = 0;
  std::string s;
  std::vector<Json> a;
  std::map<std::string, Json> o;

  const Json& operator[](const char* k) const {
    static const Json none;
    auto it = o.find(k);
    return kind == Obj && it != o.end() ? it->second : none;
  }
  const char* str() const { return kind == Str ? s.c_str() : ""; }
  double num() const { return kind == Num ? n : 0; }
  bool truthy() const { return kind == Bool && b; }
  bool isNull() const { return kind == Null; }
};

struct Reader {
  const char* p;
  bool ok = true;

  void ws() { while (*p == ' ' || *p == '\n' || *p == '\r' || *p == '\t') p++; }
  bool eat(char c) { ws(); if (*p == c) { p++; return true; } return false; }

  static void utf8(std::string& out, unsigned cp) {
    if (cp < 0x80) out += (char)cp;
    else if (cp < 0x800) { out += (char)(0xC0 | cp >> 6); out += (char)(0x80 | (cp & 63)); }
    else if (cp < 0x10000) { out += (char)(0xE0 | cp >> 12); out += (char)(0x80 | ((cp >> 6) & 63)); out += (char)(0x80 | (cp & 63)); }
    else { out += (char)(0xF0 | cp >> 18); out += (char)(0x80 | ((cp >> 12) & 63)); out += (char)(0x80 | ((cp >> 6) & 63)); out += (char)(0x80 | (cp & 63)); }
  }

  std::string string() {
    std::string out;
    p++;   // opening quote
    while (*p && *p != '"') {
      if (*p != '\\') { out += *p++; continue; }
      p++;
      switch (*p) {
        case 'n': out += '\n'; break;
        case 't': out += '\t'; break;
        case 'r': out += '\r'; break;
        case 'b': out += '\b'; break;
        case 'f': out += '\f'; break;
        case 'u': {
          unsigned cp = (unsigned)strtoul(std::string(p + 1, 4).c_str(), nullptr, 16);
          p += 4;
          if (cp >= 0xD800 && cp < 0xDC00 && p[1] == '\\' && p[2] == 'u') {   // surrogate pair
            const unsigned lo = (unsigned)strtoul(std::string(p + 3, 4).c_str(), nullptr, 16);
            cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
            p += 6;
          }
          utf8(out, cp);
          break;
        }
        default: out += *p; break;   // \" \\ \/
      }
      if (*p) p++;
    }
    if (*p != '"') ok = false; else p++;
    return out;
  }

  Json value() {
    Json v;
    ws();
    if (*p == '{') {
      p++;
      v.kind = Json::Obj;
      if (eat('}')) return v;
      do {
        ws();
        if (*p != '"') { ok = false; return v; }
        std::string k = string();
        if (!eat(':')) { ok = false; return v; }
        v.o[k] = value();
      } while (ok && eat(','));
      if (!eat('}')) ok = false;
    } else if (*p == '[') {
      p++;
      v.kind = Json::Arr;
      if (eat(']')) return v;
      do v.a.push_back(value()); while (ok && eat(','));
      if (!eat(']')) ok = false;
    } else if (*p == '"') {
      v.kind = Json::Str;
      v.s = string();
    } else if (!strncmp(p, "true", 4)) { v.kind = Json::Bool; v.b = true; p += 4; }
    else if (!strncmp(p, "false", 5)) { v.kind = Json::Bool; p += 5; }
    else if (!strncmp(p, "null", 4)) { p += 4; }
    else {
      char* end;
      v.n = strtod(p, &end);
      if (end == p) ok = false;
      v.kind = Json::Num;
      p = end;
    }
    return v;
  }
};

static inline bool parse(const std::string& text, Json& out) {
  Reader r{text.c_str()};
  out = r.value();
  r.ws();
  return r.ok && !*r.p;
}

static inline agentsdeck_link_v1_Tool toolOf(const char* t) {
  static const struct { const char* name; agentsdeck_link_v1_Tool tool; } k[] = {
      {"claude", agentsdeck_link_v1_Tool_TOOL_CLAUDE}, {"codex", agentsdeck_link_v1_Tool_TOOL_CODEX},
      {"gemini", agentsdeck_link_v1_Tool_TOOL_GEMINI}, {"shell", agentsdeck_link_v1_Tool_TOOL_SHELL},
      {"opencode", agentsdeck_link_v1_Tool_TOOL_OPENCODE}, {"sim", agentsdeck_link_v1_Tool_TOOL_SIM}};
  for (const auto& e : k)
    if (!strcmp(t, e.name)) return e.tool;
  return agentsdeck_link_v1_Tool_TOOL_UNSPECIFIED;
}

static inline agentsdeck_link_v1_Status statusOf(const char* s) {
  static const char* const k[] = {"", "starting", "running", "waiting", "idle", "error", "exited"};
  for (int i = 1; i < 7; i++)
    if (!strcmp(s, k[i])) return (agentsdeck_link_v1_Status)i;
  return agentsdeck_link_v1_Status_STATUS_UNSPECIFIED;
}

static inline agentsdeck_link_v1_PromptKind kindOf(const char* s) {
  if (!strcmp(s, "permission")) return agentsdeck_link_v1_PromptKind_PROMPT_PERMISSION;
  if (!strcmp(s, "question")) return agentsdeck_link_v1_PromptKind_PROMPT_QUESTION;
  if (!strcmp(s, "input")) return agentsdeck_link_v1_PromptKind_PROMPT_INPUT;
  return agentsdeck_link_v1_PromptKind_PROMPT_UNSPECIFIED;
}

#define FIXTURE_COPY(dst, src) copyUtf8((dst), sizeof(dst), (src))

// One JSON agent as the deck sends it with every field group.
static inline void agentToLink(const Json& j, agentsdeck_link_v1_Agent& a) {
  memset(&a, 0, sizeof a);
  FIXTURE_COPY(a.id, j["id"].str());
  a.slot = (uint32_t)j["slot"].num();
  FIXTURE_COPY(a.title, j["title"].str());
  FIXTURE_COPY(a.ai_title, j["aiTitle"].str());
  a.tool = toolOf(j["tool"].str());
  a.status = statusOf(j["status"].str());
  a.status_since_ms = parseIso8601(j["statusSince"].str()) * 1000;
  FIXTURE_COPY(a.cwd, j["cwd"].str());
  FIXTURE_COPY(a.folder, j["folder"].str());
  FIXTURE_COPY(a.branch, j["branch"].str());
  FIXTURE_COPY(a.model, j["model"].str());
  FIXTURE_COPY(a.model_label, j["modelLabel"].str());
  FIXTURE_COPY(a.effort, j["effort"].str());
  if (!j["activity"].isNull()) {
    a.has_activity = true;
    FIXTURE_COPY(a.activity.tool, j["activity"]["tool"].str());
    FIXTURE_COPY(a.activity.detail, j["activity"]["detail"].str());
  }
  const Json& w = j["waiting"];
  if (!w.isNull()) {
    a.has_waiting = true;
    FIXTURE_COPY(a.waiting.id, w["id"].str());
    a.waiting.kind = kindOf(w["kind"].str());
    FIXTURE_COPY(a.waiting.title, w["title"].str());
    FIXTURE_COPY(a.waiting.detail, w["detail"].str());
    FIXTURE_COPY(a.waiting.context, w["context"].str());
    for (const Json& o : w["options"].a) {
      if (a.waiting.options_count >= 6) break;
      agentsdeck_link_v1_Option& op = a.waiting.options[a.waiting.options_count++];
      FIXTURE_COPY(op.key, o["key"].str());
      FIXTURE_COPY(op.label, o["label"].str());
      op.primary = o["primary"].truthy();
    }
  }
  FIXTURE_COPY(a.error, j["error"]["message"].str());
  a.has_tokens = true;
  a.tokens.input = (int64_t)j["tokens"]["input"].num();
  a.tokens.output = (int64_t)j["tokens"]["output"].num();
  a.has_context = true;
  a.context.used = (int64_t)j["context"]["used"].num();
  a.context.window = (int64_t)j["context"]["window"].num();
  a.cost_usd = j["costUsd"].num();
  a.turns = (uint32_t)j["turns"].num();
  a.focused = j["focused"].truthy();
  a.unseen = j["unseen"].truthy();
  a.external = j["external"].truthy();
  a.subagent_count = (uint32_t)j["subagents"].a.size();
  for (const Json& sj : j["subagents"].a) {
    if (a.subagents_count >= 6) break;
    agentsdeck_link_v1_Subagent& sa = a.subagents[a.subagents_count++];
    FIXTURE_COPY(sa.id, sj["id"].str());
    FIXTURE_COPY(sa.title, sj["title"].str());
    FIXTURE_COPY(sa.type, sj["type"].str());
    FIXTURE_COPY(sa.tool, sj["tool"].str());
  }
  a.lost = j["lost"].truthy();
  a.resumable = j["resumable"].truthy();
  a.attached = j["attached"].truthy();
  if (!j["hat"].isNull()) {
    a.has_hat = true;
    FIXTURE_COPY(a.hat.shape, j["hat"]["shape"].str());
    FIXTURE_COPY(a.hat.color, j["hat"]["color"].str());
    a.hat.automatic = j["hat"]["auto"].truthy();
  }
}

// The frames of one snapshot, as the deck sends them after Subscribe.
static inline bool snapshotFrames(const std::string& stateJson, std::vector<agentsdeck_link_v1_Frame>& out) {
  Json doc;
  if (!parse(stateJson, doc) || doc["agents"].kind != Json::Arr) return false;
  out.clear();
  auto frame = [&](pb_size_t which) -> agentsdeck_link_v1_Frame& {
    out.emplace_back();
    agentsdeck_link_v1_Frame& f = out.back();
    memset(&f, 0, sizeof f);
    f.which_body = which;
    return f;
  };
  agentsdeck_link_v1_Frame& b = frame(agentsdeck_link_v1_Frame_snapshot_begin_tag);
  b.body.snapshot_begin.has_server = true;
  FIXTURE_COPY(b.body.snapshot_begin.server.name, doc["server"]["name"].str());
  FIXTURE_COPY(b.body.snapshot_begin.server.version, doc["server"]["version"].str());
  b.body.snapshot_begin.server.time_ms = parseIso8601(doc["server"]["time"].str()) * 1000;
  b.body.snapshot_begin.agent_count = (uint32_t)doc["agents"].a.size();
  for (const Json& j : doc["agents"].a) {
    agentsdeck_link_v1_Frame& f = frame(agentsdeck_link_v1_Frame_agent_changed_tag);
    f.body.agent_changed.has_agent = true;
    agentToLink(j, f.body.agent_changed.agent);
  }
  frame(agentsdeck_link_v1_Frame_snapshot_end_tag);
  return true;
}

static inline std::string slurp(const char* path) {
  std::ifstream f(path);
  std::stringstream ss;
  ss << f.rdbuf();
  return ss.str();
}

// Loads a State JSON file into a DeckFleet through deck_model.h.
static inline bool load(const char* path, DeckFleet& d, uint32_t nowMillis = 0) {
  std::vector<agentsdeck_link_v1_Frame> frames;
  if (!snapshotFrames(slurp(path), frames)) return false;
  for (const auto& f : frames) deckApply(d, f, nowMillis);
  return d.have;
}

}  // namespace fixture
