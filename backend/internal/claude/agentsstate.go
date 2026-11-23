package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// StateWatch tells, without running anything, when `claude agents --json`
// may answer differently for an account. Claude Code keeps a small file for
// every running session (sessions/<pid>.json: its id, folder, name and
// status) and one for every background job (jobs/<id>/state.json), and
// writes them when a session starts, ends, is renamed, starts or ends a
// turn, or a job needs you. Starting the claude CLI costs about a quarter of
// a second of CPU; looking at those files costs next to nothing.
//
// Not safe for concurrent use.
type StateWatch struct {
	files map[string]stateFile // path -> what it held when last read
}

type stateFile struct {
	size      int64
	mod       time.Time
	sessionID string
}

// Key sums up the session and job files of the Claude config folder dir:
// it changes whenever one is added, removed or rewritten, or a session's
// process ends without removing its file. Sessions for which ignore is true
// (the deck's own agents, already watched through their hooks) don't count.
// ok is false when dir has no sessions folder (a Claude Code that keeps
// them elsewhere, or none yet): the key then tells nothing.
func (w *StateWatch) Key(dir string, ignore func(sessionID string) bool) (key string, ok bool) {
	if w.files == nil {
		w.files = map[string]stateFile{}
	}
	sessions := filepath.Join(dir, "sessions")
	ents, err := os.ReadDir(sessions)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	seen := map[string]bool{}
	for _, de := range ents { // ReadDir sorts by name
		name := de.Name()
		if !strings.HasSuffix(name, ".json") || de.IsDir() {
			continue
		}
		path := filepath.Join(sessions, name)
		info, err := de.Info()
		if err != nil {
			continue // removed meanwhile
		}
		seen[path] = true
		f, known := w.files[path]
		if !known || f.size != info.Size() || !f.mod.Equal(info.ModTime()) {
			f = stateFile{size: info.Size(), mod: info.ModTime(), sessionID: fileSessionID(path)}
			w.files[path] = f
		}
		if f.sessionID != "" && ignore != nil && ignore(f.sessionID) {
			continue
		}
		alive := true
		if pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json")); err == nil {
			alive = proc.Alive(pid)
		}
		fmt.Fprintf(&b, "s %s %d %d %t\n", name, f.size, f.mod.UnixNano(), alive)
	}
	for p := range w.files {
		if !seen[p] {
			delete(w.files, p)
		}
	}
	jobs, _ := filepath.Glob(filepath.Join(dir, "jobs", "*", "state.json"))
	sort.Strings(jobs)
	for _, p := range jobs {
		if info, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "j %s %d %d\n", filepath.Base(filepath.Dir(p)), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String(), true
}

// fileSessionID is the session id a session file names ("" when it can't
// be read, e.g. half written: it is read again once it changes).
func fileSessionID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var f struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &f) != nil {
		return ""
	}
	return f.SessionID
}
