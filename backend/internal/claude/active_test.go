package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/store"
)

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "0b5c2f4e-1111-4222-8333-944455556666.jsonl")
	body := strings.Join(lines, "\n")
	if len(lines) > 0 {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ts(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// The newest user or assistant message sets the last activity; lines that
// are not conversation, and broken lines, don't.
func TestTailLastActive(t *testing.T) {
	path := writeLines(t,
		`{"type":"user","timestamp":"2026-11-02T09:00:00Z","message":{"role":"user","content":"start"}}`,
		`{"type":"assistant","timestamp":"2026-11-02T09:05:00.250Z","message":{"id":"m1","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"user","timestamp":"2026-11-02T09:03:00Z","message":{"role":"user","content":[{"type":"tool_result"}]}}`,
		`{"type":"assistant", broken`,
		`{"type":"summary","timestamp":"2026-11-02T10:00:00Z","summary":"a summary"}`,
		`{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-11-02T10:01:00Z"}`,
		`{"type":"user","isMeta":true,"timestamp":"2026-11-02T10:02:00Z","message":{"role":"user","content":"caveat"}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-11-02T10:03:00Z","message":{"id":"s1"}}`,
		`{"type":"user","timestamp":"not a time","message":{"role":"user","content":"x"}}`,
		`{"type":"ai-title","aiTitle":"Tidy the notes","timestamp":"2026-11-02T10:04:00Z"}`,
	)
	e := &store.Entry{Transcript: path}
	Tail(e)
	if want := ts("2026-11-02T09:05:00.25Z"); !e.A.LastActive.Equal(want) {
		t.Fatalf("last active %v, want %v", e.A.LastActive, want)
	}

	// A newer message appended later moves it on.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","timestamp":"2026-11-03T08:00:00Z","message":{"role":"user","content":"more"}}` + "\n")
	f.Close()
	if !Tail(e) || !e.A.LastActive.Equal(ts("2026-11-03T08:00:00Z")) {
		t.Fatalf("after a new prompt: %v", e.A.LastActive)
	}
}

func TestTailLastActiveEmpty(t *testing.T) {
	e := &store.Entry{Transcript: writeLines(t)}
	Tail(e)
	if !e.A.LastActive.IsZero() {
		t.Fatalf("empty transcript: %v", e.A.LastActive)
	}
	e = &store.Entry{Transcript: writeLines(t, `{"type":"summary"}`, `garbage`)}
	Tail(e)
	if !e.A.LastActive.IsZero() {
		t.Fatalf("no messages: %v", e.A.LastActive)
	}
}

// After a restart the transcript is read again from the start: a time the
// service logged itself that is newer than the transcript stays.
func TestTailNeverMovesBack(t *testing.T) {
	path := writeLines(t, `{"type":"user","timestamp":"2026-11-02T09:00:00Z","message":{"role":"user","content":"start"}}`)
	e := &store.Entry{Transcript: path}
	e.A.LastActive = ts("2026-11-02T11:00:00Z")
	Tail(e)
	if !e.A.LastActive.Equal(ts("2026-11-02T11:00:00Z")) {
		t.Fatalf("moved back to %v", e.A.LastActive)
	}
}

// Hooks for messages stamp the time themselves; starting a session doesn't.
func TestApplyStampsActivity(t *testing.T) {
	now := ts("2026-11-04T12:00:00Z")
	old := Now
	Now = func() time.Time { return now }
	defer func() { Now = old }()

	e := &store.Entry{}
	Apply(e, Event{Name: "SessionStart"})
	if !e.A.LastActive.IsZero() {
		t.Fatalf("session start counted: %v", e.A.LastActive)
	}
	for _, name := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"} {
		e := &store.Entry{}
		Apply(e, Event{Name: name, ToolName: "Bash"})
		if !e.A.LastActive.Equal(now) {
			t.Errorf("%s: last active %v", name, e.A.LastActive)
		}
	}
}
