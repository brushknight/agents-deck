# agents deck protocol v1

One Go daemon (`agentctl serve`) owns all agent state. Clients:

| client | transport | auth |
|---|---|---|
| desk panel (Flutter, 720×720), later a phone app | HTTPS on the network, `:7341`, **TLS 1.3 only** | a **client certificate per device**, signed by the deck's CA; scope view, act or admin |
| web UI (served by the daemon) | HTTP on `127.0.0.1:7340` only | HttpOnly SameSite=Strict cookie set by a one-time login URL from `agentctl web` |
| menu bar app, Claude Code hooks, `agentctl` CLI | unix socket `~/.local/share/agents-terminal/agentd.sock` (0600); on Windows `%LOCALAPPDATA%\agents-deck\agentd.sock` (Windows 10 1803 and later have unix sockets) | filesystem permissions: your user (on Windows, the folder under your profile is private to you) |
| ESP32 boards (Deck Link) | USB serial, see [link/protocol.md](link/protocol.md) | approved once per board (at once when plugged into a registered USB port, unless turned off); scope view, act or admin |
| ESP32 boards on Wi-Fi | `GET /v1/link` with `Upgrade: deck-link` on the network listener (`:7341`, TLS 1.3): the connection becomes a Deck Link stream ([pairing and Wi-Fi](link/protocol.md#pairing-and-wi-fi)) | the board's client certificate, issued over USB to an approved board; the same device, scope and audit log as on USB |

Every listener serves the same `/v1` API. Nothing listens on the network without TLS and a device certificate.
The management API (`/admin/v1`, [below](#management-api-admin-v1)) is served only on the unix socket and the
loopback dashboard, never on the network.
The loopback listener rejects any `Host` other than `127.0.0.1:7340`/`localhost:7340` and any
mutating request whose `Origin` is not that host.

## Endpoints

| method | path | body | result |
|---|---|---|---|
| GET | `/v1/state` | – | `State` |
| GET | `/v1/events` | – | Server-Sent Events, see below |
| POST | `/v1/agents/{id}/answer` | `{"promptId": "...", "key": "1"}` | `204`; `409` if `promptId` is no longer the agent's current prompt; `400` if `key` is not one of its options |
| POST | `/v1/agents/{id}/focus` | – | `204` — brings the agent's terminal to the front on the Mac (iTerm tab / overlay) |
| POST | `/v1/agents/{id}/interrupt` | – | `204` — sends Esc to the agent (stops the current turn) |
| POST | `/v1/agents/{id}/dismiss` | – | `204` — stops the agent if still running and removes it from the fleet |
| POST | `/v1/agents/{id}/move` | `{"slot": 9}` | `204` — puts the agent at board position 0..255; free positions are fine (the board can have gaps), and an agent already there swaps into the mover's old position. Used by drag-and-drop on the web and the panel |
| GET | `/v1/live` | – | `200` `{"sessions": [{"sessionId", "title", "cwd", "folder", "started", "onBoard", "background"}]}` — active Claude sessions on this machine that agentctl didn't start: in terminals, or in the background (`claude --bg`, the Claude desktop app) |
| POST | `/v1/agents/{id}/hat` | `{"shape": "cap", "color": "teal"}` (`color` optional), `{"shape": "none"}`, or `{"auto": true}` | `204` — picks the hat of the agent's working folder (every agent working there wears it), or back to the automatic one; stored in `config.json` `"hats"` |
| GET | `/v1/hats` | – | `200` `{"shapes": […], "colors": […], "overrides": {"<folder>": {"shape", "color"}}}` |
| POST | `/v1/hats` | `{"folder": "/Users/sam/dev/app", "shape", "color"}` or `{"folder", "auto": true}` | `204` — the same, by folder |
| POST | `/v1/live/{session}/add` | `{"slot": 9}` (optional) | `204` — puts that session on the deck, watched (`external: true`, `tool: "claude"`): status, context, cost and subagents from its transcript; answer, interrupt and resume return an error; dismiss takes it off the deck. It stays added: it comes back whenever it runs |
| POST | `/v1/restore` | – | `200` `{"results": [{"id", "title", "action": "resumed\|relaunched\|removed\|failed", "error"}]}` — brings back every agent with `lost: true`: Claude agents on their own conversation, other tools fresh in their folder, each in its old slot |
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
  "subagents": [                  // omitted when none: subagents still working, oldest first
    { "id": "a1f3c9", "title": "map the payment retry paths", "type": "Explore", "tool": "Grep" }
  ],
  "startedAt": "2026-10-08T00:13:00Z",
  "updatedAt": "2026-10-08T00:51:02Z"
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

Free text ("type something") stays at the terminal. An answer typed at the terminal moves the deck's
prompt along with it (an older step's answer gets `409`), and the prompt clears once Claude has the
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
| `view` | `GET /v1/state`, `/v1/events`, `/v1/hats`, `/v1/accounts` |
| `act` | also answer, focus, interrupt, move, order, hats |
| `admin` | also dismiss, resume, hand over to another account, restore, `GET /v1/live`, add a live session |

A request beyond the device's scope gets `403` with the command that would raise it.

**Boards on USB.** The daemon opens only the serial ports you registered (`agentctl link add`). A new
board that says `Hello` on one of them is approved at once with scope `act`: plugging a board into
this Mac, on a port you registered, is treated as your consent. It is audited (`"action": "approve",
"detail": "approved automatically over USB on /dev/cu.usbmodem…, scope act"`). `admin` is only ever
given by hand (`agentctl devices scope`). It never happens over Wi-Fi, on the network listener or for
`agentctl link sim`, and a revoked board stays revoked until `agentctl devices approve`. Turn it off
with `agentctl devices auto-approve usb off` (new boards then wait for `agentctl devices approve`), or
give new boards `view` with `--scope view`; the setting lives next to `devices.json`
(`devices-settings.json`) and `agentctl devices` shows it.

**Boards on Wi-Fi.** A Deck Link board is paired over USB serial: once it is approved, it sends a
certificate request for a key it made on the chip, and the deck signs it and records it on the board's
own entry (`CertRequest`/`CertIssued`, [link/protocol.md](link/protocol.md#pairing-and-wi-fi)). Over
Wi-Fi it connects to this listener with that certificate and opens `GET /v1/link` (`Upgrade:
deck-link`), a transport rather than an API call: the connection carries Deck Link frames, its
commands go through the same scope check and audit log under the board's name, its `Hello` must carry
the board id the certificate was issued to, and revoking the board ends the session and fails its
next handshake (the certificate is dropped for good; approved again, the board pairs again over USB).
Wi-Fi passwords never go through `/v1`: `agentctl link wifi` hands them to the daemon on the unix
socket (`POST /local/link/wifi`), which passes them to a board connected over USB right now and keeps
nothing. The one exception is the default network (`agentctl link wifi --default --ssid NAME`, `POST
/local/link/wifi-default`): its password is kept in the macOS Keychain (`dev.agents-deck`, item
`wifi-default`; a 0600 file under `keys/` elsewhere), and the deck sends it, over USB only, to every
approved board that is paired and lacks it, once per connection, unless the board was given its own
network. It is never logged or audited (`"action": "wifi default set"` without the network or password).

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
each board is connected right now (USB or Wi-Fi);
`agentctl devices revoke|approve|remove <name>` and `agentctl devices scope <name> <scope>` manage them.

**Audit.** Every request that changes something is appended to `audit.log` as one JSON line:
`{"time", "device", "action": "POST /v1/agents/k3f9a2/answer", "status": 204}`; firmware downloads
(`GET /v1/firmware/…`) are recorded too, with the byte range in `detail`; the web dashboard is
`web`, the unix socket `local`.

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
| PATCH | `/admin/v1/settings` | any of those keys; `"wifi": {"ssid", "password", "send"}` or `{"forget": true}` | `{"settings", "message", "restartNeeded", "sent"}`. The Wi-Fi password goes to the OS secret store and to boards on USB; it is never returned, logged or audited. Start at login registers or removes the service (and the Linux tray app's autostart entry) after answering, since that restarts or stops the service |
| POST | `/admin/v1/restart` | – | `202`: restarts the service through what starts it at login |
| POST | `/admin/v1/cli` | `{"installed": true\|false}` | adds or removes `~/.local/bin/agentctl` (a link to this agentctl; never a file it didn't make) |
| POST | `/admin/v1/open-data-folder` | – | `204`: shows the data folder in the file manager on this computer |
| GET | `/admin/v1/agents` | – | `{"tools": [{"id", "name", "found", "on", "agents", "state", "detail", "note"}], "accounts": [{"name", "color", "default", "agents", "costUsd"}], "colors"}` |
| POST | `/admin/v1/accounts/{name}/color` | `{"color"}` | `204` |
| GET | `/admin/v1/devices` | – | `{"devices": [{"id", "name", "kind", "scope", "state", "online", "connection", "port", "type", "board", "model", "firmware", "wifi", "update": {"version", "how", "ready", "note"}, "job"}], "latest", "releaseKey", "ota"}` |
| POST | `/admin/v1/devices/{id}/{action}` | `approve`, `revoke`, `remove`; `scope` `{"scope"}`; `rename` `{"name"}`; `wifi` `{"ssid", "password"}`, `{"forget": true}` or `{"useDefault": true}`; `update` `{"version"}` | `204`, or for `wifi` `{"sent", "message"}`, for `update` `202` and a job. `rename` changes the name Settings and `agentctl` show; the id stays. `update` flashes a board on USB through its registered port and offers a board on Wi-Fi the update over the air |
| POST | `/admin/v1/pair` | `{"name", "scope"}` | a one-time pairing code, as `agentctl pair` gives |
| GET | `/admin/v1/ports` | – | `{"ports": [{"id", "path", "vid", "pid", "likely", "state", "registered", "device", "job"}]}`, states `new`, `registered`, `connected`, `busy`, `flashing`, `unplugged`, `ignored`, `other`. Listed from the system's USB ids: **no port is opened to list it** |
| POST | `/admin/v1/ports/{id}/{action}` | `register`; `identify`; `flash` `{"board", "version"}`; `forget` | `register` is "yes, it's mine" (`config.json` `link.ports`). `identify` and `flash` answer `403` until the port is registered. `identify` answers from the board's Deck Link session when one is live, else borrows the port from the Deck Link manager and asks the ROM bootloader: `{"source", "chip", "flashSize", "candidates"}`. `flash` downloads and checks the image, borrows the port, writes it at the manifest's offset and hands the port back so the board pairs: `202` and a job; one at a time per port. `forget` stops using a registered port, or marks a new one "not mine" |
| GET | `/admin/v1/firmware` | – | the latest release's boards: `{"version", "boards": [{"board", "name", "chip", "types", "usb", "flash_size", "wifi", "ota", "picture", "version"}], "releaseKey"}` |
| GET | `/admin/v1/jobs` | – | `{"jobs": […]}`, newest first (kept in memory, the last 50) |
| GET | `/admin/v1/jobs/{id}` | – | `{"id", "kind": "flash"\|"board-update"\|"app-update", "state": "running"\|"done"\|"failed"\|"cancelled", "percent", "stage", "message", "error", "bootHelp", "steps", "done", "total"}` |
| POST | `/admin/v1/jobs/{id}/cancel` | – | `202` |
| GET | `/admin/v1/events` | – | Server-Sent Events: `job` (a Job, on every change) and `system` (as `GET /admin/v1/system`, every 10 seconds and after a job ends) |
| GET | `/admin/v1/update` | – | the updater's state (`state`, `current`, `version`, `notes`, `message`, `install`) plus `mode`, `checked`, `releaseKey`, `job` |
| POST | `/admin/v1/update/check` | – | reads the latest release now, then downloads it in the background so it is ready |
| POST | `/admin/v1/update/apply` | – | `202` and a job: download if needed, then install; the service and the app restart |
| GET | `/admin/v1/diagnostics` | – | `{"report", "log", "uptime", "agents", "boardsOnline", "boards", "dataDir", "logFile"}`: the report holds versions, the platform, counts and board models; the log's last lines have home paths, folders, quoted text, agent, device, account, user, host and network names and addresses left out. `dataDir` and `logFile` are for this computer's page, not the report |

**Jobs and events.** Jobs come on `/admin/v1/events`, not on `/v1/events`: that stream also goes
to paired devices on the network and to Deck Link boards, and nothing from the management API does.

**Updates.** The service checks for a new release two minutes after it starts and then once a day,
as `updates` says: `notify` downloads and checks it and waits for Install; `auto` also installs it;
`off` never checks. Board firmware is never updated by itself.

**No release key.** A build without `release.pub` still lists releases and the firmware catalog,
but `flash`, device `update` and `update/apply` answer `409` and say so; nothing from the network is
installed.

**Errors** are `{"error": "…"}` in plain words; a flash that can't reach the bootloader also sets
`"bootHelp": true` (hold BOOT, press RST).

## Demo mode

`agentctl serve --demo` serves a fixed, animated fleet (the agents in `fixtures/state.json`
with statuses cycling) on both listeners, so the panel and web UI can be developed without
running real agents. `answer` / `focus` / `interrupt` are accepted and change the demo state.
The management API pretends too: made-up boards, USB ports, a release and a flasher in a folder of
its own, so Settings can be tried without touching a serial port, `config.json` or the network.
