package admin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// mkdirs makes folders under root and returns them in full.
func mkdirs(t *testing.T, root string, rel ...string) []string {
	t.Helper()
	var out []string
	for _, r := range rel {
		dir := filepath.Join(root, filepath.FromSlash(r))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		out = append(out, dir)
	}
	return out
}

// worktree makes dir a git worktree: a .git file pointing into a
// repository's worktrees.
func worktree(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /srv/repo/.git/worktrees/"+filepath.Base(dir)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A project's day: each folder in the project's order, the agents in it now
// (worktrees noted), and the time its agents worked today, those removed
// today included. Agents of other projects, of a nested project and of none
// stay out.
func TestDayOf(t *testing.T) {
	root := t.TempDir()
	d := mkdirs(t, root, "harbor/ios", "harbor/server", "harbor/site", "harbor/ios/.claude/worktrees/fix-login",
		"harbor/server/pkg", "harbor/server/sub", "harbor/ios/watch", "northwind", "elsewhere")
	ios, server, site, wt, pkg, sub, watch, northwind, elsewhere := d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8]
	worktree(t, wt)
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../.git/modules/sub\n"), 0o600); err != nil {
		t.Fatal(err) // a submodule: not a worktree
	}
	harbor := Project{ID: "p1", Name: "harbor", Folders: []string{ios, server, site}}
	nested := Project{ID: "p2", Name: "harbor-watch", Folders: []string{watch}}
	other := Project{ID: "p3", Name: "northwind", Folders: []string{northwind}}
	idx := indexFolders([]Project{harbor, nested, other})
	match := func(cwd string) (*model.Project, string) { m := matchFolder(idx, cwd); return m.ref, m.dir }

	agents := []model.Agent{
		{ID: "a1", Title: "login screen", Cwd: ios, Status: model.Running},
		{ID: "a2", Title: "fix login", Cwd: filepath.Join(wt, "src"), Status: model.Waiting},
		{ID: "a3", Folder: "pkg", Cwd: pkg, Status: model.Idle},
		{ID: "a4", Title: "submodule", Cwd: sub, Status: model.Idle},
		{ID: "a5", Title: "watch app", Cwd: watch, Status: model.Running},
		{ID: "a6", Title: "other project", Cwd: northwind, Status: model.Running},
		{ID: "a7", Title: "no project", Cwd: elsewhere, Status: model.Running},
		{ID: "a8", Title: "no folder", Status: model.Running},
	}
	worked := map[string]time.Duration{"a1": 50 * time.Minute, "a2": 10*time.Minute + 40*time.Second, "a3": 20 * time.Second,
		"a5": time.Hour, "a6": time.Hour, "a7": time.Hour}
	gone := map[string]time.Duration{filepath.Join(site, "docs"): 15 * time.Minute, elsewhere: time.Hour, watch: time.Hour}
	since := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

	day := dayOf(harbor, agents, worked, gone, match, since)
	if day.Project != (model.Project{ID: "p1", Name: "harbor"}) || !day.Since.Equal(since) || len(day.Folders) != 3 {
		t.Fatalf("day: %+v", day)
	}
	f := day.Folders
	if f[0].Folder != ios || f[0].Path != ios || f[0].ActiveMin != 61 || len(f[0].Agents) != 2 {
		t.Errorf("ios: %+v", f[0])
	}
	if a := f[0].Agents; a[0] != (DayAgent{ID: "a1", Name: "login screen", Status: model.Running}) ||
		a[1] != (DayAgent{ID: "a2", Name: "fix login", Status: model.Waiting, Worktree: ".claude/worktrees/fix-login"}) {
		t.Errorf("ios agents: %+v", a)
	}
	if f[1].ActiveMin != 0 || len(f[1].Agents) != 2 || f[1].Agents[0].Name != "pkg" || f[1].Agents[0].Worktree != "" || f[1].Agents[1].Worktree != "" {
		t.Errorf("server (a package and a submodule, under half a minute): %+v", f[1])
	}
	if f[2].ActiveMin != 15 || len(f[2].Agents) != 0 || f[2].Agents == nil {
		t.Errorf("site (an agent removed today): %+v", f[2])
	}

	// A project with no agents: its folders, empty.
	day = dayOf(Project{ID: "p4", Name: "kestrel", Folders: []string{"~/kestrel-not-here", "relative", "~/kestrel-not-here/"}}, agents, worked, gone, match, since)
	if len(day.Folders) != 1 || day.Folders[0].Folder != "~/kestrel-not-here" || day.Folders[0].Path == day.Folders[0].Folder || len(day.Folders[0].Agents) != 0 {
		t.Errorf("no agents: %+v", day)
	}
}

func TestWorktreeOf(t *testing.T) {
	root := t.TempDir()
	d := mkdirs(t, root, "app/wt/a/b", "app/plain")
	worktree(t, filepath.Join(root, "app", "wt"))
	app := filepath.Join(root, "app")
	for cwd, want := range map[string]string{d[0]: "wt", filepath.Join(app, "wt"): "wt", d[1]: "", app: "", root: ""} {
		if got := worktreeOf(cwd, app); got != want {
			t.Errorf("%s: %q, want %q", cwd, got, want)
		}
	}
	worktree(t, app) // the folder itself is a worktree: that's its own checkout
	if got := worktreeOf(app, app); got != "" {
		t.Errorf("the folder itself: %q", got)
	}
}

// GET /admin/v1/projects/{name|id}/today.
func TestProjectToday(t *testing.T) {
	r := newRig(t)
	root := t.TempDir()
	d := mkdirs(t, root, "harbor/ios", "harbor/server", "kestrel")
	code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "harbor", "folders": []string{d[0], d[1]}}))
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	id := out["id"].(string)
	if code, out := r.do(t, "POST", "/admin/v1/projects", body(map[string]any{"name": "kestrel", "folder": d[2]})); code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	r.a.Store.Add(&store.Entry{A: model.Agent{ID: "a1", Title: "login screen", Cwd: d[0]}})
	r.a.Store.Update("a1", func(e *store.Entry) bool { e.SetStatus(model.Running); return true })
	r.a.Store.Add(&store.Entry{A: model.Agent{ID: "a2", Title: "elsewhere", Cwd: root}})

	for _, key := range []string{"harbor", "HARBOR", id} {
		code, out := r.do(t, "GET", "/admin/v1/projects/"+key+"/today", "")
		if code != 200 {
			t.Fatalf("%s: %d %v", key, code, out)
		}
		p := out["project"].(map[string]any)
		fs := out["folders"].([]any)
		if p["id"] != id || p["name"] != "harbor" || out["since"] == nil || len(fs) != 2 {
			t.Fatalf("%s: %v", key, out)
		}
		ios, server := fs[0].(map[string]any), fs[1].(map[string]any)
		agents := ios["agents"].([]any)
		if ios["path"] != d[0] || len(agents) != 1 || agents[0].(map[string]any)["name"] != "login screen" ||
			agents[0].(map[string]any)["status"] != "running" || ios["activeMin"] != 0.0 {
			t.Errorf("%s ios: %v", key, ios)
		}
		if len(server["agents"].([]any)) != 0 || server["activeMin"] != 0.0 {
			t.Errorf("%s server: %v", key, server)
		}
	}
	// A project with no agents.
	if code, out := r.do(t, "GET", "/admin/v1/projects/kestrel/today", ""); code != 200 || len(out["folders"].([]any)) != 1 ||
		len(out["folders"].([]any)[0].(map[string]any)["agents"].([]any)) != 0 {
		t.Errorf("kestrel: %d %v", code, out)
	}
	if code, out := r.do(t, "GET", "/admin/v1/projects/northwind/today", ""); code != 404 || out["error"] == "" {
		t.Errorf("an unknown project: %d %v", code, out)
	}
}
