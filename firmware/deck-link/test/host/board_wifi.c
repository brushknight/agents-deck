/* board_wifi: a Deck Link board on the host that pairs over "USB serial" and
 * then connects over the network, with the kit's own pairing (deck_pair.c)
 * and TLS 1.3 transport (deck_tls.c) on mbedTLS 3. The daemon's Go test
 * (backend/internal/link/wifi_kit_test.go) runs it against the real deck.
 *
 *   board_wifi --serial --store DIR [--board-id ID] [--seconds N]
 *       stdin/stdout are the serial line: Hello; once approved, a key and a
 *       CSR (CertRequest); the certificate it gets back and any Wi-Fi
 *       credentials (WifiConfig) are kept in DIR, one file per key, as NVS
 *       keeps them on a board.
 *   board_wifi --wifi --store DIR [--board-id ID] [--seconds N]
 *       loads the certificate from DIR and connects to the deck it names,
 *       host by host, over TLS 1.3 (GET /v1/link), then runs the usual
 *       session: Hello, Welcome, Subscribe, the snapshot.
 *
 * stderr, one event per line:
 *   welcome name=<n> scope=<0-3> approved=<0|1> cert=<serial or ->
 *   pair request seq=<n>          paired serial=<s> name=<n> hosts=<n>
 *   pair refused code=<n> error=<rest>
 *   wifi ssid=<s> forget=<0|1>    (never the password)
 *   tls connected host=<h>        tls failed host=<h> error=<n> <text>
 *   snapshot_begin agents=<n>     agent id=<id> status=<s>    snapshot_end
 *   closed error=<n> <text>
 * Exit status: 0 at --seconds or EOF, 3 when no host could be reached, 4
 * when the certificate was rejected.
 */
#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "deck_link.h"
#include "deck_pair.h"
#include "deck_store.h"
#include "deck_tiles.h"
#include "deck_tls.h"

#if !DECK_LINK_HAS_TLS13
#error "board_wifi needs mbedTLS 3 with TLS 1.3 (make board_wifi points at Homebrew's)"
#endif

static const char *store_dir = ".";
static deck_kv_t kv;
static deck_cert_t cert;
static deck_pairing_t pairing;
static deck_tls_t tls;
static int write_failed;

/* ---- a key-value store in files (NVS on a board) ---------------------------- */

static void kv_path(char *out, size_t n, const char *key) { snprintf(out, n, "%s/%s", store_dir, key); }

static bool fkv_get(void *u, const char *key, void *buf, size_t *len)
{
	(void)u;
	char p[512];
	kv_path(p, sizeof p, key);
	FILE *f = fopen(p, "rb");
	if (!f)
		return false;
	size_t n = fread(buf, 1, *len, f);
	int more = fgetc(f) != EOF;
	fclose(f);
	if (more)
		return false;
	*len = n;
	return true;
}

static bool fkv_set(void *u, const char *key, const void *buf, size_t len)
{
	(void)u;
	char p[512], tmp[520];
	kv_path(p, sizeof p, key);
	snprintf(tmp, sizeof tmp, "%s.tmp", p);
	FILE *f = fopen(tmp, "wb");
	if (!f)
		return false;
	bool ok = fwrite(buf, 1, len, f) == len;
	ok = fclose(f) == 0 && ok;
	return ok && rename(tmp, p) == 0;
}

static bool fkv_erase(void *u, const char *key)
{
	(void)u;
	char p[512];
	kv_path(p, sizeof p, key);
	return unlink(p) == 0 || errno == ENOENT;
}

/* ---- the board ---------------------------------------------------------------- */

static size_t serial_write(const uint8_t *d, size_t n, void *user)
{
	(void)user;
	size_t done = 0;
	while (done < n) {
		ssize_t w = write(STDOUT_FILENO, d + done, n - done);
		if (w < 0) {
			if (errno == EINTR)
				continue;
			write_failed = 1;
			break;
		}
		done += (size_t)w;
	}
	return done;
}

static uint32_t now_ms(void *user)
{
	(void)user;
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (uint32_t)((uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u);
}

static bool over_serial;

static void on_welcome(deck_link_t *c, const char *name, agentsdeck_link_v1_Scope scope, bool approved)
{
	const char *serial = deck_link_cert_serial(c);
	fprintf(stderr, "welcome name=%s scope=%d approved=%d cert=%s\n", name, (int)scope, approved ? 1 : 0,
		serial[0] ? serial : "-");
	/* Pairing: over USB only, once approved, when the deck doesn't hold our certificate. */
	if (over_serial && approved && !pairing.seq && deck_cert_needed(&cert, serial)) {
		uint32_t seq = deck_pair_request(c, &pairing, "board-wifi");
		fprintf(stderr, "pair request seq=%u\n", (unsigned)seq);
	}
}

static void on_cert_issued(deck_link_t *c, uint32_t seq, const agentsdeck_link_v1_CertIssued *in)
{
	(void)c;
	deck_cert_t next;
	if (!deck_pair_accept(&pairing, seq, in, &next)) {
		fprintf(stderr, "pair refused code=0 error=unusable certificate\n");
		return;
	}
	if (!deck_store_save_cert(&kv, &next)) {
		fprintf(stderr, "pair refused code=0 error=can't save\n");
		return;
	}
	cert = next;
	fprintf(stderr, "paired serial=%s name=%s hosts=%u\n", cert.cert_serial, cert.device_name,
		(unsigned)cert.host_count);
}

static void on_result(deck_link_t *c, uint32_t seq, uint32_t code, const char *err)
{
	(void)c;
	if (pairing.seq && seq == pairing.seq) {
		fprintf(stderr, "pair refused code=%u error=%s\n", (unsigned)code, err);
		deck_pair_cancel(&pairing);
	}
}

static uint32_t on_wifi_config(deck_link_t *c, const char *ssid, const char *password, bool forget)
{
	(void)c;
	fprintf(stderr, "wifi ssid=%s forget=%d\n", forget ? "-" : ssid, forget ? 1 : 0);
	if (forget)
		return deck_store_clear_wifi(&kv) ? 204 : 500;
	deck_wifi_t w;
	memset(&w, 0, sizeof w);
	snprintf(w.ssid, sizeof w.ssid, "%s", ssid);
	snprintf(w.password, sizeof w.password, "%s", password);
	bool ok = deck_store_save_wifi(&kv, &w);
	memset(&w, 0, sizeof w);
	return ok ? 204 : 500;
}

static void on_snapshot_begin(deck_link_t *c, const agentsdeck_link_v1_Server *s, uint32_t n)
{
	(void)c, (void)s;
	fprintf(stderr, "snapshot_begin agents=%u\n", (unsigned)n);
}

static void on_agent(deck_link_t *c, const agentsdeck_link_v1_Agent *a)
{
	(void)c;
	fprintf(stderr, "agent id=%s status=%s\n", a->id, deck_status_name(a->status));
}

static void on_snapshot_end(deck_link_t *c)
{
	(void)c;
	fprintf(stderr, "snapshot_end\n");
}

static void usage(void)
{
	fprintf(stderr, "usage: board_wifi (--serial | --wifi) --store DIR [--board-id ID] [--seconds N]\n");
	exit(2);
}

int main(int argc, char **argv)
{
	static deck_link_t link;
	deck_link_config_t cfg;
	memset(&cfg, 0, sizeof cfg);
	cfg.board_id = "AA:BB:CC:00:00:77";
	cfg.type = "board-wifi";
	cfg.firmware = "0.1.0";
	cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE;
	long seconds = 10;
	int mode = 0;
	for (int i = 1; i < argc; i++) {
		const char *a = argv[i], *v = i + 1 < argc ? argv[i + 1] : NULL;
		if (strcmp(a, "--serial") == 0)
			mode = 1;
		else if (strcmp(a, "--wifi") == 0)
			mode = 2;
		else if (!v)
			usage();
		else if (strcmp(a, "--store") == 0)
			store_dir = v, i++;
		else if (strcmp(a, "--board-id") == 0)
			cfg.board_id = v, i++;
		else if (strcmp(a, "--seconds") == 0)
			seconds = strtol(v, NULL, 10), i++;
		else
			usage();
	}
	if (!mode)
		usage();
	signal(SIGPIPE, SIG_IGN);
	setvbuf(stderr, NULL, _IOLBF, 0);
	kv = (deck_kv_t){fkv_get, fkv_set, fkv_erase, NULL};
	(void)deck_store_load_cert(&kv, &cert);

	cfg.millis = now_ms;
	cfg.on_welcome = on_welcome;
	cfg.on_cert_issued = on_cert_issued;
	cfg.on_result = on_result;
	cfg.on_snapshot_begin = on_snapshot_begin;
	cfg.on_agent = on_agent;
	cfg.on_snapshot_end = on_snapshot_end;
	int in_fd = STDIN_FILENO;
	if (mode == 1) {
		over_serial = true;
		cfg.write = serial_write;
		cfg.on_wifi_config = on_wifi_config;
	} else {
		if (!deck_cert_valid(&cert)) {
			fprintf(stderr, "tls failed host=- error=%d %s\n", DECK_TLS_BAD_CONFIG,
				deck_tls_strerror(DECK_TLS_BAD_CONFIG));
			return 3;
		}
		/* The deck's hosts in order: its .local name, then its addresses. */
		deck_tls_err_t e = DECK_TLS_CONNECT;
		for (uint8_t i = 0; i < cert.host_count; i++) {
			e = deck_tls_open(&tls, &cert, cert.hosts[i], NULL, 4000);
			if (e == DECK_TLS_OK) {
				fprintf(stderr, "tls connected host=%s\n", cert.hosts[i]);
				break;
			}
			fprintf(stderr, "tls failed host=%s error=%d %s (mbedtls -0x%04x)\n", cert.hosts[i], (int)e,
				deck_tls_strerror(e), (unsigned)-tls.mbedtls_error);
			deck_tls_close(&tls);
			if (e == DECK_TLS_CERT_REJECTED)
				return 4;
		}
		if (e != DECK_TLS_OK)
			return 3;
		cfg.write = deck_tls_write;
		cfg.user = &tls;
		in_fd = tls.fd;
	}
	deck_link_init(&link, &cfg);
	deck_link_begin(&link);

	uint32_t start = now_ms(NULL);
	int status = 0;
	while (!write_failed) {
		if (seconds > 0 && (uint32_t)(now_ms(NULL) - start) >= (uint32_t)seconds * 1000u)
			break;
		uint8_t buf[1024];
		if (mode == 2) {
			int n = deck_tls_read(&tls, buf, sizeof buf);
			if (n > 0) {
				deck_link_feed(&link, buf, (size_t)n);
				continue;
			}
			if (n < 0) {
				fprintf(stderr, "closed error=%d %s\n", -n, deck_tls_strerror((deck_tls_err_t)-n));
				status = -n == DECK_TLS_CERT_REJECTED ? 4 : 0;
				break;
			}
		}
		struct pollfd pfd = {in_fd, POLLIN, 0};
		if (poll(&pfd, 1, 50) > 0 && mode == 1) {
			ssize_t n = read(STDIN_FILENO, buf, sizeof buf);
			if (n == 0)
				break;
			if (n > 0)
				deck_link_feed(&link, buf, (size_t)n);
		}
		deck_link_poll(&link);
	}
	deck_tls_close(&tls);
	return status;
}
