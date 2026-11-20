#pragma once
// A faster frame send for the QSPI AMOLEDs (the T4-S3's RM690B0, the 1.8"'s
// CO5300) than Arduino_GFX's writePixels.
//
// Arduino_GFX sends a frame in chunks of 1024 pixels: it byte-swaps one into
// its DMA buffer, sends it and waits, then the next, so the bus idles while
// the CPU swaps and the CPU idles while the bus sends (a 450 x 600 frame:
// 70 ms at 40 MHz, where the bus alone needs 27). Here two buffers take
// turns: the next chunk is swapped while the last one is on the wire, and the
// chunks are four times larger (fewer transactions).
//
// It sends on Arduino_GFX's own SPI device (the bus is acquired for good when
// it isn't shared), so the board's main.cpp includes Arduino_GFX_Library.h
// with its private members visible (#define private public around it) to reach the
// device handle and the chip-select pin. A frame goes out as the panel's RAM
// write (0x32 with 0x2C), into the window set just before.
#include <driver/spi_master.h>
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <string.h>

namespace qspi_fast {

constexpr uint32_t kChunkPx = 4096;   // 8 KB; the bus takes up to 8192 (max_transfer_sz)

static uint32_t* buf[2] = {nullptr, nullptr};
static uint32_t swapUs = 0;            // time spent swapping (the debug report)
// The frame is already in the panel's byte order (Canvas::be): a chunk is a
// plain copy, a third of the time a swap takes on this CPU.
static bool preswapped = false;
// A transfer that didn't start or didn't finish in time: the send gives up on
// the fast way for good (the board falls back to Arduino_GFX), instead of
// waiting forever with the screen frozen. failures counts them (debug report).
static bool broken = false;
static uint32_t failures = 0;
constexpr TickType_t kWait = pdMS_TO_TICKS(200);   // a chunk takes well under 1 ms

static bool waitOne(spi_device_handle_t h) {
  spi_transaction_t* done = nullptr;
  if (spi_device_get_trans_result(h, &done, kWait) == ESP_OK) return true;
  broken = true;
  failures++;
  return false;
}
static spi_transaction_ext_t tr[2];

// The two buffers, in internal RAM (DMA). False: not enough memory (the
// board then sends with Arduino_GFX as before).
static bool begin() {
  for (auto& b : buf)
    if (!b) b = (uint32_t*)heap_caps_aligned_alloc(16, kChunkPx * 2, MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
  return buf[0] && buf[1];
}

// The clock the SPI driver really runs the panel at, in kHz.
template <typename Bus>
static int actualKhz(Bus* bus) {
  int khz = 0;
  spi_device_get_actual_freq(bus->_handle, &khz);
  return khz;
}

// Two RGB565 pixels a word, each byte-swapped: the panel takes them high byte first.
static inline void swapInto(uint32_t* dst, const uint32_t* src, uint32_t words) {
  for (uint32_t i = 0; i < words; i++) {
    const uint32_t w = src[i];
    dst[i] = ((w & 0x00FF00FFu) << 8) | ((w >> 8) & 0x00FF00FFu);
  }
}

// `len` pixels (an even number, from a 4-byte aligned buffer) to the panel's
// RAM, into the window the display set. `Bus` is Arduino_ESP32QSPI.
template <typename Bus>
static void pixels(Bus* bus, const uint16_t* data, uint32_t len) {
  *bus->_csPortClr = bus->_csPinMask;   // chip select low for the whole frame
  int inflight = 0, k = 0;
  bool first = true;
  while (len) {
    const uint32_t n = len > kChunkPx ? kChunkPx : len;
    if (inflight == 2) {                 // the older one (this buffer) is done before it is filled again
      if (!waitOne(bus->_handle)) break;
      inflight--;
    }
    const uint32_t s0 = esp_timer_get_time();
    if (preswapped) memcpy(buf[k], data, n * 2);
    else swapInto(buf[k], (const uint32_t*)data, n / 2);
    swapUs += (uint32_t)esp_timer_get_time() - s0;
    spi_transaction_ext_t& t = tr[k];
    memset(&t, 0, sizeof t);
    if (first) {                         // the RAM write command, then pixels
      t.base.flags = SPI_TRANS_MODE_QIO;
      t.base.cmd = 0x32;
      t.base.addr = 0x003C00;
      first = false;
    } else {                             // more pixels: no command, address or dummy
      t.base.flags = SPI_TRANS_MODE_QIO | SPI_TRANS_VARIABLE_CMD | SPI_TRANS_VARIABLE_ADDR | SPI_TRANS_VARIABLE_DUMMY;
    }
    t.base.tx_buffer = buf[k];
    t.base.length = n * 16;
    if (spi_device_queue_trans(bus->_handle, &t.base, kWait) != ESP_OK) {
      broken = true;
      failures++;
      break;
    }
    inflight++;
    data += n;
    len -= n;
    k ^= 1;
  }
  while (inflight > 0 && waitOne(bus->_handle)) inflight--;
  *bus->_csPortSet = bus->_csPinMask;
}

// The same for a frame given as one pointer per row (w pixels each, w even):
// whole rows to a chunk, each row swapped from wherever it is.
template <typename Bus>
static void rows(Bus* bus, const uint16_t* const* rowPtr, uint32_t w, uint32_t h) {
  const uint32_t perChunk = kChunkPx / w;
  *bus->_csPortClr = bus->_csPinMask;
  int inflight = 0, k = 0;
  bool first = true;
  for (uint32_t y = 0; y < h;) {
    const uint32_t nrows = h - y < perChunk ? h - y : perChunk;
    if (inflight == 2) {
      if (!waitOne(bus->_handle)) break;
      inflight--;
    }
    const uint32_t s0 = esp_timer_get_time();
    for (uint32_t r = 0; r < nrows; r++) {
      if (preswapped) memcpy(buf[k] + r * (w / 2), rowPtr[y + r], w * 2);
      else swapInto(buf[k] + r * (w / 2), (const uint32_t*)rowPtr[y + r], w / 2);
    }
    swapUs += (uint32_t)esp_timer_get_time() - s0;
    spi_transaction_ext_t& t = tr[k];
    memset(&t, 0, sizeof t);
    if (first) {
      t.base.flags = SPI_TRANS_MODE_QIO;
      t.base.cmd = 0x32;
      t.base.addr = 0x003C00;
      first = false;
    } else {
      t.base.flags = SPI_TRANS_MODE_QIO | SPI_TRANS_VARIABLE_CMD | SPI_TRANS_VARIABLE_ADDR | SPI_TRANS_VARIABLE_DUMMY;
    }
    t.base.tx_buffer = buf[k];
    t.base.length = nrows * w * 16;
    if (spi_device_queue_trans(bus->_handle, &t.base, kWait) != ESP_OK) {
      broken = true;
      failures++;
      break;
    }
    inflight++;
    y += nrows;
    k ^= 1;
  }
  while (inflight > 0 && waitOne(bus->_handle)) inflight--;
  *bus->_csPortSet = bus->_csPinMask;
}

// A one-off measure for the debug report: reading a frame from PSRAM with
// memcpy, swapping it from PSRAM, and swapping the same amount inside internal RAM.
static void bench(const uint16_t* frame, uint32_t len, uint32_t out[3]) {
  out[0] = out[1] = out[2] = 0;
  if (!buf[0]) return;
  const uint32_t words = kChunkPx / 2;
  uint32_t t0 = esp_timer_get_time();
  for (uint32_t i = 0; i + kChunkPx <= len; i += kChunkPx) memcpy(buf[0], frame + i, kChunkPx * 2);
  uint32_t t1 = esp_timer_get_time();
  for (uint32_t i = 0; i + kChunkPx <= len; i += kChunkPx) swapInto(buf[0], (const uint32_t*)(frame + i), words);
  uint32_t t2 = esp_timer_get_time();
  for (uint32_t i = 0; i + kChunkPx <= len; i += kChunkPx) swapInto(buf[0], buf[1], words);
  uint32_t t3 = esp_timer_get_time();
  out[0] = t1 - t0;
  out[1] = t2 - t1;
  out[2] = t3 - t2;
}

}  // namespace qspi_fast

// For the debug report in grid_app.h: the swapping, per 5 s.
#define QSPI_FAST_REPORT() do { uint32_t b_[3]; qspi_fast::bench(frame, (uint32_t)canvas.w * canvas.h, b_); PANEL_LOG("bench: a frame from PSRAM: memcpy %u us, swap %u us; the same swap in internal RAM %u us", (unsigned)b_[0], (unsigned)b_[1], (unsigned)b_[2]); PANEL_LOG("send: %u ms swapping in 5 s", (unsigned)(qspi_fast::swapUs / 1000)); if (qspi_fast::failures) PANEL_LOG("send: %u failed transfers, fast send off", (unsigned)qspi_fast::failures); qspi_fast::swapUs = 0; } while (0)
