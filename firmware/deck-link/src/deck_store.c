/* Deck Link: the board's Wi-Fi certificate and network, kept in a key-value
 * store (deck_store.h). */
#include "deck_store.h"

#include <string.h>

/* Keys (NVS allows 15 characters). The whole certificate is one blob: NVS
 * writes a blob all or nothing, so a reset while saving never leaves a key
 * with another key's certificate. */
#define K_CERT "dl_cert"
#define K_WIFI "dl_wifi"

#define CERT_VERSION 1

typedef struct {
	uint8_t version;
	deck_cert_t cert;
} cert_blob_t;

#define WIFI_VERSION 1

typedef struct {
	uint8_t version;
	char ssid[DECK_SSID_SIZE];
	char password[DECK_PASSWORD_SIZE];
} wifi_blob_t;

static void copy_str(char *dst, size_t size, const char *src)
{
	size_t n = src ? strlen(src) : 0;
	if (n > size - 1)
		n = 0; /* too long: leave dst empty rather than cut a name */
	memcpy(dst, src ? src : "", n);
	dst[n] = '\0';
}

static bool terminated(const char *s, size_t size) { return memchr(s, '\0', size) != NULL; }

bool deck_cert_valid(const deck_cert_t *c)
{
	return c && c->key_len > 0 && c->key_len <= sizeof c->key && c->cert_len > 0 && c->cert_len <= sizeof c->cert &&
	       c->ca_len > 0 && c->ca_len <= sizeof c->ca && c->host_count > 0 && c->host_count <= DECK_HOSTS_MAX &&
	       c->port > 0 && c->hosts[0][0] && c->cert_serial[0];
}

bool deck_cert_needed(const deck_cert_t *c, const char *welcome_serial)
{
	if (!deck_cert_valid(c))
		return true;
	return !welcome_serial || strcmp(c->cert_serial, welcome_serial) != 0;
}

bool deck_cert_from_issued(deck_cert_t *c, const agentsdeck_link_v1_CertIssued *in)
{
	if (in->cert.size == 0 || in->cert.size > sizeof c->cert || in->ca.size == 0 || in->ca.size > sizeof c->ca ||
	    in->hosts_count == 0 || in->port == 0 || in->port > 65535 || !in->cert_serial[0])
		return false;
	memcpy(c->cert, in->cert.bytes, in->cert.size);
	c->cert_len = in->cert.size;
	memcpy(c->ca, in->ca.bytes, in->ca.size);
	c->ca_len = in->ca.size;
	memset(c->hosts, 0, sizeof c->hosts);
	c->host_count = 0;
	for (pb_size_t i = 0; i < in->hosts_count && i < DECK_HOSTS_MAX; i++) {
		if (!in->hosts[i][0])
			continue;
		copy_str(c->hosts[c->host_count], DECK_HOST_SIZE, in->hosts[i]);
		c->host_count++;
	}
	c->port = (uint16_t)in->port;
	c->has_server_fp = in->server_fingerprint.size == sizeof c->server_fp;
	if (c->has_server_fp)
		memcpy(c->server_fp, in->server_fingerprint.bytes, sizeof c->server_fp);
	copy_str(c->device_name, sizeof c->device_name, in->device_name);
	copy_str(c->cert_serial, sizeof c->cert_serial, in->cert_serial);
	return c->host_count > 0;
}

static bool get_blob(const deck_kv_t *kv, const char *key, void *buf, size_t want)
{
	size_t n = want;
	return kv && kv->get && kv->get(kv->user, key, buf, &n) && n == want;
}

bool deck_store_load_cert(const deck_kv_t *kv, deck_cert_t *c)
{
	static cert_blob_t b; /* about 2 KB: not on a small task's stack */
	memset(c, 0, sizeof *c);
	bool ok = get_blob(kv, K_CERT, &b, sizeof b) && b.version == CERT_VERSION && deck_cert_valid(&b.cert) &&
		  terminated(b.cert.device_name, sizeof b.cert.device_name) &&
		  terminated(b.cert.cert_serial, sizeof b.cert.cert_serial);
	for (uint8_t i = 0; ok && i < b.cert.host_count; i++)
		ok = terminated(b.cert.hosts[i], sizeof b.cert.hosts[i]);
	if (ok)
		*c = b.cert;
	memset(&b, 0, sizeof b);
	return ok;
}

bool deck_store_save_cert(const deck_kv_t *kv, const deck_cert_t *c)
{
	static cert_blob_t b;
	if (!kv || !kv->set || !deck_cert_valid(c))
		return false;
	memset(&b, 0, sizeof b);
	b.version = CERT_VERSION;
	b.cert = *c;
	bool ok = kv->set(kv->user, K_CERT, &b, sizeof b);
	memset(&b, 0, sizeof b);
	return ok;
}

bool deck_store_clear_cert(const deck_kv_t *kv) { return kv && kv->erase && kv->erase(kv->user, K_CERT); }

bool deck_store_load_wifi(const deck_kv_t *kv, deck_wifi_t *w)
{
	memset(w, 0, sizeof *w);
	wifi_blob_t b;
	if (!get_blob(kv, K_WIFI, &b, sizeof b) || b.version != WIFI_VERSION ||
	    !terminated(b.ssid, sizeof b.ssid) || !terminated(b.password, sizeof b.password) || !b.ssid[0]) {
		memset(&b, 0, sizeof b);
		return false;
	}
	memcpy(w->ssid, b.ssid, sizeof w->ssid);
	memcpy(w->password, b.password, sizeof w->password);
	memset(&b, 0, sizeof b);
	return true;
}

bool deck_store_save_wifi(const deck_kv_t *kv, const deck_wifi_t *w)
{
	if (!kv || !kv->set || !w->ssid[0])
		return false;
	wifi_blob_t b;
	memset(&b, 0, sizeof b);
	b.version = WIFI_VERSION;
	copy_str(b.ssid, sizeof b.ssid, w->ssid);
	copy_str(b.password, sizeof b.password, w->password);
	bool ok = b.ssid[0] && kv->set(kv->user, K_WIFI, &b, sizeof b);
	memset(&b, 0, sizeof b);
	return ok;
}

bool deck_store_clear_wifi(const deck_kv_t *kv) { return kv && kv->erase && kv->erase(kv->user, K_WIFI); }

/* ---- NVS (ESP-IDF) ---------------------------------------------------------- */

#if defined(ESP_PLATFORM)
#include "nvs.h"
#include "nvs_flash.h"

static bool nvs_get_cb(void *user, const char *key, void *buf, size_t *len)
{
	return nvs_get_blob((nvs_handle_t)(uintptr_t)user, key, buf, len) == ESP_OK;
}

static bool nvs_set_cb(void *user, const char *key, const void *buf, size_t len)
{
	nvs_handle_t h = (nvs_handle_t)(uintptr_t)user;
	return nvs_set_blob(h, key, buf, len) == ESP_OK && nvs_commit(h) == ESP_OK;
}

static bool nvs_erase_cb(void *user, const char *key)
{
	nvs_handle_t h = (nvs_handle_t)(uintptr_t)user;
	esp_err_t e = nvs_erase_key(h, key);
	return (e == ESP_OK || e == ESP_ERR_NVS_NOT_FOUND) && nvs_commit(h) == ESP_OK;
}

bool deck_kv_nvs_open(deck_kv_t *kv, const char *ns)
{
	esp_err_t e = nvs_flash_init();
	if (e == ESP_ERR_NVS_NO_FREE_PAGES || e == ESP_ERR_NVS_NEW_VERSION_FOUND) {
		/* Not erased here: that would drop every other namespace too. */
		return false;
	}
	nvs_handle_t h;
	if (nvs_open(ns, NVS_READWRITE, &h) != ESP_OK)
		return false;
	kv->get = nvs_get_cb;
	kv->set = nvs_set_cb;
	kv->erase = nvs_erase_cb;
	kv->user = (void *)(uintptr_t)h;
	return true;
}
#endif
