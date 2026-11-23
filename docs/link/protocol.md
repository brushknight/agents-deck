# Deck Link protocol v1

Deck Link connects small boards (ESP32 and similar) to the agents deck over USB serial, once
paired over Wi-Fi (see [Pairing and Wi-Fi](#pairing-and-wi-fi)), and over Bluetooth LE (see
[Bluetooth](#bluetooth)). The frames are the same on all three. A board sees
the same agents, statuses and prompts as the web dashboard and can take the same actions, within the
scope you give it. The messages are defined in
[`proto/agentsdeck/link/v1/link.proto`](../../proto/agentsdeck/link/v1/link.proto); size limits are in
[`link.options`](../../proto/agentsdeck/link/v1/link.options). The firmware kit in
[`firmware/deck-link`](../../firmware/deck-link) implements all of this in C; read on if you write your
own, or to debug.

To build a new device, follow [new-device.md](new-device.md).

## Serial settings

8 data bits, no parity, 1 stop bit, no flow control. The deck opens the port at 115200 baud unless you
set another rate (`agentctl link add <port> --baud 921600`). Boards with native USB (ESP32-S3, -C3, -C6)
ignore the rate.

Opening a serial port can reset an ESP32 dev board (DTR/RTS). The board then simply starts over with
`Hello`. The deck only opens ports you registered with `agentctl link add`, and lets go of a port
while another program (a flasher, a serial monitor) has it open: it notices when the board goes quiet
(the other program reset it or reads its bytes), and at least every ten minutes.

## Framing

Each message is one `Frame` (see `link.proto`), sent as:

```
COBS( 0xDE 0xC1 | 0x01 | <Frame, protobuf> | CRC16 hi | CRC16 lo ) 0x00
```

- `0xDE 0xC1` is the magic and `0x01` the wire version.
- The CRC is CRC-16/CCITT-FALSE (poly `0x1021`, init `0xFFFF`, no reflection, no final xor) over
  magic, version and protobuf bytes. Check value: `"123456789"` → `0x29B1`.
- COBS ([Consistent Overhead Byte Stuffing](https://en.wikipedia.org/wiki/Consistent_Overhead_Byte_Stuffing))
  removes every zero byte, so `0x00` ends a frame. After any noise or a corrupt frame, a reader
  resynchronises at the next `0x00`.
- A frame is at most **4096 bytes** before COBS, unless the board asks for less in `Hello.max_frame`
  (at least 512). The largest frame the deck can send with the limits in `link.options` is 3413 bytes.

Anything that isn't a valid frame is ignored, including the ESP32's boot log. Since `0xDE 0xC1` never
occurs in UTF-8 text, printing text on the same port is harmless.

Example: `Frame{seq: 1, ping: {time_ms: 5}}` is protobuf `08 01 62 02 08 05`; with magic, version and
CRC that's `DE C1 01 08 01 62 02 08 05 88 B6`, and on the wire (COBS + delimiter)
`0C DE C1 01 08 01 62 02 08 05 88 B6 00`.

### Legacy: JSON lines

For quick experiments (`Serial.println` in an Arduino sketch) a board may instead send one JSON object
per line: the same `Frame` in protobuf's standard JSON mapping, field names in lowerCamelCase.

```json
{"seq":1,"hello":{"protocol":1,"boardId":"24:6F:28:3F:A9:C1","type":"led-strip","firmware":"0.1.0"}}
{"seq":2,"subscribe":{"fields":1}}
{"seq":3,"answer":{"agentId":"k3f9a2","promptId":"p17","key":"1"}}
```

The deck answers in the format of the board's `Hello`. JSON mode has full parity but costs about three
times the bytes and needs a JSON parser on the board. It is kept for prototypes; new firmware should
use the binary frames.

## Conversation

```
board                                   deck
  │ Hello {board_id, type, firmware, …} ──▶│  registers the board (pending if new; on a
  │                                         │  registered USB port, approved at once)
  │◀── Welcome {device_name, scope, approved}
  │      (approved = false: nothing more until `agentctl devices approve <name>`;
  │       then a new Welcome with approved = true)
  │ Subscribe {first_slot, slot_count, fields} ──▶
  │◀── SnapshotBegin {server, agent_count}
  │◀── AgentChanged {agent}   × each subscribed agent, by slot
  │◀── SnapshotEnd
  │            … later …
  │◀── AgentChanged {agent}   an agent changed (complete for the subscribed fields)
  │◀── AgentRemoved {id}      gone, or moved out of the subscribed slots
  │ Answer {seq: 7, agent_id, prompt_id, key} ──▶
  │◀── Result {seq: 7, code: 204}
  │◀── Ping {time_ms}   every 5 s
  │ Pong {same seq, time_ms} ──▶
```

- **Hello** must be the board's first frame and is sent again after every reset. A second `Hello`
  starts the conversation over (new `Welcome`, the board subscribes again).
- **Approval.** A new board on a registered USB port (`agentctl link add`) is approved at once, scope
  `act`: physical USB on a port you registered is treated as your consent. The audit log says
  `approved automatically over USB on <port>`. It never happens over Wi-Fi or for `agentctl link sim`, a
  revoked board stays revoked until `agentctl devices approve`, and `admin` is always given by hand.
  `agentctl devices auto-approve usb off` makes new boards wait for `agentctl devices approve` again
  (`--scope view` gives them view instead).
- **Welcome** is sent again whenever the board's approval or scope changes: when you approve it, change
  its scope (`agentctl devices scope <name> act`) or revoke it (`approved: false`). Its `cert_serial`
  is the board's Wi-Fi certificate as the deck knows it (see below).
- **Subscribe** chooses agents by board position (`first_slot`, `slot_count`; `0` = all) and fields by
  group bits. Sending it again replaces the subscription and resends the snapshot:

  | bit | group | fields |
  |---|---|---|
  | 1 | `FIELDS_TILE` | `title tool status status_since_ms folder context unseen lost focused external app hat account activity.tool subagent_count attention_slot queued` |
  | 2 | `FIELDS_CARD` | `ai_title cwd branch model model_label effort activity.detail last_prompt error resumable attached subagents started_at_ms updated_at_ms last_active_ms` |
  | 4 | `FIELDS_PROMPT` | `waiting` |
  | 8 | `FIELDS_NUMBERS` | `tokens cost_usd turns` |

  `id` and `slot` are always set. `fields: 0` means `FIELDS_TILE`.
- **The "Needs you" grid.** `attention_slot` is the agent's spot on the "Needs you" screen, 1 to 16 (a
  4×4 grid, numbered by rows), or 0 when it isn't there. The deck decides who is there and where, so
  every board and screen shows the same agents in the same spots (rules in
  [protocol.md](../protocol.md#the-needs-you-grid)). A 4×4 board draws spot n in cell n. A board with
  fewer cells shows the agents in order of their spot, lowest first, in the cells it has; the rest
  aren't shown, but the count on its mark includes them. To see every agent on it, subscribe to every
  agent (`slot_count: 0`).
- **Updates.** When an agent changes in a way the board can see (a subscribed field), the deck resends
  that whole agent in `AgentChanged`. An agent that leaves the subscribed slots, or the deck, gets
  `AgentRemoved`.
- **Liveness.** The deck sends `Ping` every 5 s; answer with a `Pong` carrying the same `seq` and
  `time_ms`. A board may ping the deck too. A board should show "deck offline" after 15 s without any
  frame and keep listening: when the deck comes back it sends `Welcome` again on the board's next
  `Hello`, so resending `Hello` after a long silence is a good idea.

## Commands

Every command mirrors one endpoint of the web API ([protocol.md](../protocol.md)) and gets a reply with
the request's `seq`: `Result` (the web API's status code and error text) or a typed reply.

| command | web API | needs scope | reply |
|---|---|---|---|
| `GetState` | `GET /v1/state` | view | the snapshot again, then `Result 204` |
| `ListHats` | `GET /v1/hats` | view | `HatList` |
| `ListAccounts` | `GET /v1/accounts` | view | `AccountList` |
| `Answer {agent_id, prompt_id, key}` | `POST /v1/agents/{id}/answer` | act | `Result`: 204; 409 if the prompt changed; 400 if `key` isn't an option. A question with several parts comes one step at a time, each a new prompt; `Option.selected` marks ticked answers of a pick-any question (see the web API's Prompt) |
| `SendPrompt {agent_id, text, interrupt, mode, front}` | `POST /v1/agents/{id}/prompt` | act | `Result`: 204; with `mode` `SEND_NOW` (the default), 409 while the agent works (unless `interrupt`), asks a question, starts or has exited; `SEND_INTERRUPT` is `interrupt`; `SEND_QUEUE` answers 200 when the prompt went in at once, 202 when it was queued to go in once the agent's turn ends (`front`: before the prompts queued earlier; `Agent.queued` counts them); `SEND_TYPE` types it in now even while the agent works (the tool's own queue holds it), 204, and 409 while it asks a question, starts or has exited. 422 for a session the deck can't type into. `text` is at most 383 bytes (384 with its NUL in `link.options`), so the whole frame fits the smallest frame a board may ask for (512 bytes); the web API takes up to 4000 |
| `Queue {agent_id}` | `GET /v1/agents/{id}/queue` | view | `QueueList`: the first 4 queued prompts, next first, text clipped to 63 bytes; `total` says how many there are |
| `Queue {agent_id, cancel_id}` | `POST /v1/agents/{id}/queue/{qid}/cancel` | act | `Result`: 204; 404 when it was sent or cancelled already |
| `Focus`, `Interrupt` `{agent_id}` | `POST /v1/agents/{id}/focus`, `…/interrupt` | act | `Result` |
| `Move {agent_id, slot}` | `POST /v1/agents/{id}/move` | act | `Result` |
| `MoveAttention {agent_id, slot}` | `POST /v1/agents/{id}/attention` | act | `Result`: 204; 409 if the agent isn't on the "Needs you" grid; 400 if `slot` isn't 1 to 16 |
| `Order {agent_ids}` | `POST /v1/order` | act | `Result` |
| `Stash {agent_ids, all}` | `POST /v1/stash` | act | `Result` |
| `MagicView {agent_ids}` | `POST /v1/magic/view` | act | `Result` |
| `SetHat {agent_id or folder, shape, color, automatic}` | `POST /v1/agents/{id}/hat`, `POST /v1/hats` | act | `Result` |
| `Dismiss`, `Resume` `{agent_id}` | `POST /v1/agents/{id}/dismiss`, `…/resume` | admin | `Result` |
| `Handoff {agent_id, account}` | `POST /v1/agents/{id}/handoff` | admin | `Result` (200 when it started) |
| `Restore {agent_ids}` | `POST /v1/restore` | admin | `RestoreResult`: the listed lost agents, or every lost agent when `agent_ids` is empty |
| `ListLive` | `GET /v1/live` | admin | `LiveSessions` |
| `AddLive {session_id, slot}` | `POST /v1/live/{session}/add` | admin | `Result` |

`Result.code` is the HTTP status the web API would give: 204 done, 400 bad request, 403 beyond the
board's scope (or not approved yet), 404 no such agent, 409 the prompt changed, 501 not available,
500 failed. Commands run through the same code as the web API and are logged in the deck's audit log
under the board's name.

## Pairing and Wi-Fi

A board that pairs over USB can later connect over Wi-Fi, as the same device: the same name, scope,
approval, revocation and audit log. Over Wi-Fi it speaks the same frames, so everything above works
unchanged.

### Pairing (over USB serial, or Bluetooth)

```
board                                   deck
  │◀── Welcome {approved: true, cert_serial: ""}
  │ CertRequest {seq: 9, csr} ──▶│  approved boards only, USB only
  │◀── CertIssued {seq: 9, cert, ca, hosts, port, server_fingerprint, device_name, cert_serial}
```

- The board makes an EC P-256 key **on the chip** (it never leaves it) and sends a certificate signing
  request for it, `CertRequest.csr` (DER, or PEM; at most 600 bytes).
- Only an approved board gets a certificate, and only over USB serial or an encrypted, bonded
  Bluetooth link ([Bluetooth](#bluetooth)): anything else gets a `Result` (403 not approved or not on
  USB or Bluetooth, 400 a bad request, 503 the network listener is off). The deck signs
  it with its CA (the one in the Keychain, [protocol.md](../protocol.md#devices-and-security)) and
  records it on the board's own entry in `devices.json`.
- `CertIssued` says where to use it: `hosts` are the Mac's `.local` name, then its LAN addresses (at
  most 4); `port` is the network listener's (7341); `server_fingerprint` is the SHA-256 of the
  listener's certificate (32 bytes); `ca` is the deck's CA (DER). The board keeps all of it with the
  key.
- `Welcome.cert_serial` is the certificate the deck holds for the board (`""` for none). A board whose
  stored certificate has the same serial doesn't ask again; one with none or another asks, with a
  new key. `agentctl link pair <board>` makes the deck forget it: the board pairs again the next time it
  is on USB. Revoking a board (`agentctl devices revoke`) also drops its certificate for good;
  approved again, it pairs again over USB.
- The board's key and certificate live in its flash (NVS on an ESP32). NVS isn't encrypted unless you
  enable flash encryption (and NVS encryption): someone holding the board can read them, and the Wi-Fi
  password. Revoke a lost board.

### Wi-Fi credentials (over USB serial, or Bluetooth)

`agentctl link wifi <board> --ssid <name>` asks for the password (without echo; `--password` also
works) and the daemon sends it to the board over USB (or Bluetooth):

```
board                                   deck
  │◀── WifiConfig {seq, ssid, password}    (forget: true to drop them)
  │ Result {same seq, code: 204} ──▶│
  │ NetStatus {wifi: JOINING} … {wifi: IDLE, ip, rssi} ──▶│   the board tries the network once
```

- Only a board on USB serial or on an encrypted, bonded Bluetooth link right now gets them: otherwise
  the command says so and nothing is kept. Over Bluetooth the board tries the network while staying
  on Bluetooth, as it stays on USB.
  The Mac never stores the password; the daemon doesn't log or audit it (the audit log records
  `wifi <board>` with `set` or `forget`).
- The board answers with a `Result` (204 kept, 400 bad, 501 no Wi-Fi in this firmware), then tries the
  network while staying on USB and reports the outcome in `NetStatus` (`IDLE` with its address when
  it worked, or `WRONG_PASSWORD`, `NO_NETWORK`, `FAILED`). `agentctl link wifi` waits for it.
- There is no `/v1` endpoint for this: `/v1` is reachable from the network, and a Wi-Fi password
  shouldn't travel further than the Mac's unix socket and the board's USB cable. The web dashboard and
  a companion app get it only through local endpoints (`POST /local/link/wifi` on the unix socket).
- A board given its own network this way keeps it: the default network below doesn't replace it.
  `agentctl link wifi <board> --use-default` undoes that (and sends the default at once if the board is
  on USB).

### The default network

Set once, every board gets it by itself:

```bash
agentctl link wifi --default --ssid "my network"      # asks for the password; --open for none
agentctl link wifi --forget-default                   # boards keep what they have
```

The daemon keeps the default's password in the OS secret store (the macOS login Keychain, service
`dev.agents-deck`, item `wifi-default`; a 0600 file under the data folder's `keys/` elsewhere), never in
`config.json`, a log or the audit log (`wifi default set` / `wifi default cleared`, nothing more).
When an approved board on a registered USB port, or on Bluetooth, has paired (`NetStatus.has_cert`)
and reports no network (`WIFI_NOT_SET`) or another one than the default (`NetStatus.ssid`), the deck
sends it `WifiConfig` with the default, as above:

```
board                                   deck
  │ Hello ──▶│                                    (approved at once on a registered port)
  │◀── Welcome {approved: true, cert_serial: ""}
  │ CertRequest ──▶│◀── CertIssued
  │ NetStatus {has_cert: true, wifi: NOT_SET} ──▶│
  │◀── WifiConfig {ssid: default, password}
  │ Result 204 ──▶│ NetStatus {JOINING} … {IDLE, ip} ──▶│   logged: "sent default network … ok"
```

- At most once per connection: a board that fails with it (`WRONG_PASSWORD`, `NO_NETWORK`, `FAILED`,
  no answer) is not sent it again until it is plugged in again. One that has the default's SSID isn't
  sent it again, unless it failed with it last time or the default changed since (a new password).
- Never to a board with its own network (`agentctl link wifi <board> --ssid`; `devices.json` keeps
  only that flag, no password), never over Wi-Fi, never to a board that isn't approved or paired, and
  never to a board whose firmware has no Wi-Fi (`WIFI_UNSUPPORTED`).
- `NetStatus` says only the board's SSID and state, not its password: a board holding the default's
  SSID with an old password is resent only if this daemon saw it fail or sent an older default.
- `agentctl link` shows the default and, per board, how sending it went (`default lab: ok`,
  `default lab: wrong password`); `agentctl devices` too.

### Over Wi-Fi

The board connects to the network listener with **TLS 1.3** and its client certificate, verifies the
deck against `ca` (when the host it dialled isn't one the listener's certificate names, the
fingerprint from pairing stands in, as for the desk panel), and asks for a Deck Link stream:

```
GET /v1/link HTTP/1.1
Host: my-mac.local
Upgrade: deck-link
Connection: Upgrade

HTTP/1.1 101 Switching Protocols
```

From then on the connection carries the same binary frames as USB (Hello, Welcome, Subscribe …).

- The certificate is the board's identity: its `Hello.board_id` must be the board id of the device the
  certificate belongs to (otherwise `Result 403` and the connection closes), and the session ends as
  soon as the device is revoked or pairs again. A revoked board fails the TLS handshake; the board
  shows "certificate rejected · pair again over USB".
- `CertRequest` and `WifiConfig` are refused over Wi-Fi: pairing and credentials go over USB or
  Bluetooth only.
- The deck pings every 5 s as usual and closes a Wi-Fi session that has been silent for 30 s.
- `hosts` are tried in order: the `.local` name (mDNS on the board), then the addresses.

### Which transport a board uses

At boot a board listens on USB for a few seconds. A deck that answers there (a `Welcome` after its
`Hello`) wins. If nobody does, and the board has a network and a certificate, it joins the network and
connects over TLS. A deck that appears on USB later takes over at once (the board drops Wi-Fi): USB
is preferred, so pairing, new credentials and recovery always work over the cable. A board on USB
whose deck goes quiet for 30 s tries Wi-Fi meanwhile. Off USB, the deck's uplink preference picks
between Wi-Fi and Bluetooth ([Uplinks](#uplinks)).

### NetStatus

After each `Welcome`, and whenever it changes, a board may say how it is connected: `transport`
(`SERIAL`, `WIFI` or `BLE`), `wifi` (`UNSUPPORTED`, `NOT_SET`, `IDLE`, `JOINING`, `CONNECTED`,
`WRONG_PASSWORD`, `NO_NETWORK`, `FAILED`, `DECK_UNREACHABLE`, `CERT_REJECTED`), `ssid`, `ip`, `rssi`,
`has_cert`, `ble` (`UNSUPPORTED`, `SETUP`: no bond, `BONDED`, `CONNECTED`; unset from firmware older
than Bluetooth) and `uplink` (the preference it holds). `agentctl link` and `agentctl devices` show it.
It is never the password.

| message | direction | field | over |
|---|---|---|---|
| `CertRequest` | board → deck | 60 | USB serial, Bluetooth |
| `CertIssued` | deck → board, reply | 61 | USB serial, Bluetooth |
| `WifiConfig` | deck → board, answered with `Result` | 62 | USB serial, Bluetooth |
| `NetStatus` | board → deck | 63 | any |

These are not web API commands, so they have no route; the parity test keeps commands in fields 30–49
and checks these are documented here.

## Firmware updates over Wi-Fi

A board on Wi-Fi can be updated without its cable. A board on USB is updated by writing the full image
over the cable (the flasher), so the deck never offers an update over USB, and a board refuses one
there.

```
board (on Wi-Fi)                                     deck
  │◀── FirmwareOffer {board, version, size, sha256, path}      you pressed "update"
  │ FirmwareStatus {DOWNLOADING, percent} … ──▶│
  │ GET /v1/firmware/{board}/{version}  (TLS 1.3, its certificate; Range to resume) ──▶ app image
  │ FirmwareStatus {VERIFYING} ──▶│
  │ FirmwareStatus {RESTARTING} ──▶│
  ╳ restarts into the new firmware
  │ Hello {firmware: version} ──▶│◀── Welcome           the board keeps the new firmware now
```

- `FirmwareOffer` names the `board` the image is for (its folder under `firmware/devices`, e.g.
  `lilygo-t4-s3`), the `version` it reports once it runs, the image's `size` and `sha256` (32 bytes), and
  the `path` to download it from on the deck's network listener, `/v1/firmware/{board}/{version}`
  ([protocol.md](../protocol.md#endpoints)). The image is the app alone (`agents-deck-<board>-<version>.app.bin`
  in a release), not the full image flashed over USB: the board's name, certificate and Wi-Fi stay.
- The board answers with `FirmwareStatus`: `REFUSED` with a reason when it won't take the offer (the
  `board` isn't its own, it is on USB, it has no second firmware slot or the image is bigger than it,
  it is already updating, or its firmware can't update over Wi-Fi: the kit answers that for firmware
  without a handler), then `DOWNLOADING` with `percent` every few percent, `VERIFYING`, `RESTARTING`, or
  `FAILED` with a reason. After `FAILED` it keeps the firmware it runs.
- The download uses the same TLS 1.3 connection rules as `GET /v1/link` (the board's certificate, the
  deck's CA or fingerprint), on a second connection beside the Deck Link session. The deck serves an
  image only to a Deck Link board whose type runs that board's firmware, and audits every request. A
  dropped connection resumes with `Range: bytes=<written>-`.
- The board writes the image to its other firmware slot (every released Wi-Fi board has two of 6.25
  MB), checks its size and SHA-256 while it writes, checks it is an app for its chip built for its own
  board (the build carries its board's name), and only then marks it to boot. It restarts into it
  and keeps it only once the deck answers its `Hello` with a `Welcome` (over Wi-Fi or USB). If the new
  firmware doesn't reach the deck within 5 minutes, or restarts before it does (a crash, a power cut),
  the board goes back to its old firmware, once; its `Hello` then reports the old version, and the deck
  shows the update as rolled back.
- The deck keeps per board the version it runs (`Hello.firmware`) and the last update's state
  (offered, downloading, verifying, restarting, done, failed, refused, rolled back) for the management
  API. Updates are never automatic. `agentctl link update <board> --image FILE --board B --version V`
  offers an image file from the Mac (a build to try) and follows the update.

| message | direction | field | over |
|---|---|---|---|
| `FirmwareOffer` | deck → board | 64 | Wi-Fi |
| `FirmwareStatus` | board → deck | 65 | either (the board may have moved to USB meanwhile) |

Like pairing, these set up the board itself: they are not web API commands and have no route.

## Bluetooth

A board can reach the deck over Bluetooth LE: next to a laptop with no Wi-Fi and no cable, and to be
set up from nothing without a cable. Deck Link itself doesn't change: the same frames travel over a
GATT byte stream. The touch boards' `idf5` firmware has it (ESP32-S3, NimBLE); the kit's part is
[`deck_ble.h`](../../firmware/deck-link/src/deck_ble.h), the board's
[`ble_link.h`](../../firmware/panel-core/src/ble_link.h).

### GATT

| | UUID | properties |
|---|---|---|
| service | `7e3a0001-5d2c-4f1b-9a6e-a9e17d5c0de0` | primary |
| RX, deck → board | `7e3a0002-5d2c-4f1b-9a6e-a9e17d5c0de0` | write, write without response; encrypted, authenticated |
| TX, board → deck | `7e3a0003-5d2c-4f1b-9a6e-a9e17d5c0de0` | notify; encrypted, authenticated |
| Info | `7e3a0004-5d2c-4f1b-9a6e-a9e17d5c0de0` | read, no encryption |

- Deck Link frames (COBS, each ending in `0x00`) are written to RX and notified on TX as a plain byte
  stream, cut to the link's ATT MTU minus 3. There is no framing of its own: a frame may span many
  writes, and a write may hold the end of one frame and the start of the next. The board asks for an
  ATT MTU of 247 (244 bytes per write or notification).
- Info is UTF-8 `board=<board>;id=<board id>;fw=<version>;paired=<0|1>`, for example
  `board=lilygo-t4-s3;id=AA:BB:CC:1A:2B:3C;fw=0.3.0;paired=0`. `board` is its folder under
  `firmware/devices`, `id` its `Hello.board_id`, `paired` whether it holds a Bluetooth bond. Anyone in
  range can read it; it says nothing a scan doesn't.
- The deck sends best with write without response, paced by the system's flow control. A write the
  board can't take now (its buffer of 32 KB is full) is refused whole: a write request then gets
  "insufficient resources", and the deck sends it again. A whole snapshot of 32 agents with every
  field set is about 24 KB.

### Security

Nothing goes over Bluetooth without an encrypted link from **LE Secure Connections** pairing with a
**passkey the board shows on its own screen**, and a **bond**. Seeing the passkey proves the person is
looking at that board, as a USB cable proves it is plugged in.

- The board is display only. When the computer pairs, the board shows a 6-digit passkey and its name
  (`pair deck-1a2b3c`, `042917`, "type it on the computer"); the person types it there.
- Secure Connections only (P-256, 16-byte keys, no legacy pairing), MITM required. RX and TX refuse
  anything else with "insufficient authentication", which is what makes the computer pair. A pairing
  that ends without the passkey (Just Works) is undone and the connection dropped.
- One bond. A board without one is **in setup**: connectable for anyone, and it pairs with whoever
  types its passkey. It doesn't ask for encryption itself: pairing starts when the computer asks for
  RX or TX, so a scan that only reads Info never makes the computer ask for a code. A bonded board
  takes **only its bonded deck**: it refuses any new pairing (it never shows a passkey then), refuses
  a repeat pairing from the bonded deck too, and drops a connection that hasn't encrypted with the
  bond's keys within 10 s.
- **Forget** on the board: hold BOOT (or, while it shows a notice, its screen) for 5 s; it asks
  "forget Bluetooth?"; press again within 10 s. The bond is gone and the board is in setup again.
  Remove the board from the computer's Bluetooth settings too, or that computer can't pair with it
  again.
- A Bluetooth board is still a device with a name, a scope and an audit trail: adding it on the
  computer (`agentctl bluetooth add`, which needs the passkey) approves it; it is never approved
  automatically on first sight. Revoking it ends its sessions.
- What binds the computer's Bluetooth identifier for the board to a device is what the encrypted link
  proves, never the advertisement or a read of Info without encryption. Once TX is on (so the link is
  encrypted and authenticated), the deck reads Info again over it, and its `id` must be the one
  advertised. Then either the board says `Hello` over that link (a board with no other link), or, when
  it has a live session on USB or on Wi-Fi with its certificate, that session must report the secure
  central (`NetStatus.ble = CONNECTED`, sent when it happens) after the attempt began. A `Hello` over
  Bluetooth from a board live elsewhere isn't enough while it is being added
  ([Adding a board that has another link](#adding-a-board-that-has-another-link)).
- Once approved, it pairs for Wi-Fi (`CertRequest`) and takes Wi-Fi credentials (`WifiConfig`) over
  Bluetooth exactly as over USB. The bond and the certificate are kept in the board's NVS, which is
  plain flash unless flash encryption is on (see [Pairing](#pairing-over-usb-serial-or-bluetooth)).

### Advertising

- The advertisement carries the service UUID; the scan response carries the name `deck-` and the last
  six hex digits of the board id, lower case (`AA:BB:CC:1A:2B:3C` is `deck-1a2b3c`).
- A board without a bond always advertises (setup), every 100 to 150 ms, on USB too.
- A bonded board always advertises, so its deck can find it again: every 150 to 300 ms while
  Bluetooth may carry its session ([Uplinks](#uplinks)), and on standby every 1 to 1.3 s while another
  link carries it (to be added, checked, or taken over from when that link goes). On standby it is no
  more open than otherwise: it still takes only its bonded deck. The computer uses resolvable private
  addresses, so the board can't filter by address before a connection; it filters by the bond's keys.
- On a connection it asks for the 2M PHY, 251-byte packets and an interval of 15 to 30 ms (latency 0,
  timeout 4 s); a bonded board then asks for encryption.

### Uplinks

A board may have USB, Wi-Fi and Bluetooth at once, but carries **one session at a time**: a link that
takes over closes the other (on Bluetooth, the board drops the connection).

- USB wins while a deck answers there, and for the first 4 s after boot while the board listens for
  one; a deck silent on USB for 30 s lets the board go elsewhere.
- Off USB, the deck's preference decides, sent in `LinkConfig` and kept by the board across restarts:

  | `uplink` | the board uses |
  |---|---|
  | `AUTO` (default) | Wi-Fi when its session is up, else Bluetooth; it keeps trying Wi-Fi meanwhile and moves to it once it works |
  | `WIFI` | Wi-Fi only; Bluetooth only while it has no Wi-Fi network or certificate yet, so it can still be set up |
  | `BLE` | Bluetooth only; Wi-Fi stays off |

- The board answers `LinkConfig` with a `Result` (204 kept, 400 not a preference, 500 not saved, 501
  no Bluetooth in this firmware) and a `NetStatus` saying what it uses. `LinkConfig` may come over any
  link. The deck sends it to an approved board whose `NetStatus.ble` is set (and not `UNSUPPORTED`)
  when `NetStatus.uplink` differs from the device's preference: at the start of a session and after
  every change, once per value per session; after a 501 it stops for the session.
- A central that has just encrypted (it paired, or used the bond) keeps its link for 20 s even while
  another link carries the session, without a `Hello`: the **introduction**, so the deck can finish
  adding the board. After that, or at once for a connection on which Bluetooth carried the session,
  the board drops a central that Bluetooth isn't wanted for.
- Firmware updates come over Wi-Fi only: a board on Bluetooth refuses an offer ("on Bluetooth: update
  it over Wi-Fi or USB"). While an update downloads the board stops Bluetooth (the TLS download wants
  its memory) and starts it again after.

```
board (no bond, no cable)                         deck (its Bluetooth helper)
  advertises deck-1a2b3c ─────────────────────────▶ scan, connect, read Info (paired=0)
  ◀── subscribe TX / write RX: "insufficient authentication" → the computer pairs
  shows "pair deck-1a2b3c / 042917" ◀── the person types 042917 on the computer
  encrypted, bonded
  │ Hello ──▶│                                      registers it, pending
  │◀── Welcome {approved: false}                    the person approves it
  │◀── Welcome {approved: true, cert_serial: ""}
  │ CertRequest ──▶│◀── CertIssued                  paired for Wi-Fi
  │ NetStatus {transport: BLE, ble: CONNECTED, wifi: NOT_SET} ──▶│
  │◀── WifiConfig {ssid, password} │ Result 204 ──▶│ NetStatus {JOINING} … {IDLE, ip} ──▶│
  (auto) joins Wi-Fi, TLS 1.3 up: drops Bluetooth, Hello over Wi-Fi
```

### Adding a board that has another link

Most boards that get Bluetooth are already on USB or Wi-Fi. They don't say `Hello` over Bluetooth
then (another link carries the session), so the deck confirms the link on the session the board
already has:

```
board (on USB, in setup)                           deck
  │ Hello, Welcome … on USB                         agentctl bluetooth add deck-1a2b3c
  advertises deck-1a2b3c ◀──────────────────────── connect, read Info (paired=0): "type the code"
  ◀── subscribe TX: "insufficient authentication" → the computer pairs
  shows "pair deck-1a2b3c / 042917" ◀── the person types 042917 on the computer
  encrypted, bonded; the introduction starts (20 s, no Hello)
  │ NetStatus {transport: SERIAL, ble: CONNECTED} on USB ──▶│
                                  ◀── read Info again, over the encrypted link: id matches
                                                    bound to this Bluetooth identifier, approved
                                  ◀── hangs up      Bluetooth stands by (USB is in use)
  │ NetStatus {ble: BONDED} on USB ──▶│
  advertises on standby
```

A board the computer is still paired with (an attempt that failed after pairing) goes the same way
with no passkey: the computer encrypts with the bond it holds. Then `agentctl devices uplink NAME ble`
makes Bluetooth the board's uplink when the cable is out, and `auto` or `wifi` moves it back.

| message | direction | field | over |
|---|---|---|---|
| `LinkConfig` | deck → board, answered with `Result` | 66 | any |

`LinkConfig` sets up the board itself: it is not a web API command and has no route.

### Without hardware

`board_wifi --ble PATH` (in `firmware/deck-link/test/host`, `make board_wifi`) is a board whose
Bluetooth link is a Unix socket: it listens at PATH, and whatever connects plays an encrypted, bonded
central (it is never asked for the passkey, which it prints). It speaks the same byte stream as RX and
TX, cut to `--mtu` (default 185), and pairs, takes credentials and the uplink preference as a board
does. With `--serial` or `--wifi` as well it has both links: the first carries the session, a
central gets the introduction and is reported in `NetStatus` there, and Bluetooth takes over when the
first link ends. See the top of `board_wifi.c`.

## Limits

The deck clips text to the sizes in `link.options` (on a UTF-8 boundary, ending in "…") and sends at
most the number of list items given there (for example 6 prompt options, 6 subagents). Firmware built
with nanopb from the same file can therefore use fixed buffers and never sees a field it can't hold. A
CI test fails if the Go limits and `link.options` disagree, and another if any web API endpoint or
Agent field has no Deck Link counterpart.
