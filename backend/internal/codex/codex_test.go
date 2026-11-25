package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

func TestTailLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	// History before we start watching: a finished turn is not "hungry".
	write(
		`{"type":"session_meta","payload":{"id":"t1","cwd":"/work/app"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-5-codex","cwd":"/work/app"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","model_context_window":258400}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
	)
	e := &store.Entry{Transcript: path}
	Tail(e)
	if e.A.Status != model.Idle || e.A.Unseen || e.A.ModelLabel != "5-codex" || e.A.Context.Window != 258400 {
		t.Fatalf("after replay: %+v", e.A)
	}
	write(
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment>x</environment>"},{"type":"input_text","text":"add  a\nhealth check"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"go test ./...\nmore"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5000,"cached_input_tokens":3000,"output_tokens":400},"last_token_usage":{"input_tokens":4200},"model_context_window":258400}}}`,
	)
	Tail(e)
	if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Detail != "go test ./..." || e.A.LastPrompt != "add a health check" {
		t.Fatalf("running: %+v %+v", e.A, e.A.Activity)
	}
	if e.A.Context.Used != 4200 || e.A.Tokens.CacheRead != 3000 || e.A.Tokens.Input != 2000 {
		t.Fatalf("tokens: %+v %+v", e.A.Tokens, e.A.Context)
	}
	write(`{"type":"response_item","payload":{"type":"function_call","name":"request_user_input_async","arguments":"{\"question\":\"which port?\"}"}}`)
	Tail(e)
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.Title != "which port?" {
		t.Fatalf("question: %+v", e.A.Waiting)
	}
	write(`{"type":"response_item","payload":{"type":"function_call_output","output":"8080"}}`, `{"type":"event_msg","payload":{"type":"task_complete"}}`)
	Tail(e)
	if e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("a turn finishing while watched should be hungry: %+v", e.A)
	}
}

func TestAgentIDStable(t *testing.T) {
	a, b := AgentID("019a-thread"), AgentID("019a-thread")
	if a != b || len(a) != 6 || a[0] != 'x' {
		t.Fatalf("%q %q", a, b)
	}
}

func TestTailStartsNearTheEndOfHugeRollouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	var b strings.Builder
	b.WriteString(`{"type":"event_msg","payload":{"type":"task_started"}}` + "\n")
	filler := `{"type":"response_item","payload":{"type":"reasoning","summary":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	for b.Len() < maxRead+maxRead/2 {
		b.WriteString(filler)
	}
	b.WriteString(`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &store.Entry{Transcript: path}
	Tail(e)
	if e.A.Status != model.Idle || e.A.Unseen {
		t.Fatalf("one pass should reach the finished turn at the end: %+v", e.A)
	}
	if e.Offset != int64(b.Len()) || e.Pending != "" {
		t.Fatalf("offset %d of %d, pending %q", e.Offset, b.Len(), e.Pending)
	}
}

func TestRolloutLastActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := []string{
		`{"timestamp":"2026-11-02T09:00:00Z","type":"session_meta","payload":{"id":"x"}}`,
		`{"timestamp":"2026-11-02T09:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]}}`,
		`{"timestamp":"2026-11-02T09:04:00Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}"}}`,
		`{"timestamp":"2026-11-02T09:09:00Z","type":"event_msg","payload":{"type":"token_count"}}`,
		`{"timestamp":"2026-11-02T09:10:00Z","type":"response_item","payload":{"type":"message","role":"developer","content":[]}}`,
		`not json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &store.Entry{Transcript: path}
	Tail(e)
	if want := time.Date(2026, 11, 2, 9, 4, 0, 0, time.UTC); !e.A.LastActive.Equal(want) {
		t.Fatalf("last active %v, want %v", e.A.LastActive, want)
	}
}

func TestTailLastAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	e := &store.Entry{Transcript: path}
	write(
		`{"timestamp":"2026-03-02T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a health check"}]}}`,
		`{"timestamp":"2026-03-02T10:00:01Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-03-02T10:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Looking at the router."}]}}`,
		`{"timestamp":"2026-03-02T10:00:05Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Added **/healthz** in `+"`server.go`"+`."}]}}`,
	)
	Tail(e)
	if e.A.LastAnswer != "" {
		t.Fatalf("turn still running: %q", e.A.LastAnswer)
	}
	write(`{"timestamp":"2026-03-02T10:00:06Z","type":"event_msg","payload":{"type":"task_complete"}}`)
	Tail(e)
	if e.A.LastAnswer != "Added /healthz in server.go." || e.A.LastAnswerAt.Format(time.RFC3339) != "2026-03-02T10:00:06Z" {
		t.Fatalf("answer %q at %v", e.A.LastAnswer, e.A.LastAnswerAt)
	}
	// task_complete's own final message wins; an aborted turn keeps the answer before.
	write(
		`{"timestamp":"2026-03-02T10:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"and a readiness check"}]}}`,
		`{"timestamp":"2026-03-02T10:01:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Working on it."}]}}`,
		`{"timestamp":"2026-03-02T10:01:03Z","type":"event_msg","payload":{"type":"turn_aborted"}}`,
	)
	Tail(e)
	if e.A.LastAnswer != "Added /healthz in server.go." {
		t.Fatalf("aborted turn replaced the answer: %q", e.A.LastAnswer)
	}
	write(
		`{"timestamp":"2026-03-02T10:02:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}}`,
		`{"timestamp":"2026-03-02T10:02:09Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"Added /readyz too.\n\nBoth return 200."}}`,
	)
	Tail(e)
	if e.A.LastAnswer != "Added /readyz too. Both return 200." {
		t.Fatalf("answer %q", e.A.LastAnswer)
	}
}
