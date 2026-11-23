package pets

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// rig is a book on a fake clock with one agent, "a1".
type rig struct {
	t    *testing.T
	b    *Book
	at   time.Time
	tok  int64
	sess string
	st   model.Status
}

func newRig(t *testing.T, path string) *rig {
	r := &rig{t: t, at: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), sess: "s1", st: model.Idle}
	r.b = Open(path, nil)
	r.b.now = func() time.Time { return r.at }
	return r
}

func (r *rig) feed() bool {
	return r.b.Feed([]Sight{{ID: "a1", Session: r.sess, Tokens: model.Tokens{Input: r.tok / 2, Output: r.tok - r.tok/2, CacheRead: 9_000_000}, Status: r.st}})
}

func (r *rig) eat(n int64) { r.tok += n; r.feed() }

func (r *rig) wait(d time.Duration) { r.at = r.at.Add(d); r.feed() }

func (r *rig) view() *model.Pet { return r.b.View(model.Agent{ID: "a1"}) }

func TestLevels(t *testing.T) {
	for _, c := range []struct {
		ate   int64
		level int
		stage string
	}{
		{0, 0, Egg}, {49_999, 0, Egg}, {50_000, 1, Baby}, {62_500, 2, Baby}, {122_070, 5, Kid},
		{372_529, 10, Grown}, {3_469_447, 20, Strong}, {32_311_743, 30, Mighty}, {300_926_554, 40, Legend},
	} {
		lv, next := LevelOf(c.ate)
		if lv != c.level || StageOf(lv) != c.stage {
			t.Errorf("ate %d: level %d %s, want %d %s", c.ate, lv, StageOf(lv), c.level, c.stage)
		}
		if next <= c.ate || next != Threshold(lv+1) {
			t.Errorf("ate %d: next level at %d", c.ate, next)
		}
	}
	// The design's milestones: kid about 120k, grown 370k, strong 3.5M, mighty 32M, legend 300M.
	if Threshold(5) != 122_070 || Threshold(40) != 300_926_554 {
		t.Errorf("thresholds: kid %d, legend %d", Threshold(5), Threshold(40))
	}
}

func TestEatingAndHatching(t *testing.T) {
	r := newRig(t, "")
	if p := r.view(); p.Stage != Egg || p.Hearts != 4 || p.Mood != Fed || p.Name != "" || p.NextAt != FirstLevel {
		t.Fatalf("a new agent's pet: %+v", p)
	}
	r.eat(30_000)
	if p := r.view(); p.Ate != 30_000 || p.Level != 0 || p.Name != "" {
		t.Errorf("cracking egg: %+v", p)
	}
	r.at = r.at.Add(time.Minute)
	r.eat(25_000)
	p := r.view()
	if p.Level != 1 || p.Stage != Baby || p.Name == "" || !p.HatchedAt.Equal(r.at) {
		t.Fatalf("hatched: %+v", p)
	}
	name := p.Name
	r.eat(5_000_000)
	if p := r.view(); p.Name != name || p.Stage != Strong || p.Ate != 5_055_000 {
		t.Errorf("grown: %+v", p)
	}
}

// Only input and output feed it, counted once: a total that drops (a
// transcript read again) feeds nothing until it passes the old mark, and a
// new session counts from zero.
func TestFoodCountsOnce(t *testing.T) {
	r := newRig(t, "")
	r.eat(100_000)
	r.tok = 40_000 // read again after a restart, half way
	r.feed()
	r.tok = 100_000
	r.feed()
	if p := r.view(); p.Ate != 100_000 {
		t.Errorf("re-read transcript fed it again: ate %d", p.Ate)
	}
	r.sess, r.tok = "s2", 0
	r.feed()
	r.eat(10_000)
	if p := r.view(); p.Ate != 110_000 {
		t.Errorf("new session: ate %d, want 110000", p.Ate)
	}
}

func TestHungerAndWaking(t *testing.T) {
	r := newRig(t, "")
	r.eat(60_000)
	r.wait(11 * time.Hour)
	if p := r.view(); p.Hearts != 4 || p.Mood != Fed {
		t.Errorf("11 h: %+v", p)
	}
	r.wait(time.Hour)
	if p := r.view(); p.Hearts != 3 || p.Mood != Fed {
		t.Errorf("12 h: %+v", p)
	}
	r.wait(12 * time.Hour)
	if p := r.view(); p.Hearts != 2 || p.Mood != Hungry {
		t.Errorf("a day: %+v", p)
	}
	r.wait(48 * time.Hour)
	if p := r.view(); p.Hearts != 0 || p.Mood != Weak {
		t.Errorf("three days: %+v", p)
	}
	r.wait(4 * 24 * time.Hour)
	p := r.view()
	if p.Mood != Asleep || p.Level != 1 {
		t.Errorf("a week: %+v", p)
	}
	// Views between looks count the time too, without a Feed.
	r.at = r.at.Add(time.Hour)
	if p := r.view(); p.Mood != Asleep {
		t.Errorf("view: %+v", p)
	}
	// Any food wakes it to two hearts; every 20k brings one more.
	r.eat(1)
	if p := r.view(); p.Hearts != 2 || p.Mood != Hungry || p.Level != 1 {
		t.Errorf("woken: %+v", p)
	}
	r.eat(39_999)
	if p := r.view(); p.Hearts != 4 || p.Mood != Fed {
		t.Errorf("fed again: %+v", p)
	}
}

// The clock stops while the agent works or waits on you.
func TestClockStopsWhileBusy(t *testing.T) {
	r := newRig(t, "")
	r.eat(60_000)
	r.st = model.Waiting
	r.wait(0)
	r.wait(5 * 24 * time.Hour)
	if p := r.view(); p.Hearts != 4 {
		t.Errorf("waiting on you for 5 days: %+v", p)
	}
	r.st = model.Running
	r.wait(0)
	r.wait(24 * time.Hour)
	r.st = model.Idle
	r.wait(0)
	r.wait(13 * time.Hour)
	if p := r.view(); p.Hearts != 3 {
		t.Errorf("13 h idle after working: %+v", p)
	}
}

func TestOff(t *testing.T) {
	r := newRig(t, "")
	on := false
	r.b.Enabled = func() bool { return on }
	r.eat(60_000)
	if r.view() != nil {
		t.Fatal("pets are off: no pet in the API")
	}
	on = true
	r.wait(0)
	r.eat(1)
	if p := r.view(); p.Ate != 60_001 {
		t.Errorf("food eaten while off counts once pets are on: %+v", p)
	}
	// Time with pets off doesn't count.
	on = false
	r.wait(10 * 24 * time.Hour)
	on = true
	if p := r.view(); p.Mood != Fed || p.Hearts != 4 {
		t.Errorf("after 10 days off: %+v", p)
	}
}

func TestSavedAndForgotten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pets.json")
	r := newRig(t, path)
	r.eat(70_000)
	name := r.view().Name
	if err := r.b.Save(); err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t, path)
	r2.tok = 70_000
	r2.feed()
	if p := r2.view(); p.Ate != 70_000 || p.Name != name {
		t.Errorf("after a restart: %+v", p)
	}
	// Names differ between pets while there are names left.
	r2.b.Feed([]Sight{{ID: "a1", Session: "s1", Tokens: model.Tokens{Input: 70_000}}, {ID: "b2", Tokens: model.Tokens{Input: 60_000}}})
	if other := r2.b.View(model.Agent{ID: "b2"}); other.Name == "" || other.Name == name {
		t.Errorf("second pet's name %q (first %q)", other.Name, name)
	}
	// An agent gone for a month loses its pet.
	r2.at = r2.at.Add(31 * 24 * time.Hour)
	r2.b.Feed([]Sight{{ID: "b2", Tokens: model.Tokens{Input: 60_000}}})
	if p := r2.b.View(model.Agent{ID: "a1"}); p.Ate != 0 {
		t.Errorf("forgotten pet: %+v", p)
	}
}

func TestSwitch(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	on := Switch(cfg)
	if on() {
		t.Error("no config: off")
	}
	os.WriteFile(cfg, []byte(`{"term":"tmux"}`), 0o600)
	if on() {
		t.Error("not set: off")
	}
	os.WriteFile(cfg, []byte(`{"term":"tmux","pets":true}`), 0o600)
	if !on() {
		t.Error("pets: true is on")
	}
}

// Screens hear of a meal and of a heart lost, not of every look.
func TestFeedReportsChanges(t *testing.T) {
	r := newRig(t, "")
	r.feed()
	if r.feed() {
		t.Error("nothing new")
	}
	r.tok = 10
	if !r.feed() {
		t.Error("a meal")
	}
	r.at = r.at.Add(12 * time.Hour)
	if !r.feed() {
		t.Error("a heart lost")
	}
	r.at = r.at.Add(time.Hour)
	if r.feed() {
		t.Error("an hour more, same hearts")
	}
}
