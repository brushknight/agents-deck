/* Deck Link: download a firmware update over Wi-Fi (docs/link/protocol.md,
 * "Firmware updates over Wi-Fi").
 *
 * deck_fw_download() fetches the image a FirmwareOffer names from the deck's
 * network listener (deck_tls_get: TLS 1.3, the board's certificate), hands it
 * to a sink in order, a chunk at a time (bounded RAM: one 4 KB buffer), and
 * checks its size and SHA-256 against the offer. A dropped connection is
 * resumed with HTTP Range from what the sink already took, a few times.
 *
 * It blocks for the whole download: run it in a task of its own and report
 * progress from your main loop with deck_firmware_status() (the Deck Link
 * core isn't thread-safe; the sink's progress callback runs in the download's
 * task). The sink decides where the bytes go: on an ESP32, esp_ota_write()
 * into the other app slot (panel-core's ota.h); on a host, a file.
 *
 * Needs TLS 1.3 (deck_net_config.h); without it, it returns DECK_FW_UNSUPPORTED.
 */
#ifndef DECK_FW_H
#define DECK_FW_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "deck_store.h"
#include "proto/link.pb.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef enum {
	DECK_FW_OK = 0,
	DECK_FW_UNSUPPORTED, /* no TLS 1.3 in this build */
	DECK_FW_CONNECT,     /* the deck couldn't be reached (after the retries) */
	DECK_FW_REFUSED,     /* the deck said no (403, 404, 503): err has its words */
	DECK_FW_CERT,        /* the deck refused the certificate */
	DECK_FW_SHORT,       /* the connection kept dropping before the end */
	DECK_FW_TOO_BIG,     /* the deck sent more than the offer's size */
	DECK_FW_CHECKSUM,    /* the image's SHA-256 isn't the offer's */
	DECK_FW_SINK,        /* the sink failed (a flash write) */
	DECK_FW_CANCELLED,   /* cancel was set */
} deck_fw_err_t;

typedef struct {
	/* Takes the next len bytes of the image, at offset (always the end of
	 * what it took before). false: stop with DECK_FW_SINK. */
	bool (*write)(void *user, uint64_t offset, const uint8_t *data, size_t len);
	/* Progress, from the download's task (optional). */
	void (*progress)(void *user, uint64_t done, uint64_t total);
	void *user;
	/* Set it (from anywhere) to stop at the next chunk (optional). */
	volatile bool *cancel;
	uint32_t timeout_ms; /* per connect and per read; 0: 15000 */
	uint8_t retries;     /* reconnects after a drop; 0: 4 */
} deck_fw_sink_t;

/* Downloads offer->path from host (addr: where it resolved, or NULL) into the
 * sink and checks it. err (may be NULL) gets a short reason for
 * FirmwareStatus.error. */
deck_fw_err_t deck_fw_download(const deck_cert_t *c, const char *host, const char *addr,
			       const agentsdeck_link_v1_FirmwareOffer *offer, const deck_fw_sink_t *sink, char *err,
			       size_t err_size);

const char *deck_fw_strerror(deck_fw_err_t e);

#ifdef __cplusplus
}
#endif

#endif
