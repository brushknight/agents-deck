package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Made-up transcript lines, shaped like the ones Claude Code writes.

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func prompt(at time.Time, text string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":%q}}`, stamp(at), text)
}

func reply(at time.Time, id, stop string, blocks string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-opus-5-5","stop_reason":%q,"content":[%s],"usage":{"input_tokens":5,"output_tokens":20}}}`,
		stamp(at), id, stop, blocks)
}

func toolCall(at time.Time, id, tool string) string {
	return reply(at, id, "tool_use", fmt.Sprintf(`{"type":"tool_use","id":"tu_%s","name":%q,"input":{}}`, id, tool))
}

func finalReply(at time.Time, id string) string {
	return reply(at, id, "end_turn", `{"type":"text","text":"Done."}`)
}

func toolResult(at time.Time, id string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_%s","content":"ok"}]}}`, stamp(at), id)
}

func apiError(at time.Time) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"isApiErrorMessage":true,"message":{"id":"err","model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 Overloaded"}]}}`, stamp(at))
}

func interrupt(at time.Time, forTool bool) string {
	text := "[Request interrupted by user]"
	if forTool {
		text = "[Request interrupted by user for tool use]"
	}
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":%q}]}}`, stamp(at), text)
}

// Notes Claude Code keeps appending to a session that is doing nothing.
func idleNotes(at time.Time) []string {
	return []string{
		`{"type":"last-prompt","lastPrompt":"fix the tests","sessionId":"s"}`,
		`{"type":"custom-title","customTitle":"Sample project","sessionId":"s"}`,
		`{"type":"permission-mode","permissionMode":"default","sessionId":"s"}`,
		fmt.Sprintf(`{"type":"frame-link","timestamp":%q,"artifactCount":0}`, stamp(at)),
		`{"type":"artifact-autoreact-ledger","v":1,"artifacts":[]}`,
	}
}

// A compaction Claude Code ran on its own between turns: boundary, summary,
// attachments and a note.
func idleCompaction(at time.Time) []string {
	return []string{
		fmt.Sprintf(`{"type":"system","subtype":"compact_boundary","isMeta":false,"timestamp":%q,"content":"Conversation compacted","compactMetadata":{"trigger":"auto"}}`, stamp(at)),
		fmt.Sprintf(`{"type":"user","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"timestamp":%q,"message":{"role":"user","content":"This session is being continued from a previous conversation."}}`, stamp(at)),
		fmt.Sprintf(`{"type":"attachment","timestamp":%q,"attachment":{"type":"file"}}`, stamp(at)),
		fmt.Sprintf(`{"type":"system","subtype":"informational","isMeta":true,"timestamp":%q,"content":"Compacted while idle"}`, stamp(at)),
	}
}

func writeTo(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

// A service restart re-reads every transcript from the start. An API error
// days ago, followed by hundreds of normal replies and a finished turn, must
// not come back as "recovered: running" for an agent doing nothing.
func TestTailReplayKeepsAFinishedTurnIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	old := time.Now().Add(-72 * time.Hour)
	writeTo(t, path,
		prompt(old, "build the feature"),
		apiError(old.Add(time.Minute)),
		toolCall(old.Add(2*time.Minute), "m1", "Bash"),
		toolResult(old.Add(3*time.Minute), "m1"),
		finalReply(old.Add(4*time.Minute), "m2"),
		prompt(old.Add(time.Hour), "and the docs"),
		finalReply(old.Add(time.Hour+time.Minute), "m3"),
	)
	e := &store.Entry{Transcript: path}
	e.SetStatus(model.Idle) // as the store kept it
	Tail(e)
	if e.A.Status != model.Idle {
		t.Fatalf("replayed history revived the agent: %s", e.A.Status)
	}

	// The same, for an agent the store kept in error: it recovered long ago,
	// and its turn is over.
	e = &store.Entry{Transcript: path}
	e.SetStatus(model.Error)
	Tail(e)
	if e.A.Status != model.Idle || e.A.Error != nil {
		t.Fatalf("recovered and finished: %s %+v", e.A.Status, e.A.Error)
	}
}

func TestTailAPIErrorWhileWatching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	now := time.Now()
	writeTo(t, path, prompt(now, "go"), toolCall(now, "m1", "Read"))
	e := &store.Entry{Transcript: path}
	e.SetStatus(model.Running)
	Tail(e)

	appendLines(t, path, apiError(now))
	Tail(e)
	if e.A.Status != model.Error || e.A.Error == nil || !strings.Contains(e.A.Error.Message, "529") {
		t.Fatalf("api error: %s %+v", e.A.Status, e.A.Error)
	}
	// Claude Code retries and the turn goes on.
	appendLines(t, path, toolCall(now, "m2", "Bash"))
	Tail(e)
	if e.A.Status != model.Running || e.A.Error != nil {
		t.Fatalf("recovered mid-turn: %s", e.A.Status)
	}
	// A transcript read in one go that ends on an error shows the error.
	other := &store.Entry{Transcript: path}
	appendLines(t, path, apiError(now))
	Tail(other)
	if other.A.Status != model.Error {
		t.Fatalf("ends on an error: %s", other.A.Status)
	}
}

// Claude Code compacts an idle session on its own (trigger "auto", like a
// compaction in the middle of a turn). The agent was idle and stays idle
// once the compaction is done.
func TestCompactionBetweenTurns(t *testing.T) {
	for _, end := range []string{"hook", "transcript"} {
		e := &store.Entry{}
		e.SetStatus(model.Idle)
		Apply(e, Event{Name: "PreCompact", Trigger: "auto"})
		if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != CompactTool {
			t.Fatalf("%s: compacting: %+v", end, e.A)
		}
		if end == "hook" {
			Apply(e, Event{Name: "SessionStart", Source: "compact"})
		} else {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			writeTo(t, path, idleCompaction(time.Now())...)
			e.Transcript = path
			Tail(e)
		}
		if e.A.Status != model.Idle || e.A.Activity != nil {
			t.Fatalf("%s: idle again after compacting between turns: %s %+v", end, e.A.Status, e.A.Activity)
		}
	}

	// Shown running only because subagents were writing: still between turns.
	e := &store.Entry{Background: true}
	e.SetStatus(model.Running)
	Apply(e, Event{Name: "PreCompact", Trigger: "auto"})
	Apply(e, Event{Name: "SessionStart", Source: "compact"})
	if e.A.Status != model.Idle {
		t.Fatalf("subagents only: %s", e.A.Status)
	}

	// In the middle of a turn the turn goes on (see TestCompaction too).
	e = &store.Entry{}
	Apply(e, ev("UserPromptSubmit", "prompt", "go"))
	Apply(e, Event{Name: "PreCompact", Trigger: "auto"})
	Apply(e, Event{Name: "SessionStart", Source: "compact"})
	if e.A.Status != model.Running {
		t.Fatalf("mid-turn: %s", e.A.Status)
	}
}

func TestReadTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	at := time.Now().Add(-48 * time.Hour).Truncate(time.Millisecond)
	cases := []struct {
		name   string
		lines  []string
		over   bool
		inTool bool
		tool   string
	}{
		{"final reply", []string{prompt(at, "go"), toolCall(at, "m1", "Edit"), toolResult(at, "m1"), finalReply(at, "m2")}, true, false, "Edit"},
		{"final reply, then notes", append([]string{prompt(at, "go"), finalReply(at, "m1")}, idleNotes(time.Now())...), true, false, ""},
		{"final reply, then an idle compaction", append([]string{prompt(at, "go"), finalReply(at, "m1")}, idleCompaction(time.Now())...), true, false, ""},
		{"interrupted", []string{prompt(at, "go"), toolCall(at, "m1", "Bash"), interrupt(at, true)}, true, false, "Bash"},
		{"interrupted while thinking", []string{prompt(at, "go"), interrupt(at, false)}, true, false, ""},
		{"api error", []string{prompt(at, "go"), apiError(at)}, true, false, ""},
		{"tool call out", []string{prompt(at, "go"), toolCall(at, "m1", "Bash")}, false, true, "Bash"},
		{"thinking after a tool result", []string{prompt(at, "go"), toolCall(at, "m1", "Grep"), toolResult(at, "m1")}, false, false, "Grep"},
		{"thinking about a prompt", []string{finalReply(at, "m0"), prompt(at, "next")}, false, false, ""},
		{"a prompt typed that says the words", []string{finalReply(at, "m0"), prompt(at, "[Request interrupted by user] means what?")}, false, false, ""},
		{"compacting mid-turn", append([]string{prompt(at, "go"), toolCall(at, "m1", "Read"), toolResult(at, "m1")}, idleCompaction(time.Now())...), false, false, "Read"},
		{"a subagent's final reply is not the turn's", []string{
			prompt(at, "go"), toolCall(at, "m1", "Agent"),
			fmt.Sprintf(`{"type":"assistant","isSidechain":true,"timestamp":%q,"message":{"id":"s1","stop_reason":"end_turn","content":[{"type":"text","text":"x"}]}}`, stamp(at)),
		}, false, true, "Agent"},
		{"text Claude Code adds after the reply", []string{
			finalReply(at, "m1"),
			fmt.Sprintf(`{"type":"user","isMeta":true,"timestamp":%q,"message":{"role":"user","content":"<local-command-caveat>"}}`, stamp(time.Now())),
		}, true, false, ""},
	}
	for _, c := range cases {
		writeTo(t, path, c.lines...)
		got, ok := ReadTurn(path)
		if !ok || got.Over != c.over || got.InTool != c.inTool || got.Tool != c.tool {
			t.Errorf("%s: %+v, want over=%v inTool=%v tool=%q", c.name, got, c.over, c.inTool, c.tool)
		}
		if ok && !got.At.Equal(at) {
			t.Errorf("%s: at %v, want the latest message's time %v", c.name, got.At, at)
		}
	}
	if _, ok := ReadTurn(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Error("missing file")
	}
}

// A watched session interrupted with Esc, whose transcript then gets notes
// appended (a fresh file time), is not working.
func TestTranscriptTurnIgnoresNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	old := time.Now().Add(-3 * time.Hour)
	writeTo(t, path, append([]string{prompt(old, "go"), toolCall(old, "m1", "Bash"), interrupt(old, true)}, idleNotes(time.Now())...)...)
	if running, _ := TranscriptTurn(path, 10*time.Minute); running {
		t.Fatal("interrupted turn shown running")
	}
	// A prompt hours old with nothing after it: not a turn in progress either.
	writeTo(t, path, append([]string{prompt(old, "go")}, idleNotes(time.Now())...)...)
	if running, _ := TranscriptTurn(path, 10*time.Minute); running {
		t.Fatal("notes count as activity")
	}
	writeTo(t, path, prompt(time.Now(), "go"))
	if running, _ := TranscriptTurn(path, 10*time.Minute); !running {
		t.Fatal("a fresh prompt is a turn in progress")
	}
}

func TestSettle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	agent := func(status model.Status, since time.Duration) *store.Entry {
		e := &store.Entry{Transcript: path}
		e.SetStatus(status)
		e.A.StatusSince = ago(since)
		return e
	}
	cases := []struct {
		name   string
		e      *store.Entry
		lines  []string
		settle bool
	}{
		{"running for days after a finished turn (a replayed status)", agent(model.Running, 45*time.Hour),
			append([]string{prompt(ago(72*time.Hour), "go"), finalReply(ago(72*time.Hour), "m1")}, idleNotes(ago(time.Minute))...), true},
		{"interrupted with Esc", agent(model.Running, 5*time.Minute),
			[]string{prompt(ago(5*time.Minute), "go"), toolCall(ago(4*time.Minute), "m1", "Bash"), interrupt(ago(time.Minute), true)}, true},
		{"the Stop hook was lost", agent(model.Running, 10*time.Minute),
			[]string{prompt(ago(10*time.Minute), "go"), finalReply(ago(2*time.Minute), "m1")}, true},
		{"a long tool call", agent(model.Running, 3*time.Hour),
			[]string{prompt(ago(3*time.Hour), "go"), toolCall(ago(3*time.Hour), "m1", "Bash")}, false},
		{"a long think", agent(model.Running, 20*time.Minute),
			[]string{finalReply(ago(time.Hour), "m0"), prompt(ago(20*time.Minute), "go"), toolCall(ago(19*time.Minute), "m1", "Read"), toolResult(ago(19*time.Minute), "m1")}, false},
		{"just finished: the Stop hook may be on its way", agent(model.Running, 10*time.Minute),
			[]string{prompt(ago(10*time.Minute), "go"), finalReply(ago(5*time.Second), "m1")}, false},
		{"just prompted: the prompt isn't in the transcript yet", agent(model.Running, 5*time.Second),
			[]string{finalReply(ago(time.Hour), "m0")}, false},
		{"compacting between turns", func() *store.Entry {
			e := agent(model.Running, time.Minute)
			e.A.Activity = &model.Activity{Tool: CompactTool}
			return e
		}(), []string{finalReply(ago(time.Hour), "m0")}, false},
		{"a compaction that never reported its end", func() *store.Entry {
			e := agent(model.Running, time.Hour)
			e.A.Activity = &model.Activity{Tool: CompactTool}
			return e
		}(), append([]string{finalReply(ago(2*time.Hour), "m0")}, idleCompaction(ago(time.Hour))...), true},
		{"running only for its subagents", func() *store.Entry {
			e := agent(model.Running, time.Hour)
			e.Background = true
			return e
		}(), []string{finalReply(ago(2*time.Hour), "m0")}, false},
		{"a permission refused with Esc in the terminal", agent(model.Waiting, 5*time.Minute),
			[]string{prompt(ago(6*time.Minute), "go"), toolCall(ago(5*time.Minute), "m1", "Bash"), interrupt(ago(time.Minute), true)}, true},
		{"a permission still open", agent(model.Waiting, 2*time.Hour),
			[]string{prompt(ago(2*time.Hour), "go"), toolCall(ago(2*time.Hour), "m1", "Bash")}, false},
		{"waiting, the turn that ended is an older one", agent(model.Waiting, 5*time.Minute),
			[]string{finalReply(ago(time.Hour), "m0")}, false},
		{"idle stays idle", agent(model.Idle, time.Hour), []string{finalReply(ago(time.Hour), "m0")}, false},
	}
	for _, c := range cases {
		writeTo(t, path, c.lines...)
		was := c.e.A.Status
		got := Settle(c.e, now)
		if got != c.settle {
			t.Errorf("%s: settled %v, want %v", c.name, got, c.settle)
			continue
		}
		if got && (c.e.A.Status != model.Idle || c.e.A.Activity != nil || c.e.A.Waiting != nil) {
			t.Errorf("%s: %s %+v", c.name, c.e.A.Status, c.e.A)
		}
		if !got && c.e.A.Status != was {
			t.Errorf("%s: status moved to %s", c.name, c.e.A.Status)
		}
	}
	if Settle(&store.Entry{A: model.Agent{Status: model.Running}}, now) {
		t.Error("no transcript: nothing to go by")
	}
}

// Days of notes after the final reply push it far up the file.
func TestReadTurnPastManyNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	old := time.Now().Add(-6 * 24 * time.Hour).Truncate(time.Millisecond)
	lines := []string{prompt(old, "go"), toolCall(old, "m1", "Write"), toolResult(old, "m1"), finalReply(old, "m2")}
	ledger := `{"type":"artifact-autoreact-ledger","v":1,"artifacts":["` + strings.Repeat("a", 4800) + `"]}`
	for n := 0; n < 3*turnChunk/len(ledger); n++ {
		lines = append(lines, ledger)
	}
	writeTo(t, path, lines...)
	got, ok := ReadTurn(path)
	if !ok || !got.Over || !got.At.Equal(old) || got.Tool != "Write" {
		t.Fatalf("final reply under %d KB of notes: %+v", 3*turnChunk>>10, got)
	}
}

// Later calls read only what was appended, and still follow the turn.
func TestReadTurnFollowsAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	now := time.Now().Truncate(time.Millisecond)
	writeTo(t, path, prompt(now, "go"), finalReply(now, "m1"))
	check := func(name string, over, inTool bool, tool string) {
		t.Helper()
		got, ok := ReadTurn(path)
		if !ok || got.Over != over || got.InTool != inTool || got.Tool != tool {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	check("finished", true, false, "")
	appendLines(t, path, idleNotes(now)...)
	check("notes change nothing", true, false, "")
	appendLines(t, path, prompt(now, "next"))
	check("a new prompt", false, false, "")
	appendLines(t, path, toolCall(now, "m2", "Bash"))
	check("a tool call", false, true, "Bash")

	// A line still being written is read once it is whole.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	line := finalReply(now, "m3")
	f.WriteString(toolResult(now, "m2") + "\n" + line[:40])
	check("half a reply", false, false, "Bash")
	f.WriteString(line[40:] + "\n")
	f.Close()
	check("the whole reply", true, false, "Bash")

	// A file written anew is read anew.
	writeTo(t, path, prompt(now, "a different session that is longer than the first one"), toolCall(now, "x1", "Grep"), toolResult(now, "x1"), toolCall(now, "x2", "Glob"))
	check("replaced", false, true, "Glob")
}
