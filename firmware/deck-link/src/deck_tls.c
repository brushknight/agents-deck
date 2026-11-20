/* Deck Link over Wi-Fi: see deck_tls.h. */
#if !defined(_POSIX_C_SOURCE) && !defined(ESP_PLATFORM)
#define _POSIX_C_SOURCE 200809L
#endif
#define MBEDTLS_ALLOW_PRIVATE_ACCESS /* the received alert's code (deck_tls_read) */

#include "deck_tls.h"

#include <string.h>

const char *deck_tls_strerror(deck_tls_err_t e)
{
	switch (e) {
	case DECK_TLS_OK: return "ok";
	case DECK_TLS_UNSUPPORTED: return DECK_LINK_NO_TLS13_REASON;
	case DECK_TLS_BAD_CONFIG: return "no usable certificate: pair over USB";
	case DECK_TLS_RESOLVE: return "the deck's name didn't resolve";
	case DECK_TLS_CONNECT: return "the deck didn't answer";
	case DECK_TLS_HANDSHAKE: return "TLS failed";
	case DECK_TLS_SERVER_UNTRUSTED: return "the server isn't this deck";
	case DECK_TLS_CERT_REJECTED: return "the deck refused the certificate: pair again over USB";
	case DECK_TLS_UPGRADE: return "the deck didn't start Deck Link";
	case DECK_TLS_CLOSED: return "connection closed";
	}
	return "error";
}

#if DECK_LINK_HAS_TLS13

#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <stdio.h>
#include <stdlib.h>
#include <strings.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#include "mbedtls/net_sockets.h"
#include "mbedtls/sha256.h"
#if defined(MBEDTLS_PSA_CRYPTO_C)
#include "psa/crypto.h"
#endif

/* TLS alerts that mean "your certificate": bad, unsupported, revoked, expired, unknown, unknown CA, required. */
static bool cert_alert(int a) { return (a >= 42 && a <= 46) || a == 48 || a == 116; }

static uint32_t now_ms(void)
{
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (uint32_t)ts.tv_sec * 1000u + (uint32_t)(ts.tv_nsec / 1000000);
}

/* Waits until fd is readable (or writable) or ms pass. */
static bool wait_fd(int fd, bool write, uint32_t ms)
{
	fd_set set;
	FD_ZERO(&set);
	FD_SET(fd, &set);
	struct timeval tv = {(long)(ms / 1000), (long)((ms % 1000) * 1000)};
	int r = select(fd + 1, write ? NULL : &set, write ? &set : NULL, NULL, &tv);
	return r > 0;
}

static int bio_send(void *ctx, const unsigned char *buf, size_t len)
{
	deck_tls_t *t = (deck_tls_t *)ctx;
	ssize_t n = send(t->fd, buf, len, 0);
	if (n >= 0)
		return (int)n;
	if (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR)
		return MBEDTLS_ERR_SSL_WANT_WRITE;
	return MBEDTLS_ERR_NET_SEND_FAILED;
}

static int bio_recv(void *ctx, unsigned char *buf, size_t len)
{
	deck_tls_t *t = (deck_tls_t *)ctx;
	ssize_t n = recv(t->fd, buf, len, 0);
	if (n > 0)
		return (int)n;
	if (n == 0)
		return MBEDTLS_ERR_SSL_PEER_CLOSE_NOTIFY;
	if (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR)
		return MBEDTLS_ERR_SSL_WANT_READ;
	return MBEDTLS_ERR_NET_RECV_FAILED;
}

/* The deck's certificate must chain to its CA. When the host we dialled isn't
 * one of its names (a new address), its SHA-256 from pairing stands in, as
 * for the deck's other clients (docs/protocol.md, "Pairing"). */
static int verify_cb(void *p, mbedtls_x509_crt *crt, int depth, uint32_t *flags)
{
	deck_tls_t *t = (deck_tls_t *)p;
	if (depth == 0 && (*flags & MBEDTLS_X509_BADCERT_CN_MISMATCH) && t->has_fp) {
		unsigned char sum[32];
		if (mbedtls_sha256(crt->raw.p, crt->raw.len, sum, 0) == 0 && memcmp(sum, t->fp, sizeof sum) == 0)
			*flags &= ~(uint32_t)MBEDTLS_X509_BADCERT_CN_MISMATCH;
	}
	return 0;
}

static int tcp_connect(const char *host, uint16_t port, uint32_t timeout_ms, deck_tls_err_t *err)
{
	char ports[8];
	snprintf(ports, sizeof ports, "%u", (unsigned)port);
	struct addrinfo hints, *res = NULL;
	memset(&hints, 0, sizeof hints);
	hints.ai_family = AF_UNSPEC;
	hints.ai_socktype = SOCK_STREAM;
	if (getaddrinfo(host, ports, &hints, &res) != 0 || !res) {
		*err = DECK_TLS_RESOLVE;
		return -1;
	}
	int fd = -1;
	*err = DECK_TLS_CONNECT;
	for (struct addrinfo *a = res; a && fd < 0; a = a->ai_next) {
		fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
		if (fd < 0)
			continue;
		fcntl(fd, F_SETFL, fcntl(fd, F_GETFL, 0) | O_NONBLOCK);
		int r = connect(fd, a->ai_addr, a->ai_addrlen);
		if (r != 0 && errno == EINPROGRESS && wait_fd(fd, true, timeout_ms)) {
			int so = 0;
			socklen_t sl = sizeof so;
			r = getsockopt(fd, SOL_SOCKET, SO_ERROR, &so, &sl) == 0 && so == 0 ? 0 : -1;
		}
		if (r != 0) {
			close(fd);
			fd = -1;
		}
	}
	freeaddrinfo(res);
	return fd;
}

static deck_tls_err_t fail(deck_tls_t *t, deck_tls_err_t e, int mbed)
{
	t->mbedtls_error = mbed;
	return e;
}

/* A fatal alert from the deck: what it was. */
static deck_tls_err_t alert_error(deck_tls_t *t, int rc)
{
	if (rc == MBEDTLS_ERR_SSL_FATAL_ALERT_MESSAGE && t->ssl.in_msg)
		t->alert = t->ssl.in_msg[1];
	if (rc == MBEDTLS_ERR_SSL_FATAL_ALERT_MESSAGE && cert_alert(t->alert))
		return DECK_TLS_CERT_REJECTED;
	return DECK_TLS_CLOSED;
}

/* Writes everything or fails, within the timeout. */
static bool write_all(deck_tls_t *t, const uint8_t *d, size_t n, uint32_t timeout_ms)
{
	uint32_t start = now_ms();
	while (n > 0) {
		int r = mbedtls_ssl_write(&t->ssl, d, n);
		if (r > 0) {
			d += r;
			n -= (size_t)r;
			continue;
		}
		if (r != MBEDTLS_ERR_SSL_WANT_WRITE && r != MBEDTLS_ERR_SSL_WANT_READ) {
			t->mbedtls_error = r;
			return false;
		}
		if ((uint32_t)(now_ms() - start) >= timeout_ms)
			return false;
		wait_fd(t->fd, r == MBEDTLS_ERR_SSL_WANT_WRITE, 50);
	}
	return true;
}

/* A header's value in the HTTP head (case-insensitive name), or NULL. */
static const char *header(const char *head, const char *name)
{
	size_t n = strlen(name);
	for (const char *l = strstr(head, "\r\n"); l && l[2]; l = strstr(l + 2, "\r\n")) {
		const char *k = l + 2;
		if (strncasecmp(k, name, n) == 0 && k[n] == ':') {
			k += n + 1;
			while (*k == ' ' || *k == '\t')
				k++;
			return k;
		}
	}
	return NULL;
}

/* Reads an HTTP answer's head; keeps what follows it (the stream, or the
 * start of the body). code: its status; length: Content-Length (-1 none);
 * first: where a 206's Content-Range starts (0 otherwise). */
static deck_tls_err_t read_head(deck_tls_t *t, uint32_t timeout_ms, int *code, long long *length,
				unsigned long long *first)
{
	char buf[1024];
	size_t len = 0;
	uint32_t start = now_ms();
	*length = -1;
	*first = 0;
	for (;;) {
		int r = mbedtls_ssl_read(&t->ssl, (unsigned char *)buf + len, sizeof buf - 1 - len);
		if (r > 0) {
			len += (size_t)r;
			buf[len] = '\0';
			char *end = strstr(buf, "\r\n\r\n");
			if (end) {
				end[2] = '\0'; /* the head, each line ending in \r\n */
				if (sscanf(buf, "HTTP/1.%*d %d", code) != 1)
					return DECK_TLS_UPGRADE;
				const char *v = header(buf, "Content-Length");
				if (v)
					*length = strtoll(v, NULL, 10);
				if ((v = header(buf, "Content-Range")) != NULL && strncmp(v, "bytes ", 6) == 0)
					*first = strtoull(v + 6, NULL, 10);
				size_t rest = len - (size_t)(end + 4 - buf);
				if (rest > sizeof t->pre)
					return DECK_TLS_UPGRADE;
				memcpy(t->pre, end + 4, rest);
				t->pre_len = rest;
				t->pre_off = 0;
				return DECK_TLS_OK;
			}
			if (len >= sizeof buf - 1)
				return DECK_TLS_UPGRADE;
			continue;
		}
		if (r == MBEDTLS_ERR_SSL_RECEIVED_NEW_SESSION_TICKET)
			continue;
		if (r != MBEDTLS_ERR_SSL_WANT_READ && r != MBEDTLS_ERR_SSL_WANT_WRITE) {
			t->mbedtls_error = r;
			return alert_error(t, r) == DECK_TLS_CERT_REJECTED ? DECK_TLS_CERT_REJECTED : DECK_TLS_UPGRADE;
		}
		if ((uint32_t)(now_ms() - start) >= timeout_ms)
			return DECK_TLS_UPGRADE;
		wait_fd(t->fd, false, 50);
	}
}

/* Reads the HTTP answer to the upgrade; keeps what follows it. */
static deck_tls_err_t read_upgrade(deck_tls_t *t, uint32_t timeout_ms)
{
	int code = 0;
	long long length;
	unsigned long long first;
	deck_tls_err_t e = read_head(t, timeout_ms, &code, &length, &first);
	if (e != DECK_TLS_OK)
		return e;
	if (code == 401 || code == 403)
		return DECK_TLS_CERT_REJECTED;
	return code == 101 ? DECK_TLS_OK : DECK_TLS_UPGRADE;
}

/* TLS 1.3 to the deck with the board's certificate, up to the end of the handshake. */
static deck_tls_err_t tls_connect(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr,
				  uint32_t timeout_ms)
{
	memset(t, 0, sizeof *t);
	t->fd = -1;
	t->write_timeout_ms = 3000;
	if (!deck_cert_valid(c) || !host || !host[0])
		return DECK_TLS_BAD_CONFIG;
#if defined(MBEDTLS_PSA_CRYPTO_C)
	(void)psa_crypto_init(); /* TLS 1.3 runs on PSA */
#endif
	mbedtls_ssl_init(&t->ssl);
	mbedtls_ssl_config_init(&t->conf);
	mbedtls_x509_crt_init(&t->ca);
	mbedtls_x509_crt_init(&t->own);
	mbedtls_pk_init(&t->key);
	mbedtls_entropy_init(&t->ent);
	mbedtls_ctr_drbg_init(&t->drbg);
	t->inited = true;
	memcpy(t->fp, c->server_fp, sizeof t->fp);
	t->has_fp = c->has_server_fp;

	static const unsigned char pers[] = "deck-link tls";
	int rc = mbedtls_ctr_drbg_seed(&t->drbg, mbedtls_entropy_func, &t->ent, pers, sizeof pers - 1);
	if (rc)
		return fail(t, DECK_TLS_BAD_CONFIG, rc);
	if ((rc = mbedtls_x509_crt_parse_der(&t->ca, c->ca, c->ca_len)) != 0 ||
	    (rc = mbedtls_x509_crt_parse_der(&t->own, c->cert, c->cert_len)) != 0 ||
	    (rc = mbedtls_pk_parse_key(&t->key, c->key, c->key_len, NULL, 0, mbedtls_ctr_drbg_random, &t->drbg)) != 0)
		return fail(t, DECK_TLS_BAD_CONFIG, rc);
	if ((rc = mbedtls_ssl_config_defaults(&t->conf, MBEDTLS_SSL_IS_CLIENT, MBEDTLS_SSL_TRANSPORT_STREAM,
					      MBEDTLS_SSL_PRESET_DEFAULT)) != 0)
		return fail(t, DECK_TLS_BAD_CONFIG, rc);
	/* TLS 1.3 only, like the deck's listener. */
	mbedtls_ssl_conf_min_tls_version(&t->conf, MBEDTLS_SSL_VERSION_TLS1_3);
	mbedtls_ssl_conf_max_tls_version(&t->conf, MBEDTLS_SSL_VERSION_TLS1_3);
	mbedtls_ssl_conf_authmode(&t->conf, MBEDTLS_SSL_VERIFY_REQUIRED);
	mbedtls_ssl_conf_ca_chain(&t->conf, &t->ca, NULL);
	mbedtls_ssl_conf_verify(&t->conf, verify_cb, t);
	mbedtls_ssl_conf_rng(&t->conf, mbedtls_ctr_drbg_random, &t->drbg);
	if ((rc = mbedtls_ssl_conf_own_cert(&t->conf, &t->own, &t->key)) != 0 ||
	    (rc = mbedtls_ssl_setup(&t->ssl, &t->conf)) != 0 || (rc = mbedtls_ssl_set_hostname(&t->ssl, host)) != 0)
		return fail(t, DECK_TLS_BAD_CONFIG, rc);

	deck_tls_err_t err;
	t->fd = tcp_connect(addr && addr[0] ? addr : host, c->port, timeout_ms, &err);
	if (t->fd < 0)
		return err;
	mbedtls_ssl_set_bio(&t->ssl, t, bio_send, bio_recv, NULL);

	uint32_t start = now_ms();
	while ((rc = mbedtls_ssl_handshake(&t->ssl)) != 0) {
		if (rc != MBEDTLS_ERR_SSL_WANT_READ && rc != MBEDTLS_ERR_SSL_WANT_WRITE) {
			t->mbedtls_error = rc;
			if (rc == MBEDTLS_ERR_X509_CERT_VERIFY_FAILED)
				return DECK_TLS_SERVER_UNTRUSTED;
			return alert_error(t, rc) == DECK_TLS_CERT_REJECTED ? DECK_TLS_CERT_REJECTED : DECK_TLS_HANDSHAKE;
		}
		if ((uint32_t)(now_ms() - start) >= timeout_ms)
			return DECK_TLS_HANDSHAKE;
		wait_fd(t->fd, rc == MBEDTLS_ERR_SSL_WANT_WRITE, 50);
	}
	return DECK_TLS_OK;
}

deck_tls_err_t deck_tls_open(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr,
			     uint32_t timeout_ms)
{
	deck_tls_err_t err = tls_connect(t, c, host, addr, timeout_ms);
	if (err != DECK_TLS_OK)
		return err;
	/* In TLS 1.3 the deck checks our certificate after the handshake: a
	 * revoked one shows up as an alert on the first read. */
	char req[160];
	int n = snprintf(req, sizeof req,
			 "GET /v1/link HTTP/1.1\r\nHost: %s\r\nUpgrade: deck-link\r\nConnection: Upgrade\r\n\r\n", host);
	if (n <= 0 || (size_t)n >= sizeof req || !write_all(t, (const uint8_t *)req, (size_t)n, timeout_ms))
		return alert_error(t, t->mbedtls_error) == DECK_TLS_CERT_REJECTED ? DECK_TLS_CERT_REJECTED
										   : DECK_TLS_UPGRADE;
	err = read_upgrade(t, timeout_ms);
	if (err != DECK_TLS_OK)
		return err;
	t->open = true;
	return DECK_TLS_OK;
}

deck_tls_err_t deck_tls_get(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr, const char *path,
			    uint64_t from, uint32_t timeout_ms, int *status, int64_t *length)
{
	*status = 0;
	*length = -1;
	if (!path || path[0] != '/' || strpbrk(path, " \r\n"))
		return DECK_TLS_BAD_CONFIG;
	deck_tls_err_t err = tls_connect(t, c, host, addr, timeout_ms);
	if (err != DECK_TLS_OK)
		return err;
	char req[320];
	int n = snprintf(req, sizeof req, "GET %s HTTP/1.1\r\nHost: %s\r\nRange: bytes=%llu-\r\nConnection: close\r\n\r\n",
			 path, host, (unsigned long long)from);
	if (n <= 0 || (size_t)n >= sizeof req || !write_all(t, (const uint8_t *)req, (size_t)n, timeout_ms))
		return alert_error(t, t->mbedtls_error) == DECK_TLS_CERT_REJECTED ? DECK_TLS_CERT_REJECTED
										   : DECK_TLS_UPGRADE;
	long long len;
	unsigned long long first;
	err = read_head(t, timeout_ms, status, &len, &first);
	if (err != DECK_TLS_OK)
		return err;
	if (*status == 401)
		return DECK_TLS_CERT_REJECTED;
	/* What we asked for: the whole image from 0 (200, or 206 from 0), or the rest from `from` (206). */
	if ((*status == 200 && from == 0) || (*status == 206 && first == from))
		*length = len;
	else if (*status == 200 || *status == 206)
		return DECK_TLS_UPGRADE;
	t->open = true;
	return DECK_TLS_OK;
}

int deck_tls_read_wait(deck_tls_t *t, uint8_t *buf, size_t cap, uint32_t timeout_ms)
{
	uint32_t start = now_ms();
	for (;;) {
		int n = deck_tls_read(t, buf, cap);
		if (n != 0)
			return n;
		if ((uint32_t)(now_ms() - start) >= timeout_ms)
			return 0;
		wait_fd(t->fd, false, 50);
	}
}

int deck_tls_read(deck_tls_t *t, uint8_t *buf, size_t cap)
{
	if (!t->open)
		return -(int)DECK_TLS_CLOSED;
	if (t->pre_off < t->pre_len) {
		size_t n = t->pre_len - t->pre_off;
		if (n > cap)
			n = cap;
		memcpy(buf, t->pre + t->pre_off, n);
		t->pre_off += n;
		return (int)n;
	}
	for (;;) {
		int r = mbedtls_ssl_read(&t->ssl, buf, cap);
		if (r > 0)
			return r;
		if (r == MBEDTLS_ERR_SSL_RECEIVED_NEW_SESSION_TICKET)
			continue;
		if (r == MBEDTLS_ERR_SSL_WANT_READ || r == MBEDTLS_ERR_SSL_WANT_WRITE)
			return 0;
		t->mbedtls_error = r;
		t->open = false;
		return -(int)alert_error(t, r);
	}
}

size_t deck_tls_write(const uint8_t *data, size_t len, void *user)
{
	deck_tls_t *t = (deck_tls_t *)user;
	if (!t || !t->open)
		return 0;
	if (!write_all(t, data, len, t->write_timeout_ms)) {
		t->open = false;
		return 0;
	}
	return len;
}

void deck_tls_close(deck_tls_t *t)
{
	if (!t->inited)
		return;
	if (t->open)
		(void)mbedtls_ssl_close_notify(&t->ssl);
	t->open = false;
	if (t->fd >= 0)
		close(t->fd);
	t->fd = -1;
	mbedtls_ssl_free(&t->ssl);
	mbedtls_ssl_config_free(&t->conf);
	mbedtls_x509_crt_free(&t->ca);
	mbedtls_x509_crt_free(&t->own);
	mbedtls_pk_free(&t->key);
	mbedtls_ctr_drbg_free(&t->drbg);
	mbedtls_entropy_free(&t->ent);
	t->inited = false;
}

#else /* no TLS 1.3 */

deck_tls_err_t deck_tls_open(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr,
			     uint32_t timeout_ms)
{
	(void)c;
	(void)host;
	(void)addr;
	(void)timeout_ms;
	memset(t, 0, sizeof *t);
	return DECK_TLS_UNSUPPORTED;
}

int deck_tls_read(deck_tls_t *t, uint8_t *buf, size_t cap)
{
	(void)t;
	(void)buf;
	(void)cap;
	return -(int)DECK_TLS_UNSUPPORTED;
}

size_t deck_tls_write(const uint8_t *data, size_t len, void *user)
{
	(void)data;
	(void)len;
	(void)user;
	return 0;
}

deck_tls_err_t deck_tls_get(deck_tls_t *t, const deck_cert_t *c, const char *host, const char *addr, const char *path,
			    uint64_t from, uint32_t timeout_ms, int *status, int64_t *length)
{
	(void)c, (void)host, (void)addr, (void)path, (void)from, (void)timeout_ms;
	memset(t, 0, sizeof *t);
	*status = 0;
	*length = -1;
	return DECK_TLS_UNSUPPORTED;
}

int deck_tls_read_wait(deck_tls_t *t, uint8_t *buf, size_t cap, uint32_t timeout_ms)
{
	(void)timeout_ms;
	return deck_tls_read(t, buf, cap);
}

void deck_tls_close(deck_tls_t *t) { t->open = false; }

#endif
