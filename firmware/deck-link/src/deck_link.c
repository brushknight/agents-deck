/* Deck Link core: see deck_link.h and docs/link/protocol.md. */
#include "deck_link.h"

#include <string.h>

#include "crc16.h"
#include "proto/pb_decode.h"
#include "proto/pb_encode.h"

#if DECK_LINK_MAX_FRAME < 512
#error "DECK_LINK_MAX_FRAME must be at least 512"
#endif

static const uint8_t magic[3] = {DECK_LINK_MAGIC0, DECK_LINK_MAGIC1, DECK_LINK_WIRE_VERSION};

/* ---- helpers --------------------------------------------------------------- */

/* Copies src into dst[size] (NUL included), cutting on a UTF-8 boundary. */
static void copy_str(char *dst, size_t size, const char *src)
{
	size_t n = 0;
	if (src) {
		n = strlen(src);
		if (n > size - 1) {
			n = size - 1;
			while (n > 0 && ((uint8_t)src[n] & 0xC0) == 0x80)
				n--; /* don't split a multi-byte character */
		}
		memcpy(dst, src, n);
	}
	dst[n] = '\0';
}

static uint32_t now_ms(deck_link_t *ctx)
{
	return ctx->cfg.millis ? ctx->cfg.millis(ctx->cfg.user) : 0;
}

static uint32_t next_seq(deck_link_t *ctx)
{
	if (++ctx->seq == 0)
		ctx->seq = 1;
	return ctx->seq;
}

/* ---- encoding -------------------------------------------------------------- */

size_t deck_link_encode(uint32_t seq, pb_size_t tag, const pb_msgdesc_t *fields, const void *msg,
			uint8_t *out, size_t out_size)
{
	/* Build the raw frame far enough into out that COBS can encode it in
	 * place towards the front (see deck_cobs_encode). */
	size_t off = out_size / 254 + 2;
	if (out_size < off + 1 + 5)
		return 0;
	uint8_t *raw = out + off;
	size_t raw_cap = out_size - off - 1; /* keep one byte for the 0x00 */

	memcpy(raw, magic, 3);
	pb_ostream_t s = pb_ostream_from_buffer(raw + 3, raw_cap - 3 - 2);
	if (seq != 0) {
		if (!pb_encode_tag(&s, PB_WT_VARINT, agentsdeck_link_v1_Frame_seq_tag) ||
		    !pb_encode_varint(&s, seq))
			return 0;
	}
	if (!pb_encode_tag(&s, PB_WT_STRING, tag) || !pb_encode_submessage(&s, fields, msg))
		return 0;
	size_t raw_len = 3 + s.bytes_written;
	uint16_t crc = deck_crc16(raw, raw_len);
	raw[raw_len++] = (uint8_t)(crc >> 8);
	raw[raw_len++] = (uint8_t)crc;

	size_t n = deck_cobs_encode(raw, raw_len, out);
	out[n++] = 0;
	return n;
}

static uint32_t send_frame(deck_link_t *ctx, uint32_t seq, pb_size_t tag, const pb_msgdesc_t *fields,
			   const void *msg)
{
	size_t n = deck_link_encode(seq, tag, fields, msg, ctx->tx, sizeof ctx->tx);
	if (n == 0 || !ctx->cfg.write || ctx->cfg.write(ctx->tx, n, ctx->cfg.user) != n) {
		ctx->stats.tx_errors++;
		return 0;
	}
	ctx->stats.tx_frames++;
	return seq;
}

static void send_hello(deck_link_t *ctx)
{
	const deck_link_config_t *c = &ctx->cfg;
	agentsdeck_link_v1_Hello h;
	memset(&h, 0, sizeof h);
	h.protocol = DECK_LINK_PROTOCOL;
	copy_str(h.board_id, sizeof h.board_id, c->board_id);
	copy_str(h.type, sizeof h.type, c->type);
	copy_str(h.firmware, sizeof h.firmware, c->firmware);
	if (c->display_width && c->display_height) {
		h.has_display = true;
		h.display.width = c->display_width;
		h.display.height = c->display_height;
		h.display.color = c->display_color;
	}
	for (uint8_t i = 0; i < c->input_count && i < 4; i++)
		h.inputs[h.inputs_count++] = c->inputs[i];
	uint32_t mf = c->max_frame ? c->max_frame : DECK_LINK_MAX_FRAME;
	if (mf > DECK_LINK_MAX_FRAME)
		mf = DECK_LINK_MAX_FRAME;
	if (mf < 512)
		mf = 512;
	h.max_frame = mf;
	ctx->last_hello_ms = now_ms(ctx);
	send_frame(ctx, next_seq(ctx), agentsdeck_link_v1_Frame_hello_tag, agentsdeck_link_v1_Hello_fields, &h);
}

/* ---- lifecycle ------------------------------------------------------------- */

void deck_link_init(deck_link_t *ctx, const deck_link_config_t *cfg)
{
	memset(ctx, 0, sizeof *ctx);
	ctx->cfg = *cfg;
	ctx->sub.first_slot = cfg->first_slot;
	ctx->sub.slot_count = cfg->slot_count;
	ctx->sub.fields = cfg->fields;
}

void deck_link_begin(deck_link_t *ctx)
{
	ctx->state = DECK_LINK_CONNECTING;
	ctx->approved = false;
	ctx->cert_serial[0] = '\0';
	ctx->scope = agentsdeck_link_v1_Scope_SCOPE_UNSPECIFIED;
	ctx->last_rx_ms = now_ms(ctx);
	send_hello(ctx);
}

void deck_link_poll(deck_link_t *ctx)
{
	uint32_t now = now_ms(ctx);
	switch (ctx->state) {
	case DECK_LINK_CONNECTING:
		if ((uint32_t)(now - ctx->last_hello_ms) >= DECK_LINK_HELLO_RETRY_MS)
			send_hello(ctx);
		break;
	case DECK_LINK_ONLINE:
		if ((uint32_t)(now - ctx->last_rx_ms) >= DECK_LINK_OFFLINE_MS) {
			ctx->state = DECK_LINK_OFFLINE;
			if (ctx->cfg.on_offline)
				ctx->cfg.on_offline(ctx);
			send_hello(ctx);
		}
		break;
	case DECK_LINK_OFFLINE:
		if ((uint32_t)(now - ctx->last_hello_ms) >= DECK_LINK_OFFLINE_HELLO_MS)
			send_hello(ctx);
		break;
	}
}

/* ---- decoding -------------------------------------------------------------- */

static void dispatch(deck_link_t *ctx)
{
	const deck_link_config_t *c = &ctx->cfg;
	agentsdeck_link_v1_Frame *f = &ctx->frame;

	ctx->stats.rx_frames++;
	ctx->last_rx_ms = now_ms(ctx);
	if (ctx->state == DECK_LINK_OFFLINE && f->which_body != agentsdeck_link_v1_Frame_welcome_tag) {
		ctx->state = DECK_LINK_ONLINE; /* the deck was only quiet */
		if (c->on_online)
			c->on_online(ctx);
	}
	if (c->on_frame)
		c->on_frame(ctx, f);

	switch (f->which_body) {
	case agentsdeck_link_v1_Frame_welcome_tag: {
		const agentsdeck_link_v1_Welcome *w = &f->body.welcome;
		bool was_online = ctx->state == DECK_LINK_ONLINE;
		ctx->state = DECK_LINK_ONLINE;
		ctx->approved = w->approved;
		ctx->scope = w->scope;
		copy_str(ctx->cert_serial, sizeof ctx->cert_serial, w->cert_serial);
		if (!was_online && c->on_online)
			c->on_online(ctx);
		if (w->approved) /* a Welcome restarts the conversation: subscribe (again) */
			send_frame(ctx, next_seq(ctx), agentsdeck_link_v1_Frame_subscribe_tag,
				   agentsdeck_link_v1_Subscribe_fields, &ctx->sub);
		if (c->on_welcome)
			c->on_welcome(ctx, w->device_name, w->scope, w->approved);
		break;
	}
	case agentsdeck_link_v1_Frame_ping_tag: {
		agentsdeck_link_v1_Pong p;
		p.time_ms = f->body.ping.time_ms;
		send_frame(ctx, f->seq, agentsdeck_link_v1_Frame_pong_tag, agentsdeck_link_v1_Pong_fields, &p);
		if (c->on_ping)
			c->on_ping(ctx, f->seq, f->body.ping.time_ms);
		break;
	}
	case agentsdeck_link_v1_Frame_snapshot_begin_tag:
		if (c->on_snapshot_begin)
			c->on_snapshot_begin(ctx, &f->body.snapshot_begin.server, f->body.snapshot_begin.agent_count);
		break;
	case agentsdeck_link_v1_Frame_agent_changed_tag:
		if (c->on_agent && f->body.agent_changed.has_agent)
			c->on_agent(ctx, &f->body.agent_changed.agent);
		break;
	case agentsdeck_link_v1_Frame_agent_removed_tag:
		if (c->on_agent_removed)
			c->on_agent_removed(ctx, f->body.agent_removed.id);
		break;
	case agentsdeck_link_v1_Frame_snapshot_end_tag:
		if (c->on_snapshot_end)
			c->on_snapshot_end(ctx);
		break;
	case agentsdeck_link_v1_Frame_result_tag:
		if (c->on_result)
			c->on_result(ctx, f->seq, f->body.result.code, f->body.result.error);
		break;
	case agentsdeck_link_v1_Frame_hat_list_tag:
		if (c->on_hat_list)
			c->on_hat_list(ctx, f->seq, &f->body.hat_list);
		break;
	case agentsdeck_link_v1_Frame_live_sessions_tag:
		if (c->on_live_sessions)
			c->on_live_sessions(ctx, f->seq, &f->body.live_sessions);
		break;
	case agentsdeck_link_v1_Frame_restore_result_tag:
		if (c->on_restore_result)
			c->on_restore_result(ctx, f->seq, &f->body.restore_result);
		break;
	case agentsdeck_link_v1_Frame_cert_issued_tag:
		if (c->on_cert_issued)
			c->on_cert_issued(ctx, f->seq, &f->body.cert_issued);
		break;
	case agentsdeck_link_v1_Frame_wifi_config_tag: {
		const agentsdeck_link_v1_WifiConfig *w = &f->body.wifi_config;
		agentsdeck_link_v1_Result r;
		memset(&r, 0, sizeof r);
		r.code = c->on_wifi_config ? c->on_wifi_config(ctx, w->ssid, w->password, w->forget) : 501;
		if (!c->on_wifi_config)
			copy_str(r.error, sizeof r.error, "no Wi-Fi on this board");
		send_frame(ctx, f->seq, agentsdeck_link_v1_Frame_result_tag, agentsdeck_link_v1_Result_fields, &r);
		/* The frame held a password: don't keep it around. */
		memset(&f->body.wifi_config, 0, sizeof f->body.wifi_config);
		break;
	}
	default:
		break; /* a Pong, or something this board doesn't handle */
	}
}

/* Checks a COBS-encoded candidate without changing it: valid COBS, at least
 * magic+version+CRC, and the CRC matches. The CRC runs two bytes behind the
 * decoded stream so the trailing checksum is left out. */
static bool check_candidate(const uint8_t *s, size_t n)
{
	uint16_t crc = DECK_CRC16_INIT;
	uint8_t hist[2] = {0, 0};
	size_t out = 0;
	if (n == 0 || s[0] < 4) /* the first block must hold the magic */
		return false;
	for (size_t i = 0; i < n;) {
		size_t code = s[i];
		if (code == 0 || i + code > n)
			return false;
		for (size_t k = 1; k <= code; k++) {
			uint8_t b;
			if (k < code)
				b = s[i + k];
			else if (code < 0xFF && i + code < n)
				b = 0;
			else
				break;
			if (out >= 2)
				crc = deck_crc16_update(crc, &hist[out & 1], 1);
			hist[out & 1] = b;
			out++;
		}
		i += code;
	}
	if (out < 5)
		return false;
	return crc == (uint16_t)((hist[(out - 2) & 1] << 8) | hist[(out - 1) & 1]);
}

/* The chunk rx[0..rx_len) ended at a 0x00: find the frame in it, skipping
 * noise in front (its COBS code byte sits just before the magic), exactly
 * like the Go decoder. */
static void process_chunk(deck_link_t *ctx)
{
	uint8_t *buf = ctx->rx;
	size_t len = ctx->rx_len;
	bool tried = false;
	for (size_t j = 1; j + 3 <= len; j++) {
		if (memcmp(buf + j, magic, 3) != 0)
			continue;
		size_t start = j - 1;
		tried = true;
		if (!check_candidate(buf + start, len - start))
			continue;
		size_t raw_len;
		/* Decoding in place destroys the chunk, so this is the last try. */
		if (deck_cobs_decode(buf + start, len - start, buf + start, &raw_len)) {
			pb_istream_t s = pb_istream_from_buffer(buf + start + 3, raw_len - 5);
			if (pb_decode(&s, agentsdeck_link_v1_Frame_fields, &ctx->frame)) {
				dispatch(ctx);
				return;
			}
		}
		break; /* CRC fine but not a Frame this build can hold */
	}
	if (tried)
		ctx->stats.rx_bad++;
}

/* The buffer is full and no 0x00 came: keep what may still be a frame. */
static void rx_trim(deck_link_t *ctx)
{
	uint8_t *buf = ctx->rx;
	size_t len = ctx->rx_len;
	for (size_t j = 2; j + 3 <= len; j++) {
		if (memcmp(buf + j, magic, 3) == 0) { /* a later frame start: drop the noise before it */
			memmove(buf, buf + j - 1, len - (j - 1));
			ctx->rx_len = len - (j - 1);
			return;
		}
	}
	if (len >= 4 && memcmp(buf + 1, magic, 3) == 0) { /* one frame, too large */
		ctx->stats.rx_overflow++;
		ctx->rx_len = 0;
		ctx->rx_discard = true;
		return;
	}
	/* Noise: keep the last 3 bytes, which may start a frame. */
	memmove(buf, buf + len - 3, 3);
	ctx->rx_len = 3;
}

void deck_link_feed(deck_link_t *ctx, const uint8_t *data, size_t len)
{
	for (size_t i = 0; i < len; i++) {
		uint8_t b = data[i];
		if (b == 0) {
			if (!ctx->rx_discard && ctx->rx_len > 0)
				process_chunk(ctx);
			ctx->rx_len = 0;
			ctx->rx_discard = false;
			continue;
		}
		if (ctx->rx_discard)
			continue;
		if (ctx->rx_len == sizeof ctx->rx)
			rx_trim(ctx);
		if (ctx->rx_discard)
			continue;
		ctx->rx[ctx->rx_len++] = b;
	}
}

/* ---- state ----------------------------------------------------------------- */

deck_link_state_t deck_link_state(const deck_link_t *ctx) { return ctx->state; }
bool deck_link_online(const deck_link_t *ctx) { return ctx->state == DECK_LINK_ONLINE; }
bool deck_link_approved(const deck_link_t *ctx) { return ctx->approved; }
agentsdeck_link_v1_Scope deck_link_scope(const deck_link_t *ctx) { return ctx->scope; }
void *deck_link_user(const deck_link_t *ctx) { return ctx->cfg.user; }
const deck_link_stats_t *deck_link_stats(const deck_link_t *ctx) { return &ctx->stats; }
const char *deck_link_cert_serial(const deck_link_t *ctx) { return ctx->cert_serial; }

/* ---- commands -------------------------------------------------------------- */

static uint32_t send_cmd(deck_link_t *ctx, pb_size_t tag, const pb_msgdesc_t *fields, const void *msg)
{
	return send_frame(ctx, next_seq(ctx), tag, fields, msg);
}

/* Focus, Interrupt, Dismiss and Resume are all { string agent_id = 1; }. */
static uint32_t send_agent_cmd(deck_link_t *ctx, pb_size_t tag, const pb_msgdesc_t *fields, const char *agent_id)
{
	/* Same layout for all four (a static assert below); nanopb reads by offset. */
	agentsdeck_link_v1_Focus m;
	copy_str(m.agent_id, sizeof m.agent_id, agent_id);
	return send_cmd(ctx, tag, fields, &m);
}

typedef char focus_layout_check[(sizeof(agentsdeck_link_v1_Focus) == sizeof(agentsdeck_link_v1_Interrupt) &&
				  sizeof(agentsdeck_link_v1_Focus) == sizeof(agentsdeck_link_v1_Dismiss) &&
				  sizeof(agentsdeck_link_v1_Focus) == sizeof(agentsdeck_link_v1_Resume))
				 ? 1
				 : -1];

uint32_t deck_get_state(deck_link_t *ctx)
{
	agentsdeck_link_v1_GetState m = agentsdeck_link_v1_GetState_init_zero;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_get_state_tag, agentsdeck_link_v1_GetState_fields, &m);
}

uint32_t deck_answer(deck_link_t *ctx, const char *agent_id, const char *prompt_id, const char *key)
{
	agentsdeck_link_v1_Answer m;
	copy_str(m.agent_id, sizeof m.agent_id, agent_id);
	copy_str(m.prompt_id, sizeof m.prompt_id, prompt_id);
	copy_str(m.key, sizeof m.key, key);
	return send_cmd(ctx, agentsdeck_link_v1_Frame_answer_tag, agentsdeck_link_v1_Answer_fields, &m);
}

uint32_t deck_focus(deck_link_t *ctx, const char *agent_id)
{
	return send_agent_cmd(ctx, agentsdeck_link_v1_Frame_focus_tag, agentsdeck_link_v1_Focus_fields, agent_id);
}

uint32_t deck_interrupt(deck_link_t *ctx, const char *agent_id)
{
	return send_agent_cmd(ctx, agentsdeck_link_v1_Frame_interrupt_tag, agentsdeck_link_v1_Interrupt_fields,
			      agent_id);
}

uint32_t deck_dismiss(deck_link_t *ctx, const char *agent_id)
{
	return send_agent_cmd(ctx, agentsdeck_link_v1_Frame_dismiss_tag, agentsdeck_link_v1_Dismiss_fields, agent_id);
}

uint32_t deck_resume(deck_link_t *ctx, const char *agent_id)
{
	return send_agent_cmd(ctx, agentsdeck_link_v1_Frame_resume_tag, agentsdeck_link_v1_Resume_fields, agent_id);
}

uint32_t deck_move(deck_link_t *ctx, const char *agent_id, uint32_t slot)
{
	agentsdeck_link_v1_Move m;
	copy_str(m.agent_id, sizeof m.agent_id, agent_id);
	m.slot = slot;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_move_tag, agentsdeck_link_v1_Move_fields, &m);
}

uint32_t deck_order(deck_link_t *ctx, const char *const *agent_ids, size_t n)
{
	agentsdeck_link_v1_Order m;
	size_t max = sizeof m.agent_ids / sizeof m.agent_ids[0];
	m.agent_ids_count = 0;
	for (size_t i = 0; i < n && i < max; i++)
		copy_str(m.agent_ids[m.agent_ids_count++], sizeof m.agent_ids[0], agent_ids[i]);
	return send_cmd(ctx, agentsdeck_link_v1_Frame_order_tag, agentsdeck_link_v1_Order_fields, &m);
}

uint32_t deck_set_hat(deck_link_t *ctx, const char *agent_id, const char *folder, const char *shape,
		      const char *color, bool auto_hat)
{
	agentsdeck_link_v1_SetHat m;
	bool by_agent = agent_id && agent_id[0];
	copy_str(m.agent_id, sizeof m.agent_id, by_agent ? agent_id : NULL);
	copy_str(m.folder, sizeof m.folder, by_agent ? NULL : folder);
	copy_str(m.shape, sizeof m.shape, shape);
	copy_str(m.color, sizeof m.color, color);
	m.automatic = auto_hat;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_set_hat_tag, agentsdeck_link_v1_SetHat_fields, &m);
}

uint32_t deck_list_hats(deck_link_t *ctx)
{
	agentsdeck_link_v1_ListHats m = agentsdeck_link_v1_ListHats_init_zero;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_list_hats_tag, agentsdeck_link_v1_ListHats_fields, &m);
}

uint32_t deck_list_live(deck_link_t *ctx)
{
	agentsdeck_link_v1_ListLive m = agentsdeck_link_v1_ListLive_init_zero;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_list_live_tag, agentsdeck_link_v1_ListLive_fields, &m);
}

uint32_t deck_add_live(deck_link_t *ctx, const char *session_id, int32_t slot)
{
	agentsdeck_link_v1_AddLive m;
	copy_str(m.session_id, sizeof m.session_id, session_id);
	m.slot = slot;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_add_live_tag, agentsdeck_link_v1_AddLive_fields, &m);
}

uint32_t deck_restore(deck_link_t *ctx)
{
	agentsdeck_link_v1_Restore m = agentsdeck_link_v1_Restore_init_zero;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_restore_tag, agentsdeck_link_v1_Restore_fields, &m);
}

uint32_t deck_subscribe(deck_link_t *ctx, uint32_t first_slot, uint32_t slot_count, uint32_t fields)
{
	ctx->sub.first_slot = first_slot;
	ctx->sub.slot_count = slot_count;
	ctx->sub.fields = fields;
	return send_cmd(ctx, agentsdeck_link_v1_Frame_subscribe_tag, agentsdeck_link_v1_Subscribe_fields, &ctx->sub);
}

uint32_t deck_ping(deck_link_t *ctx)
{
	agentsdeck_link_v1_Ping m;
	m.time_ms = now_ms(ctx);
	return send_cmd(ctx, agentsdeck_link_v1_Frame_ping_tag, agentsdeck_link_v1_Ping_fields, &m);
}

uint32_t deck_cert_request(deck_link_t *ctx, const uint8_t *csr, size_t len)
{
	agentsdeck_link_v1_CertRequest m;
	if (!csr || len == 0 || len > sizeof m.csr.bytes)
		return 0;
	m.csr.size = (pb_size_t)len;
	memcpy(m.csr.bytes, csr, len);
	return send_cmd(ctx, agentsdeck_link_v1_Frame_cert_request_tag, agentsdeck_link_v1_CertRequest_fields, &m);
}

uint32_t deck_net_status(deck_link_t *ctx, const agentsdeck_link_v1_NetStatus *status)
{
	return send_cmd(ctx, agentsdeck_link_v1_Frame_net_status_tag, agentsdeck_link_v1_NetStatus_fields, status);
}
