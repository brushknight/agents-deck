package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An agent's project: the folder itself or anything inside it, by whole
// path segments, ~/ expanded, the deepest folder winning and the first
// project winning a folder two of them list.
func TestMatchFolder(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder")
	}
	in := func(p ...string) string { return filepath.Join(append([]string{home}, p...)...) }
	sep := string(filepath.Separator)
	list := []Project{
		{ID: "p1", Name: "harbor-ios", Folders: []string{"~/dev/app/ios"}}, // before its parent: the order doesn't decide depth
		{ID: "p2", Name: "harbor", Folders: []string{"~/dev/app", in("dev", "app-web") + "/"}},
		{ID: "p3", Name: "northwind", Folders: []string{in("srv", "northwind")}},
		{ID: "p4", Name: "late", Folders: []string{in("srv", "northwind")}}, // the same folder again (an older file): p3 wins
		{ID: "p5", Name: "nowhere", Folders: []string{"relative/path", ""}},
	}
	idx := indexFolders(list)
	for _, c := range []struct{ cwd, id, folder string }{
		{in("dev", "app"), "p2", in("dev", "app")},                                     // exact
		{in("dev", "app") + "/", "p2", in("dev", "app")},                               // a trailing slash
		{"~/dev/app", "p2", in("dev", "app")},                                          // ~ in the working folder too
		{in("dev", "app", "server", "cmd"), "p2", in("dev", "app")},                    // nested
		{in("dev", "app", "ios"), "p1", in("dev", "app", "ios")},                       // the deepest wins
		{in("dev", "app", "ios", "Sources", "App"), "p1", in("dev", "app", "ios")},     // the deepest wins, nested
		{in("dev", "app", ".claude", "worktrees", "agent-a1"), "p2", in("dev", "app")}, // a worktree inside the folder
		{in("dev", "app-web", "src"), "p2", in("dev", "app-web")},                      // the project's second folder
		{in("dev", "app2"), "", ""},                                                    // not inside: a segment boundary
		{in("dev", "ap"), "", ""},                                                      // a prefix of a folder
		{in("dev"), "", ""},                                                            // above every folder
		{in("srv", "northwind", "docs"), "p3", in("srv", "northwind")},                 // an exact tie: the first in the list
		{in("srv", "northwind") + sep + ".." + sep + "harbor", "", ""},                 // cleaned before it is compared
		{"relative/path", "", ""},                                                      // not a full path
		{"", "", ""},                                                                   // no folder
	} {
		m := matchFolder(idx, c.cwd)
		id := ""
		if m.ref != nil {
			id = m.ref.ID
		}
		if id != c.id || m.dir != c.folder {
			t.Errorf("%s: project %q in %q, want %q in %q", c.cwd, id, m.dir, c.id, c.folder)
		}
	}
	if !inside(sep+"srv", sep) || inside(sep+"srva", sep+"srv") || !inside(sep+"srv", sep+"srv") {
		t.Error("inside: the root, a segment boundary or the folder itself")
	}
}

// The service answers ProjectOf from memory and works it out again when the
// projects change; the screens hear of the change.
func TestProjectOf(t *testing.T) {
	r := newRig(t)
	app := t.TempDir()
	ios := filepath.Join(app, "ios")
	if err := os.Mkdir(ios, 0o700); err != nil {
		t.Fatal(err)
	}
	if p, f := r.a.ProjectOf(filepath.Join(app, "src")); p != nil || f != "" {
		t.Fatalf("no projects yet: %v %q", p, f)
	}
	code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "harbor", "folder": app}))
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	p, f := r.a.ProjectOf(filepath.Join(ios, "src"))
	if p == nil || p.Name != "harbor" || p.ID != out["id"] || f != filepath.Clean(app) {
		t.Fatalf("after add: %v %q", p, f)
	}
	p.Name = "changed" // a copy: the next answer is the same
	if p, _ := r.a.ProjectOf(filepath.Join(ios, "src")); p.Name != "harbor" {
		t.Errorf("the answer was shared: %v", p)
	}

	ch, stop := r.a.Store.Subscribe()
	defer stop()
	// A project for a folder inside: it wins there, and only there.
	if code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "harbor-ios", "folder": ios})); code != 200 {
		t.Fatalf("a nested folder: %d %v", code, out)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Error("the screens didn't hear of the new project")
	}
	if p, f := r.a.ProjectOf(filepath.Join(ios, "src")); p == nil || p.Name != "harbor-ios" || f != filepath.Clean(ios) {
		t.Errorf("deepest: %v %q", p, f)
	}
	if p, _ := r.a.ProjectOf(filepath.Join(app, "web")); p == nil || p.Name != "harbor" {
		t.Errorf("beside the nested folder: %v", p)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/projects/harbor-ios/remove", ""); code != 204 {
		t.Fatalf("remove: %d", code)
	}
	if p, _ := r.a.ProjectOf(filepath.Join(ios, "src")); p == nil || p.Name != "harbor" {
		t.Errorf("after remove: %v", p)
	}
}

// A folder belongs to one project: saving a project with a folder another
// lists already is refused, naming it. A folder inside another project's is
// fine.
func TestProjectFolderOwner(t *testing.T) {
	r := newRig(t)
	app, web := t.TempDir(), t.TempDir()
	ios := filepath.Join(app, "ios")
	if err := os.Mkdir(ios, 0o700); err != nil {
		t.Fatal(err)
	}
	if code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "harbor", "folders": []string{app, web}})); code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	for _, folders := range [][]string{{web}, {ios, app + "/"}, {filepath.Join(app, "ios", "..")}} {
		code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "northwind", "folders": folders}))
		if msg, _ := out["error"].(string); code != 409 || !strings.Contains(msg, "harbor") {
			t.Errorf("%v: %d %v", folders, code, out)
		}
	}
	if code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "northwind", "folders": []string{ios}})); code != 200 {
		t.Fatalf("a folder inside another project's: %d %v", code, out)
	}
	if code, out := r.do(t, "POST", "/admin/v1/projects/northwind/update", body(map[string]any{"name": "northwind", "folders": []string{ios, web}})); code != 409 || !strings.Contains(out["error"].(string), "harbor") {
		t.Errorf("update onto another project's folder: %d %v", code, out)
	}
	// A project keeps its own folders when it is saved again.
	if code, out := r.do(t, "POST", "/admin/v1/projects/harbor/update", body(map[string]any{"name": "harbour", "folders": []string{web, app}})); code != 200 {
		t.Errorf("save with its own folders: %d %v", code, out)
	}
	// ~/ and the full path are the same folder.
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, web); err == nil && !strings.HasPrefix(rel, "..") {
			if code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "tilde", "folder": "~/" + rel})); code != 409 {
				t.Errorf("a ~/ folder another project has in full: %d %v", code, out)
			}
		}
	}
}

// A projects.json from before one owner per folder, with two projects
// listing the same folder, still loads: both are kept, the first in the list
// wins the folder, and saving either keeps refusing it until one lets go.
func TestProjectsLegacyDuplicates(t *testing.T) {
	r := newRig(t)
	work := t.TempDir()
	old := `{"projects": [{"id": "p00000001", "name": "harbor", "folders": ["` + work + `"]},
	                      {"id": "p00000002", "name": "northwind", "folders": ["` + work + `/"]}]}`
	if err := os.WriteFile(filepath.Join(r.a.DataDir, "projects.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if ps := r.a.Projects(); len(ps) != 2 {
		t.Fatalf("loaded: %+v", ps)
	}
	if p, f := r.a.ProjectOf(filepath.Join(work, "src")); p == nil || p.ID != "p00000001" || f != filepath.Clean(work) {
		t.Errorf("the tie: %v %q, want harbor", p, f)
	}
	code, out := r.do(t, "POST", "/admin/v1/projects/northwind/update", body(map[string]any{"name": "northwind", "folders": []string{work}}))
	if code != 409 || !strings.Contains(out["error"].(string), "harbor") {
		t.Errorf("save the second as it is: %d %v", code, out)
	}
	other := t.TempDir()
	if code, out := r.do(t, "POST", "/admin/v1/projects/northwind/update", body(map[string]any{"name": "northwind", "folders": []string{other}})); code != 200 {
		t.Fatalf("let go of the folder: %d %v", code, out)
	}
	if p, _ := r.a.ProjectOf(filepath.Join(other, "src")); p == nil || p.ID != "p00000002" {
		t.Errorf("after the fix: %v", p)
	}
}
