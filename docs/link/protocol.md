# Deck Link protocol v1

Deck Link connects small boards (ESP32 and similar) to the agents deck over USB serial, and, once
paired, over Wi-Fi (see [Pairing and Wi-Fi](#pairing-and-wi-fi)). A board sees
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
while another program (a flasher, a serial monitor) has it open.

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
  (at least 512). The largest frame the deck can send with the limits in `link.options` is 3387 bytes.

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
  | 1 | `FIELDS_TILE` | `title tool status status_since_ms folder context unseen lost focused external app hat account activity.tool subagent_count` |
  | 2 | `FIELDS_CARD` | `ai_title cwd branch model model_label effort activity.detail last_prompt error resumable attached subagents started_at_ms updated_at_ms` |
  | 4 | `FIELDS_PROMPT` | `waiting` |
  | 8 | `FIELDS_NUMBERS` | `tokens cost_usd turns` |

  `id` and `slot` are always set. `fields: 0` means `FIELDS_TILE`.
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
| `Focus`, `Interrupt` `{agent_id}` | `POST /v1/agents/{id}/focus`, `…/interrupt` | act | `Result` |
| `Move {agent_id, slot}` | `POST /v1/agents/{id}/move` | act | `Result` |
| `Order {agent_ids}` | `POST /v1/order` | act | `Result` |
| `SetHat {agent_id or folder, shape, color, automatic}` | `POST /v1/agents/{id}/hat`, `POST /v1/hats` | act | `Result` |
| `Dismiss`, `Resume` `{agent_id}` | `POST /v1/agents/{id}/dismiss`, `…/resume` | admin | `Result` |
| `Handoff {agent_id, account}` | `POST /v1/agents/{id}/handoff` | admin | `Result` (200 when it started) |
| `Restore` | `POST /v1/restore` | admin | `RestoreResult` |
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

### Pairing (over USB serial only)

```
board                                   deck
  │◀── Welcome {approved: true, cert_serial: ""}
  │ CertRequest {seq: 9, csr} ──▶│  approved boards only, USB only
  │◀── CertIssued {seq: 9, cert, ca, hosts, port, server_fingerprint, device_name, cert_serial}
```

- The board makes an EC P-256 key **on the chip** (it never leaves it) and sends a certificate signing
  request for it, `CertRequest.csr` (DER, or PEM; at most 600 bytes).
- Only an approved board gets a certificate, and only over USB serial: anything else gets a `Result`
  (403 not approved or not on USB, 400 a bad request, 503 the network listener is off). The deck signs
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

### Wi-Fi credentials (over USB serial only)

`agentctl link wifi <board> --ssid <name>` asks for the password (without echo; `--password` also
works) and the daemon sends it to the board over USB:

```
board                                   deck
  │◀── WifiConfig {seq, ssid, password}    (forget: true to drop them)
  │ Result {same seq, code: 204} ──▶│
  │ NetStatus {wifi: JOINING} … {wifi: IDLE, ip, rssi} ──▶│   the board tries the network once
```

- Only a board on USB serial right now gets them: otherwise the command says so and nothing is kept.
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
When an approved board on a registered USB port has paired (`NetStatus.has_cert`) and reports no
network (`WIFI_NOT_SET`) or another one than the default (`NetStatus.ssid`), the deck sends it
`WifiConfig` with the default, as above:

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
- `CertRequest` and `WifiConfig` are refused over Wi-Fi: pairing and credentials go over USB only.
- The deck pings every 5 s as usual and closes a Wi-Fi session that has been silent for 30 s.
- `hosts` are tried in order: the `.local` name (mDNS on the board), then the addresses.

### Which transport a board uses

At boot a board listens on USB for a few seconds. A deck that answers there (a `Welcome` after its
`Hello`) wins. If nobody does, and the board has a network and a certificate, it joins the network and
connects over TLS. A deck that appears on USB later takes over at once (the board drops Wi-Fi): USB
is preferred, so pairing, new credentials and recovery always work over the cable. A board on USB
whose deck goes quiet for 30 s tries Wi-Fi meanwhile.

### NetStatus

After each `Welcome`, and whenever it changes, a board may say how it is connected: `transport`
(`SERIAL` or `WIFI`), `wifi` (`UNSUPPORTED`, `NOT_SET`, `IDLE`, `JOINING`, `CONNECTED`,
`WRONG_PASSWORD`, `NO_NETWORK`, `FAILED`, `DECK_UNREACHABLE`, `CERT_REJECTED`), `ssid`, `ip`, `rssi` and
`has_cert`. `agentctl link` and `agentctl devices` show it. It is never the password.

| message | direction | field | over |
|---|---|---|---|
| `CertRequest` | board → deck | 60 | USB serial |
| `CertIssued` | deck → board, reply | 61 | USB serial |
| `WifiConfig` | deck → board, answered with `Result` | 62 | USB serial |
| `NetStatus` | board → deck | 63 | either |

These are not web API commands, so they have no route; the parity test keeps commands in fields 30–49
and checks these are documented here.

## Limits

The deck clips text to the sizes in `link.options` (on a UTF-8 boundary, ending in "…") and sends at
most the number of list items given there (for example 6 prompt options, 6 subagents). Firmware built
with nanopb from the same file can therefore use fixed buffers and never sees a field it can't hold. A
CI test fails if the Go limits and `link.options` disagree, and another if any web API endpoint or
Agent field has no Deck Link counterpart.
