#pragma once
// Bluetooth LE for the touch boards: the Deck Link GATT service
// (docs/link/protocol.md, "Bluetooth") on ESP-IDF's NimBLE host, which
// arduino-esp32 3.x builds into its ESP32-S3 libraries. net_link.h drives it;
// ble_model.h holds the rules, deck_ble.h the byte stream.
//
//   - GATT: the service with RX (deck to board, write and write without
//     response), TX (board to deck, notify) and Info (read, no encryption).
//     RX and TX need an encrypted, authenticated link with a 16-byte key:
//     the stack answers anything else with "insufficient authentication",
//     which makes the computer pair.
//   - Security: LE Secure Connections only, MITM, bonding; the board is
//     display only, so the computer asks for the 6-digit passkey the board
//     shows (net_model.h's words, on every layout). A pairing that ends
//     without MITM (Just Works) is undone and the central dropped.
//   - One bond. With none the board is in setup: connectable for anyone,
//     and pairing starts only when the computer asks for RX or TX (so a scan
//     that just reads Info never makes it ask for a code). With one it takes
//     only that deck: a new pairing is refused (no passkey is ever shown
//     then), a repeat pairing from the bonded deck too, and a central that
//     hasn't encrypted with the bond's keys within 10 s is dropped. forget()
//     clears the bond (BOOT held, ble_model.h).
//   - Connection: 2M PHY, data length 251, ATT MTU up to 247, an interval of
//     15 to 30 ms: about what a Mac allows, enough for a 32-agent snapshot in
//     well under a second.
//   - stop() shuts the stack down for a firmware update (its TLS download
//     wants the heap); begin() starts it again.
//
// Builds without NimBLE (arduino-esp32 2.0.x has Bluedroid, and no TLS 1.3,
// so no Wi-Fi either) get PANEL_HAS_BLE 0 and none of this.
#if defined(ARDUINO)
#include <Arduino.h>
#include <esp_idf_version.h>
#include <sdkconfig.h>

#include "ble_model.h"
#include "deck_ble.h"
#include "deck_net_config.h"

#if !defined(PANEL_NO_BLE) && defined(CONFIG_BT_NIMBLE_ENABLED) && ESP_IDF_VERSION_MAJOR >= 5 && DECK_LINK_HAS_TLS13
#define PANEL_HAS_BLE 1
#else
#define PANEL_HAS_BLE 0
#endif

#if PANEL_HAS_BLE
#include <esp_heap_caps.h>
#include <esp_log.h>
#include <esp_random.h>

#include "esp32-hal-alloc-ble-mem.h"   // keeps arduino-esp32 from freeing the controller's memory at boot
#include "host/ble_gap.h"
#include "host/ble_hs.h"
#include "host/ble_store.h"
#include "host/util/util.h"
#include "nimble/nimble_port.h"
#include "nimble/nimble_port_freertos.h"
#include "services/gap/ble_svc_gap.h"
#include "services/gatt/ble_svc_gatt.h"

extern "C" void ble_store_config_init(void);

#ifndef NET_LOG
#define NET_LOG(...) do {} while (0)
#endif

namespace ble_link {

constexpr uint32_t kRxRing = 32768;           // bytes from the deck waiting for the loop: a whole snapshot
constexpr uint32_t kSetupWaitMs = 90000;      // setup: a central has this long to pair (someone types)
constexpr uint32_t kBondedWaitMs = 10000;     // bonded: this long to encrypt with the bond's keys
constexpr uint32_t kPasskeyMs = 60000;        // a passkey on the screen goes after this long
constexpr int kMsysReserve = 6;               // mbufs left for what the deck writes: TX waits below this

static deck_ble_t stream;
static uint8_t* ring = nullptr;
static char name[DECK_BLE_NAME_SIZE] = "";
static char info[DECK_BLE_INFO_SIZE] = "";
static portMUX_TYPE infoLock = portMUX_INITIALIZER_UNLOCKED;
static bool running = false;                 // the stack is up (begin() .. stop())

// Set by the NimBLE task, read by the loop.
static volatile bool synced = false;
static volatile uint16_t conn = BLE_HS_CONN_HANDLE_NONE;
static volatile uint32_t connAt = 0;
static volatile bool bondedAtConnect = false;
static volatile bool secure = false;         // encrypted, authenticated, 16-byte key, the bonded peer
static volatile uint32_t secureAt = 0;       // when it became secure
static volatile bool carried = false;        // Bluetooth carried the session on this connection
static volatile bool subscribed = false;     // the deck listens on TX
static volatile uint16_t mtu = DECK_BLE_MTU_MIN;
static volatile bool mtuChanged = false;
static volatile bool pairing = false;        // a passkey is on the screen
static volatile uint32_t passkey = 0;
static volatile uint32_t passkeyAt = 0;
static volatile bool bondChanged = true;     // re-count the bonds in the loop
static volatile bool dropBadPeer = false;    // a pairing to undo (no passkey, or not our deck)
static ble_addr_t badPeer;
static ble_addr_t bondPeer;                  // the bonded deck when this connection began

static uint8_t ownAddrType = BLE_OWN_ADDR_PUBLIC;
static uint16_t txHandle = 0;
static bool bonded = false;                  // the loop's count of bonds
static BleAdvertise advNow = BleAdvertise::Off;

static const ble_uuid128_t kSvcUuid = BLE_UUID128_INIT(DECK_BLE_UUID128(0x01));
static const ble_uuid128_t kRxUuid = BLE_UUID128_INIT(DECK_BLE_UUID128(0x02));
static const ble_uuid128_t kTxUuid = BLE_UUID128_INIT(DECK_BLE_UUID128(0x03));
static const ble_uuid128_t kInfoUuid = BLE_UUID128_INIT(DECK_BLE_UUID128(0x04));

static struct ble_gatt_chr_def chrs[4];
static struct ble_gatt_svc_def svcs[2];

static int gapEvent(struct ble_gap_event* event, void* arg);

static int bondCount() {
  ble_addr_t peers[4];
  int n = 0;
  if (ble_store_util_bonded_peers(peers, &n, 4) != 0) return 0;
  return n;
}

// Full store: never make room by dropping the bond we hold (a stranger's
// pairing then fails); otherwise NimBLE's usual oldest-first.
static int storeStatus(struct ble_store_status_event* event, void* arg) {
  if (bondCount() > 0) return BLE_HS_ESTORE_CAP;
  return ble_store_util_status_rr(event, arg);
}

// The link is what RX and TX need: encrypted with a passkey (MITM), a full-size key.
static bool linkOk(uint16_t h, struct ble_gap_conn_desc* out = nullptr) {
  struct ble_gap_conn_desc d;
  if (ble_gap_conn_find(h, &d) != 0) return false;
  if (out) *out = d;
  return d.sec_state.encrypted && d.sec_state.authenticated && d.sec_state.key_size >= 16;
}

// ---------------------------------------------------------------- GATT

static int access(uint16_t h, uint16_t, struct ble_gatt_access_ctxt* ctxt, void* arg) {
  const uintptr_t which = (uintptr_t)arg;
  if (which == 3 && ctxt->op == BLE_GATT_ACCESS_OP_READ_CHR) {   // Info: anyone may read it
    char copy[DECK_BLE_INFO_SIZE];
    portENTER_CRITICAL(&infoLock);
    memcpy(copy, info, sizeof copy);
    portEXIT_CRITICAL(&infoLock);
    return os_mbuf_append(ctxt->om, copy, strlen(copy)) == 0 ? 0 : BLE_ATT_ERR_INSUFFICIENT_RES;
  }
  if (which == 1 && ctxt->op == BLE_GATT_ACCESS_OP_WRITE_CHR) {  // RX: Deck Link bytes from the deck
    // The stack checked encryption and authentication already (the flags); check it is our deck too.
    if (h != conn || !secure) return BLE_ATT_ERR_INSUFFICIENT_AUTHEN;
    uint8_t buf[DECK_BLE_MTU];
    uint16_t len = 0;
    if (ble_hs_mbuf_to_flat(ctxt->om, buf, sizeof buf, &len) != 0) return BLE_ATT_ERR_INVALID_ATTR_VALUE_LEN;
    // Full: refused whole, so a write request is answered "insufficient resources" and sent again.
    return deck_ble_rx_push(&stream, buf, len) ? 0 : BLE_ATT_ERR_INSUFFICIENT_RES;
  }
  return BLE_ATT_ERR_UNLIKELY;
}

static void gattSetup() {
  memset(chrs, 0, sizeof chrs);
  memset(svcs, 0, sizeof svcs);
  chrs[0].uuid = &kRxUuid.u;
  chrs[0].access_cb = access;
  chrs[0].arg = (void*)1;
  chrs[0].flags = BLE_GATT_CHR_F_WRITE | BLE_GATT_CHR_F_WRITE_NO_RSP | BLE_GATT_CHR_F_WRITE_ENC |
                  BLE_GATT_CHR_F_WRITE_AUTHEN;
  chrs[0].min_key_size = 16;
  chrs[1].uuid = &kTxUuid.u;
  chrs[1].access_cb = access;
  chrs[1].arg = (void*)2;
  chrs[1].flags = BLE_GATT_CHR_F_NOTIFY | BLE_GATT_CHR_F_NOTIFY_INDICATE_ENC | BLE_GATT_CHR_F_NOTIFY_INDICATE_AUTHEN;
  chrs[1].min_key_size = 16;
  chrs[1].val_handle = &txHandle;
  chrs[2].uuid = &kInfoUuid.u;
  chrs[2].access_cb = access;
  chrs[2].arg = (void*)3;
  chrs[2].flags = BLE_GATT_CHR_F_READ;
  svcs[0].type = BLE_GATT_SVC_TYPE_PRIMARY;
  svcs[0].uuid = &kSvcUuid.u;
  svcs[0].characteristics = chrs;
}

// TX: one notification. False when the stack has no room (deck_ble_pump tries again later).
static bool notify(const uint8_t* d, size_t n, void*) {
  const uint16_t h = conn;
  if (h == BLE_HS_CONN_HANDLE_NONE || !secure || !subscribed) return false;
  if (os_msys_num_free() < kMsysReserve) return false;   // leave buffers for the deck's writes
  struct os_mbuf* om = ble_hs_mbuf_from_flat(d, (uint16_t)n);
  if (!om) return false;
  return ble_gatts_notify_custom(h, txHandle, om) == 0;   // the stack frees om either way
}

// ---------------------------------------------------------------- GAP

static void onSync() {
  ble_hs_util_ensure_addr(0);
  ble_hs_id_infer_auto(0, &ownAddrType);
  synced = true;
}

static void onReset(int reason) {
  synced = false;
  NET_LOG("ble: host reset, reason %d", reason);
}

static void endConnection() {
  conn = BLE_HS_CONN_HANDLE_NONE;
  secure = false;
  carried = false;
  subscribed = false;
  pairing = false;
  mtu = DECK_BLE_MTU_MIN;
}

static int gapEvent(struct ble_gap_event* event, void*) {
  switch (event->type) {
    case BLE_GAP_EVENT_CONNECT: {
      if (event->connect.status != 0) break;   // advertising ended without a connection: the loop starts it again
      const uint16_t h = event->connect.conn_handle;
      if (conn != BLE_HS_CONN_HANDLE_NONE) {   // one central at a time
        ble_gap_terminate(h, BLE_ERR_REM_USER_CONN_TERM);
        break;
      }
      endConnection();
      conn = h;
      connAt = millis();
      {
        ble_addr_t peers[4];
        int n = 0;
        bondedAtConnect = ble_store_util_bonded_peers(peers, &n, 4) == 0 && n > 0;
        if (bondedAtConnect) bondPeer = peers[0];
      }
      // Quick: 2M PHY, long packets, a short interval (Apple's rules: min >= 15 ms, max >= min + 15 ms).
      ble_gap_set_prefered_le_phy(h, BLE_GAP_LE_PHY_2M_MASK, BLE_GAP_LE_PHY_2M_MASK, BLE_GAP_LE_PHY_CODED_ANY);
      ble_gap_set_data_len(h, 251, 2120);
      struct ble_gap_upd_params p;
      memset(&p, 0, sizeof p);
      p.itvl_min = BLE_GAP_CONN_ITVL_MS(15);
      p.itvl_max = BLE_GAP_CONN_ITVL_MS(30);
      p.latency = 0;
      p.supervision_timeout = BLE_GAP_SUPERVISION_TIMEOUT_MS(4000);
      ble_gap_update_params(h, &p);
      // Bonded: ask for encryption at once, the deck encrypts with the bond's
      // keys. In setup, wait: the computer pairs when it subscribes to TX
      // (insufficient authentication), never for a scan that only reads Info.
      {
        // The computer encrypts by itself: with the bond's keys as soon as it
        // touches TX, or by pairing in setup. Asking for it from here as well
        // collided with the computer's own request on a real Mac (the second
        // one failed, status 7), so the board only waits and checks.
        NET_LOG("ble: connected, %s", bondedAtConnect ? "bonded" : "setup");
      }
      break;
    }
    case BLE_GAP_EVENT_DISCONNECT:
      NET_LOG("ble: disconnected, reason 0x%x, was %s", event->disconnect.reason, secure ? "secure" : "not secure");
      if (event->disconnect.conn.conn_handle == conn) endConnection();
      break;
    case BLE_GAP_EVENT_ADV_COMPLETE:
      break;   // the loop starts it again when it should run
    case BLE_GAP_EVENT_PASSKEY_ACTION: {
      const uint16_t h = event->passkey.conn_handle;
      if (event->passkey.params.action != BLE_SM_IOACT_DISP || bondedAtConnect) {
        // Only a display (no yes/no, no typing here), and no new pairing while a bond exists.
        ble_gap_terminate(h, BLE_ERR_AUTH_FAIL);
        break;
      }
      struct ble_sm_io io;
      memset(&io, 0, sizeof io);
      io.action = BLE_SM_IOACT_DISP;
      io.passkey = deck_ble_passkey(esp_random());
      passkey = io.passkey;
      passkeyAt = millis();
      pairing = true;
      ble_sm_inject_io(h, &io);
      break;
    }
    case BLE_GAP_EVENT_ENC_CHANGE: {
      const uint16_t h = event->enc_change.conn_handle;
      pairing = false;
      struct ble_gap_conn_desc d;
      const bool ok = event->enc_change.status == 0 && linkOk(h, &d);
      const bool found = ble_gap_conn_find(h, &d) == 0;
      const bool ourDeck = found && bondedAtConnect && ble_addr_cmp(&d.peer_id_addr, &bondPeer) == 0;
      NET_LOG("ble: encryption change status %d, link ok %d, enc %d auth %d bonded %d key %d, our deck %d", event->enc_change.status,
              (int)ok, found ? d.sec_state.encrypted : -1, found ? d.sec_state.authenticated : -1,
              found ? d.sec_state.bonded : -1, found ? d.sec_state.key_size : -1, (int)ourDeck);
      // Bonded: only the bonded deck. Setup: this pairing made the bond. And
      // only with the passkey (MITM), a full key, and a bond.
      if (event->enc_change.status != 0 && found && d.sec_state.encrypted) break;   // a second request failed; the link itself still stands (poll() judges it)
      if (!ok || !d.sec_state.bonded || (bondedAtConnect && !ourDeck)) {
        if (found && d.sec_state.encrypted && !ourDeck) {
          badPeer = d.peer_id_addr;   // whatever this pairing stored goes (poll() deletes it)
          dropBadPeer = true;
        }
        ble_gap_terminate(h, BLE_ERR_AUTH_FAIL);
        break;
      }
      secureAt = millis();
      secure = (h == conn);
      bondChanged = true;
      break;
    }
    case BLE_GAP_EVENT_REPEAT_PAIRING:
      // The bonded deck wants to pair again (it lost its keys): no. Forget the
      // bond on the board (hold BOOT) and pair from scratch.
      return BLE_GAP_REPEAT_PAIRING_IGNORE;
    case BLE_GAP_EVENT_SUBSCRIBE:
      if (event->subscribe.attr_handle == txHandle && event->subscribe.conn_handle == conn)
        subscribed = event->subscribe.cur_notify;
      break;
    case BLE_GAP_EVENT_MTU:
      if (event->mtu.conn_handle == conn) {
        mtu = event->mtu.value;
        mtuChanged = true;
      }
      break;
    default:
      break;
  }
  return 0;
}

static void hostTask(void*) {
  nimble_port_run();   // returns after nimble_port_stop()
  nimble_port_freertos_deinit();
}

// ---------------------------------------------------------------- the loop's side

static void setInfo(const char* board, const char* boardId, const char* firmware) {
  char v[DECK_BLE_INFO_SIZE];
  deck_ble_info(v, sizeof v, board, boardId, firmware, bonded);
  portENTER_CRITICAL(&infoLock);
  memcpy(info, v, sizeof info);
  portEXIT_CRITICAL(&infoLock);
}

static char infoBoard[32] = "", infoId[40] = "", infoFw[24] = "";

// Starts the stack: the GATT service, bonds from NVS. False when it can't.
static bool begin(const char* board, const char* boardId, const char* firmware) {
  if (running) return true;
  snprintf(infoBoard, sizeof infoBoard, "%s", board ? board : "");
  snprintf(infoId, sizeof infoId, "%s", boardId ? boardId : "");
  snprintf(infoFw, sizeof infoFw, "%s", firmware ? firmware : "");
  deck_ble_name(boardId, name);
  if (!ring) {
    // In PSRAM (the NimBLE task writes, the loop reads; neither from an interrupt).
    size_t size = kRxRing;
    ring = (uint8_t*)heap_caps_malloc(size, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!ring) {
      size = kRxRing / 4;
      ring = (uint8_t*)heap_caps_malloc(size, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    }
    if (!ring) return false;
    const deck_ble_config_t sc = {notify, nullptr, ring, size};
    deck_ble_init(&stream, &sc);
  }
  esp_log_level_set("NimBLE", ESP_LOG_WARN);
  if (nimble_port_init() != ESP_OK) {
    NET_LOG("ble: the stack didn't start");
    return false;
  }
  ble_hs_cfg.sync_cb = onSync;
  ble_hs_cfg.reset_cb = onReset;
  ble_hs_cfg.store_status_cb = storeStatus;
  ble_hs_cfg.sm_io_cap = BLE_HS_IO_DISPLAY_ONLY;
  ble_hs_cfg.sm_bonding = 1;
  ble_hs_cfg.sm_mitm = 1;
  ble_hs_cfg.sm_sc = 1;
  ble_hs_cfg.sm_sc_only = 1;   // LE Secure Connections, 16-byte keys; no legacy pairing
  ble_hs_cfg.sm_our_key_dist = BLE_SM_PAIR_KEY_DIST_ENC | BLE_SM_PAIR_KEY_DIST_ID;
  ble_hs_cfg.sm_their_key_dist = BLE_SM_PAIR_KEY_DIST_ENC | BLE_SM_PAIR_KEY_DIST_ID;
  gattSetup();
  ble_svc_gap_init();
  ble_svc_gatt_init();
  if (ble_gatts_count_cfg(svcs) != 0 || ble_gatts_add_svcs(svcs) != 0) {
    NET_LOG("ble: GATT setup failed");
    nimble_port_deinit();
    return false;
  }
  ble_svc_gap_device_name_set(name);
  ble_att_set_preferred_mtu(DECK_BLE_MTU);
  ble_store_config_init();   // bonds live in NVS (CONFIG_BT_NIMBLE_NVS_PERSIST)
  endConnection();
  synced = false;
  bondChanged = true;
  advNow = BleAdvertise::Off;
  nimble_port_freertos_init(hostTask);
  running = true;
  NET_LOG("ble: up as %s", name);
  return true;
}

// Shuts the stack down (a firmware update wants the heap). begin() again later.
static void stop() {
  if (!running) return;
  deck_ble_close(&stream);
  if (conn != BLE_HS_CONN_HANDLE_NONE) ble_gap_terminate(conn, BLE_ERR_REM_USER_CONN_TERM);
  ble_gap_adv_stop();
  if (nimble_port_stop() == 0) nimble_port_deinit();
  running = false;
  synced = false;
  endConnection();
  advNow = BleAdvertise::Off;
  NET_LOG("ble: stopped");
}

static void advertise(BleAdvertise mode) {
  if (!running || !synced) return;
  const bool on = ble_gap_adv_active();
  if (conn != BLE_HS_CONN_HANDLE_NONE) mode = BleAdvertise::Off;   // advertising stops on a connection anyway
  if (mode == advNow && on == (mode != BleAdvertise::Off)) return;
  if (on) ble_gap_adv_stop();
  advNow = mode;
  if (mode == BleAdvertise::Off) return;
  // The service UUID in the advertisement (18 bytes and the flags), the name in the scan response.
  struct ble_hs_adv_fields f;
  memset(&f, 0, sizeof f);
  f.flags = BLE_HS_ADV_F_DISC_GEN | BLE_HS_ADV_F_BREDR_UNSUP;
  f.uuids128 = &kSvcUuid;
  f.num_uuids128 = 1;
  f.uuids128_is_complete = 1;
  struct ble_hs_adv_fields r;
  memset(&r, 0, sizeof r);
  r.name = (const uint8_t*)name;
  r.name_len = (uint8_t)strlen(name);
  r.name_is_complete = 1;
  if (ble_gap_adv_set_fields(&f) != 0 || ble_gap_adv_rsp_set_fields(&r) != 0) return;
  struct ble_gap_adv_params p;
  memset(&p, 0, sizeof p);
  p.conn_mode = BLE_GAP_CONN_MODE_UND;
  p.disc_mode = BLE_GAP_DISC_MODE_GEN;
  // Setup: found quickly. Bonded: a little slower, still a reconnect within a
  // second or so. Standby (another link carries the session): about once a
  // second, enough for the deck to find it and little air time beside Wi-Fi.
  p.itvl_min = BLE_GAP_ADV_ITVL_MS(mode == BleAdvertise::Open ? 100 : mode == BleAdvertise::Bonded ? 150 : 1000);
  p.itvl_max = BLE_GAP_ADV_ITVL_MS(mode == BleAdvertise::Open ? 150 : mode == BleAdvertise::Bonded ? 300 : 1300);
  const int rc = ble_gap_adv_start(ownAddrType, nullptr, BLE_HS_FOREVER, &p, gapEvent, nullptr);
  if (rc != 0) advNow = BleAdvertise::Off;
}

static void disconnect() {
  const uint16_t h = conn;
  if (h != BLE_HS_CONN_HANDLE_NONE) ble_gap_terminate(h, BLE_ERR_REM_USER_CONN_TERM);
}

// Forgets the bond: the board is in setup again.
static bool forget() {
  if (!running) return false;
  disconnect();
  const bool ok = ble_store_clear() == 0;
  bondChanged = true;
  advNow = BleAdvertise::Off;   // advertise() starts again, open
  ble_gap_adv_stop();
  return ok;
}

// The loop: bonds, timeouts. Call every loop.
static void poll() {
  if (!running) return;
  if (dropBadPeer) {
    dropBadPeer = false;
    ble_store_util_delete_peer(&badPeer);
    bondChanged = true;
  }
  if (bondChanged && synced) {
    bondChanged = false;
    // One bond: keep the deck that just paired, drop any other.
    ble_addr_t peers[4];
    int n = 0;
    if (ble_store_util_bonded_peers(peers, &n, 4) == 0 && n > 1 && secure) {
      struct ble_gap_conn_desc d;
      if (ble_gap_conn_find(conn, &d) == 0)
        for (int i = 0; i < n; i++)
          if (ble_addr_cmp(&peers[i], &d.peer_id_addr) != 0) ble_store_util_delete_peer(&peers[i]);
    }
    bonded = bondCount() > 0;
    setInfo(infoBoard, infoId, infoFw);
  }
  const uint32_t now = millis();
  const uint16_t h = conn;
  // The link's own state decides, not the order events came in: on a real
  // Mac the encryption event can arrive before this side has noted the
  // connection, and then it was missed for good.
  if (h != BLE_HS_CONN_HANDLE_NONE && !secure && !pairing) {
    struct ble_gap_conn_desc d;
    if (linkOk(h, &d) && d.sec_state.bonded) {
      ble_addr_t peers[4];
      int n = 0;
      bool known = false;
      if (ble_store_util_bonded_peers(peers, &n, 4) == 0)
        for (int i = 0; i < n; i++)
          if (ble_addr_cmp(&peers[i], &d.peer_id_addr) == 0) known = true;
      if (known) {
        NET_LOG("ble: the link is encrypted with the bond's keys");
        secureAt = now;
        secure = true;
        bondChanged = true;
      }
    }
  }
  if (h != BLE_HS_CONN_HANDLE_NONE && !secure &&
      now - connAt > (bondedAtConnect ? kBondedWaitMs : kSetupWaitMs))
  {
    NET_LOG("ble: no encryption within the wait, hanging up");
    ble_gap_terminate(h, BLE_ERR_REM_USER_CONN_TERM);   // didn't encrypt (or pair) in time
  }
  if (pairing && now - passkeyAt > kPasskeyMs) {
    pairing = false;
    if (h != BLE_HS_CONN_HANDLE_NONE) ble_gap_terminate(h, BLE_ERR_REM_USER_CONN_TERM);
  }
  if (mtuChanged) {
    mtuChanged = false;
    deck_ble_set_mtu(&stream, mtu);
  }
}

static bool isBonded() { return bonded; }   // as last counted, also while stopped
// Connected to the bonded deck over a secure link, and it listens on TX.
static bool ready() { return running && secure && subscribed && conn != BLE_HS_CONN_HANDLE_NONE; }
static bool centralSecure() { return running && secure; }
// How long the central has been secure (0 when it isn't).
static uint32_t secureForMs() { return running && secure ? millis() - secureAt : 0; }
// Bluetooth carries the session on this connection (net_link.h).
static void markCarried() { carried = true; }
static bool carriedNow() { return carried; }
static uint16_t attMtu() { return mtu; }
static deck_ble_t* bytes() { return &stream; }
static const char* bleName() { return name; }

static BleView view() {
  BleView v;
  v.capable = true;
  snprintf(v.name, sizeof v.name, "%s", name);
  if (!running) return v;
  if (pairing) {
    v.phase = BlePhase::Pairing;
    v.passkey = passkey;
  } else if (secure) {
    v.phase = BlePhase::Connected;
  } else {
    v.phase = bonded ? BlePhase::Bonded : BlePhase::Setup;
  }
  return v;
}

}  // namespace ble_link

#endif  // PANEL_HAS_BLE
#endif  // ARDUINO
