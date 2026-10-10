#!/usr/bin/env python3
"""Talk to the board over USB serial: save its framebuffer as PNG, or print its status.

Usage: .venv/bin/python tools/capture.py out/board.png [--scale 2] [--port /dev/cu.usbmodem101]
       .venv/bin/python tools/capture.py --status
"""
import argparse
import glob
import json
import sys
import time
from pathlib import Path

import serial

W, H = 320, 172


def find_port(explicit: str | None) -> str:
    if explicit:
        return explicit
    ports = sorted(glob.glob("/dev/cu.usbmodem*"))
    if not ports:
        sys.exit("no /dev/cu.usbmodem* port: is the board connected?")
    return ports[0]


def open_port(name: str) -> serial.Serial:
    # The chip's USB-Serial/JTAG resets when RTS is high while DTR is low.
    # macOS raises both on open. pyserial would then drop DTR first and pass
    # through exactly that state, so drop RTS on open and DTR afterwards.
    port = serial.Serial(port=None, baudrate=115200, timeout=1)
    port.rts = False
    port.port = name
    port.open()
    port.dtr = False
    return port


def ask(port: serial.Serial, cmd: str, key: str, seconds: float = 8) -> dict:
    port.reset_input_buffer()
    port.write((json.dumps({"cmd": cmd}, separators=(",", ":")) + "\n").encode())
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            reply = json.loads(port.readline())
        except (ValueError, UnicodeError):
            continue
        if isinstance(reply, dict) and key in reply:
            return reply
    sys.exit(f"no answer to {cmd!r} from the board")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("output", nargs="?", type=Path)
    ap.add_argument("--status", action="store_true")
    ap.add_argument("--scale", type=int, default=1)
    ap.add_argument("--port")
    args = ap.parse_args()
    if not args.status and not args.output:
        ap.error("give an output file or --status")

    with open_port(find_port(args.port)) as port:
        if args.status:
            print(json.dumps(ask(port, "status", "fw"), indent=2, ensure_ascii=False))
        if args.output:
            header = ask(port, "frame", "frame_bytes")
            want = header["frame_bytes"]
            data = bytearray()
            deadline = time.monotonic() + 15
            while len(data) < want and time.monotonic() < deadline:
                data.extend(port.read(want - len(data)))
            if len(data) != W * H * 2:
                sys.exit(f"incomplete framebuffer: {len(data)} of {W * H * 2} bytes")

    if args.output:
        from PIL import Image

        pixels = []
        for i in range(0, len(data), 2):
            c = data[i] | data[i + 1] << 8
            pixels.append(((c >> 11 & 31) * 255 // 31, (c >> 5 & 63) * 255 // 63, (c & 31) * 255 // 31))
        im = Image.new("RGB", (W, H))
        im.putdata(pixels)
        if args.scale > 1:
            im = im.resize((W * args.scale, H * args.scale), Image.NEAREST)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        im.save(args.output)
        print(f"saved {args.output}")


if __name__ == "__main__":
    main()
