package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// fakeBook records what the project tools pass on.
type fakeBook struct {
	list    []ProjectInfo
	created []ProjectInfo
	changed map[string]ProjectChange
	removed []string
	started []ProjectStartOpts
}

func (f *fakeBook) ListProjects() []ProjectInfo { return append([]ProjectInfo(nil), f.list...) }
func (f *fakeBook) CreateProject(p ProjectInfo) (ProjectInfo, error) {
	if len(p.Folders) == 0 {
		return p, errors.New("give the project's folder")
	}
	f.created = append(f.created, p)
	p.ID = "p00000001"
	return p, nil
}
func (f *fakeBook) ChangeProject(project string, c ProjectChange) (ProjectInfo, error) {
	if f.changed == nil {
		f.changed = map[string]ProjectChange{}
	}
	f.changed[project] = c
	return ProjectInfo{ID: "p0a1b2c3d", Name: project}, nil
}
func (f *fakeBook) StartInProject(project string, o ProjectStartOpts) (ProjectStartedAgent, error) {
	if project != "harbor" {
		return ProjectStartedAgent{}, errors.New("no project called " + project)
	}
	f.started = append(f.started, o)
	return ProjectStartedAgent{ID: "new1", Project: "harbor", Folder: "/work/harbor", Tool: "codex"}, nil
}
func (f *fakeBook) RemoveProject(project string) error {
	f.removed = append(f.removed, project)
	return nil
}

// The project tools pass their arguments to the project service and
// answer with what it says; changing needs scope admin, listing doesn't.
func TestMCPProjectTools(t *testing.T) {
	s, _, tok := newMCPServer(t)
	h := s.LocalHandler()

	_, out := rpc(h, "/mcp", tok["looker"], call("list_projects", nil))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "not available") {
		t.Errorf("no project service: %v %s", isErr, text)
	}

	book := &fakeBook{list: []ProjectInfo{{ID: "p0a1b2c3d", Name: "harbor", Folders: []string{"/work/harbor"}}, {ID: "p11111111", Name: "quay", Folders: []string{"/work/quay"}}}}
	s.Projects = book
	s.Store.Add(&store.Entry{A: model.Agent{ID: "h1", Title: "h1", Status: model.Idle, Tool: "claude", Cwd: "/work/harbor"}})
	s.Store.Project = func(cwd string) (*model.Project, string) {
		if strings.HasPrefix(cwd, "/work/harbor") {
			return &model.Project{ID: "p0a1b2c3d", Name: "harbor"}, "/work/harbor"
		}
		return nil, ""
	}
	_, out = rpc(h, "/mcp", tok["looker"], call("list_projects", nil))
	text, isErr := toolText(t, out)
	var list struct{ Projects []ProjectInfo }
	if err := json.Unmarshal([]byte(text), &list); err != nil || isErr || len(list.Projects) != 2 || list.Projects[0].Agents != 1 || list.Projects[1].Agents != 0 {
		t.Fatalf("list_projects: %v %s", err, text)
	}

	for _, name := range []string{"create_project", "update_project", "remove_project"} {
		_, out = rpc(h, "/mcp", tok["doer"], call(name, map[string]any{"project": "harbor", "name": "x", "folders": []string{"/x"}}))
		if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "needs admin") {
			t.Errorf("%s with scope act: %s", name, text)
		}
	}
	if len(book.created)+len(book.changed)+len(book.removed) != 0 {
		t.Fatal("a refused call reached the project service")
	}

	_, out = rpc(h, "/mcp", tok["chief"], call("create_project", map[string]any{"name": "dock", "folders": []any{"/work/dock", "/work/dock-web"}, "tool": "codex"}))
	if text, isErr := toolText(t, out); isErr || len(book.created) != 1 || len(book.created[0].Folders) != 2 || book.created[0].Tool != "codex" || !strings.Contains(text, "p00000001") {
		t.Errorf("create_project: %s %+v", text, book.created)
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("create_project", map[string]any{"name": "dock"}))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "folder") {
		t.Errorf("the service's refusal is the answer: %s", text)
	}

	_, out = rpc(h, "/mcp", tok["chief"], call("update_project", map[string]any{"project": "harbor", "add_folders": "/work/harbor-api", "account": ""}))
	c := book.changed["harbor"]
	if _, isErr := toolText(t, out); isErr || c.Name != nil || c.Tool != nil || c.Account == nil || *c.Account != "" || len(c.AddFolders) != 1 || c.AddFolders[0] != "/work/harbor-api" {
		t.Errorf("update_project: %+v", c)
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("update_project", map[string]any{"project": "harbor", "name": " "}))
	if _, isErr := toolText(t, out); !isErr {
		t.Error("an empty new name is refused")
	}

	_, out = rpc(h, "/mcp", tok["chief"], call("remove_project", map[string]any{"project": "quay"}))
	if _, isErr := toolText(t, out); isErr || len(book.removed) != 1 || book.removed[0] != "quay" {
		t.Errorf("remove_project: %v", book.removed)
	}
}

// start_agent with a project starts through the project service (its folder,
// tool and account); without one, in the folder as before.
func TestMCPStartInProject(t *testing.T) {
	s, ctl, tok := newMCPServer(t)
	h := s.LocalHandler()
	book := &fakeBook{}
	s.Projects = book

	_, out := rpc(h, "/mcp", tok["chief"], call("start_agent", map[string]any{}))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "project") {
		t.Errorf("neither project nor folder: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("start_agent", map[string]any{"project": "harbor", "folder": "/work/harbor-api", "title": "api"}))
	text, isErr := toolText(t, out)
	if isErr || !strings.Contains(text, "new1") || len(book.started) != 1 {
		t.Fatalf("start in a project: %s", text)
	}
	if o := book.started[0]; o.Folder != "/work/harbor-api" || o.Title != "api" || o.Tool != "" {
		t.Errorf("passed on: %+v", o)
	}
	if len(ctl.started) != 0 {
		t.Error("a project start also created an agent by folder")
	}
	_, out = rpc(h, "/mcp", tok["chief"], call("start_agent", map[string]any{"project": "nope"}))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "no project called nope") {
		t.Errorf("unknown project: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("start_agent", map[string]any{"project": "harbor"}))
	if _, isErr := toolText(t, out); !isErr || len(book.started) != 1 {
		t.Error("scope act started an agent in a project")
	}
}
