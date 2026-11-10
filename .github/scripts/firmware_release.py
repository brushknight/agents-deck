#!/usr/bin/env python3
"""Firmware release images: one file per board, flashed at 0x0.

  build   build a board's env with PlatformIO, read its flash layout from the
          build (`pio project metadata`), merge bootloader, partition table,
          boot_app0 and app into one image, check it, and write an ESP Web
          Tools manifest for it
  merge   the same merge and checks from files you name (an existing build)
  index   collect the boards' manifests into firmware-manifest.json

What the deck needs to know about a board (its name, the Hello types its
firmware reports, its USB ids, flash size, Wi-Fi, picture) comes from
firmware/devices/<board>/board.json. Boards with Wi-Fi also get the app on its
own (`.app.bin`), the image the deck sends for an over-the-air update.

Standard library only, plus `esptool` (merge-bin) and `pio` on PATH. Used by
.github/workflows/release.yml; see docs/releasing.md.
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

# ESP image header chip ids (esptool's IMAGE_CHIP_ID) and ESP Web Tools' names.
CHIPS = {
    "esp32": (0, "ESP32"),
    "esp32s2": (2, "ESP32-S2"),
    "esp32c3": (5, "ESP32-C3"),
    "esp32s3": (9, "ESP32-S3"),
    "esp32c6": (13, "ESP32-C6"),
}
# Where the ROM looks for the second-stage bootloader.
BOOTLOADER_OFFSET = {"esp32": 0x1000, "esp32s2": 0x1000}
PARTITION_TABLE_OFFSET = 0x8000  # CONFIG_PARTITION_TABLE_OFFSET, the default on every build here
# esp_app_desc_t starts the app's first segment: after the 24-byte image header
# and the 8-byte segment header.
APP_DESC_OFFSET = 32
APP_DESC_MAGIC = 0xABCD5432
# Flash size in the image header (high nibble of byte 3), as esptool writes it.
FLASH_SIZES = {0: 1 << 20, 1: 2 << 20, 2: 4 << 20, 3: 8 << 20, 4: 16 << 20, 5: 32 << 20, 6: 64 << 20, 7: 128 << 20}
REPO = Path(__file__).resolve().parents[2]


def fail(msg):
    sys.exit(f"firmware_release: {msg}")


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def partitions(table):
    """The entries of a binary partition table: (label, type, subtype, offset, size)."""
    out = []
    for i in range(0, len(table), 32):
        entry = table[i : i + 32]
        magic = struct.unpack_from("<H", entry)[0] if len(entry) == 32 else 0
        if magic != 0x50AA:  # 0xEBEB is the MD5 entry, 0xFFFF the end
            break
        _, ptype, subtype, offset, size = struct.unpack_from("<HBBII", entry)
        label = entry[12:28].split(b"\0")[0].decode()
        out.append((label, ptype, subtype, offset, size))
    return out


def check_image(data, offset, chip, what):
    """An ESP app or bootloader image starts at offset: magic 0xE9 and the right chip id."""
    if len(data) < offset + 24 or data[offset] != 0xE9:
        fail(f"{what}: no ESP image at {offset:#x}")
    chip_id = struct.unpack_from("<H", data, offset + 12)[0]
    if chip_id != CHIPS[chip][0]:
        fail(f"{what}: chip id {chip_id} at {offset:#x}, expected {CHIPS[chip][0]} ({chip})")


def load_board(path):
    """firmware/devices/<board>/board.json, checked: the fields the manifest takes from it."""
    try:
        b = json.loads(Path(path).read_text())
    except (OSError, ValueError) as e:
        fail(f"{path}: {e}")
    want = {"name": str, "chip": str, "types": list, "usb": list, "flash_size": int, "wifi": bool, "picture": str}
    for key, kind in want.items():
        if not isinstance(b.get(key), kind) or isinstance(b.get(key), bool) != (kind is bool):
            fail(f"{path}: {key} must be a {kind.__name__}")
    if b["chip"] not in CHIPS:
        fail(f"{path}: unknown chip {b['chip']}")
    if not b["types"] or not all(isinstance(t, str) and t for t in b["types"]):
        fail(f"{path}: types must list the Hello types the firmware reports")
    for u in b["usb"]:
        if not (isinstance(u, dict) and all(re.fullmatch(r"[0-9a-f]{4}", str(u.get(k, ""))) for k in ("vid", "pid"))):
            fail(f"{path}: usb ids are {{\"vid\": \"303a\", \"pid\": \"1001\"}}, four lower-case hex digits each")
    if b["flash_size"] not in FLASH_SIZES.values():
        fail(f"{path}: flash_size {b['flash_size']} isn't a flash size")
    if not (REPO / b["picture"]).is_file():
        fail(f"{path}: no picture at {b['picture']}")
    return b


def check_app(path, chip, table):
    """The app on its own, as an over-the-air update writes it: an ESP image for
    this chip with an app description, small enough for every OTA slot."""
    data = Path(path).read_bytes()
    check_image(data, 0, chip, Path(path).name)
    if len(data) < APP_DESC_OFFSET + 80 or struct.unpack_from("<I", data, APP_DESC_OFFSET)[0] != APP_DESC_MAGIC:
        fail(f"{Path(path).name}: no app description (esp_app_desc_t) at the start of the first segment")
    version = data[APP_DESC_OFFSET + 16 : APP_DESC_OFFSET + 48].split(b"\0")[0].decode(errors="replace")
    project = data[APP_DESC_OFFSET + 48 : APP_DESC_OFFSET + 80].split(b"\0")[0].decode(errors="replace")
    slots = [p for p in table if p[1] == 0 and 0x10 <= p[2] <= 0x1F]  # app, ota_0 .. ota_15
    if len(slots) < 2:
        fail(f"the partition table has {len(slots)} OTA slot(s): an over-the-air update needs two")
    smallest = min(slots, key=lambda p: p[4])
    if len(data) > smallest[4]:
        fail(f"{Path(path).name} ({len(data)} bytes) doesn't fit OTA slot {smallest[0]} ({smallest[4]} bytes)")
    return {"size": len(data), "slot": smallest[4], "app_version": version, "project": project}


def merge(chip, images, out):
    """images: [(offset, path)], bootloader first. Writes out and checks the layout."""
    images = sorted(images)
    cmd = [
        sys.executable, "-m", "esptool", "--chip", chip, "merge-bin", "-o", str(out),
        # keep: the headers stay as PlatformIO wrote them (flash mode, speed and size)
        "--flash-mode", "keep", "--flash-freq", "keep", "--flash-size", "keep",
    ]
    for offset, path in images:
        cmd += [hex(offset), str(path)]
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL)

    data = Path(out).read_bytes()
    for offset, path in images:
        part = Path(path).read_bytes()
        got = data[offset : offset + len(part)]
        # With "keep", merge-bin leaves every byte as built, the bootloader header included.
        if got != part:
            fail(f"{out}: {Path(path).name} at {offset:#x} differs from the source file")
    boot_off = BOOTLOADER_OFFSET.get(chip, 0x0)
    if images[0][0] != boot_off:
        fail(f"bootloader at {images[0][0]:#x}, expected {boot_off:#x} on {chip}")
    check_image(data, boot_off, chip, "bootloader")

    table = partitions(data[PARTITION_TABLE_OFFSET : PARTITION_TABLE_OFFSET + 0xC00])
    if not table:
        fail(f"no partition table at {PARTITION_TABLE_OFFSET:#x}")
    apps = [p for p in table if p[1] == 0]
    otadata = [p for p in table if p[1] == 1 and p[2] == 0]
    app_off = images[-1][0]
    if not apps or apps[0][3] != app_off:
        fail(f"app at {app_off:#x}, but the partition table's first app is at {apps[0][3] if apps else None}")
    if os.path.getsize(images[-1][1]) > apps[0][4]:
        fail(f"app ({os.path.getsize(images[-1][1])} bytes) is bigger than its partition ({apps[0][4]})")
    if otadata and otadata[0][3] not in [o for o, _ in images]:
        fail(f"nothing written to otadata at {otadata[0][3]:#x} (boot_app0 missing)")
    check_image(data, app_off, chip, "app")
    flash_size = FLASH_SIZES.get(data[boot_off + 3] >> 4)
    return {"size": len(data), "partitions": table, "app_offset": app_off, "flash_size": flash_size}


def web_tools_manifest(name, version, chip, file):
    """https://esphome.github.io/esp-web-tools/: parts' paths are relative to the manifest."""
    return {
        "name": name,
        "version": version,
        "new_install_prompt_erase": True,
        "builds": [{"chipFamily": CHIPS[chip][1], "parts": [{"path": file, "offset": 0}]}],
    }


def board_info(args):
    """board.json (the default path or --board-json); --name and --chip must agree with it."""
    path = Path(args.board_json) if args.board_json else REPO / "firmware" / "devices" / args.board / "board.json"
    if not path.is_file():
        if args.board_json or not args.allow_no_board_json:
            fail(f"no {path}: every released board needs one (see docs/releasing.md)")
        return None
    b = load_board(path)
    if args.name and args.name != b["name"]:
        fail(f"--name {args.name!r} differs from {path}'s name {b['name']!r}")
    if args.chip and args.chip != b["chip"]:
        fail(f"--chip {args.chip} differs from {path}'s chip {b['chip']}")
    return b


def write_outputs(args, images, info):
    board = board_info(args)
    args.name = args.name or (board and board["name"])
    args.chip = args.chip or (board and board["chip"]) or "esp32s3"
    if not args.name:
        fail("--name is needed when there is no board.json")
    out_dir = Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    base = f"agents-deck-{args.board}-{args.version}"
    image = out_dir / f"{base}.bin"
    layout = merge(args.chip, images, image)
    extra = {}
    if board:
        if layout["flash_size"] != board["flash_size"]:
            fail(f"the image says {layout['flash_size']} bytes of flash, board.json {board['flash_size']}")
        end = max(p[3] + p[4] for p in layout["partitions"])
        if end > board["flash_size"]:
            fail(f"the partition table ends at {end:#x}, past the {board['flash_size']}-byte flash")
        extra = {k: board[k] for k in ("name", "types", "usb", "flash_size", "wifi", "picture")}
        extra["ota"] = None
        if board["wifi"]:  # over the air needs the network: only Wi-Fi boards get the app on its own
            app = out_dir / f"{base}.app.bin"
            shutil.copyfile(images[-1][1], app)
            got = check_app(app, args.chip, layout["partitions"])
            extra["ota"] = {"file": app.name, "size": got["size"], "sha256": sha256(app)}
            print(f"{app.name}: {got['size']} bytes of a {got['slot']}-byte OTA slot, "
                  f"app {got['project']!r} version {got['app_version']!r}")
    manifest = web_tools_manifest(args.name, args.version, args.chip, image.name)
    manifest["agents_deck"] = {  # ignored by ESP Web Tools; read by `index`
        "board": args.board,
        "version": args.version,
        "chip": args.chip,
        "file": image.name,
        "offset": "0x0",
        "size": layout["size"],
        "sha256": sha256(image),
        **info,
        **extra,
    }
    (out_dir / f"{base}.manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"{image.name}: {layout['size']} bytes, sha256 {manifest['agents_deck']['sha256']}")
    for offset, path in images:
        print(f"  {offset:#08x}  {Path(path).name}")
    for label, ptype, subtype, offset, size in layout["partitions"]:
        print(f"  partition {label:<10} type {ptype} sub {subtype:#04x} at {offset:#08x}, {size} bytes")


def cmd_build(args):
    # The firmware reports this as its version in Hello (when it is built with it).
    env = dict(os.environ, AGENTS_DECK_VERSION=args.version)
    subprocess.run(["pio", "run", "-d", args.dir, "-e", args.env], check=True, env=env)
    with tempfile.TemporaryDirectory() as tmp:
        meta_path = Path(tmp) / "metadata.json"
        subprocess.run(
            ["pio", "project", "metadata", "-d", args.dir, "-e", args.env,
             "--json-output", "--json-output-path", str(meta_path)],
            check=True, stdout=subprocess.DEVNULL, env=env,
        )
        meta = json.loads(meta_path.read_text())[args.env]
    extra = meta["extra"]
    images = [(int(i["offset"], 16), i["path"]) for i in extra["flash_images"]]
    app = Path(meta["prog_path"]).with_suffix(".bin")
    images.append((app_offset(extra, images), str(app)))
    write_outputs(args, images, {"env": args.env, "project": args.dir})


def app_offset(extra, images):
    """Where the app goes. PlatformIO's metadata says so; pioarduino's (idf5)
    doesn't, so then it is the first app partition, as the bootloader picks."""
    if "application_offset" in extra:
        return int(extra["application_offset"], 16)
    table = [p for o, p in images if o == PARTITION_TABLE_OFFSET]
    if not table:
        fail("no application_offset in the metadata and no partition table among the flash images")
    apps = [p for p in partitions(Path(table[0]).read_bytes()) if p[1] == 0]
    if not apps:
        fail(f"no app partition in {table[0]}")
    return apps[0][3]


def cmd_merge(args):
    images = []
    for spec in args.image:
        offset, _, path = spec.partition("=")
        images.append((int(offset, 16), path))
    write_outputs(args, images, {})


def cmd_index(args):
    boards = []
    for path in sorted(Path(args.dir).glob("*.manifest.json")):
        m = json.loads(path.read_text())
        d = m["agents_deck"]
        if sha256(Path(args.dir) / d["file"]) != d["sha256"]:
            fail(f"{d['file']}: sha256 doesn't match its manifest")
        ota = d.get("ota")
        if ota and sha256(Path(args.dir) / ota["file"]) != ota["sha256"]:
            fail(f"{ota['file']}: sha256 doesn't match its manifest")
        boards.append({"name": m["name"], "chipFamily": m["builds"][0]["chipFamily"],
                       "manifest": path.name, **d})
    names = [b["board"] for b in boards]
    if len(set(names)) != len(names):
        fail(f"a board appears twice: {names}")
    if not boards:
        fail(f"no *.manifest.json in {args.dir}")
    index = {"version": args.version, "boards": boards}
    Path(args.dir, "firmware-manifest.json").write_text(json.dumps(index, indent=2) + "\n")
    print(f"firmware-manifest.json: {len(boards)} boards")


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    for name in ("build", "merge"):
        s = sub.add_parser(name)
        s.add_argument("--board", required=True, help="folder name under firmware/devices")
        s.add_argument("--name", help="the board's name as people know it (default: board.json's)")
        s.add_argument("--chip", choices=sorted(CHIPS), help="default: board.json's, else esp32s3")
        s.add_argument("--board-json", help="default: firmware/devices/<board>/board.json")
        s.add_argument("--version", required=True, help="e.g. 0.1.0")
        s.add_argument("--out", default="dist")
        if name == "build":
            s.add_argument("--dir", required=True, help="the PlatformIO project")
            s.add_argument("--env", required=True)
            s.set_defaults(allow_no_board_json=False)
        else:
            s.add_argument("--allow-no-board-json", action="store_true",
                           help="a board without board.json: no deck fields, no OTA image")
            s.add_argument("--image", action="append", required=True, metavar="OFFSET=PATH",
                           help="bootloader, partitions, boot_app0 and app, e.g. 0x10000=firmware.bin")
    s = sub.add_parser("index")
    s.add_argument("--version", required=True)
    s.add_argument("--dir", default="dist")
    args = p.parse_args()
    {"build": cmd_build, "merge": cmd_merge, "index": cmd_index}[args.cmd](args)


if __name__ == "__main__":
    main()
