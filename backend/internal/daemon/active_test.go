package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/herdr"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// Restore with ids brings back only those, keeps what they last did, and
// says why for an id it can't restore.
func TestRestoreSome(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	home := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", home)
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-s%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })

	st := store.New(filepath.Join(home, "agents.json"), "test", "0")
	d := &Daemon{Store: st, Agentctl: "/bin/true"}
	dir := t.TempDir()
	a, err := d.Create(server.CreateOpts{Dir: dir, Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.Create(server.CreateOpts{Dir: dir, Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if act := tmux.SessionActivity(); act["a-"+a].IsZero() {
		t.Fatalf("tmux gives no activity time: %v", act)
	}
	was := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	st.Update(a, func(e *store.Entry) bool { return e.MarkActive(was) })

	exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run()
	d.Adopt()
	res := d.Restore(a, "nosuch")
	if len(res) != 2 {
		t.Fatalf("results: %+v", res)
	}
	byID := map[string]RestoreResult{}
	for _, r := range res {
		byID[r.ID] = r
	}
	if r := byID[a]; r.Action != "relaunched" || r.Error != "" {
		t.Fatalf("restore %s: %+v", a, r)
	}
	if r := byID["nosuch"]; r.Action != "failed" || r.Error != "no such agent" {
		t.Fatalf("unknown id: %+v", r)
	}
	if e, _ := st.Get(b); !e.A.Lost {
		t.Fatal("an agent not named must stay lost")
	}
	if e, _ := st.Get(a); e.A.Lost || !e.A.LastActive.Equal(was) {
		t.Fatalf("restored agent: lost=%v last active %v (want %v kept)", e.A.Lost, e.A.LastActive, was)
	}
	if r := d.Restore(a); len(r) != 1 || r[0].Error != "not lost with the tmux server" {
		t.Fatalf("restoring a running agent: %+v", r)
	}
}

// Only tools without a transcript take tmux's output time, and not the
// output of starting up.
func TestTmuxActive(t *testing.T) {
	start := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	shell := store.Entry{A: model.Agent{Tool: "shell", StartedAt: start}}
	if tmuxActive(shell, start.Add(2*time.Second)) {
		t.Error("a shell's first prompt is not activity")
	}
	if !tmuxActive(shell, start.Add(time.Minute)) {
		t.Error("later output is")
	}
	if tmuxActive(store.Entry{A: model.Agent{Tool: "claude", StartedAt: start}}, start.Add(time.Hour)) {
		t.Error("Claude agents have a transcript")
	}
	if tmuxActive(shell, time.Time{}) {
		t.Error("no time from tmux")
	}
}

// herdr gives no times: an agent it reports working is active now.
func TestHerdrWorkingStampsActivity(t *testing.T) {
	e := &store.Entry{A: model.Agent{Status: model.Idle}}
	herdrApply(e, herdr.Agent{Status: "idle"}, "", "")
	if !e.A.LastActive.IsZero() {
		t.Fatal("idle on first sight says nothing about when")
	}
	herdrApply(e, herdr.Agent{Status: "working"}, "", "")
	if time.Since(e.A.LastActive) > time.Minute {
		t.Fatalf("working: last active %v", e.A.LastActive)
	}
}
