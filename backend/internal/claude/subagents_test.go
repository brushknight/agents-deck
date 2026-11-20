package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A made-up session: the main transcript and its subagents folder.
type session struct {
	t    *testing.T
	main string
}

func newSession(t *testing.T) *session {
	dir := t.TempDir()
	s := &session{t: t, main: filepath.Join(dir, "5e551011-aaaa-4bbb-8ccc-0123456789ab.jsonl")}
	if err := os.MkdirAll(s.subDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	s.write("") // the file exists
	return s
}

func (s *session) subDir() string {
	return filepath.Join(strings.TrimSuffix(s.main, ".jsonl"), "subagents")
}

// write appends raw text to the main transcript.
func (s *session) write(text string) {
	f, err := os.OpenFile(s.main, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) lines(ls ...string) { s.write(strings.Join(ls, "\n") + "\n") }

// subFile writes a subagent's own transcript, last written at mod.
func (s *session) subFile(id string, mod time.Time, lines ...string) {
	p := filepath.Join(s.subDir(), "agent-"+id+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.subDir(), "agent-"+id+".meta.json"), []byte(`{"agentType":"Explore","description":"look around"}`), 0o600); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) working(since, now time.Time) []string {
	var ids []string
	for _, x := range Subagents(s.main, since, now) {
		ids = append(ids, x.ID)
	}
	return ids
}

func lstamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// jsonStr writes v as Claude Code does: "<" and ">" as they are.
func jsonStr(v string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

// The records Claude Code writes (field names as in real transcripts).
func launch(at time.Time, id, desc string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"Async agent launched successfully."}]}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":%q,"description":%q,"prompt":"p","outputFile":"o"}}`, lstamp(at), id, desc)
}

func note(id, status string) string {
	return fmt.Sprintf("<task-notification>\n<task-id>%s</task-id>\n<tool-use-id>toolu_1</tool-use-id>\n<status>%s</status>\n<summary>Agent %q finished</summary>\n<result>done %s</result>\n</task-notification>", id, status, id, status)
}

// finish is a notification as Claude Code writes it: queued, then delivered.
func finish(at time.Time, id, status string) []string {
	n := note(id, status)
	return []string{
		fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","timestamp":%q,"content":%s}`, lstamp(at), jsonStr(n)),
		fmt.Sprintf(`{"type":"attachment","timestamp":%q,"attachment":{"type":"queued_command","prompt":%s,"commandMode":"task-notification"}}`, lstamp(at), jsonStr(n)),
		fmt.Sprintf(`{"type":"user","timestamp":%q,"origin":{"kind":"task-notification"},"message":{"role":"user","content":%s}}`, lstamp(at), jsonStr(n)),
	}
}

func resume(at time.Time, id string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"ok"}]},"toolUseResult":{"success":true,"message":"resumed","resumedAgentId":%q}}`, lstamp(at), id)
}

func taskStop(at time.Time, id string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"stopped"}]},"toolUseResult":{"message":"stopped","task_id":%q,"task_type":"local_agent","command":"x"}}`, lstamp(at), id)
}

const (
	subToolCall = `{"type":"assistant","message":{"stop_reason":null,"content":[{"type":"tool_use","name":"Grep"}]}}`
	subResult   = `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`
	subFinal    = `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`
)

var t0 = time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

func TestSubagentsLaunchFinish(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "map the parser"), launch(t0, "a2222222222222222", "write tests"))
	s.subFile("a1111111111111111", t0.Add(time.Minute), subToolCall)
	got := Subagents(s.main, time.Time{}, t0.Add(2*time.Minute))
	if len(got) != 2 || got[0].ID != "a1111111111111111" || got[1].ID != "a2222222222222222" {
		t.Fatalf("both launched work, in launch order: %+v", got)
	}
	if a := got[0]; a.Title != "map the parser" || a.Type != "Explore" || a.Tool != "Grep" {
		t.Fatalf("title from the launch, type and tool from its own transcript: %+v", a)
	}
	if b := got[1]; b.Title != "write tests" || b.Tool != "" {
		t.Fatalf("no transcript of its own yet: %+v", b)
	}
	s.lines(finish(t0.Add(3*time.Minute), "a1111111111111111", "completed")...)
	if ids := s.working(time.Time{}, t0.Add(4*time.Minute)); len(ids) != 1 || ids[0] != "a2222222222222222" {
		t.Fatalf("a1 completed: %v", ids)
	}
	s.lines(finish(t0.Add(5*time.Minute), "a2222222222222222", "completed")...)
	if ids := s.working(time.Time{}, t0.Add(6*time.Minute)); ids != nil {
		t.Fatalf("all done: %v", ids)
	}
}

func TestSubagentsQuietForAnHour(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "long build"))
	// Its own transcript stops after a tool result: the old guess (written
	// within 2 minutes) would have dropped it long ago.
	s.subFile("a1111111111111111", t0.Add(time.Minute), subToolCall, subResult)
	if ids := s.working(time.Time{}, t0.Add(time.Hour)); len(ids) != 1 {
		t.Fatalf("quiet for an hour, no notification: still working: %v", ids)
	}
	if ids := s.working(time.Time{}, t0.Add(time.Minute+SubagentCap+time.Second)); ids != nil {
		t.Fatalf("no sign of life past the cap: over: %v", ids)
	}
}

func TestSubagentsResume(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "review"))
	s.lines(finish(t0.Add(time.Minute), "a1111111111111111", "completed")[0]) // queued only, not delivered yet
	if ids := s.working(time.Time{}, t0.Add(2*time.Minute)); ids != nil {
		t.Fatalf("completed: %v", ids)
	}
	s.lines(resume(t0.Add(3*time.Minute), "a1111111111111111"))
	if ids := s.working(time.Time{}, t0.Add(4*time.Minute)); len(ids) != 1 {
		t.Fatalf("resumed: working again: %v", ids)
	}
	// The first run's notification delivered after the resume ends nothing.
	s.lines(finish(t0.Add(time.Minute), "a1111111111111111", "completed")[1:]...)
	if ids := s.working(time.Time{}, t0.Add(5*time.Minute)); len(ids) != 1 {
		t.Fatalf("an old notification delivered late: still working: %v", ids)
	}
	s.lines(strings.ReplaceAll(finish(t0.Add(6*time.Minute), "a1111111111111111", "completed")[0], "done completed", "second run done"))
	if ids := s.working(time.Time{}, t0.Add(7*time.Minute)); ids != nil {
		t.Fatalf("the resumed run completed: %v", ids)
	}
}

func TestSubagentsStoppedOrFailed(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "one"), launch(t0, "a2222222222222222", "two"),
		launch(t0, "a3333333333333333", "three"), launch(t0, "a4444444444444444", "four"))
	s.lines(finish(t0.Add(time.Minute), "a1111111111111111", "failed")...)
	s.lines(finish(t0.Add(time.Minute), "a2222222222222222", "killed")[0])
	s.lines(taskStop(t0.Add(time.Minute), "a3333333333333333"))
	// a4 was stopped mid tool call (its transcript ends on a tool result,
	// never a final reply): its notification says so.
	s.subFile("a4444444444444444", t0.Add(time.Minute), subToolCall, subResult)
	s.lines(finish(t0.Add(time.Minute), "a4444444444444444", "stopped")[0])
	if ids := s.working(time.Time{}, t0.Add(2*time.Minute)); ids != nil {
		t.Fatalf("failed, killed, stopped: none working: %v", ids)
	}
}

func TestSubagentsEarlierRun(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "from the last run"))
	s.subFile("a1111111111111111", t0.Add(time.Minute), subToolCall)
	restart := t0.Add(10 * time.Minute)
	if ids := s.working(restart, restart.Add(time.Minute)); ids != nil {
		t.Fatalf("launched before the session's process started: not working: %v", ids)
	}
	s.lines(launch(restart.Add(time.Minute), "a2222222222222222", "this run"))
	if ids := s.working(restart, restart.Add(2*time.Minute)); len(ids) != 1 || ids[0] != "a2222222222222222" {
		t.Fatalf("this run's: %v", ids)
	}
}

func TestSubagentsIncremental(t *testing.T) {
	s := newSession(t)
	l := launch(t0, "a1111111111111111", "x")
	s.write(l[:40]) // a line still being written
	if ids := s.working(time.Time{}, t0); ids != nil {
		t.Fatalf("half a line: nothing yet: %v", ids)
	}
	s.write(l[40:] + "\n")
	if ids := s.working(time.Time{}, t0); len(ids) != 1 {
		t.Fatalf("the rest of the line: %v", ids)
	}
	seen := lifeCache.m[s.main].size
	s.lines(`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`)
	s.working(time.Time{}, t0)
	if got := lifeCache.m[s.main].size; got <= seen {
		t.Fatalf("read on from where it stopped: %d then %d", seen, got)
	}
	// A replaced file (rewritten from scratch) is read again from its start.
	if err := os.WriteFile(s.main, []byte(launch(t0, "a2222222222222222", "y")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ids := s.working(time.Time{}, t0); len(ids) != 1 || ids[0] != "a2222222222222222" {
		t.Fatalf("replaced: %v", ids)
	}
	// Appended past a chunk boundary, with a long line across it.
	big := fmt.Sprintf(`{"type":"user","message":{"content":%s}}`, jsonStr(strings.Repeat("x", lifeChunk+100)))
	s.lines(big, finish(t0.Add(time.Minute), "a2222222222222222", "completed")[0])
	if ids := s.working(time.Time{}, t0.Add(2*time.Minute)); ids != nil {
		t.Fatalf("finished after a long line: %v", ids)
	}
}

func TestSubagentsNotRecords(t *testing.T) {
	s := newSession(t)
	n := note("a1111111111111111", "completed")
	s.lines(launch(t0, "a1111111111111111", "x"),
		// A notification quoted in a tool's output (a file read, a command).
		fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","content":%s}]},"toolUseResult":{"stdout":%s}}`, jsonStr(n), jsonStr(n)),
		// A request blob carries old messages: never a record.
		fmt.Sprintf(`{"type":"api-request-blob","content":%s}`, jsonStr(n)),
		// A monitor's event: no status, the task goes on.
		fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","content":%s}`, jsonStr("<task-notification><task-id>a1111111111111111</task-id><event>line</event></task-notification>")),
		// A background shell command is not a subagent.
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"started"}]},"toolUseResult":{"stdout":"","backgroundTaskId":"b1234567"}}`,
	)
	if ids := s.working(time.Time{}, t0.Add(time.Minute)); len(ids) != 1 || ids[0] != "a1111111111111111" {
		t.Fatalf("only the launch counts: %v", ids)
	}
}

func TestSubagentsWorkflow(t *testing.T) {
	s := newSession(t)
	s.lines(fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":[{"type":"tool_result","content":"ok"}]},"toolUseResult":{"status":"async_launched","taskId":"w12345678","taskType":"local_workflow","workflowName":"review","summary":"review the branch","runId":"r"}}`, lstamp(t0)),
		// One that failed to compile never ran.
		fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":[{"type":"tool_result","content":"no"}]},"toolUseResult":{"status":"async_launched","taskId":"w87654321","taskType":"local_workflow","error":"bad script"}}`, lstamp(t0)))
	got := Subagents(s.main, time.Time{}, t0.Add(time.Minute))
	if len(got) != 1 || got[0].ID != "w12345678" || got[0].Type != "workflow" || got[0].Title != "review the branch" {
		t.Fatalf("a workflow run is agents at work: %+v", got)
	}
	s.lines(finish(t0.Add(2*time.Minute), "w12345678", "completed")[0])
	if ids := s.working(time.Time{}, t0.Add(3*time.Minute)); ids != nil {
		t.Fatalf("workflow done: %v", ids)
	}
}

func TestSubagentsEndedWithoutNotification(t *testing.T) {
	s := newSession(t)
	s.lines(launch(t0, "a1111111111111111", "x"))
	s.subFile("a1111111111111111", t0.Add(time.Minute), subToolCall, subResult, subFinal)
	if ids := s.working(time.Time{}, t0.Add(2*time.Minute)); len(ids) != 1 {
		t.Fatalf("just gave its final reply: the notification is on its way: %v", ids)
	}
	if ids := s.working(time.Time{}, t0.Add(time.Minute+subagentEndGrace+time.Second)); ids != nil {
		t.Fatalf("final reply long ago, no notification: over: %v", ids)
	}
}

func TestSubagentsFallbackSkipsKnown(t *testing.T) {
	s := newSession(t)
	now := time.Now()
	// Known to the main transcript and done, though its own file looks busy.
	s.lines(launch(now.Add(-time.Minute), "a1111111111111111", "x"))
	s.lines(finish(now.Add(-time.Minute), "a1111111111111111", "killed")[0])
	s.subFile("a1111111111111111", now, subToolCall)
	// Never mentioned (a foreground subagent): its own file decides.
	s.subFile("a2222222222222222", now, subToolCall)
	got := Subagents(s.main, time.Time{}, now)
	if len(got) != 1 || got[0].ID != "a2222222222222222" || got[0].Title != "look around" {
		t.Fatalf("the main transcript beats the guess: %+v", got)
	}
}

// Hook events from subagents carry the main thread's session id. They must
// not move the agent between running and idle while only subagents work.
func TestSubagentHooksNoFlap(t *testing.T) {
	s := newSession(t)
	now := time.Now()
	s.lines(launch(now.Add(-time.Minute), "a1111111111111111", "x"), launch(now.Add(-time.Minute), "a2222222222222222", "y"))
	e := &store.Entry{Transcript: s.main}
	e.A.StartedAt = now.Add(-time.Hour)
	Apply(e, Event{Name: "UserPromptSubmit", Prompt: "go"})
	Apply(e, Event{Name: "Stop"})
	if e.A.Status != model.Running || !e.Background || !e.A.Unseen || len(e.A.Subagents) != 2 ||
		e.A.Activity == nil || e.A.Activity.Detail != "2 subagents working" {
		t.Fatalf("turn over, subagents at work: running for them, no idle in between: %+v bg=%v", e.A, e.Background)
	}
	since := e.A.StatusSince
	sub := func(name, tool string) Event {
		return Event{Name: name, ToolName: tool, AgentID: "a1111111111111111", SessionID: "5e551011-aaaa-4bbb-8ccc-0123456789ab"}
	}
	for _, ev := range []Event{sub("PreToolUse", "Bash"), sub("PostToolUse", "Bash"), {Name: "Notification", NotificationType: "idle_prompt"}, sub("PreToolUse", "Read")} {
		Apply(e, ev)
		if FollowSubagents(e) && e.A.Status != model.Running {
			t.Fatalf("%s: flapped", ev.Name)
		}
		if e.A.Status != model.Running || !e.Background || e.A.Activity == nil || e.A.Activity.Tool != "Task" || !e.A.StatusSince.Equal(since) {
			t.Fatalf("%s from a subagent: still running for the subagents: %+v bg=%v", ev.Name, e.A, e.Background)
		}
	}

	// A subagent asks permission: that shows, and ends back on its work.
	Apply(e, Event{Name: "PermissionRequest", ToolName: "Bash", AgentID: "a1111111111111111"})
	if e.A.Status != model.Waiting {
		t.Fatalf("a subagent's permission prompt shows: %s", e.A.Status)
	}
	if FollowSubagents(e) || e.A.Status != model.Waiting {
		t.Fatal("waiting wins over the subagents")
	}
	Apply(e, sub("PreToolUse", "Read")) // another call by the one that asked? same id: answered
	if e.A.Status != model.Running || !e.Background {
		t.Fatalf("answered: back to the subagents' work: %+v bg=%v", e.A, e.Background)
	}
	Apply(e, Event{Name: "PermissionRequest", ToolName: "Bash", AgentID: "a1111111111111111"})
	Apply(e, Event{Name: "PreToolUse", ToolName: "Grep", AgentID: "a2222222222222222"})
	if e.A.Status != model.Waiting {
		t.Fatal("another subagent's tool call does not answer the prompt")
	}
	Apply(e, sub("PostToolUse", "Bash"))

	// The last one finishes: idle and unseen.
	s.lines(finish(now, "a1111111111111111", "completed")[0], finish(now, "a2222222222222222", "completed")[0])
	e.A.Unseen = false // you looked at it meanwhile
	e.A.Subagents = Subagents(s.main, e.RunSince(), time.Now())
	if !FollowSubagents(e) || e.A.Status != model.Idle || !e.A.Unseen || e.Background {
		t.Fatalf("all done: idle, unseen: %+v bg=%v", e.A, e.Background)
	}
	if FollowSubagents(e) {
		t.Fatal("settled")
	}
}

func TestSubagentHooksMainTurn(t *testing.T) {
	// The main thread in a turn: its own tool calls set the activity, and a
	// subagent's do not (no "thinking" from a subagent's tool result).
	e := &store.Entry{}
	Apply(e, Event{Name: "UserPromptSubmit", Prompt: "go"})
	Apply(e, Event{Name: "PreToolUse", ToolName: "Agent", ToolInput: json.RawMessage(`{"description":"dig"}`)})
	Apply(e, Event{Name: "PostToolUse", ToolName: "Grep", AgentID: "a1111111111111111"})
	if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != "Agent" {
		t.Fatalf("still in the Agent call: %+v", e.A.Activity)
	}
	// A subagent's permission prompt while the main thread waits on it.
	Apply(e, Event{Name: "PermissionRequest", ToolName: "Bash", AgentID: "a1111111111111111"})
	Apply(e, Event{Name: "PostToolUse", ToolName: "Bash", AgentID: "a1111111111111111"})
	if e.A.Status != model.Running || e.Background {
		t.Fatalf("answered: the turn goes on: %+v bg=%v", e.A, e.Background)
	}
	Apply(e, Event{Name: "Stop"})
	if e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("no subagents left: idle: %+v", e.A)
	}
}

func TestSubagentSessionStartRun(t *testing.T) {
	s := newSession(t)
	s.lines(launch(time.Now().Add(-time.Minute), "a1111111111111111", "x"))
	e := &store.Entry{Transcript: s.main}
	Now = func() time.Time { return time.Now().UTC() }
	Apply(e, Event{Name: "SessionStart", Source: "resume"})
	Apply(e, Event{Name: "Stop"})
	if e.A.Status != model.Idle || len(e.A.Subagents) != 0 {
		t.Fatalf("resumed: the subagent of the earlier run is gone: %+v", e.A)
	}
}

func TestSubagentsEscapedNotification(t *testing.T) {
	// A writer that escapes "<" and ">" (as Go's encoding/json does) still
	// writes a notification that counts.
	s := newSession(t)
	n, _ := json.Marshal(note("a1111111111111111", "completed"))
	s.lines(launch(t0, "a1111111111111111", "x"), `{"type":"queue-operation","operation":"enqueue","content":`+string(n)+`}`)
	if ids := s.working(time.Time{}, t0.Add(time.Minute)); ids != nil {
		t.Fatalf("escaped notification: %v", ids)
	}
}
