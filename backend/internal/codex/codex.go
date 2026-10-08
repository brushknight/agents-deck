// Package codex shows sessions of the Codex app (and Codex CLI) on the board,
// read-only and without any wrapper or login: thread names come from Codex's
// own state database (~/.codex/state_*.sqlite, opened read-only through the
// system sqlite3), live status from each thread's rollout log
// (~/.codex/sessions/…/rollout-*.jsonl).
package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Home is Codex's data folder.
func Home() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// Thread is one row of Codex's threads table.
type Thread struct {
	ID          string `json:"id"`
	RolloutPath string `json:"rollout_path"`
	Cwd         string `json:"cwd"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	Branch      string `json:"git_branch"`
	Model       string `json:"model"`
	UpdatedAtMs int64  `json:"updated_at_ms"`
}

// stateDB is the newest state_<n>.sqlite (the number is a schema version).
func stateDB() string {
	m, _ := filepath.Glob(filepath.Join(Home(), "state_*.sqlite"))
	sort.Strings(m)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

// RecentThreads lists unarchived threads updated within window, newest first.
func RecentThreads(window time.Duration) ([]Thread, error) {
	db := stateDB()
	if db == "" {
		return nil, nil
	}
	since := time.Now().Add(-window).UnixMilli()
	q := "select id, rollout_path, cwd, coalesce(title,'') as title, coalesce(name,'') as name, " +
		"coalesce(git_branch,'') as git_branch, coalesce(model,'') as model, coalesce(updated_at_ms,0) as updated_at_ms " +
		"from threads where coalesce(archived,0)=0 and coalesce(updated_at_ms,0) >= " + itoa(since) +
		" order by updated_at_ms desc limit 32"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/sqlite3", "-readonly", "-json", db, q).Output()
	if err != nil {
		return nil, err
	}
	var ts []Thread
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	return ts, json.Unmarshal(out, &ts)
}

// AgentID derives a stable board id ([a-z0-9]{6}) from a Codex thread id.
func AgentID(threadID string) string {
	sum := sha256.Sum256([]byte("codex:" + threadID))
	return "x" + hex.EncodeToString(sum[:])[:5]
}

// DisplayTitle: the user's name for the thread, else Codex's title, else the folder.
func (t Thread) DisplayTitle() string {
	switch {
	case t.Name != "":
		return t.Name
	case t.Title != "":
		return t.Title
	}
	return filepath.Base(t.Cwd)
}

const maxRead = 8 << 20

type rolloutLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type payload struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Input  string `json:"input"`
	Args   string `json:"arguments"`
	Role   string `json:"role"`
	Model  string `json:"model"`
	Window int64  `json:"model_context_window"`
	Info   *struct {
		Total struct {
			Input  int64 `json:"input_tokens"`
			Cached int64 `json:"cached_input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"total_token_usage"`
		Last struct {
			Input int64 `json:"input_tokens"`
		} `json:"last_token_usage"`
		Window int64 `json:"model_context_window"`
	} `json:"info"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Tail folds rollout lines appended since the last call into the agent.
func Tail(e *store.Entry) bool {
	f, err := os.Open(e.Transcript)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	if st.Size() < e.Offset {
		e.Offset, e.Pending = 0, ""
	}
	if st.Size() == e.Offset {
		return false
	}
	first := e.Offset == 0
	skipPartial := false
	if first && st.Size() > maxRead {
		// Long threads grow rollouts past a gigabyte. Status, model and the
		// (cumulative) token counts all come from recent lines, so start at
		// the tail instead of replaying the whole history.
		e.Offset, skipPartial = st.Size()-maxRead, true
	}
	if _, err := f.Seek(e.Offset, io.SeekStart); err != nil {
		return false
	}
	buf, _ := io.ReadAll(io.LimitReader(f, maxRead))
	if skipPartial {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return false
		}
		e.Offset += int64(i + 1)
		buf = buf[i+1:]
	}
	e.Offset += int64(len(buf))
	data := append([]byte(e.Pending), buf...)
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		e.Pending = string(data)
		return false
	}
	e.Pending = string(data[last+1:])

	sc := bufio.NewScanner(bytes.NewReader(data[:last]))
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	changed := false
	for sc.Scan() {
		var l rolloutLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		var p payload
		if json.Unmarshal(l.Payload, &p) != nil {
			continue
		}
		changed = apply(e, l.Type, p, first) || changed
	}
	return changed
}

func apply(e *store.Entry, kind string, p payload, replay bool) bool {
	switch kind {
	case "turn_context":
		if p.Model != "" {
			e.A.Model, e.A.ModelLabel = p.Model, strings.TrimPrefix(p.Model, "gpt-")
			return true
		}
	case "event_msg":
		switch p.Type {
		case "task_started":
			e.SetStatus(model.Running)
			e.A.Unseen = false
			if p.Window > 0 {
				e.A.Context.Window = p.Window
			}
			return true
		case "task_complete":
			e.SetStatus(model.Idle)
			// A turn that finished before we started watching isn't news.
			e.A.Unseen = !replay
			return true
		case "turn_aborted":
			e.SetStatus(model.Idle)
			return true
		case "token_count":
			if p.Info == nil {
				return false
			}
			e.A.Tokens = model.Tokens{Input: p.Info.Total.Input - p.Info.Total.Cached, CacheRead: p.Info.Total.Cached, Output: p.Info.Total.Output}
			if p.Info.Last.Input > 0 { // the closing count of a turn reports 0
				e.A.Context.Used = p.Info.Last.Input
			}
			if p.Info.Window > 0 {
				e.A.Context.Window = p.Info.Window
			}
			return true
		}
	case "response_item":
		switch p.Type {
		case "message":
			if p.Role != "user" {
				return false
			}
			for i := len(p.Content) - 1; i >= 0; i-- {
				t := strings.TrimSpace(p.Content[i].Text)
				if t != "" && !strings.HasPrefix(t, "<") {
					e.A.LastPrompt = clip(strings.Join(strings.Fields(t), " "), 280)
					e.A.Turns++
					return true
				}
			}
		case "function_call", "custom_tool_call":
			return toolCall(e, p)
		case "function_call_output", "custom_tool_call_output":
			if e.A.Status == model.Waiting {
				e.SetStatus(model.Running) // the question was answered
				return true
			}
		case "reasoning":
			if e.A.Status == model.Running && e.A.Activity != nil {
				e.A.Activity = nil // thinking between tools
				return true
			}
		}
	}
	return false
}

// toolCall maps Codex tools onto the board's activity vocabulary.
func toolCall(e *store.Entry, p payload) bool {
	arg := p.Input
	if arg == "" {
		arg = p.Args
	}
	switch p.Name {
	case "request_user_input_async", "request_user_input":
		e.SetStatus(model.Waiting)
		e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "input", Title: question(arg), Context: "answer in the Codex app"}
		return true
	case "wait", "sleep":
		return false
	case "apply_patch":
		e.SetStatus(model.Running)
		e.A.Activity = &model.Activity{Tool: "Edit", Detail: "apply patch"}
	case "exec", "shell", "exec_command", "js":
		e.SetStatus(model.Running)
		e.A.Activity = &model.Activity{Tool: "Bash", Detail: clip(firstLine(arg), 160)}
	default:
		e.SetStatus(model.Running)
		e.A.Activity = &model.Activity{Tool: p.Name}
	}
	return true
}

func question(arg string) string {
	var in map[string]any
	if json.Unmarshal([]byte(arg), &in) == nil {
		for _, k := range []string{"question", "prompt", "message", "title"} {
			if s, ok := in[k].(string); ok && s != "" {
				return clip(s, 200)
			}
		}
	}
	return "codex is asking you something"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
