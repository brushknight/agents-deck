#!/usr/bin/env python3
"""Tests for firmware_release.py's merge, board.json, OTA image and index,
on a made-up build (no PlatformIO). Needs esptool (merge-bin):

    python3 -m unittest discover -s .github/scripts -p 'test_*.py'
"""

import importlib.util
import json
import struct
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "firmware_release.py"
spec = importlib.util.spec_from_file_location("firmware_release", SCRIPT)
fr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fr)

try:
    import esptool  # noqa: F401

    HAVE_ESPTOOL = True
except ImportError:
    HAVE_ESPTOOL = False

APP_OFFSET = 0x10000
SLOT = 0x300000


def esp_image(chip_id, payload, flash_size_nibble=4, app_desc=True):
    """A minimal ESP image: header, one segment (starting with esp_app_desc_t), padding."""
    seg = b""
    if app_desc:
        desc = struct.pack("<II8x", fr.APP_DESC_MAGIC, 0)
        desc += b"0.0.0-test".ljust(32, b"\0") + b"made-up-board".ljust(32, b"\0")
        seg = desc
    seg += payload
    seg += b"\0" * (-len(seg) % 4)
    header = bytes([0xE9, 1, 2, (flash_size_nibble << 4) | 0x0F]) + struct.pack("<I", 0x40370000)
    header += bytes([0xEE, 0, 0, 0]) + struct.pack("<H", chip_id) + bytes(9) + bytes([0])
    assert len(header) == 24
    return header + struct.pack("<II", 0x3C000020, len(seg)) + seg


def partition_table(entries):
    out = b""
    for label, ptype, sub, off, size in entries:
        out += struct.pack("<HBBII", 0x50AA, ptype, sub, off, size) + label.encode().ljust(16, b"\0") + bytes(4)
    return out + b"\xff" * 32


TABLE = [
    ("nvs", 1, 0x02, 0x9000, 0x5000),
    ("otadata", 1, 0x00, 0xE000, 0x2000),
    ("app0", 0, 0x10, APP_OFFSET, SLOT),
    ("app1", 0, 0x11, APP_OFFSET + SLOT, SLOT),
]


class FakeBuild:
    def __init__(self, root, table=TABLE, app=None, flash_nibble=4):
        self.root = Path(root)
        self.root.mkdir(parents=True, exist_ok=True)
        (self.root / "bootloader.bin").write_bytes(esp_image(9, b"boot" * 100, flash_nibble, app_desc=False))
        (self.root / "partitions.bin").write_bytes(partition_table(table))
        (self.root / "boot_app0.bin").write_bytes(b"\xff" * 0x2000)
        (self.root / "firmware.bin").write_bytes(app if app is not None else esp_image(9, b"app!" * 1000))

    def images(self):
        return [
            f"0x0={self.root / 'bootloader.bin'}",
            f"0x8000={self.root / 'partitions.bin'}",
            f"0xe000={self.root / 'boot_app0.bin'}",
            f"{APP_OFFSET:#x}={self.root / 'firmware.bin'}",
        ]


def board_json(path, **over):
    b = {
        "name": "Made-up Board", "chip": "esp32s3", "types": ["made-up-board", "made-up-old"],
        "usb": [{"vid": "303a", "pid": "1001"}], "flash_size": 16 << 20, "wifi": True,
        "picture": "docs/images/devices/lcd4-home.png",
    }
    b.update(over)
    Path(path).write_text(json.dumps(b))
    return path


def run(*args):
    return subprocess.run([sys.executable, str(SCRIPT), *args], capture_output=True, text=True)


@unittest.skipUnless(HAVE_ESPTOOL, "needs esptool")
class MergeTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.out = self.tmp / "dist"

    def merge(self, build, *extra):
        return run("merge", "--board", "made-up", "--version", "1.2.3", "--out", str(self.out),
                   *sum((["--image", i] for i in build.images()), []), *extra)

    def test_wifi_board_gets_ota_image_and_fields(self):
        b = FakeBuild(self.tmp / "build")
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertEqual(r.returncode, 0, r.stderr)
        app = self.out / "agents-deck-made-up-1.2.3.app.bin"
        self.assertEqual(app.read_bytes(), (b.root / "firmware.bin").read_bytes())
        m = json.loads((self.out / "agents-deck-made-up-1.2.3.manifest.json").read_text())
        d = m["agents_deck"]
        self.assertEqual(m["name"], "Made-up Board")
        self.assertEqual(d["types"], ["made-up-board", "made-up-old"])
        self.assertEqual(d["usb"], [{"vid": "303a", "pid": "1001"}])
        self.assertEqual(d["flash_size"], 16 << 20)
        self.assertTrue(d["wifi"])
        self.assertEqual(d["picture"], "docs/images/devices/lcd4-home.png")
        self.assertEqual(d["ota"], {"file": app.name, "size": app.stat().st_size, "sha256": fr.sha256(app)})
        for key in ("board", "chip", "file", "offset", "size", "sha256", "version", "name"):
            self.assertIn(key, d)

        r = run("index", "--version", "1.2.3", "--dir", str(self.out))
        self.assertEqual(r.returncode, 0, r.stderr)
        idx = json.loads((self.out / "firmware-manifest.json").read_text())
        self.assertEqual(idx["version"], "1.2.3")
        self.assertEqual(idx["boards"][0]["ota"]["file"], app.name)

        app.write_bytes(app.read_bytes() + b"x")  # a changed OTA image fails the index
        r = run("index", "--version", "1.2.3", "--dir", str(self.out))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("app.bin: sha256", r.stderr)

    def test_usb_only_board_has_no_ota_image(self):
        b = FakeBuild(self.tmp / "build")
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json", wifi=False)))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse((self.out / "agents-deck-made-up-1.2.3.app.bin").exists())
        d = json.loads((self.out / "agents-deck-made-up-1.2.3.manifest.json").read_text())["agents_deck"]
        self.assertIsNone(d["ota"])

    def test_app_bigger_than_ota_slot_fails(self):
        table = [TABLE[0], TABLE[1], ("app0", 0, 0x10, APP_OFFSET, 0x200000), ("app1", 0, 0x11, 0x210000, 0x1000)]
        b = FakeBuild(self.tmp / "build", table=table)
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("doesn't fit OTA slot app1", r.stderr)

    def test_one_app_slot_fails_for_wifi_board(self):
        table = [TABLE[0], TABLE[1], ("factory", 0, 0x00, APP_OFFSET, SLOT)]
        b = FakeBuild(self.tmp / "build", table=table)
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("needs two", r.stderr)

    def test_app_without_description_fails(self):
        b = FakeBuild(self.tmp / "build", app=esp_image(9, b"x" * 400, app_desc=False))
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("no app description", r.stderr)

    def test_app_for_another_chip_fails(self):
        b = FakeBuild(self.tmp / "build", app=esp_image(5, b"x" * 400))
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("chip id 5", r.stderr)

    def test_flash_size_must_match_board_json(self):
        b = FakeBuild(self.tmp / "build", flash_nibble=3)  # 8 MB in the header
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("bytes of flash", r.stderr)

    def test_name_must_match_board_json(self):
        b = FakeBuild(self.tmp / "build")
        r = self.merge(b, "--board-json", str(board_json(self.tmp / "board.json")), "--name", "Other")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("differs from", r.stderr)

    def test_merge_without_board_json(self):
        b = FakeBuild(self.tmp / "build")
        r = self.merge(b, "--name", "Made-up Board")
        self.assertNotEqual(r.returncode, 0)  # every released board needs one
        r = self.merge(b, "--name", "Made-up Board", "--allow-no-board-json")
        self.assertEqual(r.returncode, 0, r.stderr)
        d = json.loads((self.out / "agents-deck-made-up-1.2.3.manifest.json").read_text())["agents_deck"]
        self.assertNotIn("ota", d)


class BoardJsonTest(unittest.TestCase):
    def test_released_boards(self):
        """Every board in the release workflow has a valid board.json."""
        repo = HERE.parents[1]
        workflow = (repo / ".github" / "workflows" / "release.yml").read_text()
        boards = [line.split("board:")[1].split("#")[0].strip()
                  for line in workflow.splitlines() if line.strip().startswith("- board:")]
        self.assertEqual(len(boards), 6, boards)
        for board in boards:
            b = fr.load_board(repo / "firmware" / "devices" / board / "board.json")
            self.assertTrue(b["types"])

    def test_bad_board_json(self):
        tmp = Path(tempfile.mkdtemp())
        for bad in ({"usb": [{"vid": "303A", "pid": "1001"}]}, {"wifi": "yes"}, {"types": []},
                    {"flash_size": 1000}, {"picture": "docs/images/devices/nothing.png"}, {"flash_size": True}):
            with self.subTest(bad=bad):
                with self.assertRaises(SystemExit):
                    fr.load_board(board_json(tmp / "b.json", **bad))


if __name__ == "__main__":
    unittest.main()
