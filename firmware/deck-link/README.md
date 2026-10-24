# Deck Link firmware kit

A C library that connects a small board (ESP32 and similar) to the agents deck over USB serial. It
speaks [Deck Link](../../docs/link/protocol.md): protobuf frames (nanopb) with COBS framing and a
CRC-16, the Hello/Welcome handshake, subscriptions, commands, pings and reconnects. Your code only
draws and reacts. To build a new device, see [new-device.md](../../docs/link/new-device.md).

- `src/deck_link.h`: the core. C99, no malloc, no Arduino, no OS. One context struct holds everything.
- `src/deck_tiles.h`: optional helper that keeps a small table of agents for simple devices.
- `src/deck_store.h`, `src/deck_pair.h`, `src/deck_tls.h`: Wi-Fi (see below): what a board keeps in
  NVS, its own key and certificate request, and a TLS 1.3 client transport.
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
`deck_resume`, `deck_move(agent_id, slot)`, `deck_order(ids, n)`,
`deck_set_hat(agent_id, folder, shape, color, auto_hat)`, `deck_list_hats`, `deck_list_live`,
`deck_add_live(session_id, slot)`, `deck_restore`, `deck_subscribe(first_slot, slot_count, fields)` and
`deck_ping`. Strings are cut to the `link.options` limits on a UTF-8 boundary.

State: `deck_link_state()` (`CONNECTING`, `ONLINE`, `OFFLINE`), `deck_link_online()`,
`deck_link_approved()`, `deck_link_scope()`, `deck_link_stats()`.

`deck_tiles` keeps up to 16 agents sorted by slot with what a simple device shows: id, slot, title,
status, tool, folder, hat, context use in permille, unseen/focused/lost, and the waiting prompt's id,
title and options (key, label, primary). Helpers: `deck_tiles_find`, `deck_tiles_count_status`,
`deck_tiles_first_waiting`, `deck_tile_primary`, `deck_status_name`.


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

## Memory

Measured on ESP32 (xtensa gcc) with the defaults:

| what | bytes |
|---|---|
| `deck_link_t` (all of the link's state) | about 8160 |
| of which the RX buffer (`DECK_LINK_RX_SIZE`) | 4121 |
| of which the decoded `agentsdeck_link_v1_Frame` | 3184 |
| of which the TX buffer (a CertRequest fits) | 623 |
| `deck_tiles_t` (16 tiles of 472) | 7568 |
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
make -C test/host test        # COBS, CRC, wire bytes, stream resync, handshake timers, tiles, pairing messages, storage
make -C test/host board_sim   # builds test/host/build/board_sim
make -C test/host test_tls    # pairing with mbedTLS 3 (Homebrew's mbedtls, or MBEDTLS=<prefix>)
make -C test/host board_wifi  # a board that pairs over serial and connects over TLS 1.3
```

`board_sim` is a board on stdin (RX) and stdout (TX), used by the daemon's Go tests. `board_wifi` pairs
the same way, keeps its certificate in a folder, and then connects to the deck over TLS 1.3 with the
kit's own transport (`backend/internal/link/wifi_kit_test.go` runs it end to end). Its flags and the
lines it logs to stderr are documented at the top of `test/host/board_sim.c`.
