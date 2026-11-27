// Package copilot shows GitHub Copilot CLI sessions on the board, read-only.
// Copilot keeps one event log per session under ~/.copilot/session-state;
// a session is listed while its own inuse lock belongs to a live process.
// Message and permission text is never read into the board.
package copilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Home is Copilot's data folder.
func Home() string {
	if d := os.Getenv("COPILOT_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".copilot")
}

// Session is one live Copilot CLI session, without its messages.
type Session struct {
	ID         string
	Title      string
	Cwd        string
	Model      string
	ModelLabel string
	Activity   string
	Status     model.Status
	Tokens     model.Tokens
	Context    model.Context
	Cost       float64
	LastActive time.Time
	Since      time.Time
	Turns      int
}

// AgentID derives a stable board id from a Copilot session id.
func AgentID(id string) string {
	sum := sha256.Sum256([]byte("copilot:" + id))
	return "p" + hex.EncodeToString(sum[:])[:5]
}

// Tracker keeps the parsed tail of each live session's event log.
type Tracker struct {
	mu     sync.Mutex
	tails  map[string]*tail
	sums   map[string]summary
	sumsAt time.Time
}

func NewTracker() *Tracker { return &Tracker{tails: map[string]*tail{}} }

// Sessions lists live sessions with recent activity, or a pending permission.
func (t *Tracker) Sessions(window time.Duration, now time.Time) []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := activeSessions()
	for id := range t.tails {
		if _, ok := active[id]; !ok {
			delete(t.tails, id)
		}
	}
	if t.sums == nil || now.Sub(t.sumsAt) > 30*time.Second {
		t.sums = summaries()
		t.sumsAt = now
	}
	var out []Session
	for id, events := range active {
		tl := t.tails[id]
		if tl == nil {
			tl = &tail{pending: map[string]bool{}}
			t.tails[id] = tl
		}
		if err := tl.update(events); err != nil {
			continue
		}
		s := tl.session(id, t.sums[id], now)
		if now.Sub(s.LastActive) > window && len(tl.pending) == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}

// activeSessions maps session ids to their event logs, for sessions whose
// own inuse lock names a live process.
func activeSessions() map[string]string {
	root := filepath.Join(Home(), "session-state")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		locks, _ := filepath.Glob(filepath.Join(root, d.Name(), "inuse.*.lock"))
		live := false
		for _, lock := range locks {
			pid := lockPID(lock)
			if pid > 0 && proc.Alive(pid) {
				live = true
				break
			}
		}
		if !live {
			continue
		}
		events := filepath.Join(root, d.Name(), "events.jsonl")
		if _, err := os.Stat(events); err == nil {
			out[d.Name()] = events
		}
	}
	return out
}

func lockPID(path string) int {
	name := filepath.Base(path) // inuse.<pid>.lock
	name = strings.TrimPrefix(name, "inuse.")
	name = strings.TrimSuffix(name, ".lock")
	pid, err := strconv.Atoi(name)
	if err != nil {
		return 0
	}
	return pid
}

type summary struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Cwd     string `json:"cwd"`
	Branch  string `json:"branch"`
	Updated string `json:"updated_at"`
	Turns   int    `json:"turns"`
}

func summaries() map[string]summary {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil
	}
	db := filepath.Join(Home(), "session-store.db")
	if _, err := os.Stat(db); err != nil {
		return nil
	}
	q := "select s.id, coalesce(s.summary,'') as summary, coalesce(s.cwd,'') as cwd, " +
		"coalesce(s.branch,'') as branch, coalesce(s.updated_at,'') as updated_at, " +
		"(select count(*) from turns t where t.session_id=s.id) as turns from sessions s"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-readonly", "-json", db, q)
	proc.Tidy(cmd)
	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil
	}
	var rows []summary
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	m := map[string]summary{}
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

type event struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type tail struct {
	offset        int64
	pending       map[string]bool
	turn          bool
	turnSince     time.Time
	pendingSince  time.Time
	cwd           string
	model         string
	modelLabel    string
	activity      string
	tokens        model.Tokens
	contextUsed   int64
	contextWindow int64
	cost          float64
	lastActive    time.Time
}

func (t *tail) update(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < t.offset {
		*t = tail{pending: map[string]bool{}}
	}
	if st.Size() == t.offset {
		return nil
	}
	if _, err := f.Seek(t.offset, 0); err != nil {
		return err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil
	}
	complete := data[:end+1]
	t.offset += int64(len(complete))
	for _, line := range bytes.Split(complete, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e event
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		t.apply(e)
	}
	return nil
}

func (t *tail) apply(e event) {
	at, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
	if at.After(t.lastActive) {
		t.lastActive = at
	}
	switch e.Type {
	case "session.start":
		var d struct {
			Context struct {
				Cwd string `json:"cwd"`
			} `json:"context"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.Context.Cwd != "" {
			t.cwd = d.Context.Cwd
		}
	case "session.model_change":
		var d struct {
			NewModel string `json:"newModel"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.NewModel != "" {
			t.model = d.NewModel
			t.modelLabel = shortModel(d.NewModel)
		}
	case "assistant.message":
		var d struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.Model != "" {
			t.model = d.Model
			t.modelLabel = shortModel(d.Model)
		}
	case "session.usage_record":
		var d struct {
			Usage struct {
				Model           string  `json:"model"`
				ModelDisplay    string  `json:"modelDisplayName"`
				InputTokens     int64   `json:"inputTokens"`
				OutputTokens    int64   `json:"outputTokens"`
				CacheRead       int64   `json:"cacheReadTokens"`
				CacheWrite      int64   `json:"cacheWriteTokens"`
				Cost            float64 `json:"cost"`
				MaxPromptTokens int64   `json:"maxPromptTokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(e.Data, &d) == nil {
			u := d.Usage
			t.tokens = model.Tokens{Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
			t.cost = u.Cost
			t.contextUsed = u.InputTokens + u.CacheRead
			if u.MaxPromptTokens > 0 {
				t.contextWindow = u.MaxPromptTokens
			}
			if u.Model != "" {
				t.model = u.Model
				t.modelLabel = shortModel(u.Model)
			} else if u.ModelDisplay != "" {
				t.modelLabel = u.ModelDisplay
			}
		}
	case "session.compaction_start":
		var d struct {
			CurrentTokens int64 `json:"currentTokens"`
			TokenLimit    int64 `json:"tokenLimit"`
		}
		if json.Unmarshal(e.Data, &d) == nil {
			if d.CurrentTokens > 0 {
				t.contextUsed = d.CurrentTokens
			}
			if d.TokenLimit > 0 {
				t.contextWindow = d.TokenLimit
			}
		}
	case "assistant.turn_start":
		t.turn = true
		t.turnSince = at
		t.activity = ""
	case "assistant.turn_end":
		t.turn = false
		t.activity = ""
	case "permission.requested":
		var d struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(e.Data, &d) == nil && d.RequestID != "" {
			if len(t.pending) == 0 {
				t.pendingSince = at
			}
			t.pending[d.RequestID] = true
		}
	case "permission.completed", "human_response.recorded":
		var d struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(e.Data, &d) == nil {
			delete(t.pending, d.RequestID)
			if len(t.pending) == 0 {
				t.pendingSince = time.Time{}
			}
		}
	case "tool.execution_start":
		var d struct {
			ToolName string `json:"toolName"`
		}
		if json.Unmarshal(e.Data, &d) == nil {
			t.activity = d.ToolName
		}
	case "tool.execution_complete":
		t.activity = ""
	case "session.shutdown":
		t.turn = false
		t.turnSince = time.Time{}
		t.pending = map[string]bool{}
		t.pendingSince = time.Time{}
		t.activity = ""
	}
}

func (t *tail) session(id string, s summary, now time.Time) Session {
	status := model.Idle
	active := t.lastActive
	if active.IsZero() && s.Updated != "" {
		active, _ = time.Parse(time.RFC3339Nano, s.Updated)
	}
	switch {
	case len(t.pending) > 0:
		status = model.Waiting
	case t.turn && now.Sub(active) < 10*time.Minute:
		status = model.Running
	}
	cwd := t.cwd
	if cwd == "" {
		cwd = s.Cwd
	}
	title := s.Summary
	if title == "" {
		title = filepath.Base(cwd)
	}
	since := active
	switch status {
	case model.Waiting:
		if !t.pendingSince.IsZero() {
			since = t.pendingSince
		}
	case model.Running:
		if !t.turnSince.IsZero() {
			since = t.turnSince
		}
	}
	return Session{
		ID: id, Title: title, Cwd: cwd, Model: t.model, ModelLabel: t.modelLabel,
		Activity: t.activity, Status: status, Tokens: t.tokens,
		Context: model.Context{Used: t.contextUsed, Window: t.contextWindow},
		Cost:    t.cost, LastActive: active, Since: since, Turns: s.Turns,
	}
}

func shortModel(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
