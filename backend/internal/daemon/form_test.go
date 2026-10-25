package daemon

import (
	"encoding/json"
	"errors"
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

const formAsk = `{"questions":[
 {"question":"Which color?","header":"Color","options":[{"label":"Red"},{"label":"Green"}]},
 {"question":"Which toppings?","header":"Toppings","multiSelect":true,"options":[{"label":"Cheese"},{"label":"Ham"},{"label":"Olives"}]}]}`

// formAgent is an agent whose pane runs sh -c script, waiting on the form.
func formAgent(t *testing.T, d *Daemon, script string) string {
	t.Helper()
	id := store.NewID()
	e := &store.Entry{TmuxName: "a-" + id, A: model.Agent{ID: id, Tool: "claude", Status: model.Running, Cwd: t.TempDir()}}
	d.Store.Add(e)
	if err := tmux.NewSession(e.TmuxName, e.A.Cwd, nil, []string{"/bin/sh", "-c", script}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "AskUserQuestion", "tool_input": json.RawMessage(formAsk)})
	if err := d.Hook(id, body); err != nil {
		t.Fatal(err)
	}
	return id
}

func waiting(t *testing.T, d *Daemon, id string) *model.Prompt {
	t.Helper()
	e, _ := d.Store.Get(id)
	if e.A.Waiting == nil {
		t.Fatalf("not waiting: %+v", e.A)
	}
	return e.A.Waiting
}

// The deck walks a question form: each answer types what Claude Code's tab
// expects (a number, Tab to leave a pick-any tab, 1 to submit) and opens the
// next step.
func TestAnswerWalksAForm(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-f%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })
	d := &Daemon{Store: store.New("", "test", "0"), Agentctl: "/bin/true"}

	log := filepath.Join(t.TempDir(), "keys")
	id := formAgent(t, d, "stty raw -echo; exec dd bs=1 of="+log+" 2>/dev/null")
	time.Sleep(300 * time.Millisecond)
	for _, key := range []string{"2", "1", "3", "3", "1", "4", "1"} {
		p := waiting(t, d, id)
		if err := d.Answer(id, p.ID, key); err != nil {
			t.Fatalf("key %s on %+v: %v", key, p, err)
		}
		if key != "1" || p.Title != "submit these answers?" {
			if np := waiting(t, d, id); np.ID == p.ID {
				t.Fatalf("key %s: the next step needs a new id", key)
			}
		}
	}
	e, _ := d.Store.Get(id)
	if e.A.Status != model.Running || e.Form != nil {
		t.Fatalf("submitted: %+v", e.A)
	}
	if err := d.Answer(id, "p1", "1"); !errors.Is(err, server.ErrStalePrompt) {
		t.Fatalf("an old step: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	b, _ := os.ReadFile(log)
	if got := string(b); got != "21331\t1" {
		t.Fatalf("typed %q", got)
	}
}

// An answer typed in the terminal moves the form on: the deck's older step is
// refused and the prompt follows the screen.
func TestAnswerFollowsAFormAnsweredInTheTerminal(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-g%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })
	d := &Daemon{Store: store.New("", "test", "0"), Agentctl: "/bin/true"}

	screen := `←  ☒ Color  ☐ Toppings  ✔ Submit  →\n\nWhich toppings?\n\n❯ 1. [ ] Cheese\n  2. [✔] Ham\n  3. [ ] Olives\n`
	id := formAgent(t, d, "printf '"+screen+"'; exec sleep 30")
	time.Sleep(400 * time.Millisecond)
	p := waiting(t, d, id)
	if err := d.Answer(id, p.ID, "1"); !errors.Is(err, server.ErrStalePrompt) {
		t.Fatalf("answer to a step already answered: %v", err)
	}
	np := waiting(t, d, id)
	if np.Title != "which toppings?" || np.Options[1].Label != "Ham" || !np.Options[1].Selected || np.Options[0].Selected {
		t.Fatalf("the prompt should follow the screen: %+v", np)
	}
}

// Cancelling a form at the terminal fires no hook: the screen closes the prompt.
func TestFormCancelledInTheTerminal(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-h%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })
	d := &Daemon{Store: store.New("", "test", "0"), Agentctl: "/bin/true"}

	id := formAgent(t, d, `printf '⏺ User declined to answer questions\n'; exec sleep 30`)
	time.Sleep(300 * time.Millisecond)
	var changed bool
	d.Store.Update(id, func(e *store.Entry) bool {
		changed = d.askOnScreen(e)
		return changed
	})
	if changed {
		t.Fatal("a dialog this new may not be drawn yet: wait")
	}
	d.Store.Update(id, func(e *store.Entry) bool {
		e.A.StatusSince = time.Now().Add(-5 * time.Second)
		changed = d.askOnScreen(e)
		return changed
	})
	if e, _ := d.Store.Get(id); !changed || e.A.Status != model.Idle || e.A.Waiting != nil || e.Form != nil {
		t.Fatalf("declined at the terminal: %+v", e.A)
	}
}
