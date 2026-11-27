package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/copilot"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

func TestSyncCopilotStates(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	now := time.Now().UTC()
	s := copilot.Session{ID: "cp1", Title: "fix", Cwd: "/work/app", Status: model.Running,
		LastActive: now, Since: now.Add(-time.Minute), Tokens: model.Tokens{Output: 20},
		Context: model.Context{Used: 100, Window: 1000}, Cost: 0.25, Turns: 3}
	d.syncCopilot([]copilot.Session{s})
	e, ok := d.Store.Get(copilot.AgentID("cp1"))
	if !ok || e.A.Status != model.Running || e.A.App != "copilot" || e.A.Tool != "copilot" {
		t.Fatalf("running: %+v", e)
	}
	if e.A.Tokens.Output != 20 || e.A.Context.Used != 100 || e.A.CostUSD != 0.25 || e.A.Turns != 3 {
		t.Fatalf("usage: %+v", e.A)
	}
	s.Status = model.Waiting
	d.syncCopilot([]copilot.Session{s})
	e, _ = d.Store.Get(copilot.AgentID("cp1"))
	if e.A.Status != model.Waiting {
		t.Fatalf("waiting: %+v", e.A)
	}
	// Hide, and do not resurrect it on the same activity.
	d.copilotHide(e)
	if _, ok := d.Store.Get(copilot.AgentID("cp1")); ok {
		t.Fatal("hide left the tile")
	}
	d.syncCopilot([]copilot.Session{s})
	if _, ok := d.Store.Get(copilot.AgentID("cp1")); ok {
		t.Fatal("hidden tile came back")
	}
	// New activity brings it back.
	s.LastActive = now.Add(time.Minute)
	s.Status = model.Running
	d.syncCopilot([]copilot.Session{s})
	if _, ok := d.Store.Get(copilot.AgentID("cp1")); !ok {
		t.Fatal("new activity did not bring it back")
	}
	// A session that disappears leaves the board.
	d.syncCopilot(nil)
	if _, ok := d.Store.Get(copilot.AgentID("cp1")); ok {
		t.Fatal("removed session stayed")
	}
}

func TestCopilotHideSyncConcurrent(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	s := copilot.Session{ID: "cp1", Title: "x", Cwd: "/work/app", Status: model.Running,
		LastActive: time.Now().UTC(), Since: time.Now().UTC()}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); d.syncCopilot([]copilot.Session{s}) }()
		go func() {
			defer wg.Done()
			if e, ok := d.Store.Get(copilot.AgentID("cp1")); ok {
				d.copilotHide(e)
			}
		}()
	}
	wg.Wait()
}
