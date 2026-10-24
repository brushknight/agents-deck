package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/cmux"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// cmuxWatch mirrors the Claude agents running in cmux's terminals onto the
// board: their state from cmux's Claude hooks, numbers from their
// transcripts, and focus / answer / interrupt through cmux's socket.
type cmuxWatch struct {
	mu       sync.Mutex
	sessions func() ([]cmux.Session, error) // cmux.Sessions; a fake in tests
	dial     func() cmuxAPI                 // cmux.Client; a fake in tests
	hidden   map[string]float64             // agent id -> its session's updatedAt when you hid it
	denied   bool                           // the socket refused us (logged once)
}

// cmuxAPI is what the deck asks of cmux's socket.
type cmuxAPI interface {
	Screen(surface string) (string, error)
	SendText(surface, text string) error
	SendKey(surface, key string) error
	Focus(surface string) error
}

func cmuxAgentID(surface string) string {
	sum := sha256.Sum256([]byte("cmux:" + surface))
	return "m" + hex.EncodeToString(sum[:])[:5]
}

// WatchCmux reads cmux's Claude sessions every 2 seconds (no-op with
// "cmux": false in config.json).
func (d *Daemon) WatchCmux() {
	if d.Cfg.Cmux != nil && !*d.Cfg.Cmux {
		return
	}
	logged := ""
	for {
		list, err := d.cmuxSessions()
		if err != nil {
			if err.Error() != logged {
				log.Printf("cmux: %v", err)
			}
			logged = err.Error()
		} else {
			logged = ""
			d.syncCmux(list)
		}
		time.Sleep(2 * time.Second)
	}
}

func (d *Daemon) cmuxSessions() ([]cmux.Session, error) {
	d.cmux.mu.Lock()
	f := d.cmux.sessions
	d.cmux.mu.Unlock()
	if f != nil {
		return f()
	}
	return cmux.Sessions()
}

func (d *Daemon) cmuxClient() cmuxAPI {
	d.cmux.mu.Lock()
	defer d.cmux.mu.Unlock()
	if d.cmux.dial != nil {
		return d.cmux.dial()
	}
	return cmux.Client{Socket: cmux.Socket()}
}

// cmuxErr logs the first refusal with what to change in cmux.
func (d *Daemon) cmuxErr(err error) error {
	if errors.Is(err, cmux.ErrDenied) {
		d.cmux.mu.Lock()
		first := !d.cmux.denied
		d.cmux.denied = true
		d.cmux.mu.Unlock()
		if first {
			log.Printf("cmux: %v", err)
		}
	}
	return err
}

// syncCmux puts cmux's running Claude sessions on the board, and takes off
// the ones that are gone.
func (d *Daemon) syncCmux(list []cmux.Session) {
	d.cmux.mu.Lock()
	hidden := d.cmux.hidden
	d.cmux.mu.Unlock()
	want := map[string]cmux.Session{}
	for _, s := range list {
		id := cmuxAgentID(s.Surface)
		if h, ok := hidden[id]; ok && h >= s.Updated {
			continue
		}
		want[id] = s
	}
	for _, e := range d.Store.Entries() {
		if e.A.App == "cmux" {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	for id, s := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, CmuxSurface: s.Surface}
			e.A = model.Agent{ID: id, Tool: "claude", External: true, App: "cmux", Status: model.Idle,
				Title: filepath.Base(s.Cwd), Cwd: s.Cwd, Folder: filepath.Base(s.Cwd)}
			d.Store.Add(e)
		}
		cur, _ := d.Store.Get(id)
		// A new question: its options are on the agent's screen. Read it
		// outside the store's lock.
		var screen string
		if s.Lifecycle == "needsInput" && cur.CmuxTurn != s.Updated {
			var err error
			if screen, err = d.cmuxClient().Screen(s.Surface); err != nil {
				_ = d.cmuxErr(err)
			}
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := false
			e.CmuxSurface = s.Surface
			if e.SessionID != s.ID {
				e.SessionID, e.Transcript, e.Offset, e.Usage, e.Pending = s.ID, s.Transcript, 0, nil, ""
				changed = true
			}
			tool, interrupted := "", time.Time{}
			if e.Transcript != "" {
				if claude.Tail(e) { // model, context, tokens, cost, title
					changed = true
				}
				_, tool = claude.TranscriptTurn(e.Transcript, 10*time.Minute)
				interrupted = claude.InterruptedAt(e.Transcript)
			}
			if s.Cwd != "" && s.Cwd != e.A.Cwd {
				e.A.Cwd, e.A.Folder, changed = s.Cwd, filepath.Base(s.Cwd), true
			}
			changed = cmuxApply(e, s, tool, screen, interrupted) || changed
			return d.subagents(e) || changed
		})
	}
}

// cmuxApply maps the state cmux's hooks report onto the agent; tool is the
// tool call the agent is in (from its transcript), screen (may be "") the
// terminal's text when a new question appeared, interrupted when the
// transcript last shows a turn interrupted (zero if not).
func cmuxApply(e *store.Entry, s cmux.Session, tool, screen string, interrupted time.Time) bool {
	changed := false
	state := s.Lifecycle
	// No hook runs when a turn is interrupted: an interrupt after the hooks'
	// last report ends the turn they report.
	stopped := !interrupted.IsZero() && interrupted.After(seconds(s.Updated))
	if stopped {
		state = "idle"
	}
	switch state {
	case "running":
		if e.A.Status != model.Running {
			e.SetStatus(model.Running)
			e.A.Unseen = false
			changed = true
		}
		var act *model.Activity
		if tool != "" {
			act = &model.Activity{Tool: tool}
		}
		if !activityEqual(act, e.A.Activity) {
			e.A.Activity, changed = act, true
		}
	case "needsInput":
		if e.CmuxTurn == s.Updated {
			break // handled this report already (or answered it from the deck)
		}
		p := screenPrompt(e, tool, screen, "cmux")
		if screen == "" { // no access to the screen: the hook's note
			p = bodyPrompt(e, p, s.Body)
		}
		e.CmuxTurn = s.Updated
		if e.A.Status == model.Waiting && samePrompt(e.A.Waiting, p) {
			break // the same question, reported again
		}
		e.SetStatus(model.Waiting)
		e.A.Waiting = p
		changed = true
	default: // idle
		if e.A.Status == model.Running || e.A.Status == model.Waiting {
			e.SetStatus(model.Idle) // also clears the activity
			e.A.Unseen = !stopped   // a turn finished while we watched (you stopped it yourself)
			changed = true
		}
	}
	return changed
}

// seconds turns cmux's timestamps (seconds since 1970) into a time.
func seconds(s float64) time.Time {
	return time.UnixMilli(int64(s * 1000))
}

// bodyPrompt fills a prompt from the hook's last note ("Do you prefer red or
// blue?\n[Red] [Blue]") when the screen can't be read; it's answered in cmux.
func bodyPrompt(e *store.Entry, p *model.Prompt, body string) *model.Prompt {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	if first != "" && p.Title == "needs you" {
		p.Title = strings.ToLower(clipText(first, 200))
	}
	if p.Kind == "question" && len(p.Options) == 0 {
		p.Kind = "input"
	}
	return p
}

func samePrompt(a, b *model.Prompt) bool {
	if a == nil || b == nil || a.Title != b.Title || a.Detail != b.Detail || len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i].Key != b.Options[i].Key || a.Options[i].Label != b.Options[i].Label {
			return false
		}
	}
	return true
}

// ---- actions --------------------------------------------------------------

// cmuxSend types the option's key into the agent's terminal.
func (d *Daemon) cmuxSend(e store.Entry, key string) error {
	return d.cmuxErr(d.cmuxClient().SendText(e.CmuxSurface, key))
}

// cmuxInterrupt presses Esc, which stops the agent's current turn.
func (d *Daemon) cmuxInterrupt(e store.Entry) error {
	return d.cmuxErr(d.cmuxClient().SendKey(e.CmuxSurface, "escape"))
}

// cmuxFocus shows the agent's terminal in cmux and brings cmux forward.
// Without socket access it can only bring cmux forward.
func (d *Daemon) cmuxFocus(e store.Entry) error {
	d.Store.Update(e.A.ID, func(x *store.Entry) bool { was := x.A.Unseen; x.A.Unseen = false; return was })
	err := d.cmuxClient().Focus(e.CmuxSurface)
	if errors.Is(err, cmux.ErrDenied) {
		_ = exec.Command("/usr/bin/open", "-b", "com.cmuxterm.app").Run()
	}
	if err != nil {
		return d.cmuxErr(err)
	}
	log.Printf("focus %s (cmux)", e.A.ID)
	return nil
}

// cmuxHide keeps a cmux agent off the board until its state changes.
func (d *Daemon) cmuxHide(e store.Entry) {
	at := float64(1 << 62)
	if list, err := d.cmuxSessions(); err == nil {
		for _, s := range list {
			if s.Surface == e.CmuxSurface {
				at = s.Updated
			}
		}
	}
	d.cmux.mu.Lock()
	if d.cmux.hidden == nil {
		d.cmux.hidden = map[string]float64{}
	}
	d.cmux.hidden[e.A.ID] = at
	d.cmux.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
