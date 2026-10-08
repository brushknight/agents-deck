package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// SessionInfo describes one Claude Code session found on disk.
type SessionInfo struct {
	ID         string    `json:"sessionId"`
	Cwd        string    `json:"cwd"`
	Branch     string    `json:"branch"`
	AITitle    string    `json:"aiTitle"`
	Custom     string    `json:"customTitle"`
	LastPrompt string    `json:"lastPrompt"`
	Modified   time.Time `json:"modified"`
	Size       int64     `json:"size"`
}

// Title is the best short name: a rename (unless it is just the folder),
// else Claude's own title, else the folder.
func (s SessionInfo) Title() string {
	if s.Custom != "" && s.Custom != filepath.Base(s.Cwd) {
		return s.Custom
	}
	if s.AITitle != "" {
		return s.AITitle
	}
	return filepath.Base(s.Cwd)
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsSessionID reports whether s is a full Claude session id.
func IsSessionID(s string) bool { return uuidRe.MatchString(s) }

// ProjectsDir is where Claude Code keeps session transcripts.
func ProjectsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// head/tail sizes: cwd sits on the first lines; title, last prompt and branch
// are re-written often enough to appear near the end.
const (
	headBytes = 64 << 10
	tailBytes = 512 << 10
)

// ScanSessions lists sessions under dir, newest first. Only top-level
// <project>/<uuid>.jsonl files count (subagent transcripts live deeper).
func ScanSessions(dir string) ([]SessionInfo, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		if !IsSessionID(id) {
			continue
		}
		if s, ok := readSession(f, id); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

type metaLine struct {
	Type       string `json:"type"`
	Cwd        string `json:"cwd"`
	GitBranch  string `json:"gitBranch"`
	AITitle    string `json:"aiTitle"`
	Custom     string `json:"customTitle"`
	LastPrompt string `json:"lastPrompt"`
}

func readSession(path, id string) (SessionInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return SessionInfo{}, false
	}
	s := SessionInfo{ID: id, Modified: st.ModTime(), Size: st.Size()}
	head := make([]byte, min(int64(headBytes), st.Size()))
	n, _ := io.ReadFull(f, head)
	scanMeta(head[:n], &s, true)
	if st.Size() > int64(n) {
		off := max(int64(n), st.Size()-tailBytes)
		tail := make([]byte, st.Size()-off)
		if m, err := f.ReadAt(tail, off); err == nil || err == io.EOF {
			tail = tail[:m]
			if i := bytes.IndexByte(tail, '\n'); i >= 0 && off > int64(n) {
				tail = tail[i+1:] // drop the partial first line
			}
			scanMeta(tail, &s, false)
		}
	}
	if s.Cwd == "" {
		return SessionInfo{}, false // not a conversation transcript
	}
	return s, true
}

// scanMeta folds metadata lines into s. In the head only the first cwd is
// taken; later lines (the tail) override title, prompt and branch.
func scanMeta(b []byte, s *SessionInfo, head bool) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap prefilter: skip lines that can't carry what we want.
		if !bytes.Contains(line, []byte(`"cwd"`)) && !bytes.Contains(line, []byte(`"aiTitle"`)) && !bytes.Contains(line, []byte(`"customTitle"`)) &&
			!bytes.Contains(line, []byte(`"lastPrompt"`)) && !bytes.Contains(line, []byte(`"gitBranch"`)) {
			continue
		}
		var m metaLine
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.Cwd != "" && (s.Cwd == "" || !head) {
			s.Cwd = m.Cwd
		}
		if m.GitBranch != "" {
			s.Branch = m.GitBranch
		}
		switch m.Type {
		case "ai-title":
			if m.AITitle != "" {
				s.AITitle = m.AITitle
			}
		case "custom-title":
			s.Custom = m.Custom
		case "last-prompt":
			if m.LastPrompt != "" {
				s.LastPrompt = clip(oneLine(m.LastPrompt), 200)
			}
		}
	}
}

// Match reports whether every word of query appears (case-insensitively) in the
// session's id, title, folder, branch or last prompt.
func (s SessionInfo) Match(query string) bool {
	hay := strings.ToLower(strings.Join([]string{s.ID, s.AITitle, s.Cwd, s.Branch, s.LastPrompt}, " "))
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// FindSession resolves a full id or a unique id prefix (≥ 4 chars).
func FindSession(all []SessionInfo, ref string) (SessionInfo, int) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	var hits []SessionInfo
	for _, s := range all {
		if s.ID == ref {
			return s, 1
		}
		if len(ref) >= 4 && strings.HasPrefix(s.ID, ref) {
			hits = append(hits, s)
		}
	}
	if len(hits) == 1 {
		return hits[0], 1
	}
	return SessionInfo{}, len(hits)
}

// Subagents lists the subagents of the session at transcript that are still
// working: their own transcript (<session>/subagents/agent-*.jsonl) was
// written within window and doesn't end with a final reply. Subagents can
// keep working while the main loop is idle (background agents, or another
// process on the same session) and fire no hook of ours.
func Subagents(transcript string, window time.Duration) []model.Subagent {
	if transcript == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "agent-*.jsonl"))
	cutoff := time.Now().Add(-window)
	type found struct {
		s     model.Subagent
		since time.Time
	}
	var out []found
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.ModTime().Before(cutoff) {
			continue
		}
		done, tool, start := subagentTail(f, st.Size())
		if done {
			continue
		}
		id := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(f), ".jsonl"), "agent-")
		s := model.Subagent{ID: id, Tool: tool}
		var meta struct {
			Type        string `json:"agentType"`
			Description string `json:"description"`
		}
		if b, err := os.ReadFile(strings.TrimSuffix(f, ".jsonl") + ".meta.json"); err == nil && json.Unmarshal(b, &meta) == nil {
			s.Title, s.Type = clip(oneLine(meta.Description), 80), meta.Type
		}
		if s.Title == "" {
			s.Title = "subagent"
		}
		out = append(out, found{s, start})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].since.Before(out[j].since) })
	subs := make([]model.Subagent, len(out))
	for i, f := range out {
		subs[i] = f.s
	}
	return subs
}

// subagentTail reads the end of a subagent transcript: whether its last
// message is a final reply (no tool call pending), the tool of its latest
// call, and roughly when it started (file birth time).
func subagentTail(path string, size int64) (done bool, tool string, start time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return true, "", start
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		start = birthTime(st)
	}
	off := max(0, size-256<<10)
	buf := make([]byte, size-off)
	n, _ := f.ReadAt(buf, off)
	lines := bytes.Split(bytes.TrimSpace(buf[:n]), []byte("\n"))
	var m struct {
		Type    string `json:"type"`
		Message *struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}
	decided := false
	for i := len(lines) - 1; i >= 0 && (tool == "" || !decided); i-- {
		m.Type, m.Message = "", nil
		if json.Unmarshal(lines[i], &m) != nil || m.Message == nil {
			continue
		}
		if !decided && (m.Type == "assistant" || m.Type == "user") {
			decided = true
			done = m.Type == "assistant" && m.Message.StopReason == "end_turn"
		}
		if m.Type == "assistant" && tool == "" {
			for _, c := range m.Message.Content {
				if c.Type == "tool_use" {
					tool = c.Name
				}
			}
		}
	}
	return done, tool, start
}
