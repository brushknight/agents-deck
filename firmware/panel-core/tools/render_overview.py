#!/usr/bin/env python3
"""The device screenshots for docs/devices.md and the README, in docs/images/devices/.

Usage (from the repository's root, Python with pillow):

    python3 firmware/panel-core/tools/render_overview.py

Builds the host renderer (sim/render.cpp), draws every board's screens from the
sample fleet in docs/fixtures/state.json, and saves a few of each as PNG with the
glass's shape cut out (a circle for the round boards, rounded corners for the
others) on a transparent background. Fake data only: the fixture's agents, the
"my network" Wi-Fi name.
"""
import subprocess
from pathlib import Path

from PIL import Image, ImageDraw

CORE = Path(__file__).resolve().parent.parent
ROOT = CORE.parent.parent
OUT = CORE / "out" / "overview"
DEST = ROOT / "docs/images/devices"

# layout name for sim/render, the ms argument, the glass's shape (corner radius, or "round").
BOARDS = {
    "spring": (0, "round"),
    "spring28": (0, "round"),
    "lcd4": (300, 10),
    "amoled18": (300, 44),
    "t4s3": (300, 24),
    "freenove28": (300, 0),
    "147": (300, 18),
}

# output name: (layout, frame)
SHOTS = {
    "round-21-home": ("spring", "home-8"),
    "round-21-done": ("spring", "spring-11-selected-done"),
    "round-21-detail": ("spring", "detail"),
    "round-21-prompt": ("spring", "prompt"),
    "round-21-wifi": ("spring", "wifi-joining"),
    "round-28-home": ("spring28", "home-22"),
    "round-28-detail": ("spring28", "detail"),
    "round-28-prompt": ("spring28", "prompt-question"),
    "round-28-wifi": ("spring28", "home-wifi"),
    "lcd4-home": ("lcd4", "home"),
    "lcd4-card": ("lcd4", "card"),
    "lcd4-permission": ("lcd4", "permission"),
    "lcd4-wifi": ("lcd4", "wifi-joining"),
    "amoled18-home": ("amoled18", "home"),
    "amoled18-card": ("amoled18", "card"),
    "amoled18-permission": ("amoled18", "permission"),
    "amoled18-wifi": ("amoled18", "wifi-joining"),
    "t4s3-home": ("t4s3", "home"),
    "t4s3-card": ("t4s3", "card"),
    "t4s3-question": ("t4s3", "question"),
    "t4s3-wifi": ("t4s3", "wifi-joining"),
    "freenove28-home": ("freenove28", "home"),
    "freenove28-card": ("freenove28", "card"),
    "freenove28-question": ("freenove28", "question"),
    "freenove28-wifi": ("freenove28", "wifi-joining"),
    "lcd147-waiting": ("147", "auto"),
    "lcd147-running": ("147", "hero-00"),
    "lcd147-approval": ("147", "notice-unapproved"),
}


def build() -> Path:
    OUT.mkdir(parents=True, exist_ok=True)
    exe = OUT / "render"
    subprocess.run(
        ["c++", "-std=c++17", "-O2", "-Isrc", "-I../deck-link/src", "-I../deck-link/src/proto",
         "-DPANEL_MAX_AGENTS=64", "-DPANEL_OPTION_LABEL=96", "-DPANEL_LAST_PROMPT=160",
         "sim/render.cpp", "-o", str(exe)], cwd=CORE, check=True)
    for layout, (ms, _) in BOARDS.items():
        d = OUT / layout
        d.mkdir(exist_ok=True)
        subprocess.run([str(exe), str(ROOT / "docs/fixtures/state.json"), str(d), str(ms), layout],
                       cwd=CORE, check=True, stdout=subprocess.DEVNULL)
    return OUT


def mask(size, shape) -> Image.Image:
    # Drawn at 4x and scaled down, for a smooth edge.
    s = 4
    m = Image.new("L", (size[0] * s, size[1] * s), 0)
    d = ImageDraw.Draw(m)
    box = (0, 0, size[0] * s - 1, size[1] * s - 1)
    if shape == "round":
        d.ellipse(box, fill=255)
    else:
        d.rounded_rectangle(box, radius=shape * s, fill=255)
    return m.resize(size, Image.LANCZOS)


def main() -> None:
    out = build()
    DEST.mkdir(parents=True, exist_ok=True)
    for name, (layout, frame) in SHOTS.items():
        im = Image.open(out / layout / f"{frame}.ppm").convert("RGB")
        im.putalpha(mask(im.size, BOARDS[layout][1]))
        path = DEST / f"{name}.png"
        im.save(path, optimize=True)
        print(f"{path.relative_to(ROOT)}: {im.width}x{im.height}")


if __name__ == "__main__":
    main()
