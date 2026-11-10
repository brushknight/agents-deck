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
};

enum class OtaBoot : uint8_t {
  Normal,             // nothing pending
  PendingBootloader,  // the bootloader waits for a confirm (PENDING_VERIFY): it rolls back on a reset
  PendingApp,         // no bootloader rollback: the app counts starts and goes back itself
  RevertNow,          // started too often without reaching the deck: go back now
};

// At boot. pendingVerify: the running slot's state is PENDING_VERIFY (the
// bootloader's rollback is on). m: the NVS mark (boots not yet counted).
static inline OtaBoot otaBootDecision(bool pendingVerify, const OtaMark& m) {
  if (pendingVerify) return OtaBoot::PendingBootloader;
  if (!m.pending || !m.prev[0]) return OtaBoot::Normal;   // nothing to go back to
  if (m.boots + 1 > kOtaMaxBoots) return OtaBoot::RevertNow;
  return OtaBoot::PendingApp;
}

// While pending: time to give up?
static inline bool otaGiveUp(uint32_t since, uint32_t now) { return now - since >= kOtaConfirmMs; }

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
