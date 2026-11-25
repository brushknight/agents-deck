# MCP: let one agent run the others

agents deck is also an [MCP](https://modelcontextprotocol.io) server. Add it to a capable agent
(Claude Code, for example) and that agent can see every agent on your deck, give them tasks, answer
their questions, stop them, queue the next task for one that is busy, and start new ones in a project
folder with a first prompt. You talk to
one agent; it orchestrates the rest.

It works only on this computer: over HTTP on the dashboard's address (`127.0.0.1`), or over stdio
through `agentctl`. Both need a token.

## Make a token

In **Settings, Agents, MCP**, click **New token**, give it a name and pick what it may do:

| scope | in Settings | may |
|---|---|---|
| `view` | look only | `list_agents`, `get_agent`, `list_queue`, `list_projects` |
| `act` | can answer (the default) | also `send_prompt` (every mode), `cancel_queued`, `answer`, `interrupt` |
| `admin` | can manage | also `start_agent`, `list_sessions`, `add_session`, `create_project`, `update_project`, `remove_project` |

The token (`adk_` and 64 hex digits) is shown once, with the commands below filled in. The deck keeps
only its SHA-256, so a lost token can't be shown again: revoke it and make another. The list shows
when each token was last used; **Revoke** stops one at once.

From a terminal:

```sh
agentctl mcp token create claude-main              # scope act
agentctl mcp token create boss --scope admin       # can also start agents
agentctl mcp token list
agentctl mcp token revoke claude-main
```

## Add it to Claude Code

Over HTTP (the service must be running; `7340` is the dashboard's port):

```sh
claude mcp add --transport http agents-deck http://127.0.0.1:7340/mcp \
  --header "Authorization: Bearer adk_…"
```

Over stdio: Claude Code starts `agentctl mcp`, which passes each message to the running service
through its unix socket, with the token from `AGENTSDECK_MCP_TOKEN` (or `--token`):

```sh
claude mcp add agents-deck -e AGENTSDECK_MCP_TOKEN=adk_… -- agentctl mcp
```

Use the full path of `agentctl` when it isn't on Claude Code's `PATH` (Settings fills it in). Both
transports check the token the same way: the same lookup, scopes, audit log and revocation. Without a
valid token the first call (`initialize`) fails and says how to make one.

## Tools

Every tool call runs the deck's `/v1` API as the token (`mcp:<name>`), through the same scope check
and audit log as a paired device. A refused or failed call comes back as a tool result with `isError`
and a plain reason, for example "the agent is working on a turn: wait until it is idle, or send with
interrupt to stop it first".

| tool | scope | arguments | what it does |
|---|---|---|---|
| `list_agents` | view | `project` (a project's name, any case, or its id: only its agents) | every agent, compact: `id`, `title`, `folder`, `tool`, `runsIn` (`deck`, `herdr`, `cmux`, `orca`, or read-only elsewhere), `status`, how long in it, what it is doing, its last prompt and last answer, the question it waits on (`promptId`, option keys and labels), `contextPct`, working `subagents`, `queued` (prompts waiting for its turn to end), `acceptsPrompts`, `project` (the name of the project its folder is in; omitted for none) |
| `get_agent` | view | `agent_id` | one agent in full: the whole question, subagents, `queued`, model, branch, folder, tokens, cost, turns |
| `send_prompt` | act | `agent_id`, `text`, `mode` (`queue`, `type`, `now` or `interrupt`; default `queue`), `queue_position` (`back` or `front`; default `back`), `interrupt` (the same as `mode: interrupt`, for older clients) | types the text into the agent's session and presses Enter (`POST /v1/agents/{id}/prompt`), now or once its turn ends: see [Queue a prompt](#queue-a-prompt-for-a-busy-agent). While the computer is busy and the CPU guard holds work that can wait, `mode: queue` queues it even for an idle agent. Answers `sent: …`, or, when queued, `{"status": "queued", "queue_id", "position", …}` |
| `list_queue` | view | `agent_id` | the prompts queued for the agent, next first: `queue_id`, `position`, `text`, `by` (who queued it), `queuedAt` |
| `cancel_queued` | act | `agent_id`, `queue_id` | takes a queued prompt back before it is typed in |
| `answer` | act | `agent_id`, `prompt_id`, `key` | picks an option of the question the agent waits on |
| `interrupt` | act | `agent_id` | presses Esc: stops the current turn |
| `start_agent` | admin | `project` (name or id) or `folder`, or both; `tool` (`claude`, `codex` or `shell`), `title`, `account`, `prompt` | starts an agent on the deck: in a project, in its first folder (or `folder`, one of its folders) with its tool and account unless you pick others; or in any folder; with `prompt`, waits (up to a minute) until it is ready and sends it. If the agent asks something first (trusting a new folder), the result shows the question: answer it, then `send_prompt` |
| `list_sessions` | admin | `project` (a project's name, any case, or its id: only sessions working in its folders) | Claude sessions started elsewhere on this computer (as `GET /v1/live`) |
| `add_session` | admin | `session_id` | puts one of them on the deck, watched (read-only) |
| `list_projects` | view | – | the projects: `id`, `name`, `folders` (full paths), `tool` and `account` their agents start with, and `agents` (how many on the deck work in them now) |
| `create_project` | admin | `name`, `folders` (full paths or `~/…`, at most 20), `tool`, `account` | adds a project, with the same checks as Settings: the folders exist, and a folder belongs to one project |
| `update_project` | admin | `project` (name or id), `name`, `add_folders`, `remove_folders`, `tool`, `account` | renames it, adds or removes folders, changes what its agents start with; what is left out stays. The id stays the same |
| `remove_project` | admin | `project` (name or id) | takes it off the list; its folders and agents stay, with no project |

## What can take a prompt

`send_prompt` types into a terminal, so it reaches only terminals the deck can type into:

- **Agents the deck started** (`agentctl new`, the deck's **+**, `start_agent`): they run in the
  deck's own tmux. The text goes in as one paste (bracketed when the program asks for it, as Claude
  Code and Codex do), so newlines stay part of the prompt, then Enter.
- **Agents in herdr, cmux or Orca**: through that app's own interface, as answers already do.
- **Not sessions you started by hand in another terminal**, or Codex app threads. Nothing can type
  into another app's terminal, so these are read-only: you see them and can answer nothing. The tool
  says so; start agents with `start_agent` to give them tasks.

`send_prompt` has four modes:

- **`queue`** (the default): at once when the agent is idle, else when its turn ends, in order. Send
  five and they go in one after another, as if you typed five prompts into its chat. See below.
- **`type`**: typed into the agent's chat now, even while it works, so the tool's own queue holds
  it, as when you type ahead yourself. Claude Code reads it at its next step, during the current
  turn, rather than after it. The deck doesn't list it and it can't be cancelled; refused while the
  agent asks a question, since the text would answer it.
- **`now`**: refused (`409`) while the agent is working on a turn, waiting on a question
  (use `answer`), still starting, or exited.
- **`interrupt`**: a working or asking agent gets Esc first, then the prompt. `interrupt: true` does
  the same, for clients written before `mode`.

Agents whose status the deck can't see (Codex or a shell started here) take a prompt at any time, in
every mode. Text: up to 4000 bytes, newlines and tabs but no other control characters (an escape
sequence could press keys of its own).

## Queue a prompt for a busy agent

With `mode: queue` (or no mode) you don't have to poll until an agent is idle, or stop it. The deck keeps the
prompt and types it in the moment the agent's turn ends (its status goes `idle`). If the agent is idle
already and nothing is queued for it, the prompt goes in at once and the answer is `sent`.

```json
{"name": "send_prompt", "arguments": {"agent_id": "k3f9a2", "text": "now run the integration tests", "mode": "queue"}}
```

```json
{"status": "queued", "agent_id": "k3f9a2", "queue_id": "q7m2x0d", "position": 1,
 "note": "typed in when k3f9a2's turn ends (status idle); list_queue shows it, cancel_queued takes it back"}
```

- **One per turn, in order.** Queued prompts go in one at a time: each waits until the turn the one
  before it started has ended. `queue_position: front` puts a prompt before the ones queued earlier;
  `back` (the default) after them.
- **Questions are never answered for you.** While the agent waits on a question or a permission, its
  queue waits too, and the question stays on the deck for you (or the orchestrator, with `answer`).
  Once it is answered and that turn ends, the queue goes on.
- **See and cancel.** `list_agents` and `get_agent` show `queued: n`; `list_queue` lists the prompts
  with their `queue_id`; `cancel_queued` takes one back. The deck shows the count on the agent's tile
  and lists the prompts, each with **Cancel**, on its page.
- **Kept across restarts.** The queue is `prompt-queue.json` in the data folder (0600). At most 20
  prompts per agent. An agent that has exited takes no new prompts; one that leaves the deck has its
  queue dropped after 10 minutes.
- **Every tool.** The deck keeps the queue itself, so Claude Code, Codex and shells behave the same:
  each queued prompt goes in through the same path as `now`. An agent whose status the deck can't
  see gets it at once.

```json
{"name": "list_queue", "arguments": {"agent_id": "k3f9a2"}}
{"name": "cancel_queued", "arguments": {"agent_id": "k3f9a2", "queue_id": "q7m2x0d"}}
```

## Security

- **Local only.** `POST /mcp` is served on the loopback dashboard (`127.0.0.1`) and the unix socket,
  never on the network listener. The dashboard's `Host` check applies, and a request from a web page
  (an `Origin` other than the dashboard's) is refused, so a site in your browser can't reach it.
- **A token on every transport.** The stdio transport talks to the service over its unix socket, but
  still sends the token, and the service checks it as on HTTP. The dashboard's login cookie is not a
  token.
- **Scopes.** A token may do what its scope allows and nothing more; `start_agent` needs `admin`.
  Queueing and cancelling need `act`, as sending does; `list_queue` needs `view`.
- **Stored as a hash.** `mcp-tokens.json` in the data folder (0600) holds names, scopes and SHA-256
  hashes, never tokens. A revoked token's hash is dropped.
- **Audited.** Every call that changes something is in `audit.log` under `mcp:<name>`, for example
  `{"device": "mcp:claude-main", "action": "POST /v1/agents/k3f9a2/prompt", "status": 204}`, and
  `"action": "start agent"` for `start_agent`. A queued prompt has its id in `detail`
  (`"status": 202, "detail": "queued q7m2x0d at the back"`; a cancel `"cancelled q7m2x0d"`), and the
  deck logs typing it in under the same name: `{"device": "mcp:claude-main", "action": "deliver queued
  prompt", "status": 204, "detail": "q7m2x0d to k3f9a2"}`. The prompt's text is not logged.
- **What an orchestrator can do.** An `act` token can type anything into your agents, and they act on
  it with the permissions you gave them. Give `admin` only to an agent you would let start new
  agents in any folder.

## Protocol

JSON-RPC 2.0 over Streamable HTTP, protocol version `2025-06-18` (`2025-03-26` and `2024-11-05` are
accepted and echoed). `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`;
answers are `application/json` (there is no server-sent stream, so `GET /mcp` is `405`). Batches are
answered as batches; a notification gets `202` and no body; an unknown method `-32601`; a missing or
wrong token HTTP `401` with JSON-RPC error `-32001`. The server is `agents-deck` with the service's
version. The `/v1` and admin calls behind it are in [protocol.md](protocol.md).
