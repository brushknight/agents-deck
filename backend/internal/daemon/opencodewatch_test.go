package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/opencode"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

func TestOpenCodePrompt(t *testing.T) {
	p := opencodePrompt(opencode.Permission{ID: "r1", SessionID: "s", Action: "bash", Resources: []string{"rm -rf x"}, Message: "run this?"})
	if p.ID != "r1" || p.Kind != "permission" || p.Title != "run this?" || p.Context != "bash" {
		t.Fatalf("prompt: %+v", p)
	}
	if len(p.Options) != 2 || p.Options[0].Key != "1" || !p.Options[0].Primary || p.Options[1].Key != "3" {
		t.Fatalf("options: %+v", p.Options)
	}
	p = opencodePrompt(opencode.Permission{ID: "r2", Save: []string{"bash"}, Action: "bash"})
	if len(p.Options) != 3 || p.Options[1].Key != "2" {
		t.Fatalf("always option: %+v", p.Options)
	}
}

func TestSyncOpenCodeStates(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	now := time.Now()
	s := opencode.Session{ID: "ses_1", Directory: "/work/app", Title: "fix",
		TimeUpdated: now.UnixMilli(), TimeBusy: now.UnixMilli(), TimeIdle: now.Add(-time.Minute).UnixMilli(),
		TokensInput: 10, TokensOutput: 20, CacheRead: 30, Cost: 0.5, Turns: 2}
	d.syncOpenCode([]opencode.Session{s}, map[string]bool{"ses_1": true}, true, nil, map[string]bool{"ses_1": true})
	e, ok := d.Store.Get(opencode.AgentID("ses_1"))
	if !ok || e.A.Status != model.Running || e.A.App != "opencode" || e.A.Tool != "opencode" {
		t.Fatalf("running: %+v", e)
	}
	if e.A.Tokens.Output != 20 || e.A.Tokens.CacheRead != 30 || e.A.CostUSD != 0.5 || e.A.Turns != 2 {
		t.Fatalf("usage: %+v", e.A)
	}
	perms := map[string][]opencode.Permission{"ses_1": {{ID: "r1", SessionID: "ses_1", Action: "bash", Resources: []string{"ls"}, Save: []string{"bash"}}}}
	d.syncOpenCode([]opencode.Session{s}, map[string]bool{}, true, perms, map[string]bool{"ses_1": true})
	e, _ = d.Store.Get(opencode.AgentID("ses_1"))
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.ID != "r1" || len(e.A.Waiting.Options) != 3 {
		t.Fatalf("waiting: %+v", e.A)
	}
	// An API failure must not turn a waiting session idle.
	d.syncOpenCode([]opencode.Session{s}, nil, false, nil, nil)
	e, _ = d.Store.Get(opencode.AgentID("ses_1"))
	if e.A.Status != model.Waiting || e.A.Waiting == nil {
		t.Fatalf("api failure: %+v", e.A)
	}
	// A permission call failing while the session is active must not clear
	// the prompt either.
	d.syncOpenCode([]opencode.Session{s}, map[string]bool{"ses_1": true}, true, nil, map[string]bool{"ses_1": false})
	e, _ = d.Store.Get(opencode.AgentID("ses_1"))
	if e.A.Status != model.Waiting || e.A.Waiting == nil {
		t.Fatalf("permission failure: %+v", e.A)
	}
	// The answered request stays answered while the API still lists it.
	d.opencode.mu.Lock()
	d.opencode.done = map[string]bool{"r1": true}
	d.opencode.mu.Unlock()
	d.syncOpenCode([]opencode.Session{s}, map[string]bool{}, true, perms, map[string]bool{"ses_1": true})
	e, _ = d.Store.Get(opencode.AgentID("ses_1"))
	if e.A.Status != model.Idle || e.A.Waiting != nil {
		t.Fatalf("answered: %+v", e.A)
	}
	// Hide, and keep it hidden for an unchanged session.
	d.opencodeHide(e)
	if _, ok := d.Store.Get(opencode.AgentID("ses_1")); ok {
		t.Fatal("hide left the tile")
	}
	d.syncOpenCode([]opencode.Session{s}, map[string]bool{"ses_1": true}, true, nil, map[string]bool{"ses_1": true})
	if _, ok := d.Store.Get(opencode.AgentID("ses_1")); ok {
		t.Fatal("hidden tile came back")
	}
	// New activity brings it back.
	s2 := s
	s2.TimeUpdated = time.Now().Add(time.Minute).UnixMilli()
	d.syncOpenCode([]opencode.Session{s2}, map[string]bool{"ses_1": true}, true, nil, map[string]bool{"ses_1": true})
	if _, ok := d.Store.Get(opencode.AgentID("ses_1")); !ok {
		t.Fatal("new activity did not bring it back")
	}
}

func TestOpenCodeHideSyncConcurrent(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	s := opencode.Session{ID: "ses_1", Directory: "/work/app", Title: "x",
		TimeUpdated: time.Now().UnixMilli()}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.syncOpenCode([]opencode.Session{s}, map[string]bool{"ses_1": true}, true, nil, map[string]bool{"ses_1": true})
		}()
		go func() {
			defer wg.Done()
			if e, ok := d.Store.Get(opencode.AgentID("ses_1")); ok {
				d.opencodeHide(e)
			}
		}()
	}
	wg.Wait()
}
