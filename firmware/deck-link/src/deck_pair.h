/* Deck Link pairing helper: the board's own key and its certificate request.
 *
 * The board makes an EC P-256 key on the chip (it never leaves it), keeps it
 * with deck_store.h, and sends a CSR for it over USB serial
 * (deck_cert_request). An approved board gets its certificate back in
 * on_cert_issued (docs/link/protocol.md, "Pairing and Wi-Fi").
 *
 * Needs mbedTLS 3 (deck_net_config.h); otherwise every call fails with
 * DECK_PAIR_UNSUPPORTED.
 */
#ifndef DECK_PAIR_H
#define DECK_PAIR_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "deck_link.h"
#include "deck_net_config.h"
#include "deck_store.h"

#ifdef __cplusplus
extern "C" {
#endif

#define DECK_PAIR_OK 0
#define DECK_PAIR_UNSUPPORTED -1 /* no mbedTLS 3 / TLS 1.3 in this build */
#define DECK_PAIR_FAILED -2      /* mbedTLS failed (no entropy, no memory) */
#define DECK_PAIR_TOO_BIG -3     /* the output buffer is too small */

#define DECK_CSR_MAX sizeof(((agentsdeck_link_v1_CertRequest *)0)->csr.bytes)

/* A new EC P-256 private key, DER (SEC1), into der[cap]. */
int deck_pair_make_key(uint8_t *der, size_t cap, size_t *len);
/* A certificate signing request for key (DER), subject CN=cn, DER. */
int deck_pair_make_csr(const uint8_t *key, size_t key_len, const char *cn, uint8_t *der, size_t cap, size_t *len);

/* A pairing in progress: the new key, until its certificate arrives. */
typedef struct {
	uint8_t key[DECK_KEY_MAX];
	size_t key_len;
	uint32_t seq; /* the CertRequest's, 0: none pending */
} deck_pairing_t;

/* The usual flow: a new key (pairing again makes a new one) and its CSR,
 * sent with deck_cert_request(). Returns the request's seq, or 0. The
 * certificate the board has now keeps working until the new one arrives. */
uint32_t deck_pair_request(deck_link_t *link, deck_pairing_t *p, const char *cn);
/* In on_cert_issued: the reply to p's request becomes the board's
 * certificate (out, with p's key), ready for deck_store_save_cert(). False
 * for another seq or an unusable reply. p is wiped either way when it's ours. */
bool deck_pair_accept(deck_pairing_t *p, uint32_t seq, const agentsdeck_link_v1_CertIssued *in, deck_cert_t *out);
/* The deck refused (a Result for p's seq): forget the new key. */
void deck_pair_cancel(deck_pairing_t *p);

#ifdef __cplusplus
}
#endif

#endif
