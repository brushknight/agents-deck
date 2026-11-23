package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/term"
)

// errLive answers actions a watched session can't take from the deck.
var errLive = errors.New("this Claude session runs in its own terminal: answer, interrupt or stop it there")

// LiveCandidate is a Claude session running by hand in some terminal on this
// machine, which can be added to the deck.
type LiveCandidate struct {
	SessionID string    `json:"sessionId"`
	Title     string    `json:"title"`
	Cwd       string    `json:"cwd"`
	Folder    string    `json:"folder"`
	Started   time.Time `json:"started"`
	OnBoard   bool      `json:"onBoard"`
	// Background: a session running without a terminal (claude --bg, or one
	// the Claude desktop app runs); focus opens it with `claude attach`.
	Background bool `json:"background,omitempty"`
}

// liveWatch tracks hand-started Claude sessions and the ones you added.
type liveWatch struct {
	mu         sync.Mutex
	candidates []LiveCandidate
	added      map[string]bool // session id -> on the deck (persisted)
	offDeck    map[string]bool // session id -> you took it off the deck (persisted): `sessions auto` leaves it off
	loaded     bool

	// meta: each found session's transcript metadata, kept while the
	// transcript's size and time stay the same (sessionMeta).
	meta map[string]liveMeta

	claudeBin  string        // resolved once from the login shell
	reported   []claude.Live // last `claude agents --json`
	reportedAt time.Time
	reportedOK bool
}

func addedPath() string   { return filepath.Join(paths.Data(), "watched.json") }
func offDeckPath() string { return filepath.Join(paths.Data(), "offdeck.json") }

// liveAgentID derives a stable board id ([a-z0-9]{6}) from a session id.
func liveAgentID(sessionID string) string {
	sum := sha256.Sum256([]byte("claude-live:" + sessionID))
	return "l" + hex.EncodeToString(sum[:])[:5]
}

// liveAdded is the set of sessions you put on the deck; the caller holds
// d.live.mu. It loads the set you took off the deck too.
func (d *Daemon) liveAdded() map[string]bool {
	if !d.live.loaded {
		d.live.loaded = true
		d.live.added = readIDs(addedPath())
		d.live.offDeck = readIDs(offDeckPath())
	}
	return d.live.added
}

func (d *Daemon) saveAdded()   { writeIDs(addedPath(), d.live.added) }
func (d *Daemon) saveOffDeck() { writeIDs(offDeckPath(), d.live.offDeck) }

// readIDs reads a JSON list of session ids (a missing file is an empty set).
func readIDs(path string) map[string]bool {
	set := map[string]bool{}
	var ids []string
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &ids)
	}
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func writeIDs(path string, set map[string]bool) {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if b, err := json.Marshal(ids); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

type liveMeta struct {
	size int64
	mod  time.Time
	info claude.SessionInfo
}

// sessionMeta is l's transcript metadata (title, folder). Reading it parses
// up to half a megabyte of JSON, and syncLive asks for every session every
// 3 seconds: it is read again only when the transcript changed. Call with
// d.live.mu held.
func (d *Daemon) sessionMeta(l claude.Live) claude.SessionInfo {
	st, err := os.Stat(l.Transcript)
	if err != nil {
		info, _ := claude.ReadSession(l.Transcript, l.SessionID)
		return info
	}
	if m, ok := d.live.meta[l.SessionID]; ok && m.size == st.Size() && m.mod.Equal(st.ModTime()) {
		return m.info
	}
	info, _ := claude.ReadSession(l.Transcript, l.SessionID)
	if d.live.meta == nil {
		d.live.meta = map[string]liveMeta{}
	}
	d.live.meta[l.SessionID] = liveMeta{size: st.Size(), mod: st.ModTime(), info: info}
	return info
}

// WatchLive finds hand-started Claude sessions every few seconds and keeps the
// ones you added on the board (watch-only: status from their transcripts).
func (d *Daemon) WatchLive() {
	for {
		d.syncLive()
		time.Sleep(3 * time.Second)
	}
}

func (d *Daemon) syncLive() {
	// Our own agents run in tmux panes: never list those.
	skip := d.paneTTYs()
	ours := map[string]bool{}
	for _, e := range d.Store.Entries() {
		if (!e.External || inApp(e)) && e.SessionID != "" {
			ours[e.SessionID] = true // our own agents, and Orca's and herdr's (already on the deck)
		}
	}
	var found []claude.Live
	for _, l := range d.discover(skip) {
		if !ours[l.SessionID] {
			found = append(found, l)
		}
	}

	auto := CurrentSessions() == SessionsAuto
	d.live.mu.Lock()
	added := d.liveAdded()
	joined := false
	cands := make([]LiveCandidate, 0, len(found))
	want := map[string]claude.Live{}
	for _, l := range found {
		if auto && !added[l.SessionID] && !d.live.offDeck[l.SessionID] {
			added[l.SessionID] = true // `sessions auto`: it joins the deck by itself
			joined = true
			log.Printf("session %s joined the deck by itself (sessions auto)", l.SessionID)
		}
		info := d.sessionMeta(l)
		title := info.Title()
		if l.Name != "" && (info.AITitle == "" && info.Custom == "") {
			title = l.Name
		}
		cands = append(cands, LiveCandidate{SessionID: l.SessionID, Title: title, Cwd: l.Cwd,
			Folder: filepath.Base(l.Cwd), Started: l.Started, OnBoard: added[l.SessionID], Background: l.Background})
		if added[l.SessionID] {
			want[liveAgentID(l.SessionID)] = l
		}
	}
	d.live.candidates = cands
	for id := range d.live.meta { // sessions that ended
		if !slices.ContainsFunc(found, func(l claude.Live) bool { return l.SessionID == id }) {
			delete(d.live.meta, id)
		}
	}
	if joined {
		d.saveAdded()
	}
	d.live.mu.Unlock()

	// Sessions that ended (or were taken off the deck) leave the board; they
	// come back if they run again, as long as they stay added.
	for _, e := range d.Store.Entries() {
		if e.External && e.A.Tool == "claude" && !inApp(e) {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	for id, l := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, SessionID: l.SessionID, Transcript: l.Transcript, LiveTTY: l.TTY, LiveAttach: l.ShortID}
			e.A = model.Agent{ID: id, Tool: "claude", External: true, Status: model.Idle,
				Title: filepath.Base(l.Cwd), Cwd: l.Cwd, Folder: filepath.Base(l.Cwd)}
			d.Store.Add(e)
		}
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := claude.Tail(e) // tokens, context, model, title
			e.LiveTTY, e.LiveAttach = l.TTY, l.ShortID
			changed = setSubagents(e, l.Started) || changed // it is live: its subagents may be too
			changed = d.liveStatus(e, l) || changed
			return claude.FollowSubagents(e) || changed
		})
	}
}

// liveStatus reads a watched session's state off the end of its transcript:
// working until Claude's final reply of the turn, then idle (hungry once a
// turn finishes while we watch).
func (d *Daemon) liveStatus(e *store.Entry, l claude.Live) bool {
	turn, tool := claude.TranscriptTurn(e.Transcript, 10*time.Minute)
	running := turn
	changed := false
	if l.Reported { // Claude Code's own report beats reading the transcript
		// Busy with the main thread between turns is its subagents at work:
		// FollowSubagents shows that.
		running = l.Busy && (turn || len(e.A.Subagents) == 0)
		if l.Blocked {
			if e.A.Status != model.Waiting {
				e.SetStatus(model.Waiting)
				e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "input",
					Title: "needs you", Context: "answer it in its own terminal"}
				return true
			}
			return false
		}
		if e.A.Status == model.Waiting { // answered elsewhere
			e.SetStatus(model.Running)
			changed = true
		}
	}
	switch {
	case running && (e.A.Status != model.Running || e.Background):
		e.SetStatus(model.Running)
		e.Background = false // the main thread is in a turn again
		e.A.Activity = nil
		e.A.Unseen = false
		changed = true
	case !running && e.A.Status == model.Running && !e.Background:
		e.SetStatus(model.Idle) // also clears the activity
		e.A.Unseen = true       // a turn finished while we watched
		changed = true
	}
	if running {
		cur := ""
		if e.A.Activity != nil {
			cur = e.A.Activity.Tool
		}
		if cur != tool {
			e.A.Activity = nil
			if tool != "" {
				e.A.Activity = &model.Activity{Tool: tool}
			}
			changed = true
		}
	}
	return changed
}

// LiveJSON lists hand-started sessions for GET /v1/live.
func (d *Daemon) LiveJSON() any {
	d.live.mu.Lock()
	defer d.live.mu.Unlock()
	out := append([]LiveCandidate(nil), d.live.candidates...)
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// AddLive puts a hand-started session on the deck (and keeps it there whenever
// it runs), at board position slot when slot >= 0 (an agent there swaps out).
func (d *Daemon) AddLive(sessionID string, slot int) error {
	d.live.mu.Lock()
	found := false
	for _, c := range d.live.candidates {
		if c.SessionID == sessionID || len(sessionID) >= 4 && strings.HasPrefix(c.SessionID, sessionID) {
			sessionID, found = c.SessionID, true
			break
		}
	}
	if found {
		d.liveAdded()[sessionID] = true
		d.saveAdded()
		if d.live.offDeck[sessionID] {
			delete(d.live.offDeck, sessionID)
			d.saveOffDeck()
		}
	}
	d.live.mu.Unlock()
	if !found {
		return server.ErrNotFound
	}
	d.syncLive()
	if slot >= 0 {
		d.Store.Place(liveAgentID(sessionID), slot)
	}
	log.Printf("added live session %s to the deck", sessionID)
	return nil
}

// removeLive takes a watched session off the deck (it keeps running).
func (d *Daemon) removeLive(e store.Entry) {
	d.live.mu.Lock()
	delete(d.liveAdded(), e.SessionID)
	d.saveAdded()
	d.live.offDeck[e.SessionID] = true // with `sessions auto` it doesn't come back by itself
	d.saveOffDeck()
	d.live.mu.Unlock()
	d.Store.Remove(e.A.ID)
}

// discover lists hand-started and background Claude sessions: from Claude
// Code itself (`claude agents --json`, every 10 s), else by scanning
// processes (older Claude Code).
func (d *Daemon) discover(skip map[string]bool) []claude.Live {
	d.live.mu.Lock()
	if d.live.claudeBin == "" {
		d.live.claudeBin = LookPathLogin("claude")
		if d.live.claudeBin == "" {
			d.live.claudeBin = "-" // not installed: don't look again
		}
	}
	bin, fresh := d.live.claudeBin, time.Since(d.live.reportedAt) < 10*time.Second
	cached, ok := d.live.reported, d.live.reportedOK
	d.live.mu.Unlock()
	if !fresh && bin != "-" {
		cached, ok = activeSessions(bin, skip)
		d.live.mu.Lock()
		d.live.reported, d.live.reportedOK, d.live.reportedAt = cached, ok, time.Now()
		d.live.mu.Unlock()
	}
	if ok {
		return cached
	}
	return claude.LiveSessions(skip)
}

// focusLive brings up the terminal tab a watched session runs in; a
// background session opens in a new iTerm tab with `claude attach`.
func (d *Daemon) focusLive(e store.Entry) error {
	d.Store.Update(e.A.ID, func(x *store.Entry) bool { was := x.A.Unseen; x.A.Unseen = false; return was })
	if e.LiveTTY == "" && e.LiveAttach != "" {
		return d.attachBackground(e)
	}
	if e.LiveTTY == "" {
		return errors.New("its terminal is unknown")
	}
	how := "tab selected"
	if err := d.iterm.selectTTY(e.LiveTTY); err != nil {
		how = "window only (" + err.Error() + ")"
	}
	if err := term.StealFocus(e.LiveTTY); err != nil {
		return err
	}
	log.Printf("focus %s (live): %s", e.A.ID, how)
	return nil
}

// attachBackground opens a background session in a new iTerm tab
// (`claude attach <id>`, Claude Code's own way in).
func (d *Daemon) attachBackground(e store.Entry) error {
	d.live.mu.Lock()
	bin := d.live.claudeBin
	d.live.mu.Unlock()
	if bin == "" || bin == "-" || !shortIDRe.MatchString(e.LiveAttach) {
		return errors.New("can't open this background session")
	}
	cmd := shellJoin([]string{bin, "attach", e.LiveAttach})
	script := "tell application \"iTerm2\"\n activate\n if (count of windows) = 0 then create window with default profile\n tell current window to create tab with default profile command \"" +
		strings.ReplaceAll(cmd, `"`, `\"`) + "\"\nend tell"
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("iTerm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("focus %s (background): claude attach %s", e.A.ID, e.LiveAttach)
	return nil
}

var shortIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
