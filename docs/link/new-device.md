# Adding a Deck Link device

How to build firmware for a new kind of board and get it merged. Written for people and for coding
agents: follow it top to bottom and every step has a check.

Read [protocol.md](protocol.md) once first. You won't need its byte-level details: the firmware kit in
[`firmware/deck-link`](../../firmware/deck-link) does framing, the handshake, pings and reconnects. Your
code only draws and reacts.

## 1. Start from the closest example

| your device has | copy |
|---|---|
| LEDs only, no input | `firmware/deck-link/examples/status-led` |
| a small screen and a button | `firmware/deck-link/examples/oled-tile` |
| a rotary encoder or knob | `firmware/deck-link/examples/knob` |

```sh
cp -r firmware/deck-link/examples/oled-tile firmware/devices/<your-device>
```

Name the folder after what the device is (`desk-strip`, `eink-badge`), lowercase with dashes. Set
`type` in its config to the same name: the deck uses it to name the board (`<type>-<id>`).

## 2. Decide what it subscribes to and what it may do

- **Fields.** Subscribe only to what you show. `FIELDS_TILE` is enough for status lights and lists;
  add `FIELDS_PROMPT` to show and answer prompts, `FIELDS_CARD` for details, `FIELDS_NUMBERS` for
  tokens and cost. Less data means less RAM and fewer updates.
- **Slots.** A device that shows one page of the deck subscribes to `first_slot` / `slot_count` (16 per
  page); `slot_count = 0` is every agent.
- **Scope.** New boards start at `view`. A device that answers prompts or focuses agents needs `act`;
  dismiss, resume and restore need `admin`. Say which in the device's README; the owner grants it
  with `agentctl devices scope <name> act`.

## 3. Write the device

- Use the kit's callbacks (`on_agent`, `on_agent_removed`, `on_snapshot_end`, `on_welcome`,
  `on_result`, `on_offline`) or the `deck_tiles` helper, which keeps a small table of agents for you.
- Answer a prompt with `deck_answer(agent_id, prompt_id, key)`: always send the prompt's own `id` from
  `waiting.id`. If the prompt changed in the meantime you get `Result` 409; show the new one.
- Show these states clearly: *not approved yet* (`Welcome.approved == false`: tell the person to run
  `agentctl devices approve <name>`; on a registered USB port this is rare, as new boards are approved
  at once unless the owner turned that off), *deck offline* (`on_offline`), and *needs you*
  (`status == WAITING`), which is the point of most devices.
- Keep the serial port for Deck Link. Text prints are ignored by the deck but waste bandwidth; never
  print from an interrupt while a frame may be going out.
- Stay inside the limits in `link.options`: the deck never sends longer strings or lists, so size your
  buffers from the generated `*_size` constants.

## 4. Build it

```sh
~/.platformio/penv/bin/pio run -d firmware/devices/<your-device>
```

Fix every warning in your code. CI builds every folder under `firmware/devices` and
`firmware/deck-link/examples` the same way.

## 5. Test it

Without hardware, the host tests run the kit against the real deck code:

```sh
make -C firmware/deck-link/test/host test
cd backend && go test ./internal/link/...
```

With the board plugged in (and not registered with the daemon):

```sh
agentctl link ports
agentctl link check /dev/cu.usbmodem1101 --seconds 30
```

`link check` plays a deck with a demo fleet (your real agents are not involved), approves the board
with admin scope, and reports whether it says Hello correctly, subscribes, answers pings and recovers
from garbage on the line. Press the device's buttons while it runs: every command it sends is printed
with the deck's answer. All checks must pass.

Then use it for real:

```sh
agentctl link add /dev/cu.usbmodem1101
```

Plugged into a port you registered, a new board is approved at once with scope `act` (the USB cable
on your Mac counts as your consent; the audit log notes it). `admin` is given by hand
(`agentctl devices scope <type>-<id> admin`). With `agentctl devices auto-approve usb off` it waits
instead:

```sh
agentctl devices approve <type>-<id>
agentctl devices scope <type>-<id> act    # if it answers prompts
```

A board with Wi-Fi then pairs by itself, and gets the deck's default network if one is set
(`agentctl link wifi --default --ssid NAME`, [protocol.md](protocol.md#the-default-network)).

## 6. Open the pull request

The PR template asks for:

- the `agentctl link check` output (all `ok`),
- the PlatformIO RAM and flash numbers,
- the scope the device needs and why,
- a photo or a short video of it working,
- a README in the device folder: what it does, the parts list, wiring, and how to flash it.

Don't change `proto/` in a device PR. If a device needs something the protocol doesn't carry, open an
issue first: protocol changes have to land in the daemon, the web API and every client together.
