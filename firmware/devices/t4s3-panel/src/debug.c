#include "debug.h"

#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>
#include <string.h>

#include "agents.h"
#include "driver/usb_serial_jtag.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "lvgl.h"

static SemaphoreHandle_t usb_lock;

void usb_write_lock(void)
{
	if (!usb_lock)
		usb_lock = xSemaphoreCreateMutex();
	xSemaphoreTake(usb_lock, portMAX_DELAY);
}

void usb_write_unlock(void) { xSemaphoreGive(usb_lock); }

#ifdef PANEL_DEBUG
static char line[32];
static size_t line_len;
static volatile bool snap_wanted, perf_wanted, log_wanted;
static char notes[1024];
static size_t notes_len;
static volatile uint32_t frames, perf_since, busy_us, spi_us;
static volatile int tap_x, tap_y, to_x, to_y;
static volatile uint32_t tap_from, tap_until;

void debug_scan(const uint8_t *data, size_t len)
{
	for (size_t i = 0; i < len; i++) {
		char c = (char)data[i];
		if (c == '\n' || c == '\0') {
			line[line_len] = '\0';
			int x, y, x2, y2;
			if (strcmp(line, "!snap") == 0)
				snap_wanted = true;
			else if (strcmp(line, "!perf") == 0)
				perf_wanted = true;
			else if (strcmp(line, "!log") == 0)
				log_wanted = true;
			else if (sscanf(line, "!drag %d %d %d %d", &x, &y, &x2, &y2) == 4) {
				tap_x = x, tap_y = y, to_x = x2, to_y = y2;
				tap_from = now_ms();
				tap_until = tap_from + 300;
			} else if (sscanf(line, "!tap %d %d", &x, &y) == 2) {
				tap_x = x, tap_y = y, to_x = x, to_y = y;
				tap_from = now_ms();
				tap_until = tap_from + 150;
			}
			line_len = 0;
		} else if (line_len < sizeof line - 1 && c >= ' ' && c < 0x7F) {
			line[line_len++] = c;
		} else {
			line_len = 0;
		}
	}
}

bool debug_touch(int *x, int *y)
{
	uint32_t now = now_ms();
	if ((int32_t)(tap_until - now) <= 0)
		return false;
	float f = (float)(now - tap_from) / (float)(tap_until - tap_from);
	*x = tap_x + (int)((to_x - tap_x) * f);
	*y = tap_y + (int)((to_y - tap_y) * f);
	return true;
}

static void put(const void *p, size_t n)
{
	const uint8_t *b = p;
	while (n > 0) {
		int w = usb_serial_jtag_write_bytes(b, n > 4096 ? 4096 : n, pdMS_TO_TICKS(500));
		if (w <= 0)
			return;
		b += w, n -= w;
	}
}

void debug_frame(void) { frames++; }

void debug_note(const char *fmt, ...)
{
	char b[96];
	va_list ap;
	va_start(ap, fmt);
	int n = vsnprintf(b, sizeof b, fmt, ap);
	va_end(ap);
	if (n < 0)
		return;
	if (notes_len + n + 2 > sizeof notes)
		notes_len = 0;
	notes_len += snprintf(notes + notes_len, sizeof notes - notes_len, "%u:%s|", (unsigned)(now_ms() % 100000), b);
}
void debug_busy(uint32_t us) { busy_us += us; }
void debug_spi(uint32_t us) { spi_us += us; }

void debug_poll(void)
{
	if (log_wanted) {
		log_wanted = false;
		usb_write_lock();
		put("\0LOG ", 5);
		put(notes, notes_len);
		put("\n", 1);
		usb_write_unlock();
		notes_len = 0;
	}
	if (perf_wanted) { // frames drawn since the last "!perf"
		perf_wanted = false;
		char b[64];
		uint32_t now = now_ms();
		int n = snprintf(b, sizeof b, "%cPERF %u %u %u %u\n", 0, (unsigned)frames, (unsigned)(now - perf_since),
				 (unsigned)(busy_us / 1000), (unsigned)(spi_us / 1000));
		frames = 0, perf_since = now, busy_us = 0, spi_us = 0;
		usb_write_lock();
		put(b, n);
		usb_write_unlock();
	}
	if (!snap_wanted)
		return;
	snap_wanted = false;
	lv_draw_buf_t *img = lv_snapshot_take(lv_screen_active(), LV_COLOR_FORMAT_RGB565);
	if (!img)
		return;
	char head[32];
	int n = snprintf(head, sizeof head, "%cSNAP %d %d\n", 0, (int)img->header.w, (int)img->header.h);
	usb_write_lock();
	put(head, n);
	for (uint32_t y = 0; y < img->header.h; y++)
		put(img->data + y * img->header.stride, img->header.w * 2);
	usb_write_unlock();
	lv_draw_buf_destroy(img);
}
#endif
