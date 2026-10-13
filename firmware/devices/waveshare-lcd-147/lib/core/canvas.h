#pragma once
// Drawing into a plain RGB565 framebuffer. Every primitive clips, so callers
// may draw partly or fully outside the canvas.
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

#include "font_data.h"

constexpr uint16_t rgb565(uint8_t r, uint8_t g, uint8_t b) {
  return (uint16_t)(((r & 0xF8) << 8) | ((g & 0xFC) << 3) | (b >> 3));
}

// Mix `fg` over `bg` with a 4-bit alpha (0 = bg, 15 = fg).
static inline uint16_t blend565(uint16_t bg, uint16_t fg, uint8_t a) {
  if (a >= 15) return fg;
  if (a == 0) return bg;
  const uint32_t na = 15 - a;
  const uint32_t r = (((bg >> 11) & 31) * na + ((fg >> 11) & 31) * a + 7) / 15;
  const uint32_t g = (((bg >> 5) & 63) * na + ((fg >> 5) & 63) * a + 7) / 15;
  const uint32_t b = ((bg & 31) * na + (fg & 31) * a + 7) / 15;
  return (uint16_t)(r << 11 | g << 5 | b);
}

// Decodes one code point and advances `p`. A malformed byte yields U+FFFD.
static inline uint32_t utf8Next(const char*& p) {
  const unsigned char c = (unsigned char)*p++;
  if (c < 0x80) return c;
  int extra = (c & 0xE0) == 0xC0 ? 1 : (c & 0xF0) == 0xE0 ? 2 : (c & 0xF8) == 0xF0 ? 3 : -1;
  if (extra < 0) return 0xFFFD;
  uint32_t cp = c & (0x3F >> extra);
  for (int i = 0; i < extra; i++) {
    const unsigned char n = (unsigned char)*p;
    if ((n & 0xC0) != 0x80) return 0xFFFD;
    cp = cp << 6 | (n & 0x3F);
    p++;
  }
  return cp;
}

static inline int utf8Len(const char* s) {
  int n = 0;
  while (*s) { utf8Next(s); n++; }
  return n;
}

struct Canvas {
  uint16_t* px;
  int w, h;

  void fill(uint16_t c) {
    for (int i = 0; i < w * h; i++) px[i] = c;
  }

  void rect(int x, int y, int rw, int rh, uint16_t c) {
    int x1 = x + rw, y1 = y + rh;
    if (x < 0) x = 0;
    if (y < 0) y = 0;
    if (x1 > w) x1 = w;
    if (y1 > h) y1 = h;
    for (int yy = y; yy < y1; yy++)
      for (int xx = x; xx < x1; xx++) px[yy * w + xx] = c;
  }

  // Outline of thickness t, drawn inside the given box.
  void frame(int x, int y, int fw, int fh, uint16_t c, int t = 1) {
    rect(x, y, fw, t, c);
    rect(x, y + fh - t, fw, t, c);
    rect(x, y + t, t, fh - 2 * t, c);
    rect(x + fw - t, y + t, t, fh - 2 * t, c);
  }

  // Bresenham line with a t-by-t square brush whose top-left corner follows the line.
  void line(int x0, int y0, int x1, int y1, uint16_t c, int t = 1) {
    const int dx = abs(x1 - x0), sx = x0 < x1 ? 1 : -1;
    const int dy = -abs(y1 - y0), sy = y0 < y1 ? 1 : -1;
    int err = dx + dy;
    for (int guard = 0; guard < 4096; guard++) {
      rect(x0, y0, t, t, c);
      if (x0 == x1 && y0 == y1) break;
      const int e2 = 2 * err;
      if (e2 >= dy) { err += dy; x0 += sx; }
      if (e2 <= dx) { err += dx; y0 += sy; }
    }
  }

  static int glyphIndex(const Font& f, uint32_t cp) {
    int lo = 0, hi = f.count - 1;
    while (lo <= hi) {
      const int mid = (lo + hi) / 2;
      if (f.codes[mid] == cp) return mid;
      if (f.codes[mid] < cp) lo = mid + 1; else hi = mid - 1;
    }
    return -1;
  }

  // One glyph with its cell's top-left corner at (x, y). Unknown code points draw '?'.
  void glyph(const Font& f, int x, int y, uint32_t cp, uint16_t c) {
    int gi = glyphIndex(f, cp);
    if (gi < 0) gi = glyphIndex(f, '?');
    if (gi < 0) return;
    const int stride = (f.cellW + 1) / 2;
    const uint8_t* g = f.bits + (size_t)gi * stride * f.cellH;
    for (int gy = 0; gy < f.cellH; gy++) {
      const int yy = y + gy;
      if (yy < 0 || yy >= h) continue;
      for (int gx = 0; gx < f.cellW; gx++) {
        const int xx = x + gx;
        if (xx < 0 || xx >= w) continue;
        const uint8_t b = g[gy * stride + gx / 2];
        const uint8_t a = (gx & 1) ? (b & 0x0F) : (b >> 4);
        if (a) px[yy * w + xx] = blend565(px[yy * w + xx], c, a);
      }
    }
  }

  // UTF-8 text, top-left at (x, y). With maxW >= 0 the text is cut to fit and
  // ends with "…". `keepTail` cuts the start instead (useful for paths).
  // Returns the width drawn in pixels.
  int text(const Font& f, int x, int y, const char* s, uint16_t c, int maxW = -1, bool keepTail = false) {
    const int n = utf8Len(s);
    int room = maxW < 0 ? n : maxW / f.cellW;
    if (room <= 0 || n == 0) return 0;
    const bool cut = n > room;
    int skip = 0, take = n;
    if (cut) {
      take = room - 1;
      if (keepTail) skip = n - take;
    }
    int cx = x;
    if (cut && keepTail) { glyph(f, cx, y, 0x2026, c); cx += f.cellW; }
    for (int i = 0; i < skip; i++) utf8Next(s);
    for (int i = 0; i < take; i++) { glyph(f, cx, y, utf8Next(s), c); cx += f.cellW; }
    if (cut && !keepTail) { glyph(f, cx, y, 0x2026, c); cx += f.cellW; }
    return cx - x;
  }
};
