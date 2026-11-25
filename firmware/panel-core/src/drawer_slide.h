#pragma once
// The board's panel (control_panel.h) while it moves, for a layout that keeps
// no pictures of its own (layout_big.h): the screen under it and the panel
// all the way down are each drawn once, into two frames kept for the motion,
// and every moving frame is cut from them (the panel's last rows above its
// edge, a 2-pixel line, the screen below). The motion then costs a copy of
// rows, or nothing on a board that sends rows (frameRows), instead of two
// drawings a frame. Pure C++ (no Arduino).
#include <math.h>
#include <stdlib.h>
#include <string.h>

#include "canvas.h"
#include "panel_layout.h"

struct DrawerSlide {
  DrawerSlide() = default;
  DrawerSlide(const DrawerSlide&) = delete;
  DrawerSlide& operator=(const DrawerSlide&) = delete;
  ~DrawerSlide() {
    free(under);
    free(panel);
    free(rowTable);
    free(edgeRow);
  }

  // The pictures are out of date (the panel stopped moving): drawn again the next time it moves.
  void reset() {
    cached = false;
    rowsReady = false;
  }

  // After draw(): the moving frame as one pointer per row, or nullptr.
  const uint16_t* const* rows() const { return rowsReady ? rowTable : nullptr; }

  // A moving frame at `open` (0 shut, 1 down) on c, or into rows() when
  // rowsOk. drawUnder(c) draws the screen; drawPanel(c) the panel all the way
  // down over it. False: no room for the pictures (draw the frame instead).
  template <class Under, class Panel>
  bool slide(Canvas& c, float open, bool rowsOk, uint16_t line, Under drawUnder, Panel drawPanel) {
    rowsReady = false;
    const int W = c.w, H = c.h;
    const size_t n = (size_t)W * H;
    if (c.top != 0) return false;
    if (W != w || H != h) {                         // a canvas of another size: start over
      free(under);
      free(panel);
      free(rowTable);
      free(edgeRow);
      under = panel = edgeRow = nullptr;
      rowTable = nullptr;
      cached = false;
      w = W;
      h = H;
    }
    if (!under) under = (uint16_t*)malloc(n * 2);
    if (!panel) panel = (uint16_t*)malloc(n * 2);
    if (!under || !panel) return false;
    if (!cached || lineColor != c.enc(line)) {
      drawUnder(c);
      memcpy(under, c.px, n * 2);
      drawPanel(c);
      memcpy(panel, c.px, n * 2);
      cached = true;
      lineColor = c.enc(line);
      if (edgeRow) for (int x = 0; x < W; x++) edgeRow[x] = lineColor;
    }
    const int edge = (int)lroundf(open * (float)H);
    if (rowsOk) {
      if (!rowTable) rowTable = (const uint16_t**)malloc(sizeof(uint16_t*) * H);
      if (!edgeRow) {
        edgeRow = (uint16_t*)malloc((size_t)W * 2);
        if (edgeRow) for (int x = 0; x < W; x++) edgeRow[x] = lineColor;
      }
      if (rowTable && edgeRow) {
        for (int y = 0; y < H; y++) rowTable[y] = rowAt(y, edge);
        rowsReady = true;
        return true;
      }
    }
    const size_t row = (size_t)W * 2;
    for (int y = 0; y < H; y++) {
      uint16_t* dst = c.px + (size_t)y * W;
      if (y >= edge && y < edge + 2 && edge > 0 && edge < H) {
        for (int x = 0; x < W; x++) dst[x] = lineColor;
      } else {
        memcpy(dst, rowAt(y, edge), row);
      }
    }
    return true;
  }

 private:
  const uint16_t* rowAt(int y, int edge) const {
    if (y < edge) return panel + (size_t)(h - edge + y) * w;
    if (y < edge + 2 && edge > 0 && edge < h) return edgeRow;
    return under + (size_t)y * w;
  }

  uint16_t* under = nullptr;
  uint16_t* panel = nullptr;
  const uint16_t** rowTable = nullptr;
  uint16_t* edgeRow = nullptr;
  uint16_t lineColor = 0;
  int w = 0, h = 0;
  bool cached = false;
  bool rowsReady = false;
};
