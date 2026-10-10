#!/usr/bin/env python3
"""Join PPM frames from sim/render into one PNG sheet.

Usage: .venv/bin/python tools/ppm2png.py out/sheet.png out/frames/hero-*.ppm [--cols 3] [--scale 2]
"""
import argparse
from pathlib import Path

from PIL import Image


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("output", type=Path)
    ap.add_argument("frames", nargs="+", type=Path)
    ap.add_argument("--cols", type=int, default=3)
    ap.add_argument("--scale", type=int, default=1)
    ap.add_argument("--gap", type=int, default=6)
    args = ap.parse_args()

    images = [Image.open(p).convert("RGB") for p in args.frames]
    w, h = images[0].size
    cols = min(args.cols, len(images))
    rows = (len(images) + cols - 1) // cols
    sheet = Image.new("RGB", (cols * w + (cols + 1) * args.gap, rows * h + (rows + 1) * args.gap), (60, 60, 60))
    for i, im in enumerate(images):
        sheet.paste(im, (args.gap + (i % cols) * (w + args.gap), args.gap + (i // cols) * (h + args.gap)))
    if args.scale > 1:
        sheet = sheet.resize((sheet.width * args.scale, sheet.height * args.scale), Image.NEAREST)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    sheet.save(args.output)
    print(f"{args.output}: {len(images)} frames, {sheet.width}x{sheet.height}")


if __name__ == "__main__":
    main()
