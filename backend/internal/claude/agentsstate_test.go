package claude

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStateWatchKey(t *testing.T) {
	dir := t.TempDir()
	var w StateWatch
	if _, ok := w.Key(dir, nil); ok {
		t.Fatal("no sessions folder: the key tells nothing")
	}
	sessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string, mod time.Time) {
		t.Helper()
		p := filepath.Join(sessions, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	key := func(ignore func(string) bool) string {
		t.Helper()
		k, ok := w.Key(dir, ignore)
		if !ok {
			t.Fatal("not ok")
		}
		return k
	}
	t0 := time.Unix(1_800_000_000, 0)
	self := strconv.Itoa(os.Getpid()) + ".json" // a process that runs
	empty := key(nil)

	write(self, `{"sessionId":"aaaa","status":"idle"}`, t0)
	write(self[:len(self)-5]+".deadbeef.key", "not a session", t0) // other files don't count
	k1 := key(nil)
	if k1 == empty {
		t.Fatal("a new session must change the key")
	}
	if key(nil) != k1 {
		t.Fatal("nothing changed: same key")
	}

	write(self, `{"sessionId":"aaaa","status":"busy"}`, t0.Add(time.Second))
	k2 := key(nil)
	if k2 == k1 {
		t.Fatal("a status change must change the key")
	}
	if key(func(id string) bool { return id == "aaaa" }) != empty {
		t.Fatal("ignored sessions don't count")
	}

	// A session whose process ended without removing its file.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no true:", err)
	}
	dead := strconv.Itoa(cmd.Process.Pid) + ".json"
	write(dead, `{"sessionId":"bbbb"}`, t0)
	k3 := key(nil)
	if k3 == k2 {
		t.Fatal("another session must change the key")
	}

	// Background jobs.
	job := filepath.Join(dir, "jobs", "ab12cd34")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(job, "state.json"), []byte(`{"state":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	k4 := key(nil)
	if k4 == k3 {
		t.Fatal("a new background job must change the key")
	}
	if err := os.WriteFile(filepath.Join(job, "state.json"), []byte(`{"state":"blocked"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(job, "state.json"), t0.Add(time.Minute), t0.Add(time.Minute))
	if key(nil) == k4 {
		t.Fatal("a job that needs you must change the key")
	}

	// Ended sessions are forgotten.
	_ = os.Remove(filepath.Join(sessions, dead))
	_ = os.Remove(filepath.Join(sessions, self))
	key(nil)
	if len(w.files) != 0 {
		t.Fatalf("forgotten: %v", w.files)
	}
}

// The process behind a session file is checked on every call: one that
// ends without a word changes the key.
func TestStateWatchProcessEnds(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessions, 0o700)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep:", err)
	}
	name := strconv.Itoa(cmd.Process.Pid) + ".json"
	if err := os.WriteFile(filepath.Join(sessions, name), []byte(`{"sessionId":"cccc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var w StateWatch
	before, _ := w.Key(dir, nil)
	_ = cmd.Process.Kill() // our own child
	_ = cmd.Wait()
	after, _ := w.Key(dir, nil)
	if before == after {
		t.Fatal("the session's process ended: the key must change")
	}
}
