package admin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
)

// No test looks at the real computer's programs, apps or login shell.
func init() {
	empty, _ := os.MkdirTemp("", "admin-test-tools")
	newProbe = func() probe { return fakeProbe(empty) }
}

// fakeProbe is a computer whose home, programs and apps are all under root.
func fakeProbe(root string) probe {
	bin := filepath.Join(root, "bin")
	return probe{
		home: filepath.Join(root, "home"), goos: "darwin",
		lookPath: func(name string) (string, error) {
			if f := filepath.Join(bin, name); isProgram(f) {
				return f, nil
			}
			return "", exec.ErrNotFound
		},
		loginPath: func(string) string { return "" },
		binDirs:   []string{filepath.Join(root, "usr-local-bin")},
		appDirs:   []string{filepath.Join(root, "Applications")},
		orcaBin: func() string {
			if f := filepath.Join(bin, "orca"); isProgram(f) {
				return f
			}
			return ""
		},
		herdrSock: func() []string { return nil },
		env:       func(string) string { return "" },
	}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func program(t *testing.T, dir, name string) {
	t.Helper()
	mkdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func rowOf(rows []Tool, id string) Tool {
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	return Tool{}
}

// Nothing on the computer: every tool says it isn't here and where it looked.
func TestToolsNothingFound(t *testing.T) {
	root := t.TempDir()
	rows := toolRows(fakeProbe(root), map[string]any{}, map[string]int{}, false)
	if len(rows) != len(tools) {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if r.Found || r.State != "missing" || !strings.HasPrefix(r.Detail, "looked for") {
			t.Errorf("%s: %+v", r.ID, r)
		}
		if strings.Contains(r.Detail, root) {
			t.Errorf("%s: detail names the full home path: %s", r.ID, r.Detail)
		}
	}
}

// The owner's case: no codex on PATH, but ~/.codex with sessions, which is
// what the deck's Codex watcher reads. That is Codex, found.
func TestToolsCodexFromItsDataFolder(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, p.home, ".codex", "sessions")
	r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "codex")
	if !r.Found || r.State != "found" || r.Detail != "~/.codex" {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.Note, "last 6 hours") {
		t.Errorf("note: %q", r.Note)
	}
	// With its sessions on the deck it is connected, and the note goes.
	r = rowOf(toolRows(p, map[string]any{"codexWindowHours": 2.0}, map[string]int{"codex": 2}, false), "codex")
	if r.State != "connected" || r.Note != "" {
		t.Fatalf("%+v", r)
	}
	// The window from config.json.
	r = rowOf(toolRows(p, map[string]any{"codexWindowHours": 2.0}, map[string]int{}, false), "codex")
	if !strings.Contains(r.Note, "last 2 hours") {
		t.Errorf("note: %q", r.Note)
	}
}

// The Codex app or its CLI without a data folder yet: found, and the note
// says why nothing shows.
func TestToolsCodexAppOrCommand(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, root, "Applications", "Codex.app")
	r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "codex")
	if !r.Found || r.State != "found" || !strings.HasSuffix(r.Detail, "Codex.app") || !strings.Contains(r.Note, "hasn't saved a session") {
		t.Fatalf("%+v", r)
	}
	// The app counts on macOS only.
	p.goos = "linux"
	if r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "codex"); r.Found {
		t.Fatalf("linux: %+v", r)
	}
	// A codex program in one of the usual folders a service's PATH misses.
	program(t, filepath.Join(root, "usr-local-bin"), "codex")
	r = rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "codex")
	if !r.Found || !strings.Contains(r.Detail, filepath.Join("usr-local-bin", "codex")) {
		t.Fatalf("%+v", r)
	}
	// CODEX_HOME moves the data folder, as it does for the watcher.
	custom := mkdir(t, root, "elsewhere")
	p.env = func(k string) string {
		if k == "CODEX_HOME" {
			return custom
		}
		return ""
	}
	r = rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "codex")
	if !strings.Contains(r.Detail, custom) || strings.Contains(r.Note, "hasn't saved") {
		t.Fatalf("%+v", r)
	}
}

// The login shell finds what the service's PATH doesn't.
func TestToolsLoginPath(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	p.loginPath = func(name string) string {
		if name == "claude" {
			return "/opt/example/bin/claude"
		}
		return ""
	}
	r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "claude")
	if !r.Found || !strings.Contains(r.Detail, "/opt/example/bin/claude") {
		t.Fatalf("%+v", r)
	}
}

// Claude Code: its data folder without the command is found, with a note
// that sessions can't be listed.
func TestToolsClaude(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, p.home, ".claude")
	r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "claude")
	if !r.Found || r.Detail != "~/.claude" || !strings.Contains(r.Note, "isn't on your PATH") {
		t.Fatalf("%+v", r)
	}
	program(t, filepath.Join(root, "bin"), "claude")
	r = rowOf(toolRows(p, map[string]any{}, map[string]int{"claude": 3}, false), "claude")
	if r.State != "connected" || r.Agents != 3 || r.Note != "" {
		t.Fatalf("%+v", r)
	}
}

// Orca is read through its orca command; the app alone is found but says so.
func TestToolsOrcaNeedsItsCommand(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, root, "Applications", "Orca.app")
	r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "orca")
	if !r.Found || !strings.Contains(r.Note, "orca command") {
		t.Fatalf("%+v", r)
	}
	program(t, filepath.Join(root, "bin"), "orca")
	r = rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "orca")
	if !r.Found || strings.Contains(r.Note, "orca command") {
		t.Fatalf("%+v", r)
	}
}

// herdr and cmux: their folders count; herdr says whether it runs.
func TestToolsHerdrAndCmux(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, p.home, ".config", "herdr")
	mkdir(t, p.home, ".cmuxterm")
	rows := toolRows(p, map[string]any{}, map[string]int{}, false)
	if r := rowOf(rows, "herdr"); !r.Found || !strings.Contains(r.Note, "isn't running") {
		t.Fatalf("%+v", r)
	}
	if r := rowOf(rows, "cmux"); !r.Found || r.Detail != "~/.cmuxterm" {
		t.Fatalf("%+v", r)
	}
	p.herdrSock = func() []string { return []string{"/tmp/example.sock"} }
	if r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "herdr"); r.Note != "" {
		t.Fatalf("%+v", r)
	}
}

// Turned off in config.json wins over everything, and says how.
func TestToolsTurnedOff(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	mkdir(t, p.home, ".codex")
	r := rowOf(toolRows(p, map[string]any{"codex": false}, map[string]int{}, false), "codex")
	if r.On || r.State != "off" || !strings.Contains(r.Note, `"codex": false`) {
		t.Fatalf("%+v", r)
	}
}

// tmux: needed for agentctl new.
func TestToolsTmux(t *testing.T) {
	root := t.TempDir()
	p := fakeProbe(root)
	if r := rowOf(toolRows(p, map[string]any{}, map[string]int{}, false), "tmux"); r.Found || !strings.Contains(r.Note, "agentctl new") {
		t.Fatalf("%+v", r)
	}
	program(t, filepath.Join(root, "bin"), "tmux")
	if r := rowOf(toolRows(p, map[string]any{}, map[string]int{"tmux": 4}, false), "tmux"); r.State != "connected" || r.Agents != 4 {
		t.Fatalf("%+v", r)
	}
}

// Start here: the account new agents start in, kept in config.json "account"
// next to the account list (which stays as it was).
func TestAccountDefault(t *testing.T) {
	r := newRig(t)
	home := t.TempDir()
	if err := accounts.Save([]accounts.Account{
		{Name: "work", Dir: filepath.Join(home, "work"), Color: "teal"},
		{Name: "side", Dir: filepath.Join(home, "side"), Color: "pink"},
	}, "work"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Save(nil, "") })
	if code, _ := r.do(t, "POST", "/admin/v1/accounts/side/default", ""); code != 204 {
		t.Fatalf("default: %d", code)
	}
	if d := accounts.Default(); d.Name != "side" {
		t.Errorf("new agents start in %q, want side", d.Name)
	}
	if n := len(accounts.Configured()); n != 2 {
		t.Errorf("%d accounts after, want 2", n)
	}
	_, out := r.do(t, "GET", "/admin/v1/agents", "")
	for _, a := range out["accounts"].([]any) {
		m := a.(map[string]any)
		if m["default"] != (m["name"] == "side") {
			t.Errorf("GET /admin/v1/agents: %v", m)
		}
	}
	if code, _ := r.do(t, "POST", "/admin/v1/accounts/nobody/default", ""); code != 404 {
		t.Errorf("an unknown account: %d, want 404", code)
	}
	if d := accounts.Default(); d.Name != "side" {
		t.Errorf("a refused call changed the default to %q", d.Name)
	}
}
