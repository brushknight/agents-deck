package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

// ActiveSessions asks Claude Code itself for every active session on this
// machine, interactive and background (`claude agents --json`), minus those
// on skipTTYs. ok is false when the claude CLI can't answer (missing, too
// old): then use [LiveSessions].
func ActiveSessions(claudeBin string, skipTTYs map[string]bool) (out []Live, ok bool) {
	if claudeBin == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBin, "agents", "--json")
	cmd.Dir = os.TempDir() // no project folder: nothing to trust or read
	raw, err := cmd.Output()
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
		l.Transcript = findTranscript(a.Cwd, a.SessionID)
		out = append(out, l)
	}
	return out, true
}

// ttysOf maps the interactive sessions' processes to their terminals.
func ttysOf(list []claudeAgent) map[int]string {
	var pids []string
	for _, a := range list {
		if a.PID > 0 && a.Kind != "background" {
			pids = append(pids, strconv.Itoa(a.PID))
		}
	}
	ttys := map[int]string{}
	if len(pids) == 0 {
		return ttys
	}
	out, _ := exec.Command("/bin/ps", "-o", "pid=,tty=", "-p", strings.Join(pids, ",")).Output()
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[1] != "??" {
			if pid, err := strconv.Atoi(f[0]); err == nil {
				ttys[pid] = "/dev/" + f[1]
			}
		}
	}
	return ttys
}

// findTranscript is a session's transcript: in its folder's project
// directory, else wherever it was written (a worktree, a moved folder).
func findTranscript(cwd, sessionID string) string {
	p := filepath.Join(ProjectDirFor(cwd), sessionID+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if m, _ := filepath.Glob(filepath.Join(ProjectsDir(), "*", sessionID+".jsonl")); len(m) > 0 {
		return m[0]
	}
	return p
}

// ProjectDirFor is Claude's transcript folder for a working directory: the
// path with every character other than a letter or digit turned into "-".
func ProjectDirFor(cwd string) string {
	return filepath.Join(ProjectsDir(), nonAlnum.ReplaceAllString(cwd, "-"))
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// LiveSessions lists interactive `claude` processes that have a terminal and
// aren't on one of skipTTYs (agentctl's own tmux panes), each matched to its
// session transcript. Processes whose transcript can't be found are left out.
func LiveSessions(skipTTYs map[string]bool) []Live {
	out, err := exec.Command("/bin/ps", "-axww", "-o", "pid=,lstart=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	var procs []Live
	var args = map[int][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 { // pid, 5 lstart fields, tty, argv0
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		tty, argv := f[6], f[7:]
		if tty == "??" || tty == "-" || !isInteractiveClaude(argv) {
			continue
		}
		dev := "/dev/" + tty
		if skipTTYs[dev] {
			continue
		}
		started, _ := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(f[1:6], " "), time.Local)
		procs = append(procs, Live{PID: pid, TTY: dev, Started: started})
		args[pid] = argv
	}
	if len(procs) == 0 {
		return nil
	}
	cwds := processCwds(procs)
	claimed := map[string]bool{}
	// Explicit session ids first, then the oldest processes pick first.
	sort.Slice(procs, func(i, j int) bool { return procs[i].Started.Before(procs[j].Started) })
	var res []Live
	for _, explicitPass := range []bool{true, false} {
		for i := range procs {
			p := &procs[i]
			if p.SessionID != "" {
				continue
			}
			p.Cwd = cwds[p.PID]
			if p.Cwd == "" {
				continue
			}
			id := sessionArg(args[p.PID])
			if explicitPass != (id != "") {
				continue
			}
			if id == "" {
				id = newestTranscript(ProjectDirFor(p.Cwd), p.Started, claimed)
			}
			if id == "" || claimed[id] {
				continue
			}
			claimed[id] = true
			p.SessionID = id
			p.Transcript = filepath.Join(ProjectDirFor(p.Cwd), id+".jsonl")
			res = append(res, *p)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Started.Before(res[j].Started) })
	return res
}

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

// processCwds asks lsof for the working directories of the given processes.
func processCwds(procs []Live) map[int]string {
	pids := make([]string, len(procs))
	for i, p := range procs {
		pids[i] = strconv.Itoa(p.PID)
	}
	out, _ := exec.Command("/usr/sbin/lsof", "-a", "-d", "cwd", "-p", strings.Join(pids, ","), "-Fpn").Output()
	cwds := map[int]string{}
	pid := 0
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			pid, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "n") && pid != 0:
			cwds[pid] = l[1:]
		}
	}
	return cwds
}
