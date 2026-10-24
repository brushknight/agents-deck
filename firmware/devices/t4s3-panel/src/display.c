// RM690B0 over QSPI. A register write is command 0x02 with the register in
// the address phase; pixels go as command 0x32 with address 0x002C00 (RAMWR)
// on all four lines. The init sequence and pins are LilyGo's
// (github.com/Xinyuan-LilyGO/LilyGo-AMOLED-Series, BOARD_AMOLED_241).
#include "display.h"

#include <string.h>

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define PIN_D0 14
#define PIN_D1 10
#define PIN_D2 16
#define PIN_D3 12
#define PIN_SCK 15
#define PIN_CS 11
#define PIN_RST 13
#define PIN_POWER 9 // the panel's power switch, active high
#define SPI_HZ (36 * 1000 * 1000)
#define COL_OFFSET 16 // the panel's columns start at 16 in its RAM
#define CHUNK 32768   // bytes per SPI transaction
#define MAX_CHUNKS 4  // per write: 450 x 100 pixels

static const char *TAG = "display";
static spi_device_handle_t spi;
static spi_transaction_ext_t chunks[MAX_CHUNKS];
static int queued; // chunks sent but not collected
static void (*done_fn)(void *);
static void *done_arg;

// After each chunk (in the SPI interrupt): the last one ends the write.
static void IRAM_ATTR chunk_sent(spi_transaction_t *t)
{
	if ((intptr_t)t->user == 1) {
		gpio_set_level(PIN_CS, 1);
		if (done_fn)
			done_fn(done_arg);
	}
}

// Collects the chunks of the last write, so the queue is free again.
static void collect(void)
{
	spi_transaction_t *t;
	while (queued > 0 && spi_device_get_trans_result(spi, &t, portMAX_DELAY) == ESP_OK)
		queued--;
}

// One register write: command 0x02, register in the address phase.
static void collect(void);

static void reg(uint8_t r, const uint8_t *data, size_t len)
{
	collect();
	spi_transaction_t t = {
		.flags = SPI_TRANS_MULTILINE_CMD | SPI_TRANS_MULTILINE_ADDR,
		.cmd = 0x02,
		.addr = (uint32_t)r << 8,
		.length = len * 8,
		.tx_buffer = len ? data : NULL,
	};
	gpio_set_level(PIN_CS, 0);
	ESP_ERROR_CHECK(spi_device_polling_transmit(spi, &t));
	gpio_set_level(PIN_CS, 1);
}

static void reg1(uint8_t r, uint8_t v) { reg(r, &v, 1); }

void display_init(void)
{
	gpio_config_t out = {
		.pin_bit_mask = (1ULL << PIN_CS) | (1ULL << PIN_RST) | (1ULL << PIN_POWER),
		.mode = GPIO_MODE_OUTPUT,
	};
	ESP_ERROR_CHECK(gpio_config(&out));
	gpio_set_level(PIN_CS, 1);
	gpio_set_level(PIN_POWER, 1);
	vTaskDelay(pdMS_TO_TICKS(20));

	spi_bus_config_t bus = {
		.data0_io_num = PIN_D0,
		.data1_io_num = PIN_D1,
		.sclk_io_num = PIN_SCK,
		.data2_io_num = PIN_D2,
		.data3_io_num = PIN_D3,
		.max_transfer_sz = CHUNK,
		.flags = SPICOMMON_BUSFLAG_MASTER | SPICOMMON_BUSFLAG_QUAD,
	};
	ESP_ERROR_CHECK(spi_bus_initialize(SPI2_HOST, &bus, SPI_DMA_CH_AUTO));
	spi_device_interface_config_t dev = {
		.command_bits = 8,
		.address_bits = 24,
		.mode = 0,
		.clock_speed_hz = SPI_HZ,
		.spics_io_num = -1, // CS by hand: a frame is several transactions
		.flags = SPI_DEVICE_HALFDUPLEX,
		.queue_size = MAX_CHUNKS,
		.post_cb = chunk_sent,
	};
	ESP_ERROR_CHECK(spi_bus_add_device(SPI2_HOST, &dev, &spi));
	int khz = 0;
	spi_device_get_actual_freq(spi, &khz);
	ESP_LOGI(TAG, "QSPI at %d kHz", khz);

	gpio_set_level(PIN_RST, 1);
	vTaskDelay(pdMS_TO_TICKS(20));
	gpio_set_level(PIN_RST, 0);
	vTaskDelay(pdMS_TO_TICKS(20));
	gpio_set_level(PIN_RST, 1);
	vTaskDelay(pdMS_TO_TICKS(120));

	reg1(0xFE, 0x20); // page 0x20
	reg1(0x26, 0x0A); // MIPI off
	reg1(0x24, 0x80); // SPI write RAM
	reg1(0x5A, 0x51); // SWIRE
	reg1(0x5B, 0x2E);
	reg1(0xFE, 0x00); // page 0
	reg1(0x3A, 0x55); // 16 bits per pixel
	reg1(0xC2, 0x00);
	vTaskDelay(pdMS_TO_TICKS(10));
	reg1(0x35, 0x00); // tearing effect on
	reg1(0x36, 0x00); // portrait, RGB
	reg1(0x51, 0x00); // brightness 0 until the first frame
	reg(0x11, NULL, 0); // sleep out
	vTaskDelay(pdMS_TO_TICKS(120));
	reg(0x29, NULL, 0); // display on
	vTaskDelay(pdMS_TO_TICKS(10));
	ESP_LOGI(TAG, "RM690B0 ready");
}

void display_brightness(uint8_t level) { reg1(0x51, level); }

void display_draw(int x1, int y1, int x2, int y2, const uint16_t *pixels, void (*done)(void *), void *arg)
{
	collect();
	uint16_t xs = x1 + COL_OFFSET, xe = x2 + COL_OFFSET;
	uint8_t caset[4] = {xs >> 8, xs & 0xFF, xe >> 8, xe & 0xFF};
	uint8_t raset[4] = {y1 >> 8, y1 & 0xFF, y2 >> 8, y2 & 0xFF};
	reg(0x2A, caset, 4);
	reg(0x2B, raset, 4);

	size_t left = (size_t)(x2 - x1 + 1) * (y2 - y1 + 1) * 2;
	const uint8_t *p = (const uint8_t *)pixels;
	done_fn = done;
	done_arg = arg;
	gpio_set_level(PIN_CS, 0);
	for (int i = 0; left > 0 && i < MAX_CHUNKS; i++) {
		size_t n = left > CHUNK ? CHUNK : left;
		spi_transaction_ext_t *t = &chunks[i];
		memset(t, 0, sizeof *t);
		t->base.flags = SPI_TRANS_MODE_QIO;
		if (i == 0) {
			t->base.cmd = 0x32;
			t->base.addr = 0x002C00; // RAMWR
		} else { // the rest of the same write: data only
			t->base.flags |= SPI_TRANS_VARIABLE_CMD | SPI_TRANS_VARIABLE_ADDR | SPI_TRANS_VARIABLE_DUMMY;
		}
		t->base.tx_buffer = p;
		t->base.length = n * 8;
		p += n;
		left -= n;
		t->base.user = (void *)(intptr_t)(left == 0);
		ESP_ERROR_CHECK(spi_device_queue_trans(spi, &t->base, portMAX_DELAY));
		queued++;
	}
}
