package daemon

import (
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/cmux"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type fakeCmux struct {
	screen string
	err    error // every call fails with it (cmux.ErrDenied: no socket access)
	sent   []string
	keys   []string
}

func (f *fakeCmux) Screen(string) (string, error) { return f.screen, f.err }
func (f *fakeCmux) Focus(string) error            { return f.err }
func (f *fakeCmux) SendText(surface, text string) error {
	f.sent = append(f.sent, surface+" "+text)
	return f.err
}
func (f *fakeCmux) SendKey(surface, key string) error {
	f.keys = append(f.keys, key)
	return f.err
}

const cmuxSession = "11111111-1111-4111-8111-111111111111"

func cmuxList(lifecycle string, at float64) []cmux.Session {
	return []cmux.Session{{ID: cmuxSession, Surface: "S1", PID: 101, Cwd: "/work/api", Lifecycle: lifecycle,
		Body: "Do you prefer red or blue?\n[Red] [Blue]", Updated: at}}
}

func TestCmuxAgentsOnTheDeck(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	f := &fakeCmux{screen: questionScreen}
	d.cmux.dial = func() cmuxAPI { return f }
	id := cmuxAgentID("S1")

	d.syncCmux(cmuxList("running", 1))
	e, ok := d.Store.Get(id)
	if !ok || e.A.Status != model.Running || e.A.App != "cmux" || e.A.Tool != "claude" || e.A.Title != "api" ||
		!e.A.External || e.CmuxSurface != "S1" || e.SessionID != cmuxSession {
		t.Fatalf("running agent: %+v", e)
	}

	// Waiting: the dialog's question and options, read off the screen.
	d.syncCmux(cmuxList("needsInput", 2))
	e, _ = d.Store.Get(id)
	p := e.A.Waiting
	if e.A.Status != model.Waiting || p == nil || p.Kind != "question" || p.Title != "do you prefer red or blue?" ||
		len(p.Options) != 4 || p.Options[1].Label != "blue" || p.Context != "answer it in cmux" {
		t.Fatalf("waiting agent: %+v / %+v", e.A, p)
	}
	// The same question reported again (a later hook event) keeps its id.
	d.syncCmux(cmuxList("needsInput", 3))
	if e2, _ := d.Store.Get(id); e2.A.Waiting == nil || e2.A.Waiting.ID != p.ID {
		t.Fatal("prompt rebuilt for the same question")
	}

	// Answering types the key into the terminal; a stale prompt is refused.
	if err := d.Answer(id, "old", "1"); err != server.ErrStalePrompt {
		t.Fatalf("stale answer: %v", err)
	}
	if err := d.Answer(id, p.ID, "2"); err != nil || len(f.sent) != 1 || f.sent[0] != "S1 2" {
		t.Fatalf("answer: %v, sent %q", err, f.sent)
	}
	// Until the hooks report again, the old report doesn't bring the question back.
	d.syncCmux(cmuxList("needsInput", 3))
	if e, _ = d.Store.Get(id); e.A.Status != model.Running {
		t.Fatalf("answered question came back: %+v", e.A)
	}
	if err := d.Interrupt(id); err != nil || len(f.keys) != 1 || f.keys[0] != "escape" {
		t.Fatalf("interrupt: %v, keys %q", err, f.keys)
	}

	// A finished turn is hungry until you look.
	d.syncCmux(cmuxList("idle", 4))
	if e, _ = d.Store.Get(id); e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("finished: %+v", e.A)
	}
	if err := d.Focus(id); err != nil {
		t.Fatal(err)
	}
	if e, _ = d.Store.Get(id); e.A.Unseen {
		t.Fatal("still hungry after focus")
	}

	// Hidden until its state changes; gone when the session ends.
	d.cmux.sessions = func() ([]cmux.Session, error) { return cmuxList("idle", 4), nil }
	if err := d.Dismiss(id); err != nil {
		t.Fatal(err)
	}
	d.syncCmux(cmuxList("idle", 4))
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("hidden agent back")
	}
	d.syncCmux(cmuxList("running", 5))
	if _, ok := d.Store.Get(id); !ok {
		t.Fatal("hidden agent stayed away after it started working")
	}
	d.syncCmux(nil)
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("agent stayed after its session ended")
	}
}

func TestCmuxInterrupted(t *testing.T) {
	e := &store.Entry{}
	e.SetStatus(model.Running)
	// The hooks last said running (at 100 s); the transcript shows an Esc after that.
	if !cmuxApply(e, cmux.Session{Lifecycle: "running", Updated: 100}, "", "", time.Unix(105, 0)) ||
		e.A.Status != model.Idle || e.A.Unseen {
		t.Fatalf("interrupted: %+v", e.A)
	}
	// A new turn: the hooks report again after the interrupt.
	cmuxApply(e, cmux.Session{Lifecycle: "running", Updated: 110}, "", "", time.Unix(105, 0))
	if e.A.Status != model.Running {
		t.Fatalf("new turn: %+v", e.A)
	}
}

func TestCmuxWithoutSocketAccess(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	f := &fakeCmux{err: cmux.ErrDenied}
	d.cmux.dial = func() cmuxAPI { return f }
	id := cmuxAgentID("S1")

	// The question comes from the hooks' note; it's answered in cmux.
	d.syncCmux(cmuxList("needsInput", 1))
	e, _ := d.Store.Get(id)
	if p := e.A.Waiting; e.A.Status != model.Waiting || p == nil || p.Kind != "input" ||
		p.Title != "do you prefer red or blue?" || len(p.Options) != 0 {
		t.Fatalf("waiting agent: %+v / %+v", e.A, p)
	}
	if err := d.Interrupt(id); err != cmux.ErrDenied {
		t.Fatalf("interrupt: %v", err)
	}
}
