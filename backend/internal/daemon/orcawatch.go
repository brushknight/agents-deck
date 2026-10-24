package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/orca"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// orcaWatch mirrors agents running in the Orca app onto the board: status
// from Orca (its own agent hooks), numbers from the agent's transcript, and
// focus / answer / interrupt through Orca's terminals.
type orcaWatch struct {
	mu     sync.Mutex
	client orcaAPI
	hidden map[string]int64 // pane key -> the agent's updatedAt when you hid it
}

// orcaAPI is what the deck asks of Orca (orca.Client; a fake in tests).
type orcaAPI interface {
	Read() (orca.Snapshot, error)
	Screen(handle string) (string, error)
	Send(handle, text string, enter bool) error
	Switch(handle string) error
	Front() error
}

func orcaAgentID(pane string) string {
	sum := sha256.Sum256([]byte("orca:" + pane))
	return "o" + hex.EncodeToString(sum[:])[:5]
}

// WatchOrca polls Orca every 2 seconds while it runs (no-op when it isn't
// installed, or with "orca": false in config.json).
func (d *Daemon) WatchOrca() {
	if d.Cfg.Orca != nil && !*d.Cfg.Orca {
		return
	}
	bin := orca.Bin()
	if bin == "" {
		return
	}
	d.orca.mu.Lock()
	d.orca.client = orca.Client{Bin: bin}
	d.orca.mu.Unlock()
	logged := ""
	for {
		if !orca.Running() {
			d.syncOrca(orca.Snapshot{})
		} else if snap, err := d.orcaClient().Read(); err != nil {
			if err.Error() != logged {
				log.Printf("orca: %v", err)
				logged = err.Error()
			}
		} else {
			logged = ""
			d.syncOrca(snap)
		}
		time.Sleep(2 * time.Second)
	}
}

func (d *Daemon) orcaClient() orcaAPI {
	d.orca.mu.Lock()
	defer d.orca.mu.Unlock()
	return d.orca.client
}

type orcaRow struct {
	a orca.Agent
	w orca.Worktree
	t orca.Terminal
}

func (d *Daemon) syncOrca(snap orca.Snapshot) {
	terms := map[string]orca.Terminal{}
	for _, t := range snap.Terminals {
		terms[t.PaneKey()] = t
	}
	d.orca.mu.Lock()
	hidden := d.orca.hidden
	d.orca.mu.Unlock()
	want := map[string]orcaRow{}
	for _, w := range snap.Worktrees {
		for _, a := range w.Agents {
			t, ok := terms[a.PaneKey]
			if !ok || !t.Connected {
				continue // its terminal is gone or asleep
			}
			if h, ok := hidden[a.PaneKey]; ok && h >= a.UpdatedAt {
				continue
			}
			want[orcaAgentID(a.PaneKey)] = orcaRow{a, w, t}
		}
	}
	for _, e := range d.Store.Entries() {
		if e.OrcaPane != "" {
			if _, ok := want[e.A.ID]; !ok {
				d.Store.Remove(e.A.ID)
			}
		}
	}
	for id, r := range want {
		if _, ok := d.Store.Get(id); !ok {
			e := &store.Entry{External: true, OrcaPane: r.a.PaneKey, OrcaTerminal: r.t.Handle}
			e.A = model.Agent{ID: id, Tool: orcaTool(r.a.AgentType), External: true, App: "orca", Status: model.Idle,
				Title: orcaTitle(r), Cwd: r.w.Path, Folder: filepath.Base(r.w.Path), Branch: orcaBranch(r.w.Branch)}
			d.Store.Add(e)
		}
		// A new question: its options are on the agent's screen (Orca sends
		// only a one-line summary of the tool call). Read it outside the lock.
		var screen string
		if cur, ok := d.Store.Get(id); ok && (r.a.State == "blocked" || r.a.State == "waiting") && cur.OrcaTurn != r.a.StateStartedAt {
			screen, _ = d.orcaClient().Screen(r.t.Handle)
		}
		session := d.orcaSession(r.w.Path)
		d.Store.Update(id, func(e *store.Entry) bool {
			changed := false
			if e.OrcaTerminal != r.t.Handle {
				e.OrcaTerminal = r.t.Handle
			}
			if session.SessionID != "" && e.SessionID != session.SessionID {
				e.SessionID, e.Transcript, e.Offset, e.Usage, e.Pending = session.SessionID, session.Transcript, 0, nil, ""
				changed = true
			}
			if e.Transcript != "" && e.A.Tool == "claude" && claude.Tail(e) { // model, context, tokens, cost
				changed = true
			}
			if t := orcaTitle(r); t != e.A.Title && e.A.AITitle == "" {
				e.A.Title, changed = t, true
			}
			if b := orcaBranch(r.w.Branch); b != "" && b != e.A.Branch {
				e.A.Branch, changed = b, true
			}
			if r.a.Prompt != "" && r.a.Prompt != e.A.LastPrompt {
				e.A.LastPrompt, changed = clipText(r.a.Prompt, 280), true
			}
			if orcaApply(e, r.a, screen) {
				changed = true
			}
			if subs := orcaSubagents(r.a); !subagentsEqual(subs, e.A.Subagents) {
				e.A.Subagents, changed = subs, true
			}
			return changed
		})
	}
}

// orcaSession is the Claude session running in an Orca worktree, from
// Claude Code's own list (for the transcript), when exactly one runs there.
func (d *Daemon) orcaSession(path string) claude.Live {
	d.live.mu.Lock()
	defer d.live.mu.Unlock()
	var found claude.Live
	n := 0
	for _, l := range d.live.reported {
		if l.Cwd == path {
			found, n = l, n+1
		}
	}
	if n != 1 {
		return claude.Live{}
	}
	return found
}

// orcaApply maps Orca's state onto the agent; screen (may be "") is the
// terminal's text when a new prompt appeared.
func orcaApply(e *store.Entry, a orca.Agent, screen string) bool {
	changed := false
	switch {
	case a.State == "working" && !a.Interrupted:
		if e.A.Status != model.Running {
			e.SetStatus(model.Running)
			e.A.Unseen = false
			changed = true
		}
		var act *model.Activity
		if a.ToolName != "" {
			act = &model.Activity{Tool: a.ToolName, Detail: clipText(a.ToolInput, 200)}
		}
		if !activityEqual(act, e.A.Activity) {
			e.A.Activity, changed = act, true
		}
	case a.State == "blocked" || a.State == "waiting":
		if e.A.Status != model.Waiting || e.OrcaTurn != a.StateStartedAt {
			e.SetStatus(model.Waiting)
			e.A.Waiting = orcaPrompt(e, a, screen)
			e.OrcaTurn = a.StateStartedAt
			changed = true
		}
	default: // done (or interrupted)
		if e.A.Status != model.Idle {
			if e.A.Status == model.Running || e.A.Status == model.Waiting {
				e.A.Unseen = true // a turn finished while we watched
			}
			e.SetStatus(model.Idle)
			changed = true
		}
	}
	return changed
}

// orcaPrompt builds what the agent is asking, with the options read off its
// screen when they're there.
func orcaPrompt(e *store.Entry, a orca.Agent, screen string) *model.Prompt {
	if a.ToolName == "" {
		detail := a.LastAssistantMessage
		p := &model.Prompt{ID: e.NextPromptID(), Kind: "input", Title: "needs you", Detail: clipText(detail, 2000),
			Context: "answer it in Orca"}
		if opts := claude.ParseOptions(screen); len(opts) >= 2 {
			p.Kind, p.Options = "question", opts
		}
		return p
	}
	p := claude.PromptFor(e, a.ToolName, orcaToolInput(a.ToolName, a.ToolInput))
	p.Context = strings.TrimSuffix(p.Context, " · Orca") + " · in Orca"
	if opts := claude.ParseOptions(screen); len(opts) >= 2 {
		p.Options = opts
		if p.Kind == "input" {
			p.Kind = "question"
		}
	} else if p.Kind == "question" && len(p.Options) == 0 {
		p.Kind = "input"
	}
	if p.Kind == "question" && p.Title == "" {
		p.Title = strings.ToLower(clipText(a.ToolInput, 200))
	}
	return p
}

// orcaToolInput rebuilds a tool input from Orca's one-line summary (the
// command, path, url or pattern), enough for the prompt's title and detail.
func orcaToolInput(tool, summary string) json.RawMessage {
	key := "command"
	switch tool {
	case "Read", "Edit", "MultiEdit", "Write", "NotebookEdit":
		key = "file_path"
	case "WebFetch":
		key = "url"
	case "WebSearch":
		key = "query"
	case "Grep", "Glob":
		key = "pattern"
	case "AskUserQuestion":
		b, _ := json.Marshal(map[string]any{"questions": []map[string]any{{"question": summary}}})
		return b
	}
	b, _ := json.Marshal(map[string]string{key: summary})
	return b
}

func orcaSubagents(a orca.Agent) []model.Subagent {
	var out []model.Subagent
	for _, s := range a.Subagents {
		if s.State == "done" || s.State == "completed" {
			continue
		}
		out = append(out, model.Subagent{ID: s.ID, Title: s.Description, Type: s.AgentType})
	}
	return out
}

func subagentsEqual(a, b []model.Subagent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func activityEqual(a, b *model.Activity) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func orcaTool(agentType string) string {
	switch t := strings.ToLower(agentType); {
	case strings.Contains(t, "claude"):
		return "claude"
	case strings.Contains(t, "codex"):
		return "codex"
	case strings.Contains(t, "gemini"):
		return "gemini"
	case strings.Contains(t, "opencode"):
		return "opencode"
	case t == "" || t == "unknown":
		return "claude"
	default:
		return t
	}
}

func orcaTitle(r orcaRow) string {
	if r.w.DisplayName != "" {
		return r.w.DisplayName
	}
	return filepath.Base(r.w.Path)
}

func orcaBranch(b string) string { return strings.TrimPrefix(b, "refs/heads/") }

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// ---- actions --------------------------------------------------------------

// orcaFocus shows the agent's terminal in Orca and brings Orca forward.
func (d *Daemon) orcaFocus(e store.Entry) error {
	d.Store.Update(e.A.ID, func(x *store.Entry) bool { was := x.A.Unseen; x.A.Unseen = false; return was })
	if err := d.orcaClient().Switch(e.OrcaTerminal); err != nil {
		return err
	}
	_ = d.orcaClient().Front()
	log.Printf("focus %s: orca terminal", e.A.ID)
	return nil
}

// orcaSend types the option's key into the agent's Orca terminal.
func (d *Daemon) orcaSend(e store.Entry, key string) error {
	return d.orcaClient().Send(e.OrcaTerminal, key, false)
}

// orcaInterrupt sends Esc, which stops the agent's current turn.
func (d *Daemon) orcaInterrupt(e store.Entry) error {
	return d.orcaClient().Send(e.OrcaTerminal, "\x1b", false)
}

// orcaHide keeps an Orca agent off the board until it's active again.
func (d *Daemon) orcaHide(e store.Entry) {
	d.orca.mu.Lock()
	if d.orca.hidden == nil {
		d.orca.hidden = map[string]int64{}
	}
	d.orca.hidden[e.OrcaPane] = time.Now().UnixMilli()
	d.orca.mu.Unlock()
	d.Store.Remove(e.A.ID)
}
