package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRolloutThreads(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)
	write := func(day time.Time, name, first string, mod time.Time) {
		d := filepath.Join(dir, day.Format("2006"), day.Format("01"), day.Format("02"))
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte(first+"\n{\"type\":\"event_msg\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	yesterday := now.AddDate(0, 0, -1)
	write(now, "rollout-a.jsonl", `{"type":"session_meta","payload":{"id":"t-new","cwd":"/work/api","git":{"branch":"main"}}}`, now.Add(-time.Minute))
	write(yesterday, "rollout-b.jsonl", `{"type":"session_meta","payload":{"id":"t-late","cwd":"/work/web"}}`, now.Add(-2*time.Hour))
	write(yesterday, "rollout-c.jsonl", `{"type":"session_meta","payload":{"id":"t-old","cwd":"/work/old"}}`, now.Add(-20*time.Hour))
	write(now, "rollout-d.jsonl", `{"type":"event_msg"}`, now) // not a rollout's first line
	ts, err := rolloutThreads(dir, 6*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[0].ID != "t-new" || ts[0].Cwd != "/work/api" || ts[0].Branch != "main" || ts[1].ID != "t-late" {
		t.Fatalf("got %+v", ts)
	}
	if ts[0].DisplayTitle() != "api" || filepath.Base(ts[0].RolloutPath) != "rollout-a.jsonl" {
		t.Errorf("title %q, path %q", ts[0].DisplayTitle(), ts[0].RolloutPath)
	}
}
