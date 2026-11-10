#pragma once
// Firmware updates over Wi-Fi on the board (docs/link/protocol.md, "Firmware
// updates over Wi-Fi"). net_link.h uses it; arduino-esp32 3.x / ESP-IDF 5
// builds with TLS 1.3 only (the idf5 projects). Elsewhere the kit answers
// every offer with REFUSED.
//
//   - The offer (on the Wi-Fi link only): its board must be this build's
//     (AGENTS_DECK_BOARD, set by tools/build_info.py), the other app slot must
//     exist and hold it, and no update may be running or waiting for its
//     confirm. Otherwise REFUSED with the reason.
//   - The download runs in a task of its own (the screen keeps drawing and
//     the link keeps answering): deck_fw.c fetches the image over TLS 1.3 with
//     the board's certificate, 4 KB at a time, hashing as it goes, and
//     esp_ota_write() puts it in the other slot (OTA_WITH_SEQUENTIAL_WRITES:
//     the slot is erased as the image arrives). esp_ota_end() checks it is a
//     valid app for this chip; deck_fw.c checks size and SHA-256 against the
//     offer. Only then esp_ota_set_boot_partition() and a restart.
//   - The new firmware keeps itself only once the deck answers its Hello
//     (welcome()). Rollback: arduino-esp32 3.x's prebuilt bootloader has
//     CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE (its sdkconfig), so a new image
//     starts PENDING_VERIFY and the bootloader goes back to the old one if the
//     board resets before the confirm (a crash, a power cut). verifyRollbackLater()
//     below stops the Arduino core from confirming it at start-up. On top of
//     that the app gives up after kOtaConfirmMs without a deck. For a
//     bootloader without rollback (the state stays NEW), the app does it
//     itself: an NVS mark with the old slot, counted starts, and
//     esp_ota_set_boot_partition() back; the mark is cleared before it
//     switches, so it never loops.
#if defined(ARDUINO)
#include <Arduino.h>

#include "deck_fw.h"
#include "deck_link.h"
#include "deck_net_config.h"
#include "deck_store.h"
#include "ota_model.h"

#if DECK_LINK_HAS_TLS13
#include <esp_ota_ops.h>
#include <esp_partition.h>
#endif

#ifndef NET_LOG
#define NET_LOG(...) do {} while (0)
#endif

// The board this firmware is for (its folder under firmware/devices) and its
// version: tools/build_info.py sets them from the project and the release.
#ifndef AGENTS_DECK_BOARD
#define AGENTS_DECK_BOARD ""
#endif

#if DECK_LINK_HAS_TLS13
// Keep a new image PENDING_VERIFY past the Arduino core's start-up: ota::welcome()
// confirms it once the deck answers (arduino-esp32 3.x, esp32-hal-misc.c).
extern "C" bool verifyRollbackLater() { return true; }

// The board this image is for, in its flash: an update must carry this
// board's mark, or it isn't installed (another board's build, offered under
// this board's name, would start but drive the wrong screen).
#define OTA_BOARD_MARK "agents-deck-board:" AGENTS_DECK_BOARD
__attribute__((used)) static const char kOtaBoardMark[] = OTA_BOARD_MARK;
#endif

namespace ota {

static OtaView view;   // for the screen (net_link::view())

#if DECK_LINK_HAS_TLS13

static deck_kv_t kv;
static bool kvOk = false;
static OtaBoot boot = OtaBoot::Normal;
static uint32_t pendingSince = 0;
static OtaMark mark;

// The download task and the main loop share these.
enum class Task : uint8_t { Idle, Running, Done };
static volatile Task task = Task::Idle;
static volatile uint32_t taskDone = 0, taskTotal = 0;
static volatile bool taskWritten = false;      // the image is in: checking it
static volatile bool cancel = false;
static deck_fw_err_t taskResult = DECK_FW_OK;
static char taskErr[96] = "";
static agentsdeck_link_v1_FirmwareOffer offer;  // the one being installed
static const deck_cert_t* taskCert = nullptr;
static char taskHost[DECK_HOST_SIZE] = "";
static char taskAddr[48] = "";
static const esp_partition_t* target = nullptr;
static esp_ota_handle_t handle = 0;

// Reporting, from the main loop.
static deck_link_t* via = nullptr;              // the core the offer came on
static uint8_t lastPct = 0;
static uint32_t lastReportMs = 0;
static uint32_t restartAt = 0, failedUntil = 0;

static bool loadMark(OtaMark& m) {
  size_t n = sizeof m;
  m = OtaMark();
  return kvOk && kv.get(kv.user, "ota", &m, &n) && n == sizeof m;
}
static bool saveMark(const OtaMark& m) { return kvOk && kv.set(kv.user, "ota", &m, sizeof m); }
static void clearMark() {
  if (kvOk) kv.erase(kv.user, "ota");
  mark = OtaMark();
}

// Back to the old firmware, for good: the mark goes first, so this happens once.
static void revert(const char* why) {
  NET_LOG("ota: going back to the old firmware (%s)", why);
  if (boot == OtaBoot::PendingBootloader) {
    clearMark();
    esp_ota_mark_app_invalid_rollback_and_reboot();   // returns only if there is nothing to go back to
    boot = OtaBoot::Normal;
    return;
  }
  const esp_partition_t* prev =
      esp_partition_find_first(ESP_PARTITION_TYPE_APP, ESP_PARTITION_SUBTYPE_ANY, mark.prev);
  clearMark();
  boot = OtaBoot::Normal;
  if (prev && esp_ota_set_boot_partition(prev) == ESP_OK) {
    delay(100);
    esp_restart();
  }
}

// At start-up, before anything else that could crash: is this an update
// waiting for its confirm?
static void begin() {
  kvOk = deck_kv_nvs_open(&kv, "decklink");
  const esp_partition_t* run = esp_ota_get_running_partition();
  esp_ota_img_states_t st = ESP_OTA_IMG_UNDEFINED;
  const bool known = run && esp_ota_get_state_partition(run, &st) == ESP_OK;
  (void)loadMark(mark);
  boot = otaBootDecision(known && st == ESP_OTA_IMG_PENDING_VERIFY, mark, run ? run->label : "");
  switch (boot) {
    case OtaBoot::RevertNow:
      revert("it kept restarting");
      break;
    case OtaBoot::PendingApp:
      mark.boots++;
      (void)saveMark(mark);
      pendingSince = millis();
      break;
    case OtaBoot::PendingBootloader:
      pendingSince = millis();
      break;
    case OtaBoot::Normal:
      // An image the app switched to by itself (going back) starts NEW: keep it.
      if (known && st == ESP_OTA_IMG_NEW) esp_ota_mark_app_valid_cancel_rollback();
      if (mark.pending) clearMark();   // back on the old firmware, or a mark without a slot
      break;
  }
  NET_LOG("ota: slot %s, state %d, %s", run ? run->label : "?", (int)st,
          boot == OtaBoot::Normal ? "confirmed" : "waiting for the deck to confirm it");
}

// A Welcome from the deck (either link): the new firmware works.
static bool welcome() {
  if (boot != OtaBoot::PendingBootloader && boot != OtaBoot::PendingApp) return false;
  esp_ota_mark_app_valid_cancel_rollback();
  clearMark();
  boot = OtaBoot::Normal;
  NET_LOG("ota: the deck answered: keeping this firmware");
  return true;
}

static bool sinkWrite(void*, uint64_t, const uint8_t* d, size_t n) { return esp_ota_write(handle, d, n) == ESP_OK; }
static void sinkProgress(void*, uint64_t done, uint64_t total) {
  taskDone = (uint32_t)done;
  taskTotal = (uint32_t)total;
}

// Does the written image carry this board's mark (kOtaBoardMark, with its NUL)?
static bool hasMark(const esp_partition_t* p, uint32_t size) {
  static uint8_t buf[1024 + sizeof kOtaBoardMark];
  return otaFindMark(kOtaBoardMark, sizeof kOtaBoardMark, size, buf, sizeof buf,
                     [p](uint32_t off, uint8_t* to, uint32_t n) { return esp_partition_read(p, off, to, n) == ESP_OK; });
}

static void downloadTask(void*) {
  taskErr[0] = '\0';
  esp_err_t e = esp_ota_begin(target, OTA_WITH_SEQUENTIAL_WRITES, &handle);
  if (e != ESP_OK) {
    taskResult = DECK_FW_SINK;
    snprintf(taskErr, sizeof taskErr, "can't start writing (%s)", esp_err_to_name(e));
  } else {
    deck_fw_sink_t sink = {sinkWrite, sinkProgress, nullptr, &cancel, 15000, 4};
    taskResult = deck_fw_download(taskCert, taskHost, taskAddr[0] ? taskAddr : nullptr, &offer, &sink, taskErr,
                                  sizeof taskErr);
    if (taskResult == DECK_FW_OK) {
      taskWritten = true;
      e = esp_ota_end(handle);   // a valid app for this chip (and signed, with secure boot)
      if (e != ESP_OK) {
        taskResult = DECK_FW_CHECKSUM;
        snprintf(taskErr, sizeof taskErr, "not a valid app image (%s)", esp_err_to_name(e));
      } else if (!AGENTS_DECK_BOARD[0] || !hasMark(target, (uint32_t)offer.size)) {
        taskResult = DECK_FW_CHECKSUM;
        snprintf(taskErr, sizeof taskErr, "the image isn't a build for %s", AGENTS_DECK_BOARD);
      }
    } else {
      esp_ota_abort(handle);
    }
  }
  handle = 0;
  task = Task::Done;
  vTaskDelete(nullptr);
}

static void report(agentsdeck_link_v1_FirmwareState st, uint8_t pct, const char* err) {
  if (via) deck_firmware_status(via, st, pct, err);
  lastPct = pct;
  lastReportMs = millis();
}

static void failed(const char* why) {
  NET_LOG("ota: failed: %s", why);
  view.phase = OtaPhase::Failed;
  snprintf(view.error, sizeof view.error, "%s", why);
  failedUntil = millis() + 8000;
}

// The deck's offer (from the core's on_firmware_offer). onWifi: it came over
// the Wi-Fi link; cert, host and addr: where that link goes.
static void offered(deck_link_t* ctx, const agentsdeck_link_v1_FirmwareOffer* o, bool onWifi, const deck_cert_t* cert,
                    const char* host, const char* addr) {
  char why[96] = "";
  target = esp_ota_get_next_update_partition(nullptr);
  if (task != Task::Idle || restartAt) snprintf(why, sizeof why, "already updating");
  else if (boot != OtaBoot::Normal) snprintf(why, sizeof why, "still confirming the last update");
  else if (!onWifi) snprintf(why, sizeof why, "on USB: update it over the cable");
  else if (!target) snprintf(why, sizeof why, "no second firmware slot");
  else (void)deck_firmware_offer_check(o, AGENTS_DECK_BOARD, target->size, why, sizeof why);
  NET_LOG("ota: offer %s %s, %u bytes: %s", o->board, o->version, (unsigned)o->size, why[0] ? why : "taking it");
  if (why[0]) {
    deck_firmware_status(ctx, agentsdeck_link_v1_FirmwareState_FIRMWARE_REFUSED, 0, why);
    return;
  }
  offer = *o;
  via = ctx;
  taskCert = cert;
  snprintf(taskHost, sizeof taskHost, "%s", host);
  snprintf(taskAddr, sizeof taskAddr, "%s", addr ? addr : "");
  taskDone = 0;
  taskTotal = (uint32_t)o->size;
  taskWritten = false;
  cancel = false;
  view = OtaView();
  view.phase = OtaPhase::Downloading;
  snprintf(view.version, sizeof view.version, "%s", o->version);
  report(agentsdeck_link_v1_FirmwareState_FIRMWARE_DOWNLOADING, 0, nullptr);
  task = Task::Running;
  // mbedTLS's handshake wants a deep stack, as for the link (net_link.h).
  if (xTaskCreatePinnedToCore(downloadTask, "deck-ota", 12288, nullptr, 1, nullptr, 0) != pdPASS) {
    task = Task::Idle;
    report(agentsdeck_link_v1_FirmwareState_FIRMWARE_FAILED, 0, "no memory for the download");
    failed("no memory");
  }
}

// Every loop: progress to the deck and the screen, the end of the download,
// the restart, and the confirm's deadline. link: the core to report on now.
static void loop(deck_link_t* link) {
  const uint32_t now = millis();
  if (link) via = link;
  if (task == Task::Running) {
    const uint8_t pct = taskTotal ? (uint8_t)((uint64_t)taskDone * 100 / taskTotal) : 0;
    view.percent = pct;
    if (taskWritten && view.phase == OtaPhase::Downloading) {
      view.phase = OtaPhase::Verifying;
      report(agentsdeck_link_v1_FirmwareState_FIRMWARE_VERIFYING, 100, nullptr);
    } else if (view.phase == OtaPhase::Downloading && otaReportDue(lastPct, lastReportMs, pct, now)) {
      report(agentsdeck_link_v1_FirmwareState_FIRMWARE_DOWNLOADING, pct, nullptr);
    }
  } else if (task == Task::Done) {
    task = Task::Idle;
    if (taskResult != DECK_FW_OK) {
      report(agentsdeck_link_v1_FirmwareState_FIRMWARE_FAILED, lastPct, taskErr);
      failed(taskErr);
    } else {
      if (view.phase != OtaPhase::Verifying) report(agentsdeck_link_v1_FirmwareState_FIRMWARE_VERIFYING, 100, nullptr);
      // Remember where we came from (for a bootloader without rollback), then switch.
      const esp_partition_t* run = esp_ota_get_running_partition();
      OtaMark m;
      m.pending = 1;
      snprintf(m.prev, sizeof m.prev, "%s", run ? run->label : "");
      snprintf(m.next, sizeof m.next, "%s", target->label);
      const bool saved = saveMark(m);
      const esp_err_t e = saved ? esp_ota_set_boot_partition(target) : ESP_FAIL;
      if (e != ESP_OK) {
        clearMark();
        char why[64];
        snprintf(why, sizeof why, saved ? "can't switch slots (%s)" : "can't save the update's state", esp_err_to_name(e));
        report(agentsdeck_link_v1_FirmwareState_FIRMWARE_FAILED, 100, why);
        failed(why);
      } else {
        view.phase = OtaPhase::Restarting;
        view.percent = 100;
        report(agentsdeck_link_v1_FirmwareState_FIRMWARE_RESTARTING, 100, nullptr);
        NET_LOG("ota: %s written to %s; restarting", offer.version, target->label);
        restartAt = now + 1500;   // the screen says so, and the frame reaches the deck
      }
    }
  }
  if (restartAt && (int32_t)(now - restartAt) >= 0) esp_restart();
  if (view.phase == OtaPhase::Failed && (int32_t)(now - failedUntil) >= 0) view = OtaView();
  if ((boot == OtaBoot::PendingBootloader || boot == OtaBoot::PendingApp) && otaGiveUp(pendingSince, now))
    revert("the deck didn't answer in time");
}

#else  // no TLS 1.3: no updates over Wi-Fi (the kit refuses offers)

static inline void begin() {}
static inline bool welcome() { return false; }
static inline void loop(deck_link_t*) {}

#endif

}  // namespace ota

#endif  // ARDUINO
