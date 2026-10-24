// Development helpers, built with -DPANEL_DEBUG (env t4s3-debug): the USB
// port also takes "!snap" (the panel dumps its screen as raw RGB565 after
// "\0SNAP <w> <h>\n"), "!perf" (frames drawn and where the time went),
// "!log" (touch and scroll events, as notes), "!tap <x> <y>" (a 150 ms
// touch there) and "!drag <x1> <y1> <x2> <y2>" (a 300 ms swipe).
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef PANEL_DEBUG
// The link task passes every received chunk here first.
void debug_scan(const uint8_t *data, size_t len);
// The UI task: a pending touch, and a pending screen dump.
bool debug_touch(int *x, int *y);
void debug_poll(void);
// Counts a whole frame drawn; "!perf" answers "\0PERF <frames> <ms>\n".
void debug_frame(void);
// Keeps a short note for "!log" (answered with "\0LOG <notes>\n").
void debug_note(const char *fmt, ...);
// Time in lv_timer_handler, and the display busy sending, in microseconds.
void debug_busy(uint32_t us);
void debug_spi(uint32_t us);
#endif

// Serializes writes to the USB port (Deck Link frames and dumps).
void usb_write_lock(void);
void usb_write_unlock(void);
