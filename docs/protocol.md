# agents deck protocol v1

One Go daemon (`agentctl serve`) owns all agent state. Clients:

| client | transport | auth |
|---|---|---|
| panel (Flutter, 720×720) | HTTPS on the LAN, `:7341`, self-signed cert **pinned by SHA-256 fingerprint** | `Authorization: Bearer <device token>` |
| web UI (served by the daemon) | HTTP on `127.0.0.1:7340` only | HttpOnly SameSite=Strict cookie set by a one-time login URL from `agentctl web` |
| Claude Code hooks, `agentctl` CLI | unix socket `~/.local/share/agents-terminal/agentd.sock` (0600) | filesystem permissions |

Both HTTP listeners serve the same `/v1` API. Nothing listens on the LAN without TLS + token.
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
| POST | `/v1/restore` | – | `200` `{"results": [{"id", "title", "action": "resumed\|relaunched\|removed\|failed", "error"}]}` — brings back every agent with `lost: true`: Claude agents on their own conversation, other tools fresh in their folder, each in its old slot |
| POST | `/v1/order` | `{"ids": ["k3f9a2", "h0m3l4", …]}` | `204` — compacts: puts those agents in slots 0..n-1 in that order (others keep their order after them) |
| POST | `/v1/agents/{id}/resume` | – | `204` — restarts an exited Claude agent (`resumable: true`) on its previous conversation, same slot |

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
```

Status meanings (what the tiles show):

| status | meaning | tile |
|---|---|---|
| `starting` | launched, Claude not ready yet | dim critter, "starting" |
| `running` | working on a turn | ink critter walking, `activity` + context % |
| `waiting` | blocked on the user (`waiting` set) | **full accent tile**, "?" |
| `idle` | turn finished, waiting for a new prompt | dim critter asleep |
| `error` | API error or crashed | accent outline, X eyes, "!" |
| `exited` | process gone; stays 10 min (resumable), then moves to the CLI history (`agentctl resume`) | dim, hollow |

## Pairing the panel

`agentctl pair` prints the values the panel needs:

```json
{ "url": "https://my-mac.local:7341", "token": "…64 hex…", "fingerprint": "sha256:AB:CD:…" }
```

The panel stores them in its own config and verifies the server certificate by fingerprint
only (no CA). `agentctl pair --rotate` issues a new token and invalidates the old one.

## Demo mode

`agentctl serve --demo` serves a fixed, animated fleet (the agents in `fixtures/state.json`
with statuses cycling) on both listeners, so the panel and web UI can be developed without
running real agents. `answer` / `focus` / `interrupt` are accepted and change the demo state.
