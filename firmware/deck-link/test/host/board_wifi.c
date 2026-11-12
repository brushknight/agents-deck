/* board_wifi: a Deck Link board on the host that pairs over "USB serial" (or
 * "Bluetooth") and then connects over the network, with the kit's own
 * pairing (deck_pair.c), TLS 1.3 transport (deck_tls.c) on mbedTLS 3 and
 * Bluetooth byte stream (deck_ble.c). The daemon's Go test
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
 *   board_wifi --ble PATH --store DIR [--mtu N] [--board-id ID] [--seconds N]
 *       a board whose Bluetooth link is a Unix socket at PATH: whatever
 *       connects plays the deck's Bluetooth helper on an encrypted, bonded
 *       link (the passkey it would have typed is printed, never asked for).
 *       The socket carries what RX and TX would: Deck Link bytes, read and
 *       sent in pieces of at most MTU - 3 (default MTU 185) through
 *       deck_ble.c. One central at a time; it disconnects by closing. Over
 *       it the board pairs (CertRequest), takes Wi-Fi credentials
 *       (WifiConfig) and the uplink preference (LinkConfig), and reports
 *       NetStatus with transport BLE, as a board does.
 *   board_wifi (--serial | --wifi) --ble PATH ...
 *       both at once, as a board with a cable (or Wi-Fi) and Bluetooth: the
 *       first link carries the session while its deck answers; a central
 *       that connects meanwhile is kept for --ble-intro-ms (default 20000)
 *       without a Hello (the deck adds the board then) and reported in
 *       NetStatus on the first link (ble=CONNECTED, then BONDED once it
 *       goes). When the first link ends (stdin closes, TLS closes) Bluetooth
 *       takes the session: Hello on a central already there, or on the next.
 *       A central that connects is taken as bonded from then on (kept in
 *       DIR as ble_bond), as if it had paired with the passkey.
 *   --focus ID
 *       after its first snapshot, asks the deck to focus agent ID (Focus), a
 *       command as a person pressing a tile sends one.
 *   --firmware-board B --firmware-out FILE  (with --wifi)
 *       takes the deck's FirmwareOffer when its board is B: checks a Range
 *       request, downloads the image with deck_fw.c into FILE, checks it and
 *       reports FirmwareStatus as a board does (without the restart).
 *
 * stderr, one event per line:
 *   welcome name=<n> scope=<0-3> approved=<0|1> cert=<serial or ->
 *   pair request seq=<n>          paired serial=<s> name=<n> hosts=<n>
 *   pair refused code=<n> error=<rest>
 *   wifi ssid=<s> forget=<0|1>    (never the password)
 *   tls connected host=<h>        tls failed host=<h> error=<n> <text>
 *   snapshot_begin agents=<n>     agent id=<id> status=<s>    snapshot_end
 *   closed error=<n> <text>
 *   firmware offer board=<b> version=<v> size=<n>   firmware refused <why>
 *   firmware range from=<n> status=<n> length=<n>   firmware failed error=<n> <why>
 *   firmware written bytes=<n>
 *   ble name=<deck-xxxxxx> passkey=<6 digits> info=<the Info value>
 *   ble connected          ble disconnected          uplink value=<0|1|2>
 *   ble standby            (connected while the other link carries: no Hello)
 *   ble carries            (Hello over Bluetooth)
 *   ble dropped why=<intro|other-link>   (the board let the central go)
 *   serial unplugged       wifi gone      (with --ble: the first link ended)
 *   focus seq=<n> id=<id>  result seq=<n> code=<n>   (the deck's answers)
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

#include <sys/socket.h>
#include <sys/un.h>

#include "deck_ble.h"
#include "deck_fw.h"
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

static bool over_serial; /* the first link is serial: pairing and credentials allowed */
static bool over_wifi;	 /* the first link is TLS */
static bool over_ble;	 /* a Bluetooth socket, alone or beside the first link */
static bool primary_up;	 /* the first link is there (stdin open, TLS open) */
static deck_link_t plink; /* the first link's core */
static deck_link_t blink; /* the Bluetooth core */
static bool ble_carrying; /* Bluetooth carries the session (said Hello) */
static bool ble_bonded;

static size_t serial_write(const uint8_t *d, size_t n, void *user)
{
	(void)user;
	size_t done = 0;
	while (done < n) {
		ssize_t w = write(STDOUT_FILENO, d + done, n - done);
		if (w < 0) {
			if (errno == EINTR)
				continue;
			if (over_ble)
				primary_up = false; /* unplugged: Bluetooth may take over */
			else
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

static deck_ble_t ble;
static uint8_t ble_ring[32768];
static int ble_fd = -1;

/* TX: one notification, a write on the socket. */
static bool sock_notify(const uint8_t *d, size_t n, void *user)
{
	(void)user;
	size_t done = 0;
	while (done < n) {
		ssize_t w = write(ble_fd, d + done, n - done);
		if (w < 0 && errno == EINTR)
			continue;
		if (w <= 0)
			return false;
		done += (size_t)w;
	}
	return true;
}

static void send_net_status(deck_link_t *c)
{
	agentsdeck_link_v1_NetStatus s = agentsdeck_link_v1_NetStatus_init_zero;
	deck_wifi_t w;
	s.transport = c == &blink ? agentsdeck_link_v1_Transport_TRANSPORT_BLE
		      : over_serial ? agentsdeck_link_v1_Transport_TRANSPORT_SERIAL
				    : agentsdeck_link_v1_Transport_TRANSPORT_WIFI;
	s.ble = ble_fd >= 0 ? agentsdeck_link_v1_BleState_BLE_CONNECTED
		: ble_bonded ? agentsdeck_link_v1_BleState_BLE_BONDED
			     : agentsdeck_link_v1_BleState_BLE_SETUP;
	s.uplink = deck_store_load_uplink(&kv);
	s.has_cert = deck_cert_valid(&cert);
	if (deck_store_load_wifi(&kv, &w)) {
		snprintf(s.ssid, sizeof s.ssid, "%s", w.ssid);
		s.wifi = c == &plink && over_wifi ? agentsdeck_link_v1_WifiState_WIFI_CONNECTED
						   : agentsdeck_link_v1_WifiState_WIFI_IDLE;
	} else {
		s.wifi = agentsdeck_link_v1_WifiState_WIFI_NOT_SET;
	}
	memset(&w, 0, sizeof w);
	deck_net_status(c, &s);
}

static uint32_t on_link_config(deck_link_t *c, agentsdeck_link_v1_Uplink u)
{
	fprintf(stderr, "uplink value=%d\n", (int)u);
	if ((unsigned)u > agentsdeck_link_v1_Uplink_UPLINK_BLE)
		return 400;
	bool ok = deck_store_save_uplink(&kv, u);
	if (ok)
		send_net_status(c);
	return ok ? 204 : 500;
}
static const char *fw_board, *fw_out, *tls_host;
static FILE *fw_file;
static uint32_t fw_last_pct;

static bool fw_write(void *u, uint64_t off, const uint8_t *d, size_t n)
{
	(void)u, (void)off;
	return fwrite(d, 1, n, fw_file) == n;
}

static void fw_progress(void *u, uint64_t done, uint64_t total)
{
	deck_link_t *c = (deck_link_t *)u;
	uint32_t pct = (uint32_t)(done * 100 / total);
	if (pct >= fw_last_pct + 10 || done == total) {
		fw_last_pct = pct;
		deck_firmware_status(c, agentsdeck_link_v1_FirmwareState_FIRMWARE_DOWNLOADING, pct, NULL);
	}
}

/* A board takes an offer: here synchronously (a board runs it in a task). */
static void on_firmware_offer(deck_link_t *c, const agentsdeck_link_v1_FirmwareOffer *o)
{
	char why[96];
	fprintf(stderr, "firmware offer board=%s version=%s size=%llu\n", o->board, o->version, (unsigned long long)o->size);
	if (!deck_firmware_offer_check(o, fw_board, 0x640000, why, sizeof why)) {
		fprintf(stderr, "firmware refused %s\n", why);
		deck_firmware_status(c, agentsdeck_link_v1_FirmwareState_FIRMWARE_REFUSED, 0, why);
		return;
	}
	/* Resuming: the rest from the middle. */
	deck_tls_t t;
	int status = 0;
	int64_t length = -1;
	uint64_t from = o->size / 2;
	deck_tls_err_t e = deck_tls_get(&t, &cert, tls_host, NULL, o->path, from, 4000, &status, &length);
	fprintf(stderr, "firmware range from=%llu status=%d length=%lld error=%d\n", (unsigned long long)from, status,
		(long long)length, (int)e);
	deck_tls_close(&t);

	fw_file = fopen(fw_out, "wb");
	fw_last_pct = 0;
	deck_fw_sink_t sink = {fw_write, fw_progress, c, NULL, 4000, 2};
	deck_fw_err_t r = fw_file ? deck_fw_download(&cert, tls_host, NULL, o, &sink, why, sizeof why) : DECK_FW_SINK;
	if (fw_file)
		fclose(fw_file);
	if (r != DECK_FW_OK) {
		fprintf(stderr, "firmware failed error=%d %s\n", (int)r, why);
		deck_firmware_status(c, agentsdeck_link_v1_FirmwareState_FIRMWARE_FAILED, fw_last_pct, why);
		return;
	}
	deck_firmware_status(c, agentsdeck_link_v1_FirmwareState_FIRMWARE_VERIFYING, 100, NULL);
	deck_firmware_status(c, agentsdeck_link_v1_FirmwareState_FIRMWARE_RESTARTING, 100, NULL);
	fprintf(stderr, "firmware written bytes=%llu\n", (unsigned long long)o->size);
}

static void on_welcome(deck_link_t *c, const char *name, agentsdeck_link_v1_Scope scope, bool approved)
{
	const char *serial = deck_link_cert_serial(c);
	fprintf(stderr, "welcome name=%s scope=%d approved=%d cert=%s\n", name, (int)scope, approved ? 1 : 0,
		serial[0] ? serial : "-");
	/* Pairing: over USB (or Bluetooth) only, once approved, when the deck doesn't hold our certificate. */
	if (over_ble)
		send_net_status(c);
	bool hands = (c == &plink && over_serial) || (c == &blink && ble_carrying);
	if (hands && approved && !pairing.seq && deck_cert_needed(&cert, serial)) {
		uint32_t seq = deck_pair_request(c, &pairing, "board-wifi");
		fprintf(stderr, "pair request seq=%u\n", (unsigned)seq);
	}
}

static void on_cert_issued(deck_link_t *c, uint32_t seq, const agentsdeck_link_v1_CertIssued *in)
{
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
	if (over_ble)
		send_net_status(c);
}

static void on_result(deck_link_t *c, uint32_t seq, uint32_t code, const char *err)
{
	(void)c;
	fprintf(stderr, "result seq=%u code=%u\n", (unsigned)seq, (unsigned)code);
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

static const char *focus_id;
static bool focus_sent;

static void on_snapshot_end(deck_link_t *c)
{
	fprintf(stderr, "snapshot_end\n");
	if (focus_id && !focus_sent) {
		focus_sent = true;
		uint32_t seq = deck_focus(c, focus_id);
		fprintf(stderr, "focus seq=%u id=%s\n", (unsigned)seq, focus_id);
	}
}

static void usage(void)
{
	fprintf(stderr, "usage: board_wifi (--serial | --wifi | --ble PATH | --serial --ble PATH | --wifi --ble PATH) --store DIR\n\t[--board-id ID] [--seconds N] [--mtu N] [--ble-intro-ms N] [--focus ID] [--firmware VERSION]\n\t[--firmware-board B --firmware-out FILE]\n");
	exit(2);
}

/* The first link carries the session: its deck answered. */
static bool primary_online(void) { return primary_up && deck_link_state(&plink) == DECK_LINK_ONLINE; }

static void ble_close(const char *why)
{
	if (why)
		fprintf(stderr, "ble dropped why=%s\n", why);
	fprintf(stderr, "ble disconnected\n");
	deck_ble_close(&ble);
	close(ble_fd);
	ble_fd = -1;
	ble_carrying = false;
	if (primary_online())
		send_net_status(&plink); /* the deck hears the connection go */
}

static void ble_carry(void)
{
	ble_carrying = true;
	fprintf(stderr, "ble carries\n");
	deck_link_begin(&blink); /* Hello over Bluetooth */
}

int main(int argc, char **argv)
{
	deck_link_config_t cfg;
	memset(&cfg, 0, sizeof cfg);
	cfg.board_id = "AA:BB:CC:00:00:77";
	cfg.type = "board-wifi";
	cfg.firmware = "0.1.0";
	cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE;
	long seconds = 10;
	const char *ble_path = NULL;
	long mtu = 185;
	long intro_ms = 20000;
	for (int i = 1; i < argc; i++) {
		const char *a = argv[i], *v = i + 1 < argc ? argv[i + 1] : NULL;
		if (strcmp(a, "--serial") == 0)
			over_serial = true;
		else if (strcmp(a, "--wifi") == 0)
			over_wifi = true;
		else if (!v)
			usage();
		else if (strcmp(a, "--store") == 0)
			store_dir = v, i++;
		else if (strcmp(a, "--ble") == 0)
			over_ble = true, ble_path = v, i++;
		else if (strcmp(a, "--mtu") == 0)
			mtu = strtol(v, NULL, 10), i++;
		else if (strcmp(a, "--ble-intro-ms") == 0)
			intro_ms = strtol(v, NULL, 10), i++;
		else if (strcmp(a, "--board-id") == 0)
			cfg.board_id = v, i++;
		else if (strcmp(a, "--firmware-board") == 0)
			fw_board = v, i++;
		else if (strcmp(a, "--firmware-out") == 0)
			fw_out = v, i++;
		else if (strcmp(a, "--focus") == 0)
			focus_id = v, i++;
		else if (strcmp(a, "--firmware") == 0)
			cfg.firmware = v, i++;
		else if (strcmp(a, "--seconds") == 0)
			seconds = strtol(v, NULL, 10), i++;
		else
			usage();
	}
	if ((!over_serial && !over_wifi && !over_ble) || (over_serial && over_wifi))
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
	int listen_fd = -1;
	if (over_ble) {
		if (mtu < DECK_BLE_MTU_MIN || mtu > 517)
			usage();
		char bond[2];
		size_t bl = sizeof bond;
		ble_bonded = fkv_get(NULL, "ble_bond", bond, &bl);
		deck_ble_config_t bc = {sock_notify, NULL, ble_ring, sizeof ble_ring};
		deck_ble_init(&ble, &bc);
		deck_link_config_t b = cfg;
		b.write = deck_ble_write;
		b.user = &ble;
		b.on_wifi_config = on_wifi_config;
		b.on_link_config = on_link_config;
		deck_link_init(&blink, &b);
		struct sockaddr_un sa;
		memset(&sa, 0, sizeof sa);
		sa.sun_family = AF_UNIX;
		if (strlen(ble_path) >= sizeof sa.sun_path)
			usage();
		snprintf(sa.sun_path, sizeof sa.sun_path, "%s", ble_path);
		unlink(ble_path);
		listen_fd = socket(AF_UNIX, SOCK_STREAM, 0);
		if (listen_fd < 0 || bind(listen_fd, (struct sockaddr *)&sa, sizeof sa) != 0 || listen(listen_fd, 1) != 0) {
			perror("board_wifi: ble socket");
			return 1;
		}
		char name[DECK_BLE_NAME_SIZE], info[DECK_BLE_INFO_SIZE];
		struct timespec ts;
		clock_gettime(CLOCK_REALTIME, &ts);
		deck_ble_name(cfg.board_id, name);
		deck_ble_info(info, sizeof info, "board-wifi", cfg.board_id, cfg.firmware, true);
		fprintf(stderr, "ble name=%s passkey=%06u info=%s\n", name,
			(unsigned)deck_ble_passkey((uint32_t)ts.tv_nsec ^ (uint32_t)getpid()), info);
	}
	if (over_serial) {
		deck_link_config_t p = cfg;
		p.write = serial_write;
		p.on_wifi_config = on_wifi_config;
		if (over_ble)
			p.on_link_config = on_link_config; /* the preference comes over any link */
		deck_link_init(&plink, &p);
		primary_up = true;
	} else if (over_wifi) {
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
				tls_host = cert.hosts[i];
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
		deck_link_config_t p = cfg;
		p.write = deck_tls_write;
		p.user = &tls;
		if (fw_out)
			p.on_firmware_offer = on_firmware_offer;
		if (over_ble)
			p.on_link_config = on_link_config;
		deck_link_init(&plink, &p);
		primary_up = true;
	}
	if (primary_up)
		deck_link_begin(&plink);

	uint32_t start = now_ms(NULL), ble_since = 0;
	int status = 0;
	while (!write_failed) {
		uint32_t now = now_ms(NULL);
		if (seconds > 0 && (uint32_t)(now - start) >= (uint32_t)seconds * 1000u)
			break;
		uint8_t buf[1024];
		if (over_wifi && primary_up) {
			int n;
			while ((n = deck_tls_read(&tls, buf, sizeof buf)) > 0)
				deck_link_feed(&plink, buf, (size_t)n);
			if (n < 0) {
				fprintf(stderr, "closed error=%d %s\n", -n, deck_tls_strerror((deck_tls_err_t)-n));
				status = -n == DECK_TLS_CERT_REJECTED ? 4 : 0;
				if (!over_ble)
					break;
				fprintf(stderr, "wifi gone\n");
				deck_tls_close(&tls);
				primary_up = false;
			}
		}
		struct pollfd pfd[2];
		int np = 0, pi = -1, bi = -1;
		if (primary_up) {
			pi = np;
			pfd[np++] = (struct pollfd){over_serial ? STDIN_FILENO : tls.fd, POLLIN, 0};
		}
		if (over_ble) {
			bi = np;
			pfd[np++] = (struct pollfd){ble_fd >= 0 ? ble_fd : listen_fd, POLLIN, 0};
		}
		if (poll(pfd, (nfds_t)np, 50) > 0) {
			if (pi >= 0 && over_serial && (pfd[pi].revents & (POLLIN | POLLHUP))) {
				ssize_t n = read(STDIN_FILENO, buf, sizeof buf);
				if (n == 0) {
					if (!over_ble)
						break;
					fprintf(stderr, "serial unplugged\n");
					primary_up = false;
				} else if (n > 0) {
					deck_link_feed(&plink, buf, (size_t)n);
				}
			}
			if (bi >= 0 && (pfd[bi].revents & (POLLIN | POLLHUP))) {
				if (ble_fd < 0) {
					ble_fd = accept(listen_fd, NULL, NULL);
					if (ble_fd >= 0) {
						fprintf(stderr, "ble connected\n");
						deck_ble_open(&ble, (uint16_t)mtu);
						ble_since = now_ms(NULL);
						if (!ble_bonded) /* it paired: bonded from now on */
							{
							(void)fkv_set(NULL, "ble_bond", "1", 1);
							ble_bonded = true;
						}
						if (primary_online()) {
							fprintf(stderr, "ble standby\n");
							send_net_status(&plink); /* the deck hears a secure central */
						} else {
							ble_carry();
						}
					}
				} else {
					/* A write from the deck: at most MTU - 3 bytes, as over the air. */
					size_t want = (size_t)mtu - 3 < sizeof buf ? (size_t)mtu - 3 : sizeof buf;
					ssize_t n = read(ble_fd, buf, want);
					if (n <= 0)
						ble_close(NULL);
					else if (!deck_ble_rx_push(&ble, buf, (size_t)n))
						fprintf(stderr, "board_wifi: ble write refused (%zd bytes)\n", n);
				}
			}
		}
		if (ble_fd >= 0) {
			if (ble_carrying && primary_online()) {
				ble_close("other-link"); /* the other link's deck takes over */
			} else if (!ble_carrying && !primary_online()) {
				ble_carry(); /* the other link went: Bluetooth takes the session */
			} else if (!ble_carrying && (uint32_t)(now_ms(NULL) - ble_since) >= (uint32_t)intro_ms) {
				ble_close("intro"); /* introduced, and not wanted */
			}
		}
		if (primary_up)
			deck_link_poll(&plink);
		if (ble_fd >= 0 && ble_carrying) {
			deck_ble_rx_feed(&ble, &blink, 0);
			deck_link_poll(&blink);
			deck_ble_pump(&ble, 0);
		}
	}
	deck_tls_close(&tls);
	if (listen_fd >= 0) {
		close(listen_fd);
		unlink(ble_path);
	}
	return status;
}
