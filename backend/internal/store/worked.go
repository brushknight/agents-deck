package store

import (
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// How long each agent worked today: the time it spent running (at work on a
// turn, or its subagents were), since midnight on this computer's clock. It
// comes from the status changes the deck sees anyway (SetStatus), so it
// reads nothing: a stretch of running is added when it ends, and the one
// going on now is added when asked. Kept with the agent, so a restart keeps
// it; agents removed today leave theirs behind, by working folder, until the
// service stops.

// midnight is the start of t's day, in t's time zone.
func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func dayOf(t time.Time) string { return t.Format(time.DateOnly) }

// running is how long the current stretch of running lasted today, up to now.
func (e *Entry) running(now time.Time) time.Duration {
	if e.A.Status != model.Running {
		return 0
	}
	from := e.A.StatusSince
	if m := midnight(now); from.Before(m) {
		from = m
	}
	if d := now.Sub(from); d > 0 {
		return d
	}
	return 0
}

// endStretch adds the stretch of running that ends now (the status changes)
// to the day's total.
func (e *Entry) endStretch(now time.Time) {
	d := e.running(now)
	if day := dayOf(now); e.WorkDay != day {
		e.WorkDay, e.Worked = day, 0
	}
	e.Worked += d
}

// WorkedToday is how long the agent has been running today, up to now.
func (e *Entry) WorkedToday(now time.Time) time.Duration {
	d := e.running(now)
	if e.WorkDay == dayOf(now) {
		d += e.Worked
	}
	return d
}

// goneWork is the time worked today by agents removed today, by working
// folder.
type goneWork struct {
	day string
	m   map[string]time.Duration
}

func (g *goneWork) add(now time.Time, cwd string, d time.Duration) {
	if day := dayOf(now); g.day != day {
		g.day, g.m = day, map[string]time.Duration{}
	}
	if d > 0 && cwd != "" {
		g.m[cwd] += d
	}
}

// Today is how long each agent on the deck worked today (by id), and how
// long the agents removed today did (by working folder).
func (s *Store) Today(now time.Time) (agents, gone map[string]time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agents = make(map[string]time.Duration, len(s.entries))
	for id, e := range s.entries {
		if d := e.WorkedToday(now); d > 0 {
			agents[id] = d
		}
	}
	gone = map[string]time.Duration{}
	if s.gone.day == dayOf(now) {
		for cwd, d := range s.gone.m {
			gone[cwd] = d
		}
	}
	return agents, gone
}
