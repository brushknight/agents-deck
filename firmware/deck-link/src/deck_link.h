/* Deck Link: the agents deck over a serial port, for small boards.
 *
 * A transport-agnostic C99 core: no malloc, no Arduino, no OS. You give it a
 * write callback and a millisecond clock, feed it the bytes you receive and
 * call deck_link_poll() often; it handles framing (COBS + magic + CRC), the
 * Hello/Welcome handshake, the subscription, Ping/Pong and offline
 * detection, and calls your event callbacks with decoded messages.
 *
 * The wire protocol is docs/link/protocol.md; the messages are
 * proto/agentsdeck/link/v1/link.proto (generated into proto/link.pb.h).
 *
 * Not thread-safe: call every function from one task. Callbacks run inside
 * deck_link_feed()/deck_link_poll() and may send commands. Pointers passed to
 * callbacks are valid only during the call.
 */
#ifndef DECK_LINK_H
#define DECK_LINK_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "cobs.h"
#include "proto/link.pb.h"

#ifdef __cplusplus
extern "C" {
#endif

#define DECK_LINK_PROTOCOL 1
#define DECK_LINK_WIRE_VERSION 1
#define DECK_LINK_MAGIC0 0xDE
#define DECK_LINK_MAGIC1 0xC1

/* The largest frame (before COBS) this build accepts. The protocol allows
 * 4096; with link.options the deck never sends more than
 * agentsdeck_link_v1_Frame_size + 5 (3328). Lower it (>= 512) to save RAM;
 * the deck then clips what it sends (Hello.max_frame). */
#ifndef DECK_LINK_MAX_FRAME
#define DECK_LINK_MAX_FRAME 4096
#endif

/* Timers (milliseconds). */
#ifndef DECK_LINK_HELLO_RETRY_MS
#define DECK_LINK_HELLO_RETRY_MS 2000u /* resend Hello until a Welcome */
#endif
#ifndef DECK_LINK_OFFLINE_MS
#define DECK_LINK_OFFLINE_MS 15000u /* no frame for this long: offline */
#endif
#ifndef DECK_LINK_OFFLINE_HELLO_MS
#define DECK_LINK_OFFLINE_HELLO_MS 5000u /* resend Hello while offline */
#endif

/* RX holds one COBS-encoded frame (plus a little noise in front of it). */
#define DECK_LINK_RX_SIZE (DECK_COBS_MAX(DECK_LINK_MAX_FRAME) + 8)

/* The largest frame a board sends is a CertRequest (or an Order): magic and
 * version (3) + seq (6) + body tag and length (4) + the message + CRC (2). */
#define DECK_LINK_TX_BODY_MAX                                                                                  \
	(agentsdeck_link_v1_CertRequest_size > agentsdeck_link_v1_Order_size ? agentsdeck_link_v1_CertRequest_size \
									      : agentsdeck_link_v1_Order_size)
#define DECK_LINK_TX_RAW_MAX (3 + 6 + 4 + DECK_LINK_TX_BODY_MAX + 2)
#define DECK_LINK_TX_OFFSET (DECK_LINK_TX_RAW_MAX / 254 + 2)
#define DECK_LINK_TX_SIZE (DECK_LINK_TX_OFFSET + DECK_LINK_TX_RAW_MAX + 1)

typedef agentsdeck_link_v1_Frame deck_frame_t;
typedef agentsdeck_link_v1_Agent deck_agent_t;

typedef enum {
	DECK_LINK_CONNECTING = 0, /* Hello sent, no Welcome yet */
	DECK_LINK_ONLINE,         /* Welcome received, frames arriving */
	DECK_LINK_OFFLINE         /* was online, nothing for DECK_LINK_OFFLINE_MS */
} deck_link_state_t;

typedef struct deck_link deck_link_t;

/* Writes bytes to the serial port; returns how many were written. */
typedef size_t (*deck_write_fn)(const uint8_t *data, size_t len, void *user);
/* A free-running millisecond clock (wraps at 2^32). */
typedef uint32_t (*deck_millis_fn)(void *user);

typedef struct {
	/* Transport (required). */
	deck_write_fn write;
	deck_millis_fn millis;
	void *user; /* passed to write/millis; read it in callbacks with deck_link_user() */

	/* Hello (board_id is required: the MAC or USB serial number). */
	const char *board_id; /* <= 39 bytes */
	const char *type;     /* <= 31 bytes, e.g. "oled-tile" */
	const char *firmware; /* <= 23 bytes, e.g. "1.0.0" */
	uint32_t display_width, display_height; /* 0 x 0: no display */
	bool display_color;
	agentsdeck_link_v1_Input inputs[4];
	uint8_t input_count;
	uint32_t max_frame; /* 0: DECK_LINK_MAX_FRAME */

	/* The subscription sent after each approved Welcome (see deck_subscribe). */
	uint32_t first_slot;
	uint32_t slot_count; /* 0: every agent */
	uint32_t fields;     /* agentsdeck_link_v1_FieldGroup bits; 0: TILE */

	/* Events (all optional). */
	void (*on_welcome)(deck_link_t *ctx, const char *device_name, agentsdeck_link_v1_Scope scope, bool approved);
	void (*on_snapshot_begin)(deck_link_t *ctx, const agentsdeck_link_v1_Server *server, uint32_t agent_count);
	void (*on_agent)(deck_link_t *ctx, const agentsdeck_link_v1_Agent *agent);
	void (*on_agent_removed)(deck_link_t *ctx, const char *id);
	void (*on_snapshot_end)(deck_link_t *ctx);
	void (*on_result)(deck_link_t *ctx, uint32_t seq, uint32_t code, const char *error);
	void (*on_hat_list)(deck_link_t *ctx, uint32_t seq, const agentsdeck_link_v1_HatList *list);
	void (*on_live_sessions)(deck_link_t *ctx, uint32_t seq, const agentsdeck_link_v1_LiveSessions *live);
	void (*on_restore_result)(deck_link_t *ctx, uint32_t seq, const agentsdeck_link_v1_RestoreResult *res);
	void (*on_online)(deck_link_t *ctx);
	void (*on_offline)(deck_link_t *ctx);
	void (*on_ping)(deck_link_t *ctx, uint32_t seq, uint64_t time_ms); /* already answered */
	void (*on_frame)(deck_link_t *ctx, const agentsdeck_link_v1_Frame *frame); /* every frame, before the above */

	/* Pairing and Wi-Fi (docs/link/protocol.md, "Pairing and Wi-Fi"). */
	/* The reply to deck_cert_request(): the board's certificate and where to
	 * use it (deck_store.h keeps it). */
	void (*on_cert_issued)(deck_link_t *ctx, uint32_t seq, const agentsdeck_link_v1_CertIssued *cert);
	/* The deck sends Wi-Fi credentials (forget: drop them). Return the code
	 * the core answers with: 204 stored, 400 bad, 500 failed. Unset: 501. */
	uint32_t (*on_wifi_config)(deck_link_t *ctx, const char *ssid, const char *password, bool forget);
} deck_link_config_t;

typedef struct {
	uint32_t rx_frames;   /* valid frames received */
	uint32_t rx_bad;      /* chunks with the magic that failed (COBS, CRC, protobuf) */
	uint32_t rx_overflow; /* frames dropped for being larger than the buffer */
	uint32_t tx_frames;   /* frames written */
	uint32_t tx_errors;   /* frames that didn't fit or were written short */
} deck_link_stats_t;

/* Everything the link needs, in one struct: declare it static or global
 * (about 8 KB with the defaults; see README). Treat the fields as private. */
struct deck_link {
	deck_link_config_t cfg;
	deck_link_state_t state;
	bool approved;
	agentsdeck_link_v1_Scope scope;
	char cert_serial[sizeof(((agentsdeck_link_v1_Welcome *)0)->cert_serial)]; /* the last Welcome's */
	uint32_t seq;
	uint32_t last_rx_ms;    /* last valid frame */
	uint32_t last_hello_ms; /* last Hello sent */
	size_t rx_len;
	bool rx_discard; /* dropping an oversized frame until the next 0x00 */
	deck_link_stats_t stats;
	agentsdeck_link_v1_Subscribe sub;
	uint8_t rx[DECK_LINK_RX_SIZE];
	uint8_t tx[DECK_LINK_TX_SIZE];
	agentsdeck_link_v1_Frame frame; /* the frame being dispatched */
};

/* Copies cfg into ctx and resets it. Call once, before deck_link_begin(). */
void deck_link_init(deck_link_t *ctx, const deck_link_config_t *cfg);
/* Sends Hello and starts the handshake (again, if called later). */
void deck_link_begin(deck_link_t *ctx);
/* Feeds received bytes: decodes complete frames and calls the callbacks.
 * Noise (boot log, text) is skipped; it resyncs at the next 0x00. */
void deck_link_feed(deck_link_t *ctx, const uint8_t *data, size_t len);
/* Runs the timers: Hello retries and offline detection. Call it often. */
void deck_link_poll(deck_link_t *ctx);

deck_link_state_t deck_link_state(const deck_link_t *ctx);
bool deck_link_online(const deck_link_t *ctx);   /* state == ONLINE */
bool deck_link_approved(const deck_link_t *ctx); /* the last Welcome said approved */
agentsdeck_link_v1_Scope deck_link_scope(const deck_link_t *ctx);
void *deck_link_user(const deck_link_t *ctx);
const deck_link_stats_t *deck_link_stats(const deck_link_t *ctx);

/* Commands. Each sends one frame and returns its seq (the deck's reply
 * carries it), or 0 if the frame couldn't be sent. Strings longer than
 * link.options allows are cut. Replies arrive in on_result (or on_hat_list,
 * on_live_sessions, on_restore_result). */
uint32_t deck_get_state(deck_link_t *ctx);
uint32_t deck_answer(deck_link_t *ctx, const char *agent_id, const char *prompt_id, const char *key);
uint32_t deck_focus(deck_link_t *ctx, const char *agent_id);
uint32_t deck_interrupt(deck_link_t *ctx, const char *agent_id);
uint32_t deck_dismiss(deck_link_t *ctx, const char *agent_id);
uint32_t deck_resume(deck_link_t *ctx, const char *agent_id);
uint32_t deck_move(deck_link_t *ctx, const char *agent_id, uint32_t slot);
uint32_t deck_order(deck_link_t *ctx, const char *const *agent_ids, size_t n);
/* folder is used when agent_id is NULL or empty; color may be NULL. */
uint32_t deck_set_hat(deck_link_t *ctx, const char *agent_id, const char *folder, const char *shape,
		      const char *color, bool auto_hat);
uint32_t deck_list_hats(deck_link_t *ctx);
uint32_t deck_list_live(deck_link_t *ctx);
uint32_t deck_add_live(deck_link_t *ctx, const char *session_id, int32_t slot); /* slot -1: anywhere */
uint32_t deck_restore(deck_link_t *ctx);
/* Replaces the subscription (also the one sent after future Welcomes). */
uint32_t deck_subscribe(deck_link_t *ctx, uint32_t first_slot, uint32_t slot_count, uint32_t fields);
/* Pings the deck (it answers with a Pong); rarely needed. */
uint32_t deck_ping(deck_link_t *ctx);

/* Pairing and Wi-Fi. deck_cert_request sends a certificate signing request
 * (DER, at most agentsdeck_link_v1_CertRequest_size - 3 bytes; deck_pair.h
 * makes one) over USB serial; the deck answers in on_cert_issued, or with a
 * Result (403 not approved, 400 bad request). deck_net_status tells the deck
 * how the board is connected (`agentctl link` shows it). */
uint32_t deck_cert_request(deck_link_t *ctx, const uint8_t *csr, size_t len);
uint32_t deck_net_status(deck_link_t *ctx, const agentsdeck_link_v1_NetStatus *status);
/* The serial of the certificate the deck holds for this board, from the last
 * Welcome ("" for none). A board whose stored certificate has another serial
 * asks for a new one. */
const char *deck_link_cert_serial(const deck_link_t *ctx);

/* Low level: encodes one frame (seq + the body field `tag` holding `msg` of
 * type `fields`) into wire bytes, delimiter included. Returns the length, or
 * 0 if it doesn't fit in out_size. Used by the core and the tests. */
size_t deck_link_encode(uint32_t seq, pb_size_t tag, const pb_msgdesc_t *fields, const void *msg,
			uint8_t *out, size_t out_size);

#ifdef __cplusplus
}
#endif

#endif
