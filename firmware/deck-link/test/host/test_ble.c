/* Host tests for Deck Link over Bluetooth LE (deck_ble.h) with a fake GATT
 * link: names and Info, chunking to the MTU, back-pressure on TX, the RX
 * ring (also across two threads), and a whole session over the fake link:
 * Hello, approval, pairing (CertRequest / CertIssued), a 32-agent snapshot
 * through a small ring, Wi-Fi credentials and the uplink preference. */
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cobs.h"
#include "crc16.h"
#include "deck_ble.h"
#include "deck_link.h"
#include "proto/pb_encode.h"

static int failures, checks;

#define CHECK(cond)                                                                      \
	do {                                                                             \
		checks++;                                                                \
		if (!(cond)) {                                                           \
			failures++;                                                      \
			fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #cond); \
		}                                                                        \
	} while (0)

/* ---- a fake stack: notifications with credits (the stack's buffers) --------- */

static uint8_t sent[65536];
static size_t sent_len;
static size_t largest_chunk, chunks;
static int credits = -1; /* -1: unlimited */
static deck_link_t *deck_side; /* when set, notifications go straight to the deck's decoder */

static bool fake_notify(const uint8_t *d, size_t n, void *user)
{
	(void)user;
	if (credits == 0)
		return false;
	if (credits > 0)
		credits--;
	if (n > largest_chunk)
		largest_chunk = n;
	chunks++;
	if (deck_side)
		deck_link_feed(deck_side, d, n);
	else if (sent_len + n <= sizeof sent) {
		memcpy(sent + sent_len, d, n);
		sent_len += n;
	}
	return true;
}

static void reset_fake(void)
{
	sent_len = largest_chunk = chunks = 0;
	credits = -1;
	deck_side = NULL;
}

static uint8_t ring[8192];
static deck_ble_t ble;

static void ble_setup(size_t rx_size)
{
	deck_ble_config_t cfg = {fake_notify, NULL, ring, rx_size};
	deck_ble_init(&ble, &cfg);
	reset_fake();
}

/* ---- names ----------------------------------------------------------------- */

static void test_names(void)
{
	char name[DECK_BLE_NAME_SIZE];
	deck_ble_name("AA:BB:CC:1A:2B:3C", name);
	CHECK(strcmp(name, "deck-1a2b3c") == 0);
	deck_ble_name("aabbccddeeff", name);
	CHECK(strcmp(name, "deck-ddeeff") == 0);
	deck_ble_name("sim-0001", name); /* 's', 'i', 'm' and '-' aren't hex: "0001" with zeros in front */
	CHECK(strcmp(name, "deck-000001") == 0);
	deck_ble_name("", name);
	CHECK(strcmp(name, "deck-000000") == 0);
	deck_ble_name(NULL, name);
	CHECK(strcmp(name, "deck-000000") == 0);

	char info[DECK_BLE_INFO_SIZE];
	size_t n = deck_ble_info(info, sizeof info, "lilygo-t4-s3", "AA:BB:CC:1A:2B:3C", "0.3.0", false);
	CHECK(strcmp(info, "board=lilygo-t4-s3;id=AA:BB:CC:1A:2B:3C;fw=0.3.0;paired=0") == 0);
	CHECK(n == strlen(info));
	n = deck_ble_info(info, 12, "lilygo-t4-s3", "x", "1", true);
	CHECK(n == 11 && strlen(info) == 11);
	/* The longest fields fit. */
	char board[32], id[40], fw[24];
	memset(board, 'b', 31), board[31] = 0;
	memset(id, 'i', 39), id[39] = 0;
	memset(fw, 'f', 23), fw[23] = 0;
	n = deck_ble_info(info, sizeof info, board, id, fw, true);
	CHECK(n < sizeof info - 1 && strstr(info, ";paired=1") != NULL);

	CHECK(deck_ble_passkey(0) == 0);
	CHECK(deck_ble_passkey(999999) == 999999);
	CHECK(deck_ble_passkey(1000000) == 0);
	CHECK(deck_ble_passkey(0xFFFFFFFFu) < 1000000);

	/* The UUIDs, least significant byte first. */
	const uint8_t svc[16] = {DECK_BLE_UUID128(0x01)};
	const uint8_t want[16] = {0xe0, 0x0d, 0x5c, 0x7d, 0xe1, 0xa9, 0x6e, 0x9a,
				  0x1b, 0x4f, 0x2c, 0x5d, 0x01, 0x00, 0x3a, 0x7e};
	CHECK(memcmp(svc, want, 16) == 0);
	/* Read back most significant first, it is the string. */
	char s[40];
	const uint8_t tx[16] = {DECK_BLE_UUID128(0x03)};
	snprintf(s, sizeof s, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x", tx[15], tx[14],
		 tx[13], tx[12], tx[11], tx[10], tx[9], tx[8], tx[7], tx[6], tx[5], tx[4], tx[3], tx[2], tx[1], tx[0]);
	CHECK(strcmp(s, DECK_BLE_TX_UUID) == 0);
}

/* ---- TX: chunking and back-pressure ------------------------------------------ */

static void pattern(uint8_t *p, size_t n, unsigned seed)
{
	for (size_t i = 0; i < n; i++)
		p[i] = (uint8_t)((i * 31u + seed * 7u) % 251u + 1u);
}

static void test_tx_chunks(void)
{
	uint8_t frame[700];
	pattern(frame, sizeof frame, 1);
	const uint16_t mtus[] = {23, 100, 185, 247, 517};
	const size_t want_chunk[] = {20, 97, 182, 244, 244}; /* capped at DECK_BLE_CHUNK_MAX */
	for (size_t m = 0; m < sizeof mtus / sizeof mtus[0]; m++) {
		ble_setup(sizeof ring);
		deck_ble_open(&ble, mtus[m]);
		CHECK(deck_ble_write(frame, sizeof frame, &ble) == sizeof frame);
		CHECK(deck_ble_pending(&ble) == sizeof frame);
		CHECK(deck_ble_pump(&ble, 0) == sizeof frame);
		CHECK(sent_len == sizeof frame && memcmp(sent, frame, sizeof frame) == 0);
		CHECK(largest_chunk == want_chunk[m]);
		CHECK(chunks == (sizeof frame + want_chunk[m] - 1) / want_chunk[m]);
		CHECK(deck_ble_pending(&ble) == 0);
	}

	/* The MTU grows after the stream opened (the central asked for more). */
	ble_setup(sizeof ring);
	deck_ble_open(&ble, 23);
	deck_ble_set_mtu(&ble, 247);
	CHECK(deck_ble_write(frame, sizeof frame, &ble) == sizeof frame);
	deck_ble_pump(&ble, 0);
	CHECK(largest_chunk == 244 && chunks == 3);

	/* max_notifies bounds one pump. */
	ble_setup(sizeof ring);
	deck_ble_open(&ble, 23);
	deck_ble_write(frame, 100, &ble);
	CHECK(deck_ble_pump(&ble, 2) == 40 && deck_ble_pending(&ble) == 60);
	CHECK(deck_ble_pump(&ble, 0) == 60 && memcmp(sent, frame, 100) == 0);
}

static void test_tx_backpressure(void)
{
	uint8_t frame[600];
	pattern(frame, sizeof frame, 2);

	/* The stack takes two notifications, then has no room: the rest waits. */
	ble_setup(sizeof ring);
	deck_ble_open(&ble, 247);
	deck_ble_write(frame, sizeof frame, &ble);
	credits = 2;
	CHECK(deck_ble_pump(&ble, 0) == 488);
	CHECK(deck_ble_stats(&ble)->tx_busy == 1 && deck_ble_pending(&ble) == 112);
	CHECK(deck_ble_pump(&ble, 0) == 0 && deck_ble_stats(&ble)->tx_busy == 2);
	credits = -1;
	CHECK(deck_ble_pump(&ble, 0) == 112);
	CHECK(sent_len == sizeof frame && memcmp(sent, frame, sizeof frame) == 0);

	/* A full queue refuses a whole frame (never part of one). */
	ble_setup(sizeof ring);
	deck_ble_open(&ble, 247);
	credits = 0;
	size_t queued = 0;
	while (deck_ble_write(frame, sizeof frame, &ble) == sizeof frame)
		queued += sizeof frame;
	CHECK(queued == (DECK_BLE_TX_SIZE / sizeof frame) * sizeof frame);
	CHECK(deck_ble_pending(&ble) == queued && deck_ble_stats(&ble)->tx_dropped == 1);
	credits = -1;
	deck_ble_pump(&ble, 0);
	CHECK(sent_len == queued);
	for (size_t off = 0; off < queued; off += sizeof frame)
		CHECK(memcmp(sent + off, frame, sizeof frame) == 0);

	/* Across the ring's end, many times, with odd sizes: the bytes stay in order. */
	ble_setup(sizeof ring);
	deck_ble_open(&ble, 61);
	static uint8_t want[60000];
	size_t wl = 0;
	for (unsigned i = 0; i < 200; i++) {
		size_t n = 37 + (i * 53) % 400;
		uint8_t buf[512];
		pattern(buf, n, i);
		if (deck_ble_write(buf, n, &ble) == n) {
			memcpy(want + wl, buf, n);
			wl += n;
		}
		credits = (int)(i % 4); /* the stack frees a few buffers at a time */
		deck_ble_pump(&ble, 0);
	}
	credits = -1;
	deck_ble_pump(&ble, 0);
	CHECK(sent_len == wl && memcmp(sent, want, wl) == 0);
	CHECK(largest_chunk == 58);

	/* Closed: nothing is queued or sent; opening again starts empty. */
	ble_setup(sizeof ring);
	CHECK(deck_ble_write(frame, 10, &ble) == 0);
	deck_ble_open(&ble, 247);
	deck_ble_write(frame, 10, &ble);
	deck_ble_close(&ble);
	CHECK(deck_ble_pump(&ble, 0) == 0 && sent_len == 0);
	deck_ble_open(&ble, 247);
	CHECK(deck_ble_pending(&ble) == 0);
}

/* ---- RX ring --------------------------------------------------------------------- */

static void test_rx_ring(void)
{
	uint8_t a[300], out[1024];
	pattern(a, sizeof a, 3);
	ble_setup(1000);
	CHECK(!deck_ble_rx_push(&ble, a, 10)); /* closed */
	deck_ble_open(&ble, 247);
	CHECK(deck_ble_rx_push(&ble, a, 300));
	CHECK(deck_ble_rx_push(&ble, a, 300));
	CHECK(deck_ble_rx_push(&ble, a, 300));
	CHECK(deck_ble_rx_available(&ble) == 900);
	CHECK(!deck_ble_rx_push(&ble, a, 100)); /* 99 free: refused whole */
	CHECK(deck_ble_rx_push(&ble, a, 99));
	CHECK(deck_ble_stats(&ble)->rx_refused == 2);
	CHECK(deck_ble_rx_pop(&ble, out, 250) == 250 && memcmp(out, a, 250) == 0);
	CHECK(deck_ble_rx_push(&ble, a, 250)); /* across the end */
	size_t n = deck_ble_rx_pop(&ble, out, sizeof out);
	CHECK(n == 50 + 300 + 300 + 99 + 250);
	CHECK(memcmp(out, a + 250, 50) == 0 && memcmp(out + 50, a, 300) == 0 && memcmp(out + 650, a, 99) == 0 &&
	      memcmp(out + 749, a, 250) == 0);
	CHECK(deck_ble_rx_available(&ble) == 0);

	/* A new connection drops what the last one left. */
	CHECK(deck_ble_rx_push(&ble, a, 30));
	deck_ble_close(&ble);
	deck_ble_open(&ble, 247);
	CHECK(deck_ble_rx_available(&ble) == 0);
}

/* One task writes (the stack), another reads (the loop): 4 MB, byte for byte. */
#define STRESS_BYTES (4u << 20)
static volatile int stress_bad;

static void *producer(void *arg)
{
	(void)arg;
	uint8_t buf[512];
	uint32_t sent_n = 0, r = 1;
	while (sent_n < STRESS_BYTES) {
		r = r * 1103515245u + 12345u;
		size_t n = 1 + (r >> 16) % 244;
		if (n > STRESS_BYTES - sent_n)
			n = STRESS_BYTES - sent_n;
		for (size_t i = 0; i < n; i++)
			buf[i] = (uint8_t)((sent_n + i) * 7u);
		if (deck_ble_rx_push(&ble, buf, n))
			sent_n += (uint32_t)n;
	}
	return NULL;
}

static void test_rx_threads(void)
{
	ble_setup(4096);
	deck_ble_open(&ble, 247);
	pthread_t t;
	pthread_create(&t, NULL, producer, NULL);
	uint8_t buf[700];
	uint32_t got = 0, r = 7;
	while (got < STRESS_BYTES) {
		r = r * 1103515245u + 12345u;
		size_t n = deck_ble_rx_pop(&ble, buf, 1 + (r >> 16) % sizeof buf);
		for (size_t i = 0; i < n; i++)
			if (buf[i] != (uint8_t)((got + i) * 7u))
				stress_bad++;
		got += (uint32_t)n;
	}
	pthread_join(t, NULL);
	CHECK(stress_bad == 0 && got == STRESS_BYTES);
}

/* ---- a whole session over the fake link ---------------------------------------- */

/* The board: a deck_link_t whose write is the BLE stream. */
static deck_link_t board;
/* The deck's side: a second core used only to decode what the board sends
 * (every frame reaches on_frame). */
static deck_link_t deck_dec;
static agentsdeck_link_v1_Frame from_board[64];
static int from_board_n;

static void deck_saw(deck_link_t *c, const agentsdeck_link_v1_Frame *f)
{
	(void)c;
	if (from_board_n < 64)
		from_board[from_board_n++] = *f;
}

static const agentsdeck_link_v1_Frame *last_from_board(pb_size_t which)
{
	for (int i = from_board_n - 1; i >= 0; i--)
		if (from_board[i].which_body == which)
			return &from_board[i];
	return NULL;
}

static size_t dec_write(const uint8_t *d, size_t n, void *u) { return (void)d, (void)u, n; }

static uint32_t clock_ms;
static uint32_t fake_millis(void *u) { return (void)u, clock_ms; }

/* The board's events. */
static int b_welcome, b_approved, b_agents, b_snap_end, b_issued;
static uint32_t b_issued_seq;
static size_t b_cert_len;
static uint8_t b_cert[800];
static char b_ssid[40];
static agentsdeck_link_v1_Uplink b_uplink = (agentsdeck_link_v1_Uplink)99;
static uint8_t csr[560];

static void b_on_welcome(deck_link_t *c, const char *n, agentsdeck_link_v1_Scope s, bool approved)
{
	(void)n, (void)s;
	b_welcome++;
	b_approved = approved;
	/* An approved board without a certificate asks for one, over Bluetooth as over USB. */
	if (approved && deck_link_cert_serial(c)[0] == '\0')
		CHECK(deck_cert_request(c, csr, sizeof csr) != 0);
}
static void b_on_agent(deck_link_t *c, const agentsdeck_link_v1_Agent *a) { (void)c, (void)a, b_agents++; }
static void b_on_snap_end(deck_link_t *c) { (void)c, b_snap_end++; }
static void b_on_issued(deck_link_t *c, uint32_t seq, const agentsdeck_link_v1_CertIssued *ci)
{
	(void)c;
	b_issued++;
	b_issued_seq = seq;
	b_cert_len = ci->cert.size;
	memcpy(b_cert, ci->cert.bytes, ci->cert.size);
}
static uint32_t b_on_wifi(deck_link_t *c, const char *ssid, const char *pw, bool forget)
{
	(void)c, (void)pw, (void)forget;
	snprintf(b_ssid, sizeof b_ssid, "%s", ssid);
	return 204;
}
static uint32_t b_on_link_config(deck_link_t *c, agentsdeck_link_v1_Uplink u)
{
	(void)c;
	b_uplink = u;
	return 204;
}

/* One turn of the board's loop: drain the ring, run the timers, send. */
static void board_loop(void)
{
	deck_ble_rx_feed(&ble, &board, 0);
	deck_link_poll(&board);
	deck_ble_pump(&ble, 0);
}

static size_t deck_bytes, deck_writes, deck_retries;

/* The deck sends a frame: encoded, cut to the MTU, written one ATT write at a
 * time; a write the ring refuses is sent again after the board's loop ran
 * (what "insufficient resources" makes a central do). */
static void deck_send(const agentsdeck_link_v1_Frame *f, uint16_t mtu)
{
	static uint8_t raw[8192], wire[8192];
	raw[0] = 0xDE, raw[1] = 0xC1, raw[2] = 1;
	pb_ostream_t s = pb_ostream_from_buffer(raw + 3, sizeof raw - 5);
	CHECK(pb_encode(&s, agentsdeck_link_v1_Frame_fields, f));
	size_t n = 3 + s.bytes_written;
	uint16_t crc = deck_crc16(raw, n);
	raw[n++] = (uint8_t)(crc >> 8);
	raw[n++] = (uint8_t)crc;
	size_t m = deck_cobs_encode(raw, n, wire);
	wire[m++] = 0;
	size_t per = (size_t)mtu - 3;
	for (size_t off = 0; off < m;) {
		size_t k = m - off < per ? m - off : per;
		int tries = 0;
		while (!deck_ble_rx_push(&ble, wire + off, k)) {
			deck_retries++;
			board_loop();
			CHECK(++tries < 100);
			if (tries >= 100)
				return;
		}
		off += k;
		deck_writes++;
	}
	deck_bytes += m;
}

static agentsdeck_link_v1_Frame mk(uint32_t seq, pb_size_t which)
{
	agentsdeck_link_v1_Frame f;
	memset(&f, 0, sizeof f);
	f.seq = seq;
	f.which_body = which;
	return f;
}

static void fill(char *dst, size_t size, const char *word, unsigned i)
{
	size_t n = 0;
	while (n + 1 < size) {
		dst[n] = word[(n + i) % strlen(word)];
		n++;
	}
	dst[n] = 0;
}

/* An agent with every TILE and CARD field filled to its limit: the worst case. */
static agentsdeck_link_v1_Frame mk_full_agent(unsigned i)
{
	agentsdeck_link_v1_Frame f = mk(0, agentsdeck_link_v1_Frame_agent_changed_tag);
	agentsdeck_link_v1_Agent *a = &f.body.agent_changed.agent;
	f.body.agent_changed.has_agent = true;
	snprintf(a->id, sizeof a->id, "a%05u", i);
	a->slot = i;
	fill(a->title, sizeof a->title, "fix the login flow ", i);
	fill(a->ai_title, sizeof a->ai_title, "Refactoring the session cache ", i);
	a->status = i % 4 == 0 ? agentsdeck_link_v1_Status_STATUS_WAITING : agentsdeck_link_v1_Status_STATUS_RUNNING;
	a->status_since_ms = 1780000000000 + i;
	fill(a->cwd, sizeof a->cwd, "/work/projects/example/", i);
	fill(a->folder, sizeof a->folder, "example-service", i);
	fill(a->branch, sizeof a->branch, "feature/long-branch-name", i);
	fill(a->model, sizeof a->model, "model-name", i);
	fill(a->model_label, sizeof a->model_label, "Label", i);
	a->has_activity = true;
	fill(a->activity.tool, sizeof a->activity.tool, "Edit", i);
	fill(a->activity.detail, sizeof a->activity.detail, "src/session/cache.go ", i);
	a->has_context = true;
	a->context.used = 120000;
	a->context.window = 200000;
	a->has_hat = true;
	snprintf(a->hat.shape, sizeof a->hat.shape, "crown");
	snprintf(a->hat.color, sizeof a->hat.color, "orange");
	if (a->status == agentsdeck_link_v1_Status_STATUS_WAITING) {
		a->has_waiting = true;
		snprintf(a->waiting.id, sizeof a->waiting.id, "p%u", i);
		fill(a->waiting.title, sizeof a->waiting.title, "Allow this command? ", i);
		a->waiting.options_count = 4;
		for (int k = 0; k < 4; k++) {
			snprintf(a->waiting.options[k].key, sizeof a->waiting.options[k].key, "%d", k + 1);
			fill(a->waiting.options[k].label, 48, "Yes, and don't ask again ", i + (unsigned)k);
		}
		a->waiting.options[0].primary = true;
	}
	return f;
}

static void test_session(void)
{
	const uint16_t mtu = 185; /* what a Mac often agrees to */
	ble_setup(4096);         /* a small ring: the snapshot needs back-pressure */
	pattern(csr, sizeof csr, 9);

	deck_link_config_t dc;
	memset(&dc, 0, sizeof dc);
	dc.write = dec_write;
	dc.millis = fake_millis;
	dc.on_frame = deck_saw;
	deck_link_init(&deck_dec, &dc);
	deck_side = &deck_dec;
	from_board_n = 0;

	deck_link_config_t bc;
	memset(&bc, 0, sizeof bc);
	bc.write = deck_ble_write;
	bc.user = &ble;
	bc.millis = fake_millis;
	bc.board_id = "AA:BB:CC:1A:2B:3C";
	bc.type = "ble-board";
	bc.firmware = "0.3.0";
	bc.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_CARD |
		    agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT;
	bc.on_welcome = b_on_welcome;
	bc.on_agent = b_on_agent;
	bc.on_snapshot_end = b_on_snap_end;
	bc.on_cert_issued = b_on_issued;
	bc.on_wifi_config = b_on_wifi;
	bc.on_link_config = b_on_link_config;
	deck_link_init(&board, &bc);

	/* The link is secure: the stream opens and the board says Hello. */
	deck_ble_open(&ble, mtu);
	deck_link_begin(&board);
	board_loop();
	const agentsdeck_link_v1_Frame *h = last_from_board(agentsdeck_link_v1_Frame_hello_tag);
	CHECK(h && strcmp(h->body.hello.board_id, "AA:BB:CC:1A:2B:3C") == 0);
	CHECK(largest_chunk <= (size_t)mtu - 3);

	/* Not approved yet (a Bluetooth board never is on first sight). */
	agentsdeck_link_v1_Frame w = mk(0, agentsdeck_link_v1_Frame_welcome_tag);
	w.body.welcome.protocol = 1;
	snprintf(w.body.welcome.device_name, sizeof w.body.welcome.device_name, "ble-board-1a2b3c");
	deck_send(&w, mtu);
	board_loop();
	CHECK(b_welcome == 1 && !b_approved && last_from_board(agentsdeck_link_v1_Frame_subscribe_tag) == NULL);

	/* Approved: it subscribes and asks for a certificate (pairing). */
	w.body.welcome.approved = true;
	w.body.welcome.scope = agentsdeck_link_v1_Scope_SCOPE_ACT;
	deck_send(&w, mtu);
	board_loop();
	CHECK(b_welcome == 2 && b_approved);
	CHECK(last_from_board(agentsdeck_link_v1_Frame_subscribe_tag) != NULL);
	const agentsdeck_link_v1_Frame *cr = last_from_board(agentsdeck_link_v1_Frame_cert_request_tag);
	CHECK(cr && cr->body.cert_request.csr.size == sizeof csr &&
	      memcmp(cr->body.cert_request.csr.bytes, csr, sizeof csr) == 0);
	CHECK(chunks > 3); /* the CSR took several notifications */

	/* The certificate comes back in many writes. */
	agentsdeck_link_v1_Frame ci = mk(cr ? cr->seq : 0, agentsdeck_link_v1_Frame_cert_issued_tag);
	ci.body.cert_issued.cert.size = 780;
	pattern(ci.body.cert_issued.cert.bytes, 780, 5);
	ci.body.cert_issued.ca.size = 560;
	pattern(ci.body.cert_issued.ca.bytes, 560, 6);
	ci.body.cert_issued.hosts_count = 2;
	snprintf(ci.body.cert_issued.hosts[0], 64, "deck-host.local");
	snprintf(ci.body.cert_issued.hosts[1], 64, "192.0.2.10");
	ci.body.cert_issued.port = 7341;
	snprintf(ci.body.cert_issued.cert_serial, 40, "0a1b2c");
	deck_send(&ci, mtu);
	board_loop();
	CHECK(b_issued == 1 && cr && b_issued_seq == cr->seq && b_cert_len == 780 &&
	      memcmp(b_cert, ci.body.cert_issued.cert.bytes, 780) == 0);

	/* A 32-agent snapshot, every field full, through a 4 KB ring. */
	deck_bytes = deck_writes = deck_retries = 0;
	agentsdeck_link_v1_Frame sb = mk(0, agentsdeck_link_v1_Frame_snapshot_begin_tag);
	sb.body.snapshot_begin.agent_count = 32;
	deck_send(&sb, mtu);
	for (unsigned i = 0; i < 32; i++) {
		agentsdeck_link_v1_Frame a = mk_full_agent(i);
		deck_send(&a, mtu);
	}
	agentsdeck_link_v1_Frame se = mk(0, agentsdeck_link_v1_Frame_snapshot_end_tag);
	deck_send(&se, mtu);
	board_loop();
	CHECK(b_agents == 32 && b_snap_end == 1);
	CHECK(deck_retries > 0); /* the ring was full at times, and nothing was lost */
	CHECK(deck_link_stats(&board)->rx_bad == 0 && deck_link_stats(&board)->rx_overflow == 0);
	printf("ble: a full 32-agent snapshot is %zu bytes in %zu writes at ATT MTU %u (%zu waits for room)\n",
	       deck_bytes, deck_writes, (unsigned)mtu, deck_retries);

	/* Wi-Fi credentials and the uplink preference, answered with a Result. */
	agentsdeck_link_v1_Frame wc = mk(41, agentsdeck_link_v1_Frame_wifi_config_tag);
	snprintf(wc.body.wifi_config.ssid, sizeof wc.body.wifi_config.ssid, "lab-net");
	snprintf(wc.body.wifi_config.password, sizeof wc.body.wifi_config.password, "not-a-real-password");
	deck_send(&wc, mtu);
	board_loop();
	const agentsdeck_link_v1_Frame *r = last_from_board(agentsdeck_link_v1_Frame_result_tag);
	CHECK(strcmp(b_ssid, "lab-net") == 0 && r && r->seq == 41 && r->body.result.code == 204);

	agentsdeck_link_v1_Frame lc = mk(42, agentsdeck_link_v1_Frame_link_config_tag);
	lc.body.link_config.uplink = agentsdeck_link_v1_Uplink_UPLINK_BLE;
	deck_send(&lc, mtu);
	board_loop();
	r = last_from_board(agentsdeck_link_v1_Frame_result_tag);
	CHECK(b_uplink == agentsdeck_link_v1_Uplink_UPLINK_BLE && r && r->seq == 42 && r->body.result.code == 204);

	/* Firmware without Bluetooth: 501. */
	board.cfg.on_link_config = NULL;
	lc.seq = 43;
	deck_send(&lc, mtu);
	board_loop();
	r = last_from_board(agentsdeck_link_v1_Frame_result_tag);
	CHECK(r && r->seq == 43 && r->body.result.code == 501);

	/* The link drops: the board's frames go nowhere (and say so), until it opens again. */
	deck_ble_close(&ble);
	uint32_t before = deck_link_stats(&board)->tx_errors;
	CHECK(deck_ping(&board) == 0 && deck_link_stats(&board)->tx_errors == before + 1);
	deck_ble_open(&ble, mtu);
	CHECK(deck_ping(&board) != 0);
	board_loop();
	CHECK(last_from_board(agentsdeck_link_v1_Frame_ping_tag) != NULL);
	CHECK(deck_ble_stats(&ble)->rx_refused > 0);
	deck_side = NULL;
}

int main(void)
{
	test_names();
	test_tx_chunks();
	test_tx_backpressure();
	test_rx_ring();
	test_rx_threads();
	test_session();
	if (failures) {
		fprintf(stderr, "%d of %d checks failed\n", failures, checks);
		return 1;
	}
	printf("test_ble: all %d checks passed\n", checks);
	return 0;
}
