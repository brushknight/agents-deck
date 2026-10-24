// CST226SE at 0x5A. A touch report is 28 bytes from register 0x00: byte 5
// holds the number of points, byte 6 is 0xAB in a valid report, and point 0
// is bytes 1-3 (12-bit x and y). The protocol is from LilyGo's SensorLib
// (TouchClassCST226).
#include "touch.h"

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define PIN_SDA 6
#define PIN_SCL 7
#define PIN_RST 17
#define ADDR 0x5A

static const char *TAG = "touch";
static i2c_master_dev_handle_t dev;

static esp_err_t write2(uint8_t a, uint8_t b)
{
	uint8_t buf[2] = {a, b};
	return i2c_master_transmit(dev, buf, 2, 50);
}

bool touch_init(void)
{
	i2c_master_bus_config_t bus_cfg = {
		.i2c_port = I2C_NUM_0,
		.sda_io_num = PIN_SDA,
		.scl_io_num = PIN_SCL,
		.clk_source = I2C_CLK_SRC_DEFAULT,
		.glitch_ignore_cnt = 7,
		.flags.enable_internal_pullup = true,
	};
	i2c_master_bus_handle_t bus;
	ESP_ERROR_CHECK(i2c_new_master_bus(&bus_cfg, &bus));
	i2c_device_config_t dev_cfg = {
		.dev_addr_length = I2C_ADDR_BIT_LEN_7,
		.device_address = ADDR,
		.scl_speed_hz = 400000,
	};
	ESP_ERROR_CHECK(i2c_master_bus_add_device(bus, &dev_cfg, &dev));

	gpio_config_t out = {.pin_bit_mask = 1ULL << PIN_RST, .mode = GPIO_MODE_OUTPUT};
	ESP_ERROR_CHECK(gpio_config(&out));
	gpio_set_level(PIN_RST, 0);
	vTaskDelay(pdMS_TO_TICKS(100));
	gpio_set_level(PIN_RST, 1);
	vTaskDelay(pdMS_TO_TICKS(100));

	// Ask for its firmware check code ("CACA" in the top half) to know it's there.
	uint8_t cmd[2] = {0xD1, 0xFC}, code[4] = {0};
	bool ok = write2(0xD1, 0x01) == ESP_OK; // command mode
	vTaskDelay(pdMS_TO_TICKS(10));
	ok = ok && i2c_master_transmit_receive(dev, cmd, 2, code, 4, 50) == ESP_OK;
	write2(0xD1, 0x09); // back to reporting touches
	if (!ok || code[3] != 0xCA || code[2] != 0xCA) {
		ESP_LOGW(TAG, "no CST226 (check code %02x%02x%02x%02x)", code[3], code[2], code[1], code[0]);
		return false;
	}
	ESP_LOGI(TAG, "CST226 ready");
	return true;
}

bool touch_read(int *x, int *y)
{
	uint8_t r = 0x00, b[28];
	if (i2c_master_transmit_receive(dev, &r, 1, b, sizeof b, 20) != ESP_OK)
		return false;
	if (b[6] != 0xAB || b[0] == 0xAB || b[5] == 0x80)
		return false;
	int points = b[5] & 0x7F;
	if (points == 0 || points > 5) {
		write2(0x00, 0xAB); // clear a bad report
		return false;
	}
	*x = (b[1] << 4) | (b[3] >> 4);
	*y = (b[2] << 4) | (b[3] & 0x0F);
	return true;
}
