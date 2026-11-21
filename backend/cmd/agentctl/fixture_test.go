package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// A small dataset: one agent at work with a hat of its own and subagents,
// one waiting for an answer.
const testDataset = `{
  "server": {"name": "test lab"},
  "steady": true,
  "agents": [
    {"id": "work01", "slot": 0, "title": "radio", "tool": "claude", "status": "running",
     "cwd": "/work/radio", "folder": "radio", "context": {"used": 1000, "window": 200000},
     "hat": {"shape": "propeller", "color": "pink", "auto": true},
     "activities": [{"tool": "Read", "detail": "decode.py"}, {"tool": "Bash", "detail": "pytest -q"}],
     "subagents": [{"id": "s1", "title": "Read the captures", "type": "Explore", "tool": "Read", "tools": ["Read", "Grep"]},
                   {"id": "s2", "title": "Write the decoder", "tool": "Edit"},
                   {"id": "s3", "title": "Run the tests", "tool": "Bash"}]},
    {"id": "wait02", "slot": 1, "title": "bench", "tool": "claude", "status": "waiting",
     "cwd": "/work/bench", "folder": "bench", "lastActiveAgo": "2m",
     "hat": {"shape": "wizard", "color": "sky", "auto": true},
     "activities": [{"tool": "Bash", "detail": "./read-chip.sh"}],
     "waiting": {"id": "p7", "kind": "permission", "title": "run this command?", "detail": "./read-chip.sh",
                 "options": [{"key": "1", "label": "yes", "primary": true}, {"key": "2", "label": "no"}]}}
  ]
}`

// sock asks the demo service over its socket (AGENTSTERM_HOME in env), as the CLI does.
func sock(t *testing.T, env []string, method, path string, in, out any) {
	t.Helper()
	var home string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "AGENTSTERM_HOME="); ok {
			home = v
		}
	}
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(home, "agentd.sock"))
		},
	}}
	var body bytes.Buffer
	if in != nil {
		_ = json.NewEncoder(&body).Encode(in)
	}
	req, _ := http.NewRequest(method, "http://agentd"+path, &body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %s", method, path, resp.Status)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// serve --demo --fixture FILE serves the dataset's fleet under the dataset's
// deck name, with its hats and subagents, and an answer from the CLI sends
// the waiting agent back to work.
func TestServeDemoFixture(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fleet.json")
	if err := os.WriteFile(file, []byte(testDataset), 0o600); err != nil {
		t.Fatal(err)
	}
	env := demoDeckWith(t, "wait02", "--fixture", file)
	fleet := func() (model.State, map[string]model.Agent) {
		t.Helper()
		var s model.State
		sock(t, env, "GET", "/v1/state", nil, &s)
		by := map[string]model.Agent{}
		for _, a := range s.Agents {
			by[a.ID] = a
		}
		return s, by
	}
	s, by := fleet()
	if s.Server.Name != "test lab" || len(s.Agents) != 2 {
		t.Fatalf("deck %q with %d agents, want \"test lab\" with 2", s.Server.Name, len(s.Agents))
	}
	if h := by["work01"].Hat; h == nil || h.Shape != "propeller" || h.Color != "pink" {
		t.Errorf("work01 wears %+v, want the dataset's pink propeller", h)
	}
	if h := by["wait02"].Hat; h == nil || h.Shape != "wizard" || h.Color != "sky" {
		t.Errorf("wait02 wears %+v, want the dataset's sky wizard hat", h)
	}
	if n := len(by["work01"].Subagents); n != 3 {
		t.Errorf("work01 has %d subagents, want 3", n)
	}
	if w := by["wait02"].Waiting; by["wait02"].Status != model.Waiting || w == nil || w.ID != "p7" {
		t.Fatalf("wait02 is %s with prompt %+v, want it waiting on p7", by["wait02"].Status, w)
	}
	sock(t, env, "POST", "/v1/agents/wait02/answer", map[string]string{"promptId": "p7", "key": "1"}, nil)
	_, by = fleet()
	a := by["wait02"]
	if a.Status != model.Running || a.Waiting != nil || a.Activity == nil || a.Activity.Detail != "./read-chip.sh" {
		t.Errorf("after the answer wait02 is %s (prompt %+v, activity %+v), want it running ./read-chip.sh", a.Status, a.Waiting, a.Activity)
	}
}

// --fixture without --demo, and a dataset that can't be read, stop serve
// with a reason.
func TestServeFixtureErrors(t *testing.T) {
	env := demoDeckWith(t, "r3l34s", "--still") // builds agentctl; its service is not used here
	for i, kv := range env {
		if strings.HasPrefix(kv, "AGENTSTERM_HOME=") {
			env[i] = "AGENTSTERM_HOME=" + t.TempDir() // a folder with no service
		}
	}
	if out, err := run(t, env, "serve", "--fixture", "nothing.json"); err == nil || !strings.Contains(out, "--fixture goes with --demo") {
		t.Errorf("serve --fixture without --demo: %v %q", err, out)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"agents": [{"id": "a", "status": "waiting"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, env, "serve", "--demo", "--fixture", bad); err == nil || !strings.Contains(out, "waiting agent needs") {
		t.Errorf("serve --demo --fixture with a bad dataset: %v %q", err, out)
	}
}
