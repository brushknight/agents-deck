package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/herdr"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/term"
)

// herdrWatch mirrors agents running in herdr's panes onto the board: status
// from herdr (it reads their screens), numbers from a Claude agent's
// transcript, and focus / answer / interrupt through herdr's panes.
type herdrWatch struct {
	mu     sync.Mutex
	dial   func(socket string) herdrAPI // herdr.Client; a fake in tests
	hidden map[string]int64             // agent id -> its state_change_seq when you hid it
}

// herdrAPI is what the deck asks of one herdr session.
type herdrAPI interface {
	Read() (herdr.Snapshot, error)
	PID(pane string) int
	Screen(pane string) (string, error)
	SendText(pane, text string) error
	SendKeys(pane string, keys ...string) error
	Focus(pane string) error
}

func herdrAgentID(socket, terminal string) string {
	sum := sha256.Sum256([]byte("herdr:" + socket + ":" + terminal))
	return "h" + hex.EncodeToString(sum[:])[:5]
}

// WatchHerdr polls every running herdr session every 2 seconds (no-op with
// "herdr": false in config.json).
func (d *Daemon) WatchHerdr() {
	if d.Cfg.Herdr != nil && !*d.Cfg.Herdr {
		return
	}
	logged := map[string]string{}
	for {
		snaps := map[string]herdr.Snapshot{}
		for _, sock := range herdr.Sockets() {
			snap, err := d.herdrClient(sock).Read()
			if err != nil {
				// A socket left behind by a stopped server is normal.
				if !stale(err) && logged[sock] != err.Error() {
					log.Printf("herdr: %v", err)
				}
				logged[sock] = err.Error()
				continue
			}
			delete(logged, sock)
			snaps[sock] = snap
		}
		d.syncHerdr(snaps)
		time.Sleep(2 * time.Second)
	}
}

func stale(err error) bool {
	return errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "connection refused")
}

func (d *Daemon) herdrClient(socket string) herdrAPI {
	d.herdr.mu.Lock()
	defer d.herdr.mu.Unlock()
	if d.herdr.dial != nil {
		return d.herdr.dial(socket)
	}
	return herdr.Client{Socket: socket}
}

type herdrRow struct {
	socket string
	a      herdr.Agent
	label  string // its workspace's label
}

// syncHerdr puts the agents of every running herdr session (socket ->
// snapshot) on the board, and takes off the ones that are gone.
func (d *Daemon) syncHerdr(snaps map[string]herdr.Snapshot) {
	d.herdr.mu.Lock()
	hidden := d.herdr.hidden
	d.herdr.mu.Unlock()
	want := map[string]herdrRow{}
	for sock, snap := range snaps {
		labels := map[string]string{}
		for _, w := range snap.Workspaces {
			labels[w.ID] = w.Label
		}
		for _, a := range snap.Agents {
			if a.Agent == "" {
				continue
			}
			id := herdrAgentID(sock, a.TerminalID)
			if h, ok := hidden[id]; ok && h >= a.StateChangeSeq {
				continue
			}
			want[id] = herdrRow{sock, a, labels[a.WorkspaceID]}
		}
	}
	for _, e := range d.Store.Entries() {
		if e.HerdrPane != "" {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	for id, r := range want {
		dir := r.a.Dir()
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, HerdrSocket: r.socket, HerdrPane: r.a.PaneID}
			e.A = model.Agent{ID: id, Tool: herdrTool(r.a.Agent), External: true, App: "herdr", Status: model.Idle,
				Title: herdrTitle(r), Cwd: dir, Folder: filepath.Base(dir)}
			d.Store.Add(e)
		}
		cur, _ := d.Store.Get(id)
		session := claude.Live{}
		if cur.A.Tool == "claude" {
			session = d.herdrSession(r)
		}
		// A new question: its options are on the agent's screen. Read it
		// outside the store's lock.
		var screen string
		if r.a.Status == "blocked" && cur.HerdrTurn != r.a.StateChangeSeq {
			screen, _ = d.herdrClient(r.socket).Screen(r.a.PaneID)
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := false
			e.HerdrPane = r.a.PaneID
			if session.SessionID != "" && e.SessionID != session.SessionID {
				e.SessionID, e.Transcript, e.Offset, e.Usage, e.Pending = session.SessionID, session.Transcript, 0, nil, ""
				changed = true
			}
			tool := ""
			if e.Transcript != "" && e.A.Tool == "claude" {
				if claude.Tail(e) { // model, context, tokens, cost
					changed = true
				}
				_, tool = claude.TranscriptTurn(e.Transcript, 10*time.Minute)
			}
			if t := herdrTitle(r); t != e.A.Title && e.A.AITitle == "" {
				e.A.Title, changed = t, true
			}
			if dir != "" && dir != e.A.Cwd {
				e.A.Cwd, e.A.Folder, changed = dir, filepath.Base(dir), true
			}
			return herdrApply(e, r.a, tool, screen) || changed
		})
	}
}

// herdrSession is the Claude session running in an agent's pane: the one
// herdr's own Claude hook reported, else the one Claude Code lists for the
// pane's process (which follows /clear to the new session).
func (d *Daemon) herdrSession(r herdrRow) claude.Live {
	sid := ""
	if s := r.a.Session; s != nil && s.Kind == "id" {
		sid = s.Value
	}
	d.live.mu.Lock()
	reported := d.live.reported
	d.live.mu.Unlock()
	pid := 0
	if sid == "" {
		pid = d.herdrClient(r.socket).PID(r.a.PaneID)
	}
	for _, l := range reported {
		if sid != "" && l.SessionID == sid || pid != 0 && l.PID == pid {
			return l
		}
	}
	return claude.Live{}
}

// herdrApply maps herdr's status onto the agent; tool is the tool call the
// agent is in (from its transcript), screen (may be "") the pane's text when
// a new prompt appeared.
func herdrApply(e *store.Entry, a herdr.Agent, tool, screen string) bool {
	changed := false
	switch a.Status {
	case "working":
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
	case "blocked":
		if e.A.Status != model.Waiting || e.HerdrTurn != a.StateChangeSeq {
			e.SetStatus(model.Waiting)
			e.A.Waiting = screenPrompt(e, tool, screen, "herdr")
			e.HerdrTurn = a.StateChangeSeq
			changed = true
		}
	default: // idle, done (finished, not looked at yet in herdr), unknown
		unseen := a.Status == "done"
		if e.A.Status != model.Idle || e.A.Unseen != unseen {
			e.SetStatus(model.Idle)
			e.A.Unseen = unseen
			changed = true
		}
	}
	return changed
}

func herdrTool(agent string) string {
	if agent == "" {
		return "claude"
	}
	return strings.ToLower(agent)
}

func herdrTitle(r herdrRow) string {
	switch {
	case r.a.Name != "":
		return r.a.Name
	case r.label != "":
		return r.label
	}
	return filepath.Base(r.a.Dir())
}

// ---- actions --------------------------------------------------------------

// herdrFocus shows the agent's pane in herdr and brings up the terminal tab
// herdr runs in.
func (d *Daemon) herdrFocus(e store.Entry) error {
	d.Store.Update(e.A.ID, func(x *store.Entry) bool { was := x.A.Unseen; x.A.Unseen = false; return was })
	if err := d.herdrClient(e.HerdrSocket).Focus(e.HerdrPane); err != nil {
		return err
	}
	ttys := herdr.ClientTTYs(herdr.SessionName(e.HerdrSocket))
	if len(ttys) == 0 {
		return errors.New("herdr isn't open in a terminal: run herdr to see it")
	}
	how := "tab selected"
	if err := d.iterm.selectTTY(ttys[0]); err != nil {
		how = "window only (" + err.Error() + ")"
	}
	if err := term.StealFocus(ttys[0]); err != nil {
		return err
	}
	log.Printf("focus %s (herdr): %s", e.A.ID, how)
	return nil
}

// herdrSend types the option's key into the agent's pane.
func (d *Daemon) herdrSend(e store.Entry, key string) error {
	return d.herdrClient(e.HerdrSocket).SendText(e.HerdrPane, key)
}

// herdrInterrupt presses Esc, which stops the agent's current turn.
func (d *Daemon) herdrInterrupt(e store.Entry) error {
	return d.herdrClient(e.HerdrSocket).SendKeys(e.HerdrPane, "esc")
}

// herdrHide keeps a herdr agent off the board until its state changes.
func (d *Daemon) herdrHide(e store.Entry) {
	seq := int64(1 << 62)
	if snap, err := d.herdrClient(e.HerdrSocket).Read(); err == nil {
		for _, a := range snap.Agents {
			if a.PaneID == e.HerdrPane {
				seq = a.StateChangeSeq
			}
		}
	}
	d.herdr.mu.Lock()
	if d.herdr.hidden == nil {
		d.herdr.hidden = map[string]int64{}
	}
	d.herdr.hidden[e.A.ID] = seq
	d.herdr.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
