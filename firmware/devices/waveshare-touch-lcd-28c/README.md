# waveshare-touch-lcd-28c: Orbit, the deck on a round 2.8″ screen

Firmware for the [Waveshare ESP32-S3-Touch-LCD-2.8C](https://www.waveshare.com/wiki/ESP32-S3-Touch-LCD-2.8C)
(round 480×480 ST7701 on RGB, GT911 touch, ESP32-S3 with 8 MB PSRAM). It plugs into the Mac over USB
and shows the fleet through [Deck Link](../../../docs/link/protocol.md).

![screens](docs/screens.png)

> Not yet run on the board: everything here builds, and the screens above come from the host
> renderer, which draws the same pixels. See [Bring-up](#bring-up).

## What the screen shows

- **Home, the orbit.** Every agent is a critter on a ring near the edge, in deck slot order. The rim
  under each one is its status: a thin orange arc while it works, is hungry or failed; a thick orange
  bar when it needs you; grey when idle. The middle holds the agent that matters most: the one
  waiting longest, then hungry, then failed, then the busiest. It is circled on the ring too.
- **Someone waits.** The rim gets an orange halo, the waiter sits on an orange disc, and the middle
  shows its question: "tap to answer". A newly waiting agent wakes the screen to home with it in the
  middle; it never jumps straight into the prompt, so a stray touch can't answer.
- **More than 16 agents.** The ring shows 15 and a "+N" chip. Drag round the rim or tap the chip to
  page.
- **Detail** (tap a critter). The context bar runs over the top from 8 to 4 o'clock; then the critter,
  name, status and age, model and effort, and what it is doing. Round buttons on the bottom arc:
  focus (its terminal comes to the front), interrupt (while it runs) and stop (orange: hold it for a
  second while its ring fills). Swipe left or right for the neighbouring agent, down to go back.
- **Prompt** (tap the middle when it waits, or the detail's middle). The question and its answers as
  pills; the first answer is filled orange. A tap answers (`Answer` with the prompt's id), shows
  "sent" for a second, then goes back. More than three answers scroll with swipes up and down.
- After 30 s untouched it goes back home. The backlight dims after 2 minutes with nothing going on.
- Full-screen states: connecting, waiting for approval (with the exact `agentctl devices approve`
  command), deck offline (with when it was last heard), no agents.

`docs/` has every screen: `home-3`, `home-8`, `home-22`, `waiting`, `detail`, `detail-stop-held`,
`prompt`, `prompt-view`, `not-approved`, `offline`, `connecting`.

## Scope

New boards start at `view`: the board shows everything, the prompt reads "answer on the deck" and
the detail has no buttons. Give it `act` to answer prompts, focus and interrupt; `admin` adds stop
(which ends the agent and takes it off the deck):

```bash
agentctl devices scope waveshare-touch-lcd-28c-<id> act
```

## What it asks the deck for

- **Hello**: type `waveshare-touch-lcd-28c`, display 480×480 colour, input TOUCH, board id = the
  chip's MAC.
- **Subscribe**: every agent, fields `TILE | CARD | PROMPT | NUMBERS` (CARD for the detail's model,
  effort and activity). The board keeps up to 64 agents in PSRAM.
- **Commands**: `Answer`, `Focus`, `Interrupt` (scope act), `Dismiss` for stop (scope admin).

## Build, flash, connect

```bash
pio run -d firmware/devices/waveshare-touch-lcd-28c -t upload
agentctl link add /dev/cu.usbmodem1101        # the board's port: agentctl link ports
agentctl devices approve waveshare-touch-lcd-28c-<id>
```

`-e debug` builds the same firmware with one-line notes on the USB port (the deck ignores text).

## Pins

From Waveshare's demo for this board (`ESP32-S3-Touch-LCD-2.8C-Demo.zip` on the wiki,
`Arduino/examples/LVGL_Arduino`), checked against its ESP-IDF demo.

| part | GPIO |
|---|---|
| RGB bus | DE 40, VSYNC 39, HSYNC 38, PCLK 41; R 46 3 8 18 17; G 14 13 12 11 10 9; B 5 45 48 47 21 |
| ST7701 init (3-wire SPI, 9 bit) | SCK 2, SDA 1, CS on the expander's EXIO3 |
| I2C (expander, touch) | SDA 15, SCL 7 |
| GT911 touch | address 0x5D (INT held low during reset), INT 16, reset EXIO2 |
| TCA9554 expander (0x20) | EXIO1 LCD reset, EXIO2 touch reset, EXIO3 LCD CS, EXIO4 SD CS, EXIO8 buzzer |
| backlight | 6 (PWM, 20 kHz) |
| BOOT button | 0 (back home) |

RGB timing: hsync 8 / 10 / 50 (pulse, back, front), vsync 2 / 18 / 8, 16-bit RGB565, pixel clock
18 MHz. The panel's init sequence is Waveshare's, byte for byte (`src/main.cpp`).

## Bring-up

What still needs the real board:

- **Pixel clock.** Waveshare's Arduino demo runs 30 MHz with a bounce buffer (arduino-esp32 3.x).
  This build is on arduino-esp32 2.0.17 with Arduino_GFX 1.5.0, which has no bounce buffer, so it uses
  their ESP-IDF demo's 18 MHz. If the picture drifts or tears, lower it.
- **Orientation and colour order** of panel and touch (the demos map touch straight to pixels).
- **The expander's power-up**: the buzzer is on until EXIO8 goes low; the firmware does that first.
- Frame time on the chip (`-e debug` prints it every 5 s), touch feel (tap slop, swipe length, the
  25° rim drag, the 1 s hold), and the backlight levels.
- `agentctl link check` against the board.

## Tests and the host renderer

The layout is `panel-core/src/layout_orbit.h`; the detail, prompt and notices it shares with the
2.1″ Dial are in `round_ui.h`, the state machine in `round_view.h`, the circle geometry in
`round_geom.h`.

```bash
firmware/panel-core/tests/run.sh
cd firmware/panel-core
c++ -std=c++17 -DPANEL_MAX_AGENTS=64 -Isrc -I../deck-link/src -I../deck-link/src/proto sim/render.cpp -o out/render
mkdir -p out/orbit && out/render ../../docs/fixtures/state.json out/orbit 0 orbit
python3 tools/ppm2png.py out/orbit.png out/orbit/home-*.ppm --cols 3    # needs pillow
```
