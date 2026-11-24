package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRateLimits(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	old := filepath.Join(dir, "2026", "11", "20", "rollout-a.jsonl")
	cur := filepath.Join(dir, "2026", "11", "24", "rollout-b.jsonl")
	lines := []string{
		`{"timestamp":"2026-11-24T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":6.0,"window_minutes":10080,"resets_at":1795614898},"secondary":null,"plan_type":"pro"}}}`,
		`{"timestamp":"2026-11-24T08:01:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"rate_limits are not here"}]}}`,
		`{"timestamp":"2026-11-24T08:02:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"premium","primary":null,"secondary":null}}}`,
		`{"timestamp":"2026-11-24T08:03:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":7.0,"window_minutes":10080,"resets_at":1795614898},"secondary":null,"plan_type":"pro"}}}`,
	}
	for _, f := range []string{old, cur} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(cur, now.Add(-time.Minute), now.Add(-time.Minute))
	got, _ := Newest(dir, 7, now)
	if got != cur {
		t.Fatalf("Newest = %q, want %q", got, cur)
	}
	rs := RateLimits(got)
	if len(rs) != 2 {
		t.Fatalf("reports: %+v", rs)
	}
	if r := rs[0]; r.Limits.LimitID != "codex" || r.Limits.Primary.UsedPercent != 7 || r.At.Minute() != 3 {
		t.Errorf("the last codex report wins: %+v", r)
	}
	if r := rs[1]; r.Limits.LimitID != "premium" || r.Limits.Primary != nil {
		t.Errorf("premium: %+v", r)
	}
	// Files older than the days looked at don't count.
	if f, _ := Newest(dir, 1, now.AddDate(0, 0, 10)); f != "" {
		t.Errorf("nothing that recent, got %q", f)
	}
}
