// Package daemon is the live controller: it launches agents in tmux, folds
// hook events and transcripts into the store, and performs actions.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/header"
	"github.com/brushknight/agents-deck/backend/internal/limits"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/term"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

type Config struct {
	// TmuxSocket names the deck's tmux server (tmux -L), "agentsterm" when
	// empty. A second deck on one computer (a test instance) sets its own.
	TmuxSocket string `json:"tmuxSocket,omitempty"`
	// ClaudeArgs are appended to every claude launch, e.g. ["--model", "opus"].
	ClaudeArgs []string `json:"claudeArgs"`
	// Codex: mirror Codex app/CLI threads onto the board (default on).
	Codex *bool `json:"codex"`
	// UsageEverySession: the deck's status line is in every Claude
	// account's settings.json, so every session reports its usage limits
	// (Settings, off by default).
	UsageEverySession bool `json:"usageEverySession"`
	// Orca: mirror agents running in the Orca app onto the board (default on).
	Orca *bool `json:"orca"`
	// Herdr: mirror agents running in herdr onto the board (default on).
	Herdr *bool `json:"herdr"`
	// Cmux: mirror Claude agents running in cmux onto the board (default on;
	// cmux's socket access must be Automation).
	Cmux *bool `json:"cmux"`
	// Copilot: mirror live GitHub Copilot CLI sessions onto the board,
	// read-only (default on).
	Copilot *bool `json:"copilot"`
	// CodexWindowHours: threads updated within this many hours are shown (default 6).
	CodexWindowHours float64 `json:"codexWindowHours"`
	// ExitedTTLSeconds: how long exited agents stay visible (0 = 10 min, <0 = forever).
	ExitedTTLSeconds int `json:"exitedTTLSeconds"`
}

type Daemon struct {
	Store    *store.Store
	Cfg      Config
	Agentctl string // absolute path of this binary, used in hook commands

	mu        sync.Mutex
	scans     sync.WaitGroup // screen reads started by Hook, so tests can wait for them
	lastFocus time.Time
	iterm     itermLink
	reporting map[string]bool                 // ttys we asked to report focus
	headers   map[string][header.Lines]string // tmux session -> header lines it shows
	codex     codexWatch
	orca      orcaWatch
	herdr     herdrWatch
	cmux      cmuxWatch
	copilot   copilotWatch
	front     string          // tmux session of the focused terminal (last tick)
	panes     map[string]bool // our panes' terminals (last tick)
	panesAt   time.Time
	screens   screenCache // Run's goroutine only

	lastAutoRestore time.Time
	live            liveWatch
	limits          *limits.Book   // usage limits the tools report (limits.go)
	limitsKeep      *limits.Keeper // writes them to limits.json
}

func New(st *store.Store, cfg Config, agentctl string) (*Daemon, error) {
	d := &Daemon{Store: st, Cfg: cfg, Agentctl: agentctl}
	b, err := claude.HooksSettings(agentctl)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(paths.HooksFile(), b, 0o600); err != nil {
		return nil, err
	}
	d.startLimits()
	return d, nil
}

// Run reconciles tmux, transcripts and the front iTerm tab once a second.
func (d *Daemon) Run() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	accKey := accountsKey()
	for range tick.C {
		n++
		if k := accountsKey(); k != accKey { // accounts set up or changed: redraw the dots
			accKey = k
			d.Store.Touch()
		}
		d.tick(n)
	}
}

// tick is one second of Run (n counts them).
func (d *Daemon) tick(n int) {
	st := tmux.Look() // one call, not one per list
	activity := st.Activity
	d.screens.look(activity, time.Now())
	live := make(map[string]bool, len(activity))
	for name := range activity {
		live[name] = true
	}
	// The focused agent: the session of the most recently active client whose
	// terminal reports focus (tmux focus-events; no AppleScript needed).
	clients := map[string][]tmux.Client{}
	front := ""
	list := st.Clients // most recent first
	d.enableFocusReports(list)
	for _, c := range list {
		clients[c.Session] = append(clients[c.Session], c)
		if front == "" && c.Focused {
			front = c.Session
		}
	}
	d.mu.Lock()
	d.front = front
	d.panes, d.panesAt = st.PaneTTYs, time.Now()
	d.mu.Unlock()
	var gone, died []string
	d.Store.Each(func(e *store.Entry) bool {
		if e.External {
			return false // mirrored by WatchCodex
		}
		changed := claude.Tail(e)
		alive := live[e.TmuxName]
		if alive && tmuxActive(*e, activity[e.TmuxName]) && e.MarkActiveStep(activity[e.TmuxName]) {
			changed = true
		}
		young := time.Since(e.A.StartedAt) < 3*time.Second // tmux session still being created
		switch {
		case !alive && !young && e.A.Status != model.Exited:
			e.SetStatus(model.Exited)
			died = append(died, e.A.ID)
			changed = true
		case !alive && e.A.Status == model.Exited && !e.A.Lost && d.exitedTTL() > 0 && time.Since(e.A.StatusSince) > d.exitedTTL():
			remember(*e)
			gone = append(gone, e.A.ID)
		case alive && e.A.Status == model.Exited:
			e.SetStatus(model.Idle) // e.g. the daemon restarted while tmux was briefly unreachable
			e.A.Lost = false
			changed = true
		}
		if n%2 == 0 && alive && e.A.Tool == "claude" {
			if d.subagents(e, e.RunSince()) {
				changed = true
			}
			// A turn that ended without its hook (Esc, a restart) ends here;
			// while subagents work, they keep it running (see subagents).
			if !e.Sim && len(e.A.Subagents) == 0 && claude.Settle(e, time.Now()) {
				changed = true
			}
		} else if !alive && e.A.Subagents != nil {
			e.A.Subagents = nil
			changed = true
		}
		if r := e.A.Status == model.Exited && e.A.Tool == "claude" && e.SessionID != "" && !e.Sim; r != e.A.Resumable {
			e.A.Resumable = r
			changed = true
		}
		attached := len(clients[e.TmuxName]) > 0
		focused := front != "" && front == e.TmuxName
		if lookedAt(e, focused) {
			changed = true
		}
		if attached != e.A.Attached || focused != e.A.Focused {
			e.A.Attached, e.A.Focused = attached, focused
			changed = true
		}
		// Dialogs that fire no hook (workspace trust before SessionStart): read the screen.
		if alive && (e.A.Status == model.Starting || e.Menu) && time.Since(e.A.StartedAt) > 2*time.Second {
			if d.scanMenu(e) {
				changed = true
			}
		} else if alive && e.A.Status == model.Waiting && e.A.Waiting != nil && n%2 == 1 {
			if d.askOnScreen(e) {
				changed = true
			} else if claude.RefineOptions(e.A.Waiting, d.screens.read(e.TmuxName, 40)) {
				// A permission/question prompt whose options we only guessed: read the screen.
				changed = true
			}
		}
		return changed
	})
	for _, id := range gone {
		d.Store.Remove(id)
		log.Printf("removed exited agent %s", id)
	}
	// Several agents gone at once and no tmux server left: it crashed or
	// was killed, the agents didn't quit. (One agent quitting is normal,
	// and the server exits by itself after its last session.)
	if len(died) >= 2 && !st.Running {
		d.markLost(died, "tmux server stopped")
	}
	d.syncHeaders(live)
}

// tmuxActive reports whether tmux's last-output time counts as activity for
// an agent: only for tools without a transcript (a shell, Codex, Gemini or
// opencode started here), and not the output of starting up (a shell's first
// prompt is not something you did).
func tmuxActive(e store.Entry, at time.Time) bool {
	if e.A.Tool == "claude" || e.Transcript != "" || at.IsZero() {
		return false
	}
	return at.Sub(e.A.StartedAt) > startupOutput
}

// startupOutput is how long after an agent starts its output is still just
// the tool starting.
const startupOutput = 10 * time.Second

// enableFocusReports asks every newly seen client terminal to report focus
// changes, so the focused-agent highlight follows tab clicks, not just typing.
func (d *Daemon) enableFocusReports(list []tmux.Client) {
	if d.reporting == nil {
		d.reporting = map[string]bool{}
		tmux.NativeMouse = func() bool { return CurrentMouse() == MouseNative }
		tmux.ServerDefaults() // agents started by an older daemon get them too
	}
	seen := map[string]bool{}
	for _, c := range list {
		seen[c.TTY] = true
		if !d.reporting[c.TTY] {
			if err := term.ReportFocus(c.TTY); err == nil {
				d.reporting[c.TTY] = true
			}
		}
	}
	for t := range d.reporting {
		if !seen[t] {
			delete(d.reporting, t) // detached: ask again if it comes back
		}
	}
}

// subagents reads which of the session's subagents work now (since: when
// its Claude Code process started) and follows them in its status: an agent
// whose main thread is between turns runs while any are left, and is idle
// (unseen) once the last one is done. Call it only while the process lives.
func (d *Daemon) subagents(e *store.Entry, since time.Time) bool {
	changed := setSubagents(e, since)
	return claude.FollowSubagents(e) || changed
}

// lookedAt clears unseen when the agent's terminal is the focused one: you
// see what it finished. It reports whether the agent changed.
func lookedAt(e *store.Entry, focused bool) bool {
	if !focused || !e.A.Unseen {
		return false
	}
	e.A.Unseen = false
	return true
}

// setSubagents reads the session's working subagents into the agent.
func setSubagents(e *store.Entry, since time.Time) bool {
	subs := claude.Subagents(e.Transcript, since, time.Now())
	if slices.Equal(subs, e.A.Subagents) {
		return false
	}
	e.A.Subagents = subs
	return true
}

// exitedTTL is how long an exited agent stays on the board, resumable, before
// it moves to the history (config exitedTTLSeconds; default 10 min, negative =
// keep until dismissed).
func (d *Daemon) exitedTTL() time.Duration {
	switch {
	case d.Cfg.ExitedTTLSeconds < 0:
		return 0
	case d.Cfg.ExitedTTLSeconds == 0:
		return 10 * time.Minute
	}
	return time.Duration(d.Cfg.ExitedTTLSeconds) * time.Second
}

// askOnScreen follows a question (AskUserQuestion) the agent waits on from
// its screen: answers typed in the terminal move a form along, and a
// question answered or cancelled there closes the prompt (cancelling fires
// no hook). It reports whether the agent changed.
func (d *Daemon) askOnScreen(e *store.Entry) bool {
	if e.A.Waiting.Kind != "question" || time.Since(e.A.StatusSince) < formSettle {
		return false // permissions have their own reading; a new dialog may not be drawn yet
	}
	f := claude.ActiveForm(e)
	if f != nil && time.Since(f.Typed) < formSettle {
		return false
	}
	screen := d.screens.read(e.TmuxName, 0)
	if closed, answered := claude.AskClosed(screen); closed {
		if answered {
			e.SetStatus(model.Running) // PostToolUse follows
		} else {
			e.SetStatus(model.Idle) // declined: Claude's turn ends
		}
		return true
	}
	return f != nil && claude.SyncForm(e, screen)
}

// scanMenu turns an on-screen arrow-key menu into a prompt, and clears the
// prompt again once the menu is gone.
func (d *Daemon) scanMenu(e *store.Entry) bool {
	title, opts, ok := claude.ParseCursorMenu(d.screens.read(e.TmuxName, 0))
	switch {
	case ok && !e.Menu:
		e.SetStatus(model.Waiting)
		e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "permission", Title: title,
			Context: claude.ShortPath(e.A.Cwd), Options: opts}
		e.Menu = true
		return true
	case !ok && e.Menu:
		e.Menu = false
		if e.A.Status == model.Waiting {
			e.SetStatus(model.Starting)
		}
		return true
	}
	return false
}

// Hook folds one Claude Code hook payload into its agent.
func (d *Daemon) Hook(agentID string, body []byte) error {
	var ev claude.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if agentID == "" {
		agentID = d.Store.FindBySession(ev.SessionID)
	}
	scan := false
	d.mu.Lock()
	front := d.front
	d.mu.Unlock()
	if !d.Store.Update(agentID, func(e *store.Entry) bool {
		var changed bool
		changed, scan = claude.Apply(e, ev)
		lookedAt(e, front != "" && front == e.TmuxName) // finished while you were watching it
		return changed
	}) {
		return server.ErrNotFound
	}
	if scan {
		// The dialog renders a moment after the hook fires.
		d.scans.Add(1)
		go func() {
			defer d.scans.Done()
			for _, wait := range []time.Duration{250 * time.Millisecond, 900 * time.Millisecond} {
				time.Sleep(wait)
				d.Store.Update(agentID, func(e *store.Entry) bool {
					return e.A.Waiting != nil && claude.RefineOptions(e.A.Waiting, tmux.Capture(e.TmuxName, 40))
				})
			}
		}()
	}
	return nil
}

// Create starts a new agent in a detached tmux session and returns its id.
func (d *Daemon) Create(o server.CreateOpts) (string, error) {
	dir, title, tool := o.Dir, o.Title, o.Tool
	if dir == "" {
		return "", errors.New("dir required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	if tool == "" {
		tool = "claude"
	}
	titleSet := title != ""
	if title == "" {
		title = filepath.Base(abs) // placeholder until Claude names the session
	} else {
		title = d.uniqueTitle(title)
	}
	id := store.NewID()
	e := &store.Entry{TmuxName: "a-" + id}
	e.A = model.Agent{ID: id, Title: title, Tool: tool, Status: model.Starting, Cwd: abs, Folder: filepath.Base(abs)}
	e.TitleSet = titleSet

	var inner []string
	switch tool {
	case "claude":
		e.SessionID = store.NewUUID()
		acc := accounts.Default()
		if o.Account != "" {
			a, ok := accounts.Find(o.Account)
			if !ok {
				return "", fmt.Errorf("no account named %q", o.Account)
			}
			acc = a
		}
		e.Account = acc.Name
		inner = nil // built by claudeArgv below
	case "codex", "gemini", "opencode":
		inner = []string{tool}
	case "sim":
		e.Sim, e.A.Tool = true, "claude" // looks like Claude everywhere
		e.Transcript = filepath.Join(paths.Data(), "sim", id+".jsonl")
		speed := o.Speed
		if speed <= 0 {
			speed = 1
		}
		patience := o.Patience
		if patience == 0 {
			patience = 45
		}
		inner = []string{d.Agentctl, "sim-agent", "--dir", abs, "--speed", fmt.Sprint(speed),
			"--patience", fmt.Sprint(patience), "--transcript", e.Transcript}
	case "shell":
		inner = []string{proc.LoginShell()}
		e.A.Status = model.Idle
	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
	// A login shell gives the agent the user's normal PATH and environment.
	argv := loginArgv(inner)
	if tool == "claude" {
		argv = d.claudeArgv(explicitTitle(*e), accountOf(*e), []string{"--session-id", e.SessionID})
	}
	env := agentEnv(id)
	d.Store.Add(e)
	if err := tmux.NewSession(e.TmuxName, abs, env, argv); err != nil {
		d.Store.Remove(id)
		return "", err
	}
	if tool != "claude" && tool != "shell" && tool != "sim" {
		// No hooks for other tools: show them as running until they exit.
		d.Store.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Running); return true })
	}
	return id, nil
}

func (d *Daemon) uniqueTitle(base string) string {
	taken := map[string]bool{}
	for _, a := range d.Store.Snapshot().Agents {
		if a.Status != model.Exited {
			taken[a.Title] = true
		}
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		if t := fmt.Sprintf("%s-%d", base, i); !taken[t] {
			return t
		}
	}
}

// formSettle is how long Claude Code takes to redraw a form after a key; the
// screen isn't checked against the deck's step before that.
const formSettle = 1500 * time.Millisecond

// Answer types the chosen option into the agent's terminal.
func (d *Daemon) Answer(id, promptID, key string) error {
	return d.stampActive(id, d.answer(id, promptID, key))
}

// stampActive logs an answer or interrupt sent from the deck as the agent's
// latest activity, when it went through.
func (d *Daemon) stampActive(id string, err error) error {
	if err == nil {
		now := time.Now().UTC()
		d.Store.Update(id, func(e *store.Entry) bool { return e.MarkActive(now) })
	}
	return err
}

func (d *Daemon) answer(id, promptID, key string) error {
	if e, ok := d.Store.Get(id); ok {
		if h, ok := d.harnessOf(e); ok {
			return d.answerInApp(h, id, promptID, key)
		}
	}
	if e, ok := d.Store.Get(id); ok && e.External {
		return externalErr(e)
	}
	// A question form: check the screen still shows the step the deck asked.
	var screen string
	if e, ok := d.Store.Get(id); ok && claude.ActiveForm(&e) != nil && time.Since(e.Form.Typed) > formSettle {
		screen = tmux.Capture(e.TmuxName, 0)
	}
	var tmuxName string
	var label string
	var keys []string
	var form bool
	text := key
	err := errors.New("")
	ok := d.Store.Update(id, func(e *store.Entry) bool {
		p := e.A.Waiting
		if e.A.Status != model.Waiting || p == nil || p.ID != promptID {
			err = server.ErrStalePrompt
			return false
		}
		for _, o := range p.Options {
			if o.Key == key {
				label = o.Label
			}
		}
		if label == "" {
			err = server.ErrBadKey
			return false
		}
		if f := claude.ActiveForm(e); f != nil {
			if screen != "" && claude.SyncForm(e, screen) {
				err = server.ErrStalePrompt // answered in the terminal meanwhile: show where it is now
				return true
			}
			t, k, valid := claude.FormKeys(f, key)
			if !valid {
				err = server.ErrBadKey
				return false
			}
			err, tmuxName, form, text, keys = nil, e.TmuxName, true, t, k
			return false
		}
		err, tmuxName = nil, e.TmuxName
		if e.Menu {
			idx := 0
			for i, o := range p.Options {
				if o.Key == key {
					idx = i
				}
			}
			for range p.Options {
				keys = append(keys, "Up")
			}
			for i := 0; i < idx; i++ {
				keys = append(keys, "Down")
			}
			keys = append(keys, "Enter")
		}
		return false
	})
	if !ok {
		return server.ErrNotFound
	}
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		err = tmux.SendKeys(tmuxName, text, keys...)
	} else {
		err = tmux.SendKeys(tmuxName, text)
	}
	if err != nil {
		return err
	}
	log.Printf("answered %s prompt %s with option %s", id, promptID, key)
	d.Store.Update(id, func(e *store.Entry) bool {
		if e.A.Waiting == nil || e.A.Waiting.ID != promptID {
			return false
		}
		if form && claude.ActiveForm(e) != nil {
			claude.AdvanceForm(e, key)
			return true
		}
		if e.Menu {
			e.Menu = false
			e.SetStatus(model.Starting)
		} else if strings.HasPrefix(label, "no") {
			e.SetStatus(model.Idle) // Claude stops and waits for instructions
		} else {
			e.SetStatus(model.Running)
		}
		return true
	})
	return nil
}

// Interrupt sends Esc, which stops Claude's current turn.
func (d *Daemon) Interrupt(id string) error {
	return d.stampActive(id, d.interrupt(id))
}

func (d *Daemon) interrupt(id string) error {
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	if h, ok := d.harnessOf(e); ok {
		return h.interrupt(e)
	}
	if e.External {
		return externalErr(e)
	}
	if err := tmux.SendKeys(e.TmuxName, "", "Escape"); err != nil {
		return err
	}
	d.Store.Update(id, func(e *store.Entry) bool {
		if e.A.Status == model.Running || e.A.Status == model.Waiting {
			e.SetStatus(model.Idle)
			return true
		}
		return false
	})
	return nil
}

// Focus shows the agent's terminal on the Mac.
func (d *Daemon) Focus(id string) error {
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	if h, ok := d.harnessOf(e); ok {
		return h.focus(e)
	}
	if e.External && e.A.Tool == "claude" {
		return d.focusLive(e)
	}
	if e.External {
		return d.focusExternal(e)
	}
	if e.A.Status == model.Exited {
		return errors.New("agent has exited")
	}
	d.mu.Lock()
	if time.Since(d.lastFocus) < 500*time.Millisecond { // double taps
		d.mu.Unlock()
		return nil
	}
	d.lastFocus = time.Now()
	d.mu.Unlock()
	d.Store.Update(id, func(e *store.Entry) bool {
		was := e.A.Unseen
		e.A.Unseen = false
		return was
	})

	all := tmux.ClientList() // most recently active first
	var own *tmux.Client
	for i := range all {
		if all[i].Session == e.TmuxName && (own == nil || all[i].Focused && !own.Focused) {
			own = &all[i]
		}
	}
	switch mode := CurrentTerm(); mode {
	case TermTmux:
		// One-tab workflow: show the agent in the terminal used last.
		var front *tmux.Client
		for i := range all {
			if all[i].Focused {
				front = &all[i]
				break
			}
		}
		if front == nil && len(all) > 0 {
			front = &all[0]
		}
		if front != nil {
			if front.Session != e.TmuxName {
				if err := tmux.SwitchClient(front.TTY, e.TmuxName); err != nil {
					return fmt.Errorf("focus: %v", err)
				}
			}
			if err := term.StealFocus(front.TTY); err != nil {
				return fmt.Errorf("focus: %v", err)
			}
			log.Printf("focus %s [tmux]: %s switched to it", id, front.TTY)
			return nil
		}
	default: // TermITerm, TermWindow
		if own != nil {
			how := "window"
			if mode == TermITerm {
				// Select the agent's tab and pane through iTerm's API…
				how = "tab selected"
				if err := d.iterm.selectTTY(own.TTY); err != nil {
					how = "window only (" + err.Error() + ")"
				}
			}
			// …and StealFocus brings the window up (also a hidden hotkey window).
			if err := term.StealFocus(own.TTY); err != nil {
				return fmt.Errorf("focus: %v", err)
			}
			log.Printf("focus %s [%s]: %s, %s", id, mode, own.TTY, how)
			return nil
		}
	}
	return fmt.Errorf("not open in any terminal — run `agentctl attach %s` once", id)
}

// Dismiss forgets an agent; a live one is stopped first.
func (d *Daemon) Dismiss(id string) error {
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	if h, ok := d.harnessOf(e); ok {
		h.hide(e) // off the deck until it's active again; it keeps running in its app
		return nil
	}
	if e.External && e.A.Tool == "claude" {
		d.removeLive(e) // off the deck; the session keeps running
		return nil
	}
	if e.External {
		d.hideExternal(e)
		return nil
	}
	if e.A.Status != model.Exited {
		_ = tmux.Kill(e.TmuxName)
	}
	remember(e)
	if e.Sim && e.Transcript != "" {
		_ = os.Remove(e.Transcript)
	}
	d.Store.Remove(id)
	return nil
}

// Adopt marks restored agents whose tmux session is gone as exited.
func (d *Daemon) Adopt() {
	live := tmux.Sessions()
	server := tmux.ServerRunning()
	var lost []string
	d.Store.Each(func(e *store.Entry) bool {
		if e.External {
			return false
		}
		e.Offset, e.Usage, e.Pending = 0, nil, "" // rebuild totals from the transcript
		if !live[e.TmuxName] {
			// Alive when the daemon last saw it, gone now with no tmux server:
			// lost with the server or the machine (reboot), not quit.
			if e.A.Status != model.Exited && !server && !e.Sim {
				lost = append(lost, e.A.ID)
			}
			e.SetStatus(model.Exited)
		}
		return true
	})
	d.markLost(lost, "not running after the daemon started")
}

func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}
