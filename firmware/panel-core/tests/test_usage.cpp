// usage.h and the usage parts of layout_big.h: Limits frames into the model
// (kept across a snapshot), the two sources of the corner, clock times in
// the deck's zone, the 24 px row and what is drawn in it, the card's row.
#include <assert.h>

#include <vector>

#include "deck_model.h"
#include "layout_big.h"
#include "usage.h"

static agentsdeck_link_v1_Frame frame(pb_size_t tag) {
  agentsdeck_link_v1_Frame f = agentsdeck_link_v1_Frame_init_zero;
  f.which_body = tag;
  return f;
}

static void addSet(agentsdeck_link_v1_Limits& l, agentsdeck_link_v1_Tool tool, const char* account, uint32_t used,
                   agentsdeck_link_v1_LimitLevel level, int64_t resetsMs) {
  agentsdeck_link_v1_LimitSet& s = l.sets[l.sets_count++];
  s.tool = tool;
  snprintf(s.account, sizeof s.account, "%s", account);
  s.windows_count = 1;
  snprintf(s.windows[0].label, sizeof s.windows[0].label, "week");
  s.windows[0].used = used;
  s.windows[0].level = level;
  s.windows[0].resets_at_ms = resetsMs;
}

static void addAgent(DeckFleet& d, const char* id, uint32_t slot, const char* account, const char* color,
                     agentsdeck_link_v1_Status st) {
  agentsdeck_link_v1_Frame f = frame(agentsdeck_link_v1_Frame_agent_changed_tag);
  f.body.agent_changed.has_agent = true;
  agentsdeck_link_v1_Agent& a = f.body.agent_changed.agent;
  snprintf(a.id, sizeof a.id, "%s", id);
  snprintf(a.title, sizeof a.title, "t-%s", id);
  a.slot = slot;
  a.tool = agentsdeck_link_v1_Tool_TOOL_CLAUDE;
  a.status = st;
  if (account[0]) {
    a.has_account = true;
    snprintf(a.account.name, sizeof a.account.name, "%s", account);
    snprintf(a.account.color, sizeof a.account.color, "%s", color);
  }
  deckApply(d, f, 0);
}

int main() {
  const int64_t now = 1795600000;   // a Wednesday, 09:46 UTC
  static DeckFleet d;

  // ---- the model: a snapshot with the deck's zone, then the limits
  {
    agentsdeck_link_v1_Frame b = frame(agentsdeck_link_v1_Frame_snapshot_begin_tag);
    b.body.snapshot_begin.has_server = true;
    b.body.snapshot_begin.server.time_ms = now * 1000;
    b.body.snapshot_begin.server.utc_offset_s = 3600;
    deckApply(d, b, 0);
    addAgent(d, "w1", 0, "work", "yellow", agentsdeck_link_v1_Status_STATUS_RUNNING);
    addAgent(d, "w2", 1, "work", "yellow", agentsdeck_link_v1_Status_STATUS_IDLE);
    addAgent(d, "p1", 2, "personal", "sky", agentsdeck_link_v1_Status_STATUS_IDLE);
    deckApply(d, frame(agentsdeck_link_v1_Frame_snapshot_end_tag), 0);
    assert(d.fleet.count == 3 && d.fleet.utcOffset == 3600 && d.fleet.limitCount == 0);

    agentsdeck_link_v1_Frame l = frame(agentsdeck_link_v1_Frame_limits_tag);
    addSet(l.body.limits, agentsdeck_link_v1_Tool_TOOL_CLAUDE, "personal", 22, agentsdeck_link_v1_LimitLevel_LIMIT_OK, 0);
    addSet(l.body.limits, agentsdeck_link_v1_Tool_TOOL_CLAUDE, "work", 86, agentsdeck_link_v1_LimitLevel_LIMIT_NEAR,
           (now + 2 * 86400) * 1000);
    addSet(l.body.limits, agentsdeck_link_v1_Tool_TOOL_CODEX, "", 7, agentsdeck_link_v1_LimitLevel_LIMIT_OK, 0);
    assert(deckApply(d, l, 0));
    assert(d.fleet.limitCount == 3 && d.fleet.limits[1].tool == ToolKind::Claude && !strcmp(d.fleet.limits[1].account, "work"));
    assert(d.fleet.limits[1].win[0].used == 86 && d.fleet.limits[1].win[0].hot() && !d.fleet.limits[1].win[0].out());
    assert(d.fleet.limits[1].win[0].resetsAt == now + 2 * 86400 && d.fleet.limits[2].tool == ToolKind::Codex);

    // A new snapshot keeps them: limits come in their own frames.
    deckApply(d, b, 0);
    addAgent(d, "w1", 0, "work", "yellow", agentsdeck_link_v1_Status_STATUS_RUNNING);
    deckApply(d, frame(agentsdeck_link_v1_Frame_snapshot_end_tag), 0);
    assert(d.fleet.count == 1 && d.fleet.limitCount == 3);
    addAgent(d, "w2", 1, "work", "yellow", agentsdeck_link_v1_Status_STATUS_IDLE);
    addAgent(d, "p1", 2, "personal", "sky", agentsdeck_link_v1_Status_STATUS_IDLE);
  }
  const Fleet& f = d.fleet;

  // ---- the corner's two: the account in use (most agents, busy first),
  // then the other set nearest its limit
  {
    int src[2];
    assert(usageCorner(f, src) == 2 && src[0] == 1);   // work: running agents
    assert(src[1] == 0);                                // personal 22% beats codex 7%
    Fleet none = f;
    none.limitCount = 0;
    assert(usageCorner(none, src) == 0);
    Fleet one = f;
    one.limitCount = 1;
    assert(usageCorner(one, src) == 1 && src[0] == 0);
    assert(!strcmp(usageName(f.limits[2]), "codex") && !strcmp(usageName(f.limits[1]), "work"));
  }

  // ---- clock times in the deck's zone, as resetText
  {
    char b[32];
    usageWhen(now + 3600, now, 0, b, sizeof b);
    assert(!strcmp(b, "10:46"));
    usageWhen(now + 3600, now, 3600, b, sizeof b);
    assert(!strcmp(b, "11:46"));
    usageWhen(now + 86400, now, 0, b, sizeof b);
    assert(!strcmp(b, "thu 09:46"));
    usageWhen(now + 13 * 86400, now, 0, b, sizeof b);
    assert(!strcmp(b, "8 dec"));
    LimitWin w{};
    w.used = 86;
    w.level = LimitLevel::Near;
    usageValue(w, true, now, 0, b, sizeof b);
    assert(!strcmp(b, "86%"));
    w.level = LimitLevel::Out;
    w.resetsAt = now + 86400;
    usageValue(w, true, now, 0, b, sizeof b);
    assert(!strcmp(b, "out" DECK_DOT "thu 09:46"));
    usageValue(w, false, now, 0, b, sizeof b);
    assert(!strcmp(b, "out"));
    w.stale = true;
    usageValue(w, true, now, 0, b, sizeof b);
    assert(!strcmp(b, "-"));
  }

  // ---- the row: 24 px at the bottom with one page; the bars' row with more
  const BigSpec S = specBig480();
  {
    const BigGrid plain = bigGrid(S, 1), usage = bigGrid(S, 1, false, true), paged = bigGrid(S, 2, false, true);
    assert(!plain.bars.w && usage.bars.y == 456 && usage.bars.h == 24);
    assert(usage.y0 + 4 * usage.cellH + 3 * usage.gap <= 456);
    assert(paged.bars.h == bk(S, 34));
    PanelUi ui;
    ui.usage = true;
    assert(bigGridFor(S, ui, 1).bars.h == 24);
  }

  // ---- drawn: the critter in work's colour at x 10, the % in orange; the
  // card's row with its segments orange and "ctx" at the right
  {
    std::vector<uint16_t> buf(S.w * S.h);
    Canvas c{buf.data(), S.w, S.h};
    const Theme T = deckTheme(false);
    PanelUi ui;
    ui.usage = true;
    PanelInput in{&f, LinkState::Live, "test", DeckScope::Admin, 0, now, &ui, 0};
    BigPanel P(S);
    P.draw(c, in);
    const int cy = 456 + 12;
    const uint16_t yellow = sprite_detail::kHatPalette[2][0];
    assert(buf[(cy - 4) * S.w + 12] == yellow);          // the critter's top row, second cell
    assert(buf[(cy - 4) * S.w + 10] != yellow);          // its first cell is empty
    bool orange = false;
    for (int x = 31; x < 70; x++)
      for (int y = cy - 6; y < cy + 6; y++) orange |= buf[y * S.w + x] == T.accent;
    assert(orange);                                      // 86%: past 80
    Fleet bare = f;
    bare.limitCount = 0;
    in.fleet = &bare;
    ui.usage = false;
    P.draw(c, in);
    assert(buf[(cy - 4) * S.w + 12] != yellow);          // no limits: no readout

    in.fleet = &f;
    ui.usage = true;
    bigOpen(ui, f, 0);
    P.draw(c, in);
    const BigCard g = bigCard(S, f.agents[0], DeckScope::Admin);
    int on = 0;
    for (int x = 0; x < S.w; x++) on += buf[(g.ctxY + big_ui::kLineXS / 2) * S.w + x] == T.accent;
    assert(on > 17 * 6);                                 // 17 of 20 segments, 6 px each
  }
  puts("usage: ok");
  return 0;
}
