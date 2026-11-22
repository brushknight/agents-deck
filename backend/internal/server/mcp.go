package server

// MCP: the deck as a Model Context Protocol server, so one agent can run
// the others (docs/mcp.md). JSON-RPC 2.0 by hand over Streamable HTTP
// (POST /mcp on the loopback listener; POST /local/mcp on the unix socket
// for `agentctl mcp`, the stdio transport). Both need an MCP token, and
// every tool call runs through Dispatch as the token's identity: the same
// scope check, handlers and audit log as a paired device.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Starter starts agents in a folder (the live daemon), for start_agent.
type Starter interface {
	Create(o CreateOpts) (string, error)
	// WaitReady returns once the agent can take a prompt; ErrAsking when it
	// asks something first, ErrExited when it quit.
	WaitReady(id string, timeout time.Duration) error
}

// MCPProtocol is the protocol version this server speaks; MCPProtocols are
// the ones it accepts from a client (and then answers with).
const MCPProtocol = "2025-06-18"

var MCPProtocols = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// mcpMaxBody caps one request (a prompt is at most 4000 bytes).
const mcpMaxBody = 64 << 10

// startWait is how long start_agent waits for a new agent to be ready.
var startWait = 60 * time.Second

// NoTokenHelp is what a request without a valid token is told.
const NoTokenHelp = "agents deck MCP needs a token: create one in Settings, MCP (or run `agentctl mcp token create <name>`), then send it as `Authorization: Bearer <token>` (HTTP) or in AGENTSDECK_MCP_TOKEN (agentctl mcp)"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	rpcParse          = -32700
	rpcInvalid        = -32600
	rpcNoMethod       = -32601
	rpcBadParams      = -32602
	rpcUnauthorized   = -32001 // implementation-defined: no valid token
	rpcNullID         = "null"
	bearerPrefix      = "bearer "
	mcpAllowedMethods = "POST"
)

// serveMCP is POST /mcp (loopback) and POST /local/mcp (unix socket).
func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", mcpAllowedMethods)
		writeErr(w, http.StatusMethodNotAllowed, "the MCP endpoint takes POST only (no server-sent stream)")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBody+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "can't read the request")
		return
	}
	if len(body) > mcpMaxBody {
		writeRPC(w, http.StatusRequestEntityTooLarge, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(rpcNullID),
			Error: &rpcError{rpcInvalid, "the request is larger than 64 KB"}})
		return
	}
	tok, ok := s.mcpToken(r)
	if !ok {
		time.Sleep(300 * time.Millisecond) // slow down guessing
		w.Header().Set("WWW-Authenticate", `Bearer realm="agents-deck"`)
		writeRPC(w, http.StatusUnauthorized, rpcResponse{JSONRPC: "2.0", ID: firstID(body),
			Error: &rpcError{rpcUnauthorized, NoTokenHelp}})
		return
	}
	s.Tokens.Used(tok.Name)
	who := tok.Identity()

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			writeRPC(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(rpcNullID), Error: &rpcError{rpcParse, "parse error"}})
			return
		}
		if len(batch) == 0 {
			writeRPC(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(rpcNullID), Error: &rpcError{rpcInvalid, "empty batch"}})
			return
		}
		var out []rpcResponse
		for _, one := range batch {
			if resp, ok := s.mcpOne(who, one); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPC(w, http.StatusOK, out)
		return
	}
	resp, ok := s.mcpOne(who, trimmed)
	if !ok {
		w.WriteHeader(http.StatusAccepted) // a notification: nothing to answer
		return
	}
	writeRPC(w, http.StatusOK, resp)
}

// mcpToken is the active token the request carries.
func (s *Server) mcpToken(r *http.Request) (devices.Token, bool) {
	if s.Tokens == nil {
		return devices.Token{}, false
	}
	h := r.Header.Get("Authorization")
	if len(h) <= len(bearerPrefix) || !strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return devices.Token{}, false
	}
	return s.Tokens.Check(h[len(bearerPrefix):])
}

// firstID is the id of a single request, for an error answered before it is
// read properly (null when there is none).
func firstID(body []byte) json.RawMessage {
	var req rpcRequest
	if json.Unmarshal(body, &req) == nil && len(req.ID) > 0 {
		return req.ID
	}
	return json.RawMessage(rpcNullID)
}

func writeRPC(w http.ResponseWriter, code int, v any) {
	writeJSON(w, code, v)
}

// mcpOne answers one JSON-RPC message; ok is false for a notification.
func (s *Server) mcpOne(who devices.Device, raw json.RawMessage) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: json.RawMessage(rpcNullID), Error: &rpcError{rpcParse, "parse error"}}, true
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage(rpcNullID)
		}
		return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{rpcInvalid, `not a JSON-RPC 2.0 request ("jsonrpc": "2.0" and a "method")`}}, true
	}
	if len(req.ID) == 0 || string(req.ID) == rpcNullID && strings.HasPrefix(req.Method, "notifications/") {
		return rpcResponse{}, false // notifications/initialized, notifications/cancelled …: nothing to do
	}
	res, rerr := s.mcpMethod(who, req)
	if rerr != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, true
}

func (s *Server) mcpMethod(who devices.Device, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := MCPProtocol
		for _, v := range MCPProtocols {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agents-deck", "title": "agents deck", "version": s.version()},
			"instructions": "agents deck runs coding agents (Claude Code, Codex) on this computer. Use list_agents to see them, " +
				"send_prompt to give an idle agent a task (mode queue: a busy one gets it when its turn ends), answer when one waits on a question, interrupt to stop a turn, " +
				"and start_agent to start a new agent in a folder. Your token's scope: " + string(who.Scope) + ".",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{rpcBadParams, `tools/call needs {"name", "arguments"}`}
		}
		tool, ok := mcpToolFuncs[p.Name]
		if !ok {
			return nil, &rpcError{rpcBadParams, "no tool named " + p.Name + " (see tools/list)"}
		}
		args := map[string]any{}
		if len(p.Arguments) > 0 && string(p.Arguments) != rpcNullID {
			if err := json.Unmarshal(p.Arguments, &args); err != nil {
				return nil, &rpcError{rpcBadParams, "arguments must be an object"}
			}
		}
		text, err := tool(s, who, mcpArgs(args))
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(text, false), nil
	}
	return nil, &rpcError{rpcNoMethod, "method not found: " + req.Method}
}

func (s *Server) version() string {
	if s.Store == nil {
		return ""
	}
	return s.Store.Snapshot().Server.Version
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr}
}

// ---- arguments -----------------------------------------------------------------

type mcpArgs map[string]any

func (a mcpArgs) str(k string) string {
	v, _ := a[k].(string)
	return strings.TrimSpace(v)
}

func (a mcpArgs) need(k string) (string, error) {
	if v := a.str(k); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is required", k)
}

func (a mcpArgs) flag(k string) bool {
	v, _ := a[k].(bool)
	return v
}

// ---- tools ---------------------------------------------------------------------

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func schema(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

var agentIDProp = prop("string", "The agent's id from list_agents (6 characters, e.g. k3f9a2).")

var mcpTools = []mcpTool{
	{Name: "list_agents", Title: "List agents",
		Description: "Every agent on the deck, compact: id, title, folder, tool, where it runs, status (starting, running, waiting, idle, error, exited), " +
			"what it is doing, the question it waits on (with option keys to pass to answer), context use, working subagents, " +
			"queued (prompts waiting for its turn to end) and acceptsPrompts (whether send_prompt can reach it). Start here, and call again to follow progress.",
		InputSchema: schema(nil, map[string]any{}), Annotations: map[string]any{"readOnlyHint": true}},
	{Name: "get_agent", Title: "Get an agent",
		Description: "Everything about one agent: status, its last prompt, current activity, the full waiting question (detail and options), " +
			"working subagents, queued prompts (how many; list_queue lists them), tokens, cost, model, branch and folder.",
		InputSchema: schema([]string{"agent_id"}, map[string]any{"agent_id": agentIDProp}), Annotations: map[string]any{"readOnlyHint": true}},
	{Name: "send_prompt", Title: "Send a prompt",
		Description: "Types a task into the agent's session and presses Enter, as if you typed it there. Works for agents the deck started " +
			"and agents in herdr, cmux or Orca (acceptsPrompts true); sessions started by hand in another terminal are read-only. " +
			"mode queue (the default) types it in at once when the agent is idle, else keeps it and types it in when the agent's turn ends (status idle), " +
			"one queued prompt per turn, in order, like typing several prompts one after another in its chat. " +
			"Queued prompts wait while the agent asks a question: answer it, and the queue goes on after that turn. " +
			"mode type types it in now even while the agent works, so the tool's own queue takes it, as when you type ahead in its chat " +
			"(Claude Code reads it at its next step, during the current turn; refused while the agent asks a question). " +
			"mode now refuses while the agent is working or waiting on a question; mode interrupt presses Esc first to stop it. " +
			"The result says sent, or queued with its queue_id. Text is at most 4000 bytes; newlines are kept as part of the prompt.",
		InputSchema: schema([]string{"agent_id", "text"}, map[string]any{
			"agent_id": agentIDProp,
			"text":     prop("string", "The prompt, as you would type it to the agent."),
			"mode": map[string]any{"type": "string", "enum": []string{"queue", "type", "now", "interrupt"},
				"description": "queue (default): now if idle, else when its turn ends (the deck holds it). type: typed in now even while it works (the agent's own queue holds it). now: only when the agent is idle. interrupt: stop its turn (Esc), then type."},
			"queue_position": map[string]any{"type": "string", "enum": []string{"back", "front"},
				"description": "mode queue: back (default) waits behind prompts queued earlier; front goes next."},
			"interrupt": prop("boolean", "Same as mode interrupt (kept for older clients). Default false."),
		})},
	{Name: "list_queue", Title: "List queued prompts",
		Description: "The prompts queued for an agent (send_prompt with mode queue), next first: queue_id, text, who queued it and when.",
		InputSchema: schema([]string{"agent_id"}, map[string]any{"agent_id": agentIDProp}), Annotations: map[string]any{"readOnlyHint": true}},
	{Name: "cancel_queued", Title: "Cancel a queued prompt",
		Description: "Takes a queued prompt back before it is typed in. Take queue_id from send_prompt or list_queue.",
		InputSchema: schema([]string{"agent_id", "queue_id"}, map[string]any{
			"agent_id": agentIDProp,
			"queue_id": prop("string", "The queued prompt's id (q and 6 characters)."),
		})},
	{Name: "answer", Title: "Answer a question",
		Description: "Answers the question or permission prompt an agent waits on (status waiting) by picking one of its options. " +
			"Take prompt_id and the option key from list_agents or get_agent; a stale prompt_id is refused, so read again and retry.",
		InputSchema: schema([]string{"agent_id", "prompt_id", "key"}, map[string]any{
			"agent_id":  agentIDProp,
			"prompt_id": prop("string", "waiting.promptId of the agent (changes with every new question)."),
			"key":       prop("string", "The key of the option to pick, e.g. \"1\"."),
		})},
	{Name: "interrupt", Title: "Interrupt an agent",
		Description: "Presses Esc in the agent's session: stops its current turn (and closes a question unanswered). The agent stays, idle.",
		InputSchema: schema([]string{"agent_id"}, map[string]any{"agent_id": agentIDProp})},
	{Name: "start_agent", Title: "Start an agent",
		Description: "Starts a new agent in a folder on this computer, on the deck, and (with prompt) waits until it is ready and gives it " +
			"that first task. If it asks something first (trust this folder?), the result says so: answer it, then send_prompt. Needs a token with scope admin.",
		InputSchema: schema([]string{"folder"}, map[string]any{
			"folder":  prop("string", "Absolute path of the project folder to work in."),
			"tool":    map[string]any{"type": "string", "enum": []string{"claude", "codex", "shell"}, "description": "claude (default), codex, or a plain shell."},
			"title":   prop("string", "Name on the deck (default: the folder's name)."),
			"account": prop("string", "claude only: the Claude account to run in, when several are set up (default: the default one)."),
			"prompt":  prop("string", "The first task to send once it is ready (optional)."),
		})},
	{Name: "list_sessions", Title: "List other sessions",
		Description: "Claude sessions running on this computer that the deck didn't start (in other terminals, or in the background), " +
			"and whether each is on the deck. They can be watched (add_session) but not given prompts. Needs scope admin.",
		InputSchema: schema(nil, map[string]any{}), Annotations: map[string]any{"readOnlyHint": true}},
	{Name: "add_session", Title: "Watch a session",
		Description: "Puts a session from list_sessions on the deck, watched: you see its status, but it stays read-only (it runs in its own terminal). Needs scope admin.",
		InputSchema: schema([]string{"session_id"}, map[string]any{"session_id": prop("string", "sessionId from list_sessions.")})},
}

type mcpToolFunc func(s *Server, who devices.Device, a mcpArgs) (string, error)

var mcpToolFuncs map[string]mcpToolFunc

func init() {
	mcpToolFuncs = map[string]mcpToolFunc{
		"list_agents":   toolListAgents,
		"get_agent":     toolGetAgent,
		"send_prompt":   toolSendPrompt,
		"list_queue":    toolListQueue,
		"cancel_queued": toolCancelQueued,
		"answer":        toolAnswer,
		"interrupt":     toolInterrupt,
		"start_agent":   toolStartAgent,
		"list_sessions": toolListSessions,
		"add_session":   toolAddSession,
	}
}

// call runs one API request as who and turns a failure into a plain error.
func (s *Server) call(who devices.Device, method, path string, body any) ([]byte, error) {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	code, out := s.Dispatch(who, method, path, raw)
	if code < 300 {
		return out, nil
	}
	msg := errText(out)
	if msg == "" {
		msg = http.StatusText(code)
	}
	if code == http.StatusForbidden {
		msg += ": create a token with a wider scope in Settings, MCP"
	}
	return nil, errors.New(msg)
}

func errText(body []byte) string {
	var e struct{ Error string }
	if json.Unmarshal(body, &e) == nil {
		return e.Error
	}
	return ""
}

func (s *Server) agents(who devices.Device) ([]model.Agent, error) {
	out, err := s.call(who, http.MethodGet, "/v1/state", nil)
	if err != nil {
		return nil, err
	}
	var st model.State
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, err
	}
	return st.Agents, nil
}

// acceptsPrompts: the deck can type into the agent's session.
func acceptsPrompts(a model.Agent) bool {
	return a.Status != model.Exited && (!a.External || a.App != "")
}

// runsIn says where an agent runs, from the orchestrator's side.
func runsIn(a model.Agent) string {
	switch {
	case a.App != "":
		return a.App
	case a.External && a.Tool == "claude":
		return "another terminal (read-only)"
	case a.External:
		return "the Codex app (read-only)"
	}
	return "deck"
}

type mcpPrompt struct {
	PromptID string         `json:"promptId"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Detail   string         `json:"detail,omitempty"`
	Context  string         `json:"context,omitempty"`
	Options  []model.Option `json:"options"`
}

func promptOf(p *model.Prompt, full bool) *mcpPrompt {
	if p == nil {
		return nil
	}
	m := &mcpPrompt{PromptID: p.ID, Kind: p.Kind, Title: p.Title, Options: p.Options}
	if m.Options == nil {
		m.Options = []model.Option{}
	}
	if full {
		m.Detail, m.Context = p.Detail, p.Context
	} else if len(p.Detail) <= 300 {
		m.Detail = p.Detail
	}
	return m
}

type mcpAgentRow struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Folder         string     `json:"folder"`
	Tool           string     `json:"tool"`
	RunsIn         string     `json:"runsIn"`
	Status         string     `json:"status"`
	For            string     `json:"for"`
	Doing          string     `json:"doing,omitempty"`
	LastPrompt     string     `json:"lastPrompt,omitempty"`
	Waiting        *mcpPrompt `json:"waiting,omitempty"`
	Error          string     `json:"error,omitempty"`
	ContextPct     int        `json:"contextPct"`
	Subagents      int        `json:"subagents"`
	Queued         int        `json:"queued"`
	Unseen         bool       `json:"finishedUnseen,omitempty"`
	AcceptsPrompts bool       `json:"acceptsPrompts"`
}

func rowOf(a model.Agent) mcpAgentRow {
	r := mcpAgentRow{ID: a.ID, Title: a.Title, Folder: a.Folder, Tool: a.Tool, RunsIn: runsIn(a), Status: string(a.Status),
		LastPrompt: clip(a.LastPrompt, 160), Waiting: promptOf(a.Waiting, false), Subagents: len(a.Subagents), Queued: a.Queued,
		Unseen: a.Unseen, AcceptsPrompts: acceptsPrompts(a)}
	if a.AITitle != "" {
		r.Title = a.Title + " (" + a.AITitle + ")"
	}
	if !a.StatusSince.IsZero() {
		r.For = time.Since(a.StatusSince).Round(time.Second).String()
	}
	if a.Activity != nil {
		r.Doing = strings.TrimSpace(a.Activity.Tool + " " + a.Activity.Detail)
	}
	if a.Error != nil {
		r.Error = a.Error.Message
	}
	if a.Context.Window > 0 {
		r.ContextPct = int(a.Context.Used * 100 / a.Context.Window)
	}
	return r
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

func asJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}

func toolListAgents(s *Server, who devices.Device, _ mcpArgs) (string, error) {
	list, err := s.agents(who)
	if err != nil {
		return "", err
	}
	rows := make([]mcpAgentRow, 0, len(list))
	for _, a := range list {
		rows = append(rows, rowOf(a))
	}
	return asJSON(map[string]any{"agents": rows}), nil
}

func (s *Server) findAgent(who devices.Device, id string) (model.Agent, error) {
	list, err := s.agents(who)
	if err != nil {
		return model.Agent{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	return model.Agent{}, fmt.Errorf("no agent %s on the deck: list_agents shows the ids", id)
}

func toolGetAgent(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	ag, err := s.findAgent(who, id)
	if err != nil {
		return "", err
	}
	type full struct {
		mcpAgentRow
		Cwd       string           `json:"cwd"`
		Branch    string           `json:"branch,omitempty"`
		Model     string           `json:"model,omitempty"`
		Effort    string           `json:"effort,omitempty"`
		Account   string           `json:"account,omitempty"`
		Context   model.Context    `json:"context"`
		Tokens    model.Tokens     `json:"tokens"`
		CostUSD   float64          `json:"costUsd"`
		Turns     int              `json:"turns"`
		Subagents []model.Subagent `json:"subagentList,omitempty"`
		Started   time.Time        `json:"startedAt"`
		Resumable bool             `json:"resumable,omitempty"`
		Note      string           `json:"note,omitempty"`
	}
	f := full{mcpAgentRow: rowOf(ag), Cwd: ag.Cwd, Branch: ag.Branch, Model: ag.Model, Effort: ag.Effort, Context: ag.Context,
		Tokens: ag.Tokens, CostUSD: ag.CostUSD, Turns: ag.Turns, Subagents: ag.Subagents, Started: ag.StartedAt, Resumable: ag.Resumable}
	f.LastPrompt = ag.LastPrompt
	f.Waiting = promptOf(ag.Waiting, true)
	if ag.Account != nil {
		f.Account = ag.Account.Name
	}
	if !f.AcceptsPrompts && ag.Status != model.Exited {
		f.Note = "read-only: it runs in another terminal or app, so the deck can't type into it; start agents with start_agent to give them tasks"
	}
	return asJSON(f), nil
}

func toolSendPrompt(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	text, _ := a["text"].(string)
	mode := a.str("mode")
	switch {
	case mode == "" && a.flag("interrupt"):
		mode = "interrupt"
	case mode == "" && s.Queue == nil:
		mode = "now" // a service without a queue (some tests)
	case mode == "":
		mode = "queue"
	case mode != "now" && mode != "interrupt" && mode != "queue" && mode != "type":
		return "", errors.New("mode is queue, type, now or interrupt")
	case a.flag("interrupt") && mode != "interrupt":
		return "", errors.New("interrupt true goes with mode interrupt only (or leave mode out)")
	}
	body := map[string]any{"text": text, "mode": mode}
	if mode == "queue" {
		switch pos := a.str("queue_position"); pos {
		case "", "back", "front":
			body["position"] = pos
		default:
			return "", errors.New("queue_position is back or front")
		}
	}
	out, err := s.call(who, http.MethodPost, "/v1/agents/"+url.PathEscape(id)+"/prompt", body)
	if err != nil {
		return "", err
	}
	var r struct {
		Status   string `json:"status"`
		ID       string `json:"id"`
		Position int    `json:"position"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Status == "queued" {
		return asJSON(map[string]any{"status": "queued", "agent_id": id, "queue_id": r.ID, "position": r.Position,
			"note": "typed in when " + id + "'s turn ends (status idle); list_queue shows it, cancel_queued takes it back"}), nil
	}
	if mode == "type" {
		return "typed: " + id + " has the prompt in its own chat; if it was working, its tool holds it and reads it at its next step (the deck's queue doesn't list it)", nil
	}
	return "sent: " + id + " has the prompt (list_agents shows it running once it starts)", nil
}

func toolListQueue(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	out, err := s.call(who, http.MethodGet, "/v1/agents/"+url.PathEscape(id)+"/queue", nil)
	if err != nil {
		return "", err
	}
	var r struct{ Queue []QueuedPrompt }
	if err := json.Unmarshal(out, &r); err != nil {
		return "", err
	}
	type row struct {
		QueueID  string    `json:"queue_id"`
		Position int       `json:"position"`
		Text     string    `json:"text"`
		By       string    `json:"by"`
		QueuedAt time.Time `json:"queuedAt"`
	}
	rows := make([]row, 0, len(r.Queue))
	for i, q := range r.Queue {
		rows = append(rows, row{q.ID, i + 1, q.Text, q.By, q.QueuedAt})
	}
	return asJSON(map[string]any{"agent_id": id, "queue": rows}), nil
}

func toolCancelQueued(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	qid, err := a.need("queue_id")
	if err != nil {
		return "", err
	}
	if _, err := s.call(who, http.MethodPost, "/v1/agents/"+url.PathEscape(id)+"/queue/"+url.PathEscape(qid)+"/cancel", nil); err != nil {
		return "", err
	}
	return "cancelled " + qid + ": it won't be sent to " + id, nil
}

func toolAnswer(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	pid, err := a.need("prompt_id")
	if err != nil {
		return "", err
	}
	key, err := a.need("key")
	if err != nil {
		return "", err
	}
	if _, err := s.call(who, http.MethodPost, "/v1/agents/"+url.PathEscape(id)+"/answer", map[string]string{"promptId": pid, "key": key}); err != nil {
		return "", err
	}
	return "answered " + id + " with option " + key, nil
}

func toolInterrupt(s *Server, who devices.Device, a mcpArgs) (string, error) {
	id, err := a.need("agent_id")
	if err != nil {
		return "", err
	}
	if _, err := s.call(who, http.MethodPost, "/v1/agents/"+url.PathEscape(id)+"/interrupt", nil); err != nil {
		return "", err
	}
	return "interrupted " + id + ": its turn is stopped", nil
}

func toolStartAgent(s *Server, who devices.Device, a mcpArgs) (string, error) {
	if !who.Scope.Allows(devices.Admin) {
		return "", fmt.Errorf("%s may %s, start_agent needs admin: create a token with scope admin in Settings, MCP", who.Name, who.Scope)
	}
	st, ok := s.Ctl.(Starter)
	if !ok {
		return "", errors.New("starting agents is not available on this service (demo mode)")
	}
	folder, err := a.need("folder")
	if err != nil {
		return "", err
	}
	tool := a.str("tool")
	switch tool {
	case "":
		tool = "claude"
	case "claude", "codex", "shell":
	default:
		return "", errors.New("tool must be claude, codex or shell")
	}
	prompt, _ := a["prompt"].(string)
	if strings.TrimSpace(prompt) != "" {
		if prompt, err = CleanPrompt(prompt); err != nil {
			return "", err
		}
	} else {
		prompt = ""
	}
	id, err := st.Create(CreateOpts{Dir: folder, Title: a.str("title"), Tool: tool, Account: a.str("account")})
	code := http.StatusOK
	if err != nil {
		code = http.StatusBadRequest
	}
	if s.Audit != nil {
		s.Audit.Log(devices.AuditEntry{Device: who.Name, Action: "start agent", Status: code, Detail: tool + " " + id})
	}
	if s.Devices != nil {
		s.Devices.Seen(who.Name)
	}
	if err != nil {
		return "", err
	}
	log.Printf("mcp: %s started agent %s (%s)", who.Name, id, tool)
	if prompt == "" {
		return asJSON(map[string]any{"agent_id": id, "started": true, "note": "started; send it a task with send_prompt once list_agents shows it idle"}), nil
	}
	switch err := st.WaitReady(id, startWait); {
	case errors.Is(err, ErrAsking):
		ag, _ := s.findAgent(who, id)
		return asJSON(map[string]any{"agent_id": id, "started": true, "prompted": false,
			"note":    "it asks something before it can work: answer it, then send the prompt with send_prompt",
			"waiting": promptOf(ag.Waiting, true)}), nil
	case err != nil:
		return "", fmt.Errorf("started %s, but no prompt was sent: %v", id, err)
	}
	if _, err := s.call(who, http.MethodPost, "/v1/agents/"+url.PathEscape(id)+"/prompt", map[string]any{"text": prompt}); err != nil {
		return "", fmt.Errorf("started %s, but the prompt didn't go through: %v", id, err)
	}
	return asJSON(map[string]any{"agent_id": id, "started": true, "prompted": true}), nil
}

func toolListSessions(s *Server, who devices.Device, _ mcpArgs) (string, error) {
	out, err := s.call(who, http.MethodGet, "/v1/live", nil)
	if err != nil {
		return "", err
	}
	var v any
	_ = json.Unmarshal(out, &v)
	return asJSON(v), nil
}

func toolAddSession(s *Server, who devices.Device, a mcpArgs) (string, error) {
	sid, err := a.need("session_id")
	if err != nil {
		return "", err
	}
	if _, err := s.call(who, http.MethodPost, "/v1/live/"+url.PathEscape(sid)+"/add", nil); err != nil {
		return "", err
	}
	return "session " + sid + " is on the deck, watched (read-only)", nil
}
