# MCP: let one agent run the others

agents deck is also an [MCP](https://modelcontextprotocol.io) server. Add it to a capable agent
(Claude Code, for example) and that agent can see every agent on your deck, give them tasks, answer
their questions, stop them, and start new ones in a project folder with a first prompt. You talk to
one agent; it orchestrates the rest.

It works only on this computer: over HTTP on the dashboard's address (`127.0.0.1`), or over stdio
through `agentctl`. Both need a token.

## Make a token

In **Settings, Agents, MCP**, click **New token**, give it a name and pick what it may do:

| scope | in Settings | may |
|---|---|---|
| `view` | look only | `list_agents`, `get_agent` |
| `act` | can answer (the default) | also `send_prompt`, `answer`, `interrupt` |
| `admin` | can manage | also `start_agent`, `list_sessions`, `add_session` |

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
| `list_agents` | view | – | every agent, compact: `id`, `title`, `folder`, `tool`, `runsIn` (`deck`, `herdr`, `cmux`, `orca`, or read-only elsewhere), `status`, how long in it, what it is doing, its last prompt, the question it waits on (`promptId`, option keys and labels), `contextPct`, working `subagents`, `acceptsPrompts` |
| `get_agent` | view | `agent_id` | one agent in full: the whole question, subagents, model, branch, folder, tokens, cost, turns |
| `send_prompt` | act | `agent_id`, `text`, `interrupt` (default false) | types the text into the agent's session and presses Enter (`POST /v1/agents/{id}/prompt`) |
| `answer` | act | `agent_id`, `prompt_id`, `key` | picks an option of the question the agent waits on |
| `interrupt` | act | `agent_id` | presses Esc: stops the current turn |
| `start_agent` | admin | `folder`, `tool` (`claude`, `codex` or `shell`), `title`, `account`, `prompt` | starts an agent in the folder, on the deck; with `prompt`, waits (up to a minute) until it is ready and sends it. If the agent asks something first (trusting a new folder), the result shows the question: answer it, then `send_prompt` |
| `list_sessions` | admin | – | Claude sessions started elsewhere on this computer (as `GET /v1/live`) |
| `add_session` | admin | `session_id` | puts one of them on the deck, watched (read-only) |

## What can take a prompt

`send_prompt` types into a terminal, so it reaches only terminals the deck can type into:

- **Agents the deck started** (`agentctl new`, the deck's **+**, `start_agent`): they run in the
  deck's own tmux. The text goes in as one paste (bracketed when the program asks for it, as Claude
  Code and Codex do), so newlines stay part of the prompt, then Enter.
- **Agents in herdr, cmux or Orca**: through that app's own interface, as answers already do.
- **Not sessions you started by hand in another terminal**, or Codex app threads. Nothing can type
  into another app's terminal, so these are read-only: you see them and can answer nothing. The tool
  says so; start agents with `start_agent` to give them tasks.

A prompt is refused (`409`) while the agent is working on a turn, waiting on a question (use
`answer`), still starting, or exited. With `interrupt: true`, a working or asking agent gets Esc first,
then the prompt. Agents whose status the deck can't see (Codex or a shell started here) take a prompt
at any time. Text: up to 4000 bytes, newlines and tabs but no other control characters (an escape
sequence could press keys of its own).

## Security

- **Local only.** `POST /mcp` is served on the loopback dashboard (`127.0.0.1`) and the unix socket,
  never on the network listener. The dashboard's `Host` check applies, and a request from a web page
  (an `Origin` other than the dashboard's) is refused, so a site in your browser can't reach it.
- **A token on every transport.** The stdio transport talks to the service over its unix socket, but
  still sends the token, and the service checks it as on HTTP. The dashboard's login cookie is not a
  token.
- **Scopes.** A token may do what its scope allows and nothing more; `start_agent` needs `admin`.
- **Stored as a hash.** `mcp-tokens.json` in the data folder (0600) holds names, scopes and SHA-256
  hashes, never tokens. A revoked token's hash is dropped.
- **Audited.** Every call that changes something is in `audit.log` under `mcp:<name>`, for example
  `{"device": "mcp:claude-main", "action": "POST /v1/agents/k3f9a2/prompt", "status": 204}`, and
  `"action": "start agent"` for `start_agent`. The prompt's text is not logged.
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
