package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

func TestScreenFresh(t *testing.T) {
	act := time.Unix(1_800_000_000, 0)
	read := screenRead{at: act.Add(1500 * time.Millisecond), act: act}
	tick := act.Add(2 * time.Second)
	for _, c := range []struct {
		name string
		r    screenRead
		act  time.Time
		tick time.Time
		want bool
	}{
		{"nothing printed since", read, act, tick, true},
		{"printed since", read, act.Add(time.Second), tick, false},
		{"read in the second of the last output", screenRead{at: act.Add(500 * time.Millisecond), act: act}, act, tick, false},
		{"tmux looked before the read", read, act, act.Add(time.Second), false},
		{"tmux gave no time", screenRead{at: read.at}, time.Time{}, tick, false},
	} {
		if got := fresh(c.r, c.act, c.tick); got != c.want {
			t.Errorf("%s: fresh = %v", c.name, got)
		}
	}
}

// fakeTmux puts a script in place of tmux that logs every call and answers
// from files the test writes: state (for Look) and screen (capture-pane).
func fakeTmux(t *testing.T) (dir string, calls func() []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no tmux on Windows")
	}
	dir = t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\ncase \"$*\" in\n" +
		"*list-sessions*) cat '" + filepath.Join(dir, "state") + "' ;;\n" +
		"*capture-pane*) cat '" + filepath.Join(dir, "screen") + "' ;;\nesac\n"
	bin := filepath.Join(dir, "tmux")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "screen"), []byte("❯ \n"), 0o600)
	old, oldSock := tmux.Bin, tmux.Socket
	tmux.Bin, tmux.Socket = bin, "agentsdeck-test-fake"
	t.Cleanup(func() { tmux.Bin, tmux.Socket = old, oldSock })
	return dir, func() []string {
		b, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// Runs a minute of Run (60 ticks, syncLive's pane check every 3 s) with 53
// agents: 50 idle, one waiting on a question, one on a permission, and one
// starting (its screen is read for menus). busy: the three print something
// every second.
func tmuxPerMinute(t *testing.T, busy bool) (looks, captures, other int) {
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	dir, calls := fakeTmux(t)
	d := &Daemon{Store: store.New("", "test", "0")}
	long := time.Now().Add(-72 * time.Hour)
	add := func(name string, status model.Status, waiting *model.Prompt) {
		e := &store.Entry{TmuxName: "a-" + name}
		e.A = model.Agent{ID: name, Tool: "claude", Status: status, Waiting: waiting, StartedAt: long, StatusSince: long}
		d.Store.Add(e)
		d.Store.Update(name, func(e *store.Entry) bool {
			e.A.StartedAt, e.A.StatusSince = long, long // not just started
			return true
		})
	}
	for i := 0; i < 50; i++ {
		add(fmt.Sprintf("idle%02d", i), model.Idle, nil)
	}
	add("ask", model.Waiting, &model.Prompt{ID: "1", Kind: "question", Title: "which one?"})
	add("perm", model.Waiting, &model.Prompt{ID: "1", Kind: "permission", Title: "Bash"})
	add("start", model.Starting, nil)

	still := time.Now().Add(-time.Hour).Unix()
	state := func(sec int) {
		var b strings.Builder
		for _, e := range d.Store.Entries() {
			at := still
			if busy && (e.A.ID == "ask" || e.A.ID == "perm" || e.A.ID == "start") {
				at = time.Now().Unix() + int64(sec) // printed just now
			}
			fmt.Fprintf(&b, "s %s %d\n", e.TmuxName, at)
			fmt.Fprintf(&b, "p /dev/fake-pane-%s\n", e.A.ID)
		}
		b.WriteString("c a-idle00 /dev/fake-tty-1 1700000000 focused\n") // not a real terminal: nothing is written to it
		_ = os.WriteFile(filepath.Join(dir, "state"), []byte(b.String()), 0o600)
	}
	state(0)
	d.tick(1) // the first look sets up the server and every header
	start := len(calls())
	for s := 2; s <= 61; s++ {
		state(s)
		d.tick(s)
		if s%3 == 0 {
			if p := d.paneTTYs(); !p["/dev/fake-pane-ask"] {
				t.Fatalf("panes: %v", p)
			}
		}
	}
	for _, c := range calls()[start:] {
		switch {
		case strings.Contains(c, "list-sessions") && strings.Contains(c, "list-clients") && strings.Contains(c, "list-panes"):
			looks++
		case strings.Contains(c, "capture-pane"):
			captures++
		default:
			other++
			t.Logf("other: %.80s", c)
		}
	}
	return looks, captures, other
}

func TestTmuxCallsIdle(t *testing.T) {
	looks, captures, other := tmuxPerMinute(t, false)
	t.Logf("idle: %d tmux calls a minute (%d looks, %d screen reads, %d other)", looks+captures+other, looks, captures, other)
	if looks != 60 || captures != 0 || other != 0 {
		t.Fatal("idle: one look a second, no screen reads, nothing else")
	}
}

func TestTmuxCallsBusy(t *testing.T) {
	looks, captures, other := tmuxPerMinute(t, true)
	t.Logf("busy: %d tmux calls a minute (%d looks, %d screen reads, %d other)", looks+captures+other, looks, captures, other)
	// As before: the question's screen twice every 2 s, the permission's
	// once every 2 s, the starting agent's every second.
	if looks != 60 || captures != 60+30+60 || other != 0 {
		t.Fatal("busy: screens that change are read as often as before")
	}
}
