/* Deck Link: what a board keeps across resets to use Wi-Fi (docs/link/protocol.md,
 * "Pairing and Wi-Fi"): its private key, its client certificate with the deck's
 * CA and address, and the Wi-Fi network. C99, no malloc.
 *
 * Storage goes through a small key-value interface (deck_kv_t), so the same
 * code runs on ESP32 NVS (deck_kv_nvs_open, ESP-IDF only) and in host tests
 * with a fake. Keys are at most 15 characters (an NVS limit).
 *
 * NVS is plain flash: the key and the Wi-Fi password can be read off a board
 * someone holds, unless flash encryption (and NVS encryption) is enabled.
 * Revoke a lost board: `agentctl devices revoke <name>`.
 */
#ifndef DECK_STORE_H
#define DECK_STORE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "proto/link.pb.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
	/* Reads key into buf (*len: its size in, the value's length out). False if missing or too big. */
	bool (*get)(void *user, const char *key, void *buf, size_t *len);
	bool (*set)(void *user, const char *key, const void *buf, size_t len);
	bool (*erase)(void *user, const char *key); /* true also when it wasn't there */
	void *user;
} deck_kv_t;

#define DECK_KEY_MAX 160 /* an EC P-256 private key, DER (about 121 bytes) */
#define DECK_CERT_MAX sizeof(((agentsdeck_link_v1_CertIssued *)0)->cert.bytes)
#define DECK_CA_MAX sizeof(((agentsdeck_link_v1_CertIssued *)0)->ca.bytes)
#define DECK_HOSTS_MAX 4
#define DECK_HOST_SIZE sizeof(((agentsdeck_link_v1_CertIssued *)0)->hosts[0])
#define DECK_SERIAL_SIZE sizeof(((agentsdeck_link_v1_CertIssued *)0)->cert_serial)
#define DECK_NAME_SIZE sizeof(((agentsdeck_link_v1_CertIssued *)0)->device_name)

/* The board's identity for Wi-Fi: a key it made, the certificate the deck
 * signed for it, and where the deck listens. */
typedef struct {
	uint8_t key[DECK_KEY_MAX];
	size_t key_len;
	uint8_t cert[DECK_CERT_MAX]; /* DER */
	size_t cert_len;
	uint8_t ca[DECK_CA_MAX]; /* DER: the deck's CA */
	size_t ca_len;
	char hosts[DECK_HOSTS_MAX][DECK_HOST_SIZE]; /* the Mac's .local name, then its addresses */
	uint8_t host_count;
	uint16_t port;
	uint8_t server_fp[32]; /* SHA-256 of the listener's certificate */
	bool has_server_fp;
	char device_name[DECK_NAME_SIZE];
	char cert_serial[DECK_SERIAL_SIZE];
} deck_cert_t;

#define DECK_SSID_SIZE sizeof(((agentsdeck_link_v1_WifiConfig *)0)->ssid)
#define DECK_PASSWORD_SIZE sizeof(((agentsdeck_link_v1_WifiConfig *)0)->password)

typedef struct {
	char ssid[DECK_SSID_SIZE];
	char password[DECK_PASSWORD_SIZE];
} deck_wifi_t;

/* A certificate that can be used: key, certificate, CA and a host. */
bool deck_cert_valid(const deck_cert_t *c);
/* Fills c (keeping c->key) from the deck's CertIssued. False if it doesn't fit or is incomplete. */
bool deck_cert_from_issued(deck_cert_t *c, const agentsdeck_link_v1_CertIssued *in);
/* Does the board need a (new) certificate? No valid one, or the deck holds
 * another (welcome_serial is deck_link_cert_serial(): "" means none). */
bool deck_cert_needed(const deck_cert_t *c, const char *welcome_serial);

bool deck_store_load_cert(const deck_kv_t *kv, deck_cert_t *c);
bool deck_store_save_cert(const deck_kv_t *kv, const deck_cert_t *c);
bool deck_store_clear_cert(const deck_kv_t *kv);

bool deck_store_load_wifi(const deck_kv_t *kv, deck_wifi_t *w);
bool deck_store_save_wifi(const deck_kv_t *kv, const deck_wifi_t *w);
bool deck_store_clear_wifi(const deck_kv_t *kv);

#if defined(ESP_PLATFORM)
/* NVS on ESP-IDF: opens (initialising NVS if needed) namespace ns ("decklink"). */
bool deck_kv_nvs_open(deck_kv_t *kv, const char *ns);
#endif

#ifdef __cplusplus
}
#endif

#endif
