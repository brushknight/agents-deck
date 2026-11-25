/* Deck Link over Bluetooth LE: the byte stream between a deck_link_t core and
 * a GATT link (docs/link/protocol.md, "Bluetooth").
 *
 * The deck writes Deck Link bytes to the RX characteristic; the board sends
 * its own as notifications on TX. They are the same COBS-delimited frames as
 * on USB serial, cut to the link's MTU, with no framing of their own. This is
 * the part of that which doesn't depend on a Bluetooth stack: C99, no malloc,
 * no OS, so the host tests run it against a fake (test/host/test_ble.c).
 *
 *   TX, board to deck: deck_ble_write is the core's write callback. It queues
 *   a whole frame or nothing (a frame that doesn't fit counts in the core's
 *   tx_errors, as a short serial write does). deck_ble_pump sends what is
 *   queued, ATT MTU - 3 bytes per notification, for as long as the stack
 *   takes them; when it says "no room" the rest waits for the next pump.
 *
 *   RX, deck to board: the stack's write handler calls deck_ble_rx_push from
 *   its own task, and the loop drains the ring into deck_link_feed with
 *   deck_ble_rx_feed. One producer and one consumer, so no lock. A write that
 *   doesn't fit is refused whole: the handler then answers a write request
 *   with "insufficient resources" and the deck sends it again.
 *
 * The security rule (docs/protocol.md) is the caller's: open the stream only
 * on a link that is encrypted, authenticated (passkey) and bonded.
 */
#ifndef DECK_BLE_H
#define DECK_BLE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "deck_link.h"

#ifdef __cplusplus
extern "C" {
#endif

/* The GATT service and its characteristics (fixed for both sides):
 *   service 7e3a0001-5d2c-4f1b-9a6e-a9e17d5c0de0
 *   RX      7e3a0002-...  deck to board: write, write without response (encrypted, MITM)
 *   TX      7e3a0003-...  board to deck: notify (encrypted, MITM)
 *   Info    7e3a0004-...  read, no encryption: "board=<board>;id=<board id>;fw=<version>;paired=<0|1>"
 * DECK_BLE_UUID128(n) lists the bytes least significant first, the order
 * Bluetooth stacks (NimBLE's BLE_UUID128_INIT) take them in. */
#define DECK_BLE_SERVICE_UUID "7e3a0001-5d2c-4f1b-9a6e-a9e17d5c0de0"
#define DECK_BLE_RX_UUID "7e3a0002-5d2c-4f1b-9a6e-a9e17d5c0de0"
#define DECK_BLE_TX_UUID "7e3a0003-5d2c-4f1b-9a6e-a9e17d5c0de0"
#define DECK_BLE_INFO_UUID "7e3a0004-5d2c-4f1b-9a6e-a9e17d5c0de0"
#define DECK_BLE_UUID128(n) 0xe0, 0x0d, 0x5c, 0x7d, 0xe1, 0xa9, 0x6e, 0x9a, 0x1b, 0x4f, 0x2c, 0x5d, (n), 0x00, 0x3a, 0x7e

/* The ATT MTU a board asks for: 247 fills one link-layer packet of 251
 * bytes (data length extension), so a notification carries 244. */
#ifndef DECK_BLE_MTU
#define DECK_BLE_MTU 247
#endif
/* The default ATT MTU, before the central asks for more. */
#define DECK_BLE_MTU_MIN 23
/* The most bytes one notification carries (DECK_BLE_MTU - 3). */
#define DECK_BLE_CHUNK_MAX (DECK_BLE_MTU - 3)

/* Bytes queued for TX: a few of the board's frames (the largest, a
 * CertRequest, is about 620 bytes on the wire). */
#ifndef DECK_BLE_TX_SIZE
#define DECK_BLE_TX_SIZE 2048
#endif

/* "deck-" and six hex digits, and a NUL. */
#define DECK_BLE_NAME_SIZE 12
/* The Info value: board (31) and id (39) at most, the version (23) and the keys. */
#define DECK_BLE_INFO_SIZE 128

/* Sends one notification of len bytes (1..ATT MTU - 3) on TX. True when the
 * stack took it; false when it has no room now (try again later). */
typedef bool (*deck_ble_notify_fn)(const uint8_t *data, size_t len, void *user);

typedef struct {
	deck_ble_notify_fn notify;
	void *user;
	uint8_t *rx_buf; /* the RX ring, at least the largest write (512) and best a snapshot: 16-32 KB */
	size_t rx_size;
} deck_ble_config_t;

typedef struct {
	uint32_t tx_bytes;    /* bytes notified */
	uint32_t tx_notifies; /* notifications sent */
	uint32_t tx_busy;     /* times the stack had no room */
	uint32_t tx_dropped;  /* frames refused: the TX queue was full, or the stream closed */
	uint32_t rx_bytes;    /* bytes the deck wrote */
	uint32_t rx_refused;  /* writes refused: the ring was full, or the stream closed */
} deck_ble_stats_t;

/* Treat the fields as private. */
typedef struct {
	deck_ble_config_t cfg;
	uint16_t chunk; /* bytes per notification: ATT MTU - 3, at most DECK_BLE_CHUNK_MAX */
	volatile bool open;
	size_t tx_head, tx_len;
	uint8_t tx[DECK_BLE_TX_SIZE];
	uint8_t scratch[DECK_BLE_CHUNK_MAX]; /* one notification across the ring's end */
	size_t rx_head; /* the producer's (atomic) */
	size_t rx_tail; /* the consumer's (atomic) */
	deck_ble_stats_t stats;
} deck_ble_t;

void deck_ble_init(deck_ble_t *b, const deck_ble_config_t *cfg);

/* The link is secure and the deck listens on TX: the stream opens, empty,
 * with the ATT MTU agreed so far. Call from the consumer's task. */
void deck_ble_open(deck_ble_t *b, uint16_t att_mtu);
/* The ATT MTU changed (the central asked for more). */
void deck_ble_set_mtu(deck_ble_t *b, uint16_t att_mtu);
/* The link is gone: queued bytes are dropped, writes refused. */
void deck_ble_close(deck_ble_t *b);
bool deck_ble_is_open(const deck_ble_t *b);

/* The deck_link_config_t.write callback (user: the deck_ble_t). Queues all
 * of data and returns len, or nothing and 0. */
size_t deck_ble_write(const uint8_t *data, size_t len, void *ble);
/* Sends queued bytes, a notification at a time, until the queue is empty,
 * the stack has no room or max_notifies went out (0: no limit). Returns the
 * bytes sent. Call it every loop. */
size_t deck_ble_pump(deck_ble_t *b, unsigned max_notifies);
/* Bytes waiting to go out. */
size_t deck_ble_pending(const deck_ble_t *b);

/* The producer (the stack's write handler, any task): takes all of data, or
 * nothing when the stream is closed or the ring lacks room. */
bool deck_ble_rx_push(deck_ble_t *b, const uint8_t *data, size_t len);
/* The consumer: up to cap bytes out of the ring. */
size_t deck_ble_rx_pop(deck_ble_t *b, uint8_t *out, size_t cap);
/* The consumer: feeds up to max bytes (0: all there is) to the core. */
size_t deck_ble_rx_feed(deck_ble_t *b, deck_link_t *link, size_t max);
/* Bytes in the ring. */
size_t deck_ble_rx_available(const deck_ble_t *b);

const deck_ble_stats_t *deck_ble_stats(const deck_ble_t *b);

/* The board's Bluetooth name: "deck-" and the last six hex digits of its
 * board id, lower case ("AA:BB:CC:1A:2B:3C" gives "deck-1a2b3c"). An id with
 * fewer hex digits is padded with zeros in front. */
void deck_ble_name(const char *board_id, char out[DECK_BLE_NAME_SIZE]);
/* The Info characteristic's value. Returns its length (cut to cap - 1). */
size_t deck_ble_info(char *out, size_t cap, const char *board, const char *board_id, const char *firmware,
		     bool paired);

/* A passkey for the screen from a 32-bit random number: 0..999999, shown
 * with six digits ("042917"). */
uint32_t deck_ble_passkey(uint32_t random);

#ifdef __cplusplus
}
#endif

#endif
