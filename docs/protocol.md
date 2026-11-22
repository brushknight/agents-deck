# agents deck protocol v1

One Go daemon (`agentctl serve`) owns all agent state. Clients:

| client | transport | auth |
|---|---|---|
| desk panel (Flutter, 720×720), later a phone app | HTTPS on the network, `:7341`, **TLS 1.3 only** | a **client certificate per device**, signed by the deck's CA; scope view, act or admin |
| web UI (served by the daemon) | HTTP on `127.0.0.1:7340` only | HttpOnly SameSite=Strict cookie set by a one-time login URL from `agentctl web` |
| menu bar app, Claude Code hooks, `agentctl` CLI | unix socket `~/.local/share/agents-terminal/agentd.sock` (0600); on Windows `%LOCALAPPDATA%\agents-deck\agentd.sock` (Windows 10 1803 and later have unix sockets) | filesystem permissions: your user (on Windows, the folder under your profile is private to you) |
| MCP clients (an orchestrating agent such as Claude Code), [mcp.md](mcp.md) | `POST /mcp` on `127.0.0.1:7340` (Streamable HTTP), or `agentctl mcp` (stdio) through the unix socket (`POST /local/mcp`) | an **MCP token** (`Authorization: Bearer adk_…`) on both: made in Settings, MCP, kept only as a SHA-256 hash; scope view, act or admin |
| ESP32 boards (Deck Link) | USB serial, see [link/protocol.md](link/protocol.md) | approved once per board (at once when plugged into a registered USB port, unless turned off); scope view, act or admin |
| ESP32 boards over Bluetooth | Bluetooth LE: the same Deck Link frames as a byte stream through the Deck Link GATT service, run by a small helper next to `agentctl` (`deck-ble` on macOS, [clients/ble](../clients/ble/README.md)) | added by you (`agentctl bluetooth add`), paired with the 6-digit passkey shown on the board's own screen and bonded; then the same device, scope and audit log as on USB |
| ESP32 boards on Wi-Fi | `GET /v1/link` with `Upgrade: deck-link` on the network listener (`:7341`, TLS 1.3): the connection becomes a Deck Link stream ([pairing and Wi-Fi](link/protocol.md#pairing-and-wi-fi)) | the board's client certificate, issued over USB to an approved board; the same device, scope and audit log as on USB |

Bluetooth is a third way for a board to reach the deck, beside USB and Wi-Fi, with the same device,
scope, audit and revocation model: a board on it is a device you approved, never one approved on
sight ([Boards over Bluetooth](#devices-and-security)).

Every listener serves the same `/v1` API. Nothing listens on the network without TLS and a device certificate.
The MCP endpoint is served only on the loopback dashboard (`POST /mcp`) and the unix socket (`POST
/local/mcp`), never on the network, and needs an MCP token on both: each tool call runs the `/v1` API
through the same scope check and audit log as a paired device, as `mcp:<token name>` ([mcp.md](mcp.md)).
The management API (`/admin/v1`, [below](#management-api-adminv1)) is served only on the unix socket and the
loopback dashboard, never on the network.
The loopback listener rejects any `Host` other than `127.0.0.1:7340`/`localhost:7340` and any
mutating request whose `Origin` is not that host.

## Endpoints

| method | path | body | result |
|---|---|---|---|
| GET | `/v1/state` | – | `State` |
| GET | `/v1/events` | – | Server-Sent Events, see below |
| POST | `/v1/agents/{id}/answer` | `{"promptId": "...", "key": "1"}` | `204`; `409` if `promptId` is no longer the agent's current prompt; `400` if `key` is not one of its options |
| POST | `/v1/agents/{id}/prompt` | `{"text": "fix the failing test", "mode": "now"}`; `mode` `now` (default), `interrupt` or `queue`; with `queue`, `"position": "back"` (default) or `"front"` | `204`: types the text into the agent's session and presses Enter, as if you typed it there. Text: 1 to 4000 bytes of UTF-8, newlines and tabs but no other control characters; it goes in as one paste (bracketed, when the program asks for that), so its newlines don't submit it early. Works for agents the deck started (tmux) and agents in herdr, cmux or Orca. `now`: `409` while the agent works on a turn, waits on a question (answer it instead), is still starting or has exited. `interrupt` (or the older `"interrupt": true`): a working or asking agent gets Esc first. `queue`: `200` `{"status": "sent"}` when the agent is idle and nothing is queued for it (typed in at once), else `202` `{"status": "queued", "id": "q3k9x2a", "position": 2}`: kept, and typed in once the agent's turn ends (status `idle`), one queued prompt per turn, next first; `front` puts it before the prompts queued earlier. An agent waiting on a question or permission holds its queue: nothing is answered for you, and the queue goes on after the turn your answer finishes. At most 20 queued per agent (`409` beyond); kept across restarts (`prompt-queue.json`); `409` for an agent that has exited. Agents the deck can't see the status of (Codex, Gemini or a shell started here) take it at any time, in every mode. `422` for a session started by hand in another terminal or in the Codex app: nothing can type into another app's terminal. `404` unknown agent |
| GET | `/v1/agents/{id}/queue` | – | `200` `{"queue": [{"id", "text", "by", "queuedAt"}]}`: the prompts queued for the agent, next first; `by` is who queued it (`mcp:<token>`, a device's name, `web` or `local`). `404` unknown agent |
| POST | `/v1/agents/{id}/queue/{qid}/cancel` | – | `204`: takes a queued prompt back before it is typed in. `404` when it was sent or cancelled already |
| POST | `/v1/agents/{id}/focus` | – | `204` — brings the agent's terminal to the front on the Mac (iTerm tab / overlay) |
| POST | `/v1/agents/{id}/interrupt` | – | `204` — sends Esc to the agent (stops the current turn) |
| POST | `/v1/agents/{id}/dismiss` | – | `204` — stops the agent if still running and removes it from the fleet |
| POST | `/v1/agents/{id}/move` | `{"slot": 9}` | `204` — puts the agent at board position 0..255; free positions are fine (the board can have gaps), and an agent already there swaps into the mover's old position. Used by drag-and-drop on the web and the panel |
| POST | `/v1/agents/{id}/attention` | `{"slot": 3}` | `204`: moves the agent to spot 1..16 of the ["Needs you" grid](#the-needs-you-grid); an agent already there swaps into the mover's old spot (or waits for a spot, when the mover had none). The new spot is also the one it gets back when it returns. `409` when the agent isn't on the grid. Used by drag-and-drop on the web's "Needs you" screen |
| GET | `/v1/live` | – | `200` `{"sessions": [{"sessionId", "title", "cwd", "folder", "started", "onBoard", "background"}]}` — active Claude sessions on this machine that agentctl didn't start: in terminals, or in the background (`claude --bg`, the Claude desktop app) |
| POST | `/v1/agents/{id}/hat` | `{"shape": "cap", "color": "teal"}` (`color` optional), `{"shape": "none"}`, or `{"auto": true}` | `204` — picks the hat of the agent's working folder (every agent working there wears it), or back to the automatic one; stored in `config.json` `"hats"` |
| GET | `/v1/hats` | – | `200` `{"shapes": […], "colors": […], "overrides": {"<folder>": {"shape", "color"}}}` |
| POST | `/v1/hats` | `{"folder": "/Users/sam/dev/app", "shape", "color"}` or `{"folder", "auto": true}` | `204` — the same, by folder |
| POST | `/v1/live/{session}/add` | `{"slot": 9}` (optional) | `204` — puts that session on the deck, watched (`external: true`, `tool: "claude"`): status, context, cost and subagents from its transcript; answer, interrupt and resume return an error; dismiss takes it off the deck. It stays added: it comes back whenever it runs |
| POST | `/v1/restore` | `{"ids": ["k3f9a2"]}` (optional) | `200` `{"results": [{"id", "title", "action": "resumed\|relaunched\|removed\|failed", "error"}]}`: brings back the listed agents with `lost: true`, or every one when `ids` is missing or empty: Claude agents on their own conversation, other tools fresh in their folder, each in its old slot. A listed id that isn't lost comes back `failed` with the reason |
| POST | `/v1/stash` | `{"all": true}`, `{"ids": ["k3f9a2", …]}` or `{"ids": []}` | `204`: the magic trick ([features](features.md#magic-trick)), from its receiver: take the agents the giver shows (`all`, at most 16) or these, or give them back (`[]`; this computer may give them back too). While taken, the giver leaves them out of its state; every other screen is unchanged. Given back by itself 30 s after the receiver goes quiet (it repeats itself every 5 s). `409` when the trick is off, `403` from a device that isn't its receiver |
| POST | `/v1/magic/view` | `{"ids": ["k3f9a2", …]}` | `204`: from the magic trick's giver, the agents it shows now, in order (at most 16): what the next take takes. From anyone else it is ignored |
| POST | `/v1/order` | `{"ids": ["k3f9a2", "h0m3l4", …]}` | `204` — compacts: puts those agents in slots 0..n-1 in that order (others keep their order after them) |
| POST | `/v1/agents/{id}/resume` | – | `204` — restarts an exited Claude agent (`resumable: true`) on its previous conversation, same slot |
| GET | `/v1/accounts` | – | `200` `{"accounts": [{"name": "work", "color": "yellow", "default": true}]}` — the Claude accounts agents can run in (`config.json` `"accounts"`); empty while only one is set up |
| POST | `/v1/agents/{id}/handoff` | `{"account": "personal"}` | `200` `{"id": "k3f9a2"}` — continues a Claude session in another account, for when one runs out of usage: its transcript is copied into that account's folder and Claude resumes it there. An agent the deck started stops and comes back in the same slot (same `id`); a watched session keeps running in its own terminal and the copy starts as a new agent (its `id`) |
| GET | `/v1/firmware/{board}/{version}` | – | `200` the app image (`application/octet-stream`, `ETag` and `X-Firmware-SHA256` its SHA-256); `206` for a `Range` request. **Network listener only, Deck Link boards only**: the board's certificate must belong to a board whose type runs that board's firmware (`403` otherwise), `503` while the service has no firmware catalog, `404` for a version it doesn't have. Every request is audited. A board fetches it after the deck's `FirmwareOffer` ([link/protocol.md](link/protocol.md#firmware-updates-over-wi-fi)); it is board management, not a command, so it has no Deck Link command of its own |

Errors are `{"error": "message"}` with a 4xx/5xx status.

### `/v1/events`

`text/event-stream`. On connect the server sends one `state` event, then another `state`
event after every change (debounced ~150 ms), and a `: ping` comment every 15 s.
Clients reconnect with backoff (1 s → 10 s) and treat >40 s without bytes as dead.

```
event: state
data: {...State...}

```

The full state is small (≤ a few KB per agent), so there are no deltas.

## Types

```jsonc
// State
{
  "server": { "name": "workstation", "version": "0.1.0", "time": "2026-10-08T01:02:03Z" },
  "agents": [ /* Agent, ordered by slot */ ]
}

// Agent
{
  "id": "k3f9a2",                 // stable, [a-z0-9]{6}
  "slot": 0,                      // 0-based board position, stable for the agent's life; gaps allowed
  "attentionSlot": 3,             // its spot on the "Needs you" grid, 1..16 (4×4, by rows); 0 when it isn't there
  "title": "checkout-api",          // user-facing name (folder name by default)
  "aiTitle": "Fix refresh token race", // Claude's own session title, may be ""
  "tool": "claude",               // claude | codex | gemini | shell
  "status": "running",            // starting | running | waiting | idle | error | exited
  "statusSince": "2026-10-08T00:51:00Z",
  "cwd": "/Users/x/dev/checkout-api",
  "folder": "checkout-api",
  "branch": "main",               // may be ""
  "model": "claude-opus-5-5",     // may be ""
  "modelLabel": "opus 5.5",       // short display form
  "activity": { "tool": "Edit", "detail": "src/auth/session.ts" }, // or null; only while running. Besides
                                  // Claude's tools: "Compact" while compacting, "Task" for subagents at work
  "lastPrompt": "fix the refresh-token race in session.ts …",      // truncated to 280 chars
  "waiting": null,                // Prompt, only when status == "waiting"
  "error": null,                  // { "message": "api 529 overloaded" } only when status == "error"
  "tokens": { "input": 1200, "output": 84000, "cacheRead": 3100000, "cacheWrite": 210000 },
  "context": { "used": 410000, "window": 1000000 }, // used = last request's prompt size
  "costUsd": 4.12,                // estimate from public list prices
  "turns": 27,                    // user prompts in this session
  "focused": false,               // true when its terminal is the front iTerm tab on the Mac
  "attached": true,               // a terminal is currently showing it
  "resumable": false,             // exited Claude agent that /resume can bring back
  "lost": false,                  // omitted when false: ended with the tmux server (crash, reboot), not by itself; POST /v1/restore brings it back
  "unseen": false,                // finished a turn nobody has looked at yet ("hungry"); cleared when its terminal is focused or it gets a new prompt
  "external": false,              // mirrored from another app (a Codex app thread): focus opens it there, dismiss hides it; answer/interrupt/resume return an error
  "app": "orca",                  // omitted for agentctl's own: the app an external agent runs in: "orca" (the Orca app), "herdr" or "cmux";
                                  // focus switches to its terminal there, answer and interrupt work; dismiss hides it until it's active
  "hat": { "shape": "propeller", "color": "blue", "auto": true }, // omitted when none: the pixel hat for its working folder;
                                  // shape: hard hat, cap, beanie, helmet, wizard, beret, chef, crown, headphones,
                                  // propeller, top hat, cowboy, bandana · color: teal, blue, yellow, green, purple,
                                  // pink, sky, ink · auto: picked by FNV-1a of the folder (shape = h mod 13,
                                  // colour = (h >> 8) mod 8), false when chosen by hand
  "account": { "name": "work", "color": "yellow" }, // omitted while only one Claude account is set up:
                                  // the account (Claude config folder) the session runs in; color is a hat colour
  "queued": 0,                    // prompts waiting to be typed in once its turn ends (POST /v1/agents/{id}/prompt, "mode": "queue")
  "subagents": [                  // omitted when none: subagents (and workflow runs, type "workflow") still
                                  // working, oldest first. While only they work (Claude's turn is over) the
                                  // agent is "running" with activity {"tool": "Task", "detail": "2 subagents working"}
    { "id": "a1f3c9", "title": "map the payment retry paths", "type": "Explore", "tool": "Grep" }
  ],
  "startedAt": "2026-10-08T00:13:00Z",
  "updatedAt": "2026-10-08T00:51:02Z",   // any change to this record
  "lastActive": "2026-10-08T00:50:41Z"   // omitted until known: its latest message sent to it or received from it.
                                  // Claude: the newest user or assistant line of its transcript (prompts, replies,
                                  // tool calls and results; not titles, summaries, hook or system notes, isMeta
                                  // text or subagents). Codex: the same from its rollout, and Codex's own thread
                                  // time. Orca and cmux: their own time. Shells and other tools started here:
                                  // tmux's last output (not their start-up). The service also logs it itself
                                  // when it sees a prompt, a tool call, a turn end or an answer from the deck
                                  // (herdr: while it says working). Kept across restarts, never moved back
}

// Prompt — something the agent is blocked on
{
  "id": "p17",                    // changes every time a new prompt appears; send it back with the answer
  "kind": "permission",           // permission | question | input
  "title": "run this command?",   // one line
  "detail": "terraform apply -target=module.staging_net", // command, file path, question text…; may be multi-line
  "context": "bash · ~/dev/infra-network/terraform",
  "options": [                    // empty for kind == "input" (free text: use focus instead)
    { "key": "1", "label": "yes", "primary": true },
    { "key": "2", "label": "yes, and don't ask again for terraform" },
    { "key": "3", "label": "no" }
  ]
}

// Option — "selected": true marks a ticked answer of a pick-any question (omitted otherwise)
{ "key": "2", "label": "cheese", "selected": true }
```

A question with several parts (Claude's form: more than one question, or one where you can pick more
than one answer) comes one step at a time, each with a new `id`:

- each question in turn, `kind: "question"`, with `context` like `"size · question 2 of 3"`. Answering
  picks and moves to the next question.
- a pick-any question (`context` ends in `"pick any"`) lists its answers with `selected` on the ticked
  ones; answering one ticks or unticks it and sends a new prompt with the ticks updated. Its last
  option, `"next — …"`, moves on.
- the review: `title: "submit these answers?"`, the answers in `detail` (one per line), options
  `1` submit answers and `2` cancel.

Free text ("type something") has no option key. The web page sends it with
`POST /v1/agents/{id}/prompt` and `"interrupt": true`: Esc closes the question, then the text goes in
as the next prompt. An answer typed at the terminal moves the deck's prompt along with it (an older step's answer gets `409`), and the prompt clears once Claude has the
answers or the form is cancelled.

Status meanings (what the tiles show):

| status | meaning | tile |
|---|---|---|
| `starting` | launched, Claude not ready yet | dim critter, "starting" |
| `running` | working on a turn | ink critter walking, `activity` + context % |
| `waiting` | blocked on the user (`waiting` set) | **full accent tile**, "?" |
| `idle` | turn finished, waiting for a new prompt | dim critter asleep |
| `error` | API error or crashed | accent outline, X eyes, "!" |
| `exited` | process gone; stays 10 min (resumable), then moves to the CLI history (`agentctl resume`) | dim, hollow |

### The "Needs you" grid

The service keeps one "Needs you" grid of 16 spots (4×4, numbered 1 to 16 by rows) and puts each
agent's spot in `attentionSlot`, so every screen shows the same agents in the same spots.

- **Who is on it:** an agent that is `starting`, `running` or `waiting`, or `idle` with `unseen`
  (finished a turn you haven't looked at yet). It stays 5 seconds after it stops; back within those 5
  seconds and it never left. The service sends the change when the 5 seconds run out.
- **Where:** nobody moves when another agent leaves. A newcomer takes the lowest free spot. An agent
  that was on the grid before gets its old spot back when that is free, otherwise a random free spot,
  which becomes its old spot. `POST /v1/agents/{id}/attention` moves an agent by hand.
- **More than 16:** the others wait with `attentionSlot: 0` and get spots as they free up, the one that
  came first first (then the lowest `slot`).
- **Restarts:** the old spots of agentctl's own agents are kept across a service restart, so they come
  back to the same spots. The 5 seconds after an agent stops are not kept, nor are the old spots of
  agents mirrored from other apps.
- **Smaller screens** (fewer than 16 cells) show the agents in order of their spot, lowest first, in
  the cells they have; the rest aren't shown, but the count on the "Needs you" mark includes them.
  Newcomers take the lowest free spot, so the first spots fill first. Two screens of the same size
  always show the same agents.
- **An older service** sends no `attentionSlot` (always `0`). When no agent has a spot but some need
  you, clients show those agents in deck order, without the 5 seconds and without remembered spots.

## Devices and security

**The CA.** On first start the daemon creates its own certificate authority: `ca.pem` in the data
folder (public), its EC P-256 key in the macOS Keychain (`dev.agents-deck`, item `ca-key`; a 0600 file
under `keys/` on Linux and Windows). It signs the network listener's certificate (`server-cert.pem`,
reissued when the Mac's names or addresses change) and every device's certificate.

**The listener** (`:7341`) accepts TLS 1.3 only. A connection may come without a client certificate,
but then the only thing it can do is pair. With one, the certificate must chain to the deck's CA and
belong to an active device in `devices.json`; a revoked device fails the handshake.

**Scopes.** Each device has one:

| scope | may |
|---|---|
| `view` | `GET /v1/state`, `/v1/events`, `/v1/hats`, `/v1/accounts`, `/v1/agents/{id}/queue` |
| `act` | also answer, send and queue prompts (and cancel queued ones), focus, interrupt, move, order, hats |
| `admin` | also dismiss, resume, hand over to another account, restore, `GET /v1/live`, add a live session |

A request beyond the device's scope gets `403` with the command that would raise it.

**Boards on USB.** The daemon opens only the serial ports you registered (`agentctl link add`). A new
board that says `Hello` on one of them is approved at once with scope `act`: plugging a board into
this Mac, on a port you registered, is treated as your consent. It is audited (`"action": "approve",
"detail": "approved automatically over USB on /dev/cu.usbmodem…, scope act"`). `admin` is only ever
given by hand (`agentctl devices scope`). It never happens over Wi-Fi, on the network listener, over Bluetooth
(a board there is approved when you add it, below) or for `agentctl link sim`, and a revoked board stays revoked until `agentctl devices approve`. Turn it off
with `agentctl devices auto-approve usb off` (new boards then wait for `agentctl devices approve`), or
give new boards `view` with `--scope view`; the setting lives next to `devices.json`
(`devices-settings.json`) and `agentctl devices` shows it.

**Boards on Wi-Fi.** A Deck Link board is paired over USB serial (or over Bluetooth, below): once it is approved, it sends a
certificate request for a key it made on the chip, and the deck signs it and records it on the board's
own entry (`CertRequest`/`CertIssued`, [link/protocol.md](link/protocol.md#pairing-and-wi-fi)). Over
Wi-Fi it connects to this listener with that certificate and opens `GET /v1/link` (`Upgrade:
deck-link`), a transport rather than an API call: the connection carries Deck Link frames, its
commands go through the same scope check and audit log under the board's name, its `Hello` must carry
the board id the certificate was issued to, and revoking the board ends the session and fails its
next handshake (the certificate is dropped for good; approved again, the board pairs again over USB).
Wi-Fi passwords never go through `/v1`: `agentctl link wifi` hands them to the daemon on the unix
socket (`POST /local/link/wifi`), which passes them to a board connected over USB or Bluetooth right
now and keeps nothing. The one exception is the default network (`agentctl link wifi --default --ssid NAME`, `POST
/local/link/wifi-default`): its password is kept in the macOS Keychain (`dev.agents-deck`, item
`wifi-default`; a 0600 file under `keys/` elsewhere), and the deck sends it, over USB or a paired
Bluetooth link (never over Wi-Fi), to every approved board that is paired and lacks it, once per
connection, unless the board was given its own network. It is never logged or audited (`"action": "wifi default set"` without the network or password).

**Boards over Bluetooth.** The rule, never weaker than this:

> Nothing over Bluetooth without an encrypted link from LE Secure Connections pairing with a passkey
> the board shows on its own screen, and a bond. Seeing the passkey proves the person is looking at
> that board, as a USB cable proves it is plugged in. A BLE board is still a device with a name, a
> scope and an audit trail, approved by the person (never automatically on first sight), and
> revocable. Wi-Fi keeps TLS 1.3 and the device certificate, unchanged.

In practice:

- A board seen over Bluetooth is never connected to or approved on sight. `agentctl bluetooth scan`
  lists boards nearby from what they advertise and their Info characteristic (board, board id,
  firmware, whether it is in setup; readable without pairing, so it is a claim, not a proof).
  `agentctl bluetooth add <name>` (`POST /admin/v1/bluetooth/boards/{id}/add`) connects; the board's
  RX and TX characteristics need an authenticated, encrypted link, so the computer pairs: the board
  shows a 6-digit code and you type it into the window the computer opens. The deck never sees the
  code.
- The binding rests on what the encrypted link proves, never on the advertisement or the Info read
  without encryption. Once TX is on (the link is encrypted and authenticated), the helper reads Info
  again over it: its board id must be the one the scan showed. Then:
  - a board with no other link says `Hello` over Bluetooth, with that board id, or it is refused;
  - a board with a live session on USB, or on Wi-Fi with its device certificate, carries its session
    there and says no `Hello` over Bluetooth: that session must report the secure central
    (`NetStatus.ble` `CONNECTED`) after the add began. A `Hello` over Bluetooth from such a board is
    refused while it is being added. The board keeps the new link for 20 s for this, then lets it go;
    Bluetooth stands by.

  Then the board is approved with scope `act` (or `view`; never `admin`, which is given by hand),
  bound to this computer's identifier for it, and audited (`"action": "approve", "detail": "added over
  Bluetooth with the passkey on its screen, scope act"` and `"bluetooth add <name>"`, whose detail says
  what confirmed it). A revoked board is refused until `agentctl devices approve`.
- A board this computer is still paired with (an add that failed after the code was typed) is added
  the same way with no code: the computer encrypts with the bond it holds, which came from the passkey.
- After that it is a device like any other: same scope check, audit log, `agentctl devices`. Its
  later links must say `Hello` with the same board id. Revoking it ends its Bluetooth link and forgets
  it; approved again, it is added again with a new passkey.
- A board over Bluetooth is in your hands as on USB: it may be given Wi-Fi credentials (yours, or the
  default network) and paired for Wi-Fi over that link. Pairing (`CertRequest`) and credentials
  (`WifiConfig`) go only over USB or an encrypted, bonded Bluetooth link, never over Wi-Fi. A board
  whose `NetStatus` names another transport than the link it came over gets neither on that link.
  Firmware is never sent over Bluetooth: an update needs Wi-Fi or the cable.
- The helper (`deck-ble`) only carries bytes between the service and the radio. It stores no
  credentials, keys or passkeys and never sees the code: the operating system pairs and keeps the
  bond.
- A board with no bond advertises as connectable for anyone (it is in setup); it asks for no code
  until the computer asks for RX or TX, so scanning never pops up a pairing window. A bonded board
  keeps advertising (less often while another uplink carries it) so this computer can always reach it
  again, accepts only its bonded computer, and goes back to setup only from its own screen.

Known limits, plainly:

- **The service can't check the encryption.** macOS doesn't tell a program whether a Bluetooth link is
  encrypted, how it was paired or whether a bond exists (a successful TX subscription is the only
  sign; see [clients/ble](../clients/ble/README.md)). So the rule is enforced by the board's firmware:
  it refuses RX and TX on any link that isn't encrypted and authenticated with a passkey, and undoes a
  pairing that ended without one.
- **A stranger can hold the connection for a while.** A board takes one Bluetooth connection at a
  time. Anyone in range can connect to a bonded board; it drops a connection that hasn't encrypted with
  its bond's keys within 10 s, so a stranger can keep it busy for up to 10 s at a time (and again).
- **Bond keys sit in plain flash.** The board keeps its bond (and its Wi-Fi certificate and password)
  in NVS, which is not encrypted unless flash encryption is turned on: someone holding the board can
  read them. Revoke a lost board, and put it back in setup from its screen.
- The board's advertised name and Info are public: anyone nearby can see that a board is there, which
  board it is and which firmware it runs.

The bond lives in the operating system's Bluetooth settings, not in the deck's data folder. When a
board forgot this computer (put back in setup), the Boards page and `agentctl bluetooth` say so: remove
the board in System Settings, Bluetooth, then add it again.

**One session per board.** A board may reach the deck over USB, Wi-Fi and Bluetooth at once; the deck
runs one Deck Link session for it. USB always comes first (a cable is in your hands, and the board
quiets its radios while a deck answers there). Between the radios, the board's uplink preference
decides, with the same meaning the board gives it: `auto` (USB, else Wi-Fi, else Bluetooth), `wifi`
(USB, else Wi-Fi; Bluetooth only while the board isn't paired for Wi-Fi yet, to set it up) or `ble` (USB,
else Bluetooth; Wi-Fi isn't used). Set it with `agentctl devices uplink <name> auto|wifi|ble` or on
the Settings page; it is kept in `devices.json`, and the deck sends it to the board in `LinkConfig`
(over whatever link the board is on) when the board's `NetStatus` shows another one, so the board
picks its radio the same way ([Uplinks](link/protocol.md#uplinks)). When a better uplink says `Hello`,
the session on the worse one ends; a session that has heard nothing from its board for two pings
(10 s) doesn't count, since the board has left it. Bluetooth isn't connected while a better uplink is
in use, and comes back by itself when it is the best one left.

**Firmware updates over Wi-Fi.** A board on Wi-Fi is updated through the same listener and the same
certificate: the deck sends it `FirmwareOffer` over its Deck Link session (never automatically: you
start it), and the board downloads the image from `GET /v1/firmware/{board}/{version}` with its
certificate. Only Deck Link boards may download, only images for their own kind of board, each request
is audited (with the byte range), and a board revoked mid-download is cut off. The board checks the
image's SHA-256 from the offer before it restarts into it, and goes back to its old firmware if the new
one doesn't reach the deck. A board on USB is updated over the cable instead. Until the Settings page
has a button for it, `agentctl link update <board> --image FILE --board B --version V` starts one with an
image file from the Mac (`POST /local/link/firmware` on the unix socket, audited as `firmware <board>`).

**Pairing.** Two ways:

- `agentctl pair desk-panel --bundle panel.json` makes the device's key and certificate on the Mac and
  writes a bundle to copy to the device once (over SSH, or pasted into the panel's setup page):

  ```json
  { "name": "desk-panel", "url": "https://my-mac.local:7341", "ca": "-----BEGIN CERTIFICATE-----…",
    "fingerprint": "sha256:AB:CD:…", "cert": "-----BEGIN CERTIFICATE-----…", "key": "-----BEGIN PRIVATE KEY-----…" }
  ```

  Clients verify the server against `ca`; `fingerprint` (the listener's own certificate) is accepted
  when the host name isn't one the certificate covers.
- `agentctl pair phone` prints a one-time code (2 minutes, single use). The device makes its own
  EC P-256 or P-384 key and sends `POST /v1/pair {"code", "csr"}` (the CSR in PEM) without a client
  certificate; the answer is `{"name", "cert", "ca"}`. Its private key never leaves it.

Pairing a name again replaces its certificate. `agentctl devices` lists everything paired, and how
each board is connected right now (USB, Wi-Fi or Bluetooth);
`agentctl devices revoke|approve|remove <name>` and `agentctl devices scope <name> <scope>` manage them.

**Audit.** Every request that changes something is appended to `audit.log` as one JSON line:
`{"time", "device", "action": "POST /v1/agents/k3f9a2/answer", "status": 204}`; firmware downloads
(`GET /v1/firmware/…`) are recorded too, with the byte range in `detail`; the web dashboard is
`web`, the unix socket `local`, an MCP token `mcp:<name>` (and `"action": "start agent"` for MCP's
`start_agent`).

## Management API (`/admin/v1`)

The calls the Settings page, the tray app and `agentctl` use to run this computer's deck: settings,
boards, USB ports and flashing, updates and diagnostics.

**Where it is served.** On the unix socket, and on the loopback dashboard (`127.0.0.1:7340`) behind
the same session cookie, `Host` check and `Origin` check as `/v1`. **Never on the network listener**:
`:7341` answers 404 to every `/admin/` path, whatever the device's certificate or scope
(`TestAdminNeverOnNetwork`). No board or phone can change settings, flash, update or read diagnostics.

**No Deck Link counterpart, by design.** Boards never manage the computer, so none of these calls has
a `link.proto` command. They sit in their own table below, with paths that start with `/admin/v1`,
so the parity test (`TestEndpointParity`, which reads the `/v1` table above) is untouched; the
admin package checks this table against its own routes (`TestDocsListEveryCall`).

| method | admin path | body | result |
|---|---|---|---|
| GET | `/admin/v1/system` | – | `{"version", "platform", "install", "started", "boards": {"online", "total", "pending"}, "ports": {"new"}, "update": {"state", "version"}, "firmware_updates"}`: the tray app's status line |
| GET | `/admin/v1/settings` | – | every setting as one document: `startAtLogin` (`null` where there is none), `trayAtLogin` (Linux, when the tray app is installed), `notifications` (`off`, `attention`, `finished`), `sound`, `restore` (`ask`, `auto`), `updates` (`notify`, `auto`, `off`), `network`, `networkRunning`, `wifi.ssid`, `cli` |
| PATCH | `/admin/v1/settings` | any of those keys; `"wifi": {"ssid", "password", "send"}` or `{"forget": true}` | `{"settings", "message", "restartNeeded", "sent"}`. The Wi-Fi password goes to the OS secret store and to boards on USB or Bluetooth; it is never returned, logged or audited. Start at login registers or removes the service (and the Linux tray app's autostart entry) after answering, since that restarts or stops the service |
| POST | `/admin/v1/restart` | – | `202`: restarts the service through what starts it at login |
| POST | `/admin/v1/cli` | `{"installed": true\|false}` | adds or removes `~/.local/bin/agentctl` (a link to this agentctl; never a file it didn't make) |
| POST | `/admin/v1/open-data-folder` | – | `204`: shows the data folder in the file manager on this computer |
| GET | `/admin/v1/agents` | – | `{"tools": [{"id", "name", "found", "on", "agents", "state", "detail", "note"}], "accounts": [{"name", "color", "default", "agents", "costUsd"}], "colors"}` |
| POST | `/admin/v1/accounts/{name}/color` | `{"color"}` | `204` |
| POST | `/admin/v1/accounts/{name}/default` | – | `204`: new agents start in this account, as `agentctl accounts default <name>` does; `404` for an account that isn't set up |
| GET | `/admin/v1/projects` | – | `{"projects": [{"id", "name", "folders", "folder", "paths", "path", "tool", "account", "hat": {"shape", "color"}}], "tools", "canStart"}`: what you start agents in (`projects.json` in the data folder), each one or more folders (at most 20). `id` stays the same through renames. `folders` are as written (`~/` allowed), `paths` the same in full; `folder` and `path` are the first, where agents start by default. `tool` is `claude`, `codex` or `shell` (empty: claude), `account` a Claude account (empty: the default), `hat` empty for the folder's own; `canStart` is false where this service can't start agents (demo mode) |
| POST | `/admin/v1/projects` | `{"name", "folders", "tool", "account", "hat"}` (`"folder"` alone: one folder) | the project as kept. Repeated folders are dropped. `400` for an empty or long name (40 characters), no folder or more than 20, a folder that isn't a full path to a folder on this computer (`~/` is allowed), an unknown tool, account or hat; `409` for a name that is taken (names compare without case) |
| POST | `/admin/v1/projects/{name}/{action}` | `{name}` is the project's name or id. `update` the whole project (it may change its name, not its id); `remove`; `start` `{"prompt", "tool", "account", "folder"}` | `update`: the project; `remove`: `204`; `start`: `202` `{"id", "project", "folder", "tool", "prompted"}`: starts an agent in the project's first folder (`folder` picks another of its folders; `400` for one that isn't) with the project's tool and account (`tool` and `account` pick others for this agent), titled with the project's name, and sends `prompt` once it is ready. `503` where agents can't be started |
| GET | `/admin/v1/devices` | – | `{"devices": [{"id", "name", "kind", "scope", "state", "online", "connection", "port", "type", "board", "model", "firmware", "wifi", "update": {"version", "how", "ready", "note", "source"}, "job", "uplinkPref", "uplinks": [{"via", "state", "detail"}], "bluetooth", "hasBluetooth", "bleState", "boardUplink", "uplinkNote", "chip", "status": {"state", "text", "latest", "how", "ready", "note", "source"}, "fileVia", "fileNote"}], "latest", "releaseKey", "ota", "catalogError"}`. `status` is a board's one line about updates: `state` `update` (a newer release this build can install; `how` `usb`, `wifi`, `bluetooth` or `unplugged`, `ready` when it can be installed now, else `note` says why), `local` (a build made on this computer, in `firmware-builds`, newer than what the board runs and than a release this build can install, with or without a release key; `how`, `ready` and `note` as for `update`: `usb` writes its full image, `wifi` sends its app image, so a build without one is not `ready` on Wi-Fi), `current`, `newer` (it runs something newer than the latest release, such as a development build), `own` (a version that isn't a release's), `nokey` (a newer release, but this build has no release key), `error` (the release list couldn't be read; `text` says why), `unlisted`, `unknown` or `checking`; versions compare by semver precedence (`0.2.0-dev.4` < `0.2.0-dev.10` < `0.2.0`); `source` says where `latest` comes from, `release` or `local`. `fileVia` lists how a firmware file picked on this computer would reach the board now (`usb`, `wifi`), `fileNote` why it can't. `connection` is the uplink in use (`usb`, `wifi`, `bluetooth`, `network`); `uplinks` lists every way a board reaches the computer now (`via` `usb`, `wifi`, `bluetooth`; `state` `in use`, `standing by` (another uplink is in use), `connecting` (waiting for it to come in range; `detail` says why the last try failed), `not used` (its preference), `not connected` (Wi-Fi, for a board paired for it), `off`); `bluetooth` is true for a board added over Bluetooth here; `hasBluetooth` for a board whose firmware has it. `bleState` (`not supported`, `in setup`, `paired`, `connected`) and `boardUplink` (`auto`, `wifi`, `ble`) are what the board says in `NetStatus`; `uplinkNote` says what went wrong sending it its preference |
| POST | `/admin/v1/devices/{id}/{action}` | `approve`, `revoke`, `remove`; `scope` `{"scope"}`; `rename` `{"name"}`; `wifi` `{"ssid", "password"}`, `{"forget": true}` or `{"useDefault": true}`; `update` `{"version", "source"}`; `uplink` `{"uplink": "auto"\|"wifi"\|"ble"}` | `204`, or for `wifi` `{"sent", "message"}`, for `update` `202` and a job. `rename` changes the name Settings and `agentctl` show; the id stays. `update` flashes a board on USB through its registered port and offers a board on Wi-Fi the update over the air; a board only on Bluetooth gets `409`. `source` `local` installs a build made on this computer (`version` empty: the newest for the board), checked again first (`409` when its files no longer match its manifest, or on Wi-Fi without an app image); `release` a release's; empty, whichever the board's `status` offers. `uplink` `ble` needs the board added over Bluetooth first |
| POST | `/admin/v1/devices/{id}/firmware-file` | the file itself (`application/octet-stream`, at most 32 MB); `X-File-Name` names it | a firmware file picked on this computer, read before anything goes to the board: `{"file", "name", "size", "sha256", "kind", "chip", "version", "project", "via", "board", "chipChecked", "device"}`. `kind` `app` (an app image, sent over Wi-Fi) or `full` (bootloader, partition table and app, written at `0x0` over USB); `via` is how it would be installed; `version` is the app's own when it reads as one. `400` for a file that isn't an ESP32 image, is for another chip than the board's, is the wrong kind for how the board is connected, or an app image bigger than an app slot; `413` over 32 MB; `409` when the board isn't on USB or Wi-Fi. Kept an hour for the install |
| POST | `/admin/v1/devices/{id}/install-file` | `{"file", "version"}` (`version` when the file doesn't say one: what the firmware reports) | `202` and a job (`kind` `board-update`, `file` the file's name): over Wi-Fi the board is offered the file as that version, as `agentctl link update --image` does; over USB the flasher writes it at `0x0`, as `agentctl flash` does. The file isn't checked against a release signature (it is the owner's choice on this computer, also in a build without a release key); the board checks what it always checks. `409` while the board updates or when it isn't connected that way any more |
| POST | `/admin/v1/pair` | `{"name", "scope"}` | a one-time pairing code, as `agentctl pair` gives |
| GET | `/admin/v1/ports` | – | `{"ports": [{"id", "path", "vid", "pid", "likely", "state", "registered", "device", "job"}]}`, states `new`, `registered`, `connected`, `busy`, `flashing`, `unplugged`, `ignored`, `other`. Listed from the system's USB ids: **no port is opened to list it** |
| POST | `/admin/v1/ports/{id}/{action}` | `register`; `identify`; `flash` `{"board", "version", "source"}`; `forget` | `register` is "yes, it's mine" (`config.json` `link.ports`). `identify` and `flash` answer `403` until the port is registered. `identify` answers from the board's Deck Link session when one is live, else borrows the port from the Deck Link manager and asks the ROM bootloader: `{"source", "chip", "flashSize", "candidates", "local"}` (`local`: the newest builds made on this computer that fit the chip and flash, or the board's own). `flash` with `"source": "local"` writes the full image of a build made on this computer at `0x0`, checked again first, and only on a board whose chip is known (identify, or the agents deck firmware on it) and matches; otherwise `flash` downloads and checks the image, borrows the port, writes it at the manifest's offset and hands the port back so the board pairs: `202` and a job; one at a time per port. `forget` stops using a registered port, or marks a new one "not mine" |
| POST | `/admin/v1/ports/{id}/firmware-file` | the file itself (`application/octet-stream`, at most 32 MB); `X-File-Name` names it | a firmware file picked on this computer for a board with no agents deck firmware yet, read before anything goes to the board; the same answer as the device call, with `port` set, `device` empty and `via` `usb`. Only a full image is taken (bootloader at the chip's bootloader offset, partition table at `0x8000`, app), checked against the chip the port's bootloader answered at `identify`: its chip, and a flash size the board has. `400` for an app image (it only starts where a bootloader and a partition table already are, and the bootloader can't say whether they are), another chip, or an image bigger than the board's flash or built for more; `409` before `identify`, when another USB device is on the port since, while it is written to, or when the board runs agents deck (use the device call); `413` over 32 MB. Kept an hour for the install |
| POST | `/admin/v1/ports/{id}/install-file` | `{"file"}` | `202` and a job (`kind` `flash`, `file` the file's name, `port`): borrows the port, writes the file at `0x0` and hands the port back so the board pairs. Not checked against a release signature (also in a build without a release key); `404` for a file picked for another port or no longer here, `409` as for `firmware-file` |
| GET | `/admin/v1/bluetooth` | – | `{"available", "why", "helper", "radio", "scanning", "restarts", "lastError"}`: `helper` is `none` (this computer has no Bluetooth helper: "Bluetooth isn't available on this computer yet"), `starting`, `running` or `restarting`; `radio` is `on`, `off`, `unauthorized` (not allowed in the system's privacy settings), `unsupported`, `resetting` |
| POST | `/admin/v1/bluetooth/scan` | `{"on": true\|false}` (on when left out) | `204`; `503` with `why` when Bluetooth isn't available. Starting a scan forgets the boards seen before |
| GET | `/admin/v1/bluetooth/boards` | – | `{"boards": [{"id", "name", "rssi", "board", "model", "boardId", "firmware", "setup", "device", "added"}], "scanning", "available", "why"}`: the boards seen since the scan started, strongest first. `setup`: not bonded, ready to be added; `device`: already one of your devices; `added`: added over Bluetooth here |
| POST | `/admin/v1/bluetooth/boards/{id}/add` | `{"scope": "view"\|"act"}` (the setting for new boards when left out) | `202` and a job (`kind` `bluetooth-add`): stages `connect`, `passkey` (only for a board in setup: "Type the code shown on deck-1a2b3c into the window this computer opens.", sent once the helper is connected and has asked for TX, which is when the computer opens its window), `connected` (the link is encrypted; the deck checks with the board), `done` with `device`; or `failed` with the reason (wrong code or cancelled, the code not typed in time, the board dropped the link, refused by a board paired with another computer, timed out connecting, not confirmed by the board's USB or Wi-Fi session, another board's `Hello`, revoked) |
| GET | `/admin/v1/firmware` | – | the latest release's boards: `{"version", "boards": [{"board", "name", "chip", "types", "usb", "flash_size", "wifi", "ota", "picture", "version"}], "releaseKey"}` |
| GET | `/admin/v1/firmware/builds` | – | firmware built on this computer, in the data folder's `firmware-builds`: `{"dir", "builds": [{"board", "name", "chip", "types", "file", "size", "sha256", "flash_size", "ota", "wifi", "version", "added"}], "skipped"}`, by board and newest version first; `skipped` says which manifests in the folder were left out and why (a file missing, a size that doesn't match). Read from the folder, never from the network |
| POST | `/admin/v1/firmware/builds` | `multipart/form-data`: `manifest` (the board's `agents-deck-<board>-<version>.manifest.json` as the release script writes it, its `agents_deck` part, or its entry in `firmware-manifest.json`), `full` (`.bin`), `app` (`.app.bin`, when the manifest lists one); at most 66 MB | `200` and the build, kept in `firmware-builds` (one of the same board and version is replaced), as `agentctl firmware add` sends it. `400` unless every check passes: the board is a folder name and the version a semver of at most 23 characters, the manifest's file names are `agents-deck-<board>-<version>.bin` and `.app.bin` and its offset `0x0`, each image has the size and SHA-256 the manifest says, the full image is a full image and the app image an app image (the same reading as `firmware-file`), both for the manifest's chip, the full image for its flash size. `413` over the limit. Names in the folder come only from a manifest that passed these checks |
| DELETE | `/admin/v1/firmware/builds/{board}/{version}` | – | `{"removed": [versions]}`: removes a build made on this computer; `version` `all` removes every build of the board. `404` when there is none; board and version must be names as above |
| GET | `/admin/v1/jobs` | – | `{"jobs": […]}`, newest first (kept in memory, the last 50) |
| GET | `/admin/v1/jobs/{id}` | – | `{"id", "kind": "flash"\|"board-update"\|"app-update"\|"bluetooth-add", "state": "running"\|"done"\|"failed"\|"cancelled", "percent", "stage", "message", "error", "bootHelp", "steps", "done", "total", "file", "source"}` (`source` `local`: a build made on this computer) |
| POST | `/admin/v1/jobs/{id}/cancel` | – | `202` |
| GET | `/admin/v1/events` | – | Server-Sent Events: `job` (a Job, on every change) and `system` (as `GET /admin/v1/system`, every 10 seconds and after a job ends) |
| GET | `/admin/v1/update` | – | the updater's state (`state`, `current`, `version`, `notes`, `message`, `install`) plus `mode`, `checked`, `releaseKey`, `job` |
| POST | `/admin/v1/update/check` | – | reads the latest release now, then downloads it in the background so it is ready |
| POST | `/admin/v1/update/apply` | – | `202` and a job: download if needed, then install; the service and the app restart |
| GET | `/admin/v1/mcp` | – | `{"tokens": [{"name", "scope", "created", "lastUsed", "revoked"}], "url", "agentctl"}`: the MCP tokens (never their secrets or hashes), the HTTP endpoint (`http://127.0.0.1:7340/mcp`) and the agentctl the stdio transport runs |
| POST | `/admin/v1/mcp/tokens` | `{"name", "scope"}` (`scope` `act` when left out) | `{"token", "name", "scope", "url", "commands": {"http", "stdio"}}`: a new token, `adk_` and 64 hex digits, returned this once (only its SHA-256 is kept), with the `claude mcp add` commands for both transports. `400` for a bad name or scope, or an active token of the same name |
| POST | `/admin/v1/mcp/tokens/{name}/{action}` | `revoke` or `remove` | `204`: `revoke` stops the token at once (it stays listed, revoked; its hash is dropped); `remove` forgets it. `404` for no such token |
| GET | `/admin/v1/diagnostics` | – | `{"report", "log", "uptime", "agents", "boardsOnline", "boards", "dataDir", "logFile"}`: the report holds versions, the platform, counts and board models; the log's last lines have home paths, folders, quoted text, agent, device, account, user, host and network names and addresses left out. `dataDir` and `logFile` are for this computer's page, not the report |

**Jobs and events.** Jobs come on `/admin/v1/events`, not on `/v1/events`: that stream also goes
to paired devices on the network and to Deck Link boards, and nothing from the management API does.

**Updates.** The service checks for a new release two minutes after it starts and then once a day,
as `updates` says: `notify` downloads and checks it and waits for Install; `auto` also installs it;
`off` never checks. Board firmware is never updated by itself.

**No release key.** A build without `release.pub` still lists releases and the firmware catalog,
but `flash`, device `update` and `update/apply` answer `409` and say so; nothing from the network is
installed. A firmware file picked on this computer still installs (`devices/{id}/firmware-file`,
`ports/{id}/firmware-file`): it is the owner's choice and never comes from the network. So does
firmware built on this computer (`firmware/builds`, then `update` or `flash` with `"source":
"local"`): the folder is filled only through the management API, so only from this computer; every
install reads the build's files again and checks them against its manifest before anything goes to
the board. No key is generated, kept or skipped for it: a build made here is never treated as a
release.

**Errors** are `{"error": "…"}` in plain words; a flash that can't reach the bootloader also sets
`"bootHelp": true` (hold BOOT, press RST).

## Demo mode

`agentctl serve --demo` serves a fixed, animated fleet (the agents in `fixtures/state.json`
with statuses cycling) on both listeners, so the panel and web UI can be developed without
running real agents. `answer` / `focus` / `interrupt` are accepted and change the demo state.
The management API pretends too: made-up boards, USB ports, a release and a flasher in a folder of
its own, so Settings can be tried without touching a serial port, `config.json` or the network.
Bluetooth pretends as well: boards appear one by one while it looks, adding one walks through the
passkey step, and one weak board fails; `AGENTS_DECK_DEMO_BLUETOOTH=off`, `unauthorized` or `none`
shows Bluetooth unavailable instead.
The browser pictures in `docs/images` are taken from the demo by `node docs/tools/web-shots.mjs`, in
a data folder of its own with the network listener off ([devices.md](devices.md#pictures)).
