/* Deck Link pairing helper: see deck_pair.h. */
#include "deck_pair.h"

#include <string.h>

#if DECK_LINK_HAS_TLS13

#include <stdio.h>

#include "mbedtls/ctr_drbg.h"
#include "mbedtls/ecp.h"
#include "mbedtls/entropy.h"
#include "mbedtls/pk.h"
#include "mbedtls/x509_csr.h"
#if defined(MBEDTLS_PSA_CRYPTO_C)
#include "psa/crypto.h"
#endif
#if defined(ESP_PLATFORM)
#include "bootloader_random.h"
#endif

typedef struct {
	mbedtls_entropy_context ent;
	mbedtls_ctr_drbg_context drbg;
	bool ok;
} rng_t;

static void rng_open(rng_t *r)
{
#if defined(MBEDTLS_PSA_CRYPTO_C)
	(void)psa_crypto_init();
#endif
#if defined(ESP_PLATFORM)
	/* With the radio off, the chip's RNG needs this entropy source to be truly random. */
	bootloader_random_enable();
#endif
	mbedtls_entropy_init(&r->ent);
	mbedtls_ctr_drbg_init(&r->drbg);
	static const unsigned char pers[] = "deck-link pairing";
	r->ok = mbedtls_ctr_drbg_seed(&r->drbg, mbedtls_entropy_func, &r->ent, pers, sizeof pers - 1) == 0;
}

static void rng_close(rng_t *r)
{
	mbedtls_ctr_drbg_free(&r->drbg);
	mbedtls_entropy_free(&r->ent);
#if defined(ESP_PLATFORM)
	bootloader_random_disable();
#endif
}

/* mbedTLS writes DER at the end of the buffer: move it to the front. */
static int to_front(int n, uint8_t *buf, size_t cap, size_t *len)
{
	if (n == MBEDTLS_ERR_ASN1_BUF_TOO_SMALL)
		return DECK_PAIR_TOO_BIG;
	if (n <= 0)
		return DECK_PAIR_FAILED;
	memmove(buf, buf + cap - (size_t)n, (size_t)n);
	*len = (size_t)n;
	return DECK_PAIR_OK;
}

int deck_pair_make_key(uint8_t *der, size_t cap, size_t *len)
{
	rng_t r;
	mbedtls_pk_context pk;
	int rc = DECK_PAIR_FAILED;
	rng_open(&r);
	mbedtls_pk_init(&pk);
	if (r.ok && mbedtls_pk_setup(&pk, mbedtls_pk_info_from_type(MBEDTLS_PK_ECKEY)) == 0 &&
	    mbedtls_ecp_gen_key(MBEDTLS_ECP_DP_SECP256R1, mbedtls_pk_ec(pk), mbedtls_ctr_drbg_random, &r.drbg) == 0)
		rc = to_front(mbedtls_pk_write_key_der(&pk, der, cap), der, cap, len);
	mbedtls_pk_free(&pk);
	rng_close(&r);
	return rc;
}

int deck_pair_make_csr(const uint8_t *key, size_t key_len, const char *cn, uint8_t *der, size_t cap, size_t *len)
{
	rng_t r;
	mbedtls_pk_context pk;
	mbedtls_x509write_csr req;
	char subject[80];
	int rc = DECK_PAIR_FAILED;
	/* The deck names the board itself; the CN is for whoever reads the CSR.
	 * Keep it to characters that need no escaping in a DN. */
	size_t n = 0;
	for (const char *p = cn ? cn : ""; *p && n < 40; p++)
		if ((*p >= 'a' && *p <= 'z') || (*p >= 'A' && *p <= 'Z') || (*p >= '0' && *p <= '9') || *p == '-' ||
		    *p == '.' || *p == ':')
			subject[3 + n++] = *p;
	memcpy(subject, "CN=", 3);
	if (n == 0)
		memcpy(subject + 3, "board", (n = 5));
	subject[3 + n] = '\0';

	rng_open(&r);
	mbedtls_pk_init(&pk);
	mbedtls_x509write_csr_init(&req);
	if (r.ok && mbedtls_pk_parse_key(&pk, key, key_len, NULL, 0, mbedtls_ctr_drbg_random, &r.drbg) == 0) {
		mbedtls_x509write_csr_set_md_alg(&req, MBEDTLS_MD_SHA256);
		mbedtls_x509write_csr_set_key(&req, &pk);
		if (mbedtls_x509write_csr_set_subject_name(&req, subject) == 0)
			rc = to_front(mbedtls_x509write_csr_der(&req, der, cap, mbedtls_ctr_drbg_random, &r.drbg), der,
				      cap, len);
	}
	mbedtls_x509write_csr_free(&req);
	mbedtls_pk_free(&pk);
	rng_close(&r);
	return rc;
}

uint32_t deck_pair_request(deck_link_t *link, deck_pairing_t *p, const char *cn)
{
	uint8_t csr[DECK_CSR_MAX];
	size_t n = 0;
	deck_pair_cancel(p);
	if (deck_pair_make_key(p->key, sizeof p->key, &p->key_len) != DECK_PAIR_OK ||
	    deck_pair_make_csr(p->key, p->key_len, cn, csr, sizeof csr, &n) != DECK_PAIR_OK) {
		deck_pair_cancel(p);
		return 0;
	}
	p->seq = deck_cert_request(link, csr, n);
	if (!p->seq)
		deck_pair_cancel(p);
	return p->seq;
}

#else /* no TLS 1.3: pairing is pointless, the board can't use the certificate */

int deck_pair_make_key(uint8_t *der, size_t cap, size_t *len)
{
	(void)der;
	(void)cap;
	(void)len;
	return DECK_PAIR_UNSUPPORTED;
}

int deck_pair_make_csr(const uint8_t *key, size_t key_len, const char *cn, uint8_t *der, size_t cap, size_t *len)
{
	(void)key;
	(void)key_len;
	(void)cn;
	(void)der;
	(void)cap;
	(void)len;
	return DECK_PAIR_UNSUPPORTED;
}

uint32_t deck_pair_request(deck_link_t *link, deck_pairing_t *p, const char *cn)
{
	(void)link;
	(void)cn;
	deck_pair_cancel(p);
	return 0;
}

#endif

void deck_pair_cancel(deck_pairing_t *p)
{
	memset(p->key, 0, sizeof p->key);
	p->key_len = 0;
	p->seq = 0;
}

bool deck_pair_accept(deck_pairing_t *p, uint32_t seq, const agentsdeck_link_v1_CertIssued *in, deck_cert_t *out)
{
	if (!p->seq || seq != p->seq)
		return false;
	bool ok = p->key_len > 0 && p->key_len <= sizeof out->key;
	deck_cert_t c;
	memset(&c, 0, sizeof c);
	if (ok) {
		memcpy(c.key, p->key, p->key_len);
		c.key_len = p->key_len;
		ok = deck_cert_from_issued(&c, in);
	}
	if (ok)
		*out = c;
	memset(&c, 0, sizeof c);
	deck_pair_cancel(p);
	return ok;
}
