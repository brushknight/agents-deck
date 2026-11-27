package copilot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func writeSession(t *testing.T, id string, lines ...string) {
	t.Helper()
	dir := filepath.Join(Home(), "session-state", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, fmt.Sprintf("inuse.%d.lock", os.Getpid()))
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func eventLine(typ, at, data string) string {
	return `{"type":"` + typ + `","timestamp":"` + at + `","data":` + data + `}`
}

func TestTailWaitingRunningIdle(t *testing.T) {
	t.Setenv("COPILOT_HOME", t.TempDir())
	id := "dcd57062-0f12-4a41-b123-56b0df7f7677"
	start := "2026-11-25T15:25:24Z"
	writeSession(t, id, "junk",
		eventLine("session.start", start, `{"context":{"cwd":"/work/app"}}`),
		eventLine("session.model_change", "2026-11-25T15:25:25Z", `{"newModel":"gpt-6-luna"}`),
		eventLine("assistant.turn_start", "2026-11-25T15:25:26Z", `{"turnId":"t1"}`),
		eventLine("assistant.turn_end", "2026-11-25T15:26:00Z", `{"turnId":"t1"}`),
		eventLine("session.usage_record", "2026-11-25T15:26:00Z", `{"usage":{"inputTokens":1000,"outputTokens":40,"cacheReadTokens":500,"cacheWriteTokens":0,"cost":0.25,"maxPromptTokens":200000}}`),
		eventLine("permission.requested", "2026-11-25T15:26:01Z", `{"requestId":"r1"}`),
	)
	tr := NewTracker()
	list := tr.Sessions(6*time.Hour, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC))
	if len(list) != 1 {
		t.Fatalf("sessions: %+v", list)
	}
	s := list[0]
	if s.Status != model.Waiting || s.Title != "app" || s.ModelLabel != "gpt-6-luna" {
		t.Fatalf("waiting: %+v", s)
	}
	if s.Tokens.Output != 40 || s.Tokens.CacheRead != 500 || s.Cost != 0.25 {
		t.Fatalf("usage: %+v", s)
	}
	if s.Context.Used != 1500 || s.Context.Window != 200000 {
		t.Fatalf("context: %+v", s.Context)
	}
	if s.Since.Format(time.RFC3339) != "2026-11-25T15:26:01Z" {
		t.Fatalf("since: %v", s.Since)
	}
	// The permission is answered and the session goes idle.
	f, err := os.OpenFile(filepath.Join(Home(), "session-state", id, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, eventLine("permission.completed", "2026-11-25T15:27:00Z", `{"requestId":"r1"}`))
	f.Close()
	list = tr.Sessions(24*time.Hour, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC))
	if len(list) != 1 || list[0].Status != model.Idle {
		t.Fatalf("idle: %+v", list)
	}
}

func TestRunningAndWindow(t *testing.T) {
	t.Setenv("COPILOT_HOME", t.TempDir())
	id := "running-session"
	writeSession(t, id,
		eventLine("session.start", "2026-11-25T15:25:24Z", `{"context":{"cwd":"/work/app"}}`),
		eventLine("assistant.turn_start", "2026-11-25T15:25:26Z", `{"turnId":"t1"}`),
		eventLine("tool.execution_start", "2026-11-25T15:25:30Z", `{"toolName":"bash"}`),
	)
	now := time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)
	list := NewTracker().Sessions(2*time.Hour, now)
	if len(list) != 1 || list[0].Status != model.Running || list[0].Activity != "bash" {
		t.Fatalf("running: %+v", list)
	}
	if got := NewTracker().Sessions(time.Minute, now); len(got) != 0 {
		t.Fatalf("aged out: %+v", got)
	}
	// A live lock and an unmatched turn start from hours ago stay idle:
	// persisted events are not proof of current work.
	stale := NewTracker().Sessions(2*time.Hour, time.Date(2026, 11, 25, 16, 30, 0, 0, time.UTC))
	if len(stale) != 1 || stale[0].Status != model.Idle {
		t.Fatalf("stale turn: %+v", stale)
	}
}

func TestLockPID(t *testing.T) {
	if got := lockPID("/x/inuse.123.lock"); got != 123 {
		t.Fatalf("pid: %d", got)
	}
	if got := lockPID("/x/hold.lock"); got != 0 {
		t.Fatalf("bad pid: %d", got)
	}
}

func TestShutdownAndTruncateReset(t *testing.T) {
	t.Setenv("COPILOT_HOME", t.TempDir())
	id := "lifecycle"
	path := filepath.Join(Home(), "session-state", id, "events.jsonl")
	writeSession(t, id,
		eventLine("assistant.turn_start", "2026-11-25T15:25:26Z", `{"turnId":"t1"}`),
		eventLine("permission.requested", "2026-11-25T15:25:27Z", `{"requestId":"r1"}`),
		eventLine("session.usage_record", "2026-11-25T15:25:28Z", `{"usage":{"inputTokens":100,"outputTokens":10}}`),
	)
	tl := &tail{pending: map[string]bool{}}
	if err := tl.update(path); err != nil || !tl.turn || len(tl.pending) != 1 {
		t.Fatalf("tail: %+v %v", tl, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, eventLine("session.shutdown", "2026-11-25T15:26:00Z", `{}`))
	f.Close()
	if err := tl.update(path); err != nil || tl.turn || len(tl.pending) != 0 || tl.activity != "" {
		t.Fatalf("shutdown: %+v %v", tl, err)
	}
	// A shorter replacement log resets the parser instead of keeping old state.
	if err := os.WriteFile(path, []byte(eventLine("assistant.turn_start", "2026-11-25T16:00:00Z", `{"turnId":"t2"}`)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tl.update(path); err != nil || !tl.turn || tl.tokens.Output != 0 {
		t.Fatalf("truncate: %+v %v", tl, err)
	}
}

func TestDeadLockIsNotLive(t *testing.T) {
	t.Setenv("COPILOT_HOME", t.TempDir())
	id := "dead"
	writeSession(t, id, eventLine("session.start", "2026-11-25T15:25:24Z", `{"context":{"cwd":"/work/app"}}`))
	// replace the live lock with one for a PID that is certainly gone
	locks, _ := filepath.Glob(filepath.Join(Home(), "session-state", id, "inuse.*.lock"))
	for _, l := range locks {
		os.Remove(l)
	}
	if err := os.WriteFile(filepath.Join(Home(), "session-state", id, "inuse.999999.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewTracker().Sessions(time.Hour, time.Now()); len(got) != 0 {
		t.Fatalf("dead lock listed: %+v", got)
	}
}
