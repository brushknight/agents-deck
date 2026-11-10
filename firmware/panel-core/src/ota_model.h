#pragma once
// Firmware updates over Wi-Fi, the parts without the chip: what the screen
// says, how often the deck hears about progress, and what a board does at
// boot after an update. Pure C++ (no Arduino): ota.h drives it on the board,
// tests/test_net.cpp on the host. docs/link/protocol.md, "Firmware updates
// over Wi-Fi", has the flow.
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// What the screen shows while a board updates.
enum class OtaPhase : uint8_t {
  None,         // no update: the usual screens
  Downloading,  // percent
  Verifying,    // the image is in; checking it
  Restarting,   // checked: restarting into it
  Failed,       // error says why; shown for a few seconds, the board keeps its firmware
};

struct OtaView {
  OtaPhase phase = OtaPhase::None;
  uint8_t percent = 0;
  char version[24] = "";
  char error[48] = "";
};

// Tell the deck again after this many percent or this long, whichever first.
constexpr uint8_t kOtaReportStep = 5;
constexpr uint32_t kOtaReportMs = 3000;

static inline bool otaReportDue(uint8_t lastPct, uint32_t lastMs, uint8_t pct, uint32_t now) {
  return pct >= lastPct + kOtaReportStep || (pct != lastPct && now - lastMs >= kOtaReportMs) || pct == 100;
}

// ---------------------------------------------------------------- after the restart

// The new firmware keeps itself only once it reaches the deck (a Welcome)
// within this long; otherwise it goes back to the old one.
constexpr uint32_t kOtaConfirmMs = 5u * 60u * 1000u;
// Without the bootloader's rollback (see ota.h): after this many starts
// without reaching the deck (it keeps crashing), go back at once.
constexpr uint8_t kOtaMaxBoots = 3;

// What the board remembers in NVS before it restarts into an update, for
// going back without the bootloader's help.
struct OtaMark {
  uint8_t pending = 0;   // 1: the running firmware is an update not yet confirmed
  uint8_t boots = 0;     // starts since the update
  char prev[17] = "";    // the old firmware's slot ("app0")
  char next[17] = "";    // the update's slot ("app1"): the mark is about it only
};

enum class OtaBoot : uint8_t {
  Normal,             // nothing pending
  PendingBootloader,  // the bootloader waits for a confirm (PENDING_VERIFY): it rolls back on a reset
  PendingApp,         // no bootloader rollback: the app counts starts and goes back itself
  RevertNow,          // started too often without reaching the deck: go back now
};

// At boot. pendingVerify: the running slot's state is PENDING_VERIFY (the
// bootloader's rollback is on). m: the NVS mark (boots not yet counted).
// running: the running slot's label. A mark for another slot means the board
// is back on the old firmware (the bootloader went back): Normal, and the
// caller drops the mark.
static inline OtaBoot otaBootDecision(bool pendingVerify, const OtaMark& m, const char* running) {
  if (pendingVerify) return OtaBoot::PendingBootloader;
  if (!m.pending || !m.prev[0] || strcmp(m.next, running) != 0) return OtaBoot::Normal;
  if (m.boots + 1 > kOtaMaxBoots) return OtaBoot::RevertNow;
  return OtaBoot::PendingApp;
}

// While pending: time to give up?
static inline bool otaGiveUp(uint32_t since, uint32_t now) { return now - since >= kOtaConfirmMs; }

// Is mark (len bytes, its NUL included) somewhere in the first size bytes of
// an image read through read(offset, to, n)? buf holds a chunk plus len - 1
// bytes carried over, so a mark split across two reads is found.
template <typename Read>
static inline bool otaFindMark(const char* mark, size_t len, uint32_t size, uint8_t* buf, size_t cap, Read read) {
  if (len == 0 || cap < 2 * len) return false;
  const uint32_t chunk = (uint32_t)(cap - (len - 1));
  size_t keep = 0;
  for (uint32_t off = 0; off < size;) {
    const uint32_t n = size - off < chunk ? size - off : chunk;
    if (!read(off, buf + keep, n)) return false;
    const size_t have = keep + n;
    for (size_t i = 0; i + len <= have; i++)
      if (buf[i] == (uint8_t)mark[0] && memcmp(buf + i, mark, len) == 0) return true;
    keep = have < len - 1 ? have : len - 1;
    memmove(buf, buf + have - keep, keep);
    off += n;
  }
  return false;
}

// ---------------------------------------------------------------- words

// "updating" with a line or two, for the notices (net_model.h's netNotice).
struct OtaWords {
  const char* title;
  const char* line1;
  const char* line2;
  bool problem;
};

static inline OtaWords otaWords(const OtaView& v, char* buf, size_t cap) {
  switch (v.phase) {
    case OtaPhase::Downloading:
      if (v.version[0]) snprintf(buf, cap, "%s \xC2\xB7 %u%%", v.version, (unsigned)v.percent);
      else snprintf(buf, cap, "%u%%", (unsigned)v.percent);
      return {"updating", buf, "keep it powered", false};
    case OtaPhase::Verifying:  return {"updating", "checking\xE2\x80\xA6", "keep it powered", false};
    case OtaPhase::Restarting: return {"updating", "restarting\xE2\x80\xA6", nullptr, false};
    case OtaPhase::Failed:     return {"update failed", v.error[0] ? v.error : nullptr, "kept this firmware", true};
    default:                   return {nullptr, nullptr, nullptr, false};
  }
}
