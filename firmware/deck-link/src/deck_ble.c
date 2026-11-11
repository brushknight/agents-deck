/* Deck Link over Bluetooth LE: see deck_ble.h. */
#include "deck_ble.h"

#include <stdio.h>
#include <string.h>

/* The RX ring's indices cross tasks (and cores): load and store them
 * atomically, acquire/release, where the compiler can. */
#if defined(__GNUC__) || defined(__clang__)
#define LOAD_ACQ(p) __atomic_load_n((p), __ATOMIC_ACQUIRE)
#define STORE_REL(p, v) __atomic_store_n((p), (v), __ATOMIC_RELEASE)
#else
#define LOAD_ACQ(p) (*(volatile size_t *)(p))
#define STORE_REL(p, v) (*(volatile size_t *)(p) = (v))
#endif

static uint16_t chunk_of(uint16_t att_mtu)
{
	if (att_mtu < DECK_BLE_MTU_MIN)
		att_mtu = DECK_BLE_MTU_MIN;
	uint16_t c = (uint16_t)(att_mtu - 3);
	return c > DECK_BLE_CHUNK_MAX ? DECK_BLE_CHUNK_MAX : c;
}

void deck_ble_init(deck_ble_t *b, const deck_ble_config_t *cfg)
{
	memset(b, 0, sizeof *b);
	b->cfg = *cfg;
	b->chunk = chunk_of(DECK_BLE_MTU_MIN);
}

void deck_ble_open(deck_ble_t *b, uint16_t att_mtu)
{
	b->tx_head = b->tx_len = 0;
	/* Drop what an earlier connection left: the consumer owns the tail. */
	STORE_REL(&b->rx_tail, LOAD_ACQ(&b->rx_head));
	b->chunk = chunk_of(att_mtu);
	b->open = true;
}

void deck_ble_set_mtu(deck_ble_t *b, uint16_t att_mtu) { b->chunk = chunk_of(att_mtu); }

void deck_ble_close(deck_ble_t *b)
{
	b->open = false;
	b->tx_head = b->tx_len = 0;
}

bool deck_ble_is_open(const deck_ble_t *b) { return b->open; }

/* ---- TX ----------------------------------------------------------------------- */

size_t deck_ble_write(const uint8_t *data, size_t len, void *ble)
{
	deck_ble_t *b = (deck_ble_t *)ble;
	if (!b->open || len > sizeof b->tx - b->tx_len) {
		b->stats.tx_dropped++;
		return 0;
	}
	size_t tail = (b->tx_head + b->tx_len) % sizeof b->tx;
	size_t first = sizeof b->tx - tail;
	if (first > len)
		first = len;
	memcpy(b->tx + tail, data, first);
	memcpy(b->tx, data + first, len - first);
	b->tx_len += len;
	return len;
}

size_t deck_ble_pump(deck_ble_t *b, unsigned max_notifies)
{
	size_t sent = 0;
	unsigned n = 0;
	while (b->open && b->tx_len > 0 && (max_notifies == 0 || n < max_notifies)) {
		size_t len = b->tx_len < b->chunk ? b->tx_len : b->chunk;
		const uint8_t *p;
		if (b->tx_head + len <= sizeof b->tx) {
			p = b->tx + b->tx_head;
		} else { /* across the ring's end: one notification all the same */
			size_t first = sizeof b->tx - b->tx_head;
			memcpy(b->scratch, b->tx + b->tx_head, first);
			memcpy(b->scratch + first, b->tx, len - first);
			p = b->scratch;
		}
		if (!b->cfg.notify || !b->cfg.notify(p, len, b->cfg.user)) {
			b->stats.tx_busy++;
			break;
		}
		b->tx_head = (b->tx_head + len) % sizeof b->tx;
		b->tx_len -= len;
		b->stats.tx_bytes += (uint32_t)len;
		b->stats.tx_notifies++;
		sent += len;
		n++;
	}
	return sent;
}

size_t deck_ble_pending(const deck_ble_t *b) { return b->tx_len; }

/* ---- RX ----------------------------------------------------------------------- */

/* One byte of the ring stays free, so head == tail means empty. */
static size_t rx_used(size_t head, size_t tail, size_t size) { return head >= tail ? head - tail : size - tail + head; }

bool deck_ble_rx_push(deck_ble_t *b, const uint8_t *data, size_t len)
{
	size_t size = b->cfg.rx_size;
	if (!b->open || !b->cfg.rx_buf || size < 2) {
		b->stats.rx_refused++;
		return false;
	}
	size_t head = b->rx_head; /* only this side writes it */
	size_t tail = LOAD_ACQ(&b->rx_tail);
	if (len > size - 1 - rx_used(head, tail, size)) {
		b->stats.rx_refused++;
		return false;
	}
	size_t first = size - head;
	if (first > len)
		first = len;
	memcpy(b->cfg.rx_buf + head, data, first);
	memcpy(b->cfg.rx_buf, data + first, len - first);
	STORE_REL(&b->rx_head, (head + len) % size);
	b->stats.rx_bytes += (uint32_t)len;
	return true;
}

size_t deck_ble_rx_pop(deck_ble_t *b, uint8_t *out, size_t cap)
{
	size_t size = b->cfg.rx_size;
	if (!b->cfg.rx_buf || size < 2)
		return 0;
	size_t tail = b->rx_tail; /* only this side writes it */
	size_t head = LOAD_ACQ(&b->rx_head);
	size_t n = rx_used(head, tail, size);
	if (n > cap)
		n = cap;
	size_t first = size - tail;
	if (first > n)
		first = n;
	memcpy(out, b->cfg.rx_buf + tail, first);
	memcpy(out + first, b->cfg.rx_buf, n - first);
	STORE_REL(&b->rx_tail, (tail + n) % size);
	return n;
}

size_t deck_ble_rx_feed(deck_ble_t *b, deck_link_t *link, size_t max)
{
	uint8_t buf[256];
	size_t done = 0;
	while (max == 0 || done < max) {
		size_t want = sizeof buf;
		if (max && max - done < want)
			want = max - done;
		size_t n = deck_ble_rx_pop(b, buf, want);
		if (n == 0)
			break;
		deck_link_feed(link, buf, n);
		done += n;
	}
	return done;
}

size_t deck_ble_rx_available(const deck_ble_t *b)
{
	if (!b->cfg.rx_buf || b->cfg.rx_size < 2)
		return 0;
	return rx_used(LOAD_ACQ(&b->rx_head), LOAD_ACQ(&b->rx_tail), b->cfg.rx_size);
}

const deck_ble_stats_t *deck_ble_stats(const deck_ble_t *b) { return &b->stats; }

/* ---- names -------------------------------------------------------------------- */

static int hexval(char c)
{
	if (c >= '0' && c <= '9')
		return c - '0';
	if (c >= 'a' && c <= 'f')
		return c - 'a' + 10;
	if (c >= 'A' && c <= 'F')
		return c - 'A' + 10;
	return -1;
}

void deck_ble_name(const char *board_id, char out[DECK_BLE_NAME_SIZE])
{
	static const char digits[] = "0123456789abcdef";
	char six[7] = "000000";
	int k = 6;
	size_t n = board_id ? strlen(board_id) : 0;
	while (n > 0 && k > 0) { /* from the end, hex digits only */
		int v = hexval(board_id[--n]);
		if (v >= 0)
			six[--k] = digits[v];
	}
	snprintf(out, DECK_BLE_NAME_SIZE, "deck-%s", six);
}

size_t deck_ble_info(char *out, size_t cap, const char *board, const char *board_id, const char *firmware,
		     bool paired)
{
	if (!out || cap == 0)
		return 0;
	int n = snprintf(out, cap, "board=%s;id=%s;fw=%s;paired=%d", board ? board : "", board_id ? board_id : "",
			 firmware ? firmware : "", paired ? 1 : 0);
	if (n < 0) {
		out[0] = '\0';
		return 0;
	}
	return (size_t)n < cap ? (size_t)n : cap - 1;
}

uint32_t deck_ble_passkey(uint32_t random) { return random % 1000000u; }
