package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// Restore modes (`agentctl set restore …`).
const (
	RestoreAsk  = "ask"  // mark agents lost with tmux; `agentctl restore` brings them back
	RestoreAuto = "auto" // bring them back as soon as the loss is noticed
)

// autoRestoreGap keeps a crashing tmux server from being restarted in a loop.
const autoRestoreGap = 5 * time.Minute

// RestoreResult is what happened to one lost agent.
type RestoreResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Action string `json:"action"` // resumed | relaunched | removed | failed
	Error  string `json:"error,omitempty"`
}

// markLost flags agents that ended because the tmux server went away, not by
// themselves: they stay on the board (no expiry) until restored or removed.
// Several agents gone in one tick with no server left is a crash; on daemon
// start, agents that were alive before and have no session now went down
// with the machine or the server.
func (d *Daemon) markLost(ids []string, why string) {
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		d.Store.Update(id, func(e *store.Entry) bool {
			if e.Sim || e.External {
				return false
			}
			e.A.Lost = true
			return true
		})
	}
	log.Printf("%d agent(s) lost with the tmux server (%s)", len(ids), why)
	if CurrentRestore() == RestoreAuto {
		go d.autoRestore()
	}
}

func (d *Daemon) autoRestore() {
	d.mu.Lock()
	if time.Since(d.lastAutoRestore) < autoRestoreGap {
		d.mu.Unlock()
		log.Printf("auto-restore skipped: the last one was less than %s ago", autoRestoreGap)
		return
	}
	d.lastAutoRestore = time.Now()
	d.mu.Unlock()
	time.Sleep(time.Second) // let the store settle (and a starting tmux server)
	for _, r := range d.Restore() {
		log.Printf("auto-restore %s (%s): %s %s", r.ID, r.Title, r.Action, r.Error)
	}
}

// Restore brings back every agent lost with the tmux server: Claude agents on
// their own conversation, other tools fresh in their folder, simulated agents
// dropped. Each keeps its id, title and board position.
func (d *Daemon) Restore() []RestoreResult {
	var lost []store.Entry
	for _, e := range d.Store.Entries() {
		if e.A.Lost {
			lost = append(lost, e)
		}
	}
	out := make([]RestoreResult, 0, len(lost))
	for i, e := range lost {
		if i > 0 {
			time.Sleep(300 * time.Millisecond) // don't start a dozen agents in the same instant
		}
		r := RestoreResult{ID: e.A.ID, Title: e.A.Title}
		var err error
		switch {
		case e.Sim:
			d.Store.Remove(e.A.ID)
			r.Action = "removed"
		case e.A.Tool == "claude" && e.SessionID != "":
			err = d.Resume(e.A.ID)
			r.Action = "resumed"
		default:
			err = d.relaunch(e)
			r.Action = "relaunched"
		}
		if err != nil {
			r.Action, r.Error = "failed", err.Error()
		} else if r.Action != "removed" {
			d.Store.Update(e.A.ID, func(x *store.Entry) bool { x.A.Lost = false; return true })
		}
		out = append(out, r)
	}
	return out
}

// relaunch starts a lost non-Claude agent again, fresh, in its folder.
func (d *Daemon) relaunch(e store.Entry) error {
	if _, err := os.Stat(e.A.Cwd); err != nil {
		return errors.New("folder no longer exists: " + e.A.Cwd)
	}
	var inner []string
	status := model.Running
	switch e.A.Tool {
	case "codex", "gemini", "opencode":
		inner = []string{e.A.Tool}
	case "shell":
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/zsh"
		}
		inner, status = []string{sh}, model.Idle
	default:
		return fmt.Errorf("can't relaunch %q", e.A.Tool)
	}
	if err := tmux.NewSession(e.TmuxName, e.A.Cwd, agentEnv(e.A.ID), []string{"/bin/zsh", "-lc", "exec " + shellJoin(inner)}); err != nil {
		return err
	}
	d.Store.Update(e.A.ID, func(x *store.Entry) bool {
		x.SetStatus(status)
		x.A.StartedAt = time.Now().UTC()
		return true
	})
	return nil
}

// RestoreJSON is Restore for the HTTP APIs.
func (d *Daemon) RestoreJSON() any { return d.Restore() }
