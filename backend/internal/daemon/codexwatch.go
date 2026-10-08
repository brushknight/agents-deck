package daemon

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/codex"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// errExternal answers actions that only the owning app can perform.
var errExternal = errors.New("this is a Codex app session — answer, interrupt or stop it in the Codex app")

// codexWatch mirrors recent Codex app/CLI threads onto the board.
type codexWatch struct {
	mu     sync.Mutex
	hidden map[string]int64 // thread id -> updated_at_ms when hidden
}

func (d *Daemon) codexWindow() time.Duration {
	if d.Cfg.CodexWindowHours > 0 {
		return time.Duration(d.Cfg.CodexWindowHours * float64(time.Hour))
	}
	return 6 * time.Hour
}

// WatchCodex polls Codex's state every few seconds (no-op when Codex isn't installed).
func (d *Daemon) WatchCodex() {
	if d.Cfg.Codex != nil && !*d.Cfg.Codex {
		return
	}
	if _, err := os.Stat(codex.Home()); err != nil {
		return
	}
	logged := false
	for {
		threads, err := codex.RecentThreads(d.codexWindow())
		if err != nil && !logged {
			log.Printf("codex: %v", err)
			logged = true
		}
		d.syncCodex(threads)
		time.Sleep(2 * time.Second)
	}
}

func (d *Daemon) syncCodex(threads []codex.Thread) {
	d.codex.mu.Lock()
	hidden := d.codex.hidden
	d.codex.mu.Unlock()
	want := map[string]codex.Thread{}
	for _, t := range threads {
		if t.RolloutPath == "" || hidden[t.ID] >= t.UpdatedAtMs && hidden[t.ID] != 0 {
			continue
		}
		want[codex.AgentID(t.ID)] = t
	}
	// Drop threads that aged out, were archived or hidden.
	for _, a := range d.Store.Snapshot().Agents {
		if a.External {
			if _, ok := want[a.ID]; !ok {
				d.Store.Remove(a.ID)
			}
		}
	}
	for id, t := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, SessionID: t.ID, Transcript: t.RolloutPath}
			e.A = model.Agent{ID: id, Tool: "codex", External: true, Status: model.Idle,
				Title: t.DisplayTitle(), Cwd: t.Cwd, Folder: filepath.Base(t.Cwd), Branch: t.Branch, Model: t.Model}
			d.Store.Add(e)
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := codex.Tail(e)
			if title := t.DisplayTitle(); title != e.A.Title {
				e.A.Title, changed = title, true
			}
			if t.Branch != "" && t.Branch != e.A.Branch {
				e.A.Branch, changed = t.Branch, true
			}
			// The app was closed mid-turn: nothing written for a while means it isn't working.
			if e.A.Status == model.Running || e.A.Status == model.Waiting {
				if st, err := os.Stat(e.Transcript); err == nil && time.Since(st.ModTime()) > 10*time.Minute {
					e.SetStatus(model.Idle)
					changed = true
				}
			}
			return changed
		})
	}
}

// focusExternal opens the thread in the Codex app (codex:// deep link; no Apple events).
func (d *Daemon) focusExternal(e store.Entry) error {
	d.Store.Update(e.A.ID, func(x *store.Entry) bool { was := x.A.Unseen; x.A.Unseen = false; return was })
	if err := exec.Command("/usr/bin/open", "codex://threads/"+e.SessionID).Run(); err != nil {
		return err
	}
	log.Printf("focus %s: opened codex thread", e.A.ID)
	return nil
}

// hideExternal keeps a Codex thread off the board until it has new activity.
func (d *Daemon) hideExternal(e store.Entry) {
	d.codex.mu.Lock()
	if d.codex.hidden == nil {
		d.codex.hidden = map[string]int64{}
	}
	d.codex.hidden[e.SessionID] = time.Now().UnixMilli()
	d.codex.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
