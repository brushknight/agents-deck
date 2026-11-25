package claude

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
)

// fakePS stands in for ps: every process i has terminal /dev/ttys<i>, and
// it records what it was asked.
type fakePS struct {
	asked [][]int
	fail  bool
}

func (f *fakePS) lookup(pids []int) (map[int]string, bool) {
	f.asked = append(f.asked, slices.Clone(pids))
	if f.fail {
		return map[int]string{}, false
	}
	out := map[int]string{}
	for _, p := range pids {
		if p%10 != 0 { // every tenth runs without a terminal
			out[p] = fmt.Sprintf("/dev/ttys%03d", p)
		}
	}
	return out, true
}

func sessionsList(pids ...int) []claudeAgent {
	var l []claudeAgent
	for _, p := range pids {
		l = append(l, claudeAgent{PID: p, Kind: "interactive", StartedAt: int64(p) * 1000})
	}
	return l
}

// 53 sessions listed 5 times a minute (every 12 s), for an hour, nothing
// changing: ps is asked once, at the start.
func TestTTYCacheIdle(t *testing.T) {
	var c ttyCache
	var ps fakePS
	var pids []int
	for p := 101; p <= 153; p++ {
		pids = append(pids, p)
	}
	list := append(sessionsList(pids...), claudeAgent{PID: 999, Kind: "background"})
	for i := 0; i < 5*60; i++ {
		got := c.of(list, ps.lookup)
		if got[101] != "/dev/ttys101" || got[110] != "" || len(got) != 48 {
			t.Fatalf("listing %d: %v", i, got)
		}
	}
	if len(ps.asked) != 1 || len(ps.asked[0]) != 53 {
		t.Fatalf("ps asked %d times: %v", len(ps.asked), ps.asked)
	}
	t.Logf("idle: ps ran %d times in an hour (was 300)", len(ps.asked))
}

// A new session every minute (and the oldest one ending): ps runs once a
// minute, about the new one only.
func TestTTYCacheBusy(t *testing.T) {
	var c ttyCache
	var ps fakePS
	pids := []int{101, 102, 103}
	for minute := 0; minute < 60; minute++ {
		if minute > 0 {
			pids = append(pids[1:], 200+minute)
		}
		for i := 0; i < 5; i++ {
			got := c.of(sessionsList(pids...), ps.lookup)
			for _, p := range pids {
				if p%10 != 0 && got[p] == "" {
					t.Fatalf("minute %d: no terminal for %d: %v", minute, p, got)
				}
			}
		}
		if minute > 0 && !slices.Equal(ps.asked[len(ps.asked)-1], []int{200 + minute}) {
			t.Fatalf("minute %d: asked about %v", minute, ps.asked[len(ps.asked)-1])
		}
	}
	if len(ps.asked) != 60 {
		t.Fatalf("ps ran %d times in an hour, want 60", len(ps.asked))
	}
	if len(c.m) != 3 {
		t.Fatalf("ended sessions must be forgotten: %v", c.m)
	}
}

// A pid used again by a new session, and a ps that failed: both asked again.
func TestTTYCacheReuseAndFailure(t *testing.T) {
	var c ttyCache
	var ps fakePS
	c.of(sessionsList(101), ps.lookup)
	reused := []claudeAgent{{PID: 101, Kind: "interactive", StartedAt: 5}}
	c.of(reused, ps.lookup)
	if len(ps.asked) != 2 {
		t.Fatal("a pid used again by another process must be looked up again")
	}
	ps.fail = true
	if got := c.of(sessionsList(102), ps.lookup); len(got) != 0 {
		t.Fatalf("failed: %v", got)
	}
	ps.fail = false
	if got := c.of(sessionsList(102), ps.lookup); got[102] != "/dev/ttys102" || len(ps.asked) != 4 {
		t.Fatalf("after a failure ps is asked again: %v %v", got, ps.asked)
	}
}

// The real ps: an answer for a running process and for one that ended.
func TestProcessTTYs(t *testing.T) {
	if _, ok := processTTYs([]int{os.Getpid()}); !ok {
		t.Fatal("ps about this process: no answer")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no true:", err)
	}
	if ttys, ok := processTTYs([]int{cmd.Process.Pid}); !ok || len(ttys) != 0 {
		t.Fatalf("a process that ended: %v %v", ttys, ok)
	}
}
