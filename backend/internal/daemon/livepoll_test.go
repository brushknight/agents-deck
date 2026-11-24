package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// fakeAgents drives an agentsPoller the way syncLive does (every 3 s) with
// a fake clock, a fake `claude agents --json` and fake session files.
type fakeAgents struct {
	t     time.Time
	keys  map[string]string // account folder -> its session files
	asked map[string]int
	p     *agentsPoller
}

func newFakeAgents() *fakeAgents {
	f := &fakeAgents{t: time.Unix(1_800_000_000, 0), keys: map[string]string{}, asked: map[string]int{}}
	f.p = &agentsPoller{
		now: func() time.Time { return f.t },
		key: func(dir string, _ func(string) bool) (string, bool) {
			k, ok := f.keys[dir]
			return k, ok
		},
	}
	return f
}

func (f *fakeAgents) tick(accs []accounts.Account) {
	f.p.sessions(accs, func(a accounts.Account) ([]claude.Live, bool) {
		f.asked[a.Path()]++
		return []claude.Live{{SessionID: "s-" + a.Name}}, true
	}, nil)
	f.t = f.t.Add(3 * time.Second)
}

var twoAccounts = []accounts.Account{{Name: "work", Dir: "/cfg/work"}, {Name: "home", Dir: "/cfg/home"}}

// perMinute runs syncLive's loop for span and returns how often each
// account was asked, per minute; change, when set, runs before every tick.
func perMinute(f *fakeAgents, span time.Duration, change func(i int)) float64 {
	before := f.p.runs
	n := int(span / (3 * time.Second))
	for i := 0; i < n; i++ {
		if change != nil {
			change(i)
		}
		f.tick(twoAccounts)
	}
	return float64(f.p.runs-before) / span.Minutes() / float64(len(twoAccounts))
}

// Idle (no session file changes): asked once, then every agentsMaxAge
// rather than every 12 s.
func TestAgentsPollIdle(t *testing.T) {
	f := newFakeAgents()
	f.keys["/cfg/work"], f.keys["/cfg/home"] = "a", "b"
	f.tick(twoAccounts) // the first time
	got := perMinute(f, time.Hour, nil)
	if got != 0.2 {
		t.Fatalf("idle: %.2f runs a minute per account, want 0.2 (every 5 min)", got)
	}
	t.Logf("idle: %.2f runs/min per account (was 5)", got)
}

// Busy (a hand-started session changes status all the time): no more often
// than before, every 12 s (the first 3 s tick at least 10 s on).
func TestAgentsPollBusy(t *testing.T) {
	f := newFakeAgents()
	got := perMinute(f, time.Hour, func(i int) {
		f.keys["/cfg/work"] = fmt.Sprint(i) // changes every tick
		f.keys["/cfg/home"] = "same"
	})
	if want := (5.0 + 0.2) / 2; got < want-0.05 || got > want+0.05 {
		t.Fatalf("busy: %.2f runs a minute per account, want %.2f", got, want)
	}
	if f.asked["/cfg/work"] < 299 || f.asked["/cfg/home"] > 13 {
		t.Fatalf("only the account that changed is asked again: %v", f.asked)
	}
	t.Logf("busy: %v runs in an hour", f.asked)
}

// A change after a long quiet spell is picked up as soon as agentsMinGap
// allows: at the next tick, not after agentsMaxAge.
func TestAgentsPollChangeResets(t *testing.T) {
	f := newFakeAgents()
	one := twoAccounts[:1]
	f.keys["/cfg/work"] = "a"
	f.tick(one)
	for i := 0; i < 40; i++ { // two quiet minutes
		f.tick(one)
	}
	if f.p.runs != 1 {
		t.Fatalf("quiet: asked %d times", f.p.runs)
	}
	f.keys["/cfg/work"] = "b" // a new session started
	f.tick(one)
	if f.p.runs != 2 {
		t.Fatal("a change must be picked up at the next tick")
	}
	f.keys["/cfg/work"] = "c" // and another, 3 s later
	f.tick(one)
	f.tick(one)
	f.tick(one)
	if f.p.runs != 2 {
		t.Fatal("not again within agentsMinGap")
	}
	f.tick(one) // 12 s after the last run
	if f.p.runs != 3 {
		t.Fatal("the second change, once agentsMinGap passed")
	}
}

func TestAgentsPollDue(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	p := agentsPoll{at: t0, key: "k", keyOK: true, ok: true}
	for _, c := range []struct {
		after time.Duration
		key   string
		keyOK bool
		ok    bool
		want  bool
	}{
		{5 * time.Second, "other", true, true, false}, // within the gap: never
		{10 * time.Second, "k", true, true, false},    // nothing changed
		{10 * time.Second, "other", true, true, true}, // changed
		{10 * time.Second, "", false, true, true},     // can't tell: every 10 s, as before
		{10 * time.Second, "k", true, false, true},    // the last answer failed
		{4 * time.Minute, "k", true, true, false},
		{5 * time.Minute, "k", true, true, true}, // the safety net
	} {
		p.ok = c.ok
		if got := p.due(t0.Add(c.after), c.key, c.keyOK); got != c.want {
			t.Errorf("%+v: due = %v", c, got)
		}
	}
	if !(&agentsPoll{}).due(t0, "", false) {
		t.Error("never asked: due")
	}
}

// An account that doesn't answer (a claude too old for `agents`) is asked
// again every agentsMinGap, as before, and the caller falls back to the
// process scan.
func TestAgentsPollFailure(t *testing.T) {
	f := newFakeAgents()
	f.keys["/cfg/work"] = "a"
	one := twoAccounts[:1]
	fail := func(accounts.Account) ([]claude.Live, bool) { return nil, false }
	for i := 0; i < 20; i++ { // a minute
		if _, ok := f.p.sessions(one, fail, nil); ok {
			t.Fatal("no answer: not ok")
		}
		f.t = f.t.Add(3 * time.Second)
	}
	if f.p.runs != 5 {
		t.Fatalf("asked %d times in a minute, want 5", f.p.runs)
	}
}
