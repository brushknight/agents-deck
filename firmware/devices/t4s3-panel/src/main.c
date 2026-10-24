// t4s3-panel: the agents deck on a LilyGo T4-S3 (2.41" AMOLED, touch), over
// Deck Link on its USB port. Two FreeRTOS tasks: Deck Link (link.c) and the
// UI (LVGL, here and in ui.c); they share the agents state (agents.c).
#include <stdlib.h>

#include "agents.h"
#include "debug.h"
#include "display.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "link.h"
#include "lvgl.h"
#include "touch.h"
#include "ui.h"

#define BUF_LINES 100 // per draw buffer at most: 450 x 100 pixels, twice, in internal RAM
#define BRIGHTNESS 200

static const char *TAG = "panel";
static bool touch_ok;

static uint32_t tick(void) { return now_ms(); }

#ifdef PANEL_DEBUG
#include "esp_timer.h"
static int64_t sent_at;
#endif

static void IRAM_ATTR flushed(void *disp)
{
#ifdef PANEL_DEBUG
	debug_spi((uint32_t)(esp_timer_get_time() - sent_at));
#endif
	lv_display_flush_ready(disp);
}

// Sends a rendered strip by DMA; LVGL renders the next one into the other
// buffer meanwhile.
static void flush(lv_display_t *disp, const lv_area_t *area, uint8_t *px)
{
	lv_draw_sw_rgb565_swap(px, lv_area_get_size(area)); // the panel wants big-endian
#ifdef PANEL_DEBUG
	if (lv_display_flush_is_last(disp))
		debug_frame();
#endif
#ifdef PANEL_DEBUG
	sent_at = esp_timer_get_time();
#endif
	display_draw(area->x1, area->y1, area->x2, area->y2, (const uint16_t *)px, flushed, disp);
}

// The panel takes windows that start on an even pixel and are an even
// number of pixels wide and high.
static void round_area(lv_event_t *e)
{
	lv_area_t *a = lv_event_get_param(e);
	a->x1 &= ~1;
	a->y1 &= ~1;
	a->x2 |= 1;
	a->y2 |= 1;
}

static void read_touch(lv_indev_t *indev, lv_indev_data_t *data)
{
	(void)indev;
	int x, y;
	bool down = touch_ok && touch_read(&x, &y);
#ifdef PANEL_DEBUG
	down = down || debug_touch(&x, &y);
#endif
	if (down) {
		data->point.x = x;
		data->point.y = y;
		data->state = LV_INDEV_STATE_PRESSED;
	} else {
		data->state = LV_INDEV_STATE_RELEASED;
	}
}

#ifdef PANEL_DEBUG
static void indev_note(lv_event_t *e)
{
	lv_event_code_t c = lv_event_get_code(e);
	if (c != LV_EVENT_PRESSED && c != LV_EVENT_RELEASED && c != LV_EVENT_SCROLL_BEGIN && c != LV_EVENT_GESTURE &&
	    c != LV_EVENT_CLICKED)
		return;
	lv_indev_t *in = lv_event_get_target(e);
	lv_point_t p;
	lv_indev_get_point(in, &p);
	debug_note("indev ev%d obj=%p scr=%p at %d,%d", (int)c, (void *)lv_indev_get_active_obj(), (void *)lv_screen_active(),
		   (int)p.x, (int)p.y);
}
#endif

static void ui_task(void *arg)
{
	(void)arg;
	bool lit = false;
	for (;;) {
		lv_lock(); // LVGL's draw threads run alongside
		ui_update();
		lv_unlock();
#ifdef PANEL_DEBUG
		int64_t t0 = esp_timer_get_time();
#endif
		uint32_t wait = lv_timer_handler();
#ifdef PANEL_DEBUG
		debug_busy((uint32_t)(esp_timer_get_time() - t0));
#endif
#ifdef PANEL_DEBUG
		debug_poll();
#endif
		if (!lit) { // light the panel once the first frame is on it
			lv_lock(); // the panel's bus is idle between frames
			display_brightness(BRIGHTNESS);
			lv_unlock();
			lit = true;
		}
		vTaskDelay(pdMS_TO_TICKS(wait < 2 ? 2 : wait > 30 ? 30 : wait));
	}
}

void app_main(void)
{
	agents_init();
	display_init();
	touch_ok = touch_init();

	lv_init();
	lv_tick_set_cb(tick);
	lv_display_t *disp = lv_display_create(DISPLAY_W, DISPLAY_H);
	// Two draw buffers in DMA-capable internal RAM, as big as fit.
	void *b1 = NULL, *b2 = NULL;
	size_t size = 0;
	for (int lines = BUF_LINES; lines >= 20 && !b2; lines -= 20) {
		free(b1);
		size = DISPLAY_W * lines * 2;
		b1 = heap_caps_malloc(size, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
		b2 = b1 ? heap_caps_malloc(size, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL) : NULL;
	}
	ESP_LOGI(TAG, "draw buffers: 2 x %u lines", (unsigned)(size / DISPLAY_W / 2));
	lv_display_set_buffers(disp, b1, b2, size, LV_DISPLAY_RENDER_MODE_PARTIAL);
	lv_display_set_flush_cb(disp, flush);
	lv_display_add_event_cb(disp, round_area, LV_EVENT_INVALIDATE_AREA, NULL);
	lv_indev_t *indev = lv_indev_create();
	lv_indev_set_type(indev, LV_INDEV_TYPE_POINTER);
	lv_indev_set_read_cb(indev, read_touch);
#ifdef PANEL_DEBUG
	lv_indev_add_event_cb(indev, indev_note, LV_EVENT_ALL, NULL);
#endif

	ui_init();
	link_start();
	xTaskCreatePinnedToCore(ui_task, "ui", 16384, NULL, 4, NULL, 1);
	ESP_LOGI(TAG, "running (touch %s)", touch_ok ? "ok" : "missing");
}
