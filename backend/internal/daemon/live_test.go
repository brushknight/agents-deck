package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

func TestWatchedHandStartedSession(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("no script(1)")
	}
	root := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(root, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "cfg"))
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-l%d", os.Getpid()) // no panes: nothing skipped
	t.Cleanup(func() { tmux.Socket = old })
	work := filepath.Join(root, "work")
	for _, d := range []string{work, filepath.Join(root, "home")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	work, _ = filepath.EvalSymlinks(work)

	// A "claude" running by hand in a terminal (script gives it one).
	fake := filepath.Join(root, "claude")
	if err := os.Symlink("/bin/sleep", fake); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("script", "-q", "/dev/null", fake, "30")
	cmd.Dir = work
	stdin, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	time.Sleep(500 * time.Millisecond)

	id := "0b5c2f4e-1111-4222-8333-944455556666"
	dir := claude.ProjectDirFor(work)
	_ = os.MkdirAll(dir, 0o700)
	tr := filepath.Join(dir, id+".jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(tr, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		for _, l := range lines {
			fmt.Fprintln(f, l)
		}
		f.Close()
	}
	finished := `{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","stop_reason":"end_turn","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":10,"output_tokens":5}}}`
	write(`{"type":"user","cwd":"`+work+`","message":{"content":"hi"}}`, finished)

	d := &Daemon{Store: store.New("", "test", "0")}
	d.live.claudeBin = "-" // the process scan, not this machine's real `claude agents`
	d.syncLive()
	cands := d.LiveJSON().([]LiveCandidate)
	var c *LiveCandidate
	for i := range cands {
		if cands[i].Cwd == work {
			c = &cands[i]
		}
	}
	if c == nil || c.SessionID != id || c.OnBoard {
		t.Fatalf("candidate: %+v", cands)
	}
	if len(d.Store.Entries()) != 0 {
		t.Fatal("nothing goes on the deck until you add it")
	}

	if err := d.AddLive(id[:8], 5); err != nil {
		t.Fatal(err)
	}
	e, ok := d.Store.Get(liveAgentID(id))
	if e.A.Slot != 5 {
		t.Fatalf("added into the cell you picked: slot %d", e.A.Slot)
	}
	if !ok || !e.External || e.A.Tool != "claude" || e.A.Status != model.Idle || e.A.Unseen || e.LiveTTY == "" {
		t.Fatalf("on the deck, idle (an old turn isn't news): %+v %q", e.A, e.LiveTTY)
	}

	// A new turn starts: running; it ends while we watch: hungry.
	write(`{"type":"user","message":{"content":"next"}}`, `{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Grep"}],"usage":{"input_tokens":20,"output_tokens":5}}}`)
	d.syncLive()
	if e, _ = d.Store.Get(liveAgentID(id)); e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != "Grep" {
		t.Fatalf("running: %+v %+v", e.A, e.A.Activity)
	}
	write(`{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`, `{"type":"assistant","message":{"id":"m3","model":"claude-opus-5-5","stop_reason":"end_turn","content":[{"type":"text","text":"all done"}],"usage":{"input_tokens":30,"output_tokens":5}}}`)
	d.syncLive()
	if e, _ = d.Store.Get(liveAgentID(id)); e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("finished while watched: hungry: %+v", e.A)
	}

	// Off the deck: the session keeps running, and stays listed.
	if err := d.Dismiss(liveAgentID(id)); err != nil {
		t.Fatal(err)
	}
	d.syncLive()
	if _, ok := d.Store.Get(liveAgentID(id)); ok {
		t.Fatal("removed from the deck")
	}
	if cmd.ProcessState != nil {
		t.Fatal("the session itself must keep running")
	}
}

// With Claude Code's own list (`claude agents --json`), background sessions
// show up too, and "blocked" means it needs you.
func TestClaudeAgentsList(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(root, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "cfg"))
	_ = os.MkdirAll(filepath.Join(root, "home"), 0o700)
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-a%d", os.Getpid())
	t.Cleanup(func() { tmux.Socket = old })
	id := "5d6e7f80-1111-4222-8333-944455556666"
	list := `[{"id":"ab12cd34","cwd":"/work/app","kind":"background","startedAt":1791446002609,"sessionId":"` + id + `","name":"refactor auth","state":"blocked","pid":4242,"status":"busy"}]`
	fake := filepath.Join(root, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho '"+list+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{Store: store.New("", "test", "0")}
	d.live.claudeBin = fake
	d.syncLive()
	cands := d.LiveJSON().([]LiveCandidate)
	if len(cands) != 1 || cands[0].SessionID != id || !cands[0].Background || cands[0].Title != "refactor auth" {
		t.Fatalf("%+v", cands)
	}
	if err := d.AddLive(id, -1); err != nil {
		t.Fatal(err)
	}
	e, _ := d.Store.Get(liveAgentID(id))
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.LiveAttach != "ab12cd34" {
		t.Fatalf("blocked = needs you: %+v %q", e.A, e.LiveAttach)
	}
}
