package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Time worked today is the time spent running since midnight: stretches
// that ended are added up, the one going on counts up to now, and a new day
// starts again from nothing.
func TestWorkedToday(t *testing.T) {
	day := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	e := &Entry{}
	set := func(s model.Status, now time.Time) { // SetStatus with a clock
		e.endStretch(now)
		e.A.Status, e.A.StatusSince = s, now
	}

	// Running since yesterday evening: only today's part counts.
	e.A.Status, e.A.StatusSince = model.Running, day.Add(-2*time.Hour)
	if got := e.WorkedToday(at(0, 30)); got != 30*time.Minute {
		t.Fatalf("over midnight: %v", got)
	}
	set(model.Idle, at(1, 0))
	if got := e.WorkedToday(at(5, 0)); got != time.Hour {
		t.Fatalf("idle since 1:00: %v", got)
	}
	set(model.Running, at(9, 0))
	set(model.Waiting, at(9, 20)) // waiting on you isn't work
	set(model.Running, at(9, 50))
	if got := e.WorkedToday(at(10, 0)); got != time.Hour+30*time.Minute {
		t.Fatalf("running again: %v", got)
	}
	set(model.Exited, at(10, 10))
	if got := e.WorkedToday(at(23, 0)); got != time.Hour+40*time.Minute {
		t.Fatalf("ended: %v", got)
	}
	if got := e.WorkedToday(at(24+8, 0)); got != 0 {
		t.Fatalf("the next day: %v", got)
	}
	set(model.Running, at(24+8, 0))
	set(model.Idle, at(24+8, 5))
	if got := e.WorkedToday(at(24+9, 0)); got != 5*time.Minute || e.WorkDay != "2026-11-03" {
		t.Fatalf("the next day's work: %v %s", got, e.WorkDay)
	}
}

// The store answers by agent, keeps what removed agents worked today by
// their folder, and keeps the day's total across a restart.
func TestStoreToday(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	s := New(path, "test", "0")
	s.Add(&Entry{A: model.Agent{ID: "a1", Cwd: "/srv/harbor"}})
	s.Add(&Entry{A: model.Agent{ID: "a2", Cwd: "/srv/kestrel"}})
	s.Add(&Entry{A: model.Agent{ID: "a3", Cwd: "/srv/kestrel"}})
	now := time.Now()
	since := now.Add(-time.Minute)
	if since.Before(midnight(now)) {
		t.Skip("too close to midnight")
	}
	for _, id := range []string{"a1", "a2"} {
		s.Update(id, func(e *Entry) bool {
			e.A.Status, e.A.StatusSince = model.Running, since
			e.endStretch(since) // starts the day
			return true
		})
	}
	s.Update("a2", func(e *Entry) bool { e.SetStatus(model.Idle); return true })
	s.Flush()
	s.now = func() time.Time { return now }
	s.Remove("a1")

	agents, gone := s.Today(now)
	if agents["a1"] != 0 || agents["a2"] < 59*time.Second || agents["a3"] != 0 {
		t.Errorf("agents: %v", agents)
	}
	if gone["/srv/harbor"] != time.Minute || len(gone) != 1 {
		t.Errorf("gone: %v", gone)
	}
	if _, gone := s.Today(now.Add(24 * time.Hour)); len(gone) != 0 {
		t.Errorf("gone the next day: %v", gone)
	}

	again := New(path, "test", "0")
	again.Load()
	if agents, _ := again.Today(now); agents["a2"] != s.entries["a2"].Worked {
		t.Errorf("after a restart: %v", agents)
	}
}
