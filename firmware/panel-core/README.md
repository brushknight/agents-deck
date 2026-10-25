# panel-core

The shared core of the agents deck panels: one firmware core, three layout families. Small
displays use `layout_square.h`, big panels use `layout_big.h`, and round boards use
`layout_orbit.h` / `layout_dial.h`. A device folder
under [`../devices`](../devices) holds only its board's drivers (display, touch, backlight) in a
`main.cpp` and picks a layout.

| board | layout | folder |
|---|---|---|
| Waveshare ESP32-S3-LCD-1.47B, 320×172 | `layout_147.h`: one agent in detail and a mini-board | `devices/waveshare-lcd-147` |
| Waveshare ESP32-S3-Touch-LCD-4, 480×480 | `layout_big.h`, `specBig480()`: the Flutter desk panel's UI at 2/3 | `devices/waveshare-touch-lcd-4` |
| Waveshare ESP32-S3-Touch-AMOLED-1.8, 368×448 | `layout_square.h`, `specAmoled18()`: 2×2 tiles in one scrolling list | `devices/waveshare-touch-amoled-18` |
| LilyGO T4-S3, 2.41″ AMOLED, 450×600 | `layout_square.h`, `specT4S3()`: the 1.8″'s UI with 2×3 tiles on screen, bigger critters, the card's numbers one to a line | `devices/lilygo-t4-s3` |
| Freenove ESP32-S3 Display 2.8″ (FNK0104B), 240×320 | `layout_square.h`, `specFreenove28()`: the 1.8″'s UI at half size, 2×2 tiles on screen | `devices/freenove-s3-28` |
| Waveshare ESP32-S3-Touch-LCD-2.8C, 480×480 round | `layout_spring.h`, `layoutSpring28()`: the spring carousel, nine to a turn | `devices/waveshare-touch-lcd-28c` |
| Waveshare ESP32-S3-Touch-LCD-2.1, 480×480 round | `layout_spring.h`, `layoutSpring21()`: the spring carousel, seven to a turn | `devices/waveshare-touch-lcd-21` |

## What's here

| file | what |
|---|---|
| `src/deck_model.h` | Deck Link frames (from the [kit](../deck-link)) into the `Fleet`; Hello and Subscribe |
| `src/model.h` | `Fleet`, `Agent`, `LinkState` |
| `src/view.h` | hero choice, button hold, poses, status and activity lines |
| `src/canvas.h`, `src/font_data.h` | an RGB565 framebuffer; Roboto Mono in five sizes (`tools/make_font.py`) |
| `src/font_square.h` | the square boards' 16, 22, 24 and 28 px sizes (`tools/make_font_square.py`) |
| `src/sprites.h` | the critters (block and onigiri), 16 poses, 13 hats, at any whole-number scale |
| `src/deck_ui.h` | the deck's palette, how a status paints a tile, the tile's status line, text helpers |
| `src/deck_cmd.h` | the board's scope and the commands it sends (Answer, Focus, Interrupt, Dismiss), shared by square and round |
| `src/ui_buttons.h` | the card's round icon buttons (focus, interrupt, stop with its hold ring), shared by square and round |
| `src/panel_layout.h` | the square boards' layout interface (`PanelLayout`) and UI state: screens, scroll, the finger |
| `src/layout_147.h` | the 1.47" layout |
| `src/layout_big.h`, `src/font_big.h` | the big panel layout (`BigPanel`): the Flutter desk panel (grid, card, prompts) at a scale (`BigSpec`; 2/3 for 480×480); its fonts (`tools/make_font_big.py`) |
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
hit-testing follows it. The 4″, the 1.8″ AMOLED and the T4-S3 have `idf5` projects; the 1.47″ is on 2.0.17 and
USB only.

## Tests and pictures

```bash
tests/run.sh
c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 sim/render.cpp -o out/render
mkdir -p out/lcd4 && out/render ../../docs/fixtures/state.json out/lcd4 300 lcd4      # or amoled18, t4s3, freenove28, 147, orbit, dial
python3 tools/ppm2png.py out/grid.png out/lcd4/grid.ppm                            # needs pillow
tools/render_docs.sh                                                               # every device's docs/
```

## Licences

The bitmap fonts in `src/font_data.h` and `src/font_round.h` are rendered from Roboto Mono (SIL Open Font License 1.1,
`OFL-robotomono.txt`).
