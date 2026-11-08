// German text must not silently render as the unknown-glyph question mark.
#include <assert.h>
#include <string.h>
#include <vector>
#include "canvas.h"
#include "font_big.h"
#include "font_square.h"
#include "font_round.h"

static std::vector<uint16_t> render(const Font& f, const char* text) {
  std::vector<uint16_t> pixels(256 * 64);
  Canvas c{pixels.data(), 256, 64};
  c.text(f, 0, 0, text, 0xFFFF);
  return pixels;
}

int main() {
  const Font* fonts[] = {&FONT_XS, &FONT_S, &FONT_L, &FONT_XL, &FONT_XXL,
    &FONT_B20, &FONT_B15, &FONT_M13, &FONT_Q16, &FONT_Q22, &FONT_Q24B, &FONT_Q28B,
    &FONT_R30, &FONT_R30B, &FONT_R35};
  const uint32_t umlauts[] = {0x00C4, 0x00D6, 0x00DC, 0x00E4, 0x00F6, 0x00FC};
  for (const Font* f : fonts) {
    for (uint32_t cp : umlauts) assert(Canvas::glyphIndex(*f, cp) >= 0);
    // The complete Latin-1 alphabet is covered, including common French accents.
    for (uint32_t cp = 0x00C0; cp <= 0x00FF; ++cp) assert(Canvas::glyphIndex(*f, cp) >= 0);
    for (int i = 1; i < f->count; ++i) assert(f->codes[i - 1] < f->codes[i]);
    assert(render(*f, u8"äöüÄÖÜ") != render(*f, "??????"));
    assert(render(*f, u8"prüfen Grössen ändern") == render(*f, u8"pru\u0308fen Gro\u0308ssen a\u0308ndern"));
    assert(render(*f, u8"ÄÖÜäöü") == render(*f, u8"A\u0308O\u0308U\u0308a\u0308o\u0308u\u0308"));
    assert(render(*f, u8"äöüÄÖÜ") != render(*f, "aouAOU"));
  }
  assert(utf8Len(u8"prüfen") == 6);
  assert(utf8Len(u8"pru\u0308fen") == 6);
  // Width clipping counts displayed cells, not the separate combining mark.
  std::vector<uint16_t> a(64 * 32), b(64 * 32);
  Canvas ca{a.data(), 64, 32}, cb{b.data(), 64, 32};
  ca.text(FONT_S, 0, 0, u8"Grösse", 0xFFFF, 4 * FONT_S.cellW);
  cb.text(FONT_S, 0, 0, u8"Gro\u0308sse", 0xFFFF, 4 * FONT_S.cellW);
  assert(a == b);
}
