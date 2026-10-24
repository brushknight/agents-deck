#pragma once
// From Deck Link to the board's model: turns the kit's frames
// (agentsdeck_link_v1_Agent, SnapshotBegin/End, AgentRemoved, Ping) into the
// Fleet that view.h and screen.h draw. No Arduino code: the same adapter runs
// in the host tests and in sim/.
//
// The board subscribes to every agent (slot_count 0) with TILE | CARD |
// PROMPT | NUMBERS, which carries every field the screen uses. It keeps the
// kMaxAgents lowest slots, like the JSON parser it replaces, and counts the
// rest in Fleet.total.
//
// Clock: the board has none. The deck's time comes with every SnapshotBegin
// (server.time_ms) and every Ping (time_ms, every 5 s); "now" is the last one
// plus millis() since. Ages are therefore never more than 5 s of drift off.
#include <stdint.h>
#include <string.h>

#include "deck_link.h"
#include "model.h"
#include "view.h"

constexpr int kMaxKnown = kMaxAgents * 2 > 64 ? kMaxAgents * 2 : 64;   // agent ids tracked for Fleet.total

// What the board subscribes to: every field the screen draws.
constexpr uint32_t kPanelFields =
    agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_CARD |
    agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT | agentsdeck_link_v1_FieldGroup_FIELDS_NUMBERS;

struct DeckFleet {
  Fleet fleet;                    // what the screen draws
  Fleet next;                     // a snapshot being received
  bool inSnapshot = false;
  bool have = false;              // a snapshot has completed at least once
  bool newWaiting = false;        // an agent started waiting: the caller drops the button hold
  char known[kMaxKnown][12];      // every agent id on the deck (the board keeps only kMaxAgents)
  uint16_t knownCount = 0;
  char nextKnown[kMaxKnown][12];
  uint16_t nextKnownCount = 0;
  int64_t clockMs = 0;            // the deck's clock (Unix ms) at clockAt, 0 = unknown
  uint32_t clockAt = 0;           // millis() then
};

// ---------------------------------------------------------------- one agent

static inline Status statusFromLink(agentsdeck_link_v1_Status s) {
  switch (s) {
    case agentsdeck_link_v1_Status_STATUS_STARTING: return Status::Starting;
    case agentsdeck_link_v1_Status_STATUS_RUNNING:  return Status::Running;
    case agentsdeck_link_v1_Status_STATUS_WAITING:  return Status::Waiting;
    case agentsdeck_link_v1_Status_STATUS_IDLE:     return Status::Idle;
    case agentsdeck_link_v1_Status_STATUS_ERROR:    return Status::Error;
    case agentsdeck_link_v1_Status_STATUS_EXITED:   return Status::Exited;
    default:                                        return Status::Unknown;
  }
}

static inline const char* promptKindName(agentsdeck_link_v1_PromptKind k) {
  switch (k) {
    case agentsdeck_link_v1_PromptKind_PROMPT_PERMISSION: return "permission";
    case agentsdeck_link_v1_PromptKind_PROMPT_QUESTION:   return "question";
    case agentsdeck_link_v1_PromptKind_PROMPT_INPUT:      return "input";
    default:                                              return "";
  }
}

// One Deck Link agent as the board keeps it. Field for field what the JSON
// parser read: statusSince in epoch seconds, the hat by name, subagents as a count.
static inline void agentFromLink(const agentsdeck_link_v1_Agent& in, Agent& a) {
  memset(&a, 0, sizeof a);
  copyLine(a.id, sizeof a.id, in.id);
  a.slot = in.slot > 255 ? 255 : (uint8_t)in.slot;
  copyLine(a.title, sizeof a.title, in.title);
  copyLine(a.aiTitle, sizeof a.aiTitle, in.ai_title);
  a.species = in.tool == agentsdeck_link_v1_Tool_TOOL_CODEX ? Species::Codex : Species::Claude;
  a.status = statusFromLink(in.status);
  a.statusSince = in.status_since_ms > 0 ? in.status_since_ms / 1000 : 0;
  copyLine(a.model, sizeof a.model, in.model_label);
  copyLine(a.effort, sizeof a.effort, in.effort);
  copyLine(a.lastPrompt, sizeof a.lastPrompt, in.last_prompt);
  a.tokensOut = in.has_tokens && in.tokens.output > 0 ? (uint64_t)in.tokens.output : 0;
  if (in.has_activity) {
    copyLine(a.actTool, sizeof a.actTool, in.activity.tool);
    copyLine(a.actDetail, sizeof a.actDetail, in.activity.detail);
  }
  if (in.has_waiting) {
    copyLine(a.waitKind, sizeof a.waitKind, promptKindName(in.waiting.kind));
    copyLine(a.waitTitle, sizeof a.waitTitle, in.waiting.title);
    copyLine(a.waitDetail, sizeof a.waitDetail, in.waiting.detail);
    copyLine(a.promptId, sizeof a.promptId, in.waiting.id);
    copyLine(a.promptCtx, sizeof a.promptCtx, in.waiting.context);
    for (pb_size_t i = 0; i < in.waiting.options_count && a.optCount < kMaxOptions; i++) {
      Option& o = a.opts[a.optCount++];
      copyLine(o.key, sizeof o.key, in.waiting.options[i].key);
      copyLine(o.label, sizeof o.label, in.waiting.options[i].label);
      o.primary = in.waiting.options[i].primary;
    }
  }
  copyLine(a.errMsg, sizeof a.errMsg, in.error);
  if (in.has_context) {
    a.ctxUsed = in.context.used <= 0 ? 0 : in.context.used > 0xFFFFFFFF ? 0xFFFFFFFFu : (uint32_t)in.context.used;
    a.ctxWindow = in.context.window <= 0 ? 0 : in.context.window > 0xFFFFFFFF ? 0xFFFFFFFFu : (uint32_t)in.context.window;
  }
  a.costUsd = (float)in.cost_usd;
  a.turns = in.turns > 0xFFFF ? 0xFFFF : (uint16_t)in.turns;
  a.unseen = in.unseen;
  a.external = in.external;
  a.subagents = in.subagent_count > 255 ? 255 : (uint8_t)in.subagent_count;
  switch (in.tool) {
    case agentsdeck_link_v1_Tool_TOOL_CLAUDE: a.tool = ToolKind::Claude; break;
    case agentsdeck_link_v1_Tool_TOOL_CODEX:  a.tool = ToolKind::Codex; break;
    case agentsdeck_link_v1_Tool_TOOL_GEMINI: a.tool = ToolKind::Gemini; break;
    default:                                  a.tool = ToolKind::Other; break;
  }
  a.focused = in.focused;
  a.lost = in.lost;
  a.resumable = in.resumable;
  if (in.subagents_count > 0) copyLine(a.subTool, sizeof a.subTool, in.subagents[0].tool);
  copyLine(a.folder, sizeof a.folder, in.folder);
  copyLine(a.branch, sizeof a.branch, in.branch);
  // An unknown shape means no hat; an unknown colour falls back to ink, as in the web UI.
  a.hatColor = kHatInk;
  if (in.has_hat) {
    for (int i = 0; i < kHatShapeCount; i++)
      if (!strcmp(in.hat.shape, kHatShapes[i])) a.hat = (uint8_t)(i + 1);
    for (int i = 0; i < kHatColorCount; i++)
      if (!strcmp(in.hat.color, kHatColors[i])) a.hatColor = (uint8_t)i;
  }
}

// ---------------------------------------------------------------- the fleet

namespace deck_model_detail {

static inline int indexOf(const Fleet& f, const char* id) {
  for (int i = 0; i < f.count; i++)
    if (!strcmp(f.agents[i].id, id)) return i;
  return -1;
}

static inline void removeAt(Fleet& f, int i) {
  for (int k = i; k + 1 < f.count; k++) f.agents[k] = f.agents[k + 1];
  f.count--;
}

// Inserts or replaces by id, ordered by slot; keeps the kMaxAgents lowest slots.
static inline void upsert(Fleet& f, const Agent& a) {
  const int old = indexOf(f, a.id);
  if (old >= 0) removeAt(f, old);
  int pos = f.count;
  while (pos > 0 && f.agents[pos - 1].slot > a.slot) pos--;
  if (pos >= kMaxAgents) return;
  const int last = f.count < kMaxAgents ? f.count : kMaxAgents - 1;
  for (int i = last; i > pos; i--) f.agents[i] = f.agents[i - 1];
  f.agents[pos] = a;
  if (f.count < kMaxAgents) f.count++;
}

static inline void know(char (*ids)[12], uint16_t& n, const char* id) {
  for (int i = 0; i < n; i++)
    if (!strcmp(ids[i], id)) return;
  if (n < kMaxKnown) copyUtf8(ids[n++], 12, id);
}

static inline void forget(char (*ids)[12], uint16_t& n, const char* id) {
  for (int i = 0; i < n; i++)
    if (!strcmp(ids[i], id)) {
      memmove(ids[i], ids[i + 1], (size_t)(n - i - 1) * sizeof ids[0]);
      n--;
      return;
    }
}

}  // namespace deck_model_detail

static inline void deckSetClock(DeckFleet& d, int64_t deckMs, uint32_t nowMillis) {
  if (deckMs <= 0) return;
  d.clockMs = deckMs;
  d.clockAt = nowMillis;
  d.fleet.serverTime = deckMs / 1000;
}

// The deck's time now, in epoch seconds; 0 while unknown.
static inline int64_t deckNowEpoch(const DeckFleet& d, uint32_t nowMillis) {
  if (!d.clockMs) return 0;
  return (d.clockMs + (int64_t)(uint32_t)(nowMillis - d.clockAt)) / 1000;
}

// Applies one frame from the deck. Returns true when the fleet on screen changed.
static inline bool deckApply(DeckFleet& d, const agentsdeck_link_v1_Frame& f, uint32_t nowMillis) {
  using namespace deck_model_detail;
  switch (f.which_body) {
    case agentsdeck_link_v1_Frame_ping_tag:
      deckSetClock(d, (int64_t)f.body.ping.time_ms, nowMillis);
      return false;

    case agentsdeck_link_v1_Frame_snapshot_begin_tag: {
      const agentsdeck_link_v1_SnapshotBegin& b = f.body.snapshot_begin;
      memset(&d.next, 0, sizeof d.next);
      copyLine(d.next.server, sizeof d.next.server, b.has_server ? b.server.name : "");
      d.nextKnownCount = 0;
      d.inSnapshot = true;
      if (b.has_server) deckSetClock(d, b.server.time_ms, nowMillis);
      return false;
    }

    case agentsdeck_link_v1_Frame_agent_changed_tag: {
      if (!f.body.agent_changed.has_agent) return false;
      Agent a;
      agentFromLink(f.body.agent_changed.agent, a);
      if (d.inSnapshot) {
        upsert(d.next, a);
        know(d.nextKnown, d.nextKnownCount, a.id);
        d.next.total = d.nextKnownCount;
        return false;
      }
      const int prev = indexOf(d.fleet, a.id);
      if (a.status == Status::Waiting && (prev < 0 || d.fleet.agents[prev].status != Status::Waiting))
        d.newWaiting = true;   // never hide a new prompt behind a hold
      upsert(d.fleet, a);
      know(d.known, d.knownCount, a.id);
      d.fleet.total = d.knownCount;
      return true;
    }

    case agentsdeck_link_v1_Frame_agent_removed_tag: {
      const char* id = f.body.agent_removed.id;
      if (d.inSnapshot) {
        const int i = indexOf(d.next, id);
        if (i >= 0) removeAt(d.next, i);
        forget(d.nextKnown, d.nextKnownCount, id);
        d.next.total = d.nextKnownCount;
        return false;
      }
      // An agent beyond the kMaxAgents kept stays off the board until the
      // next snapshot (the deck sends one after every Welcome).
      const int i = indexOf(d.fleet, id);
      if (i >= 0) removeAt(d.fleet, i);
      forget(d.known, d.knownCount, id);
      d.fleet.total = d.knownCount;
      return true;
    }

    case agentsdeck_link_v1_Frame_snapshot_end_tag:
      if (!d.inSnapshot) return false;
      if (d.have && hasNewWaiting(d.fleet, d.next)) d.newWaiting = true;
      d.next.serverTime = d.fleet.serverTime;
      d.fleet = d.next;
      memcpy(d.known, d.nextKnown, sizeof d.known);
      d.knownCount = d.nextKnownCount;
      d.inSnapshot = false;
      d.have = true;
      return true;

    default:
      return false;
  }
}

// The board's Hello and Subscribe: a 320 x 172 colour screen, one button,
// every agent with the fields above. The caller adds the transport and callbacks.
constexpr const char* kPanelType = "waveshare-lcd-147";

// What a board says about itself in Hello.
struct PanelSpec {
  const char* type;
  uint32_t width, height;
  bool touch;          // a touch screen besides the BOOT button
};

static inline void panelConfig(deck_link_config_t& cfg, const char* boardId, const char* firmware,
                               const PanelSpec& spec) {
  cfg.board_id = boardId;
  cfg.type = spec.type;
  cfg.firmware = firmware;
  cfg.display_width = spec.width;
  cfg.display_height = spec.height;
  cfg.display_color = true;
  cfg.input_count = 0;
  if (spec.touch) cfg.inputs[cfg.input_count++] = agentsdeck_link_v1_Input_INPUT_TOUCH;
  cfg.inputs[cfg.input_count++] = agentsdeck_link_v1_Input_INPUT_BUTTON;
  cfg.first_slot = 0;
  cfg.slot_count = 0;   // every agent; the board keeps the kMaxAgents lowest slots
  cfg.fields = kPanelFields;
}

// The 1.47" board: 320 x 172, the BOOT button.
static inline void panelConfig(deck_link_config_t& cfg, const char* boardId, const char* firmware) {
  panelConfig(cfg, boardId, firmware, PanelSpec{kPanelType, 320, 172, false});
}

// The screen's link state from the kit's.
static inline LinkState linkStateOf(deck_link_state_t s, bool approved) {
  switch (s) {
    case DECK_LINK_ONLINE:  return approved ? LinkState::Live : LinkState::Unapproved;
    case DECK_LINK_OFFLINE: return LinkState::Lost;
    default:                return LinkState::Connecting;
  }
}
