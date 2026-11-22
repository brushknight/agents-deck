package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/server"
)

// fakeStarter records the agents it was asked to start and the prompts sent.
type fakeStarter struct {
	mu       sync.Mutex
	created  []server.CreateOpts
	prompted map[string]string
}

func (f *fakeStarter) Create(o server.CreateOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, o)
	return "a1", nil
}
func (f *fakeStarter) WaitReady(id string, timeout time.Duration) error { return nil }
func (f *fakeStarter) Prompt(id, text string, interrupt bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prompted == nil {
		f.prompted = map[string]string{}
	}
	f.prompted[id] = text
	return nil
}

func body(v any) string { b, _ := json.Marshal(v); return string(b) }

// Projects are kept in projects.json in the data folder: added, renamed,
// changed and removed, each checked first.
func TestProjects(t *testing.T) {
	r := newRig(t)
	work := t.TempDir()
	if code, out := r.do(t, "GET", "/admin/v1/projects", ""); code != 200 || len(out["projects"].([]any)) != 0 {
		t.Fatalf("an empty list: %d %v", code, out)
	}
	code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": " checkout ", "folder": work, "tool": "codex", "hat": map[string]string{"shape": "beret", "color": "teal"}}))
	if code != 200 || out["name"] != "checkout" || out["path"] != work {
		t.Fatalf("add: %d %v", code, out)
	}
	// ~/ is kept as written and answered in full.
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, work); err == nil && !strings.HasPrefix(rel, "..") {
			code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "tilde", "folder": "~/" + rel}))
			if code != 200 || out["folder"] != "~/"+rel || out["path"] != work {
				t.Errorf("a ~/ folder: %d %v", code, out)
			}
			r.do(t, "POST", "/admin/v1/projects/tilde/remove", "")
		}
	}
	for _, bad := range []map[string]any{
		{"name": "checkout", "folder": work},                                                     // taken (names are kept apart without case)
		{"name": "CHECKOUT", "folder": work},                                                     // also taken
		{"name": "", "folder": work},                                                             // no name
		{"name": "x", "folder": "relative/path"},                                                 // not a full path
		{"name": "x", "folder": filepath.Join(work, "not-there")},                                // no such folder
		{"name": "x", "folder": work, "tool": "vim"},                                             // no such tool
		{"name": "x", "folder": work, "account": "nobody"},                                       // no such account
		{"name": "x", "folder": work, "hat": map[string]string{"shape": "fez", "color": "teal"}}, // no such hat
		{"name": "a/b", "folder": work},                                                          // a slash in the name
		{"name": strings.Repeat("n", 41), "folder": work},                                        // too long
	} {
		if code, out := r.do(t, "POST", "/admin/v1/projects", body(bad)); code < 400 || out["error"] == "" {
			t.Errorf("%v: %d %v", bad, code, out)
		}
	}
	b, err := os.ReadFile(filepath.Join(r.a.DataDir, "projects.json"))
	if err != nil || !strings.Contains(string(b), `"folder": "`+work+`"`) || strings.Count(string(b), `"name"`) != 1 || strings.Contains(string(b), `"path"`) {
		t.Fatalf("projects.json: %v\n%s", err, b)
	}

	// Renamed, with another tool and no hat.
	if code, out := r.do(t, "POST", "/admin/v1/projects/Checkout/update", body(map[string]any{"name": "shop", "folder": work, "tool": "claude"})); code != 200 || out["name"] != "shop" {
		t.Fatalf("update: %d %v", code, out)
	}
	ps := r.a.Projects()
	if len(ps) != 1 || ps[0].Name != "shop" || ps[0].Tool != "claude" || ps[0].Hat != nil {
		t.Fatalf("after update: %+v", ps)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/checkout/update", body(map[string]any{"name": "x", "folder": work})); code != 404 {
		t.Errorf("update a gone name: %d", code)
	}
	r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "docs", "folder": work}))
	if code, _ := r.do(t, "POST", "/admin/v1/projects/docs/update", body(map[string]any{"name": "shop", "folder": work})); code != 409 {
		t.Errorf("rename onto another project: %d, want 409", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/docs/remove", ""); code != 204 {
		t.Errorf("remove: %d", code)
	}
	if ps := r.a.Projects(); len(ps) != 1 || ps[0].Name != "shop" {
		t.Errorf("after remove: %+v", ps)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/docs/remove", ""); code != 404 {
		t.Errorf("remove twice: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/shop/rename", ""); code != 404 {
		t.Errorf("an unknown action: %d", code)
	}
}

// Starting an agent in a project: its folder, its tool and account (a tool
// picked for this agent wins), the project's name as the title, and the
// first prompt once the agent is ready.
func TestProjectStart(t *testing.T) {
	r := newRig(t)
	work := t.TempDir()
	r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "shop", "folder": work, "tool": "claude"}))
	if code, out := r.do(t, "POST", "/admin/v1/projects/shop/start", body(map[string]any{"prompt": "hi"})); code != 503 || !strings.Contains(out["error"].(string), "can't start") {
		t.Errorf("without a starter: %d %v", code, out)
	}
	if _, out := r.do(t, "GET", "/admin/v1/projects", ""); out["canStart"] != false {
		t.Errorf("canStart without a starter: %v", out["canStart"])
	}
	st := &fakeStarter{}
	r.a.Starter = st
	code, out := r.do(t, "POST", "/admin/v1/projects/shop/start", body(map[string]any{"prompt": "fix the tests"}))
	if code != 202 || out["id"] != "a1" || out["prompted"] != true {
		t.Fatalf("start: %d %v", code, out)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		st.mu.Lock()
		done := st.prompted["a1"] != ""
		st.mu.Unlock()
		if done {
			break
		}
	}
	st.mu.Lock()
	if len(st.created) != 1 || st.created[0].Dir != work || st.created[0].Tool != "claude" || st.created[0].Title != "shop" {
		t.Errorf("created: %+v", st.created)
	}
	if st.prompted["a1"] != "fix the tests" {
		t.Errorf("prompted: %v", st.prompted)
	}
	st.mu.Unlock()
	if code, out := r.do(t, "POST", "/admin/v1/projects/shop/start", body(map[string]any{"tool": "codex"})); code != 202 || out["tool"] != "codex" || out["prompted"] != false {
		t.Errorf("start with codex: %d %v", code, out)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/shop/start", body(map[string]any{"account": "nobody"})); code != 400 {
		t.Errorf("an unknown account: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/shop/start", body(map[string]any{"tool": "vim"})); code != 400 {
		t.Errorf("an unknown tool: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/nope/start", ""); code != 404 {
		t.Errorf("an unknown project: %d", code)
	}
	if _, out := r.do(t, "GET", "/admin/v1/projects", ""); out["canStart"] != true {
		t.Errorf("canStart: %v", out["canStart"])
	}
}

// A project spans several folders (an app, its web front end, its server):
// repeats are dropped, each must exist, agents start in the first unless
// another of its folders is picked, and the id stays through a rename.
func TestProjectFolders(t *testing.T) {
	r := newRig(t)
	app, web, api := t.TempDir(), t.TempDir(), t.TempDir()
	code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "shop", "folders": []string{app, " " + web + " ", app, "", api}}))
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	if fs, _ := out["folders"].([]any); len(fs) != 3 || fs[0] != app || fs[1] != web || fs[2] != api || out["folder"] != app || out["path"] != app {
		t.Fatalf("folders: %v", out)
	}
	id, _ := out["id"].(string)
	if len(id) != 9 || id[0] != 'p' {
		t.Fatalf("id: %q", id)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "x", "folders": []string{app, filepath.Join(app, "gone")}})); code != 400 {
		t.Errorf("a folder that isn't there: %d", code)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = t.TempDir()
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "x", "folders": many})); code != 400 {
		t.Errorf("21 folders: %d", code)
	}
	// Renamed by its id, with one folder fewer: the same id.
	code, out = r.do(t, "POST", "/admin/v1/projects/"+id+"/update", body(map[string]any{"name": "store", "folders": []string{web, api}, "id": "pforged00"}))
	if code != 200 || out["id"] != id || out["folder"] != web {
		t.Fatalf("update: %d %v", code, out)
	}

	st := &fakeStarter{}
	r.a.Starter = st
	if code, out := r.do(t, "POST", "/admin/v1/projects/store/start", ""); code != 202 || out["folder"] != web {
		t.Errorf("start: %d %v", code, out)
	}
	if code, out := r.do(t, "POST", "/admin/v1/projects/"+id+"/start", body(map[string]any{"folder": api + "/"})); code != 202 || out["folder"] != api {
		t.Errorf("start in another folder: %d %v", code, out)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/store/start", body(map[string]any{"folder": app})); code != 400 {
		t.Errorf("start in a folder that isn't the project's: %d", code)
	}
	st.mu.Lock()
	if len(st.created) != 2 || st.created[0].Dir != web || st.created[1].Dir != api {
		t.Errorf("created: %+v", st.created)
	}
	st.mu.Unlock()
}

// projects.json from an older version (one folder, no id) reads as a
// project with that folder, and gets an id that it keeps.
func TestProjectsOlderFile(t *testing.T) {
	r := newRig(t)
	work := t.TempDir()
	old := `{"projects": [{"name": "shop", "folder": "` + work + `", "tool": "codex"}]}`
	if err := os.WriteFile(filepath.Join(r.a.DataDir, "projects.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	ps := r.a.Projects()
	if len(ps) != 1 || len(ps[0].Folders) != 1 || ps[0].Folders[0] != work || ps[0].ID == "" || ps[0].Tool != "codex" {
		t.Fatalf("read: %+v", ps)
	}
	if again := r.a.Projects(); again[0].ID != ps[0].ID {
		t.Errorf("the id changed: %s, then %s", ps[0].ID, again[0].ID)
	}
	// Still readable by the older version: "folder" stays.
	b, _ := os.ReadFile(filepath.Join(r.a.DataDir, "projects.json"))
	if !strings.Contains(string(b), `"folder": "`+work+`"`) {
		t.Errorf("projects.json: %s", b)
	}
}
