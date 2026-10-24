// round_rotate.h: every angle turns the frame the way rotateToPanel says,
// and a touch where a pixel shows maps back to that pixel.
#include <assert.h>
#include <stdio.h>

#include <vector>

#include "round_rotate.h"

int main() {
  const int n = 8;
  std::vector<uint16_t> src(n * n), dst(n * n);
  for (int i = 0; i < n * n; i++) src[i] = (uint16_t)(i + 1);
  for (int deg : {0, 90, 180, 270}) {
    rotateFrame(deg, n, src.data(), dst.data());
    for (int y = 0; y < n; y++)
      for (int x = 0; x < n; x++) {
        int px, py, bx, by;
        rotateToPanel(deg, n, x, y, px, py);
        assert(px >= 0 && px < n && py >= 0 && py < n);
        assert(dst[py * n + px] == src[y * n + x]);   // the pixel lands where it says
        rotateToFrame(deg, n, px, py, bx, by);
        assert(bx == x && by == y);                    // and a touch there finds it
      }
  }
  // Clockwise: the frame's top-left corner goes to the panel's top-right at 90°.
  int px, py;
  rotateToPanel(90, 480, 0, 0, px, py);
  assert(px == 479 && py == 0);
  rotateToPanel(270, 480, 0, 0, px, py);
  assert(px == 0 && py == 479);
  // The frame's top (12 o'clock) shows at 3 o'clock on a panel turned 90°.
  rotateToPanel(90, 480, 240, 0, px, py);
  assert(px == 479 && py == 240);
  printf("round rotate: ok\n");
  return 0;
}
