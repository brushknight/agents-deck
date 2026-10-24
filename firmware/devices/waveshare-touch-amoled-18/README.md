# waveshare-touch-amoled-18: the deck on a 1.8″ AMOLED

The deck on a [Waveshare ESP32-S3-Touch-AMOLED-1.8](https://www.waveshare.com/esp32-s3-touch-amoled-1.8.htm)
(368×448 AMOLED, touch), over USB with [Deck Link](../../../docs/link/protocol.md).

| home | scrolling | card | permission | question | approval |
|---|---|---|---|---|---|
| <img src="docs/home.png" width="150" alt="2x2 tiles"> | <img src="docs/home-scrolling.png" width="150" alt="mid-scroll with the position bar"> | <img src="docs/card.png" width="150" alt="an agent's card"> | <img src="docs/permission.png" width="150" alt="a permission prompt"> | <img src="docs/question.png" width="150" alt="a question"> | <img src="docs/not-approved.png" width="150" alt="waiting for approval"> |

(Rendered on the host from `docs/fixtures/state.json` by `firmware/panel-core/tools/render_docs.sh`,
with the glass's rounded corners masked. Every screen is in `docs/`.)

- **Home.** 2×2 tiles in one continuous vertical list, as on the T4-S3 panel.
  - Each tile shows the critter with its pose and hat, the name, a status line, the context bar, the
    tool chip and the subagent critter.
  - Drag to scroll. Let go and the list settles on a row; a flick carries further.
  - A thin bar on the left shows where you are while the list moves, and fades a second after it
    settles.
  - The header says how many agents wait and run.
- **Card.** Tap a tile. The card shows:
  - the critter, the name and the status pill;
  - what the agent is doing now;
  - context with the dotted bar;
  - tokens, cost and turns;
  - the last prompt.

  Round buttons at the bottom: **focus**, **interrupt** (while running), and **stop**, which you hold
  for 1 s. Focus and interrupt need scope `act`; stop needs `admin`.
- **Prompts.** A waiting agent opens its prompt: a one-line header, the whole question, the command
  (for permissions), and the answers with their hint lines. A permission is the orange card; a
  question is dark with an orange border. Answering needs scope `act`.
- **Back.** Swipe right, or press BOOT.
- **Other screens.** Waiting for approval (with the command and the board's name), connecting, deck
  offline, no agents.
- **AMOLED.** The background is true black, the screen dims when every agent is idle and nobody touched
  it for 30 s, and content stays clear of the glass's rounded corners (radius 44 px).

## Connect it

```sh
pio run -d firmware/devices/waveshare-touch-amoled-18 -t upload --upload-port /dev/cu.usbmodem<…>
agentctl link add /dev/cu.usbmodem<…>
agentctl devices approve waveshare-touch-amoled-18-<id>
agentctl devices scope waveshare-touch-amoled-18-<id> act    # answer, focus, interrupt; admin to stop
```

It subscribes to every agent with all fields and keeps up to 64 in PSRAM. The Hello says: display
368×448 colour, inputs TOUCH and BUTTON.

## Hardware

Pins are from Waveshare's BSP (`esp32_s3_touch_amoled_1_8` 2.0.3) and Arduino demos:

| part | pins |
|---|---|
| CO5300 AMOLED (QSPI) | CS 12, SCK 11, D0–D3 4 5 6 7; 40 MHz; 16-column offset |
| I²C | SDA 15, SCL 14: TCA9554 0x20, touch 0x15 (CST820) or 0x38 (FT3168), AXP2101 0x34 |
| TCA9554 | EXIO0 LCD reset, EXIO1 panel supply, EXIO2 touch reset |
| BOOT | GPIO0 |

The first revision of the board had an SH8601 panel with FT3168 touch. The firmware detects it by the
touch address and drops the column offset, as Waveshare's BSP does. That revision is untested. The
AXP2101 stays at its power-on defaults, as in Waveshare's code. Brightness is the panel's own (0x51).

`pio run -e debug` adds one-line notes on the USB port for bring-up (frame time, Deck Link rx / bad /
overflow counters); the deck ignores them.

## Code

`src/main.cpp` is only the board. The layout, the touch UI and Deck Link are shared with the other
panels in [`firmware/panel-core`](../../panel-core) (`layout_square.h`, `specAmoled18()`).
