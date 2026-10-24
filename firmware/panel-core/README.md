# panel-core

The shared core of the agents deck panels: one firmware core, a layout per screen. A device folder
under [`../devices`](../devices) holds only its board's drivers (display, touch, backlight) in a
`main.cpp` and picks a layout.

| board | layout | folder |
|---|---|---|
| Waveshare ESP32-S3-LCD-1.47B, 320×172 | `layout_147.h`: one agent in detail and a mini-board | `devices/waveshare-lcd-147` |
| Waveshare ESP32-S3-Touch-LCD-4, 480×480 | `layout_square.h`, `specLcd4()`: the 4×4 grid by slot, paged | `devices/waveshare-touch-lcd-4` |
| Waveshare ESP32-S3-Touch-AMOLED-1.8, 368×448 | `layout_square.h`, `specAmoled18()`: 2×2 tiles in one scrolling list | `devices/waveshare-touch-amoled-18` |
| Waveshare ESP32-S3-Touch-LCD-2.8C, 480×480 round | `layout_orbit.h`: every agent on a ring, the one that matters in the middle | `devices/waveshare-touch-lcd-28c` |
| Waveshare ESP32-S3-Touch-LCD-2.1, 480×480 round | `layout_dial.h`: one agent at a time, the fleet as dots | `devices/waveshare-touch-lcd-21` |

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
| `src/layout_square.h` | the square boards' layout (`SquarePanel`), one system with per-board numbers (`SquareSpec`): home, card, prompt, notices, touch and scrolling |
| `src/grid_app.h` | the firmware of a touch board minus its drivers (Arduino) |
| `src/round_geom.h` | circle geometry: polar placement, arcs, discs, pills, the round mask, hit tests, the rim drag |
| `src/round_view.h` | the round boards' screens and touch: home, detail, prompt, featured agent, timeout (`RoundLayout`) |
| `src/round_ui.h`, `src/font_round.h` | what Orbit and Dial share: notices, the detail, the prompt; 30 and 35 px fonts (`tools/make_font_round.py`) |
| `src/layout_orbit.h`, `src/layout_dial.h` | the round layouts (`OrbitPanel`, `DialPanel`) |
| `src/round_app.h` | the firmware of a round board minus its drivers (Arduino) |
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

## Tests and pictures

```bash
tests/run.sh
c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 sim/render.cpp -o out/render
mkdir -p out/lcd4 && out/render ../../docs/fixtures/state.json out/lcd4 300 lcd4      # or amoled18, 147, orbit, dial
python3 tools/ppm2png.py out/grid.png out/lcd4/grid.ppm                            # needs pillow
tools/render_docs.sh                                                               # every device's docs/
```

## Licences

The bitmap fonts in `src/font_data.h` and `src/font_round.h` are rendered from Roboto Mono (SIL Open Font License 1.1,
`OFL-robotomono.txt`).
