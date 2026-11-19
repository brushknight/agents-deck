package store

import (
	"errors"
	"sort"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The "Needs you" grid: one grid of AttentionSpots spots (the 4 x 4 page of
// the largest device), worked out here so every screen shows the same agents
// in the same spots. Each agent carries its spot in Agent.AttentionSlot
// (1..16, 0 = not on the grid), the way it carries its deck position in Slot.
//
//   - An agent is on the grid while it needs you (WantsYou), and for
//     AttentionLinger after it stops. Back within that time and it never left.
//   - Spots are sticky: nobody moves when another leaves. A newcomer takes the
//     lowest free spot. An agent that was here before gets its old spot back
//     (Entry.AttnHome, kept while it is away and across restarts) when that
//     is free, else a random free spot, which becomes its old spot.
//   - With all 16 spots taken, the rest wait (AttentionSlot 0) and get spots
//     as they free up: the one that came on the grid first, then the lowest
//     deck slot.
//
// arrange runs on every change (Store.changed), so every source of status
// (hooks, the daemon's polls, the watchers, the demo) passes through it, and
// a timer publishes the change when a lingering agent's time runs out.

// AttentionSpots is the size of the "Needs you" grid: 4 x 4, numbered 1..16
// by rows.
const AttentionSpots = 16

// AttentionLinger is how long an agent stays on the "Needs you" grid after
// it stops needing you.
const AttentionLinger = 5 * time.Second

// WantsYou is whether an agent belongs on the "Needs you" grid: it works
// (starting, running), asks you something (waiting), or finished a turn you
// haven't looked at yet (idle and unseen: hungry).
func WantsYou(a model.Agent) bool {
	switch a.Status {
	case model.Starting, model.Running, model.Waiting:
		return true
	case model.Idle:
		return a.Unseen
	}
	return false
}

// arrange works the grid out again, under s.mu. It reports whether any
// agent's spot changed, and sets the timer for the next lingering agent.
func (s *Store) arrange() bool {
	now := s.now()
	list := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { // deck order
		if list[i].A.Slot != list[j].A.Slot {
			return list[i].A.Slot < list[j].A.Slot
		}
		return list[i].A.ID < list[j].A.ID
	})
	before := make(map[*Entry]int, len(list))
	for _, e := range list {
		before[e] = e.A.AttentionSlot
	}

	var wake time.Time
	var members []*Entry
	for _, e := range list {
		switch {
		case WantsYou(e.A):
			e.attnLeaveAt = time.Time{}
		case e.attnOn:
			if e.attnLeaveAt.IsZero() {
				e.attnLeaveAt = now.Add(s.linger)
			}
			if !now.Before(e.attnLeaveAt) {
				e.attnOn, e.attnLeaveAt, e.A.AttentionSlot = false, time.Time{}, 0
				continue
			}
			if wake.IsZero() || e.attnLeaveAt.Before(wake) {
				wake = e.attnLeaveAt
			}
		default:
			e.A.AttentionSlot = 0
			continue
		}
		if !e.attnOn {
			e.attnOn, e.attnJoined = true, now
		}
		members = append(members, e)
	}

	// Spots that are still valid keep their agent; the others are placed again.
	var taken [AttentionSpots + 1]bool
	var waiting []*Entry
	for _, e := range members {
		if sp := e.A.AttentionSlot; sp >= 1 && sp <= AttentionSpots && !taken[sp] {
			taken[sp] = true
			continue
		}
		e.A.AttentionSlot = 0
		waiting = append(waiting, e)
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].attnJoined.Before(waiting[j].attnJoined) })
	free := func() []int {
		var f []int
		for sp := 1; sp <= AttentionSpots; sp++ {
			if !taken[sp] {
				f = append(f, sp)
			}
		}
		return f
	}
	if n := len(free()); len(waiting) > n {
		waiting = waiting[:n] // oldest first; the rest wait for a spot
	}
	put := func(e *Entry, sp int) {
		taken[sp] = true
		e.A.AttentionSlot, e.AttnHome = sp, sp
	}
	// Old spots first, so a newcomer doesn't take the spot a returning agent had.
	var rest []*Entry
	for _, e := range waiting {
		if h := e.AttnHome; h >= 1 && h <= AttentionSpots && !taken[h] {
			put(e, h)
		} else {
			rest = append(rest, e)
		}
	}
	for _, e := range rest {
		f := free()
		if e.AttnHome == 0 {
			put(e, f[0])
		} else {
			put(e, f[s.randn(len(f))])
		}
	}

	s.armLeave(now, wake)
	for _, e := range list {
		if e.A.AttentionSlot != before[e] {
			return true
		}
	}
	return false
}

// armLeave sets one timer for the next lingering agent to leave the grid:
// nothing else would publish that change.
func (s *Store) armLeave(now, wake time.Time) {
	if s.attnTimer != nil {
		s.attnTimer.Stop()
		s.attnTimer = nil
	}
	if wake.IsZero() {
		return
	}
	d := wake.Sub(now)
	if d < 0 {
		d = 0
	}
	s.attnTimer = time.AfterFunc(d, s.leaveDue)
}

// leaveDue runs when a lingering agent's time is up: it publishes only when
// the grid changed.
func (s *Store) leaveDue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.arrange() {
		s.notify()
	}
}

// Errors of PlaceAttention.
var (
	ErrNoAgent     = errors.New("no such agent")
	ErrBadSpot     = errors.New("spot must be 1..16")
	ErrNotNeedsYou = errors.New("the agent isn't on the Needs you screen")
)

// PlaceAttention moves an agent on the "Needs you" grid to spot 1..16, like
// Place does on the deck: an agent already there swaps into the mover's old
// spot (or waits for one, when the mover had none). The spot becomes the
// mover's old spot too. Only an agent on the grid can move.
func (s *Store) PlaceAttention(id string, spot int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	switch {
	case !ok:
		return ErrNoAgent
	case spot < 1 || spot > AttentionSpots:
		return ErrBadSpot
	case !e.attnOn:
		return ErrNotNeedsYou
	}
	if e.A.AttentionSlot == spot {
		return nil
	}
	for _, x := range s.entries {
		if x != e && x.A.AttentionSlot == spot {
			x.A.AttentionSlot = e.A.AttentionSlot
			if x.A.AttentionSlot != 0 {
				x.AttnHome = x.A.AttentionSlot
			}
		}
	}
	e.A.AttentionSlot, e.AttnHome = spot, spot
	s.changed()
	return nil
}
