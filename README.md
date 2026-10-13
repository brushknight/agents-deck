<p align="center"><img src="docs/images/hero-v2.png" alt="agents deck: a pixel-critter board of Claude Code and Codex agents in the macOS menu bar, with a 4-inch desk panel as a companion" width="100%"></p>

<p align="center">
  <a href="https://github.com/brushknight/agents-deck/actions/workflows/ci.yml"><img src="https://github.com/brushknight/agents-deck/actions/workflows/ci.yml/badge.svg" alt="ci"></a>
  <a href="https://github.com/brushknight/agents-deck/releases/latest"><img src="https://img.shields.io/github/v/release/brushknight/agents-deck?label=release&color=FF4D00&labelColor=1b1b18" alt="latest release"></a>
  <img src="https://img.shields.io/badge/macOS-Apple%20Silicon-7A776F?labelColor=1b1b18" alt="macOS on Apple Silicon">
</p>

# agents deck

**Mission control for your Claude Code and Codex agents**: in your menu bar, on your desk, in the browser and in your terminal.

It's self-contained on your laptop: a small Go daemon tracks every agent (who's working, who needs you, what each one is doing, its context and cost), a deck drops down from the menu bar, and a web dashboard works on your phone too. Answer permission prompts and questions from any of them, or jump straight to the agent's iTerm tab. A 4″ desk panel is an optional companion, with more devices to come.

Claude Code agents run in private tmux sessions started with `agentctl new`. Your **Codex** threads join the same board automatically, read-only, with no wrapper or login.

## Where it shows up

| | |
|---|---|
| <img src="docs/images/menubar-deck-v2.png" alt="the deck dropped down from the menu bar"> | <img src="docs/images/web-board-v2.png" alt="web dashboard"> |
| **Menu bar.** Click the critter and the deck drops down; it turns orange when an agent needs you. | **Browser.** The same board, a panel view, and every agent's card. |
| <img src="docs/images/panel-board-v2.png" alt="desk panel"> | <img src="docs/images/cli.png" alt="agentctl in the terminal"> |
| **Desk panel.** A 4″ touch panel on Wi-Fi, as a companion. | **Terminal.** `agentctl` starts, lists, resumes and restores agents. |

**Small boards** (ESP32 and friends) plug in over USB and get the same agents, prompts and actions through [Deck Link](docs/link/protocol.md); the [firmware kit](firmware/deck-link) has a status LED, an OLED tile and a knob to start from, and [`waveshare-lcd-147`](firmware/devices/waveshare-lcd-147) is a ready-made colour panel.

Each critter shows what its agent is doing: thinking, reading, editing, running commands, compacting, waiting for you, or hungry for review once it's done. It wears a pixel hat picked by its working folder, so a project's agents look alike; tap the critter to pick another. The full tour is in [docs/features.md](docs/features.md).

## Quick start

Requirements: macOS on Apple Silicon, tmux, Claude Code, and iTerm2 (for tab focus). The Codex app is optional.

```bash
curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh -s -- --app
```

It downloads the latest release, checks it against the release's `SHA256SUMS`, installs `agentctl` to `~/.local/bin`, starts the daemon at login and puts the menu bar app in `~/Applications` (drop `--app` to skip it). Then:

```bash
source <(agentctl completion zsh) # tab completion (add to ~/.zshrc)
cd ~/dev/my-project
agentctl new                      # a Claude agent for this folder (ctrl-q detaches)
agentctl web                      # the dashboard (one-time login link)
```

From source: `cd backend && go build -o ~/.local/bin/agentctl ./cmd/agentctl && agentctl install` (Go 1.24+), and `cd clients/macos && flutter build macos --release` for the menu bar app (Flutter 3.44). Want a full board without spending tokens? `agentctl sim start --root ~/dev --slots 12`.

## Commands

```text
agentctl new [dir] [-t title] [-c claude|codex|gemini|shell] [-d]   start an agent and attach (-d: stay detached)
agentctl attach <id|title>          show an agent in this terminal (ctrl-q detaches)
agentctl reopen                     iTerm tabs for every agent no terminal shows (after iTerm quits or crashes)
agentctl hats                       the hats agents wear (one per working folder)
agentctl set hat <agent|folder> <shape> [colour] | auto
agentctl live                       Claude sessions started by hand in other terminals
agentctl add <session>              put one of them on the deck (watched; or tap a free cell)
agentctl restore                    bring back agents lost with the tmux server (crash, reboot), each in its old spot
agentctl set restore ask|auto       keep lost agents for `restore` (default), or bring them back right away
agentctl ls                         the fleet at a glance
agentctl rm <id|title>              stop and forget an agent
agentctl move <id|title> <n>        put an agent at position n (free spots are fine)
agentctl sessions [words]           look up any Claude session by id, title, folder, branch or last prompt
agentctl resume <id-prefix>         continue a Claude session as an agent
agentctl web                        open the dashboard
agentctl pair <name> [--bundle F]   pair a device (the panel): its own certificate, scope view|act|admin
agentctl devices [revoke <name>]    paired devices and serial boards; cut one off
agentctl set term iterm|tmux|window how "focus" shows an agent
agentctl set mouse tmux|native      who owns the mouse in agent terminals
agentctl sim start|stop             simulated agents for demos
```

## How it works

```mermaid
flowchart LR
  subgraph tmux["tmux -L agentsterm"]
    C1["claude (agent)"]:::a
    C2["claude (agent)"]:::a
  end
  C1 & C2 -- "hooks (per-agent --settings)" --> H["agentctl hook"]
  H -- unix socket --> D["agentctl serve"]
  T["~/.claude/projects/*.jsonl"] -- "tail: tokens, context, model, title" --> D
  D -- "send-keys: answers, Esc" --> tmux
  D -- "/v1 + SSE · 127.0.0.1" --> W["web dashboard"]
  D -- "/v1 + SSE · TLS 1.3, device certificate" --> P["desk panel (Flutter)"]
  D -- "iTerm API + StealFocus escape" --> I["iTerm tab"]
  X["~/.codex: state db + rollouts"] -- "read-only: threads, status, tokens" --> D
  classDef a fill:#FF4D00,color:#0A0A0A,stroke:#FF4D00
```


Each agent gets its own hook settings, so your global `~/.claude/settings.json` is never touched. Hooks report prompts, tool calls, permission requests, compaction and stops; Claude's transcripts give tokens, context, model and title; dialogs without a hook are read off the screen. Answers are typed into the agent's tmux session. The web UI, the menu bar app and the panel all speak one small JSON API ([docs/protocol.md](docs/protocol.md)).

## Security & privacy

- **Nothing leaves your machine:** no telemetry, no update checks. The only Go dependency is Google's protobuf module.
- **The web dashboard is loopback-only,** behind a one-time login link, a strict CSP and host and origin checks.
- **The menu bar app** uses the daemon's unix socket: nothing crosses the network.
- **The desk panel** connects over TLS 1.3 with its own client certificate, signed by the deck's local CA (its key is in the macOS Keychain). Each device has a scope (view, act or admin), shows up in `agentctl devices`, can be revoked on its own, and every action it takes lands in an audit log.
- **Serial boards** have to be registered (`agentctl link add`) and approved once (`agentctl devices approve`), start with view-only access, and are logged and revocable like the panel.
- **Codex files are only read.** App control is a single Apple event (asking iTerm for its API cookie): no synthetic keystrokes, no screen reading.
- **Secrets** live in `~/.local/share/agents-terminal/` (`0700` / `0600`).

## Credits

The per-project pixel hats were inspired by [“9 NEW Claude Mods that can truly change how you work”](https://www.youtube.com/watch?v=lDrAZ1wAyVs) by Jay E ([RoboNuggets](https://www.youtube.com/@RoboNuggets)).

## Docs

[Features](docs/features.md) · [Protocol](docs/protocol.md) · [Deck Link](docs/link/protocol.md) · [New device](docs/link/new-device.md) · [Releasing](docs/releasing.md) · [Roadmap](docs/roadmap.md)

| path | |
|---|---|
| `backend/` | Go daemon and CLI (`agentctl`), with the web UI embedded. `go test -race ./...` |
| `clients/flutter/agents_ui` | The deck UI as a Flutter package, shared by the desk panel and the menu bar app. |
| `clients/macos` | The menu bar app (Flutter + a small Swift shell). |
| `proto/` | `link.proto`: the Deck Link contract for serial boards. |
| `firmware/deck-link` | The C firmware kit for Deck Link boards (ESP32), with examples. |
| `firmware/devices` | Firmware for specific boards, built on the kit (`waveshare-lcd-147`: a colour panel). |
| `docs/fixtures/state.json` | The sample fleet behind demo mode, the web fixture view and the tests. |
