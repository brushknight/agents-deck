// Package daemon is the live controller: it launches agents in tmux, folds
// hook events and transcripts into the store, and performs actions.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/term"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

type Config struct {
	// ClaudeArgs are appended to every claude launch, e.g. ["--model", "opus"].
	ClaudeArgs []string `json:"claudeArgs"`
	// Codex: mirror Codex app/CLI threads onto the board (default on).
	Codex *bool `json:"codex"`
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
	lastFocus time.Time
	iterm     itermLink
	reporting map[string]bool // ttys we asked to report focus
	codex     codexWatch
	front     string // tmux session of the focused terminal (last tick)
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
	return d, nil
}

// Run reconciles tmux, transcripts and the front iTerm tab once a second.
func (d *Daemon) Run() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	for range tick.C {
		n++
		live := tmux.Sessions()
		// The focused agent: the session of the most recently active client whose
		// terminal reports focus (tmux focus-events; no AppleScript needed).
		clients := map[string][]tmux.Client{}
		front := ""
		list := tmux.ClientList() // most recent first
		d.enableFocusReports(list)
		for _, c := range list {
			clients[c.Session] = append(clients[c.Session], c)
			if front == "" && c.Focused {
				front = c.Session
			}
		}
		d.mu.Lock()
		d.front = front
		d.mu.Unlock()
		var gone []string
		d.Store.Each(func(e *store.Entry) bool {
			if e.External {
				return false // mirrored by WatchCodex
			}
			changed := claude.Tail(e)
			alive := live[e.TmuxName]
			young := time.Since(e.A.StartedAt) < 3*time.Second // tmux session still being created
			switch {
			case !alive && !young && e.A.Status != model.Exited:
				e.SetStatus(model.Exited)
				changed = true
			case !alive && e.A.Status == model.Exited && d.exitedTTL() > 0 && time.Since(e.A.StatusSince) > d.exitedTTL():
				remember(*e)
				gone = append(gone, e.A.ID)
			case alive && e.A.Status == model.Exited:
				e.SetStatus(model.Idle) // e.g. the daemon restarted while tmux was briefly unreachable
				changed = true
			}
			if n%2 == 0 && alive && e.A.Tool == "claude" && d.subagents(e) {
				changed = true
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
			if focused && e.A.Unseen {
				e.A.Unseen = false // you looked at it
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
				// A permission/question prompt whose options we only guessed: read the screen.
				if claude.RefineOptions(e.A.Waiting, tmux.Capture(e.TmuxName, 40)) {
					changed = true
				}
			}
			return changed
		})
		for _, id := range gone {
			d.Store.Remove(id)
			log.Printf("removed exited agent %s", id)
		}
	}
}

// enableFocusReports asks every newly seen client terminal to report focus
// changes, so the focused-agent highlight follows tab clicks, not just typing.
func (d *Daemon) enableFocusReports(list []tmux.Client) {
	if d.reporting == nil {
		d.reporting = map[string]bool{}
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

// subagents tracks the session's working subagents, and shows an idle agent
// as running while any are left, back to idle once they are done or quiet.
func (d *Daemon) subagents(e *store.Entry) bool {
	if e.A.Status != model.Running {
		e.Background = false
	}
	subs := claude.Subagents(e.Transcript, time.Minute)
	changed := !slices.Equal(subs, e.A.Subagents)
	e.A.Subagents = subs
	switch {
	case len(subs) > 0 && e.A.Status == model.Idle:
		e.SetStatus(model.Running)
		e.Background = true
		changed = true
	case len(subs) == 0 && e.Background:
		e.SetStatus(model.Idle)
		e.Background = false
		changed = true
	}
	if e.Background {
		detail := "1 subagent working"
		if len(subs) != 1 {
			detail = fmt.Sprintf("%d subagents working", len(subs))
		}
		if e.A.Activity == nil || e.A.Activity.Detail != detail {
			e.A.Activity = &model.Activity{Tool: "Task", Detail: detail}
			changed = true
		}
	}
	return changed
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

// scanMenu turns an on-screen arrow-key menu into a prompt, and clears the
// prompt again once the menu is gone.
func (d *Daemon) scanMenu(e *store.Entry) bool {
	title, opts, ok := claude.ParseCursorMenu(tmux.Capture(e.TmuxName, 0))
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
		if e.A.Unseen && front == e.TmuxName {
			e.A.Unseen = false // finished while you were watching it
		}
		return changed
	}) {
		return server.ErrNotFound
	}
	if scan {
		// The dialog renders a moment after the hook fires.
		go func() {
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
		inner = []string{os.Getenv("SHELL")}
		if inner[0] == "" {
			inner[0] = "/bin/zsh"
		}
		e.A.Status = model.Idle
	default:
		return "", fmt.Errorf("unknown tool %q", tool)
	}
	// A login shell gives the agent the user's normal PATH and environment.
	argv := []string{"/bin/zsh", "-lc", "exec " + shellJoin(inner)}
	if tool == "claude" {
		argv = d.claudeArgv(explicitTitle(*e), []string{"--session-id", e.SessionID})
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

// Answer types the chosen option into the agent's terminal.
func (d *Daemon) Answer(id, promptID, key string) error {
	if e, ok := d.Store.Get(id); ok && e.External {
		return errExternal
	}
	var tmuxName string
	var label string
	var keys []string
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
		err = tmux.SendKeys(tmuxName, "", keys...)
	} else {
		err = tmux.SendKeys(tmuxName, key)
	}
	if err != nil {
		return err
	}
	log.Printf("answered %s prompt %s with option %s", id, promptID, key)
	d.Store.Update(id, func(e *store.Entry) bool {
		if e.A.Waiting == nil || e.A.Waiting.ID != promptID {
			return false
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
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	if e.External {
		return errExternal
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
	d.Store.Each(func(e *store.Entry) bool {
		if e.External {
			return false
		}
		e.Offset, e.Usage, e.Pending = 0, nil, "" // rebuild totals from the transcript
		if !live[e.TmuxName] {
			e.SetStatus(model.Exited)
		}
		return true
	})
}

func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// LookPathLogin resolves a command through the user's login shell PATH.
func LookPathLogin(name string) string {
	out, err := exec.Command("/bin/zsh", "-lc", "command -v "+shellJoin([]string{name})).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
