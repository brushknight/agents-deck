// deck_model.h: Deck Link agents and frames into the board's Fleet.
#include <assert.h>

#include <string>
#include <vector>

#include "../sim/fixture.h"
#include "crc16.h"
#include "deck_model.h"
#include "pb_decode.h"
#include "pb_encode.h"

static agentsdeck_link_v1_Agent la(const char* id, uint32_t slot, agentsdeck_link_v1_Status st, int64_t sinceMs = 0) {
  agentsdeck_link_v1_Agent a;
  memset(&a, 0, sizeof a);
  snprintf(a.id, sizeof a.id, "%s", id);
  a.slot = slot;
  a.status = st;
  a.status_since_ms = sinceMs;
  snprintf(a.title, sizeof a.title, "t-%s", id);
  return a;
}

static agentsdeck_link_v1_Frame changed(const agentsdeck_link_v1_Agent& a) {
  agentsdeck_link_v1_Frame f;
  memset(&f, 0, sizeof f);
  f.which_body = agentsdeck_link_v1_Frame_agent_changed_tag;
  f.body.agent_changed.has_agent = true;
  f.body.agent_changed.agent = a;
  return f;
}

static agentsdeck_link_v1_Frame body(pb_size_t which) {
  agentsdeck_link_v1_Frame f;
  memset(&f, 0, sizeof f);
  f.which_body = which;
  return f;
}

static agentsdeck_link_v1_Frame removed(const char* id) {
  agentsdeck_link_v1_Frame f = body(agentsdeck_link_v1_Frame_agent_removed_tag);
  snprintf(f.body.agent_removed.id, sizeof f.body.agent_removed.id, "%s", id);
  return f;
}

static void snapshot(DeckFleet& d, const std::vector<agentsdeck_link_v1_Agent>& as, int64_t timeMs = 0, uint32_t ms = 0) {
  agentsdeck_link_v1_Frame b = body(agentsdeck_link_v1_Frame_snapshot_begin_tag);
  b.body.snapshot_begin.has_server = true;
  snprintf(b.body.snapshot_begin.server.name, sizeof b.body.snapshot_begin.server.name, "deck");
  b.body.snapshot_begin.server.time_ms = timeMs;
  deckApply(d, b, ms);
  for (const auto& a : as) deckApply(d, changed(a), ms);
  deckApply(d, body(agentsdeck_link_v1_Frame_snapshot_end_tag), ms);
}

// ---- the kit's wire: a fake serial line --------------------------------------

static std::vector<uint8_t> tx;
static uint32_t clockMs = 1000;
static DeckFleet wired;

static size_t lineWrite(const uint8_t* d, size_t n, void*) { tx.insert(tx.end(), d, d + n); return n; }
static uint32_t lineMillis(void*) { return clockMs; }
static void lineFrame(deck_link_t*, const agentsdeck_link_v1_Frame* f) { deckApply(wired, *f, clockMs); }

// Encodes a deck→board frame exactly as the deck does: COBS(magic, version, protobuf, CRC) 0x00.
static std::vector<uint8_t> wire(const agentsdeck_link_v1_Frame& f) {
  static uint8_t raw[8192], out[8300];
  raw[0] = 0xDE; raw[1] = 0xC1; raw[2] = 1;
  pb_ostream_t s = pb_ostream_from_buffer(raw + 3, sizeof raw - 5);
  assert(pb_encode(&s, agentsdeck_link_v1_Frame_fields, &f));
  size_t n = 3 + s.bytes_written;
  const uint16_t crc = deck_crc16(raw, n);
  raw[n++] = (uint8_t)(crc >> 8);
  raw[n++] = (uint8_t)crc;
  size_t m = deck_cobs_encode(raw, n, out);
  out[m++] = 0;
  return std::vector<uint8_t>(out, out + m);
}

// The frames the board wrote, decoded like the deck does.
static std::vector<agentsdeck_link_v1_Frame> sent() {
  std::vector<agentsdeck_link_v1_Frame> out;
  size_t start = 0;
  for (size_t i = 0; i < tx.size(); i++) {
    if (tx[i]) continue;
    static uint8_t raw[8192];
    size_t k = 0;
    if (deck_cobs_decode(tx.data() + start, i - start, raw, &k) && k >= 5) {
      assert(raw[0] == 0xDE && raw[1] == 0xC1 && raw[2] == 1);
      assert(deck_crc16(raw, k - 2) == (uint16_t)(raw[k - 2] << 8 | raw[k - 1]));
      agentsdeck_link_v1_Frame f;
      memset(&f, 0, sizeof f);
      pb_istream_t s = pb_istream_from_buffer(raw + 3, k - 5);
      assert(pb_decode(&s, agentsdeck_link_v1_Frame_fields, &f));
      out.push_back(f);
    }
    start = i + 1;
  }
  tx.clear();
  return out;
}

int main() {
  // ---- one agent, field for field
  {
    agentsdeck_link_v1_Agent in = la("k3f9a2", 3, agentsdeck_link_v1_Status_STATUS_WAITING, 1791421323456);
    snprintf(in.title, sizeof in.title, "checkout\napi");
    snprintf(in.ai_title, sizeof in.ai_title, "Fix the race");
    in.tool = agentsdeck_link_v1_Tool_TOOL_CODEX;
    snprintf(in.model_label, sizeof in.model_label, "a-long-model-label-xy");
    in.has_activity = true;
    snprintf(in.activity.tool, sizeof in.activity.tool, "Edit");
    snprintf(in.activity.detail, sizeof in.activity.detail, "src/a.ts");
    in.has_waiting = true;
    in.waiting.kind = agentsdeck_link_v1_PromptKind_PROMPT_PERMISSION;
    snprintf(in.waiting.title, sizeof in.waiting.title, "run this command?");
    snprintf(in.waiting.detail, sizeof in.waiting.detail, "make test");
    snprintf(in.error, sizeof in.error, "boom");
    in.has_context = true;
    in.context.used = 410000;
    in.context.window = 1000000;
    in.cost_usd = 4.12;
    in.turns = 27;
    in.unseen = true;
    in.external = true;
    in.subagent_count = 5;
    for (int i = 0; i < 5; i++) {
      agentsdeck_link_v1_Subagent& sa = in.subagents[in.subagents_count++];
      snprintf(sa.title, sizeof sa.title, "task %d\nsecond line", i);
      snprintf(sa.type, sizeof sa.type, "Explore");
      snprintf(sa.tool, sizeof sa.tool, i ? "Grep" : "");
    }
    in.has_hat = true;
    snprintf(in.hat.shape, sizeof in.hat.shape, "propeller");
    snprintf(in.hat.color, sizeof in.hat.color, "blue");
    in.hat.automatic = true;
    snprintf(in.effort, sizeof in.effort, "xhigh");

    Agent a;
    agentFromLink(in, a);
    assert(!strcmp(a.effort, "xhigh"));
    assert(!strcmp(a.id, "k3f9a2") && a.slot == 3);
    assert(!strcmp(a.title, "checkout api"));                  // one line
    assert(!strcmp(a.aiTitle, "Fix the race"));
    assert(a.species == Species::Codex && a.status == Status::Waiting);
    assert(a.statusSince == 1791421323);
    assert(strlen(a.model) == sizeof a.model - 1);             // clipped to the board's buffer
    assert(!strcmp(a.actTool, "Edit") && !strcmp(a.actDetail, "src/a.ts"));
    assert(!strcmp(a.waitKind, "permission") && !strcmp(a.waitTitle, "run this command?"));
    assert(!strcmp(a.waitDetail, "make test") && !strcmp(a.errMsg, "boom"));
    assert(a.ctxUsed == 410000 && a.ctxWindow == 1000000);
    assert(a.costUsd > 4.11f && a.costUsd < 4.13f && a.turns == 27);
    assert(a.unseen && a.external && a.subagents == 5);
    // The first kMaxSubs subagents, each on one line; the count stays whole.
    assert(a.subCount == kMaxSubs && kMaxSubs == 3);
    assert(!strcmp(a.subs[0].title, "task 0 second line") && !strcmp(a.subs[0].type, "Explore") && !a.subs[0].tool[0]);
    assert(!strcmp(a.subs[2].title, "task 2 second line") && !strcmp(a.subs[2].tool, "Grep"));
    assert(!strcmp(kHatShapes[a.hat - 1], "propeller") && !strcmp(kHatColors[a.hatColor], "blue"));

    // A pick-any question step: ticked answers read "[x] …" on every layout.
    in.waiting.kind = agentsdeck_link_v1_PromptKind_PROMPT_QUESTION;
    in.waiting.options_count = 3;
    snprintf(in.waiting.options[0].key, sizeof in.waiting.options[0].key, "1");
    snprintf(in.waiting.options[0].label, sizeof in.waiting.options[0].label, "cheese");
    in.waiting.options[0].selected = true;
    snprintf(in.waiting.options[1].key, sizeof in.waiting.options[1].key, "2");
    snprintf(in.waiting.options[1].label, sizeof in.waiting.options[1].label, "ham");
    snprintf(in.waiting.options[2].key, sizeof in.waiting.options[2].key, "3");
    snprintf(in.waiting.options[2].label, sizeof in.waiting.options[2].label, "next — review your answers");
    in.waiting.options[2].primary = true;
    agentFromLink(in, a);
    assert(a.optCount == 3 && a.opts[0].selected && !strcmp(a.opts[0].label, "[x] cheese"));
    assert(!a.opts[1].selected && !strcmp(a.opts[1].label, "ham") && a.opts[2].primary);

    // No hat, an unknown shape, an unknown colour; a Claude agent with nothing else.
    agentsdeck_link_v1_Agent b = la("b", 0, agentsdeck_link_v1_Status_STATUS_IDLE);
    agentFromLink(b, a);
    assert(!a.subagents && !a.subCount);
    assert(a.hat == 0 && a.hatColor == kHatInk && a.species == Species::Claude && a.statusSince == 0);
    assert(!a.ctxWindow && !a.waitKind[0] && !a.actTool[0]);
    b.has_hat = true;
    snprintf(b.hat.shape, sizeof b.hat.shape, "sombrero");
    snprintf(b.hat.color, sizeof b.hat.color, "blue");
    agentFromLink(b, a);
    assert(a.hat == 0);
    snprintf(b.hat.shape, sizeof b.hat.shape, "bandana");
    snprintf(b.hat.color, sizeof b.hat.color, "mauve");
    agentFromLink(b, a);
    assert(a.hat == kHatShapeCount && a.hatColor == kHatInk);
    b.status = agentsdeck_link_v1_Status_STATUS_UNSPECIFIED;
    agentFromLink(b, a);
    assert(a.status == Status::Unknown);
  }

  // ---- the sample fleet of the repository, as the deck sends it
  {
    static DeckFleet d;
    assert(fixture::load(FIXTURE, d, 500));
    const Fleet& f = d.fleet;
    assert(f.count == 11 && f.total == 11);
    assert(strcmp(f.server, "workstation") == 0);
    assert(f.serverTime > 1700000000 && deckNowEpoch(d, 500) == f.serverTime);
    for (int i = 1; i < f.count; i++) assert(f.agents[i - 1].slot < f.agents[i].slot);
    assert(f.agents[f.count - 1].slot == 15);

    const Agent& a0 = f.agents[0];
    assert(strcmp(a0.title, "checkout-api") == 0);
    assert(a0.status == Status::Running && a0.species == Species::Claude);
    assert(strcmp(a0.actTool, "Edit") == 0 && a0.actDetail[0]);
    assert(a0.ctxUsed == 410000 && a0.ctxWindow == 1000000);
    assert(a0.subagents == 3 && a0.subCount == 3);
    assert(!strcmp(a0.subs[0].title, "map the payment retry paths") && !strcmp(a0.subs[0].type, "Explore"));
    assert(!strcmp(a0.subs[1].tool, "Edit"));
    assert(a0.statusSince > 0 && a0.turns > 0 && a0.costUsd > 0);
    assert(strcmp(a0.model, "opus 5.5") == 0);

    const Agent& a1 = f.agents[1];
    assert(a1.status == Status::Waiting);
    assert(a1.waitTitle[0] && a1.waitDetail[0] && strcmp(a1.waitKind, "permission") == 0);

    const Agent& a2 = f.agents[2];
    assert(a2.species == Species::Codex && a2.status == Status::Idle && a2.unseen);
    assert(f.agents[4].status == Status::Error && f.agents[4].errMsg[0]);

    assert(!strcmp(kHatShapes[a0.hat - 1], "propeller") && !strcmp(kHatColors[a0.hatColor], "blue"));
    assert(!strcmp(kHatShapes[a1.hat - 1], "cap") && !strcmp(kHatColors[a1.hatColor], "yellow"));
    assert(!strcmp(kHatShapes[f.agents[4].hat - 1], "top hat") && !strcmp(kHatShapes[f.agents[8].hat - 1], "beret"));
    for (int i = 0; i < f.count; i++) assert(f.agents[i].hat >= 1 && f.agents[i].hat <= kHatShapeCount);

    // The hero is a waiting agent, as with the JSON stream.
    const int h = autoHero(f);
    assert(h >= 0 && f.agents[h].status == Status::Waiting);
  }

  // ---- more agents than the board keeps, out of slot order
  {
    static DeckFleet d;
    std::vector<agentsdeck_link_v1_Agent> as;
    for (int i = 19; i >= 0; i--) {
      char id[16];
      snprintf(id, sizeof id, "a%d", i);
      as.push_back(la(id, (uint32_t)i, agentsdeck_link_v1_Status_STATUS_IDLE));
    }
    snapshot(d, as);
    assert(d.have && d.fleet.count == 16 && d.fleet.total == 20);
    for (int i = 0; i < 16; i++) assert(d.fleet.agents[i].slot == i);
    deckApply(d, removed("a19"), 0);                          // not on the board, still counted
    assert(d.fleet.count == 16 && d.fleet.total == 19);
  }

  // ---- updates after the snapshot, the hold and the hero
  {
    static DeckFleet d;
    ViewState v;
    snapshot(d, {la("a", 0, agentsdeck_link_v1_Status_STATUS_RUNNING, 1000000),
                 la("b", 1, agentsdeck_link_v1_Status_STATUS_IDLE, 2000000),
                 la("c", 2, agentsdeck_link_v1_Status_STATUS_RUNNING, 3000000)},
             1791421323000, 100);
    assert(!d.newWaiting && d.fleet.count == 3 && !strcmp(d.fleet.server, "deck"));
    assert(!strcmp(d.fleet.agents[autoHero(d.fleet)].id, "c"));   // equal rank: the latest change

    pressNext(d.fleet, v, 200);                                // the button holds "a"
    assert(v.hold && !strcmp(d.fleet.agents[resolveHero(d.fleet, v, 201)].id, "a"));

    // b finishes unreviewed: it outranks the runners but the hold stays.
    agentsdeck_link_v1_Agent b = la("b", 1, agentsdeck_link_v1_Status_STATUS_IDLE, 4000000);
    b.unseen = true;
    assert(deckApply(d, changed(b), 300));
    assert(!d.newWaiting && !strcmp(d.fleet.agents[resolveHero(d.fleet, v, 301)].id, "a"));
    assert(!strcmp(d.fleet.agents[autoHero(d.fleet)].id, "b"));

    // c starts waiting: a new prompt ends the hold.
    agentsdeck_link_v1_Agent c = la("c", 2, agentsdeck_link_v1_Status_STATUS_WAITING, 5000000);
    assert(deckApply(d, changed(c), 400));
    assert(d.newWaiting);
    d.newWaiting = false;
    deckApply(d, changed(c), 450);                             // the same prompt again: nothing new
    assert(!d.newWaiting);

    // A move (a and c swap slots) reorders; a new agent appears; a removal shrinks the fleet.
    deckApply(d, changed(la("a", 2, agentsdeck_link_v1_Status_STATUS_RUNNING, 1000000)), 500);
    c.slot = 0;
    deckApply(d, changed(c), 500);
    assert(d.fleet.count == 3 && !strcmp(d.fleet.agents[0].id, "c") && !strcmp(d.fleet.agents[1].id, "b") &&
           !strcmp(d.fleet.agents[2].id, "a"));
    deckApply(d, changed(la("n", 7, agentsdeck_link_v1_Status_STATUS_STARTING)), 600);
    assert(d.fleet.count == 4 && d.fleet.total == 4 && !strcmp(d.fleet.agents[3].id, "n"));
    deckApply(d, removed("a"), 700);
    assert(d.fleet.count == 3 && d.fleet.total == 3 && findAgent(d.fleet, "a") < 0);
    deckApply(d, removed("nobody"), 700);
    assert(d.fleet.count == 3);

    // A new snapshot replaces the fleet: agents not in it are gone.
    snapshot(d, {la("c", 0, agentsdeck_link_v1_Status_STATUS_WAITING, 5000000)}, 1791421400000, 800);
    assert(d.fleet.count == 1 && d.fleet.total == 1 && !d.newWaiting);   // c was waiting already
    snapshot(d, {la("z", 0, agentsdeck_link_v1_Status_STATUS_WAITING)}, 1791421400000, 800);
    assert(d.newWaiting);

    // The clock: the snapshot's time plus millis(), corrected by every Ping.
    assert(deckNowEpoch(d, 800) == 1791421400);
    assert(deckNowEpoch(d, 800 + 61500) == 1791421461);
    agentsdeck_link_v1_Frame p = body(agentsdeck_link_v1_Frame_ping_tag);
    p.body.ping.time_ms = 1791421500000;
    assert(!deckApply(d, p, 900));
    assert(deckNowEpoch(d, 900) == 1791421500 && d.fleet.serverTime == 1791421500);
    assert(deckNowEpoch(d, 900 + 2000) == 1791421502);
    static DeckFleet none;
    assert(deckNowEpoch(none, 5000) == 0);
  }

  // ---- link states for the screen
  assert(linkStateOf(DECK_LINK_CONNECTING, false) == LinkState::Connecting);
  assert(linkStateOf(DECK_LINK_ONLINE, false) == LinkState::Unapproved);
  assert(linkStateOf(DECK_LINK_ONLINE, true) == LinkState::Live);
  assert(linkStateOf(DECK_LINK_OFFLINE, true) == LinkState::Lost);

  // ---- the whole path: the kit's decoder, real wire bytes, the adapter
  {
    static deck_link_t link;
    deck_link_config_t cfg;
    memset(&cfg, 0, sizeof cfg);
    panelConfig(cfg, "AA:BB:CC:00:11:22", "test");
    cfg.write = lineWrite;
    cfg.millis = lineMillis;
    cfg.on_frame = lineFrame;
    deck_link_init(&link, &cfg);
    deck_link_begin(&link);

    // Hello says what the board is.
    std::vector<agentsdeck_link_v1_Frame> out = sent();
    assert(out.size() == 1 && out[0].which_body == agentsdeck_link_v1_Frame_hello_tag);
    const agentsdeck_link_v1_Hello& h = out[0].body.hello;
    assert(!strcmp(h.type, "waveshare-lcd-147") && !strcmp(h.board_id, "AA:BB:CC:00:11:22"));
    assert(h.has_display && h.display.width == 320 && h.display.height == 172 && h.display.color);
    assert(h.inputs_count == 1 && h.inputs[0] == agentsdeck_link_v1_Input_INPUT_BUTTON);

    // An approved Welcome: the board subscribes to every agent with the panel's fields.
    std::vector<uint8_t> stream;
    auto add = [&](const agentsdeck_link_v1_Frame& f) {
      std::vector<uint8_t> w = wire(f);
      stream.insert(stream.end(), w.begin(), w.end());
    };
    agentsdeck_link_v1_Frame w = body(agentsdeck_link_v1_Frame_welcome_tag);
    w.body.welcome.approved = true;
    w.body.welcome.scope = agentsdeck_link_v1_Scope_SCOPE_VIEW;
    snprintf(w.body.welcome.device_name, sizeof w.body.welcome.device_name, "waveshare-lcd-147-001122");
    add(w);
    // Boot-log noise, then the fixture's snapshot and a ping, fed in small pieces.
    const char* noise = "ESP-ROM:esp32s3-20210327\r\nentry 0x403c98d0\r\n";
    stream.insert(stream.end(), noise, noise + strlen(noise));
    std::vector<agentsdeck_link_v1_Frame> frames;
    assert(fixture::snapshotFrames(fixture::slurp(FIXTURE), frames));
    for (const auto& f : frames) add(f);
    agentsdeck_link_v1_Frame p = body(agentsdeck_link_v1_Frame_ping_tag);
    p.seq = 9;
    p.body.ping.time_ms = 1791421500000;
    add(p);
    for (size_t i = 0; i < stream.size(); i += 7)
      deck_link_feed(&link, stream.data() + i, stream.size() - i < 7 ? stream.size() - i : 7);

    assert(deck_link_online(&link) && deck_link_approved(&link));
    assert(wired.have && wired.fleet.count == 11 && !strcmp(wired.fleet.server, "workstation"));
    assert(wired.fleet.agents[autoHero(wired.fleet)].status == Status::Waiting);
    assert(deckNowEpoch(wired, clockMs) == 1791421500);

    out = sent();
    assert(out.size() == 2);
    assert(out[0].which_body == agentsdeck_link_v1_Frame_subscribe_tag);
    assert(out[0].body.subscribe.slot_count == 0 && out[0].body.subscribe.fields == 15);
    assert(out[1].which_body == agentsdeck_link_v1_Frame_pong_tag && out[1].seq == 9);

    // 15 s of silence: deck offline, the fleet stays, and the board only says Hello.
    clockMs += 15000;
    deck_link_poll(&link);
    assert(linkStateOf(deck_link_state(&link), deck_link_approved(&link)) == LinkState::Lost);
    assert(wired.fleet.count == 11);
    for (const auto& f : sent()) assert(f.which_body == agentsdeck_link_v1_Frame_hello_tag);
  }

  printf("ok  adapter: %d bytes per DeckFleet\n", (int)sizeof(DeckFleet));
  return 0;
}
