package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

func fastPrompts(t *testing.T) {
	a, b := afterInterrupt, beforeEnter
	afterInterrupt, beforeEnter = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { afterInterrupt, beforeEnter = a, b })
}

// A prompt reaches the pane as one bracketed paste (its newline inside it),
// then Enter; the status rules decide first.
func TestPromptIntoTmux(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	fastPrompts(t)
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-p%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })
	d := &Daemon{Store: store.New("", "test", "0"), Agentctl: "/bin/true"}

	keys := filepath.Join(t.TempDir(), "keys")
	id := store.NewID()
	e := &store.Entry{TmuxName: "a-" + id, A: model.Agent{ID: id, Tool: "claude", Status: model.Idle, Cwd: t.TempDir()}}
	d.Store.Add(e)
	// Like Claude Code: asks for bracketed paste, reads raw keys.
	script := `printf '\033[?2004h'; stty raw -echo; exec dd bs=1 of=` + keys + ` 2>/dev/null`
	if err := tmux.NewSession(e.TmuxName, e.A.Cwd, nil, []string{"/bin/sh", "-c", script}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	if err := d.Prompt(id, "fix the tests\nthen commit", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	got, _ := os.ReadFile(keys)
	if want := "\x1b[200~fix the tests\nthen commit\x1b[201~\r"; string(got) != want {
		t.Fatalf("typed %q, want %q", got, want)
	}

	set := func(s model.Status) {
		d.Store.Update(id, func(e *store.Entry) bool { e.SetStatus(s); return true })
	}
	for status, want := range map[model.Status]error{model.Running: server.ErrBusy, model.Waiting: server.ErrAsking,
		model.Starting: server.ErrStarting, model.Exited: server.ErrExited} {
		set(status)
		if err := d.Prompt(id, "x", false); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", status, err, want)
		}
	}
	// With interrupt, a working agent gets Esc first.
	set(model.Running)
	if err := d.Prompt(id, "instead", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	got, _ = os.ReadFile(keys)
	if !strings.HasSuffix(string(got), "\x1binstead\r") && !strings.HasSuffix(string(got), "\x1b\x1b[200~instead\x1b[201~\r") {
		t.Fatalf("interrupt then prompt: %q", got)
	}
	if err := d.Prompt("nope", "x", false); !errors.Is(err, server.ErrNotFound) {
		t.Errorf("unknown agent: %v", err)
	}
}

// Sessions in another terminal are read-only; agents in an app get the
// prompt through the app, Enter on its own.
func TestPromptReadOnlyAndHarness(t *testing.T) {
	fastPrompts(t)
	// A tmux server of its own, so nothing here can type into a real one.
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-r%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })
	d := &Daemon{Store: store.New("", "test", "0")}
	d.Store.Add(&store.Entry{External: true, A: model.Agent{ID: "live1", Tool: "claude", Status: model.Idle, External: true}})
	d.Store.Add(&store.Entry{External: true, A: model.Agent{ID: "cdx1", Tool: "codex", Status: model.Idle, External: true}})
	d.Store.Add(&store.Entry{A: model.Agent{ID: "noterm", Tool: "shell", Status: model.Idle}}) // no tmux session name
	for _, id := range []string{"live1", "cdx1", "noterm"} {
		if err := d.Prompt(id, "x", false); !errors.Is(err, server.ErrReadOnly) {
			t.Errorf("%s: %v", id, err)
		}
	}

	f := &fakeHerdr{}
	d.herdr.dial = func(string) herdrAPI { return f }
	d.Store.Add(&store.Entry{HerdrPane: "w1:p2", A: model.Agent{ID: "h1", Tool: "claude", Status: model.Idle, External: true, App: "herdr"}})
	if err := d.Prompt("h1", "one\ntwo", false); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.sent[0] != "w1:p2 \x1b[200~one\ntwo\x1b[201~" || len(f.keys) != 1 || f.keys[0] != "enter" {
		t.Fatalf("herdr got %q %q", f.sent, f.keys)
	}
	d.Store.Update("h1", func(e *store.Entry) bool { e.SetStatus(model.Running); return true })
	if err := d.Prompt("h1", "x", false); !errors.Is(err, server.ErrBusy) {
		t.Errorf("working herdr agent: %v", err)
	}
	if err := d.Prompt("h1", "x", true); err != nil || f.keys[1] != "esc" || f.sent[1] != "w1:p2 x" {
		t.Errorf("interrupt in herdr: %v %q %q", err, f.sent, f.keys)
	}
}
