# Releasing

## Versions

Releases are numbered with [semantic versioning](https://semver.org): `vMAJOR.MINOR.PATCH`, starting at
**v0.1.0**. While the major number is 0, a minor bump (`v0.2.0`) may change things you notice (the API,
Deck Link, how boards pair); a patch (`v0.1.1`) only fixes. A tag with a suffix (`v0.2.0-rc.1`) is a
pre-release: the same build, for trying a release before it's the latest. One version covers
everything in the release: `agentctl`, the menu bar app and every board's firmware are built from the
same tag.

## What a release contains

| file | what |
|---|---|
| `agentctl_<version>_darwin_arm64.tar.gz` | the daemon and CLI |
| `agents-deck_<version>_macos_arm64.zip` | the menu bar app (`agents deck.app`) |
| `agents-deck-<board>-<version>.bin` | a board's firmware, one image flashed at `0x0`: bootloader, partition table, `boot_app0` and the app |
| `agents-deck-<board>-<version>.manifest.json` | an [ESP Web Tools](https://esphome.github.io/esp-web-tools/) manifest for that image |
| `firmware-manifest.json` | every board's image: board, chip, file, offset, size, sha256, env, version |
| `SHA256SUMS` | checksums of all of these, checked by `install.sh` |

`<board>` is the board's folder under `firmware/devices/`, and its image is built from the firmware
[docs/flashing.md](flashing.md) recommends:

| board | project | env | framework |
|---|---|---|---|
| `waveshare-touch-lcd-28c` | `waveshare-touch-lcd-28c/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-lcd-21` | `waveshare-touch-lcd-21/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-amoled-18` | `waveshare-touch-amoled-18/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `lilygo-t4-s3` | `lilygo-t4-s3/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-lcd-4` | `waveshare-touch-lcd-4` | `waveshare-touch-lcd-4` | arduino-esp32 2.0.17, USB only (its `idf5` build is experimental) |
| `waveshare-lcd-147` | `waveshare-lcd-147` | `waveshare-s3-147b` | arduino-esp32 2.0.17, USB only |

Flashing one by hand: [docs/flashing.md](flashing.md#from-a-release-image).

## Cutting a release

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The `release` workflow builds everything for that tag, creates the GitHub release (with generated
notes) and attaches the files above. Publishing a release from the GitHub UI works too: the workflow
fills that release. To rebuild an existing tag, run the workflow by hand (Actions → release → Run
workflow). Before the first real one, a pre-release tag (`v0.1.0-rc.1`) runs the whole thing end to
end.

Before tagging: `main` is green in `ci`, and each board you changed was flashed from its `idf5` (or
2.0.17) project and checked by eye.

Every push to `main` and every pull request runs `ci`: Go vet and tests, cross-compiling for the planned
platforms, the Flutter tests of the deck UI and the menu bar app, a Linux build of the tray app with
a headless smoke test (`clients/macos/linux/test/smoke.sh`), and the firmware host tests and default
builds.

## How the firmware images are built

The `firmware` job in `.github/workflows/release.yml` runs once per board, in a pinned container
(`python:3.12.15-slim-bookworm`) with pinned tools: [pioarduino's PlatformIO
Core](https://github.com/pioarduino/platformio-core) 6.2.0 (the Core pioarduino's platform is made
for; it builds the 2.0.17 boards too) and `esptool==5.6.0`.

1. `pio run` builds the board's env. `idf5` projects use a PlatformIO home of their own
   (`PLATFORMIO_CORE_DIR=~/.platformio-pioarduino`), as on a Mac, so pioarduino's packages never meet
   PlatformIO's own espressif32 platform; their first build also rebuilds the Arduino libraries with
   TLS 1.3 (`custom_sdkconfig`). Before that build, `.github/scripts/pioarduino-scons.sh` installs the
   project's packages and gives the home Core's own SCons, labelled with the version pioarduino
   checks for: in a clean home the first build otherwise fails with `No module named
   'SCons.Tool.FortranCommon'` (pioarduino deletes Core's SCons while Core runs from it), the clash
   [flashing.md](flashing.md#troubleshooting) fixes by hand. Each board has its own cache of its
   PlatformIO home, keyed on the PlatformIO version and its `platformio.ini`.
2. `.github/scripts/firmware_release.py build` reads the flash layout from the build itself
   (`pio project metadata`: the bootloader, partition table and `boot_app0` with their offsets, and the
   app's offset) and merges them into one image with `esptool merge-bin`, keeping the flash mode, speed
   and size PlatformIO wrote. It then checks the image: each part matches its file, the bootloader and
   app are ESP32-S3 images where the chip expects them, the app sits where the partition table says
   and fits its partition, and `otadata` holds `boot_app0`.
3. It writes the board's ESP Web Tools manifest (with an `agents_deck` block that ESP Web Tools
   ignores: board, chip, file, offset, size, sha256, env), and `firmware-index` collects them all into
   `firmware-manifest.json`.

To make the same image on your Mac from a build you already have (no upload):

```bash
pio run -d firmware/devices/waveshare-touch-lcd-28c/idf5 -e idf5
pip install esptool    # or use a venv
python3 .github/scripts/firmware_release.py build --board waveshare-touch-lcd-28c \
  --name "Waveshare ESP32-S3-Touch-LCD-2.8C" --dir firmware/devices/waveshare-touch-lcd-28c/idf5 \
  --env idf5 --version 0.0.0-dev --out dist
```

(`build` runs `pio run` again, which is quick once built; `merge --image 0x0=… --image 0x8000=…` merges
files you name instead.)

### Coming soon: the web flasher

A web flasher on the project's website will flash these images from the browser (Chrome or Edge, over
Web Serial): pick a board, connect, install. It reads `firmware-manifest.json` from the latest release
to list the boards, and hands the board's ESP Web Tools manifest to the installer. The README's
[Flash a board](../README.md#flash-a-board) gets its link once it's online.

## Signing and notarization (macOS)

Without signing secrets, builds are ad-hoc signed: they run, but macOS warns the first time and treats every build as a new app (permission prompts come back after each update). With a Developer ID certificate, both `agentctl` and the app are signed with it, and the app is notarized so macOS opens it without a warning.

Repository secrets (Settings → Secrets and variables → Actions):

| secret | value |
|---|---|
| `MACOS_CERT_P12` | your **Developer ID Application** certificate with its private key, exported from Keychain Access as .p12, base64-encoded: `base64 -i cert.p12 \| pbcopy` |
| `MACOS_CERT_PASSWORD` | the password you gave the .p12 export |
| `MACOS_SIGNING_IDENTITY` | the identity name, e.g. `Developer ID Application: Your Name (TEAMID)` (`security find-identity -v -p codesigning`) |
| `APPLE_ID` | the Apple ID of the developer account (for notarization) |
| `APPLE_TEAM_ID` | the 10-character team ID |
| `APPLE_APP_PASSWORD` | an app-specific password for that Apple ID (appleid.apple.com → Sign-In and Security) |

The first three turn on signing; the last three add notarization. Notarization needs a paid Apple Developer membership; a certificate of another kind can sign, but macOS will still warn on first launch.

## Platforms

| target | status |
|---|---|
| macOS arm64 | released: `agentctl` and the menu bar app |
| macOS amd64 | `agentctl` compiles; the app needs a universal Flutter build (blocked locally by Xcode 27's `lipo`, fine on CI's Xcode) |
| Linux amd64 / arm64 | built from source by `install.sh` (CI checks `agentctl` and the tray app build): a systemd user unit for the daemon, the tray app with `--app`. No release archives yet; terminal focus still needs iTerm |
| Windows | not yet: agents run in tmux, which Windows lacks (WSL would be the path), and a few Unix-only calls need ports |

Adding a target means a job beside `macos-arm64` in `.github/workflows/release.yml` that uploads its archives as an artifact, and a download of that artifact in `publish`, which puts them in the release and `SHA256SUMS`. Adding a board means a row in the `firmware` job's matrix (and in the board table above).
