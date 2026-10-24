#!/usr/bin/env python3
"""The square boards' text sizes of Roboto Mono, for src/font_square.h.

Usage: .venv/bin/python tools/make_font_square.py

Sizes from the square boards' spec (px = cell / 0.6):
  FONT_Q16   cell 10, 16.7 px, 500   labels on the 4"
  FONT_Q22   cell 13, 21.7 px, 500   body text on the AMOLED
  FONT_Q24B  cell 14, 23.3 px, 700   questions, headlines
  FONT_Q28B  cell 17, 28.3 px, 700   values, the AMOLED's names
FONT_XL (20 px) from font_data.h and FONT_R30B (30 px bold) from font_round.h
complete the set. font_square.h is checked in; run this only to change sizes.
"""
import argparse
from pathlib import Path

import make_font

FONTS = [
    ("Q16", 10, 500),
    ("Q22", 13, 500),
    ("Q24B", 14, 700),
    ("Q28B", 17, 700),
]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--ttf", type=Path, default=make_font.TTF)
    args = ap.parse_args()
    make_font.TTF = args.ttf
    make_font.OUT = make_font.ROOT / "src/font_square.h"
    fonts = [make_font.render(*f) for f in FONTS]
    make_font.write_header(fonts)
    # write_header() declares struct Font; font_data.h has it already.
    text = make_font.OUT.read_text()
    start = text.index("// Fixed-cell font.")
    end = text.index("};", start) + 3
    text = text[:start] + '#include "font_data.h"   // struct Font\n' + text[end:]
    make_font.OUT.write_text(text)
    for f in fonts:
        print(f"FONT_{f['name']}: cell {f['w']}x{f['h']}, {len(f['codes'])} glyphs")


if __name__ == "__main__":
    main()
