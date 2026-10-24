package daemon

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/herdr"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type fakeHerdr struct {
	snap   herdr.Snapshot
	screen string
	sent   []string
	keys   []string
}

func (f *fakeHerdr) Read() (herdr.Snapshot, error) { return f.snap, nil }
func (f *fakeHerdr) PID(string) int                { return 0 }
func (f *fakeHerdr) Screen(string) (string, error) { return f.screen, nil }
func (f *fakeHerdr) Focus(string) error            { return nil }
func (f *fakeHerdr) SendText(pane, text string) error {
	f.sent = append(f.sent, pane+" "+text)
	return nil
}
func (f *fakeHerdr) SendKeys(pane string, keys ...string) error {
	f.keys = append(f.keys, keys...)
	return nil
}

const questionScreen = `
❯ ask me which colour I like
────────────────────────────────────────
 ☐ Colour
Do you prefer red or blue?
❯ 1. Red
     Choose red
  2. Blue
     Choose blue
  3. Type something.
────────────────────────────────────────
  4. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel
`

const sock = "/run/herdr.sock"

func herdrSnap(status string, seq int64) map[string]herdr.Snapshot {
	return map[string]herdr.Snapshot{sock: {
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "api"}},
		Agents: []herdr.Agent{{TerminalID: "term_1", PaneID: "w1:p2", WorkspaceID: "w1", Agent: "codex",
			Status: status, Cwd: "/work/api", StateChangeSeq: seq}},
	}}
}

func TestHerdrAgentsOnTheDeck(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	f := &fakeHerdr{screen: questionScreen}
	d.herdr.dial = func(string) herdrAPI { return f }
	id := herdrAgentID(sock, "term_1")

	d.syncHerdr(herdrSnap("working", 1))
	e, ok := d.Store.Get(id)
	if !ok || e.A.Status != model.Running || e.A.App != "herdr" || e.A.Tool != "codex" || e.A.Title != "api" ||
		e.A.Folder != "api" || !e.A.External || e.HerdrPane != "w1:p2" {
		t.Fatalf("working agent: %+v", e.A)
	}

	// Blocked: the dialog's question and options, read off the screen.
	d.syncHerdr(herdrSnap("blocked", 2))
	e, _ = d.Store.Get(id)
	p := e.A.Waiting
	if e.A.Status != model.Waiting || p == nil || p.Kind != "question" || p.Title != "do you prefer red or blue?" ||
		p.Detail != "Colour" || len(p.Options) != 4 || p.Options[1].Label != "blue" {
		t.Fatalf("blocked agent: %+v / %+v", e.A, p)
	}
	d.syncHerdr(herdrSnap("blocked", 2))
	if e2, _ := d.Store.Get(id); e2.A.Waiting.ID != p.ID {
		t.Fatal("prompt rebuilt for the same state")
	}

	// Answering types the key into the pane; a stale prompt is refused.
	if err := d.Answer(id, "old", "1"); err != server.ErrStalePrompt {
		t.Fatalf("stale answer: %v", err)
	}
	if err := d.Answer(id, p.ID, "9"); err != server.ErrBadKey {
		t.Fatalf("bad key: %v", err)
	}
	if err := d.Answer(id, p.ID, "2"); err != nil || len(f.sent) != 1 || f.sent[0] != "w1:p2 2" {
		t.Fatalf("answer: %v, sent %q", err, f.sent)
	}
	if err := d.Interrupt(id); err != nil || len(f.keys) != 1 || f.keys[0] != "esc" {
		t.Fatalf("interrupt: %v, keys %q", err, f.keys)
	}

	// Done (not looked at in herdr yet) is hungry; idle is not.
	d.syncHerdr(herdrSnap("done", 3))
	if e, _ = d.Store.Get(id); e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("done: %+v", e.A)
	}
	d.syncHerdr(herdrSnap("idle", 4))
	if e, _ = d.Store.Get(id); e.A.Unseen {
		t.Fatal("idle still hungry")
	}

	// Hidden until its state changes; gone when herdr no longer has it.
	f.snap = herdrSnap("idle", 4)[sock]
	if err := d.Dismiss(id); err != nil {
		t.Fatal(err)
	}
	d.syncHerdr(herdrSnap("idle", 4))
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("hidden agent back")
	}
	d.syncHerdr(herdrSnap("working", 5))
	if _, ok := d.Store.Get(id); !ok {
		t.Fatal("hidden agent stayed away after it started working")
	}
	d.syncHerdr(nil)
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("agent stayed after herdr stopped")
	}
}

func TestScreenQuestion(t *testing.T) {
	// A permission dialog without a rule above it.
	title, detail := screenQuestion(permissionScreen)
	if title != "Do you want to proceed?" || detail != "Bash command\nrm -rf build" {
		t.Errorf("permission: %q / %q", title, detail)
	}
	if title, _ := screenQuestion("❯ fix the tests\n\n  esc to interrupt"); title != "" {
		t.Errorf("no dialog: %q", title)
	}
}
