package cpuguard

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// fakeMeter keeps a reading every 5 s on a fake clock and averages them the
// way proc.MachineMeter does: nothing until the readings reach back.
type fakeMeter struct {
	now   time.Time
	start time.Time
	at    []time.Time
	busy  []float64
	off   string // not available, and why
}

func (m *fakeMeter) run(d time.Duration, busy float64, g *Guard) {
	for end := m.now.Add(d); m.now.Before(end); {
		m.now = m.now.Add(every)
		m.at, m.busy = append(m.at, m.now), append(m.busy, busy)
		g.Step()
	}
}

func (m *fakeMeter) Report() proc.MachineCPU {
	if m.off != "" || len(m.busy) == 0 {
		return proc.MachineCPU{Why: m.off}
	}
	return proc.MachineCPU{Available: true, Now: m.busy[len(m.busy)-1]}
}

func (m *fakeMeter) Average(span time.Duration) (float64, bool) {
	if m.off != "" || m.now.Sub(m.start) < span {
		return 0, false
	}
	sum, n := 0.0, 0
	for i := len(m.at) - 1; i >= 0 && m.at[i].After(m.now.Add(-span)); i-- {
		sum += m.busy[i]
		n++
	}
	return sum / float64(n), n > 0
}

func newTest(t *testing.T) (*Guard, *fakeMeter, *[]string) {
	t0 := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	m := &fakeMeter{now: t0, start: t0}
	var logs []string
	g := New(m)
	g.Now = func() time.Time { return m.now }
	g.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	g.Top = func() []proc.CPUUser {
		return []proc.CPUUser{{Name: "fix-tests", Percent: 61, Commands: []proc.Command{{Name: "xcodebuild", Percent: 30}, {Name: "claude", Percent: 4}}}, {Name: "agents deck", Percent: 1}}
	}
	return g, m, &logs
}

func busy(g *Guard) bool { s, _ := g.State(); return s.Busy }

// Over the limit for the whole window: busy. Under limit minus 10 for a
// minute: calm. One log line each way, naming the top users.
func TestBusyAndCalm(t *testing.T) {
	g, m, logs := newTest(t)
	changed := 0
	g.Changed = func() { changed++ }

	m.run(90*time.Second, 95, g) // 1.5 of the 2 minutes
	if busy(g) {
		t.Fatal("busy before the window was full")
	}
	m.run(30*time.Second, 95, g)
	s, _ := g.State()
	if !s.Busy || s.Since == nil || !s.Since.Equal(m.now.Add(-2*time.Minute).UTC()) || !s.Hold || s.Limit != 80 {
		t.Fatalf("after 2 minutes at 95%%: %+v", s)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "95% of the CPU for 2m, over your limit of 80%") || !strings.Contains((*logs)[0], "fix-tests 61% (xcodebuild 30%, claude 4%), agents deck 1%") {
		t.Fatalf("log %q", *logs)
	}
	if held, why := g.Holding(); !held || !strings.Contains(why, "busy (95% CPU for 2 minutes); try again in a few minutes") {
		t.Fatalf("holding %v %q", held, why)
	}

	// 75% is under the limit but not 10 under it: still busy.
	m.run(3*time.Minute, 75, g)
	if !busy(g) {
		t.Fatal("calm at 75% with a limit of 80")
	}
	// The last minute's average under 70% clears it.
	m.run(30*time.Second, 65, g) // (75+65)/2 = 70: not under
	if !busy(g) {
		t.Fatal("calm at a minute's average of 70%")
	}
	m.run(30*time.Second, 65, g)
	if busy(g) {
		t.Fatal("still busy after a minute at 65%")
	}
	if len(*logs) != 2 || !strings.Contains((*logs)[1], "calm again: 69% of the CPU for a minute, after 6 minutes busy") {
		t.Fatalf("log %q", *logs)
	}
	if changed != 2 {
		t.Errorf("changed %d times", changed)
	}
	if held, _ := g.Holding(); held {
		t.Error("holding while calm")
	}
}

// The average is what counts: short spikes over the limit inside a calm
// window don't make it busy, and around the limit it doesn't flap.
func TestNoFlapping(t *testing.T) {
	g, m, logs := newTest(t)
	g.Set(Setting{On: true, Limit: 80, For: "1m", Hold: true})
	for i := 0; i < 10; i++ {
		m.run(20*time.Second, 100, g)
		m.run(40*time.Second, 60, g)
	}
	if busy(g) || len(*logs) != 0 {
		t.Fatalf("spikes made it busy: %q", *logs)
	}
	m.run(time.Minute, 85, g)
	if !busy(g) {
		t.Fatal("not busy after a minute at 85%")
	}
	// Going back and forth between 78% and 85% never clears it.
	for i := 0; i < 10; i++ {
		m.run(30*time.Second, 78, g)
		m.run(30*time.Second, 85, g)
	}
	if !busy(g) || len(*logs) != 1 {
		t.Fatalf("flapped: %q", *logs)
	}
}

// Send them now releases the hold for this busy spell only.
func TestRelease(t *testing.T) {
	g, m, _ := newTest(t)
	g.Queued = func() int { return 3 }
	if g.Release() {
		t.Error("released while calm")
	}
	m.run(2*time.Minute, 90, g)
	if s, _ := g.State(); !s.Hold || s.Held != 3 {
		t.Fatalf("before %+v", s)
	}
	if !g.Release() {
		t.Fatal("nothing released")
	}
	if s, _ := g.State(); !s.Busy || s.Hold || s.Held != 0 || !s.Released {
		t.Fatalf("after %+v", s)
	}
	if held, _ := g.Holding(); held {
		t.Error("still holding")
	}
	m.run(time.Minute, 40, g) // calm
	m.run(2*time.Minute, 90, g)
	if held, _ := g.Holding(); !held {
		t.Error("the next busy spell isn't held")
	}
}

// Hold off: busy shows, nothing is held. Guard off: no state, nothing held.
func TestSwitches(t *testing.T) {
	g, m, _ := newTest(t)
	g.Set(Setting{On: true, Limit: 80, For: "2m", Hold: false})
	m.run(2*time.Minute, 90, g)
	if s, _ := g.State(); !s.Busy || s.Hold {
		t.Fatalf("hold off: %+v", s)
	}
	if held, _ := g.Holding(); held {
		t.Error("holding with hold off")
	}
	g.Set(Setting{On: false, Limit: 80, For: "2m", Hold: true})
	m.run(5*time.Second, 90, g)
	if _, ok := g.State(); ok {
		t.Error("a state while off")
	}
	if held, _ := g.Holding(); held {
		t.Error("holding while off")
	}
}

// A reader that stops (or a system without one) never holds anything.
func TestUnavailable(t *testing.T) {
	g, m, logs := newTest(t)
	m.off = "not measured on this system"
	m.run(10*time.Minute, 100, g)
	s, ok := g.State()
	if !ok || s.Available || s.Busy || s.Why != "not measured on this system" {
		t.Fatalf("no reader: %+v", s)
	}
	m.off = ""
	m.run(5*time.Minute, 100, g)
	if !busy(g) {
		t.Fatal("not busy at 100%")
	}
	m.off = "iostat stopped"
	m.run(5*time.Second, 100, g)
	if held, _ := g.Holding(); held || busy(g) {
		t.Fatal("still holding without readings")
	}
	if last := (*logs)[len(*logs)-1]; !strings.Contains(last, "can't read the CPU (iostat stopped), so nothing is held") {
		t.Fatalf("log %q", last)
	}
}

// The busiest minute of the day, reset the next day.
func TestPeak(t *testing.T) {
	g, m, _ := newTest(t)
	m.run(time.Minute, 40, g)
	m.run(time.Minute, 70, g)
	at := m.now
	m.run(time.Minute, 30, g)
	if s, _ := g.State(); s.Peak == nil || s.Peak.Percent != 70 || !s.Peak.At.Equal(at) {
		t.Fatalf("peak %+v", s.Peak)
	}
	m.now = m.now.Add(24 * time.Hour)
	m.run(time.Minute, 20, g)
	if s, _ := g.State(); s.Peak.Percent != 20 {
		t.Fatalf("the next day %+v", s.Peak)
	}
}

// config.json: defaults for what's left out, bad values back to defaults,
// and a setting written and read back is the same.
func TestSettingConfig(t *testing.T) {
	none := Schedule{Blocks: []Block{}}
	if s := FromConfig(nil); !reflect.DeepEqual(s, Default()) || !reflect.DeepEqual(s, Setting{On: true, Limit: 80, For: "2m", Hold: true, Slow: true, Schedule: none}) {
		t.Fatalf("default %+v", s)
	}
	var cfg map[string]any
	_ = json.Unmarshal([]byte(`{"cpuGuard": {"hold": false, "limit": 200, "for": "7m"}}`), &cfg)
	if s := FromConfig(cfg["cpuGuard"]); !reflect.DeepEqual(s, Setting{On: true, Limit: 80, For: "2m", Hold: false, Slow: true, Schedule: none}) {
		t.Fatalf("partial %+v", s)
	}
	until := time.Date(2026, 3, 4, 18, 0, 0, 0, time.UTC)
	want := Setting{On: false, Limit: 65, For: "5m", Hold: true, Slow: false,
		Schedule: Schedule{On: true, Blocks: []Block{{Days: []string{"mon", "tue"}, From: "09:00", To: "18:00", Limit: 60}}},
		Override: &Override{Limit: 70, Until: until, By: "agentctl", At: until.Add(-time.Hour)}}
	b, _ := json.Marshal(map[string]any{"cpuGuard": want})
	_ = json.Unmarshal(b, &cfg)
	if s := FromConfig(cfg["cpuGuard"]); !reflect.DeepEqual(s, want) {
		t.Fatalf("round trip %+v", s)
	}
	// A schedule edited by hand into something wrong is turned off; a
	// broken override is dropped.
	_ = json.Unmarshal([]byte(`{"cpuGuard": {"schedule": {"on": true, "blocks": [{"days": ["mon"], "from": "09:15", "to": "10:00", "limit": 60}]},
		"override": {"limit": 61, "until": "2026-03-04T18:00:00Z"}}}`), &cfg)
	if s := FromConfig(cfg["cpuGuard"]); s.Schedule.On || len(s.Schedule.Blocks) != 1 || s.Override != nil {
		t.Fatalf("broken %+v", s)
	}
	if want.Window() != 5*time.Minute {
		t.Error("window")
	}
	for _, bad := range []Setting{{Limit: 59, For: "2m"}, {Limit: 96, For: "2m"}, {Limit: 80, For: "3m"}} {
		if bad.Check() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}
