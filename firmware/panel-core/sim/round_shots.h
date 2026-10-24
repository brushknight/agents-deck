#pragma once
// Host only: every screen of a round layout (layout_orbit.h, layout_dial.h),
// drawn from the sample fleet, for sim/render.cpp. Needs PANEL_MAX_AGENTS of
// at least 22 (the 22-agent orbit).
#include <string>
#include <vector>

#include "fixture.h"
#include "layout_dial.h"
#include "layout_orbit.h"
#include "round_selftest.h"

namespace round_shots {

static inline int idx(const Fleet& f, const char* id) { return findAgent(f, id); }

// A fleet of the given agents of `src`, in their slot order.
static inline Fleet pick(const Fleet& src, std::initializer_list<const char*> ids) {
  Fleet f;
  memset(&f, 0, sizeof f);
  snprintf(f.server, sizeof f.server, "%s", src.server);
  f.serverTime = src.serverTime;
  for (const char* id : ids) {
    const int i = idx(src, id);
    if (i >= 0) deck_model_detail::upsert(f, src.agents[i]);
  }
  f.total = f.count;
  return f;
}

// The sample fleet doubled: 22 agents (slots 0-21), the copies calm.
static inline Fleet big(const Fleet& src) {
  Fleet f;
  memset(&f, 0, sizeof f);
  snprintf(f.server, sizeof f.server, "%s", src.server);
  f.serverTime = src.serverTime;
  static const char* const kNames[] = {"ledger-sync", "search-ui", "pdf-export", "ios-push", "rate-limit", "cdn-purge",
                                       "audit-log", "i18n", "perf-bench", "e2e-suite", "data-mig"};
  int slot = 0;
  for (int i = 0; i < src.count; i++) {
    Agent a = src.agents[i];
    a.slot = (uint8_t)slot++;
    deck_model_detail::upsert(f, a);
  }
  for (int i = 0; i < src.count && i < 11; i++) {
    Agent a = src.agents[i];
    snprintf(a.id, sizeof a.id, "x%02d", i);
    snprintf(a.title, sizeof a.title, "%s", kNames[i]);
    a.slot = (uint8_t)slot++;
    if (a.status == Status::Waiting) a.status = Status::Running;
    a.optCount = 0;
    deck_model_detail::upsert(f, a);
  }
  f.total = f.count;
  return f;
}

static inline void writePpm(const std::string& path, const Canvas& c) {
  std::ofstream out(path, std::ios::binary);
  out << "P6\n" << c.w << " " << c.h << "\n255\n";
  for (int i = 0; i < c.w * c.h; i++) {
    const uint16_t p = c.px[i];
    const unsigned char rgb[3] = {(unsigned char)(((p >> 11) & 31) * 255 / 31), (unsigned char)(((p >> 5) & 63) * 255 / 63),
                                  (unsigned char)((p & 31) * 255 / 31)};
    out.write((const char*)rgb, 3);
  }
}

// The sample fleet has no reasoning effort; the detail shows it next to the model.
static inline void withEffort(Fleet& f) {
  static const char* const kEffort[] = {"high", "medium", "xhigh"};
  for (int i = 0; i < f.count; i++)
    if (!f.agents[i].effort[0] && f.agents[i].species == Species::Claude)
      snprintf(f.agents[i].effort, sizeof f.agents[i].effort, "%s", kEffort[i % 3]);
}

// Every screen of one round layout into dir/<name>.ppm.
static inline int render(const std::string& dir, const RoundLayout& L, const char* device, const Fleet& all,
                         uint32_t ms, int64_t now) {
  std::vector<uint16_t> buf(480 * 480);
  Canvas c{buf.data(), 480, 480};
  RoundUi ui;
  static Fleet f;
  RoundInput in{&f, LinkState::Live, device, RoundScope::Admin, ms, now, &ui, 120};
  auto shot = [&](const char* name) {
    roundSync(ui, L, f, ms);
    ui.wake = false;
    L.draw(c, in);
    writePpm(dir + "/" + name + ".ppm", c);
  };
  auto fresh = [&](const Fleet& next) {
    f = next;
    withEffort(f);
    ui = RoundUi();
    // Agents already waiting are not news: the shot shows the plain priority order.
    for (int i = 0; i < f.count && ui.waitCount < kRoundWaitIds; i++)
      if (f.agents[i].status == Status::Waiting) snprintf(ui.waitIds[ui.waitCount++], 12, "%s", f.agents[i].id);
  };

  // Home: calm with three, busy with eight (two waiting), a crowd of 22.
  fresh(pick(all, {"k3f9a2", "d0cs1t", "t3st5a"}));
  shot("home-3");
  fresh(pick(all, {"k3f9a2", "h0m3l4", "d0cs1t", "t3st5a", "1nfr4t", "k8sn0d", "4uthfl", "h4ptic"}));
  shot("home-8");
  fresh(big(all));
  shot("home-22");
  ui.page = 1;
  shot("home-22-page2");

  // One agent starts waiting: home wakes with it featured.
  fresh(pick(all, {"k3f9a2", "d0cs1t", "t3st5a"}));
  {
    Fleet next = pick(all, {"k3f9a2", "h0m3l4", "d0cs1t", "t3st5a"});
    f = next;
  }
  shot("waiting");

  // The detail of a running agent, with stop held half way; an idle one; the prompt.
  fresh(all);
  roundOpen(ui, f, idx(f, "k3f9a2"), RoundScreen::Detail);
  shot("detail");
  ui.down = true;
  ui.downMs = ms - 500;
  ui.pressed.kind = HitKind::Stop;
  shot("detail-stop-held");
  ui.down = false;
  ui.pressed = RoundHit();
  roundOpen(ui, f, idx(f, "d0cs1t"), RoundScreen::Detail);
  shot("detail-idle");
  in.scope = RoundScope::View;
  shot("detail-view");
  in.scope = RoundScope::Admin;
  roundOpen(ui, f, idx(f, "h0m3l4"), RoundScreen::Prompt);
  shot("prompt");
  ui.pressed.kind = HitKind::Option;
  ui.pressed.option = 1;
  shot("prompt-pressed");
  ui.pressed = RoundHit();
  in.scope = RoundScope::View;
  shot("prompt-view");
  in.scope = RoundScope::Admin;
  roundOpen(ui, f, idx(f, "4uthfl"), RoundScreen::Prompt);
  shot("prompt-question");
  {
    // Two answers, then five (the list scrolls).
    static Fleet two;
    two = f;
    Agent& a = two.agents[idx(two, "h0m3l4")];
    a.optCount = 2;
    snprintf(a.opts[0].label, sizeof a.opts[0].label, "allow");
    snprintf(a.opts[1].label, sizeof a.opts[1].label, "deny");
    f = two;
    roundOpen(ui, f, idx(f, "h0m3l4"), RoundScreen::Prompt);
    shot("prompt-2");
    a.optCount = 5;
    static const char* const kMore[] = {"yes", "yes, always", "no", "no, and explain", "stop the agent"};
    for (int k = 0; k < 5; k++) {
      snprintf(a.opts[k].key, sizeof a.opts[k].key, "%d", k + 1);
      snprintf(a.opts[k].label, sizeof a.opts[k].label, "%s", kMore[k]);
      a.opts[k].primary = k == 0;
    }
    f = two;
    shot("prompt-5");
  }
  roundHome(ui);
  roundToast(ui, "sent", ms, 1000);
  shot("toast");
  ui.toast[0] = '\0';

  // The full-screen states.
  const struct { const char* name; LinkState link; bool empty; } kNotices[] = {
      {"connecting", LinkState::Connecting, false}, {"not-approved", LinkState::Unapproved, false},
      {"offline", LinkState::Lost, false},           {"no-agents", LinkState::Live, true},
  };
  for (const auto& n : kNotices) {
    in.link = n.link;
    if (n.empty) { memset(&f, 0, sizeof f); }
    shot(n.name);
  }
  return 0;
}

// The self-test's three pages, with a touch trail.
static inline int renderSelftest(const std::string& dir, const char* type, const char* found, uint32_t pclk) {
  std::vector<uint16_t> buf(480 * 480);
  Canvas c{buf.data(), 480, 480};
  SelftestState s;
  s.type = type;
  s.found = found;
  s.pclkHz = pclk;
  s.frameMs = 21;
  s.pushMs = 4;
  for (int k = 0; k <= 12; k++) selftestTouch(s, true, 150 + k * 15, 300 - (k * k) / 2, k * 16);
  selftestTouch(s, false, 0, 0, 300);
  for (int page = 0; page < kSelftestPages; page++) {
    s.page = page;
    drawSelftest(c, s, 1200);
    char name[32];
    snprintf(name, sizeof name, "/selftest-%d.ppm", page + 1);
    writePpm(dir + name, c);
  }
  return 0;
}

}  // namespace round_shots
