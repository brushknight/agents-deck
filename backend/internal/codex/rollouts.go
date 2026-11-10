package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// sqlite3 is the sqlite3 program: the system one on macOS, else the first on
// PATH; "" when there is none.
func sqlite3() string {
	if _, err := os.Stat("/usr/bin/sqlite3"); err == nil {
		return "/usr/bin/sqlite3"
	}
	if p, err := exec.LookPath("sqlite3"); err == nil {
		return p
	}
	return ""
}

// rolloutThreads lists the threads whose rollout log under dir
// (sessions/YYYY/MM/DD/rollout-*.jsonl) was written within window, newest
// first: id and folder from the log's first line (session_meta). Archived
// threads have moved out of dir, so they don't show.
func rolloutThreads(dir string, window time.Duration, now time.Time) ([]Thread, error) {
	since := now.Add(-window)
	var files []string
	// The day folders that can hold a recent log: the window, a day either side (time zones).
	for day := since.AddDate(0, 0, -1); !day.After(now.AddDate(0, 0, 1)); day = day.AddDate(0, 0, 1) {
		m, _ := filepath.Glob(filepath.Join(dir, day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		files = append(files, m...)
	}
	var ts []Thread
	seen := map[string]bool{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.ModTime().Before(since) {
			continue
		}
		id, cwd, branch := rolloutMeta(f)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ts = append(ts, Thread{ID: id, RolloutPath: f, Cwd: cwd, Branch: branch, UpdatedAtMs: st.ModTime().UnixMilli()})
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].UpdatedAtMs > ts[j].UpdatedAtMs })
	if len(ts) > 32 {
		ts = ts[:32]
	}
	return ts, nil
}

// rolloutMeta reads a rollout log's first line (session_meta).
func rolloutMeta(path string) (id, cwd, branch string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20) // the first line carries the instructions: it can be long
	if !sc.Scan() {
		return "", "", ""
	}
	var line struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
			Git struct {
				Branch string `json:"branch"`
			} `json:"git"`
		} `json:"payload"`
	}
	if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "session_meta" {
		return "", "", ""
	}
	return line.Payload.ID, line.Payload.Cwd, line.Payload.Git.Branch
}
