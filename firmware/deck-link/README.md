# Deck Link firmware kit

A C library that connects a small board (ESP32 and similar) to the agents deck over USB serial. It
speaks [Deck Link](../../docs/link/protocol.md): protobuf frames (nanopb) with COBS framing and a
CRC-16, the Hello/Welcome handshake, subscriptions, commands, pings and reconnects. Your code only
draws and reacts. To build a new device, see [new-device.md](../../docs/link/new-device.md).

- `src/deck_link.h`: the core. C99, no malloc, no Arduino, no OS. One context struct holds everything.
- `src/deck_tiles.h`: optional helper that keeps a small table of agents for simple devices.
- `src/deck_pet.h`: optional: an agent's pet (Agent pets), as the web deck draws it, at any whole-pixel
  scale and any colour depth.
- `src/deck_store.h`, `src/deck_pair.h`, `src/deck_tls.h`: Wi-Fi (see below): what a board keeps in
  NVS, its own key and certificate request, and a TLS 1.3 client transport.
- `src/deck_ble.h`: Bluetooth LE (see below): the byte stream between the core and a GATT link, with
  no Bluetooth stack in it.
- `src/DeckLink.h`: a thin Arduino wrapper (`Stream` in, `loop()` to drive it).
- `src/proto/`: `link.pb.c/h` generated from `proto/agentsdeck/link/v1`, and the nanopb 0.4.9.1
  runtime (zlib licence, `src/proto/LICENSE.txt`).
- `examples/`: `status-led` (ESP32-C3, one RGB LED), `oled-tile` (ESP32, SSD1306 and a button),
  `knob` (ESP32-S3, rotary encoder).
- `test/host/`: host tests and `board_sim`, a board on stdin/stdout.

## Use it

**PlatformIO (Arduino).** Point `lib_deps` at the kit, as the examples do:

```ini
lib_deps = symlink://../../deck-link   ; from firmware/devices/<name>
```

```cpp
#include <DeckLink.h>

DeckLink deck;
deck_tiles_t tiles;

void onFrame(deck_link_t *, const deck_frame_t *f) { deck_tiles_handle(&tiles, f); }

void setup() {
  Serial.begin(115200);
  DeckLinkConfig cfg = {};
  cfg.board_id = DeckLink::chipId();   // the MAC
  cfg.type = "my-device";
  cfg.firmware = "0.1.0";
  cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE;
  cfg.on_frame = onFrame;
  deck.begin(Serial, cfg);             // sends Hello
}

void loop() {
  deck.loop();                         // reads Serial, decodes, runs the timers
  // draw from tiles.tiles[0 .. tiles.count) when tiles.version changes
}
```

**Arduino IDE.** Copy or link `firmware/deck-link` into your `libraries` folder (it has
`library.properties`) and `#include <DeckLink.h>`.

**ESP-IDF.** The folder is also a component (`CMakeLists.txt`, `idf_component.yml`; C core only). Add it
with `EXTRA_COMPONENT_DIRS` or copy it into `components/`, then give the core a write callback (e.g.
`usb_serial_jtag_write_bytes` or `uart_write_bytes`) and a clock (`esp_timer_get_time() / 1000`), feed it
what you read and call `deck_link_poll()` in your loop.

**Anything else.** Compile `src/*.c` and `src/proto/*.c` with `src` on the include path.

## The API in brief

```c
deck_link_config_t cfg = {0};
cfg.write = my_write;          /* size_t (*)(const uint8_t *, size_t, void *user) */
cfg.millis = my_millis;        /* uint32_t (*)(void *user) */
cfg.user = &my_port;
cfg.board_id = "AA:BB:CC:DD:EE:FF";
cfg.type = "my-device";
cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT;
cfg.on_agent = on_agent;       /* every callback is optional */

static deck_link_t link;       /* ~8 KB: make it static or global */
deck_link_init(&link, &cfg);
deck_link_begin(&link);        /* Hello */
for (;;) {
    n = read_some(buf);
    deck_link_feed(&link, buf, n);
    deck_link_poll(&link);
}
```

What the core does for you:

- Sends `Hello`, and again every 2 s until a `Welcome` arrives.
- After every approved `Welcome` sends the configured `Subscribe` (or the last `deck_subscribe()`).
- Answers every `Ping` with a `Pong` (same seq and time).
- After 15 s without a frame calls `on_offline` and sends `Hello` every 5 s until the deck answers
  (`on_online`).
- Skips anything that isn't a frame and resyncs at the next `0x00`, like the deck's own decoder.

Callbacks (each gets the `deck_link_t *` first): `on_welcome(name, scope, approved)`,
`on_snapshot_begin(server, agent_count)`, `on_agent(agent)`, `on_agent_removed(id)`, `on_snapshot_end()`,
`on_result(seq, code, error)`, `on_hat_list`, `on_live_sessions`, `on_restore_result`, `on_online`,
`on_offline`, `on_ping(seq, time_ms)` and `on_frame(frame)` (every frame, before the others; this is
where `deck_tiles_handle` goes). Pointers passed to callbacks are valid only during the call. Callbacks
may send commands.

Commands return the seq the deck echoes in its reply (0 if nothing was sent):
`deck_get_state`, `deck_answer(agent_id, prompt_id, key)`, `deck_focus`, `deck_interrupt`, `deck_dismiss`,
`deck_resume`, `deck_move(agent_id, slot)`, `deck_move_attention(agent_id, slot)` (the "Needs you"
grid, 1 to 16), `deck_order(ids, n)`,
`deck_set_hat(agent_id, folder, shape, color, auto_hat)`, `deck_list_hats`, `deck_list_live`,
`deck_add_live(session_id, slot)`, `deck_restore`, `deck_subscribe(first_slot, slot_count, fields)` and
`deck_ping`. Strings are cut to the `link.options` limits on a UTF-8 boundary.

State: `deck_link_state()` (`CONNECTING`, `ONLINE`, `OFFLINE`), `deck_link_online()`,
`deck_link_approved()`, `deck_link_scope()`, `deck_link_stats()`.

`deck_tiles` keeps up to 16 agents sorted by slot with what a simple device shows: id, slot, title,
status, tool, folder, hat, context use in permille, unseen/focused/lost, and the waiting prompt's id,
title and options (key, label, primary). Helpers: `deck_tiles_find`, `deck_tiles_count_status`,
`deck_tiles_first_waiting`, `deck_tile_primary`, `deck_status_name`.


## Agent pets

While Agent pets is on (the deck's Settings, General), every agent has a pet (`Agent.pet`, in
`FIELDS_TILE`) that the deck shows in place of its critter. `deck_pet.h` keeps one in 28 bytes and
draws it as the web deck does (`pets.js`, `pet-poses.js`; [docs/pet-poses.md](../../docs/pet-poses.md)
is the spec): the egg (cracked once it has eaten 30k tokens, or ready to hatch), the baby, the kid and
the grown-up, which strong, mighty and legend pets stay, with flames round them in pale yellow, blue
or gold. A hungry pet has its mouth open; a weak grown-up lies down, brown; a hibernating pet sits in
an ice cube. The kid and up wear their own hat (`Pet.hat`, devil horns included).

The pet does the critter's poses: the eyes, arms and feet move, and the bubble shows what its agent
does (the thought dots, the magnifier, the prompt, the globe, the list, the "?", the "!", the "z z",
the cookie), with the finish flag after a turn, the mini pet in tow while it delegates and the squeeze
while it compacts. An egg rocks while its agent works. Every part has a black stroke one pet pixel
wide, four ways round it, drawn under the pet; props and bubbles are white. On orange the pet keeps
its own colours: the stroke keeps it apart.

```c
deck_pet_t pet;
deck_pet_from_proto(&pet, a->has_pet, &a->pet);         /* in on_agent; has_pet false: pets are off */

deck_pet_look_t look;                                   /* 1.6 KB: build it, draw it, drop it */
deck_pet_opts_t o = {0};
o.pose = DECK_PET_POSE_BASH;                            /* what its agent does */
o.ms = millis();                                        /* where the pose's motion is */
o.bubble = true;
deck_pet_look(&look, &pet, &o);
deck_pet_draw(&look, x, y, scale, fill, ctx);           /* fill(ctx, x, y, w, h, 0xRRGGBB, alpha) */
```

The art is 16 x 14 cells (cell (0, 0) at the pixel you give). Bubbles, the flag and the flames reach
past it, to cells -5..24 across and -12..15 down (`DECK_PET_REACH_*`, for a fixed box round a pet;
`deck_pet_bounds()` gives one look's). Colours come as 0xRRGGBB with an alpha: only the ice is
see-through, so mix it with what's under it, or skip it on a screen that can't. `deck_pet_motion()`
gives the wobble of an egg waiting to be seen. `deck_pet_look()` uses about 2.9 KB of its own scratch,
so draw pets from one task.

`test/host/pet_golden.h` holds every pose's cells over time as pets.js and pet-poses.js draw them
(`tools/pet_golden.mjs` writes it); `test_pet` checks the kit against it, so a change to the art goes
in both.

## Pairing and Wi-Fi

A board can pair over USB and then connect over Wi-Fi with the same frames
([protocol](../../docs/link/protocol.md#pairing-and-wi-fi)). The kit has the parts; the board decides
when to use them (`firmware/panel-core/src/net_link.h` does it for the touch panels):

- **Messages** (core): `deck_cert_request(ctx, csr, len)` and `on_cert_issued`; `on_wifi_config(ctx,
  ssid, password, forget)` returns the code the core answers with (unset: 501); `deck_net_status()`;
  `deck_link_cert_serial()` is the last Welcome's `cert_serial`.
- **Storage** (`deck_store.h`): `deck_cert_t` (key, certificate, CA, hosts, port, fingerprint) and
  `deck_wifi_t` through a small key-value interface, `deck_kv_t`. `deck_kv_nvs_open()` is NVS on
  ESP-IDF; tests use a fake. The certificate is one blob, so a reset while saving never mixes two.
  NVS is plain flash unless flash encryption is on: the key and the Wi-Fi password can be read off a
  board in hand.
- **Pairing** (`deck_pair.h`): `deck_pair_request()` makes an EC P-256 key on the chip and sends its
  CSR; `deck_pair_accept()` turns the reply into a `deck_cert_t`; `deck_cert_needed(cert,
  deck_link_cert_serial(ctx))` says when to ask.
- **Transport** (`deck_tls.h`): `deck_tls_open()` connects to one of the deck's hosts with TLS 1.3 and
  the client certificate, checks the deck against its CA (or the fingerprint when the host isn't one
  its certificate names) and upgrades to Deck Link (`GET /v1/link`); `deck_tls_write` is a
  `deck_write_fn`, and what `deck_tls_read` returns goes to `deck_link_feed`.

The deck's listener accepts TLS 1.3 only, so this needs **mbedTLS 3 with
`MBEDTLS_SSL_PROTO_TLS1_3`**: ESP-IDF 5 with `CONFIG_MBEDTLS_SSL_PROTO_TLS1_3=y` (arduino-esp32 3.x
leaves it off in its prebuilt libraries; pioarduino's `custom_sdkconfig` rebuilds them with it, see
the round boards' `idf5/platformio.ini`). On ESP-IDF 4.4 (arduino-esp32 2.0.x, mbedTLS 2.28) the
pairing and TLS calls compile to stubs that return "unsupported" (`deck_net_config.h`,
`DECK_LINK_HAS_TLS13`), so a board there stays on USB. `-DDECK_LINK_NO_WIFI` turns it off anywhere.

## Firmware updates over Wi-Fi

The deck can update a board on Wi-Fi (docs/link/protocol.md, "Firmware updates over Wi-Fi"):

- `on_firmware_offer` gets the deck's `FirmwareOffer`. Without it, the core answers `REFUSED`
  ("this firmware can't update over Wi-Fi"). `deck_firmware_offer_check()` does the checks every
  board makes first (its own board, a size and SHA-256, the image fits the slot, a path on
  `/v1/firmware/`), and `deck_firmware_status()` reports (`DOWNLOADING` with a percent,
  `VERIFYING`, `RESTARTING`, `FAILED`, `REFUSED`).
- `deck_fw.h`: `deck_fw_download()` fetches the image over TLS 1.3 with the board's certificate
  (`deck_tls_get()`, an HTTP GET with `Range`), hands it to your sink in order, 4 KB at a time,
  checks size and SHA-256, and resumes after a drop. It blocks: run it in a task and report from
  your loop. Writing to the flash, switching slots and the confirm after the restart are the
  board's: panel-core's `ota.h` does it with `esp_ota_*`.
- `test/host/board_wifi --firmware-board B --firmware-out FILE` takes offers on the host; the Go
  test `TestFirmwareKitUpdateOverWifi` runs it against the deck.

## Bluetooth

The same core runs over Bluetooth LE ([protocol](../../docs/link/protocol.md#bluetooth)): the core
only needs a `write` callback and bytes fed to `deck_link_feed`, so serial, TLS and Bluetooth are
three ways to fill them. `deck_ble.h` is the Bluetooth one, without a stack (C99, no malloc, no OS):

- `deck_ble_write` is the core's `write`: it queues a whole frame or nothing (a full queue counts in
  the core's `tx_errors`, like a short serial write). `deck_ble_pump()` sends the queue in
  notifications of ATT MTU - 3 bytes through your `notify` callback, and stops when the stack says it
  has no room (back-pressure); call it every loop.
- What the deck writes goes into a ring you give it (`rx_buf`, 16 to 32 KB is a whole snapshot):
  `deck_ble_rx_push()` from the stack's task, `deck_ble_rx_feed()` from your loop into the core. One
  producer and one consumer, no lock. A write that doesn't fit is refused whole, so the GATT handler
  can answer "insufficient resources".
- `deck_ble_open()` when the link is encrypted, authenticated and bonded and the deck listens on TX;
  `deck_ble_close()` when it goes. `deck_ble_name()` is the advertised name (`deck-1a2b3c`),
  `deck_ble_info()` the Info characteristic, `deck_ble_passkey()` a 6-digit passkey, and
  `DECK_BLE_UUID128()` the service's UUIDs for NimBLE.
- `on_link_config` gets the deck's uplink preference (`LinkConfig`); unset, the core answers 501.
  `deck_store_load_uplink()` and `deck_store_save_uplink()` keep it.

The GATT service, pairing and the bond are the board's: panel-core's `ble_link.h` does them with
ESP-IDF's NimBLE (arduino-esp32 3.x on the ESP32-S3). `test/host/test_ble.c` runs the stream against a
fake link, and `board_wifi --ble PATH` is a board whose Bluetooth link is a Unix socket.

## Memory

Measured on ESP32 (xtensa gcc) with the defaults:

| what | bytes |
|---|---|
| `deck_link_t` (all of the link's state) | about 8160 |
| of which the RX buffer (`DECK_LINK_RX_SIZE`) | 4121 |
| of which the decoded `agentsdeck_link_v1_Frame` | 3184 |
| of which the TX buffer (a CertRequest fits) | 623 |
| `deck_tiles_t` (16 tiles of 472) | 7568 |
| `deck_pet_t` (one agent's pet) / `deck_pet_look_t` (while drawing) / its scratch | 28 / 1640 / 2944 static |
| code: core + nanopb + generated tables | about 15 KB flash |

The largest stack frame is a command's message (a `CertRequest`, about 610 bytes; an `Order` of 32 ids is about 400). Pairing and TLS run mbedTLS: give that task 8 KB or more of stack.

To save RAM, build with a smaller `-DDECK_LINK_MAX_FRAME` (at least 512), `-DDECK_TILES_MAX=8` or
smaller `DECK_TILE_*_SIZE` values. The core reports the frame size in `Hello.max_frame` and the deck
keeps to it: it drops an agent's subagents, last prompt and folder path first, then shortens the
prompt's text and option labels; an agent that still doesn't fit is skipped. 3328 bytes fits
everything the current `link.options` allows. The decoded `Frame` (about 3.2 KB) is sized by nanopb
from `link.options` either way.

## Rules for the serial port

The port belongs to Deck Link. Text you print is ignored by the deck but never print in the middle of a
frame (from an interrupt or another task). The examples print nothing on the Deck Link port.

## Regenerating the protobuf code

```sh
tools/generate.sh          # needs nanopb_generator 0.4.9.1
tools/generate.sh --check  # fails if src/proto differs from the proto files
```

## Tests

```sh
make -C test/host test        # COBS, CRC, wire bytes, stream resync, handshake timers, tiles, pairing messages, storage, Bluetooth stream, pets
make -C test/host board_sim   # builds test/host/build/board_sim
make -C test/host test_tls    # pairing with mbedTLS 3 (Homebrew's mbedtls, or MBEDTLS=<prefix>)
make -C test/host board_wifi  # a board that pairs over serial and connects over TLS 1.3
```

`board_sim` is a board on stdin (RX) and stdout (TX), used by the daemon's Go tests. `board_wifi` pairs
the same way, keeps its certificate in a folder, and then connects to the deck over TLS 1.3 with the
kit's own transport (`backend/internal/link/wifi_kit_test.go` runs it end to end). Its flags and the
lines it logs to stderr are documented at the top of `test/host/board_sim.c`.
