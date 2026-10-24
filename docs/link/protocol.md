# Deck Link protocol v1

Deck Link connects small boards (ESP32 and similar) to the agents deck over USB serial. A board sees
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
  (at least 512). The largest frame the deck can send with the limits in `link.options` is 3323 bytes.

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
  │ Hello {board_id, type, firmware, …} ──▶│  registers the board (pending if new)
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
- **Welcome** is sent again whenever the board's approval or scope changes: when you approve it, change
  its scope (`agentctl devices scope <name> act`) or revoke it (`approved: false`).
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
| `Answer {agent_id, prompt_id, key}` | `POST /v1/agents/{id}/answer` | act | `Result`: 204; 409 if the prompt changed; 400 if `key` isn't an option |
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

## Limits

The deck clips text to the sizes in `link.options` (on a UTF-8 boundary, ending in "…") and sends at
most the number of list items given there (for example 6 prompt options, 6 subagents). Firmware built
with nanopb from the same file can therefore use fixed buffers and never sees a field it can't hold. A
CI test fails if the Go limits and `link.options` disagree, and another if any web API endpoint or
Agent field has no Deck Link counterpart.
