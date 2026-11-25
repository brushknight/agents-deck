package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/store"
)

func text(s string) string { return fmt.Sprintf(`{"type":"text","text":%q}`, s) }

func TestTailLastAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	t0 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	e := &store.Entry{Transcript: path}

	// A turn: text before a tool call is not the answer, the text after the
	// last one is. One message written a block per line counts as one.
	write(
		prompt(t0, "fix the login bug"),
		reply(t0.Add(time.Second), "m1", "tool_use", text("Let me look at the handler.")),
		toolCall(t0.Add(2*time.Second), "m1", "Read"),
		toolResult(t0.Add(3*time.Second), "m1"),
		reply(t0.Add(4*time.Second), "m2", "", `{"type":"thinking","thinking":"hmm"}`),
		reply(t0.Add(5*time.Second), "m2", "", text("## Fixed\n\nThe **token check** ran too early.")),
	)
	Tail(e)
	if e.A.LastAnswer != "" {
		t.Fatalf("the turn hasn't ended: no answer yet, got %q", e.A.LastAnswer)
	}
	write(reply(t0.Add(6*time.Second), "m2", "end_turn", text("- moved it after login\n- added a test")))
	if !Tail(e) {
		t.Fatal("an answer should change the agent")
	}
	want := "Fixed The token check ran too early. • moved it after login • added a test"
	if e.A.LastAnswer != want || !e.A.LastAnswerAt.Equal(t0.Add(6*time.Second)) {
		t.Fatalf("answer %q at %v\nwant %q", e.A.LastAnswer, e.A.LastAnswerAt, want)
	}

	// A turn you interrupt, or one that ends in a tool call only, keeps the
	// answer of the turn before. So do subagents' replies and API errors.
	write(
		prompt(t0.Add(time.Minute), "now the logout"),
		reply(t0.Add(time.Minute+time.Second), "m3", "", text("Looking at logout.")),
		interrupt(t0.Add(time.Minute+2*time.Second), false),
		fmt.Sprintf(`{"type":"assistant","isSidechain":true,"timestamp":%q,"message":{"id":"s1","model":"claude-opus-5-5","stop_reason":"end_turn","content":[%s],"usage":{"input_tokens":1,"output_tokens":1}}}`, stamp(t0.Add(2*time.Minute)), text("subagent text")),
		apiError(t0.Add(3*time.Minute)),
	)
	Tail(e)
	if e.A.LastAnswer != want {
		t.Fatalf("answer replaced by %q", e.A.LastAnswer)
	}

	// Re-reading the file from the start (a restart) finds the same answer.
	again := &store.Entry{Transcript: path}
	Tail(again)
	if again.A.LastAnswer != want {
		t.Fatalf("after a restart: %q", again.A.LastAnswer)
	}
}

// The Stop hook takes the reply as the answer even when no line of the
// transcript says the turn ended.
func TestStopHookTakesTheAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	t0 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	lines := []string{prompt(t0, "say hi"), reply(t0.Add(time.Second), "m1", "", text("Hi there."))}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &store.Entry{Transcript: path}
	Apply(e, Event{Name: "Stop"})
	if e.A.LastAnswer != "Hi there." || !e.A.LastAnswerAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("answer %q at %v", e.A.LastAnswer, e.A.LastAnswerAt)
	}
}
