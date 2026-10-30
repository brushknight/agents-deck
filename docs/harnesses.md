# Adding a harness

A harness is an app that runs coding agents itself: Orca, herdr, cmux. The deck doesn't replace it; it
reads the harness, shows its agents next to agentctl's own, and sends what you do on the deck (answer,
interrupt, open, hide) back through the harness. This page says how to add one.

## What the deck needs from a harness

Before writing code, find out what the harness lets other programs see and do. The deck needs:

| need | what for | must have? |
|---|---|---|
| a list of its agents, each with a stable id | one tile per agent; the tile goes when the agent does | yes |
| each agent's state: working, waiting for you, idle or finished | the tile's status | yes |
| the Claude session behind an agent (its session id, or its process) | tokens, context, cost, account, subagents, from the transcript | for Claude agents |
| the agent's screen as text | the question and its options when it waits on you | for answering |
| typing text and pressing keys in the agent's terminal | answers, and Esc to interrupt | for answering |
| showing the agent's terminal | **open in …** | nice to have |

How the three that exist get these:

| | agents and state | Claude session | screen, typing, focus |
|---|---|---|---|
| Orca | `orca worktree ps`, `orca terminal list` (CLI) | the one Claude session in the worktree's folder | `orca terminal read`, `send`, `switch` |
| herdr | `session.snapshot` on its socket | herdr's hook, else the pane's process | `pane.read`, `pane.send_text`, `pane.send_keys`, `agent.focus` |
| cmux | its Claude hooks' file, `~/.cmuxterm/claude-hook-sessions.json` | in that file | `surface.read_text`, `send_text`, `send_key`, `focus` on its socket |

Prefer what the harness documents (a CLI, a socket API, a state file) over reading its window or its
private storage. If it only offers part of the list, a read-only integration (tiles with status, answered
in the harness) is still worth having; say so in the docs.

## The pieces

Look at cmux for a small, complete example: `backend/internal/cmux` and
`backend/internal/daemon/cmuxwatch.go`.

### 1. A client package: `backend/internal/<name>/`

It speaks the harness's interface and nothing else: list agents, read a screen, send text, press keys,
focus. Use the Go standard library only (see `AGENTS.md`). Keep the calls short (a few seconds' timeout)
and turn the harness's errors into ones a person can act on; cmux's `ErrDenied` says which setting to
change.

Test it against a fake: a socket in a temp folder (`herdr_test.go`, `cmux_test.go`) or a fake CLI on
`PATH`. Test data uses made-up paths and names.

### 2. A watcher: `backend/internal/daemon/<name>watch.go`

- `Watch<Name>()` polls every 2 seconds. It returns at once when `config.json` has `"<name>": false`:
  add the switch to `Config` in `daemon.go` and start the watcher in `cmd/agentctl/main.go` next to the
  others.
- `sync<Name>()` puts the harness's agents on the board and takes off the ones that are gone. Each agent
  is a `store.Entry` with `External: true` and `A.App` set to the harness's name (11 characters at most:
  Deck Link carries it in 12 bytes). Its id is a letter plus the start of a SHA-256 of the agent's stable
  key; the letters in use are `h` herdr, `l` sessions added by hand, `m` cmux, `o` Orca.
- Map the harness's state onto the agent:
  - working: `model.Running`, with `A.Activity` set to the tool call from the transcript;
  - waiting for you: `model.Waiting`, with `A.Waiting` set to the prompt. `screenPrompt` (in
    `harness.go`) builds it from a Claude Code dialog on the screen: the question and numbered options.
    For a question form (several questions, tabs on screen) it shows the tab on screen, and "next" on
    a pick-any tab is typed as Tab. Build it once per question, not on every poll, so its id stays the
    same while you answer it;
  - idle: `model.Idle`. Set `A.Unseen` when a turn finished while the deck watched (the hungry tile).
- For a Claude agent, set `SessionID` and `Transcript`, then call `claude.Tail` (model, context, tokens,
  cost, title) and `d.subagents`. `syncLive` then leaves the session alone, so it doesn't show up twice.
- Keep what the actions need (a socket path, a terminal id) in new `store.Entry` fields tagged
  `json:"-"`: mirrored agents are never saved.

### 3. The actions: `harnessOf` in `backend/internal/daemon/harness.go`

Add a case for the harness's name with four functions:

| function | does |
|---|---|
| `send(e, key)` | types an answer's key ("1", "2" …) into the agent's terminal. `answerInApp` has already checked the prompt and the key. |
| `interrupt(e)` | presses Esc |
| `focus(e)` | shows the agent's terminal in the harness and brings the harness forward; clears `A.Unseen` |
| `hide(e)` | takes the agent off the deck until its state changes; it keeps running in the harness |

The web dashboard, the menu bar app, the desk panel and the boards need no changes: they show
**open in &lt;name&gt;** for any agent with `app` set.

### 4. Contract and docs

- `proto/agentsdeck/link/v1/link.proto`: add the name to the comment on `Agent.app`, then regenerate
  both sides (`go generate ./internal/link` and `firmware/deck-link/tools/generate.sh`). Only the comment
  changes.
- `docs/protocol.md`: add the name to the `app` field.
- `README.md`: a row in the harness table.
- `docs/features.md`: a section under "Agents in other harnesses": what shows up, where its state comes
  from, anything to set up in the harness, which calls the daemon makes, and the `config.json` switch.

## Testing

Unit tests with a fake client cover the mapping (`cmuxwatch_test.go`): a working agent, a question with
its options, an answer that types the key, a stale or wrong answer refused, interrupt, hide and come back,
and the tile leaving when the agent does.

Then try it for real:

1. Install the harness and start a Claude session in it.
2. Run your build of `agentctl` as the daemon and open the web dashboard.
3. Check, from the deck: the tile appears and shows working; a question (ask Claude to use
   AskUserQuestion) shows its options and your answer reaches the agent; **interrupt** stops a long
   turn and the tile goes idle; **open in …** shows the right terminal; **hide** keeps it off until it
   works again; closing the session takes the tile away.

## Rules

- Never weaken the security model in `docs/protocol.md`, and don't change the harness's own settings from
  the daemon. If the harness needs a setting turned on (cmux's socket access), say in the docs what it
  allows, and leave the choice to the person.
- No host names, user names or personal paths in code, tests or docs.
- Keep `docs/` in step with the code in the same PR.
