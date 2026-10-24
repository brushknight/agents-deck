/* Deck Link: does this build have what Wi-Fi to the deck needs?
 *
 * The deck's network listener accepts TLS 1.3 only, with a client
 * certificate. That needs mbedTLS 3 with MBEDTLS_SSL_PROTO_TLS1_3 (ESP-IDF 5:
 * CONFIG_MBEDTLS_SSL_PROTO_TLS1_3=y) and CSR writing for pairing. ESP-IDF 4.4
 * (arduino-esp32 2.0.x) ships mbedTLS 2.28, which can't do TLS 1.3: there
 * deck_pair.h and deck_tls.h compile to stubs that say so, and a board stays
 * on USB.
 *
 * DECK_LINK_HAS_TLS13 is 1 when the feature is in; -DDECK_LINK_NO_WIFI turns
 * it off.
 */
#ifndef DECK_NET_CONFIG_H
#define DECK_NET_CONFIG_H

#if !defined(DECK_LINK_NO_WIFI) && defined(__has_include)
#if __has_include(<mbedtls/build_info.h>)
#include <mbedtls/build_info.h>
#if defined(MBEDTLS_SSL_PROTO_TLS1_3) && defined(MBEDTLS_SSL_CLI_C) && defined(MBEDTLS_X509_CSR_WRITE_C) && \
	defined(MBEDTLS_ECP_C) && defined(MBEDTLS_PK_WRITE_C)
#define DECK_LINK_HAS_TLS13 1
#endif
#endif
#endif

#ifndef DECK_LINK_HAS_TLS13
#define DECK_LINK_HAS_TLS13 0
#endif

/* Why there's no Wi-Fi, for the screen and the logs. */
#define DECK_LINK_NO_TLS13_REASON "no TLS 1.3 in this build (needs mbedTLS 3 with MBEDTLS_SSL_PROTO_TLS1_3)"

#endif
