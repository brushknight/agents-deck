# Releasing

## Versions

Releases are numbered with [semantic versioning](https://semver.org): `vMAJOR.MINOR.PATCH`, starting at
**v0.1.0**. While the major number is 0, a minor bump (`v0.2.0`) may change things you notice (the API,
Deck Link, how boards pair); a patch (`v0.1.1`) only fixes. A tag with a suffix (`v0.2.0-rc.1`) is a
pre-release: the same build, for trying a release before it's the latest. One version covers
everything in the release: `agentctl`, the menu bar app and every board's firmware are built from the
same tag.

## What a release contains

`<v>` is the version without the `v` (`0.2.0`). Architectures use Go's names: `arm64`, `amd64`.

| file | what |
|---|---|
| `agents-deck_<v>_macos_universal.dmg` | the menu bar app for people: `agents deck.app` (Apple Silicon and Intel in one, with agentctl and the Bluetooth helper `deck-ble` inside) and a link to Applications |
| `agents-deck_<v>_macos_arm64.zip`, `…_macos_amd64.zip` | the same app, zipped: what `install.sh --app` and the updater fetch. One universal app under both names |
| `agents-deck_<v>_linux_<arch>.deb` | for Ubuntu 24.04 and Debian 13 or later, x64 and arm64: the tray app and agentctl, a systemd user service, an app menu entry and a udev rule for the boards' USB ports |
| `agents-deck_<v>_linux_<arch>.tar.gz` | the tray app's folder (`agents-deck/`, with agentctl in it and the service, udev rule and desktop entry in `share/`): what `install.sh --app` and the updater put in `~/.local/share/agents-deck/app` |
| `agents-deck_<v>_windows_amd64_setup.exe` | the Windows installer: per user, no administrator prompt (a preview: see below) |
| `agentctl_<v>_<os>_<arch>.tar.gz` | agentctl on its own, for `darwin` and `linux`, `arm64` and `amd64`; Windows has `agentctl_<v>_windows_amd64.zip` |
| `agents-deck-<board>-<v>.bin` | a board's firmware, one image flashed at `0x0`: bootloader, partition table, `boot_app0` and the app |
| `agents-deck-<board>-<v>.app.bin` | the app on its own, which the deck sends to a board over Wi-Fi to update it (boards with Wi-Fi only) |
| `agents-deck-<board>-<v>.manifest.json` | an [ESP Web Tools](https://esphome.github.io/esp-web-tools/) manifest for that image |
| `firmware-manifest.json` | every board: name, chip, the types it reports, its USB ids, flash size, Wi-Fi, picture, its image (file, offset, size, sha256) and its over-the-air image |
| `SHA256SUMS` | checksums of all of these |
| `SHA256SUMS.sig` | our signature on `SHA256SUMS` (Ed25519, base64). agentctl checks it before it installs or flashes anything it downloaded; `install.sh` checks it when an agentctl already installed can |

v0.1.x releases have only `agentctl_<v>_darwin_arm64.tar.gz`, `agents-deck_<v>_macos_arm64.zip`, the
firmware files and `SHA256SUMS`; their names are unchanged.

`<board>` is the board's folder under `firmware/devices/`, and its image is built from the firmware
[docs/flashing.md](flashing.md) recommends:

| board | project | env | framework |
|---|---|---|---|
| `waveshare-touch-lcd-28c` | `waveshare-touch-lcd-28c/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-lcd-21` | `waveshare-touch-lcd-21/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-amoled-18` | `waveshare-touch-amoled-18/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `lilygo-t4-s3` | `lilygo-t4-s3/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-touch-lcd-4` | `waveshare-touch-lcd-4/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB and Wi-Fi |
| `waveshare-lcd-147` | `waveshare-lcd-147/idf5` | `idf5` | arduino-esp32 3.x (pioarduino), USB only: its firmware has no Wi-Fi |

What the deck knows about each board comes from `firmware/devices/<board>/board.json`:

```json
{ "name": "LilyGO T4-S3", "chip": "esp32s3",
  "types": ["t4s3-panel"],
  "usb": [{"vid": "303a", "pid": "1001"}],
  "flash_size": 16777216, "wifi": true,
  "picture": "docs/images/devices/t4s3-home.png" }
```

`types` lists every `Hello.type` the board's firmware reports, older names kept for compatibility
included (the T4-S3 still says `t4s3-panel`, the name of its earlier firmware). `usb` is how the deck
recognises the board on a USB port (`303a:1001` is the ESP32-S3's own USB). `wifi` says whether the
released build has Wi-Fi, which is also what makes a board get an over-the-air image. The release
script fails when a board has no `board.json` or its `name` or flash size disagree with the build.

Flashing one by hand: [docs/flashing.md](flashing.md#from-a-release-image).

## Cutting a release

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The `release` workflow builds everything for that tag, creates the GitHub release (with generated
notes) and attaches the files above. Its jobs:

| job | runner | makes |
|---|---|---|
| `macos` | `macos-15` | agentctl for arm64 and amd64 (and both in one, inside the app); the Bluetooth helper `deck-ble` (`clients/ble/macos`, `swift build -c release`, universal; CI runs its `swift test`); the universal app, signed from the inside out by `packaging/macos/package.sh --deck-ble`, notarized when the Apple secrets are set; the `.dmg` and the `.zip` |
| `linux` | `ubuntu-24.04`, `ubuntu-24.04-arm` | agentctl, the tray app, the `.deb` and `.tar.gz` (`packaging/linux/package.sh`); then installs the `.deb` on the runner and checks it (`packaging/linux/check-deb.sh`) |
| `windows` | `windows-latest` | `agentctl.exe` and the installer (`packaging/windows/agents-deck.iss`, Inno Setup). Skipped with a warning while the backend doesn't build for Windows |
| `firmware` | `ubuntu-24.04`, pinned container | one job per board, see below |
| `firmware-index` | `ubuntu-24.04` | `firmware-manifest.json` |
| `publish` | `ubuntu-latest` | `SHA256SUMS`, `SHA256SUMS.sig` (signed with `agentctl release-key sign`), the GitHub release |

`publish` waits for `macos` and the firmware. If `linux` or `windows` fails, the release still goes
out, without that platform's files, and the run shows a warning: fix it and run the workflow again
for the same tag. Publishing a release from the GitHub UI works too: the workflow
fills that release. To rebuild an existing tag, run the workflow by hand (Actions → release → Run
workflow). Before the first real one, a pre-release tag (`v0.1.0-rc.1`) runs the whole thing end to
end.

Before tagging: `main` is green in `ci`, and each board you changed was flashed from its `idf5` project and
checked by eye.

Every push to `main` and every pull request runs `ci`: Go vet and tests, cross-compiling for every
release target, a Windows build and test of the service (its tests may still fail without failing the
run while the Windows port settles), the Flutter tests of the deck UI and the menu bar app, a Linux
build of the tray app with a headless smoke test (`clients/macos/linux/test/smoke.sh`), each package
built unsigned and installed on a clean runner (`.deb`, `.dmg`, the Windows installer), the release
script's tests, and the firmware host tests and default builds.

## How the firmware images are built

The `firmware` job in `.github/workflows/release.yml` runs once per board, in a pinned container
(`python:3.12.15-slim-bookworm`) with pinned tools: [pioarduino's PlatformIO
Core](https://github.com/pioarduino/platformio-core) 6.2.0 (the Core pioarduino's platform is made
for) and `esptool==5.6.0`.

1. `pio run` builds the board's env, with `AGENTS_DECK_VERSION` set to the release's version (the
   firmware reports it in Hello). `idf5` projects use a PlatformIO home of their own
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
3. For a board with Wi-Fi it copies the app out as `agents-deck-<board>-<v>.app.bin`, the
   over-the-air image, and checks it: an ESP32-S3 app image with its app description, small enough
   for the smallest OTA slot, and a partition table with two of them.
4. It writes the board's ESP Web Tools manifest (with an `agents_deck` block that ESP Web Tools
   ignores: board, chip, file, offset, size, sha256, env, and the fields from `board.json` and the
   over-the-air image), and `firmware-index` collects them all into `firmware-manifest.json`.

`python3 -m unittest discover -s .github/scripts -p 'test_*.py'` tests the script on a made-up build
(needs `esptool`).

To make the same image on your Mac from a build you already have (no upload):

```bash
pio run -d firmware/devices/waveshare-touch-lcd-28c/idf5 -e idf5
pip install esptool    # or use a venv
python3 .github/scripts/firmware_release.py build --board waveshare-touch-lcd-28c \
  --dir firmware/devices/waveshare-touch-lcd-28c/idf5 --env idf5 --version 0.0.0-dev --out dist
```

(`build` runs `pio run` again, which is quick once built; `merge --image 0x0=… --image 0x8000=…` merges
files you name instead.)

To get that build onto your boards in one click, give it a version newer than the one they run (such
as `--version 0.2.0-dev.7`; the firmware reports it) and add the folder to agents deck:
`agentctl firmware add dist`. Each board of that kind that runs an older version then shows
**0.2.0-dev.7 built on this computer** with an **Install** button in Settings, Boards, over USB or
Wi-Fi, also in a copy built from source without the release key
([flashing.md](flashing.md#firmware-built-on-this-computer)).

### The web flasher

The [web flasher](https://agents-deck.sh/#boards) flashes these images from the browser (Chrome or Edge, over Web Serial):
pick a board, connect, install. It reads `firmware-manifest.json` from the latest release to list
the boards, and hands the board's ESP Web Tools manifest to the installer.

## Signing

Every signature is optional: each one turns on when its secrets exist (Settings, then Secrets and
variables, then Actions), and the run says in a warning which ones were missing.

### The release signature

`SHA256SUMS.sig` is our Ed25519 signature on `SHA256SUMS`. agentctl has the public key built in
(`backend/internal/update/release.pub`) and installs updates and flashes firmware only from a release
whose signature matches. Without it, agentctl still says when there's a new version but installs
nothing from the network. Firmware from this computer still installs: a file picked with **Install a
firmware file...**, and builds added with `agentctl firmware add`
([flashing.md](flashing.md#firmware-built-on-this-computer)).

A build from source has the same public key in it, so it checks and installs our releases like a
released build does. It cannot make releases other copies accept: that takes the private key. A fork
that publishes its own releases makes its own key and puts its public half in `release.pub`; a build
with an empty `release.pub` installs nothing from the network.

The key was made on 20 November 2026; its public half is in `release.pub`. Making a new one (a fork,
or after the private key was lost or seen by someone else):

1. Make the pair without showing the private key on screen. `agentctl release-key new` prints both,
   so send each to its own file:

   ```sh
   ( umask 077
     out="$(agentctl release-key new 2>/dev/null)"
     printf '%s\n' "$out" | sed -n 's/^private: //p' > ~/.ssh/agents-deck-release-key
     printf '%s\n' "$out" | sed -n 's/^public:  *//p' > ~/.ssh/agents-deck-release-key.pub )
   ```

2. Store the private key as the repository secret `AGENTS_DECK_RELEASE_KEY` (Settings, Secrets and
   variables, Actions), without it passing through the terminal:
   `gh secret set AGENTS_DECK_RELEASE_KEY < ~/.ssh/agents-deck-release-key`. Keep the file as your copy
   outside GitHub, and back it up: without it no later release can be signed for the copies already
   installed.
3. Put the public key in `backend/internal/update/release.pub` and in `release_pub=` at the top of
   `install.sh`, and merge that before the next release.

Changing the key cuts off updates for every copy built with the old one: those have to be updated by
hand once.

`publish` signs with `agentctl release-key sign dist/SHA256SUMS` and checks the signature against
`release.pub`, so a key that doesn't match the code fails the release instead of shipping.

| secret | value |
|---|---|
| `AGENTS_DECK_RELEASE_KEY` | the private key from `agentctl release-key new` |

### macOS

Without signing secrets, builds are ad-hoc signed: they run, but macOS warns the first time and treats every build as a new app (permission prompts come back after each update). With a Developer ID certificate, agentctl and every piece of the app are signed with it (hardened runtime, secure timestamp), and the app and the `.dmg` are notarized and stapled, so macOS opens them without a warning.

| secret | value |
|---|---|
| `MACOS_CERT_P12` | your **Developer ID Application** certificate with its private key, exported from Keychain Access as .p12, base64-encoded: `base64 -i cert.p12 \| pbcopy` |
| `MACOS_CERT_PASSWORD` | the password you gave the .p12 export |
| `MACOS_SIGNING_IDENTITY` | the identity name, e.g. `Developer ID Application: Your Name (TEAMID)` (`security find-identity -v -p codesigning`) |
| `APPLE_ID` | the Apple ID of the developer account (for notarization) |
| `APPLE_TEAM_ID` | the 10-character team ID |
| `APPLE_APP_PASSWORD` | an app-specific password for that Apple ID (appleid.apple.com → Sign-In and Security) |

The first three turn on signing; the last three add notarization. Notarization needs a paid Apple Developer membership; a certificate of another kind can sign, but macOS will still warn on first launch.

To package on your Mac (ad hoc, no secrets needed):

```bash
(cd clients/macos && flutter build macos --release)
packaging/macos/package.sh --app "clients/macos/build/macos/Build/Products/Release/agents deck.app" \
  --agentctl ~/.local/bin/agentctl --version 0.0.0-dev --arch arm64 --out dist
```

### Windows

Unsigned for now: Windows shows "Windows protected your PC" and the installer runs after More info,
then Run anyway. To sign with a code-signing certificate kept as a file, add:

| secret | value |
|---|---|
| `WINDOWS_CERT_PFX` | the certificate with its private key, .pfx, base64-encoded |
| `WINDOWS_CERT_PASSWORD` | its password |

`packaging/windows/sign.ps1` then signs `agentctl.exe`, the app and the installer with `signtool`. A
certificate on a hardware token, a cloud key service or a signing service (SignPath) replaces that
script at the same two places in the `windows` job.

## Platforms

| target | status |
|---|---|
| macOS arm64 and amd64 | released: agentctl, and the menu bar app as one universal app. Building it universal on your own Mac is blocked by Xcode 27's `lipo`; CI's Xcode is fine |
| Linux amd64 and arm64 | released: `.deb`, the app folder, agentctl (`install.sh` installs from them; `--source` still builds). Terminal focus still needs iTerm |
| Windows amd64 | preview: agentctl and a per-user installer, not yet tried on a real Windows PC; the app joins when the Flutter project has a Windows runner. Agents run in tmux, which Windows lacks |

Adding a target means a job in `.github/workflows/release.yml` that uploads its files as an artifact,
and a download of that artifact in `publish`, which puts them in the release and `SHA256SUMS`. Adding
a board means a row in the `firmware` job's matrix, a row in the board table above and its
`board.json`.
