package daemon

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/copilot"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// copilotWatch mirrors live Copilot CLI sessions onto the board, read-only.
type copilotWatch struct {
	mu      sync.Mutex
	syncMu  sync.Mutex
	tracker *copilot.Tracker
	hidden  map[string]time.Time // session id -> last activity when hidden
}

func (d *Daemon) copilotWindow() time.Duration { return 6 * time.Hour }

// WatchCopilot reads Copilot's live session logs every 2 seconds (no-op with
// "copilot": false in config.json).
func (d *Daemon) WatchCopilot() {
	if d.Cfg.Copilot != nil && !*d.Cfg.Copilot {
		return
	}
	d.copilot.mu.Lock()
	if d.copilot.tracker == nil {
		d.copilot.tracker = copilot.NewTracker()
	}
	tracker := d.copilot.tracker
	d.copilot.mu.Unlock()
	for {
		d.syncCopilot(tracker.Sessions(d.copilotWindow(), time.Now()))
		time.Sleep(2 * time.Second)
	}
}

func (d *Daemon) syncCopilot(list []copilot.Session) {
	d.copilot.syncMu.Lock()
	defer d.copilot.syncMu.Unlock()
	d.copilot.mu.Lock()
	hidden := make(map[string]time.Time, len(d.copilot.hidden))
	for id, at := range d.copilot.hidden {
		hidden[id] = at
	}
	d.copilot.mu.Unlock()
	want := map[string]copilot.Session{}
	for _, s := range list {
		id := copilot.AgentID(s.ID)
		if h, ok := hidden[s.ID]; ok && !s.LastActive.After(h) {
			continue
		}
		want[id] = s
	}
	for _, e := range d.Store.Entries() {
		if e.A.App == "copilot" {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	for id, s := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, SessionID: s.ID}
			e.A = model.Agent{
				ID: id, Tool: "copilot", External: true, App: "copilot", Status: model.Idle,
				StatusSince: s.Since,
				Title:       s.Title, Cwd: s.Cwd, Folder: filepath.Base(s.Cwd),
				Model: s.Model, ModelLabel: s.ModelLabel,
			}
			d.Store.Add(e)
			d.Store.Update(id, func(x *store.Entry) bool { x.A.StatusSince = s.Since; return true })
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := false
			e.SessionID = s.ID
			was := e.A.Status
			if e.A.Status != s.Status {
				e.SetStatus(s.Status)
				if !s.Since.IsZero() {
					e.A.StatusSince = s.Since
				}
				if s.Status == model.Running {
					e.A.Unseen = false
				}
				if (was == model.Running || was == model.Waiting) && s.Status == model.Idle {
					e.A.Unseen = true
				}
				changed = true
			} else if e.A.StatusSince.IsZero() {
				e.A.StatusSince = s.Since
			}
			if s.Status == model.Running {
				activity := s.Activity
				if activity == "" {
					activity = "working"
				}
				act := &model.Activity{Tool: activity, Detail: "copilot"}
				if e.A.Activity == nil || *e.A.Activity != *act {
					e.A.Activity = act
					changed = true
				}
			} else if e.A.Activity != nil {
				e.A.Activity = nil
				changed = true
			}
			if s.Cwd != "" && s.Cwd != e.A.Cwd {
				e.A.Cwd, e.A.Folder, changed = s.Cwd, filepath.Base(s.Cwd), true
			}
			if s.Title != "" && s.Title != e.A.Title {
				e.A.Title, changed = s.Title, true
			}
			if s.Model != "" && s.Model != e.A.Model {
				e.A.Model, e.A.ModelLabel, changed = s.Model, s.ModelLabel, true
			}
			if e.A.Tokens != s.Tokens {
				e.A.Tokens, changed = s.Tokens, true
			}
			if e.A.Context != s.Context {
				e.A.Context, changed = s.Context, true
			}
			if e.A.CostUSD != s.Cost {
				e.A.CostUSD, changed = s.Cost, true
			}
			if s.Turns > 0 && s.Turns != e.A.Turns {
				e.A.Turns, changed = s.Turns, true
			}
			if !s.LastActive.IsZero() && e.MarkActiveStep(s.LastActive) {
				changed = true
			}
			return changed
		})
	}
}

func (d *Daemon) copilotReadOnly() error {
	return fmt.Errorf("%w: Copilot sessions are read-only here; use Copilot to answer or interrupt", server.ErrReadOnly)
}

func (d *Daemon) copilotSend(e store.Entry, key string) error { return d.copilotReadOnly() }
func (d *Daemon) copilotInterrupt(e store.Entry) error        { return d.copilotReadOnly() }
func (d *Daemon) copilotPrompt(e store.Entry, text string) error {
	return d.copilotReadOnly()
}
func (d *Daemon) copilotFocus(e store.Entry) error { return d.copilotReadOnly() }

func (d *Daemon) copilotHide(e store.Entry) {
	d.copilot.syncMu.Lock()
	defer d.copilot.syncMu.Unlock()
	d.copilot.mu.Lock()
	if d.copilot.hidden == nil {
		d.copilot.hidden = map[string]time.Time{}
	}
	if e.SessionID != "" {
		d.copilot.hidden[e.SessionID] = time.Now().UTC()
	}
	d.copilot.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
