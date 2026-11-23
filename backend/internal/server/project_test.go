package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// withProjects gives the deck two agents in projects (the matching itself is
// admin's, tested there) and one outside any.
func withProjects(s *Server) {
	s.Store.Add(&store.Entry{A: model.Agent{ID: "hb1", Title: "harbor", Status: model.Idle, Tool: "claude", Cwd: "/home/dev/harbor/api"}})
	s.Store.Add(&store.Entry{A: model.Agent{ID: "nw1", Title: "northwind", Status: model.Idle, Tool: "claude", Cwd: "/home/dev/northwind"}})
	s.Store.Project = func(cwd string) (*model.Project, string) {
		switch {
		case strings.HasPrefix(cwd, "/home/dev/harbor"):
			return &model.Project{ID: "p00000001", Name: "Harbor"}, "/home/dev/harbor"
		case strings.HasPrefix(cwd, "/home/dev/northwind"):
			return &model.Project{ID: "p00000002", Name: "Northwind"}, "/home/dev/northwind"
		}
		return nil, ""
	}
}

// Each agent on /v1 carries its project and the folder it matched; an agent
// in no project has neither.
func TestStateProject(t *testing.T) {
	s, _ := newTestServer()
	withProjects(s)
	w := do(s.LocalHandler(), "GET", "/v1/state", "", map[string]string{"Cookie": sessionCookie + "=websession"})
	var st struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || w.Code != 200 {
		t.Fatalf("state: %d %v", w.Code, err)
	}
	got := map[string]map[string]any{}
	for _, a := range st.Agents {
		got[a["id"].(string)] = a
	}
	hb := got["hb1"]
	if p, _ := hb["project"].(map[string]any); p["id"] != "p00000001" || p["name"] != "Harbor" || hb["projectFolder"] != "/home/dev/harbor" {
		t.Errorf("harbor agent: project %v, projectFolder %v", hb["project"], hb["projectFolder"])
	}
	if _, ok := got["a1"]["project"]; ok {
		t.Errorf("an agent in no project has one: %v", got["a1"])
	}
	if _, ok := got["a1"]["projectFolder"]; ok {
		t.Errorf("an agent in no project has a project folder: %v", got["a1"])
	}
}

// list_agents shows each agent's project and takes a project (name in any
// case, or id) to list only its agents.
func TestMCPListByProject(t *testing.T) {
	s, _, tok := newMCPServer(t)
	withProjects(s)
	h := s.LocalHandler()
	list := func(args map[string]any) []mcpAgentRow {
		t.Helper()
		_, out := rpc(h, "/mcp", tok["looker"], call("list_agents", args))
		text, isErr := toolText(t, out)
		var l struct{ Agents []mcpAgentRow }
		if err := json.Unmarshal([]byte(text), &l); err != nil || isErr {
			t.Fatalf("list_agents %v: %v %s", args, err, text)
		}
		return l.Agents
	}
	all := list(nil)
	if len(all) != 6 {
		t.Fatalf("every agent: %d", len(all))
	}
	for _, r := range all {
		want := map[string]string{"hb1": "Harbor", "nw1": "Northwind"}[r.ID]
		if r.Project != want {
			t.Errorf("%s: project %q, want %q", r.ID, r.Project, want)
		}
	}
	for _, by := range []string{"harbor", "HARBOR", "p00000001", " Harbor "} {
		if got := list(map[string]any{"project": by}); len(got) != 1 || got[0].ID != "hb1" {
			t.Errorf("project %q: %+v", by, got)
		}
	}
	if got := list(map[string]any{"project": "atlas"}); len(got) != 0 {
		t.Errorf("an unknown project: %+v", got)
	}
	if got := list(map[string]any{"project": ""}); len(got) != 6 {
		t.Errorf("an empty project filter: %d agents", len(got))
	}
}

// liveCtl adds sessions started elsewhere (GET /v1/live) to the MCP test's
// controller.
type liveCtl struct {
	*mcpCtl
	rows []map[string]any
}

func (l liveCtl) LiveJSON() any             { return l.rows }
func (l liveCtl) AddLive(string, int) error { return nil }

// list_sessions takes a project too: only the sessions working in its
// folders.
func TestMCPSessionsByProject(t *testing.T) {
	s, ctl, tok := newMCPServer(t)
	withProjects(s)
	s.Ctl = liveCtl{mcpCtl: ctl, rows: []map[string]any{
		{"sessionId": "s1", "cwd": "/home/dev/harbor/site"},
		{"sessionId": "s2", "cwd": "/home/dev/northwind"},
		{"sessionId": "s3", "cwd": "/home/dev/kestrel"},
		{"sessionId": "s4"},
	}}
	h := s.LocalHandler()
	list := func(args map[string]any) []string {
		t.Helper()
		_, out := rpc(h, "/mcp", tok["chief"], call("list_sessions", args))
		text, isErr := toolText(t, out)
		var l struct{ Sessions []struct{ SessionID string } }
		if err := json.Unmarshal([]byte(text), &l); err != nil || isErr {
			t.Fatalf("list_sessions %v: %v %s", args, err, text)
		}
		var ids []string
		for _, x := range l.Sessions {
			ids = append(ids, x.SessionID)
		}
		return ids
	}
	if got := list(nil); len(got) != 4 {
		t.Errorf("every session: %v", got)
	}
	for _, by := range []string{"harbor", "p00000001"} {
		if got := list(map[string]any{"project": by}); len(got) != 1 || got[0] != "s1" {
			t.Errorf("project %q: %v", by, got)
		}
	}
	if got := list(map[string]any{"project": "atlas"}); len(got) != 0 {
		t.Errorf("an unknown project: %v", got)
	}
}
