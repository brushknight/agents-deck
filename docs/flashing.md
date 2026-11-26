# Flash a board

A board needs our firmware (the program it runs) once, written to it over USB: that's "flashing". You
do it once per board, and again for a new version. There are three ways, all writing the same
ready-made image from the latest release:

1. **[From the app](#from-the-app-settings-boards)**, in Settings, Boards. The way to go once you've
   installed agents deck.
2. **[From the web flasher](#from-the-web-flasher)**, in Chrome or Edge, with nothing installed.
3. **[From a terminal](#from-a-release-image)**, with `agentctl flash`.

And a fourth, for running the newest code or changing it:
**[building it yourself](#build-from-source)** with PlatformIO. A build you made on this computer then
installs with one click, like a release: [firmware built on this computer](#firmware-built-on-this-computer).

Which board is which, with pictures of every screen: [devices.md](devices.md).

## What you need

- One of the [supported boards](devices.md).
- A USB-C cable that carries **data**. Many cables that come with chargers only carry power: the board
  then lights up but your computer never sees it. If in doubt, use the cable that came with a phone or
  a laptop, and plug straight into the computer rather than a hub.

## From the app: Settings, Boards

With agents deck installed ([install.md](install.md)):

1. **Plug the board in.** The line under the deck says **new board on USB**, and a notification says
   the same.
2. **Say it's yours.** Click that (or open Settings, Boards). Settings asks whether this USB port is
   one of your boards; click **Yes, it's mine**. Until you do, agents deck never opens the port: it only knows the
   port is there and that it looks like an ESP32.
3. **Pick the board.** If the board already runs our firmware, it's known and you skip this. Otherwise
   pick it from the pictures; the list only shows boards that match the chip and flash size the board
   reports.
4. **Install.** Click **Install firmware**. agents deck downloads the image for that board from the
   latest release, checks its signature, writes it and reads it back to verify it. About a minute,
   with a progress bar.
5. **Done.** The board restarts, pairs with the deck by itself, and shows your agents. Settings then
   offers to give it Wi-Fi.

Picked the wrong board? Nothing breaks: install again with the right one. If the board doesn't
answer, Settings shows the [BOOT and RST](#if-the-board-doesnt-answer-boot-and-rst) rescue.

**A copy of agents deck built from source** has no release key, so it can't check firmware
downloaded from a release and won't install it. Firmware built on this computer still installs in
one click: once a build is there ([below](#firmware-built-on-this-computer)), step 3 shows the boards
it is for, marked "built here", and step 4's button says **Install 0.2.0-dev.7, built on this
computer**. Without a build there, Settings says what to do instead of offering **Install firmware**,
and you can still set the board up over its USB cable with a single file: click **Install a firmware
file...** and pick a full image, either one you built (a merged image written at `0x0`) or a
release's `agents-deck-<board>-<version>.bin` you downloaded yourself. Settings reads the file first
and refuses it, with the reason, before anything is written:

- an app image (`.pio/build/<env>/firmware.bin`, a release's `.app.bin`) is the firmware alone. It
  only starts on a board that already has a bootloader and a partition table, and a board with
  nothing of ours on it may have neither, so pick the full image;
- a file for another chip than the one the board's bootloader reported;
- a file bigger than the board's flash, or built for more flash than the board has, which wouldn't
  start.

A full image is written at `0x0`: bootloader, partition table and app in one go, as the release
images are. Then the board restarts and pairs as in step 5. For a board that already runs agents
deck, the same button installs the file through the board, as described just below.

**Updates.** When a new release has newer firmware, Settings, Boards and Settings, Updates list each
board that can update, with an **Update** button; nothing updates by itself. A board on USB is flashed
again the same way. A board on Wi-Fi updates over the air: it downloads the new version from agents
deck over its encrypted connection, checks it, and restarts into it, and if the new version can't
reach the deck it goes back to the old one. The 1.47″ strip and the 4″ square have no Wi-Fi build
yet, so they update on the cable.

Updating keeps the board's name, its certificate and its Wi-Fi network.

Each board in Settings, Boards says which firmware it runs, how it is connected and one line about
updates: **Update to X available** (with an **Update** button), **0.2.0-dev.7 built on this
computer** (with an **Install** button, [below](#firmware-built-on-this-computer)), **Up to date**,
**Newer than the latest release** (a build of your own, such as `0.2.0-dev.4`), **Couldn't check for
updates** with the reason, or that a release is out and this copy installs only firmware built on
this computer (no release key). `agentctl firmware ls` lists your boards the same way under the
release's firmware and the builds made here.

## Firmware built on this computer

agents deck keeps the firmware you build in a folder of its own, `firmware-builds` in its data folder,
next to the releases. A board that runs an older version than a build there gets an **Install**
button on its row in Settings, Boards: one click, nothing to pick. It works with or without a release
key.

1. Build the board's release files with the release script, as [releasing.md](releasing.md#how-the-firmware-images-are-built)
   shows. Give it a version newer than the one the board runs, such as `0.2.0-dev.7`: it is the version
   the firmware reports, and the one agents deck compares.
2. Put the build there:

   ```bash
   agentctl firmware add dist        # the folder the script wrote; or name its files
   ```

   It takes `agents-deck-<board>-<version>.bin` (the full image), `.app.bin` (the app image, for
   boards with Wi-Fi) and `.manifest.json`, as the script writes them; a `firmware-manifest.json`
   alone also works. Each build is checked before it is kept, the same checks as a file picked in
   Settings: ESP32 images of the right kind, for the chip and flash size the manifest says, with the
   sizes and SHA-256 it lists.
3. Click **Install** on the board's row (or run `agentctl devices update <name>`). A board on its
   USB cable gets the full image written at `0x0`; a board on Wi-Fi gets the app image over the air.
   The board's line says which way before you click, and why not when it can't (a board on Wi-Fi with
   a build that has no app image: plug it in).

For a board with nothing of ours on it yet, **Set it up** offers the boards a build here is for, and
installs its full image over the cable (or `agentctl flash --local --board <board> <port>`).

`agentctl firmware ls` lists the builds with the release's firmware; `agentctl firmware rm <board>
[<version>]` removes them. Copying the files into the folder by hand works too: they are read
again, and checked again before every install.

When a release and a build here are both newer than what a board runs, the line offers the newer of
the two and says where it comes from (**Update to 0.3.0 available**, or **0.3.1-dev.2 built on this
computer**). Versions compare the semver way: `0.2.0-dev.7` is older than `0.2.0-dev.10`, and both
are older than `0.2.0`.

Nothing in this comes from the network: only this computer puts builds in the folder (the service
takes them on its own socket and on the dashboard at `127.0.0.1`, never from a board or a phone).
A build isn't checked against a release signature, it is your own; the board still checks what it
always checks.

**A firmware file of your own.** **Install a firmware file...**, on each connected board (and in **Set
it up** for a board on a USB port), installs a
`.bin` you built or downloaded, without a release: the same as `agentctl link update --image` and
`agentctl flash <port> <image.bin>`. Pick the file; Settings reads it before anything goes to the
board and refuses a file that isn't an ESP32 image, that is for another chip than the board's, or that
can't go the way the board is connected. Then it shows the board, the file's name, size and version
(it asks for the version when the file doesn't say one) and installs after you confirm:

- an app image (`.pio/build/<env>/firmware.bin`, or a release's `.app.bin`) goes over Wi-Fi: the
  board downloads it over its encrypted connection, checks it and restarts into it. The version must be
  the one the firmware reports (`AGENTS_DECK_VERSION` when it was built), so the deck can tell when
  it's back;
- a full image (a release's `agents-deck-<board>-<version>.bin`, or a merged image) is written at
  `0x0` over USB.

A file picked this way isn't checked against a release signature: it's your choice on your own
computer, and it works in a build without a release key. The board still checks what it always
checks: that the update is for its own board, the image's SHA-256 and the bootloader's checks.
Settings uploads the file only to the service on this computer (`127.0.0.1`); it is never accepted
from the network.

## From the web flasher

For a board when you haven't installed agents deck, or on a computer that isn't yours. The **[web
flasher](https://agents-deck.sh/#boards)** runs in Chrome or Edge: pick your board, click **Connect**, choose the board's
USB port, and click **Install**. Nothing to install, about a minute. It uses Web Serial, which Safari
and Firefox don't have.

It writes the same image as the app. To use the board with your agents afterwards, install agents deck
([install.md](install.md)) and confirm the board in Settings, Boards.

## From a release image

In a terminal, with `agentctl flash`. It talks to the board's bootloader itself: no Python, no
esptool. Every [release](https://github.com/brushknight/agents-deck/releases/latest) has an image
per board, `agents-deck-<board>-<version>.bin`; `<board>` is the board's folder name (the
[table below](#build-from-source) lists them, and `agentctl firmware ls` shows the ones in the latest
release).

1. Find the board's port. Plug it in and run:

   ```bash
   agentctl link ports
   ```

   ```text
     /dev/cu.usbmodem1101  303a:1001  ESP32 native USB
   ```

   A new port appears when you plug the board in: `/dev/cu.usbmodem…` on a Mac, `/dev/ttyACM…` or
   `/dev/ttyUSB…` on Linux, `COM…` on Windows. `303a:1001` is the ESP32-S3's own USB; boards with a
   USB adapter chip show as CH340, CP210x or FTDI.

2. Download the image for your board and `SHA256SUMS` from the release, and check the image in the
   folder you saved them to:

   ```bash
   shasum -a 256 -c SHA256SUMS --ignore-missing     # Linux: sha256sum -c SHA256SUMS --ignore-missing
   ```

3. Flash it:

   ```bash
   agentctl flash /dev/cu.usbmodem1101 agents-deck-waveshare-touch-lcd-28c-0.2.0.bin
   ```

   It connects, writes, verifies and restarts the board. If the deck is already using that port, it
   lets go for the flash by itself. `agentctl flash <port> --probe` only reads the chip, its MAC and
   flash size, to check you have the right port.

4. Let the deck talk to the board. The deck only opens ports you register:

   ```bash
   agentctl link add /dev/cu.usbmodem1101       # once per port
   ```

The image goes at `0x0`: it holds the bootloader, the partition table and the app, and leaves the rest
of the flash alone, so the board keeps its name, certificate and Wi-Fi network. To start a board from
nothing, erase it first with [esptool](https://github.com/espressif/esptool)
(`esptool --chip esp32s3 --port … erase-flash`); the deck then pairs it again over USB.
[releasing.md](releasing.md) says how the images are built.

## Build from source

This builds the firmware yourself with PlatformIO. It's the way to run code that isn't in a release
yet, or to change it. About 1.5 GB of disk and 15 minutes the first time (it downloads the compilers);
later builds take seconds. These steps are written for a Mac; on Linux, install PlatformIO with
`pipx install platformio` and use your `/dev/tty…` port.

You also need:

- `agentctl` running, built from the same clone, so the daemon and the firmware match:
  `./install.sh --source` ([install.md](install.md#only-the-command-line)).
- A clone of this repository: the firmware is in it.

  ```bash
  git clone https://github.com/brushknight/agents-deck && cd agents-deck
  ```

### 1. Install PlatformIO

PlatformIO is the tool that compiles the firmware and copies it onto the board. It fetches the right
compilers by itself.

```bash
brew install platformio      # gives you the `pio` command
pio --version
```

(Without Homebrew: `pipx install platformio`, or the PlatformIO extension for VS Code, which has the
same `pio` in its terminal.)

### 2. Plug the board in and register its port

```bash
agentctl link ports                          # lists USB serial ports, e.g. /dev/cu.usbmodem1101
agentctl link add /dev/cu.usbmodem1101       # once per port
```

If no new port appears, try another cable first (charge-only cables are the usual cause), then another
USB socket, then [BOOT and RST](#if-the-board-doesnt-answer-boot-and-rst). The name can change when you
use another USB socket; register the new one too.

### 3. Build and flash

From the root of the clone, the command for your board:

| board | folder (`<board>`) | firmware | flash command |
|---|---|---|---|
| 2.8″ round | `waveshare-touch-lcd-28c` | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-lcd-28c/idf5 -e idf5 -t upload` |
| 2.1″ round | `waveshare-touch-lcd-21` | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-lcd-21/idf5 -e idf5 -t upload` |
| 4″ square | `waveshare-touch-lcd-4` | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-lcd-4/idf5 -e idf5 -t upload` |
| 1.8″ AMOLED | `waveshare-touch-amoled-18` | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/waveshare-touch-amoled-18/idf5 -e idf5 -t upload` |
| LilyGO T4-S3 | `lilygo-t4-s3` | `idf5` (USB and Wi-Fi) | `pio run -d firmware/devices/lilygo-t4-s3/idf5 -e idf5 -t upload` |
| 1.47″ strip | `waveshare-lcd-147` | `idf5` (USB only) | `pio run -d firmware/devices/waveshare-lcd-147/idf5 -e idf5 -t upload` |

With more than one board plugged in, add `--upload-port /dev/cu.usbmodem…` to pick one. The deck lets
go of the port while PlatformIO uses it, so you don't need to stop anything.

What the two kinds of build are:

- **`idf5`** is arduino-esp32 3.x on ESP-IDF 5 ([pioarduino](https://github.com/pioarduino/platform-espressif32)).
  It has the TLS 1.3 the Wi-Fi link needs, and it is what the releases are built from. Its first build
  rebuilds the Arduino libraries with TLS 1.3 turned on (about five minutes; not on the 1.47″, whose
  firmware has no Wi-Fi), and again after another board's `idf5` project was built in
  between. Its tools live in a PlatformIO home of their own, `~/.platformio-pioarduino`, because they
  clash with PlatformIO's own ESP32 platform.
- **2.0.17** is arduino-esp32 2.0.17, in the board's folder itself. It talks over USB only and is no
  longer released: it is there to fall back on, for example if the 4″'s picture misbehaves on `idf5`
  ([its notes](devices.md#waveshare-esp32-s3-touch-lcd-4-square-4)).

Each board's README lists its other envs: a `debug` build that prints what it's doing on the USB port
(`pio device monitor` shows it; the deck ignores text), and on the round boards a hardware self-test.

### Or: build it once, install it with one click

`pio run -t upload` writes the build over the cable right away. To keep a build and install it from
Settings instead (on any board of that kind, over USB or Wi-Fi), build the release files and add them:

```bash
python3 .github/scripts/firmware_release.py build --board waveshare-touch-lcd-28c \
  --dir firmware/devices/waveshare-touch-lcd-28c/idf5 --env idf5 --version 0.2.0-dev.7 --out dist
agentctl firmware add dist
```

Every board of that kind that runs an older version then shows **0.2.0-dev.7 built on this computer**
with an **Install** button ([firmware built on this computer](#firmware-built-on-this-computer)).

### 4. Watch it come up

The board restarts, says "connecting", and a few seconds later shows your agents. On a registered port
a new board is approved at once with scope `act`. The README's [Flash a board](../README.md#flash-a-board)
covers scopes and approving by hand, and [Wi-Fi](../README.md#wi-fi) takes it off the cable.

### Let a coding agent do it

Every board's README is written so a coding agent can follow it step by step, and they're
independent, so you can give each board to its own subagent and flash several in parallel. Paste this
into Claude Code or Codex in a fresh clone of this repository (fill in your board):

````text
Set up agents deck on this computer from this clone, and get my <board name, e.g. Waveshare
ESP32-S3-Touch-LCD-2.8C> working on it, first over USB, then over Wi-Fi.

1. Read AGENTS.md, README.md ("Flash a board" and "Wi-Fi"), docs/flashing.md ("Build from source"),
   docs/devices.md, and the README in the board's folder under firmware/devices/. Follow the board
   README where it is more specific.
2. Check prerequisites and install what's missing, telling me before each install: Go 1.24+,
   PlatformIO (`pio`), tmux. Flutter only if I want the menu bar app.
3. Build and install agentctl from this clone: `./install.sh --source` (add `--app` for the menu bar
   app). Check the daemon is up with `agentctl status`.
4. Ask me to plug the board in with a data USB-C cable. Run `agentctl link ports`, and if no new port
   appears, tell me to try another cable (charge-only cables are common). Register the port with
   `agentctl link add <port>`.
5. Build and flash from source as docs/flashing.md says, with the env it lists for this board and
   `--upload-port <port>`. If the upload can't connect, ask me to hold BOOT, press RST, release
   BOOT, and try again. Don't edit firmware source or build flags to make it work; tell me what
   failed instead.
6. Verify: `agentctl link` shows the board on that port, and `agentctl devices` lists it as
   approved (scope act). Ask me whether I want scope admin (it adds stop) before setting it.
7. Wi-Fi (skip for boards without it): ask me for the network name only, then tell me to run
   `agentctl link wifi --default --ssid "<name>"` myself in a terminal and type the password at its
   prompt. Never ask me for the password, never put it on a command line, in a file or in your
   notes. Then check `agentctl link` for the result.
8. Tell me what to check by eye: the board shows my agents (or `agentctl sim start --root ~/dev
   --slots 6` for pretend ones); tapping an agent opens its card; a waiting agent has orange on it;
   after I unplug it and power it from a charger it comes back with a small Wi-Fi mark.
9. Finish with a short report: what you installed, the board's name, its port, its scope, and how it
   is connected now.
````

## If the board doesn't answer: BOOT and RST

A board that's crashing, or one fresh from the factory, may not answer the flasher, whichever one you
use. Put it in its download mode by hand:

1. Hold **BOOT**.
2. Press and release **RST** (or plug the cable in while holding BOOT).
3. Let go of BOOT, and flash again.

Unplug and replug the board once it's done, so it starts the new firmware.

## Troubleshooting

| what you see | what to do |
|---|---|
| no new port when you plug the board in (`agentctl link ports`, or nothing new in Settings, Boards) | Use a USB cable that carries data (charge-only cables are a common trap), and plug straight into the computer rather than a hub. On Linux, see [the board's port can't be opened](install.md#linux-the-boards-port-cant-be-opened). |
| flashing can't connect, or times out | [BOOT and RST](#if-the-board-doesnt-answer-boot-and-rst), then flash again. |
| the port is busy | Close any serial monitor (`pio device monitor`, Arduino IDE, a web flasher tab). The deck itself lets go of the port while flashing. |
| "no release key" from `agentctl firmware ls`, or Settings won't download firmware | That `agentctl` was built without the release signing key (a build from source), so it never installs anything it downloaded. Build the firmware and add it with `agentctl firmware add <folder>` ([firmware built on this computer](#firmware-built-on-this-computer)): the board then gets an **Install** button. Or pick a single image with **Install a firmware file...**, or install a release ([install.md](install.md)). |
| `pio: command not found` | Install PlatformIO ([step 1](#1-install-platformio)), then open a new terminal. |
| an `idf5` build fails with `No module named 'SCons.Tool.FortranCommon'` | pioarduino and PlatformIO Core disagree about their SCons package. Once, give the pioarduino home PlatformIO's own (the commands are below this table). |
| the board stays on "connecting" ("plugged into the Mac?", or "waiting for the deck · on USB" on the round boards) | The deck isn't talking to it on USB: confirm the board in Settings, Boards (or `agentctl link ports` and `agentctl link add <port>`; the port name can change when you use another USB socket), and check the service runs (`agentctl status`). |
| "waiting for approval" | Auto-approval is off: approve the board in Settings, Boards, or run the `agentctl devices approve …` command shown on the screen. |
| "Wi-Fi failed · wrong password" or "network not found" | Plug it into the computer and set the network again (Settings, Network, or `agentctl link wifi --default --ssid "…"`). Boards use 2.4 GHz Wi-Fi only. |
| "deck not reachable · over Wi-Fi" | The computer is asleep, the service isn't running, the computer is on another network, or a firewall blocks `agentctl`. On a Mac, allow it in System Settings, Network, Firewall, Options. |
| "deck not reachable" over Wi-Fi after you updated agents deck, while the service runs (or a panel says the TLS handshake failed) | The deck's certificate authority may have changed, so the board's certificate no longer matches: see [a board stopped connecting after an update](install.md#a-board-stopped-connecting-after-an-update). |
| "certificate rejected · pair again over USB" | The board was revoked or re-paired: plug it into the computer and it pairs again by itself. |
| an update over Wi-Fi failed, or the board came back on its old version | Plug it in and update it on the cable. A board always takes a full image over USB. |
| `agentctl link wifi` or `agentctl flash` is an unknown command | Your `agentctl` is older: update it ([install.md](install.md)), or install from the clone with `./install.sh --source`. |

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
