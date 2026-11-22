package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Live is a Claude Code session running in some terminal on this machine,
// started by hand rather than by agentctl.
type Live struct {
	PID        int       `json:"pid"`
	TTY        string    `json:"tty"` // /dev/ttys012; empty for background sessions
	Cwd        string    `json:"cwd"`
	Started    time.Time `json:"started"`
	SessionID  string    `json:"sessionId"`
	Transcript string    `json:"-"`

	// From `claude agents --json` (empty with the process-scan fallback).
	Name       string `json:"name,omitempty"`
	Background bool   `json:"background,omitempty"` // a background session: `claude attach <ShortID>`
	ShortID    string `json:"shortId,omitempty"`
	Busy       bool   `json:"busy,omitempty"`    // working on a turn
	Blocked    bool   `json:"blocked,omitempty"` // waiting for you (permission, question)
	Done       bool   `json:"done,omitempty"`    // a background session that finished
	Reported   bool   `json:"-"`                 // status came from Claude Code itself
}

// claudeAgent is one entry of `claude agents --json`.
type claudeAgent struct {
	ID        string `json:"id"` // short id, background sessions only
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"` // interactive | background
	StartedAt int64  `json:"startedAt"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	State     string `json:"state"` // background: blocked | done | …
	PID       int    `json:"pid"`
	Status    string `json:"status"` // idle | busy
}

// ActiveSessions asks Claude Code itself for every active session of one
// account, interactive and background (`claude agents --json`), minus those
// on skipTTYs. configDir is the account's CLAUDE_CONFIG_DIR ("" = unset: the
// default ~/.claude); projects are the transcript folders to look in, the
// account's own first. ok is false when the claude CLI can't answer
// (missing, too old): then use [LiveSessions].
func ActiveSessions(claudeBin, configDir string, projects []string, skipTTYs map[string]bool) (out []Live, ok bool) {
	if claudeBin == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBin, "agents", "--json")
	cmd.Dir = os.TempDir() // no project folder: nothing to trust or read
	cmd.Env = ConfigEnv(os.Environ(), configDir)
	proc.Tidy(cmd) // and on Windows no console window flashing up every few seconds
	raw, err := proc.Timed(cmd, (*exec.Cmd).Output)
	if err != nil {
		return nil, false
	}
	var list []claudeAgent
	if json.Unmarshal(raw, &list) != nil {
		return nil, false
	}
	ttys := ttysOf(list)
	for _, a := range list {
		if !IsSessionID(a.SessionID) || a.Cwd == "" {
			continue
		}
		l := Live{PID: a.PID, TTY: ttys[a.PID], Cwd: a.Cwd, SessionID: a.SessionID,
			Name: a.Name, Background: a.Kind == "background", ShortID: a.ID,
			Busy: a.Status == "busy", Blocked: a.State == "blocked", Done: a.State == "done", Reported: true}
		if a.StartedAt > 0 {
			l.Started = time.UnixMilli(a.StartedAt)
		}
		if l.TTY != "" && skipTTYs[l.TTY] {
			continue
		}
		l.Transcript = findTranscript(projects, a.Cwd, a.SessionID)
		out = append(out, l)
	}
	return out, true
}

// ttysOf maps the interactive sessions' processes to their terminals.
func ttysOf(list []claudeAgent) map[int]string {
	var pids []int
	for _, a := range list {
		if a.PID > 0 && a.Kind != "background" {
			pids = append(pids, a.PID)
		}
	}
	if len(pids) == 0 {
		return map[int]string{}
	}
	return processTTYs(pids)
}

// ConfigEnv is env with CLAUDE_CONFIG_DIR set to configDir, or removed when
// configDir is "" (Claude Code's default folder).
func ConfigEnv(env []string, configDir string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	if configDir != "" {
		out = append(out, "CLAUDE_CONFIG_DIR="+configDir)
	}
	return out
}

// findTranscript is a session's transcript: in its folder's project
// directory, else wherever it was written (a worktree, a moved folder),
// looking in each of projects in turn.
func findTranscript(projects []string, cwd, sessionID string) string {
	if len(projects) == 0 {
		projects = []string{ProjectsDir()}
	}
	for _, dir := range projects {
		p := filepath.Join(dir, projectName(cwd), sessionID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, dir := range projects {
		if m, _ := filepath.Glob(filepath.Join(dir, "*", sessionID+".jsonl")); len(m) > 0 {
			return m[0]
		}
	}
	return filepath.Join(projects[0], projectName(cwd), sessionID+".jsonl")
}

// ProjectDirFor is Claude's transcript folder for a working directory: the
// path with every character other than a letter or digit turned into "-".
func ProjectDirFor(cwd string) string {
	return filepath.Join(ProjectsDir(), projectName(cwd))
}

func projectName(cwd string) string { return nonAlnum.ReplaceAllString(cwd, "-") }

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// isInteractiveClaude: argv of Claude Code's interactive CLI, not one of its
// helpers (background pty hosts, spares, the daemon) or a one-shot -p run.
func isInteractiveClaude(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := filepath.Base(argv[0])
	if base != "claude" && base != "claude.exe" {
		return false
	}
	for _, a := range argv[1:] {
		if strings.HasSuffix(a, "agents-terminal/claude-hooks.json") {
			return false // started by agentctl (its own hooks file)
		}
		switch a {
		case "-p", "--print", "bg-pty-host", "--bg-pty-host", "bg-spare", "--bg-spare", "daemon", "mcp", "--version", "-v", "update", "doctor":
			return false
		}
	}
	return true
}

// sessionArg is the session id given on the command line, if any.
func sessionArg(argv []string) string {
	for i, a := range argv {
		for _, flag := range []string{"--resume", "-r", "--session-id"} {
			if a == flag && i+1 < len(argv) && IsSessionID(argv[i+1]) {
				return argv[i+1]
			}
			if v, ok := strings.CutPrefix(a, flag+"="); ok && IsSessionID(v) {
				return v
			}
		}
	}
	return ""
}

// newestTranscript is the session in dir most recently written since the
// process started (a fresh or resumed session writes as soon as it runs).
func newestTranscript(dir string, since time.Time, claimed map[string]bool) string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	best, bestT := "", time.Time{}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		if !IsSessionID(id) || claimed[id] {
			continue
		}
		st, err := os.Stat(f)
		if err != nil || st.ModTime().Before(since.Add(-time.Minute)) {
			continue
		}
		if st.ModTime().After(bestT) {
			best, bestT = id, st.ModTime()
		}
	}
	return best
}

// FindTranscript looks for a session's transcript in each of projects (see
// findTranscript); ok is false when none of them has it.
func FindTranscript(projects []string, cwd, sessionID string) (path string, ok bool) {
	p := findTranscript(projects, cwd, sessionID)
	_, err := os.Stat(p)
	return p, err == nil
}
