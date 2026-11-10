/* Deck Link: firmware download, see deck_fw.h. */
#include "deck_fw.h"

#include <stdio.h>
#include <string.h>

#include "deck_tls.h"

const char *deck_fw_strerror(deck_fw_err_t e)
{
	switch (e) {
	case DECK_FW_OK: return "ok";
	case DECK_FW_UNSUPPORTED: return "no TLS 1.3 in this build";
	case DECK_FW_CONNECT: return "the deck couldn't be reached";
	case DECK_FW_REFUSED: return "the deck refused the download";
	case DECK_FW_CERT: return "the deck refused the certificate";
	case DECK_FW_SHORT: return "the download kept dropping";
	case DECK_FW_TOO_BIG: return "the image is longer than offered";
	case DECK_FW_CHECKSUM: return "the image's SHA-256 doesn't match";
	case DECK_FW_SINK: return "writing the image failed";
	case DECK_FW_CANCELLED: return "cancelled";
	}
	return "error";
}

static deck_fw_err_t done(deck_fw_err_t e, char *err, size_t n, const char *why)
{
	if (err && n)
		snprintf(err, n, "%s", why ? why : deck_fw_strerror(e));
	return e;
}

#if DECK_LINK_HAS_TLS13

#include "mbedtls/sha256.h"

/* The download's buffers and contexts: one download at a time, kept off the
 * caller's stack (deck_tls_t holds the mbedTLS contexts). */
static deck_tls_t fw_tls;
static uint8_t fw_buf[4096];

deck_fw_err_t deck_fw_download(const deck_cert_t *c, const char *host, const char *addr,
			       const agentsdeck_link_v1_FirmwareOffer *o, const deck_fw_sink_t *sink, char *err,
			       size_t err_size)
{
	const uint32_t timeout = sink->timeout_ms ? sink->timeout_ms : 15000;
	const uint8_t retries = sink->retries ? sink->retries : 4;
	const uint64_t total = o->size;
	uint64_t have = 0;
	uint8_t drops = 0;
	deck_fw_err_t result = DECK_FW_OK;
	char why[96] = "";
	mbedtls_sha256_context sha;
	mbedtls_sha256_init(&sha);
	mbedtls_sha256_starts(&sha, 0);

	while (have < total) {
		if (sink->cancel && *sink->cancel) {
			result = DECK_FW_CANCELLED;
			break;
		}
		int status = 0;
		int64_t length = -1;
		deck_tls_err_t te = deck_tls_get(&fw_tls, c, host, addr, o->path, have, timeout, &status, &length);
		if (te == DECK_TLS_CERT_REJECTED) {
			deck_tls_close(&fw_tls);
			result = DECK_FW_CERT;
			break;
		}
		if (te != DECK_TLS_OK) {
			deck_tls_close(&fw_tls);
			if (++drops > retries) {
				result = have ? DECK_FW_SHORT : DECK_FW_CONNECT;
				break;
			}
			continue;
		}
		if (status != 200 && status != 206) {
			/* The deck's JSON error, as it came: {"error":"…"}. */
			int n = deck_tls_read_wait(&fw_tls, fw_buf, sizeof fw_buf - 1, 2000);
			fw_buf[n > 0 ? n : 0] = '\0';
			const char *msg = strstr((const char *)fw_buf, "\"error\":\"");
			if (msg) {
				msg += 9;
				size_t len = strcspn(msg, "\"");
				snprintf(why, sizeof why, "%d %.*s", status, (int)len, msg);
			} else {
				snprintf(why, sizeof why, "the deck answered %d", status);
			}
			deck_tls_close(&fw_tls);
			result = DECK_FW_REFUSED;
			break;
		}
		if (length >= 0 && (uint64_t)length != total - have) {
			deck_tls_close(&fw_tls);
			result = DECK_FW_TOO_BIG;
			snprintf(why, sizeof why, "the deck has %llu bytes, the offer says %llu",
				 (unsigned long long)(have + (uint64_t)length), (unsigned long long)total);
			break;
		}
		/* The body, until it ends or drops. */
		for (;;) {
			if (sink->cancel && *sink->cancel) {
				result = DECK_FW_CANCELLED;
				break;
			}
			int n = deck_tls_read_wait(&fw_tls, fw_buf, sizeof fw_buf, timeout);
			if (n <= 0)
				break; /* closed, or nothing for the timeout: resume */
			if (have + (uint64_t)n > total) {
				result = DECK_FW_TOO_BIG;
				break;
			}
			if (!sink->write(sink->user, have, fw_buf, (size_t)n)) {
				result = DECK_FW_SINK;
				break;
			}
			mbedtls_sha256_update(&sha, fw_buf, (size_t)n);
			have += (uint64_t)n;
			if (sink->progress)
				sink->progress(sink->user, have, total);
			if (have == total)
				break;
		}
		deck_tls_close(&fw_tls);
		if (result != DECK_FW_OK)
			break;
		if (have < total && ++drops > retries) {
			result = DECK_FW_SHORT;
			break;
		}
	}
	if (result == DECK_FW_OK) {
		uint8_t sum[32];
		mbedtls_sha256_finish(&sha, sum);
		if (o->sha256.size != 32 || memcmp(sum, o->sha256.bytes, 32) != 0)
			result = DECK_FW_CHECKSUM;
	}
	mbedtls_sha256_free(&sha);
	memset(fw_buf, 0, sizeof fw_buf);
	return done(result, err, err_size, why[0] ? why : NULL);
}

#else /* no TLS 1.3 */

deck_fw_err_t deck_fw_download(const deck_cert_t *c, const char *host, const char *addr,
			       const agentsdeck_link_v1_FirmwareOffer *o, const deck_fw_sink_t *sink, char *err,
			       size_t err_size)
{
	(void)c, (void)host, (void)addr, (void)o, (void)sink;
	return done(DECK_FW_UNSUPPORTED, err, err_size, NULL);
}

#endif
