package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// fakeClock is the grid's clock in tests (the store's own timer may read it).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) step(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testStore(path string) (*Store, *fakeClock) {
	c := &fakeClock{t: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)}
	s := New(path, "test", "0")
	s.now = c.now
	s.randn = func(n int) int { return n - 1 } // the highest free spot: tells "random" from "lowest"
	return s, c
}

func add(s *Store, id string, st model.Status) {
	s.Add(&Entry{A: model.Agent{ID: id, Status: st}})
}

func setStatus(s *Store, id string, st model.Status) {
	s.Update(id, func(e *Entry) bool { e.SetStatus(st); return true })
}

// spots: agent id -> spot, for every agent with one.
func spots(s *Store) map[string]int {
	out := map[string]int{}
	for _, a := range s.Snapshot().Agents {
		if a.AttentionSlot != 0 {
			out[a.ID] = a.AttentionSlot
		}
	}
	return out
}

func spotOf(s *Store, id string) int {
	e, _ := s.Get(id)
	return e.A.AttentionSlot
}

func TestWantsYou(t *testing.T) {
	for _, c := range []struct {
		a    model.Agent
		want bool
	}{
		{model.Agent{Status: model.Starting}, true}, {model.Agent{Status: model.Running}, true},
		{model.Agent{Status: model.Waiting}, true}, {model.Agent{Status: model.Idle, Unseen: true}, true},
		{model.Agent{Status: model.Idle}, false}, {model.Agent{Status: model.Error}, false},
		{model.Agent{Status: model.Exited}, false}, {model.Agent{Status: model.Exited, Unseen: true}, false},
	} {
		if got := WantsYou(c.a); got != c.want {
			t.Errorf("%s unseen=%v: %v, want %v", c.a.Status, c.a.Unseen, got, c.want)
		}
	}
}

func TestAttentionNewcomersTakeLowestFreeSpot(t *testing.T) {
	s, _ := testStore("")
	add(s, "a", model.Running)
	add(s, "b", model.Idle)
	add(s, "c", model.Waiting)
	add(s, "d", model.Idle)
	s.Update("d", func(e *Entry) bool { e.A.Unseen = true; return true }) // hungry
	if got := spots(s); fmt.Sprint(got) != "map[a:1 c:2 d:3]" {
		t.Fatalf("spots %v", got)
	}
	setStatus(s, "b", model.Running)
	if got := spotOf(s, "b"); got != 4 {
		t.Fatalf("b in %d, want 4", got)
	}
}

func TestAttentionLingerAndLeave(t *testing.T) {
	s, c := testStore("")
	add(s, "a", model.Running)
	add(s, "b", model.Running)
	add(s, "c", model.Running)
	setStatus(s, "a", model.Idle)
	if spotOf(s, "a") != 1 {
		t.Fatal("a left at once: it stays 5 s")
	}
	ch, stop := s.Subscribe()
	defer stop()
	c.step(AttentionLinger - time.Millisecond)
	s.leaveDue()
	if spotOf(s, "a") != 1 {
		t.Fatal("a left before its 5 s were up")
	}
	select {
	case <-ch:
		t.Fatal("published with nothing changed")
	default:
	}
	c.step(time.Millisecond)
	s.leaveDue()
	if got := spots(s); fmt.Sprint(got) != "map[b:2 c:3]" {
		t.Fatalf("after 5 s: %v (nobody else moves)", got)
	}
	select {
	case <-ch:
	default:
		t.Fatal("the change wasn't published")
	}

	// Back within the 5 s: it never left.
	setStatus(s, "b", model.Idle)
	c.step(3 * time.Second)
	setStatus(s, "b", model.Waiting)
	c.step(10 * time.Second)
	s.leaveDue()
	if spotOf(s, "b") != 2 {
		t.Fatal("b came back within 5 s and lost its spot")
	}
}

// The timer publishes the change by itself, with nothing else happening.
func TestAttentionLeaveTimer(t *testing.T) {
	s := New("", "test", "0")
	s.linger = 30 * time.Millisecond
	add(s, "a", model.Running)
	setStatus(s, "a", model.Idle)
	ch, stop := s.Subscribe()
	defer stop()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no change published when the linger ran out")
	}
	if spotOf(s, "a") != 0 {
		t.Fatal("a still on the grid")
	}
	s.mu.Lock()
	armed := s.attnTimer != nil
	s.mu.Unlock()
	if armed {
		t.Fatal("a timer is left running with nobody lingering")
	}
}

func TestAttentionReturnAndRandomSpot(t *testing.T) {
	s, c := testStore("")
	add(s, "a", model.Running)
	add(s, "b", model.Running)
	setStatus(s, "a", model.Idle)
	c.step(AttentionLinger)
	s.leaveDue()
	// Back while its spot is free: the old spot.
	setStatus(s, "a", model.Running)
	if spotOf(s, "a") != 1 {
		t.Fatalf("a back in %d, want its old 1", spotOf(s, "a"))
	}
	// Leaves; a newcomer takes spot 1; back: a random free spot (the test's
	// random picks the highest), which becomes its old spot.
	setStatus(s, "a", model.Idle)
	c.step(AttentionLinger)
	s.leaveDue()
	add(s, "n", model.Running)
	if spotOf(s, "n") != 1 {
		t.Fatalf("newcomer in %d, want 1", spotOf(s, "n"))
	}
	setStatus(s, "a", model.Running)
	if spotOf(s, "a") != 16 {
		t.Fatalf("a back in %d, want a random free spot (16 here)", spotOf(s, "a"))
	}
	if e, _ := s.Get("a"); e.AttnHome != 16 {
		t.Fatalf("remembered %d, want 16", e.AttnHome)
	}
}

// A returning agent and a newcomer in the same change: the returning one
// gets its old spot even when it is the lowest free one.
func TestAttentionReturningBeforeNewcomer(t *testing.T) {
	s, _ := testStore("")
	add(s, "x", model.Idle)
	add(s, "a", model.Idle)
	s.Update("a", func(e *Entry) bool { e.AttnHome = 1; return false })
	s.Each(func(e *Entry) bool { e.SetStatus(model.Running); return true })
	if got := spots(s); fmt.Sprint(got) != "map[a:1 x:2]" {
		t.Fatalf("spots %v", got)
	}
}

func TestAttentionMoreThanSixteen(t *testing.T) {
	s, c := testStore("")
	for i := range 16 {
		add(s, fmt.Sprintf("a%02d", i), model.Running)
	}
	c.step(time.Second)
	add(s, "late1", model.Idle)
	add(s, "late2", model.Idle)
	// Both start needing you at the same moment: the lower slot goes first.
	s.Each(func(e *Entry) bool {
		if e.A.Status == model.Idle {
			e.SetStatus(model.Waiting)
			return true
		}
		return false
	})
	c.step(time.Second)
	add(s, "late3", model.Running)
	if got := len(spots(s)); got != 16 {
		t.Fatalf("%d agents have spots, want 16", got)
	}
	for _, id := range []string{"late1", "late2", "late3"} {
		if spotOf(s, id) != 0 {
			t.Fatalf("%s has a spot on a full grid", id)
		}
	}
	setStatus(s, "a05", model.Idle)
	setStatus(s, "a09", model.Idle)
	c.step(AttentionLinger)
	s.leaveDue()
	if spotOf(s, "late1") != 6 || spotOf(s, "late2") != 10 || spotOf(s, "late3") != 0 {
		t.Fatalf("the ones waiting longest go first: late1 %d late2 %d late3 %d",
			spotOf(s, "late1"), spotOf(s, "late2"), spotOf(s, "late3"))
	}
}

// The old spots survive a restart; the 5 s stay doesn't need to.
func TestAttentionRestartKeepsOldSpots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	s, _ := testStore(path)
	add(s, "a", model.Running)
	add(s, "b", model.Running)
	add(s, "c", model.Running)
	if err := s.PlaceAttention("c", 9); err != nil {
		t.Fatal(err)
	}
	setStatus(s, "b", model.Exited) // away, but remembers spot 2
	s.Flush()

	r, _ := testStore(path)
	r.Load()
	if got := spots(r); fmt.Sprint(got) != "map[a:1 c:9]" {
		t.Fatalf("after a restart: %v", got)
	}
	setStatus(r, "b", model.Running)
	if spotOf(r, "b") != 2 {
		t.Fatalf("b back in %d, want its old 2", spotOf(r, "b"))
	}
}

// Every way the store changes passes through the grid.
func TestAttentionEveryChangePath(t *testing.T) {
	s, _ := testStore("")
	add(s, "a", model.Running) // Add
	if spotOf(s, "a") != 1 {
		t.Fatal("Add")
	}
	add(s, "b", model.Idle)
	s.Update("b", func(e *Entry) bool { e.A.Unseen = true; return true }) // Update
	if spotOf(s, "b") != 2 {
		t.Fatal("Update")
	}
	add(s, "c", model.Idle)
	s.Each(func(e *Entry) bool { // Each
		if e.A.ID == "c" {
			e.SetStatus(model.Starting)
			return true
		}
		return false
	})
	if spotOf(s, "c") != 3 {
		t.Fatal("Each")
	}
	s.Remove("a") // Remove frees the spot at once
	if spotOf(s, "b") != 2 || len(spots(s)) != 2 {
		t.Fatal("Remove")
	}
	add(s, "d", model.Running)
	if spotOf(s, "d") != 1 {
		t.Fatal("Remove left spot 1 taken")
	}
	// Place, Reorder and Touch change no spot.
	s.Place("d", 40)
	s.Reorder([]string{"c", "b"})
	s.Touch()
	if got := spots(s); fmt.Sprint(got) != "map[b:2 c:3 d:1]" {
		t.Fatalf("deck moves changed spots: %v", got)
	}
}

func TestPlaceAttention(t *testing.T) {
	s, _ := testStore("")
	add(s, "a", model.Running)
	add(s, "b", model.Running)
	add(s, "idle", model.Idle)
	if err := s.PlaceAttention("a", 7); err != nil || spotOf(s, "a") != 7 {
		t.Fatalf("to a free spot: %v, %d", err, spotOf(s, "a"))
	}
	if err := s.PlaceAttention("a", 2); err != nil || spotOf(s, "a") != 2 || spotOf(s, "b") != 7 {
		t.Fatalf("swap: %v, a %d b %d", err, spotOf(s, "a"), spotOf(s, "b"))
	}
	if e, _ := s.Get("b"); e.AttnHome != 7 {
		t.Fatalf("b remembers %d, want 7", e.AttnHome)
	}
	for _, c := range []struct {
		id   string
		spot int
		want error
	}{{"zz", 3, ErrNoAgent}, {"a", 0, ErrBadSpot}, {"a", 17, ErrBadSpot}, {"idle", 3, ErrNotNeedsYou}} {
		if err := s.PlaceAttention(c.id, c.spot); !errors.Is(err, c.want) {
			t.Errorf("%s to %d: %v, want %v", c.id, c.spot, err, c.want)
		}
	}
}
