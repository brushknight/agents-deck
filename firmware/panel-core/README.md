# panel-core

The shared core of the agents deck panels: one firmware core, three layout families. Small
displays use `layout_square.h`, big panels use `layout_big.h`, and round boards use
`layout_orbit.h` / `layout_dial.h`. A device folder
under [`../devices`](../devices) holds only its board's drivers (display, touch, backlight) in a
`main.cpp` and picks a layout.

| board | layout | folder |
|---|---|---|
| Waveshare ESP32-S3-LCD-1.47B, 320×172 | `layout_147.h`: one agent in detail and a mini-board | `devices/waveshare-lcd-147` |
| Waveshare ESP32-S3-Touch-LCD-4, 480×480 | `layout_big.h`, `specBig480()`: the Flutter desk panel's grid at 2/3; the boards' own card and prompt | `devices/waveshare-touch-lcd-4` |
| Waveshare ESP32-S3-Touch-AMOLED-1.8, 368×448 | `layout_square.h`, `specAmoled18()`: 2×2 tiles in one scrolling list | `devices/waveshare-touch-amoled-18` |
| LilyGO T4-S3, 2.41″ AMOLED, 450×600 | `layout_square.h`, `specT4S3()`: the 1.8″'s UI with 2×3 tiles on screen, bigger critters, the card's numbers one to a line | `devices/lilygo-t4-s3` |
| Waveshare ESP32-S3-Touch-LCD-2.8C, 480×480 round | `layout_spring.h`, `layoutSpring28()`: the spring carousel, nine to a turn | `devices/waveshare-touch-lcd-28c` |
| Waveshare ESP32-S3-Touch-LCD-2.1, 480×480 round | `layout_spring.h`, `layoutSpring21()`: the spring carousel, seven to a turn | `devices/waveshare-touch-lcd-21` |

## What's here

| file | what |
|---|---|
| `src/deck_model.h` | Deck Link frames (from the [kit](../deck-link)) into the `Fleet`; Hello and Subscribe |
| `src/model.h` | `Fleet`, `Agent`, `LinkState` |
| `src/view.h` | hero choice, button hold, poses, status and activity lines |
| `src/attention.h` | the "Needs you" screen: the deck's spots in this screen's cells (host-tested, no display code) |
| `src/canvas.h`, `src/font_data.h` | an RGB565 framebuffer; Roboto Mono in five sizes (`tools/make_font.py`) |
| `src/font_square.h` | the square boards' 16, 22, 24 and 28 px sizes (`tools/make_font_square.py`) |
| `src/sprites.h` | the critters (block and onigiri), 16 poses, 13 hats, at any whole-number scale; an agent's pet in their place (`drawCritter`) |
| `src/deck_ui.h` | the deck's palette, how a status paints a tile, the tile's status line, text helpers |
| `src/deck_cmd.h` | the board's scope and the commands it sends (Answer, Focus, Interrupt, Dismiss), shared by square and round |
| `src/ui_buttons.h` | the card's round icon buttons (focus, interrupt, stop with its hold ring), shared by square and round |
| `src/panel_layout.h` | the square boards' layout interface (`PanelLayout`) and UI state: screens, scroll, the finger |
| `src/layout_147.h` | the 1.47" layout |
| `src/layout_big.h`, `src/font_big.h` | the big panel layout (`BigPanel`): the Flutter desk panel's grid at a scale (`BigSpec`; 2/3 for 480×480), and the agent's card and prompt in the boards' design (sharp corners, the pet on its baseline, square buttons; an egg ready to hatch says "tap to hatch"); its fonts (`tools/make_font_big.py`) |
| `src/layout_square.h` | the square boards' layout (`SquarePanel`), one system with per-board numbers (`SquareSpec`): home, card, prompt, notices, touch and scrolling |
| `src/grid_app.h` | the firmware of a touch board minus its drivers (Arduino) |
| `src/round_geom.h` | circle geometry: polar placement, arcs, discs, pills, the round mask, hit tests, the rim drag |
| `src/round_view.h` | the round boards' screens and touch: home, detail, prompt, featured agent, timeout (`RoundLayout`) |
| `src/round_ui.h`, `src/font_round.h` | what the round layouts share: notices (the Wi-Fi ones too), toasts, the detail, the prompt; 30 and 35 px fonts (`tools/make_font_round.py`) |
| `src/layout_spring.h`, `src/spring.h` | the round boards' home, the spring carousel (`SpringPanel`; `layoutSpring21()`, `layoutSpring28()`), drawn in strips |
| `src/layout_orbit.h`, `src/layout_dial.h` | the earlier round homes (`OrbitPanel`, `DialPanel`), behind `ROUND_ORBIT_CLASSIC` / `ROUND_DIAL_CLASSIC` |
| `src/round_app.h` | the firmware of a round board minus its drivers (Arduino) |
| `src/net_model.h` | USB or Wi-Fi: which link a board uses and when it retries, and what the screen says about it |
| `src/net_link.h` | Wi-Fi for any touch board (Arduino, ESP32): pairing over USB, credentials and certificate in NVS, joining, the TLS 1.3 link to the deck |
| `src/ota_model.h`, `src/ota.h` | firmware updates over Wi-Fi: the words on the screen, when to tell the deck, the boot-time decision (host-tested); the download into the other app slot and the confirm or roll back (Arduino, ESP32, idf5 builds) |
| `tools/build_info.py` | the PlatformIO pre-script every device project uses: `AGENTS_DECK_BOARD` (the device folder) and `AGENTS_DECK_VERSION` (from the environment) |
| `sim/` | `render.cpp` (every screen of a layout to PPM), `panel_sim.cpp`, `fixture.h` (host only) |
| `tests/run.sh` | the host tests, with ASan and UBSan |

Everything but `grid_app.h` and `round_app.h` is plain C++ with no Arduino code, so the same drawing and touch logic
runs in the tests and in `sim/render`.

## Adding a layout

A new square board usually needs only a `SquareSpec` (`src/layout_square.h`): its size, margin,
fonts, critter scales and the spec's measurements, and whether its home is the grid or the scrolling
list. For something else, implement `PanelLayout` (`src/panel_layout.h`):
- `draw(canvas, input)` draws a whole frame from the fleet, the link state and the UI state.
- `touch` takes raw touch samples and returns the command to send.
- `tick` runs animations.
- `button` handles BOOT, and `result` takes the deck's replies.

A device passes its layout to `gridAppSetup()` in its `GridBoard`, with its display, touch and
backlight functions. Add the layout to `sim/render.cpp` so its screens can be seen without the board,
and test its hit-testing in `tests/`.

The round boards have their own interface, `RoundLayout` (`src/round_view.h`): a layout draws the
frame and says what lies under a point (`hit`), and the shared state machine does the rest (taps,
swipes, drags round the rim, the stop hold, the timeout, a new waiting agent). Its devices call
`roundAppSetup()` with a `RoundBoard`.

## Needs you

The square boards have the deck's "Needs you" screen (docs/features.md). The deck decides who is on
it and in which of its 16 spots (docs/protocol.md, "The Needs you grid"), and sends each agent's
spot in `attention_slot`, so every board shows the same as the panel and the browser. `attention.h`
only places them (`tests/test_attention.cpp`): on a 4×4 screen spot n goes in cell n; a screen with
fewer cells shows the agents in order of their spot, lowest first, and leaves out the ones that
don't fit (the count on the mark still includes them). An older deck sends no spots: then the agents
that need you (running, starting, waiting, or idle and unseen) fill the cells in deck order, with no
5 s stay. `attSync()` works it out every frame (one pass over the fleet). A layout gets it through
`PanelUi::att` (`grid_app.h` allocates it; `nullptr` means no screen) and shows it while
`PanelUi::needsYou`; the board starts with it off. The board keeps its `kMaxAgents` lowest slots, so
an agent beyond those isn't shown or counted there.

| layout | spots | getting there | where you are |
|---|---|---|---|
| `layout_big.h` (4″) | 4×4, the deck's spots as they are | swipe right (or down) on the first page, tap the mark, BOOT after the last page | a mark at the left of the page bars (`_PageBars`): solid orange there, an orange outline and the count while someone is on it; the bars' row now stays with one page |
| `layout_square.h`, grid | a page's cells (4×4: as they are; fewer: lowest spots first) | the same | the same mark among the page bars |
| `layout_square.h`, list (1.8″, T4-S3) | what the list shows at once: 2×2, 2×3, lowest spots first; no scrolling | swipe right on the list, or BOOT; swipe left or BOOT back | two small marks at the top of the header |
| `layout_147.h` (1.47″) | the mini-board's 8 cells, lowest spots first | hold BOOT 0.7 s (a short press still shows the next agent) | a thin orange line over the mini-board |

A tile there does what it does on any page. The round boards (spring carousel, Orbit, Dial) don't
have it: their home is a ring with no first page to sit left of.

## Agent pets

While Agent pets is on (the deck's Settings, General), every agent comes with its pet
(`Agent::pet`, the kit's [`deck_pet.h`](../deck-link/README.md#agent-pets)), and every board draws the
pet where the critter would be: `drawCritter()` in `sprites.h` draws the pet when there is one and
the critter otherwise, so with pets off every screen is as before. The pet does the critter's pose
(`poseOf()`), with its bubble where the critter has one, its black stroke, and its flames when it is
strong, mighty or legend; on orange it keeps its own colours. One pet cell is one sprite unit at the
critter's scale; the pet's middle is the body's, and a grown-up's feet are where the critter's are. Its
hat rises a unit higher than a critter's (the 1.47" hero stands it a unit lower), and its bubbles, flag
and flames reach further (the kit's `DECK_PET_REACH_*`): the spring carousel's critter box grows to
match (`critterBox()`), so the partial redraw and the strips still cover it. An egg waiting to be seen
rocks. The kit keeps its scratch and `drawPet()` its look in static memory, so pets are drawn from one
task.

The screens that show one agent in detail show the pet's name, stage and level (`petLine()`: "Pip
· Kid · level 7", or "Egg"):

| layout | where |
|---|---|
| `layout_147.h` | the hero's bottom line, in place of turns and cost |
| `layout_square.h` (card) | under the name and the pill |
| `layout_big.h` (card) | the pet itself, with hearts, level and the level bar beside it |
| `round_ui.h` (detail) | the third line, in place of model, effort and context (the ring still shows context) |

`tests/test_pets.cpp` checks it; `sim/render.cpp` draws every screen with pets when its last
argument is `pets`, and `pets.ppm`, the kit's pets on dark and on orange in a few poses.

## Wi-Fi

`net_link.h` gives a board a second way to the deck (docs/link/protocol.md, "Pairing and Wi-Fi"):
it pairs over USB by itself, keeps Wi-Fi credentials sent with `agentctl link wifi`, and when no deck
answers on USB it joins the network and runs a second Deck Link core over TLS 1.3 (the kit's
`deck_tls.h`). The app follows `net_link::active()` and sends its commands there; a deck on USB always
wins. `round_app.h` and `grid_app.h` wire it the same way (the Welcome, CertIssued and Result hooks,
`active()` for commands and state, a fresh fleet when the link changes, toasts); the round and square
boards' `idf5` builds have it.

On the round screens the Wi-Fi notices are `round_ui.h`'s `drawNotice()` (`netNotice()` words), and
home on Wi-Fi has a small Wi-Fi mark: the spring draws it as one of its elements under the context
bar (`SpringGeom.netBase`), so it comes and goes with the partial redraw and the strips; Dial and Orbit
draw it in `drawRoundOverlay()`. Toasts stay inside the circle (`toastBox()`); on the spring they keep
below the context bar, and a frame with a toast is drawn whole.

It needs TLS 1.3 in mbedTLS, so arduino-esp32 3.x with its libraries rebuilt with
`CONFIG_MBEDTLS_SSL_PROTO_TLS1_3=y` (pioarduino's `custom_sdkconfig`, as in the round boards'
`idf5/platformio.ini`). On arduino-esp32 2.0.17 (ESP-IDF 4.4, mbedTLS 2.28) it compiles to the USB
part only and the deck hears "no Wi-Fi" (501).

On the square boards (`grid_app.h`) `PanelInput.net` carries the same `NetView`: `layout_square.h`
and `layout_big.h` draw the Wi-Fi notices with `netNotice()` in their own notice style, and a toast on
a notice sits low, clear of its text. `PanelUi::wifi` (set by `grid_app.h`) puts the Wi-Fi mark
(`drawNetMark()`, `round_geom.h`) on home: after "agents" in the list's header (the 1.8″ AMOLED and the T4-S3);
at the right of the page bars' row in a grid (the 4″), which then stays with one page too, and
hit-testing follows it. The 4″, the 1.8″ AMOLED and the T4-S3 have `idf5` projects; so does the 1.47″, which
stays USB only.

## Bluetooth

`net_link.h` also reaches the deck over Bluetooth LE (docs/link/protocol.md, "Bluetooth") on the
`idf5` builds: `ble_link.h` runs the GATT service on ESP-IDF's NimBLE (part of arduino-esp32 3.x's
ESP32-S3 libraries, nothing to add), pairs with LE Secure Connections and a passkey the screen shows,
and keeps one bond in NVS; a third Deck Link core runs on the kit's `deck_ble.h` stream. Over it the
board pairs for Wi-Fi and takes Wi-Fi credentials as over USB, so it can be set up without a cable.
`ble_model.h` holds the rules, host-tested in `tests/test_ble.cpp`: when it advertises (a bond
always: quickly while Bluetooth may carry the session, about once a second on standby while another
link does, so the deck can find it again), when Bluetooth carries the session (USB first, then the
deck's preference: `auto`, `wifi` or `ble`), the introduction (a central that has just encrypted
keeps its link for 20 s while another link carries the session, so the deck can add the board; the
board reports it in `NetStatus` there), and the gesture that forgets the bond (BOOT, or the screen
on a notice, held 5 s, then pressed again). In setup the board asks for no encryption itself:
pairing starts when the computer subscribes to TX, never for a scan that only reads Info. A passkey
accepted shows the toast "Bluetooth paired".

`NetView.via` is `NetVia::Ble` while Bluetooth carries the session or is where the board waits; the
notices (`netNotice()`) say "Bluetooth setup", "waiting for the deck / over Bluetooth", the passkey
(`pair deck-1a2b3c`, the six digits in the accent, "type it on the computer") and "forget
Bluetooth?". A passkey and "forget?" cover any screen (`netOverlay()`), so the apps draw them as a
notice and keep touches from the agents meanwhile. On home, `PanelUi::ble` swaps the Wi-Fi mark for
the Bluetooth one (`drawBleMark()`, `round_geom.h`); the spring draws it as its `El::Net` element.

Builds without NimBLE or TLS 1.3 (arduino-esp32 2.0.17) have `PANEL_HAS_BLE 0` and none of it;
`-DPANEL_NO_BLE` turns it off on an `idf5` build. Bluetooth stops while a firmware update downloads
(the TLS download wants the heap) and starts again after.

Checking it on a board (`idf5-debug` prints `ble:` and `net:` lines; nothing here runs in CI):

1. **Pair from a Mac.** A board never paired: it shows "Bluetooth setup / deck-xxxxxx" once USB is
   quiet. Connect from the deck's Bluetooth helper (or a GATT tool such as LightBlue): the board shows
   `pair deck-xxxxxx` and six digits; type them in the Mac's dialog. Info reads `paired=1` after.
   Typing a wrong passkey fails and leaves no bond. A second Mac can't pair while the first holds the
   bond, and the board never shows it a passkey. `agentctl bluetooth scan` alone never shows a
   passkey.
2. **Add while on USB.** Plugged in and live on USB: `agentctl bluetooth add deck-xxxxxx` shows the
   passkey stage, the board shows the code over its home screen, then "Bluetooth paired"; the job
   ends "added", and the debug log shows the link dropped about 20 s later at most (or as soon as the
   deck hangs up). `agentctl devices` lists bluetooth "standing by, it uses USB".
3. **First setup with no cable** (board on a charger): approve it on the deck; it pairs for Wi-Fi
   over Bluetooth (toast "paired for Wi-Fi"), takes the default network ("trying Wi-Fi…", "Wi-Fi
   works"), then with `auto` moves to Wi-Fi and lets Bluetooth go.
4. **Snapshot speed.** `uplink ble`, 32 agents with prompts: time from the board's Hello to the full
   home (the debug log and the deck's log); expect well under a second at 2M PHY and MTU 247.
5. **Hand-over.** `auto`: switch the Wi-Fi access point off: the board goes to Bluetooth within the
   Wi-Fi retry time; on again: back to Wi-Fi, never two sessions in the deck's log. Plug USB in: USB
   takes over at once; unplug: back after 30 s.
6. **Range and reconnect.** Walk away until the link drops, come back: it reconnects by itself
   (bonded advertising), with no passkey. Restart the board and the Mac: the same.
7. **Forget.** Hold BOOT 5 s, press again: "Bluetooth forgotten", Info `paired=0`, setup again. The
   Mac must forget the board too before pairing again.
8. **An update while Bluetooth is on.** On Wi-Fi with a bond, offer a firmware: Bluetooth stops for
   the download (`ble: stopped`), the update installs, and after the restart the bond still works.
   Watch the `heap … largest` line in the debug log during the download.

## Firmware updates over Wi-Fi

`ota.h` lets the deck update a board on Wi-Fi (docs/link/protocol.md, "Firmware updates over Wi-Fi");
a board on USB is updated over the cable. `net_link.h` gives both Deck Link cores the offer callback,
and `grid_app.h` and `round_app.h` show the update's notice (`netNotice()` words: "updating", the
version and percent, "keep it powered") instead of the agents while it runs, and ignore touches.

- **The offer.** Taken only over the Wi-Fi link, only when its `board` is this build's
  (`AGENTS_DECK_BOARD`), the other app slot exists and the image fits it, and no update is running or
  waiting for its confirm. Otherwise the board answers `REFUSED` with the reason.
- **The download.** A task of its own (12 KB of stack, core 0, like the link's TLS connect) runs the
  kit's `deck_fw_download()`: a second TLS 1.3 connection with the board's certificate to
  `GET /v1/firmware/{board}/{version}`, 4 KB at a time, SHA-256 as it goes, resumed with `Range`
  after a drop. `esp_ota_begin(OTA_WITH_SEQUENTIAL_WRITES)` erases the other slot as the image
  arrives and `esp_ota_write()` fills it, so RAM use stays at the one buffer plus mbedTLS's. The
  main loop sends `FirmwareStatus` every 5 % (or 3 s).
- **The switch.** `esp_ota_end()` checks it is a valid app for the chip; the SHA-256 must be the
  offer's; and the image must carry this board's mark (`agents-deck-board:<board>`, which every
  idf5 build has in its flash), so another board's build sent under this board's name is not
  installed. Only then the board writes an NVS mark (`decklink`/`ota`: the old slot's name), calls
  `esp_ota_set_boot_partition()`, says `RESTARTING` and restarts.
- **Confirm or go back.** The new firmware keeps itself only when the deck answers its `Hello` (a
  `Welcome` on either link), and shows "update installed".
  - arduino-esp32 3.x's prebuilt bootloader is built with `CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE=y`
    (the `sdkconfig` in pioarduino's `framework-arduinoespressif32-libs/esp32s3`, and the
    `ESP_OTA_IMG_PENDING_VERIFY` handling in its `bootloader_*.elf`). The idf5 builds use that
    bootloader as is (`custom_sdkconfig` rebuilds the libraries, not the bootloader), so a new image
    starts `PENDING_VERIFY` and the bootloader goes back to the old one if the board resets before
    the confirm: a crash, a watchdog, a power cut. `ota.h` defines `verifyRollbackLater()` so the
    Arduino core doesn't confirm the image at start-up, and gives up itself
    (`esp_ota_mark_app_invalid_rollback_and_reboot()`) after 5 minutes without a deck.
  - With a bootloader without rollback (the image's state stays `NEW`), the app does the same with
    the NVS mark: it counts starts (going back after the third without a deck) and the 5 minutes,
    and switches back with `esp_ota_set_boot_partition()`. The mark is cleared before it switches, so
    it never loops.
- **2.0.17 builds** (USB only) have no TLS 1.3: `firmwareOfferHook()` is `nullptr` and the kit
  answers every offer with `REFUSED` ("this firmware can't update over Wi-Fi").

The version a board reports in `Hello` is `AGENTS_DECK_VERSION` when the build has it, otherwise the
`FW_VERSION` in its `main.cpp` (`panelFirmware()` in `deck_model.h`). Every device project's
`platformio.ini` runs `tools/build_info.py`, which sets it from the environment variable of the same
name (the release workflow exports it) and sets `AGENTS_DECK_BOARD` from the project's folder:

```bash
AGENTS_DECK_VERSION=0.2.1 pio run -d firmware/devices/lilygo-t4-s3/idf5 -e idf5
```

## Tests and pictures

```bash
tests/run.sh
c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 sim/render.cpp -o out/render
mkdir -p out/lcd4 && out/render ../../docs/fixtures/state.json out/lcd4 300 lcd4      # or amoled18, t4s3, 147, orbit, dial (ble-*.ppm: Bluetooth)
python3 tools/ppm2png.py out/grid.png out/lcd4/grid.ppm                            # needs pillow
tools/render_docs.sh                                                               # every device's docs/
```

## Licences

The bitmap fonts in `src/font_data.h` and `src/font_round.h` are rendered from Roboto Mono (SIL Open Font License 1.1,
`OFL-robotomono.txt`).
