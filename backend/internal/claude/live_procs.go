package claude

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// liveFromProcesses matches Claude Code processes to their sessions without
// knowing their working folders (Windows can't tell another process's
// folder without reading its memory): a session id on the command line
// first, else the newest transcript in any of projects written since the
// process started. The folder then comes from the transcript itself.
func liveFromProcesses(ps []proc.Process, projects []string) []Live {
	type cand struct {
		l    Live
		argv []string
	}
	var cands []cand
	for _, p := range ps {
		argv := claudeArgv(p.Argv())
		if !isInteractiveClaude(argv) {
			continue
		}
		cands = append(cands, cand{Live{PID: p.PID, Started: p.Started}, argv})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].l.Started.Before(cands[j].l.Started) })
	claimed := map[string]bool{}
	var res []Live
	for _, explicitPass := range []bool{true, false} {
		for i := range cands {
			c := &cands[i]
			if c.l.SessionID != "" {
				continue
			}
			id := sessionArg(c.argv)
			if explicitPass != (id != "") {
				continue
			}
			path := ""
			if id != "" {
				path = transcriptAnywhere(projects, id)
			} else {
				id, path = newestAnywhere(projects, c.l.Started, claimed)
			}
			if id == "" || path == "" || claimed[id] {
				continue
			}
			info, _ := readSession(path, id)
			if info.Cwd == "" {
				continue
			}
			claimed[id] = true
			c.l.SessionID, c.l.Transcript, c.l.Cwd = id, path, info.Cwd
			res = append(res, c.l)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Started.Before(res[j].Started) })
	return res
}

// claudeArgv is argv as Claude Code's own: "claude" first, whether it runs
// as claude.exe (the native install) or as node running its cli.js (npm).
// nil when the process isn't Claude Code.
func claudeArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	switch strings.ToLower(baseName(argv[0])) {
	case "claude", "claude.exe":
		return append([]string{"claude"}, argv[1:]...)
	case "node", "node.exe":
		for i, a := range argv[1:] {
			a = strings.ToLower(strings.ReplaceAll(a, `\`, "/"))
			if strings.HasSuffix(a, "/claude-code/cli.js") {
				return append([]string{"claude"}, argv[i+2:]...)
			}
		}
	}
	return nil
}

// baseName is the last element of a path with / or \ separators, whatever
// the platform this runs on.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// transcriptAnywhere is the transcript of session id in any project folder.
func transcriptAnywhere(projects []string, id string) string {
	for _, dir := range projects {
		if m, _ := filepath.Glob(filepath.Join(dir, "*", id+".jsonl")); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

// newestAnywhere is the unclaimed session, in any project folder, most
// recently written since a process started.
func newestAnywhere(projects []string, since time.Time, claimed map[string]bool) (id, path string) {
	var bestT time.Time
	for _, dir := range projects {
		files, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
		for _, f := range files {
			sid := strings.TrimSuffix(filepath.Base(f), ".jsonl")
			if !IsSessionID(sid) || claimed[sid] {
				continue
			}
			st, err := os.Stat(f)
			if err != nil || st.ModTime().Before(since.Add(-time.Minute)) {
				continue
			}
			if st.ModTime().After(bestT) {
				id, path, bestT = sid, f, st.ModTime()
			}
		}
	}
	return id, path
}
