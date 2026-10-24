#include <assert.h>
#include <string.h>
#include <vector>
#include "canvas.h"

static const uint16_t BG = rgb565(10, 10, 10), FG = rgb565(232, 228, 220), GUARD = 0xDEAD;
static const int W = 64, H = 24, PAD = 512;

struct Board {
  std::vector<uint16_t> mem;
  Canvas c;
  Board() : mem(W * H + 2 * PAD, GUARD), c{mem.data() + PAD, W, H} { c.fill(BG); }
  bool guardsIntact() const {
    for (int i = 0; i < PAD; i++)
      if (mem[i] != GUARD || mem[PAD + W * H + i] != GUARD) return false;
    return true;
  }
  int inked() const {
    int n = 0;
    for (int i = 0; i < W * H; i++) n += c.px[i] != BG;
    return n;
  }
  // Rightmost column that has ink, -1 when blank.
  int right() const {
    for (int x = W - 1; x >= 0; x--)
      for (int y = 0; y < H; y++)
        if (c.px[y * W + x] != BG) return x;
    return -1;
  }
};

int main() {
  assert(rgb565(255, 255, 255) == 0xFFFF && rgb565(0, 0, 0) == 0 && rgb565(255, 0, 0) == 0xF800);
  assert(blend565(BG, FG, 15) == FG && blend565(BG, FG, 0) == BG);
  assert(blend565(0x0000, 0xFFFF, 8) != 0x0000 && blend565(0x0000, 0xFFFF, 8) != 0xFFFF);

  // utf8
  {
    const char* s = "a\xD0\x96\xE2\x80\xA6!";       // a Ж … !
    const char* p = s;
    assert(utf8Next(p) == 'a' && utf8Next(p) == 0x0416 && utf8Next(p) == 0x2026 && utf8Next(p) == '!');
    assert(*p == '\0' && utf8Len(s) == 4);
    const char* bad = "\xD0!";                       // lead byte without its continuation
    p = bad;
    assert(utf8Next(p) == 0xFFFD && utf8Next(p) == '!');
    assert(utf8Len("\xFF\xFF") == 2);
  }

  // Everything clips.
  {
    Board b;
    b.c.rect(-100, -100, 1000, 1000, FG);
    assert(b.inked() == W * H && b.guardsIntact());
    b.c.fill(BG);
    b.c.rect(60, 20, 100, 100, FG);
    assert(b.inked() == 4 * 4);
    b.c.rect(10, 10, -5, 3, FG);                     // negative size draws nothing
    assert(b.inked() == 16);
    b.c.frame(-3, -3, 200, 200, FG, 2);
    b.c.line(-500, -40, 500, 90, FG, 3);
    b.c.line(5, 5, 5, 5, FG);
    b.c.text(FONT_L, -20, -10, "clipped on every side", FG);
    b.c.text(FONT_S, 50, 15, "\xD0\x96\xD0\x96\xD0\x96\xD0\x96\xD0\x96", FG);
    b.c.glyph(FONT_L, 1000, 1000, 'x', FG);
    assert(b.guardsIntact());
  }

  // frame draws an outline, not a filled box
  {
    Board b;
    b.c.frame(2, 2, 10, 8, FG);
    assert(b.inked() == 2 * 10 + 2 * 6);
    assert(b.c.px[3 * W + 3] == BG && b.c.px[2 * W + 2] == FG && b.c.px[9 * W + 11] == FG);
  }

  // text width and cutting
  {
    Board b;
    const int cw = FONT_S.cellW;
    assert(b.c.text(FONT_S, 0, 0, "abc", FG) == 3 * cw);
    assert(b.c.text(FONT_S, 0, 0, "", FG) == 0);
    assert(b.c.text(FONT_S, 0, 0, "\xD0\x9F\xD1\x80\xD0\xB8", FG) == 3 * cw);   // "При"

    Board cutEnd;
    assert(cutEnd.c.text(FONT_S, 0, 0, "MMMMMMMMMM", FG, 5 * cw) == 5 * cw);
    assert(cutEnd.right() < 5 * cw);                 // nothing past the limit
    Board fits;
    assert(fits.c.text(FONT_S, 0, 0, "MMMMM", FG, 5 * cw) == 5 * cw);
    // The cut text ends with an ellipsis, so its last cell differs from an 'M' cell.
    bool differs = false;
    for (int y = 0; y < H && !differs; y++)
      for (int x = 4 * cw; x < 5 * cw; x++)
        if (cutEnd.c.px[y * W + x] != fits.c.px[y * W + x]) { differs = true; break; }
    assert(differs);

    Board tail;
    assert(tail.c.text(FONT_S, 0, 0, "abcdefghij", FG, 5 * cw, true) == 5 * cw);
    Board tailRef;
    tailRef.c.text(FONT_S, 0, 0, "\xE2\x80\xA6ghij", FG);
    assert(memcmp(tail.c.px, tailRef.c.px, W * H * sizeof(uint16_t)) == 0);

    Board none;
    assert(none.c.text(FONT_S, 0, 0, "abc", FG, cw - 1) == 0 && none.inked() == 0);
    assert(none.c.text(FONT_S, 0, 0, "abc", FG, 0) == 0);
  }

  // An unknown code point is drawn as '?'.
  {
    Board a, q;
    a.c.text(FONT_L, 0, 0, "\xE6\x97\xA5", FG);      // 日
    q.c.text(FONT_L, 0, 0, "?", FG);
    assert(a.inked() > 0 && memcmp(a.c.px, q.c.px, W * H * sizeof(uint16_t)) == 0);
  }

  // The fonts cover what the screen prints.
  for (const Font* f : {&FONT_S, &FONT_L}) {
    for (uint32_t cp = 0x20; cp < 0x7F; cp++) assert(Canvas::glyphIndex(*f, cp) >= 0);
    for (uint32_t cp : {0x0410u, 0x044Fu, 0x0401u, 0x0451u, 0x00B7u, 0x2026u})
      assert(Canvas::glyphIndex(*f, cp) >= 0);
    for (int i = 1; i < f->count; i++) assert(f->codes[i - 1] < f->codes[i]);
  }
  return 0;
}
