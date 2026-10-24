#include "link.h"

#include <stdio.h>
#include <string.h>

#include "agents.h"
#include "debug.h"
#include "deck_link.h"
#include "display.h"
#include "driver/usb_serial_jtag.h"
#include "esp_mac.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#define FIRMWARE "1.0.0"

static deck_link_t link;
static QueueHandle_t actions;
static uint32_t pending_seq; // the action we wait a Result for
static char board_id[20];

static size_t usb_write(const uint8_t *data, size_t len, void *user)
{
	(void)user;
	usb_write_lock();
	int n = usb_serial_jtag_write_bytes(data, len, pdMS_TO_TICKS(50));
	usb_write_unlock();
	return n > 0 ? (size_t)n : 0;
}

static uint32_t link_millis(void *user)
{
	(void)user;
	return now_ms();
}

static void set_link(link_status_t st)
{
	agents_t *s = agents_lock();
	if (s->link != st) {
		s->link = st;
		s->version++;
	}
	agents_unlock();
}

static void on_welcome(deck_link_t *ctx, const char *name, agentsdeck_link_v1_Scope scope, bool approved)
{
	(void)ctx;
	(void)scope;
	agents_t *s = agents_lock();
	snprintf(s->device_name, sizeof s->device_name, "%s", name);
	s->link = approved ? LINK_ONLINE : LINK_NOT_APPROVED;
	s->version++;
	agents_unlock();
}

static void on_snapshot_begin(deck_link_t *ctx, const agentsdeck_link_v1_Server *server, uint32_t n)
{
	(void)ctx;
	(void)n;
	agents_t *s = agents_lock();
	if (server->time_ms)
		s->clock_offset_ms = server->time_ms - (int64_t)now_ms();
	agents_snapshot_begin(s);
	agents_unlock();
}

static void on_agent(deck_link_t *ctx, const agentsdeck_link_v1_Agent *a)
{
	(void)ctx;
	agents_t *s = agents_lock();
	agents_put(s, a);
	agents_unlock();
}

static void on_agent_removed(deck_link_t *ctx, const char *id)
{
	(void)ctx;
	agents_t *s = agents_lock();
	agents_remove(s, id);
	agents_unlock();
}

static void on_snapshot_end(deck_link_t *ctx)
{
	(void)ctx;
	agents_t *s = agents_lock();
	agents_snapshot_end(s);
	agents_unlock();
}

static void on_ping(deck_link_t *ctx, uint32_t seq, uint64_t time_ms)
{
	(void)ctx;
	(void)seq;
	agents_t *s = agents_lock();
	if (time_ms)
		s->clock_offset_ms = (int64_t)time_ms - (int64_t)now_ms();
	agents_unlock();
}

static void on_result(deck_link_t *ctx, uint32_t seq, uint32_t code, const char *error)
{
	(void)ctx;
	if (seq != pending_seq)
		return;
	pending_seq = 0;
	char msg[96];
	agents_t *s = agents_lock();
	switch (code) {
	case 200:
	case 204: snprintf(msg, sizeof msg, "done"); break;
	case 403: snprintf(msg, sizeof msg, "not allowed yet; on the Mac: agentctl devices scope %s act", s->device_name); break;
	case 409: snprintf(msg, sizeof msg, "the prompt changed"); break;
	default: snprintf(msg, sizeof msg, "%s", error && error[0] ? error : "failed");
	}
	agents_say(s, msg, code == 403 ? 8000 : 4000);
	agents_unlock();
}

static void on_offline(deck_link_t *ctx)
{
	(void)ctx;
	set_link(LINK_OFFLINE);
}

static void on_online(deck_link_t *ctx)
{
	set_link(deck_link_approved(ctx) ? LINK_ONLINE : LINK_NOT_APPROVED);
}

static void run_action(const action_t *a)
{
	switch (a->kind) {
	case ACTION_ANSWER: pending_seq = deck_answer(&link, a->agent_id, a->prompt_id, a->key); break;
	case ACTION_FOCUS: pending_seq = deck_focus(&link, a->agent_id); break;
	case ACTION_INTERRUPT: pending_seq = deck_interrupt(&link, a->agent_id); break;
	}
}

static void link_task(void *arg)
{
	(void)arg;
	uint8_t buf[512];
	deck_link_begin(&link);
	for (;;) {
		int n = usb_serial_jtag_read_bytes(buf, sizeof buf, pdMS_TO_TICKS(10));
		if (n > 0) {
#ifdef PANEL_DEBUG
			debug_scan(buf, n);
#endif
			deck_link_feed(&link, buf, n);
		}
		action_t a;
		while (xQueueReceive(actions, &a, 0) == pdTRUE)
			run_action(&a);
		deck_link_poll(&link);
	}
}

void link_start(void)
{
	usb_serial_jtag_driver_config_t usb = {.rx_buffer_size = 4096, .tx_buffer_size = 4096};
	ESP_ERROR_CHECK(usb_serial_jtag_driver_install(&usb));
	actions = xQueueCreate(8, sizeof(action_t));

	uint8_t mac[6];
	esp_read_mac(mac, ESP_MAC_WIFI_STA);
	snprintf(board_id, sizeof board_id, "%02x%02x%02x%02x%02x%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);

	deck_link_config_t cfg = {
		.write = usb_write,
		.millis = link_millis,
		.board_id = board_id,
		.type = "t4s3-panel",
		.firmware = FIRMWARE,
		.display_width = DISPLAY_W,
		.display_height = DISPLAY_H,
		.display_color = true,
		.inputs = {agentsdeck_link_v1_Input_INPUT_TOUCH},
		.input_count = 1,
		.slot_count = 0,  // every agent; the panel pages through them
		.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_CARD |
			  agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT | agentsdeck_link_v1_FieldGroup_FIELDS_NUMBERS,
		.on_welcome = on_welcome,
		.on_snapshot_begin = on_snapshot_begin,
		.on_agent = on_agent,
		.on_agent_removed = on_agent_removed,
		.on_snapshot_end = on_snapshot_end,
		.on_result = on_result,
		.on_online = on_online,
		.on_offline = on_offline,
		.on_ping = on_ping,
	};
	deck_link_init(&link, &cfg);
	xTaskCreatePinnedToCore(link_task, "deck_link", 8192, NULL, 5, NULL, 0);
}

bool link_send(const action_t *a) { return xQueueSend(actions, a, 0) == pdTRUE; }
