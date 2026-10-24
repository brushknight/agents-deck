// The T4-S3's AMOLED: an RM690B0 controller on QSPI, 450x600 in portrait.
#pragma once

#include <stdint.h>

#define DISPLAY_W 450
#define DISPLAY_H 600

// Powers the panel and runs its init sequence.
void display_init(void);

// Sets the panel's brightness (0-255).
void display_brightness(uint8_t level);

// Starts writing RGB565 pixels (big-endian, as the panel reads them) to the
// rectangle x1..x2, y1..y2 (inclusive) by DMA and returns; done(arg) runs
// (from an interrupt) once they're all sent. The pixels must stay put until
// then, and one write at a time.
void display_draw(int x1, int y1, int x2, int y2, const uint16_t *pixels, void (*done)(void *), void *arg);
