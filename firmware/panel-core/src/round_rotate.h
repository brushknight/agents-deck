#pragma once
// Turning a square frame for a panel mounted at an angle, and turning the
// touch back. `deg` (0, 90, 180 or 270) is how far the image is turned
// clockwise on its way to the panel; the touch controller reports in the
// panel's own axes, so a touch is turned back by the same angle.
//
// A board sets it with -DROUND_ROTATION=<deg> (default 0, as Waveshare's
// demos). Pure C++: the host tests check the round trip.
#include <stdint.h>
#include <string.h>

#ifndef ROUND_ROTATION
#define ROUND_ROTATION 0
#endif
static_assert(ROUND_ROTATION == 0 || ROUND_ROTATION == 90 || ROUND_ROTATION == 180 || ROUND_ROTATION == 270,
              "ROUND_ROTATION must be 0, 90, 180 or 270");

// Where frame pixel (x, y) lands on an n x n panel.
static inline void rotateToPanel(int deg, int n, int x, int y, int& px, int& py) {
  switch (deg) {
    case 90:  px = n - 1 - y; py = x; break;
    case 180: px = n - 1 - x; py = n - 1 - y; break;
    case 270: px = y; py = n - 1 - x; break;
    default:  px = x; py = y; break;
  }
}

// Which frame pixel a touch at panel pixel (px, py) is on.
static inline void rotateToFrame(int deg, int n, int px, int py, int& x, int& y) {
  switch (deg) {
    case 90:  x = py; y = n - 1 - px; break;
    case 180: x = n - 1 - px; y = n - 1 - py; break;
    case 270: x = n - 1 - py; y = px; break;
    default:  x = px; y = py; break;
  }
}

// Copies an n x n RGB565 frame onto the panel's framebuffer, turned.
static inline void rotateFrame(int deg, int n, const uint16_t* src, uint16_t* dst) {
  const int total = n * n;
  switch (deg) {
    case 180:
      for (int i = 0; i < total; i++) dst[total - 1 - i] = src[i];
      return;
    case 90:
      // Frame row y becomes panel column n-1-y, top to bottom.
      for (int y = 0; y < n; y++) {
        const uint16_t* row = src + y * n;
        uint16_t* col = dst + (n - 1 - y);
        for (int x = 0; x < n; x++) col[x * n] = row[x];
      }
      return;
    case 270:
      // Frame row y becomes panel column y, bottom to top.
      for (int y = 0; y < n; y++) {
        const uint16_t* row = src + y * n;
        uint16_t* col = dst + y + (n - 1) * n;
        for (int x = 0; x < n; x++) col[-x * n] = row[x];
      }
      return;
    default:
      memcpy(dst, src, (size_t)total * 2);
      return;
  }
}
