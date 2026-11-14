# Build and flash from source

This page puts the firmware on a board by building it yourself with PlatformIO. It's the way to run
the newest code, or to change it. The quick way, once it's online, is the web flasher in the
[README](../README.md#flash-a-board): no tools to install. Either way you do it once per board; later
updates go the same way.

Which board is which, with screenshots: [devices.md](devices.md).

## What you need

- A Mac with `agentctl` running ([quick start](../README.md#quick-start-the-mac-part)). Build it from
  the same clone (`./install.sh --source`) so the daemon and the firmware match.
- A clone of this repository: the firmware is in it.

  ```bash
  git clone https://github.com/brushknight/agents-deck && cd agents-deck
  ```

- A USB-C cable that carries **data**. Many cables that come with chargers only carry power, and the
  board then lights up but never shows a port. If in doubt, use the cable that came with a phone or a
  laptop, and plug straight into the Mac rather than a hub.
- About 1.5 GB of disk and 15 minutes for the first build (it downloads the compilers); later builds
  take seconds.

## 1. Install PlatformIO

PlatformIO is the tool that compiles the firmware and copies it onto the board. It fetches the right
compilers by itself.

```bash
brew install platformio      # gives you the `pio` command
pio --version
```

(Without Homebrew: `pipx install platformio`, or the PlatformIO extension for VS Code, which has the
same `pio` in its terminal.)

## 2. Plug the board in and find its port

```bash
agentctl link ports                          # lists USB serial ports, e.g. /dev/cu.usbmodem1101
```

A new `/dev/cu.usbmodem…` should appear when you plug the board in. If nothing appears, try another
cable first (charge-only cables are the usual cause), then another USB socket, then the BOOT and RST
step [below](#if-the-upload-cant-connect-boot-and-rst).

Register the port, so the deck can talk to the board once it's flashed. The deck only ever opens
ports you add:

```bash
agentctl link add /dev/cu.usbmodem1101       # once per port
```

The name can change when you use another USB socket; register the new one too.

## 3. Build and flash

From the root of the clone, the command for your board:

| board | firmware | flash command |
|---|---|---|
| 2.8″ round | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-lcd-28c/idf5 -e idf5 -t upload` |
| 2.1″ round | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-lcd-21/idf5 -e idf5 -t upload` |
| 4″ square | 2.0.17 (USB only, for now) | `pio run -d firmware/devices/waveshare-touch-lcd-4 -t upload` |
| 1.8″ AMOLED | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-amoled-18/idf5 -e idf5 -t upload` |
| LilyGO T4-S3 | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/lilygo-t4-s3/idf5 -e idf5 -t upload` |
| 1.47″ strip | 2.0.17 (USB only) | `pio run -d firmware/devices/waveshare-lcd-147 -t upload` |
| LilyGO T-Display-S3 | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/lilygo-t-display-s3/idf5 -e idf5 -t upload` |

With more than one board plugged in, add `--upload-port /dev/cu.usbmodem…` to pick one. The deck lets
go of the port while PlatformIO uses it, so you don't need to stop anything.

What the two kinds of build are:

- **`idf5`** is arduino-esp32 3.x on ESP-IDF 5 ([pioarduino](https://github.com/pioarduino/platform-espressif32)).
  It has the TLS 1.3 the Wi-Fi link needs. Its first build rebuilds the Arduino libraries with TLS 1.3
  turned on (about five minutes), and again after another board's `idf5` project was built in
  between. Its tools live in a PlatformIO home of their own, `~/.platformio-pioarduino`, because they
  clash with PlatformIO's own ESP32 platform.
- **2.0.17** is arduino-esp32 2.0.17, in the board's folder itself. It talks over USB only. The 4″
  uses it for now because its `idf5` build is
  [experimental](devices.md#waveshare-esp32-s3-touch-lcd-4-square-4); the 1.47″ has no Wi-Fi at all.

Each board's README lists its other envs: a `debug` build that prints what it's doing on the USB port
(`pio device monitor` shows it; the deck ignores text), and on the round boards a hardware self-test.

## 4. Watch it come up

The board restarts, says "connecting", and a few seconds later shows your agents. On a registered port
a new board is approved at once with scope `act`. The README's [Flash a board](../README.md#flash-a-board)
covers scopes and approving by hand, and [Wi-Fi](../README.md#wi-fi) takes it off the cable.

## If the upload can't connect: BOOT and RST

A board that's crashing, or one fresh from the factory, may not answer PlatformIO. Put it in its
download mode by hand:

1. Hold **BOOT**.
2. Press and release **RST** (or plug the cable in while holding BOOT).
3. Let go of BOOT, and run the flash command again.

Unplug and replug the board once it's done, so it starts the new firmware.

## From a release image

From v0.1.0 on, every [release](https://github.com/brushknight/agents-deck/releases) has a ready-made
image per board, `agents-deck-<board>-<version>.bin`, the same file the web flasher writes. To flash
one without building:

```bash
brew install esptool
shasum -a 256 -c SHA256SUMS --ignore-missing          # in the folder you downloaded them to
esptool --chip esp32s3 --port /dev/cu.usbmodem1101 write-flash 0x0 agents-deck-waveshare-touch-lcd-28c-0.1.0.bin
```

The image goes at `0x0`: it holds the bootloader, the partition table and the app. It leaves the rest
of the flash alone, so the board keeps its name, certificate and Wi-Fi network; `esptool --chip
esp32s3 --port … erase-flash` first starts it from nothing (the deck then pairs it again over USB).
[releasing.md](releasing.md) says how the images are built.

## Troubleshooting

| what you see | what to do |
|---|---|
| `agentctl link ports` finds nothing | Use a USB cable that carries data (charge-only cables are a common trap), and plug straight into the Mac rather than a hub. |
| `pio: command not found` | Install PlatformIO ([step 1](#1-install-platformio)), then open a new terminal. |
| the upload says the port is busy | Close any serial monitor (`pio device monitor`, Arduino IDE). The deck itself lets go of the port while flashing. |
| the upload can't connect, or times out | [BOOT and RST](#if-the-upload-cant-connect-boot-and-rst), then flash again. |
| an `idf5` build fails with `No module named 'SCons.Tool.FortranCommon'` | pioarduino and PlatformIO Core disagree about their SCons package. Once, give the pioarduino home PlatformIO's own (the commands are below this table). |
| the board stays on "connecting" ("plugged into the Mac?", or "waiting for the deck · on USB" on the round boards) | The deck isn't talking to it on USB: run `agentctl link ports` and `agentctl link add <port>` (the port name can change when you use another USB socket), and check the daemon runs (`agentctl ls`). |
| "waiting for approval" | Auto-approval is off: run the `agentctl devices approve …` command shown on the screen. |
| "Wi-Fi failed · wrong password" or "network not found" | Plug it into the Mac and set the network again with `agentctl link wifi --default --ssid "…"`. Boards use 2.4 GHz Wi-Fi only. |
| "deck not reachable · over Wi-Fi" | The Mac is asleep, the daemon isn't running, the Mac is on another network, or the macOS firewall blocks `agentctl`: allow it in System Settings → Network → Firewall → Options. |
| "certificate rejected · pair again over USB" | The board was revoked or re-paired: plug it into the Mac and it pairs again by itself. |
| `agentctl link wifi` is an unknown command | Your `agentctl` is older than Wi-Fi for boards: update it, or install from the clone with `./install.sh --source`. |

The SCons fix, once:

```bash
rm -rf ~/.platformio-pioarduino/packages/tool-scons
cp -R ~/.platformio/packages/tool-scons ~/.platformio-pioarduino/packages/tool-scons
python3 -c "import json,os;p=os.path.expanduser('~/.platformio-pioarduino/packages/tool-scons/package.json');d=json.load(open(p));d['version']='4.40801.0';json.dump(d,open(p,'w'))"
```

(`~/.platformio/packages/tool-scons` is there once you've built any 2.0.17 project. Without one,
`PLATFORMIO_CORE_DIR=~/.platformio-pioarduino .github/scripts/pioarduino-scons.sh <project> idf5`
does the same, as the release builds do.)

More: `agentctl link check <port>` tests a board's firmware against a demo deck, and each board's
README has the details: every screen, every build option, pins and tests.
