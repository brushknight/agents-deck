package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/cmux"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A made-up main transcript with background subagents in it.
func subTranscript(t *testing.T) (path string, add func(lines ...string)) {
	path = filepath.Join(t.TempDir(), "5e551011-aaaa-4bbb-8ccc-0123456789ab.jsonl")
	add = func(lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			fmt.Fprintln(f, l)
		}
	}
	add(`{"type":"user","message":{"role":"user","content":"go"}}`)
	return path, add
}

func subLaunch(id string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":[{"type":"tool_result","content":"launched"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":%q,"description":"dig"}}`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
}

func subDone(id string) string {
	return fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","content":"<task-notification><task-id>%s</task-id><status>completed</status><summary>done</summary></task-notification>"}`, id)
}

// The main thread finished its turn, its subagents work on, then the last
// one finishes and the main thread starts no turn: running all along, then
// idle and unseen ("feed me"), unless you are looking at its terminal.
func TestSubagentsKeepAgentWorking(t *testing.T) {
	for _, focused := range []bool{false, true} {
		tr, add := subTranscript(t)
		d := &Daemon{Store: store.New("", "test", "0")}
		e := &store.Entry{Transcript: tr, TmuxName: "agent-k1", SessionID: "5e551011-aaaa-4bbb-8ccc-0123456789ab"}
		e.A = model.Agent{ID: "k1", Tool: "claude", Status: model.Running, StartedAt: time.Now().Add(-time.Hour)}
		d.Store.Add(e)
		add(subLaunch("a1111111111111111")) // launched in this run
		if focused {
			d.front = "agent-k1"
		}
		if err := d.Hook("k1", []byte(`{"hook_event_name":"Stop","session_id":"5e551011-aaaa-4bbb-8ccc-0123456789ab"}`)); err != nil {
			t.Fatal(err)
		}
		got, _ := d.Store.Get("k1")
		if got.A.Status != model.Running || !got.Background || got.A.Activity == nil || got.A.Activity.Detail != "1 subagent working" {
			t.Fatalf("turn over, a subagent at work: running for it: %+v", got.A)
		}
		// A few polls, with the subagent's own hook events in between.
		for range 3 {
			_ = d.Hook("k1", []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","agent_id":"a1111111111111111","session_id":"5e551011-aaaa-4bbb-8ccc-0123456789ab"}`))
			d.Store.Update("k1", func(e *store.Entry) bool { return d.subagents(e, e.RunSince()) })
			if got, _ = d.Store.Get("k1"); got.A.Status != model.Running || !got.Background {
				t.Fatalf("steady while it works: %+v", got.A)
			}
		}
		add(subDone("a1111111111111111"))
		d.Store.Update("k1", func(e *store.Entry) bool {
			changed := d.subagents(e, e.RunSince())
			return lookedAt(e, focused) || changed // as the poll does
		})
		got, _ = d.Store.Get("k1")
		if got.A.Status != model.Idle || got.A.Unseen == focused || got.Background {
			t.Fatalf("focused=%v: idle, unseen unless looked at: %+v", focused, got.A)
		}
	}
}

// A watched session: Claude Code reports it busy while only its subagents
// work. The tile holds "N subagents working", it doesn't swing to the
// main thread thinking and back.
func TestWatchedSessionSubagents(t *testing.T) {
	tr, add := subTranscript(t)
	add(subLaunch("a1111111111111111"), subLaunch("a2222222222222222"),
		`{"type":"assistant","timestamp":"2026-03-02T10:00:00Z","message":{"id":"m1","stop_reason":"end_turn","content":[{"type":"text","text":"started them"}]}}`)
	d := &Daemon{Store: store.New("", "test", "0")}
	e := &store.Entry{External: true, Transcript: tr}
	e.A = model.Agent{ID: "l1", Tool: "claude", Status: model.Idle}
	l := claude.Live{Reported: true, Busy: true}
	for i := range 3 {
		setSubagents(e, l.Started)
		d.liveStatus(e, l)
		claude.FollowSubagents(e)
		if e.A.Status != model.Running || !e.Background || e.A.Activity == nil || e.A.Activity.Detail != "2 subagents working" {
			t.Fatalf("poll %d: %+v bg=%v", i, e.A, e.Background)
		}
	}
	add(subDone("a1111111111111111"), subDone("a2222222222222222"))
	l.Busy = false
	setSubagents(e, l.Started)
	d.liveStatus(e, l)
	claude.FollowSubagents(e)
	if e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("done: idle, unseen: %+v", e.A)
	}
}

// cmux's hooks hear the subagents too: "running" while the main thread is
// between turns is them at work, and holds steady.
func TestCmuxSubagents(t *testing.T) {
	e := &store.Entry{}
	e.A = model.Agent{Status: model.Running} // the main thread's turn
	e.A.Subagents = []model.Subagent{{ID: "a1111111111111111"}}
	cmuxApply(e, cmux.Session{Lifecycle: "running", Updated: 1}, "", "", time.Time{}, true)
	claude.FollowSubagents(e)
	since := e.A.StatusSince
	if e.A.Status != model.Running || !e.Background || !e.A.Unseen {
		t.Fatalf("turn over, subagents at work: %+v", e.A)
	}
	for _, state := range []string{"running", "idle", "running"} {
		if cmuxApply(e, cmux.Session{Lifecycle: state, Updated: 2}, "", "", time.Time{}, true) || claude.FollowSubagents(e) {
			t.Fatalf("%s: changed", state)
		}
		if !e.A.StatusSince.Equal(since) || !e.Background {
			t.Fatalf("%s: flapped: %+v", state, e.A)
		}
	}
	// The main thread starts a turn: its own again.
	cmuxApply(e, cmux.Session{Lifecycle: "running", Updated: 3}, "Read", "", time.Time{}, false)
	if e.Background || e.A.Activity == nil || e.A.Activity.Tool != "Read" {
		t.Fatalf("main turn: %+v", e.A)
	}
}
