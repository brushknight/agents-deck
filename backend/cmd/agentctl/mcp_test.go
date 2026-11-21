package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type nopCtl struct{}

func (nopCtl) Answer(string, string, string) error { return nil }
func (nopCtl) Focus(string) error                  { return nil }
func (nopCtl) Interrupt(string) error              { return nil }
func (nopCtl) Dismiss(string) error                { return nil }
func (nopCtl) Resume(string) error                 { return nil }

// The stdio transport needs a token like HTTP does: the service checks it on
// the unix socket (POST /local/mcp) with the same lookup and scopes.
func TestMCPStdioNeedsToken(t *testing.T) {
	st := store.New("", "test", "0")
	st.Add(&store.Entry{A: model.Agent{ID: "a1", Title: "one", Status: model.Idle}})
	dir := t.TempDir()
	srv := &server.Server{Store: st, Ctl: nopCtl{}, Tokens: &devices.Tokens{Path: filepath.Join(dir, "mcp-tokens.json")},
		Audit: &devices.Audit{Path: filepath.Join(dir, "audit.log")}}
	good, _, _ := srv.Tokens.Create("main", devices.Act)
	old, _, _ := srv.Tokens.Create("old", devices.Act)
	_ = srv.Tokens.Revoke("old")
	h := srv.UnixHandler(nil)
	post := func(body []byte, token string) (int, []byte, error) {
		r := httptest.NewRequest(http.MethodPost, "/local/mcp", bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes(), nil
	}
	session := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_agents","arguments":{}}}
`
	lines := func(token string) []map[string]any {
		var out bytes.Buffer
		if err := mcpStdio(strings.NewReader(session), &out, token, post); err != nil {
			t.Fatal(err)
		}
		var res []map[string]any
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Fatalf("not JSON: %q", l)
			}
			res = append(res, m)
		}
		return res
	}
	for why, token := range map[string]string{"no token": "", "revoked token": old, "wrong token": "adk_nope"} {
		res := lines(token)
		if len(res) != 2 {
			t.Fatalf("%s: %v", why, res)
		}
		e, _ := res[0]["error"].(map[string]any)
		if e == nil || !strings.Contains(e["message"].(string), "agentctl mcp token create") || res[0]["id"].(float64) != 1 {
			t.Errorf("%s: initialize %v", why, res[0])
		}
	}
	res := lines(good)
	if len(res) != 2 || res[0]["result"] == nil || !strings.Contains(asText(res[1]), `"id": "a1"`) {
		t.Fatalf("good token: %v", res)
	}

	// Without the service: a plain JSON-RPC error, still one line per request.
	var out bytes.Buffer
	_ = mcpStdio(strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping"}`+"\n"), &out, good,
		func([]byte, string) (int, []byte, error) { return 0, nil, errors.New("no socket") })
	if !strings.Contains(out.String(), `"id":9`) || !strings.Contains(out.String(), "agentctl status") {
		t.Fatalf("service down: %s", out.String())
	}
}

func asText(m map[string]any) string {
	res, _ := m["result"].(map[string]any)
	c, _ := res["content"].([]any)
	if len(c) == 0 {
		return ""
	}
	s, _ := c[0].(map[string]any)["text"].(string)
	return s
}
