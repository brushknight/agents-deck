package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// mcpCtl is fakeCtl that also takes prompts and starts agents.
type mcpCtl struct {
	fakeCtl
	st       *store.Store
	prompted []string
	started  []CreateOpts
}

func (c *mcpCtl) Prompt(id, text string, interrupt bool) error {
	e, ok := c.st.Get(id)
	switch {
	case !ok:
		return ErrNotFound
	case e.A.External && e.A.App == "":
		return fmt.Errorf("%w: this session runs in another terminal", ErrReadOnly)
	case e.A.Status == model.Running && !interrupt:
		return ErrBusy
	case e.A.Status == model.Waiting && !interrupt:
		return ErrAsking
	}
	c.prompted = append(c.prompted, fmt.Sprintf("%s %q %v", id, text, interrupt))
	return nil
}

// Type takes a prompt while the agent works (its tool's own queue), not
// while it asks a question.
func (c *mcpCtl) Type(id, text string) error {
	e, ok := c.st.Get(id)
	switch {
	case !ok:
		return ErrNotFound
	case e.A.Status == model.Waiting:
		return ErrAsking
	}
	c.prompted = append(c.prompted, fmt.Sprintf("%s %q typed", id, text))
	return nil
}

func (c *mcpCtl) Create(o CreateOpts) (string, error) {
	c.started = append(c.started, o)
	c.st.Add(&store.Entry{A: model.Agent{ID: "n1", Title: "new", Status: model.Idle, Tool: o.Tool}})
	return "n1", nil
}

func (c *mcpCtl) WaitReady(string, time.Duration) error { return nil }

func newMCPServer(t *testing.T) (*Server, *mcpCtl, map[string]string) {
	t.Helper()
	s, _ := newTestServer()
	s.Store.Add(&store.Entry{A: model.Agent{ID: "busy", Title: "busy", Status: model.Running, Tool: "claude",
		Activity: &model.Activity{Tool: "Edit", Detail: "main.go"}, Context: model.Context{Used: 50, Window: 200}}})
	s.Store.Add(&store.Entry{A: model.Agent{ID: "ask", Title: "ask", Status: model.Waiting, Tool: "claude",
		Waiting: &model.Prompt{ID: "p1", Kind: "permission", Title: "run this command?", Options: []model.Option{{Key: "1", Label: "yes"}, {Key: "2", Label: "no"}}}}})
	s.Store.Add(&store.Entry{A: model.Agent{ID: "ext", Title: "elsewhere", Status: model.Idle, Tool: "claude", External: true}})
	ctl := &mcpCtl{st: s.Store}
	s.Ctl = ctl
	s.Tokens = &devices.Tokens{Path: filepath.Join(t.TempDir(), "mcp-tokens.json")}
	tok := map[string]string{}
	for name, sc := range map[string]devices.Scope{"looker": devices.View, "doer": devices.Act, "chief": devices.Admin, "old": devices.Act} {
		secret, _, err := s.Tokens.Create(name, sc)
		if err != nil {
			t.Fatal(err)
		}
		tok[name] = secret
	}
	if err := s.Tokens.Revoke("old"); err != nil {
		t.Fatal(err)
	}
	return s, ctl, tok
}

// rpc posts one JSON-RPC body to h at path with a token ("" for none).
func rpc(h http.Handler, path, token, body string) (int, map[string]any) {
	hdr := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	w := do(h, "POST", path, body, hdr)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func call(name string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	return string(b)
}

// toolText is a tools/call result's text and isError.
func toolText(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", out)
	}
	content := res["content"].([]any)
	return content[0].(map[string]any)["text"].(string), res["isError"].(bool)
}

func TestMCPHandshake(t *testing.T) {
	s, _, tok := newMCPServer(t)
	h := s.LocalHandler()

	code, out := rpc(h, "/mcp", tok["doer"], `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	res, _ := out["result"].(map[string]any)
	if code != 200 || res["protocolVersion"] != "2025-03-26" || res["serverInfo"].(map[string]any)["name"] != "agents-deck" ||
		res["capabilities"].(map[string]any)["tools"] == nil || out["id"].(float64) != 1 {
		t.Fatalf("initialize: %d %v", code, out)
	}
	if _, out = rpc(h, "/mcp", tok["doer"], `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`); out["result"].(map[string]any)["protocolVersion"] != MCPProtocol {
		t.Fatalf("unknown version: %v", out)
	}
	if w := do(h, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{"Authorization": "Bearer " + tok["doer"]}); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Fatalf("notification: %d %q", w.Code, w.Body)
	}
	if _, out = rpc(h, "/mcp", tok["doer"], `{"jsonrpc":"2.0","id":"x","method":"ping"}`); out["id"] != "x" || out["result"] == nil {
		t.Fatalf("ping: %v", out)
	}
	_, out = rpc(h, "/mcp", tok["doer"], `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tools {
		m := x.(map[string]any)
		if m["inputSchema"].(map[string]any)["type"] != "object" || m["description"] == "" {
			t.Errorf("tool %v", m)
		}
		names[m["name"].(string)] = true
	}
	for _, n := range []string{"list_agents", "get_agent", "send_prompt", "answer", "interrupt", "start_agent", "list_sessions", "add_session"} {
		if !names[n] {
			t.Errorf("tools/list lacks %s", n)
		}
	}
	// JSON-RPC errors.
	if _, out = rpc(h, "/mcp", tok["doer"], `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`); out["error"].(map[string]any)["code"].(float64) != rpcNoMethod {
		t.Fatalf("unknown method: %v", out)
	}
	if _, out = rpc(h, "/mcp", tok["doer"], `{not json`); out["error"].(map[string]any)["code"].(float64) != rpcParse {
		t.Fatalf("parse error: %v", out)
	}
	if _, out = rpc(h, "/mcp", tok["doer"], `{"id":5,"method":"ping"}`); out["error"].(map[string]any)["code"].(float64) != rpcInvalid {
		t.Fatalf("no jsonrpc: %v", out)
	}
	if _, out = rpc(h, "/mcp", tok["doer"], call("fly", nil)); out["error"].(map[string]any)["code"].(float64) != rpcBadParams {
		t.Fatalf("unknown tool: %v", out)
	}
	// A batch: answers for the requests, none for the notification.
	w := do(h, "POST", "/mcp", `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"nope"}]`,
		map[string]string{"Authorization": "Bearer " + tok["doer"]})
	var batch []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil || len(batch) != 2 || batch[1]["error"] == nil {
		t.Fatalf("batch: %s", w.Body)
	}
	// No server-sent stream.
	if w := do(h, "GET", "/mcp", "", map[string]string{"Authorization": "Bearer " + tok["doer"]}); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp: %d", w.Code)
	}
}

// Without a valid token nothing happens, on the loopback listener and on
// the unix socket (agentctl mcp) alike.
func TestMCPNeedsToken(t *testing.T) {
	s, _, tok := newMCPServer(t)
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	for _, c := range []struct {
		name string
		h    http.Handler
		path string
	}{{"loopback", s.LocalHandler(), "/mcp"}, {"socket", s.UnixHandler(nil), "/local/mcp"}} {
		for why, token := range map[string]string{"missing": "", "wrong": "adk_" + strings.Repeat("0", 64), "revoked": tok["old"], "not ours": "websession"} {
			code, out := rpc(c.h, c.path, token, init)
			e, _ := out["error"].(map[string]any)
			if code != http.StatusUnauthorized || e == nil || !strings.Contains(e["message"].(string), "Settings, MCP") || out["id"].(float64) != 1 {
				t.Errorf("%s, %s token: %d %v", c.name, why, code, out)
			}
		}
		if code, _ := rpc(c.h, c.path, tok["doer"], init); code != http.StatusOK {
			t.Errorf("%s, good token: %d", c.name, code)
		}
	}
	// The web session cookie is not a token.
	w := do(s.LocalHandler(), "POST", "/mcp", init, map[string]string{"Cookie": sessionCookie + "=websession"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("cookie: %d", w.Code)
	}
	// A page on another site can't reach it, even with a token.
	w = do(s.LocalHandler(), "POST", "/mcp", init, map[string]string{"Authorization": "Bearer " + tok["doer"], "Origin": "http://evil.example"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	// Nor through DNS rebinding (another Host).
	w = do(s.LocalHandler(), "POST", "/mcp", init, map[string]string{"Authorization": "Bearer " + tok["doer"], "Host": "rebind.example:7340"})
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("foreign host: %d", w.Code)
	}
}

func TestMCPTools(t *testing.T) {
	s, ctl, tok := newMCPServer(t)
	h := s.LocalHandler()

	_, out := rpc(h, "/mcp", tok["looker"], call("list_agents", nil))
	text, isErr := toolText(t, out)
	var list struct{ Agents []mcpAgentRow }
	if err := json.Unmarshal([]byte(text), &list); err != nil || isErr || len(list.Agents) != 4 {
		t.Fatalf("list_agents: %v %s", err, text)
	}
	rows := map[string]mcpAgentRow{}
	for _, r := range list.Agents {
		rows[r.ID] = r
	}
	if r := rows["busy"]; r.Doing != "Edit main.go" || r.ContextPct != 25 || !r.AcceptsPrompts || r.RunsIn != "deck" {
		t.Errorf("busy: %+v", r)
	}
	if r := rows["ask"]; r.Waiting == nil || r.Waiting.PromptID != "p1" || len(r.Waiting.Options) != 2 {
		t.Errorf("ask: %+v", r)
	}
	if r := rows["ext"]; r.AcceptsPrompts {
		t.Errorf("an agent in another terminal accepts prompts: %+v", r)
	}

	_, out = rpc(h, "/mcp", tok["looker"], call("get_agent", map[string]any{"agent_id": "nope"}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "list_agents") {
		t.Errorf("get_agent unknown: %s", text)
	}

	// Scopes: a view token can't send prompts.
	_, out = rpc(h, "/mcp", tok["looker"], call("send_prompt", map[string]any{"agent_id": "a1", "text": "hi"}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "needs act") || len(ctl.prompted) != 0 {
		t.Errorf("view token prompted: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "a1", "text": "fix the tests\nthen commit"}))
	if text, isErr = toolText(t, out); isErr || len(ctl.prompted) != 1 || ctl.prompted[0] != `a1 "fix the tests\nthen commit" false` {
		t.Errorf("send_prompt: %s %v", text, ctl.prompted)
	}
	for agent, want := range map[string]string{"busy": "working", "ask": "question", "ext": "another terminal", "zzz": "no such agent"} {
		_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": agent, "text": "hi"}))
		if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, want) {
			t.Errorf("send_prompt to %s: %s", agent, text)
		}
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "stop, do this", "interrupt": true}))
	if _, isErr = toolText(t, out); isErr || ctl.prompted[1] != `busy "stop, do this" true` {
		t.Errorf("send_prompt with interrupt: %v", ctl.prompted)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "a1", "text": "bad \x1b[201~ key"}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "control characters") {
		t.Errorf("escape sequence: %s", text)
	}

	_, out = rpc(h, "/mcp", tok["doer"], call("answer", map[string]any{"agent_id": "a1", "prompt_id": "p1", "key": "2"}))
	if _, isErr = toolText(t, out); isErr || ctl.answered != "2" {
		t.Errorf("answer: %v", out)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("interrupt", map[string]any{"agent_id": "a1"}))
	if _, isErr = toolText(t, out); isErr {
		t.Errorf("interrupt: %v", out)
	}

	// start_agent needs admin; it starts the agent and sends its first prompt.
	_, out = rpc(h, "/mcp", tok["doer"], call("start_agent", map[string]any{"folder": t.TempDir(), "prompt": "hello"}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "needs admin") || len(ctl.started) != 0 {
		t.Errorf("act token started an agent: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("start_agent", map[string]any{"folder": "/work/app", "tool": "codex", "prompt": "hello"}))
	if text, isErr = toolText(t, out); isErr || len(ctl.started) != 1 || ctl.started[0].Tool != "codex" || ctl.prompted[len(ctl.prompted)-1] != `n1 "hello" false` {
		t.Errorf("start_agent: %s %+v %v", text, ctl.started, ctl.prompted)
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("list_sessions", nil))
	if _, isErr = toolText(t, out); isErr {
		t.Errorf("list_sessions: %v", out)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("list_sessions", nil))
	if _, isErr = toolText(t, out); !isErr {
		t.Errorf("act token listed sessions: %v", out)
	}

	// Audited under the token's name.
	b, _ := os.ReadFile(s.Audit.Path)
	for _, want := range []string{`"device":"mcp:doer","action":"POST /v1/agents/a1/prompt","status":204`, `"device":"mcp:chief","action":"start agent"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("audit lacks %s:\n%s", want, b)
		}
	}
	// Last used is noted.
	for _, tk := range s.Tokens.List() {
		if tk.Name == "doer" && tk.LastUsed.IsZero() {
			t.Error("last used not noted")
		}
	}
}

func TestPromptEndpoint(t *testing.T) {
	s, ctl, _ := newMCPServer(t)
	h := s.UnixHandler(nil)
	for _, c := range []struct {
		id, body string
		code     int
	}{
		{"a1", `{"text": "go"}`, 204},
		{"busy", `{"text": "go"}`, 409},
		{"busy", `{"text": "go", "interrupt": true}`, 204},
		{"ask", `{"text": "go"}`, 409},
		{"ext", `{"text": "go"}`, 422},
		{"zzz", `{"text": "go"}`, 404},
		{"a1", `{"text": "  "}`, 400},
		{"a1", `{"text": "` + strings.Repeat("x", MaxPromptBytes+1) + `"}`, 400},
		{"a1", `{"text": "beep\u0007"}`, 400},
		{"a1", `nope`, 400},
	} {
		if w := do(h, "POST", "/v1/agents/"+c.id+"/prompt", c.body, nil); w.Code != c.code {
			t.Errorf("%s %.40s: %d, want %d (%s)", c.id, c.body, w.Code, c.code, w.Body)
		}
	}
	if len(ctl.prompted) != 2 {
		t.Errorf("prompted %v", ctl.prompted)
	}
	if NeedScope("POST", "/v1/agents/a1/prompt") != devices.Act {
		t.Error("sending a prompt should need act")
	}
}

func TestCleanPrompt(t *testing.T) {
	if got, err := CleanPrompt("line one\r\nline two\n\n"); err != nil || got != "line one\nline two" {
		t.Errorf("%q %v", got, err)
	}
	if _, err := CleanPrompt("tab\tok"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "\n", "a\x00b", "a\x1bb", "a\u009bb", "\xff"} {
		if _, err := CleanPrompt(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
