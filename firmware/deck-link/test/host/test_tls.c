/* Host tests for pairing with mbedTLS 3 (deck_pair.c): the board's key and
 * CSR. The TLS transport itself runs against the real deck in the daemon's
 * Go test (board_wifi). Build: make test_tls (needs mbedTLS 3, e.g. Homebrew). */
#include <stdio.h>
#include <string.h>

#include "deck_pair.h"
#include "deck_tls.h"
#include "mbedtls/pk.h"
#include "mbedtls/x509_csr.h"

static int failures, checks;
#define CHECK(c)                                                                          \
	do {                                                                              \
		checks++;                                                                 \
		if (!(c)) {                                                               \
			failures++;                                                       \
			fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #c); \
		}                                                                         \
	} while (0)

int main(void)
{
	CHECK(DECK_LINK_HAS_TLS13);
	uint8_t key[DECK_KEY_MAX], key2[DECK_KEY_MAX], csr[DECK_CSR_MAX];
	size_t kn = 0, kn2 = 0, cn = 0;
	CHECK(deck_pair_make_key(key, sizeof key, &kn) == DECK_PAIR_OK && kn > 100 && kn <= sizeof key);
	CHECK(deck_pair_make_key(key2, sizeof key2, &kn2) == DECK_PAIR_OK && (kn != kn2 || memcmp(key, key2, kn) != 0));
	CHECK(deck_pair_make_key(key2, 20, &kn2) == DECK_PAIR_TOO_BIG);

	CHECK(deck_pair_make_csr(key, kn, "board \"x\"/,=", csr, sizeof csr, &cn) == DECK_PAIR_OK && cn > 0 &&
	      cn <= sizeof csr);
	/* It parses, its signature checks out, and it is for our key (P-256). */
	mbedtls_x509_csr req;
	mbedtls_x509_csr_init(&req);
	CHECK(mbedtls_x509_csr_parse_der(&req, csr, cn) == 0);
	char dn[128];
	CHECK(mbedtls_x509_dn_gets(dn, sizeof dn, &req.subject) > 0 && strcmp(dn, "CN=boardx") == 0);
	CHECK(mbedtls_pk_get_type(&req.pk) == MBEDTLS_PK_ECKEY && mbedtls_pk_get_bitlen(&req.pk) == 256);
	mbedtls_x509_csr_free(&req);
	CHECK(deck_pair_make_csr(key, 10, "x", csr, sizeof csr, &cn) == DECK_PAIR_FAILED);
	CHECK(deck_pair_make_csr(key, kn, "x", csr, 30, &cn) != DECK_PAIR_OK);

	/* A transport without a certificate doesn't start. */
	deck_tls_t t;
	deck_cert_t none;
	memset(&none, 0, sizeof none);
	CHECK(deck_tls_open(&t, &none, "127.0.0.1", NULL, 100) == DECK_TLS_BAD_CONFIG);
	deck_tls_close(&t);
	CHECK(deck_tls_read(&t, csr, sizeof csr) < 0 && deck_tls_write(csr, 1, &t) == 0);

	if (failures) {
		printf("FAIL: %d of %d checks failed\n", failures, checks);
		return 1;
	}
	printf("ok: %d checks passed (mbedTLS pairing)\n", checks);
	return 0;
}
