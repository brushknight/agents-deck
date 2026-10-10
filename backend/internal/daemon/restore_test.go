package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// A tmux server that dies under live agents (crash, reboot) leaves them lost;
// restore brings them back in their own slots.
func TestLostWithTmuxAndRestore(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	home := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", home)
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-r%d", os.Getpid())
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
	st.Place(b, 9) // restore must keep board positions
	sim := store.NewID()
	st.Add(&store.Entry{TmuxName: "a-" + sim, Sim: true, A: model.Agent{ID: sim, Tool: "claude", Status: model.Idle, Cwd: dir}})
	if !tmux.ServerRunning() {
		t.Fatal("tmux server should be up")
	}

	// The server dies; the daemon starts again (the reboot case).
	exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run()
	d.Adopt()
	for _, id := range []string{a, b} {
		if e, _ := st.Get(id); !e.A.Lost || e.A.Status != model.Exited {
			t.Fatalf("%s should be lost: %+v", id, e.A)
		}
	}
	if e, _ := st.Get(sim); e.A.Lost {
		t.Fatal("simulated agents are never lost")
	}

	// Lost agents don't expire with the exited TTL, and restore relaunches them.
	res := d.Restore()
	if len(res) != 2 {
		t.Fatalf("results: %+v", res)
	}
	for _, r := range res {
		if r.Action != "relaunched" || r.Error != "" {
			t.Fatalf("result: %+v", r)
		}
	}
	time.Sleep(300 * time.Millisecond)
	live := tmux.Sessions()
	for _, id := range []string{a, b} {
		e, _ := st.Get(id)
		if e.A.Lost || !live[e.TmuxName] {
			t.Fatalf("%s should be back: lost=%v live=%v", id, e.A.Lost, live[e.TmuxName])
		}
	}
	if e, _ := st.Get(b); e.A.Slot != 9 {
		t.Fatalf("slot = %d, want 9", e.A.Slot)
	}
	if again := d.Restore(); len(again) != 0 {
		t.Fatalf("nothing left to restore: %+v", again)
	}
}
