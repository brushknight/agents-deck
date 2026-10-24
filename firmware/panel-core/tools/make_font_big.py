#!/usr/bin/env python3
"""The big panels' text sizes of Roboto Mono, for src/font_big.h.

Usage: .venv/bin/python tools/make_font_square.py

Sizes for the big panel layout (layout_big.h): the Flutter desk panel's type
at 2/3 for a 480 x 480 board (px = cell / 0.6):
  FONT_B20   cell 12, 20 px, 700   the card's title (30 px at 720)
  FONT_B15   cell  9, 15 px, 700   context, values, questions (22-24 px at 720)
  FONT_M13   cell  8, 13.3 px, 500 doing now, buttons (19-20 px at 720)
FONT_S (11.7 px) and FONT_XS (10 px) from font_data.h complete the set.
font_big.h is checked in; run this only to change sizes.
"""
import argparse
from pathlib import Path

import make_font

FONTS = [
    ("B20", 12, 700),
    ("B15", 9, 700),
    ("M13", 8, 500),
]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--ttf", type=Path, default=make_font.TTF)
    args = ap.parse_args()
    make_font.TTF = args.ttf
    make_font.OUT = make_font.ROOT / "src/font_big.h"
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
