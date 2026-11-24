package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestMarkActive(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	e := &Entry{}
	if e.MarkActive(time.Time{}) || !e.A.LastActive.IsZero() {
		t.Fatal("a zero time must not count")
	}
	if !e.MarkActive(t0) || !e.A.LastActive.Equal(t0) {
		t.Fatal("first time")
	}
	if e.MarkActive(t0.Add(-time.Hour)) || !e.A.LastActive.Equal(t0) {
		t.Fatal("an older time must not move it back")
	}
	if e.MarkActiveStep(t0.Add(ActiveStep / 2)) {
		t.Fatal("a polled source moves it only in steps")
	}
	if !e.MarkActiveStep(t0.Add(ActiveStep)) || !e.A.LastActive.Equal(t0.Add(ActiveStep)) {
		t.Fatal("a full step counts")
	}
}

// The last-active time is saved with the agent and comes back after a
// restart (a crash keeps what the saver last wrote).
func TestLastActiveSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	at := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	s := New(path, "test", "0")
	s.Add(&Entry{A: model.Agent{ID: "abc123", Title: "notes", Tool: "shell"}})
	s.Update("abc123", func(e *Entry) bool { return e.MarkActive(at) })
	s.Flush()

	again := New(path, "test", "0")
	again.Load()
	e, ok := again.Get("abc123")
	if !ok || !e.A.LastActive.Equal(at) {
		t.Fatalf("after restart: %v %v", ok, e.A.LastActive)
	}
	if a := again.Snapshot().Agents[0]; !a.LastActive.Equal(at) {
		t.Fatalf("snapshot: %v", a.LastActive)
	}
}

// Slowed and cpu come from the CPU guard's slower, each time, and aren't
// saved with the agent.
func TestSnapshotSlow(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "agents.json"), "test", "0")
	s.Add(&Entry{A: model.Agent{ID: "a1", Title: "one", Tool: "claude"}})
	s.Add(&Entry{A: model.Agent{ID: "a2", Title: "two", Tool: "claude"}})
	s.Add(&Entry{A: model.Agent{ID: "a3", Title: "three", Tool: "claude"}})
	s.Slow = func() (map[string]string, map[string]float64) {
		return map[string]string{"a1": "guard", "a3": "you"}, map[string]float64{"a1": 41.5, "a2": 3}
	}
	got := map[string]model.Agent{}
	for _, a := range s.Snapshot().Agents {
		got[a.ID] = a
	}
	if !got["a1"].Slowed || got["a1"].SlowedByYou || got["a1"].CPU != 41.5 || got["a2"].Slowed || got["a2"].CPU != 3 ||
		!got["a3"].Slowed || !got["a3"].SlowedByYou {
		t.Errorf("snapshot %+v", got)
	}
	if e, _ := s.Get("a1"); e.A.Slowed || e.A.CPU != 0 {
		t.Errorf("stored with the agent: %+v", e.A)
	}
}
