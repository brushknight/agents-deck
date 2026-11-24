package cpuguard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// fakeSpell is the guard: busy or not, with a setting.
type fakeSpell struct {
	busy bool
	set  Setting
}

func (f *fakeSpell) Busy() bool       { return f.busy }
func (f *fakeSpell) Setting() Setting { return f.set }

// fakePrio records which processes are in background mode, and fails the
// test when anything outside an agent's terminal is touched.
type fakePrio struct {
	t     *testing.T
	bg    map[int]bool
	never map[int]bool
}

func (f *fakePrio) Background(pid int, on bool) error {
	if f.never[pid] {
		f.t.Errorf("touched pid %d, which isn't an agent's", pid)
	}
	f.bg[pid] = on
	return nil
}

// on lists the pids in background mode now.
func (f *fakePrio) on() []int {
	var pids []int
	for pid, on := range f.bg {
		if on {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

// rig: three agents on their terminals (10 cores), the service (600) with
// a helper, a hook on a1's terminal, and a browser off any terminal.
type rig struct {
	t      *testing.T
	s      *Slower
	spell  *fakeSpell
	prio   *fakePrio
	now    time.Time
	agents []proc.AgentTerminal
	ps     []proc.PS
	psErr  error
	logs   []string
}

func ps(pid, ppid int, tty string, cpu float64, comm string) proc.PS {
	return proc.PS{PID: pid, PPID: ppid, TTY: tty, CPU: cpu, Start: "Mon Nov 23 10:00:00 2026", Comm: comm}
}

func newSlowRig(t *testing.T) *rig {
	r := &rig{t: t, spell: &fakeSpell{set: Default()}, now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)}
	r.prio = &fakePrio{t: t, bg: map[int]bool{}, never: map[int]bool{1: true, 500: true, 600: true, 601: true, 702: true}}
	r.agents = []proc.AgentTerminal{{TTY: "/dev/ttys003", ID: "a1", Title: "fix the login page"},
		{TTY: "/dev/ttys004", ID: "a2", Title: "write the docs"}, {TTY: "/dev/ttys005", ID: "a3", Title: "port the tests"}}
	r.ps = []proc.PS{
		ps(1, 0, "??", 0, "/sbin/launchd"),
		ps(500, 1, "??", 300, "/Applications/Example Browser.app/Contents/MacOS/Example Browser"),
		ps(600, 1, "??", 2, "/usr/local/bin/agentctl"),
		ps(601, 600, "??", 1, "/usr/sbin/iostat"),
		ps(700, 690, "ttys003", 30, "claude"),
		ps(701, 700, "ttys003", 50, "/usr/local/go/bin/go"),
		ps(702, 700, "ttys003", 5, "/usr/local/bin/agentctl"),
		ps(710, 690, "ttys004", 60, "claude"),
		ps(720, 690, "ttys005", 55, "claude"),
	}
	r.s = &Slower{Guard: r.spell, Prio: r.prio, Self: 600, Cores: 10,
		File: filepath.Join(t.TempDir(), "slowed.json"), Now: func() time.Time { return r.now },
		Agents: func() []proc.AgentTerminal { return r.agents },
		List:   func() ([]proc.PS, error) { return r.ps, r.psErr },
		Logf:   func(f string, a ...any) { r.logs = append(r.logs, f) }}
	return r
}

// step moves the clock on and looks once.
func (r *rig) step(d time.Duration) {
	r.now = r.now.Add(d)
	r.s.Step()
}

func (r *rig) wantBG(want ...int) {
	r.t.Helper()
	if got := r.prio.on(); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		r.t.Errorf("in background mode: %v, want %v", got, want)
	}
}

func (r *rig) wantSlowed(want ...string) {
	r.t.Helper()
	if got := r.s.Slowed(); !reflect.DeepEqual(got, append([]string{}, want...)) {
		r.t.Errorf("slowed %v, want %v", got, want)
	}
}

func (r *rig) file() []slowedPID {
	b, err := os.ReadFile(r.s.File)
	if err != nil {
		return nil
	}
	var f slowedFile
	_ = json.Unmarshal(b, &f)
	return f.Slowed
}

// On entering busy the heaviest agent goes in background mode: every
// process on its terminal but the deck's hook; no program, not the deck.
// Each agent's CPU use is there while busy, and gone once calm.
func TestSlowHeaviest(t *testing.T) {
	r := newSlowRig(t)
	r.step(5 * time.Second)
	r.wantBG() // calm: nothing
	if _, cpu := r.s.Use(); len(cpu) != 0 {
		t.Errorf("cpu while calm: %v", cpu)
	}
	r.spell.busy = true
	r.step(5 * time.Second)
	r.wantSlowed("a1")
	r.wantBG(700, 701)
	slowedBy, cpu := r.s.Use()
	if slowedBy["a1"] != ByGuard || len(slowedBy) != 1 || cpu["a1"] != 8 || cpu["a2"] != 6 || cpu["a3"] != 5.5 {
		t.Errorf("use: %v %v", slowedBy, cpu)
	}
	if f := r.file(); len(f) != 2 || f[0] != (slowedPID{700, "Mon Nov 23 10:00:00 2026", "a1"}) || f[1].PID != 701 {
		t.Errorf("file %+v", f)
	}
	r.spell.busy = false
	r.step(5 * time.Second)
	r.wantSlowed()
	r.wantBG()
	if _, cpu := r.s.Use(); len(cpu) != 0 {
		t.Errorf("cpu once calm: %v", cpu)
	}
	if _, err := os.Stat(r.s.File); !os.IsNotExist(err) {
		t.Errorf("the file stays once calm: %v", err)
	}
}

// One more agent each window the computer stays busy, SlowCap in all, only
// agents using at least SlowFrom.
func TestSlowEscalation(t *testing.T) {
	r := newSlowRig(t)
	r.agents = append(r.agents, proc.AgentTerminal{TTY: "/dev/ttys006", ID: "a4", Title: "idle one"},
		proc.AgentTerminal{TTY: "/dev/ttys007", ID: "a5", Title: "fourth"})
	r.ps = append(r.ps, ps(730, 690, "ttys006", 4, "claude"), ps(740, 690, "ttys007", 52, "claude"))
	r.spell.busy = true
	r.step(5 * time.Second)
	r.wantSlowed("a1")
	r.step(30 * time.Second)
	r.step(30 * time.Second)
	r.step(30 * time.Second)
	r.wantSlowed("a1") // 1.5 minutes: not a window (2m) yet
	r.step(30 * time.Second)
	r.wantSlowed("a1", "a2")
	r.step(2 * time.Minute)
	r.wantSlowed("a1", "a2", "a3")
	r.step(2 * time.Minute)
	r.step(2 * time.Minute)
	r.wantSlowed("a1", "a2", "a3") // the cap: a5 stays at full speed
	r.wantBG(700, 701, 710, 720)
	r.spell.busy = false
	r.step(5 * time.Second)
	r.wantBG()
}

// Every 30 s the processes the agent started since go in background mode
// too, and ended ones leave the record.
func TestSlowReapply(t *testing.T) {
	r := newSlowRig(t)
	r.spell.busy = true
	r.step(5 * time.Second)
	r.ps = append(r.ps[:5], ps(701, 700, "ttys003", 50, "go"), ps(703, 701, "ttys003", 20, "/usr/bin/cc"))
	r.ps[4].Start = "Mon Nov 23 11:00:00 2026" // 700 ended, and its pid came back as another process
	r.step(10 * time.Second)
	r.wantBG(700, 701) // not yet: 30 s
	r.step(20 * time.Second)
	r.wantBG(700, 701, 703)
	pids := map[int]string{}
	for _, p := range r.file() {
		pids[p.PID] = p.Start
	}
	if len(pids) != 3 || pids[700] != "Mon Nov 23 11:00:00 2026" || pids[703] == "" {
		t.Errorf("file %v", pids)
	}
}

// Full speed undoes one agent for the rest of the busy spell; the next
// spell may slow it again.
func TestSlowFullSpeed(t *testing.T) {
	r := newSlowRig(t)
	if err := r.s.FullSpeed("a1"); !errors.Is(err, ErrNotSlowed) {
		t.Errorf("full speed, not slowed: %v", err)
	}
	r.spell.busy = true
	r.step(5 * time.Second)
	r.ps = append(r.ps, ps(703, 701, "ttys003", 1, "cc")) // started while slowed: it inherited background mode
	r.prio.bg[703] = true
	if err := r.s.FullSpeed("a1"); err != nil {
		t.Fatal(err)
	}
	r.wantSlowed()
	r.wantBG()
	r.step(30 * time.Second)
	r.wantSlowed() // a window after the first pick: not yet
	r.step(90 * time.Second)
	r.wantSlowed("a2") // the next heaviest, never a1 again in this spell
	r.step(2 * time.Minute)
	r.wantSlowed("a2", "a3")
	r.spell.busy = false
	r.step(5 * time.Second)
	r.spell.busy = true
	r.step(5 * time.Second)
	r.wantSlowed("a1")
}

// An agent that ends is undone: its processes that still run (same pid
// and start), wherever they are now.
func TestSlowAgentEnds(t *testing.T) {
	r := newSlowRig(t)
	r.spell.busy = true
	r.step(5 * time.Second)
	r.agents = r.agents[1:]
	r.ps = []proc.PS{ps(701, 1, "??", 50, "go"), ps(710, 690, "ttys004", 60, "claude")}
	r.step(30 * time.Second)
	if r.prio.bg[701] {
		t.Error("701 still in background mode")
	}
	r.wantSlowed() // the next heaviest only a window after the first
	r.step(90 * time.Second)
	r.wantSlowed("a2")
}

// Slow it by hand while busy, not while calm, not for an agent without a
// terminal; calm undoes it. Slow off: the guard picks none, and undoes its
// own.
func TestSlowNow(t *testing.T) {
	r := newSlowRig(t)
	if err := r.s.SlowNow("a2"); !errors.Is(err, ErrNotBusy) {
		t.Errorf("calm: %v", err)
	}
	r.spell.busy = true
	r.spell.set.Slow = false
	if err := r.s.SlowNow("a9"); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("unknown: %v", err)
	}
	if err := r.s.SlowNow("a3"); err != nil {
		t.Fatal(err)
	}
	r.wantSlowed("a3")
	r.wantBG(720)
	r.step(5 * time.Second)
	r.step(5 * time.Minute)
	r.wantSlowed("a3") // Slow is off: no pick, and a3 stays
	r.spell.set.Slow = true
	r.step(30 * time.Second)
	r.wantSlowed("a1", "a3")
	// a3 by you, a1 by the guard
	if got := r.s.SlowedByYou(); !reflect.DeepEqual(got, []string{"a3"}) {
		t.Errorf("slowed by you: %v", got)
	}
	if by, _ := r.s.Use(); by["a1"] != ByGuard || by["a3"] != ByYou {
		t.Errorf("use: %v", by)
	}
	r.spell.set.Slow = false
	r.step(30 * time.Second)
	r.wantSlowed("a3")
	r.wantBG(720)
	r.spell.busy = false
	r.step(5 * time.Second)
	r.wantBG()
}

// Stop undoes everything; after it the slower does nothing.
func TestSlowStop(t *testing.T) {
	r := newSlowRig(t)
	r.spell.busy = true
	r.step(5 * time.Second)
	r.s.Stop()
	r.wantBG()
	r.step(30 * time.Second)
	r.wantBG()
	if _, err := os.Stat(r.s.File); !os.IsNotExist(err) {
		t.Errorf("file after stop: %v", err)
	}
}

// A look that fails leaves the undo for the next one, and in the file.
func TestSlowUndoWithoutPS(t *testing.T) {
	r := newSlowRig(t)
	r.spell.busy = true
	r.step(5 * time.Second)
	r.psErr = errors.New("ps: timed out")
	r.spell.busy = false
	r.step(5 * time.Second)
	r.wantBG(700, 701)
	if len(r.file()) != 2 {
		t.Errorf("file %+v", r.file())
	}
	r.psErr = nil
	r.step(5 * time.Second)
	r.wantBG()
}

// After a crash: every recorded process that still runs is put back to
// full speed; a reused pid (another start time) and the deck are left
// alone; the file goes.
func TestSlowRecover(t *testing.T) {
	r := newSlowRig(t)
	for _, pid := range []int{700, 701, 710} {
		r.prio.bg[pid] = true
	}
	b, _ := json.Marshal(slowedFile{Slowed: []slowedPID{
		{700, "Mon Nov 23 10:00:00 2026", "a1"},
		{710, "Sun Nov 22 08:00:00 2026", "a2"}, // ended; 710 is another process now
		{600, "Mon Nov 23 10:00:00 2026", ""},   // the deck
		{999, "Mon Nov 23 10:00:00 2026", "a1"}, // gone
	}})
	if err := os.WriteFile(r.s.File, b, 0o600); err != nil {
		t.Fatal(err)
	}
	r.s.Recover()
	r.wantBG(701, 710)
	if _, err := os.Stat(r.s.File); !os.IsNotExist(err) {
		t.Errorf("file after recover: %v", err)
	}
	r.s.Recover() // no file: nothing
}

// The guard's state names the slowed agents; Busy follows it.
func TestGuardSlowedState(t *testing.T) {
	g, m, _ := newTest(t)
	g.Slowed = func() []string { return []string{"a1"} }
	if g.Busy() {
		t.Fatal("busy before a look")
	}
	m.run(2*time.Minute+5*time.Second, 95, g)
	s, _ := g.State()
	if !g.Busy() || !reflect.DeepEqual(s.Slowed, []string{"a1"}) {
		t.Errorf("busy %v, slowed %v", g.Busy(), s.Slowed)
	}
	g.Slowed = nil
	if s, _ = g.State(); s.Slowed == nil {
		t.Error("slowed is null, want []")
	}
}
