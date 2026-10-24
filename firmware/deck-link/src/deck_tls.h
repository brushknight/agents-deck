/* Deck Link over Wi-Fi: a TLS 1.3 client transport for the deck's network
 * listener (docs/link/protocol.md, "Pairing and Wi-Fi").
 *
 * deck_tls_open() connects to one of the deck's hosts with the board's client
 * certificate (deck_store.h), verifies the deck against its CA (or, when the
 * host isn't one the deck's certificate names, against the listener's
 * fingerprint), and turns the connection into a Deck Link stream with
 * `GET /v1/link` + `Upgrade: deck-link`. From then on it is a byte stream for
 * the core: deck_tls_write is a deck_write_fn, and what deck_tls_read returns
 * goes to deck_link_feed().
 *
 * mbedTLS 3 and BSD sockets (lwIP on ESP-IDF, POSIX on a host). Blocking
 * while it connects (up to timeout_ms per step), non-blocking after. Without
 * TLS 1.3 (deck_net_config.h) deck_tls_open returns DECK_TLS_UNSUPPORTED.
 */
#ifndef DECK_TLS_H
#define DECK_TLS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "deck_net_config.h"
#include "deck_store.h"

#if DECK_LINK_HAS_TLS13
#include "mbedtls/ctr_drbg.h"
#include "mbedtls/entropy.h"
#include "mbedtls/pk.h"
#include "mbedtls/ssl.h"
#include "mbedtls/x509_crt.h"
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef enum {
	DECK_TLS_OK = 0,
	DECK_TLS_UNSUPPORTED,     /* no TLS 1.3 in this build */
	DECK_TLS_BAD_CONFIG,      /* no usable certificate, key or CA */
	DECK_TLS_RESOLVE,         /* the host name didn't resolve */
	DECK_TLS_CONNECT,         /* no TCP connection (deck off, wrong address, firewall) */
	DECK_TLS_HANDSHAKE,       /* TLS failed (timeout, protocol) */
	DECK_TLS_SERVER_UNTRUSTED,/* the server isn't this deck (CA and fingerprint don't match) */
	DECK_TLS_CERT_REJECTED,   /* the deck refused our certificate: revoked or replaced, pair again */
	DECK_TLS_UPGRADE,         /* the deck didn't switch to Deck Link */
	DECK_TLS_CLOSED,          /* the connection ended */
} deck_tls_err_t;

typedef struct {
	bool open;
#if DECK_LINK_HAS_TLS13
	int fd;
	mbedtls_ssl_context ssl;
	mbedtls_ssl_config conf;
	mbedtls_x509_crt ca, own;
	mbedtls_pk_context key;
	mbedtls_entropy_context ent;
	mbedtls_ctr_drbg_context drbg;
	uint8_t fp[32];
	bool has_fp;
	bool inited;
#endif
	uint8_t pre[512]; /* stream bytes that came with the HTTP answer */
	size_t pre_len, pre_off;
	uint32_t write_timeout_ms;
	int mbedtls_error; /* the last mbedTLS error, for logs */
	int alert;         /* the last fatal alert from the deck, 0 for none */
} deck_tls_t;

/* Connects to host (a name or an address) on c->port and upgrades to Deck
 * Link. addr, when set, is where host resolved to (an mDNS answer): it is
 * dialled while host is still the name checked against the deck's
 * certificate. Call deck_tls_close() after, whatever it returns. */
deck_tls_err_t deck_tls_open(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr,
			     uint32_t timeout_ms);
/* Reads what has arrived: > 0 bytes, 0 nothing yet, < 0 -deck_tls_err_t
 * (the connection is over). */
int deck_tls_read(deck_tls_t *t, uint8_t *buf, size_t cap);
/* A deck_write_fn: user is the deck_tls_t. Writes all or nothing. */
size_t deck_tls_write(const uint8_t *data, size_t len, void *user);
void deck_tls_close(deck_tls_t *t);
const char *deck_tls_strerror(deck_tls_err_t e);

#ifdef __cplusplus
}
#endif

#endif
