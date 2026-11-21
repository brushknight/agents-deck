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
	if code != 200 || out["name"] != "checkout" {
		t.Fatalf("add: %d %v", code, out)
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
	if err != nil || !strings.Contains(string(b), `"folder": "`+work+`"`) || strings.Count(string(b), `"name"`) != 1 {
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
