#!/usr/bin/env python3
"""The round boards' larger sizes of Roboto Mono, for src/font_round.h.

Usage: .venv/bin/python tools/make_font_round.py

The round layouts never draw text under 26 px: they use FONT_XXL (26.7 px)
from font_data.h for body text, and these for options and titles:

  FONT_R30   cell 18 px wide, 30 px, weight 500   option bands, notices
  FONT_R30B  cell 18 px wide, 30 px, weight 700   the orbit's middle title, the primary option
  FONT_R35   cell 21 px wide, 35 px, weight 700   titles

Same glyph set and format as make_font.py (which this reuses). font_round.h
is checked in; run this only to change the sizes.
"""
import argparse
from pathlib import Path

import make_font

FONTS = [
    ("R30", 18, 500),
    ("R30B", 18, 700),
    ("R35", 21, 700),
]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--ttf", type=Path, default=make_font.TTF)
    args = ap.parse_args()
    make_font.TTF = args.ttf
    make_font.OUT = make_font.ROOT / "src/font_round.h"
    fonts = [make_font.render(*f) for f in FONTS]
    make_font.write_header(fonts)
    # write_header() declares struct Font; font_data.h has it already.
    text = make_font.OUT.read_text()
    start = text.index("// Fixed-cell font.")
    end = text.index("};", start) + 3
    text = text[:start] + '#include "font_data.h"   // struct Font\n' + text[end:]
    text = text.replace("make_font.py", "make_font_round.py", 1)
    make_font.OUT.write_text(text)
    for f in fonts:
        print(f"FONT_{f['name']}: cell {f['w']}x{f['h']}, {len(f['codes'])} glyphs, {len(f['bits'])} bytes")


if __name__ == "__main__":
    main()
