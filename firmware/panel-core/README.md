# panel-core

The shared core of the agents deck panels: one firmware core, a layout per screen. A device folder
under [`../devices`](../devices) holds only its board's drivers (display, touch, backlight) in a
`main.cpp` and picks a layout.

| board | layout | folder |
|---|---|---|
| Waveshare ESP32-S3-LCD-1.47B, 320×172 | `layout_147.h`: one agent in detail and a mini-board | `devices/waveshare-lcd-147` |
| Waveshare ESP32-S3-Touch-LCD-4, 480×480 | `layout_grid.h`, `layoutLcd4()`: a 4×4 grid by slot, page bars | `devices/waveshare-touch-lcd-4` |
| Waveshare ESP32-S3-Touch-AMOLED-1.8, 368×448 | `layout_grid.h`, `layoutAmoled18()`: a 2×2 grid, header and page dots | `devices/waveshare-touch-amoled-18` |

## What's here

| file | what |
|---|---|
| `src/deck_model.h` | Deck Link frames (from the [kit](../deck-link)) into the `Fleet`; Hello and Subscribe |
| `src/model.h` | `Fleet`, `Agent`, `LinkState` |
| `src/view.h` | hero choice, button hold, poses, status and activity lines |
| `src/canvas.h`, `src/font_data.h` | an RGB565 framebuffer; Roboto Mono in five sizes (`tools/make_font.py`) |
| `src/sprites.h` | the critters (block and onigiri), 16 poses, 13 hats, at any whole-number scale |
| `src/deck_ui.h` | the deck's palette, how a status paints a tile, the tile's status line, text helpers |
| `src/panel_layout.h` | the touch layout interface (`PanelLayout`), its UI state, gestures |
| `src/layout_147.h` | the 1.47" layout |
| `src/layout_grid.h` | the grid layout (`GridPanel`) and its card |
| `src/grid_app.h` | the firmware of a touch board minus its drivers (Arduino) |
| `sim/` | `render.cpp` (every screen of a layout to PPM), `panel_sim.cpp`, `fixture.h` (host only) |
| `tests/run.sh` | the host tests, with ASan and UBSan |

Everything but `grid_app.h` is plain C++ with no Arduino code, so the same drawing and touch logic
runs in the tests and in `sim/render`.

## Adding a layout

Implement `PanelLayout` (`src/panel_layout.h`): `draw(canvas, input)` draws a whole frame from the
fleet, the link state and the UI state; `tap`, `swipe` and `button` update the UI state, and `tap` may
return an Answer for the board to send. A device passes its layout to `gridAppSetup()` in its
`GridBoard`, with its display, touch and backlight functions. Add the layout to `sim/render.cpp` so
its screens can be seen without the board, and test its hit-testing in `tests/`.

## Tests and pictures

```bash
tests/run.sh
c++ -std=c++17 -Isrc -I../deck-link/src -I../deck-link/src/proto -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 sim/render.cpp -o out/render
mkdir -p out/lcd4 && out/render ../../docs/fixtures/state.json out/lcd4 300 lcd4      # or amoled18, 147
python3 tools/ppm2png.py out/grid.png out/lcd4/grid.ppm                            # needs pillow
```

## Licences

The bitmap fonts in `src/font_data.h` are rendered from Roboto Mono (SIL Open Font License 1.1,
`OFL-robotomono.txt`).
