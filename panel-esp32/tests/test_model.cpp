#include <assert.h>
#include <fstream>
#include <sstream>
#include <string>
#include "model.h"

static const char* kFixture = FIXTURE;

static std::string slurp(const char* path) {
  std::ifstream f(path);
  assert(f.good());
  std::stringstream ss;
  ss << f.rdbuf();
  return ss.str();
}

static std::string agentJson(int slot, const std::string& title) {
  return "{\"id\":\"a" + std::to_string(slot) + "\",\"slot\":" + std::to_string(slot) +
         ",\"title\":\"" + title + "\",\"tool\":\"claude\",\"status\":\"idle\"}";
}

int main() {
  DynamicJsonDocument doc(65536);
  static Fleet f;

  assert(!model_detail::stateFilter().overflowed());

  // The sample fleet shipped with agents-deck.
  {
    std::string text = slurp(kFixture);
    assert(parseState(text, f, doc));
    assert(f.count == 11 && f.total == 11);
    assert(strcmp(f.server, "workstation") == 0);
    assert(f.serverTime > 1700000000);
    for (int i = 1; i < f.count; i++) assert(f.agents[i - 1].slot < f.agents[i].slot);
    assert(f.agents[f.count - 1].slot == 15);

    const Agent& a0 = f.agents[0];
    assert(strcmp(a0.title, "checkout-api") == 0);
    assert(a0.status == Status::Running && a0.species == Species::Claude);
    assert(strcmp(a0.actTool, "Edit") == 0 && a0.actDetail[0]);
    assert(a0.ctxUsed == 410000 && a0.ctxWindow == 1000000);
    assert(a0.subagents == 3);
    assert(a0.statusSince > 0 && a0.turns > 0 && a0.costUsd > 0);
    assert(strcmp(a0.model, "opus 5.5") == 0);

    const Agent& a1 = f.agents[1];
    assert(a1.status == Status::Waiting);
    assert(a1.waitTitle[0] && a1.waitDetail[0] && strcmp(a1.waitKind, "permission") == 0);

    const Agent& a2 = f.agents[2];
    assert(a2.species == Species::Codex && a2.status == Status::Idle && a2.unseen);

    assert(f.agents[4].status == Status::Error && f.agents[4].errMsg[0]);

    // Hats: one per working folder, by name.
    assert(!strcmp(kHatShapes[a0.hat - 1], "propeller") && !strcmp(kHatColors[a0.hatColor], "blue"));
    assert(!strcmp(kHatShapes[a1.hat - 1], "cap") && !strcmp(kHatColors[a1.hatColor], "yellow"));
    assert(!strcmp(kHatShapes[f.agents[4].hat - 1], "top hat") && !strcmp(kHatShapes[f.agents[8].hat - 1], "beret"));
    for (int i = 0; i < f.count; i++) assert(f.agents[i].hat >= 1 && f.agents[i].hat <= kHatShapeCount);
  }

  // No hat, an unknown shape, an unknown colour, and an agent mirrored from another app.
  {
    std::string text = "{\"agents\":["
        "{\"id\":\"a\",\"slot\":0,\"status\":\"idle\"},"
        "{\"id\":\"b\",\"slot\":1,\"status\":\"idle\",\"hat\":{\"shape\":\"sombrero\",\"color\":\"blue\"}},"
        "{\"id\":\"c\",\"slot\":2,\"status\":\"idle\",\"external\":true,\"hat\":{\"shape\":\"bandana\",\"color\":\"mauve\"}}]}";
    assert(parseState(text, f, doc));
    assert(f.agents[0].hat == 0 && f.agents[1].hat == 0);
    assert(f.agents[2].hat == kHatShapeCount && f.agents[2].hatColor == kHatInk && f.agents[2].external);
  }

  // More agents than the board keeps, given out of slot order.
  {
    std::string text = "{\"server\":{\"name\":\"m\"},\"agents\":[";
    for (int i = 19; i >= 0; i--) text += agentJson(i, "t" + std::to_string(i)) + (i ? "," : "");
    text += "]}";
    assert(parseState(text, f, doc));
    assert(f.count == 16 && f.total == 20);
    for (int i = 0; i < 16; i++) assert(f.agents[i].slot == i);
    assert(strcmp(f.agents[15].title, "t15") == 0);
  }

  // A broken document leaves the previous fleet in place.
  {
    std::string good = "{\"server\":{\"name\":\"keep\"},\"agents\":[" + agentJson(3, "kept") + "]}";
    assert(parseState(good, f, doc));
    std::string cut = "{\"server\":{\"name\":\"x\"},\"agents\":[{\"id\":\"zz\"";
    assert(!parseState(cut, f, doc));
    std::string notState = "{\"error\":\"unauthorized\"}";
    assert(!parseState(notState, f, doc));
    assert(f.count == 1 && strcmp(f.server, "keep") == 0 && strcmp(f.agents[0].title, "kept") == 0);
  }

  // An empty fleet is valid.
  {
    std::string text = "{\"server\":{\"name\":\"m\",\"time\":\"2026-10-08T01:02:03Z\"},\"agents\":[]}";
    assert(parseState(text, f, doc));
    assert(f.count == 0 && f.total == 0 && f.serverTime == 1791421323);
  }

  // A long Cyrillic title is cut on a code-point boundary; newlines become spaces.
  {
    std::string title;
    for (int i = 0; i < 30; i++) title += "\xD0\x96";   // 30 x "Ж", 60 bytes
    std::string text = "{\"agents\":[{\"id\":\"u\",\"slot\":0,\"title\":\"" + title +
                       "\",\"status\":\"waiting\",\"waiting\":{\"detail\":\"line1\\nline2\"}}]}";
    assert(parseState(text, f, doc));
    const size_t n = strlen(f.agents[0].title);
    assert(n == 38);                                      // 19 whole characters fit in 39 bytes
    assert((unsigned char)f.agents[0].title[n - 1] == 0x96);
    assert(strcmp(f.agents[0].waitDetail, "line1 line2") == 0);
  }

  // Reading from a stream stops right after the object, as the SSE reader needs.
  {
    std::istringstream in("{\"agents\":[" + agentJson(1, "s") + "]}\n\ndata: next");
    assert(parseState(in, f, doc));
    assert(f.count == 1);
    std::string rest;
    std::getline(in, rest, '\0');
    assert(rest == "\n\ndata: next");
  }

  // The scratch document is too small: report failure, keep the fleet.
  {
    std::string big = slurp(kFixture);
    DynamicJsonDocument tiny(256);
    std::string keep = "{\"agents\":[" + agentJson(7, "seven") + "]}";
    assert(parseState(keep, f, doc));
    assert(!parseState(big, f, tiny));
    assert(f.count == 1 && f.agents[0].slot == 7);
  }
  return 0;
}
