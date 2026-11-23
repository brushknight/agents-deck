package cpuguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A weekly schedule for the limit: blocks of the week, each with its own
// limit, on the computer's local clock. Outside every block the plain Limit
// applies. It repeats every week until changed.

// Days are the names a block's days take, Monday first.
var Days = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// MaxBlocks is how many blocks a schedule may have.
const MaxBlocks = 50

// Where the limit in use comes from (State.LimitFrom).
const (
	FromLimit    = "limit"    // the plain limit
	FromSchedule = "schedule" // a block of the schedule
	FromOverride = "override" // a limit picked by hand, for a while
)

const (
	day  = 24 * 60 // minutes
	week = 7 * day
)

// Schedule is Setting.Schedule.
type Schedule struct {
	On     bool    `json:"on"`
	Blocks []Block `json:"blocks"`
}

// Block is part of the week with its own limit: on each of Days, From to
// To ("HH:MM", half hours; To may be "24:00"). A To before From runs past
// midnight into the next day.
type Block struct {
	Days  []string `json:"days"`
	From  string   `json:"from"`
	To    string   `json:"to"`
	Limit int      `json:"limit"`
}

// Override is a limit picked by hand, in use until Until; then the
// schedule again.
type Override struct {
	Limit int       `json:"limit"`
	Until time.Time `json:"until"`
	By    string    `json:"by,omitempty"` // who picked it: "Settings", "agentctl"
	At    time.Time `json:"at"`           // when
}

// CheckLimit says what is wrong with a limit: 60 to 95, in fives.
func CheckLimit(n int) error {
	if n < 60 || n > 95 || n%5 != 0 {
		return errors.New("must be 60 to 95, in fives")
	}
	return nil
}

// hm reads "HH:MM" on the half hour into minutes of the day; "24:00" only
// when end.
func hm(s string, end bool) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	switch {
	case !ok || len(h) != 2 || len(m) != 2 || e1 != nil || e2 != nil:
		return 0, fmt.Errorf("%q isn't a time like 09:00", s)
	case mm != 0 && mm != 30:
		return 0, fmt.Errorf("%s: times go in half hours (:00 or :30)", s)
	case hh == 24 && mm == 0 && end:
		return day, nil
	case hh < 0 || hh > 23:
		return 0, fmt.Errorf("%q isn't a time like 09:00", s)
	}
	return hh*60 + mm, nil
}

// span is one day's part of a block, in minutes of the week from Monday
// 00:00; end may pass the week's end (Sunday night into Monday).
type span struct{ start, end, block int }

func (b Block) times() (from, to int) {
	from, _ = hm(b.From, false)
	to, _ = hm(b.To, true)
	return from, to
}

// spans of a block that passed check.
func (b Block) spans(i int) []span {
	from, to := b.times()
	if to <= from {
		to += day
	}
	var out []span
	for _, d := range b.Days {
		n := dayIndex(d)
		out = append(out, span{n*day + from, n*day + to, i})
	}
	return out
}

func dayIndex(d string) int {
	for i, n := range Days {
		if n == d {
			return i
		}
	}
	return -1
}

func (b Block) check() error {
	if len(b.Days) == 0 {
		return errors.New("pick at least one day")
	}
	seen := map[string]bool{}
	for _, d := range b.Days {
		if dayIndex(d) < 0 {
			return fmt.Errorf("%q isn't a day (mon to sun)", d)
		}
		if seen[d] {
			return fmt.Errorf("%s is there twice", d)
		}
		seen[d] = true
	}
	from, err := hm(b.From, false)
	if err != nil {
		return fmt.Errorf("from: %v", err)
	}
	to, err := hm(b.To, true)
	if err != nil {
		return fmt.Errorf("to: %v", err)
	}
	if from == to {
		return errors.New("from and to are the same time")
	}
	if err := CheckLimit(b.Limit); err != nil {
		return fmt.Errorf("the limit %v", err)
	}
	return nil
}

// Label is the block in words: "Weekdays 09:00-18:00".
func (b Block) Label() string {
	return daysLabel(b.Days) + " " + b.From + "-" + b.To
}

// DaysLabel is the block's days in words: "Weekdays", "Mon, Wed".
func (b Block) DaysLabel() string { return daysLabel(b.Days) }

// Start is where the block first starts in the week, in minutes from
// Monday 00:00, to list blocks in the order of the week.
func (b Block) Start() int {
	first := week
	for _, sp := range b.spans(0) {
		first = min(first, sp.start)
	}
	return first
}

func daysLabel(days []string) string {
	on := map[string]bool{}
	for _, d := range days {
		on[d] = true
	}
	var names []string
	for _, d := range Days {
		if on[d] {
			names = append(names, strings.ToUpper(d[:1])+d[1:])
		}
	}
	switch strings.Join(names, " ") {
	case "Mon Tue Wed Thu Fri Sat Sun":
		return "Every day"
	case "Mon Tue Wed Thu Fri":
		return "Weekdays"
	case "Sat Sun":
		return "Weekends"
	}
	return strings.Join(names, ", ")
}

// Check says what is wrong with a schedule: each block, and no two blocks
// covering the same time (the message names both).
func (s Schedule) Check() error {
	if len(s.Blocks) > MaxBlocks {
		return fmt.Errorf("cpuGuard.schedule: at most %d blocks", MaxBlocks)
	}
	var all []span
	for i, b := range s.Blocks {
		if err := b.check(); err != nil {
			return fmt.Errorf("block %d (%s): %v", i+1, b.Label(), err)
		}
		all = append(all, b.spans(i)...)
	}
	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.block != b.block && overlap(a, b) {
				x, y := min(a.block, b.block), max(a.block, b.block)
				return fmt.Errorf("block %d (%s) overlaps block %d (%s)", y+1, s.Blocks[y].Label(), x+1, s.Blocks[x].Label())
			}
		}
	}
	return nil
}

func overlap(a, b span) bool {
	hit := func(a, b span) bool { return a.start < b.end && b.start < a.end }
	w := func(s span) span { return span{s.start + week, s.end + week, s.block} }
	return hit(a, b) || hit(w(a), b) || hit(a, w(b))
}

// minuteOfWeek is t's wall clock in minutes from Monday 00:00.
func minuteOfWeek(t time.Time) int {
	return (int(t.Weekday())+6)%7*day + t.Hour()*60 + t.Minute()
}

// at is the block covering a minute of the week, and its span; -1 for none.
func (s Schedule) at(mow int) (int, span) {
	for i, b := range s.Blocks {
		for _, sp := range b.spans(i) {
			if (mow >= sp.start && mow < sp.end) || (mow+week >= sp.start && mow+week < sp.end) {
				return i, sp
			}
		}
	}
	return -1, span{}
}

// Effective is the limit in use at a moment and where it comes from.
type Effective struct {
	Limit int
	From  string    // FromLimit, FromSchedule or FromOverride
	Block string    // the block, for FromSchedule: "Mon 09:00-18:00"
	Until time.Time // when it next changes; zero: not by itself
	Next  int       // the limit then
	By    string    // FromOverride: who picked it
	At    time.Time // FromOverride: when
}

// scheduled is the limit the schedule gives at t (the plain limit outside
// its blocks or with it off), the block's index or -1, and its span.
func (s Setting) scheduled(t time.Time) (int, int, span) {
	if !s.Schedule.On {
		return s.Limit, -1, span{}
	}
	i, sp := s.Schedule.at(minuteOfWeek(t))
	if i < 0 {
		return s.Limit, -1, sp
	}
	return s.Schedule.Blocks[i].Limit, i, sp
}

// NextChange is when the schedule's limit next changes after t, on t's
// wall clock (so a block at 09:00 is at 09:00 across a clock change); zero
// when it never does (no schedule, or one limit all week).
func (s Setting) NextChange(t time.Time) time.Time {
	if !s.Schedule.On {
		return time.Time{}
	}
	mow := minuteOfWeek(t)
	cur, _, _ := s.scheduled(t)
	var ahead []int
	for i, b := range s.Schedule.Blocks {
		for _, sp := range b.spans(i) {
			for _, edge := range []int{sp.start, sp.end} {
				if d := ((edge-mow)%week + week) % week; d > 0 {
					ahead = append(ahead, d)
				}
			}
		}
	}
	sort.Ints(ahead)
	now := t.Hour()*60 + t.Minute()
	for _, d := range ahead {
		limit := s.Limit
		if i, _ := s.Schedule.at((mow + d) % week); i >= 0 {
			limit = s.Schedule.Blocks[i].Limit
		}
		if limit != cur {
			// on the wall clock: time.Date carries the minutes over the days
			return time.Date(t.Year(), t.Month(), t.Day(), 0, now+d, 0, 0, t.Location())
		}
	}
	return time.Time{}
}

// EffectiveAt is the limit in use at t: an override not yet over, else the
// block of the schedule covering t, else the plain limit.
func (s Setting) EffectiveAt(t time.Time) Effective {
	if o := s.Override; o != nil && t.Before(o.Until) {
		next, _, _ := s.scheduled(o.Until.In(t.Location()))
		return Effective{Limit: o.Limit, From: FromOverride, Until: o.Until.In(t.Location()), Next: next, By: o.By, At: o.At}
	}
	limit, i, sp := s.scheduled(t)
	e := Effective{Limit: limit, From: FromLimit}
	if i >= 0 {
		d := Days[sp.start/day%7]
		b := s.Schedule.Blocks[i]
		e.From, e.Block = FromSchedule, strings.ToUpper(d[:1])+d[1:]+" "+b.From+"-"+b.To
	}
	if u := s.NextChange(t); !u.IsZero() {
		e.Until = u
		e.Next, _, _ = s.scheduled(u)
	}
	return e
}

// OverrideUntil is when a limit picked by hand at t ends by itself: the
// schedule's next change, or a week on when it has none.
func (s Setting) OverrideUntil(t time.Time) time.Time {
	if u := s.NextChange(t); !u.IsZero() {
		return u
	}
	return t.Add(7 * 24 * time.Hour)
}

// MaxOverride is how far ahead an override may end.
const MaxOverride = 7 * 24 * time.Hour

// Patch applies PATCH /admin/v1/settings' "cpuGuard" (only the keys given)
// to s, at now:
//   - "override": {"limit", "until", "by"} is a limit picked by hand; with
//     no "until" it lasts until the schedule's next change, and with the
//     schedule off it is the plain limit instead. null is Back to schedule.
//   - "limit" alone while the schedule is on is a limit picked by hand too
//     (by Settings); with "schedule" in the same patch it is the plain limit
//     for the rest of the week.
//   - turning the schedule off ends an override.
func Patch(s Setting, raw json.RawMessage, now time.Time) (Setting, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return s, fmt.Errorf("cpuGuard: %v", err)
	}
	old := s
	// Unmarshal fills what is there: give it copies, not old's.
	s.Schedule.Blocks = slices.Clone(s.Schedule.Blocks)
	if _, ok := keys["override"]; ok || s.Override == nil {
		s.Override = nil
	} else {
		o := *s.Override
		s.Override = &o
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return old, fmt.Errorf("cpuGuard: %v", err)
	}
	if s.Schedule.Blocks == nil {
		s.Schedule.Blocks = []Block{}
	}
	_, limit := keys["limit"]
	_, sched := keys["schedule"]
	_, over := keys["override"]
	switch {
	case s.Override != nil:
		o := s.Override
		if err := CheckLimit(o.Limit); err != nil {
			return old, fmt.Errorf("cpuGuard.override.limit %v", err)
		}
		if o.By == "" {
			o.By = "Settings"
		}
		o.At = now
		switch {
		case o.Until.IsZero() && !s.Schedule.On:
			s.Limit, s.Override = o.Limit, nil
		case o.Until.IsZero():
			o.Until = s.OverrideUntil(now)
		case !o.Until.After(now):
			return old, errors.New("cpuGuard.override.until is already past")
		case o.Until.After(now.Add(MaxOverride)):
			return old, errors.New("cpuGuard.override.until: at most a week ahead")
		}
	case limit && !sched && !over && s.Schedule.On:
		if err := CheckLimit(s.Limit); err != nil {
			return old, fmt.Errorf("cpuGuard.limit %v", err)
		}
		s.Override = &Override{Limit: s.Limit, Until: s.OverrideUntil(now), By: "Settings", At: now}
		s.Limit = old.Limit
	}
	if !s.Schedule.On && old.Schedule.On {
		s.Override = nil
	}
	if s.Override != nil && !now.Before(s.Override.Until) {
		s.Override = nil
	}
	if err := s.Check(); err != nil {
		return old, err
	}
	return s, nil
}

// Line is the limit in words, for the log: "60% from the schedule (Mon
// 09:00-18:00)".
func (e Effective) Line() string {
	switch e.From {
	case FromSchedule:
		return fmt.Sprintf("%d%% from the schedule (%s)", e.Limit, e.Block)
	case FromOverride:
		by := ""
		if e.By != "" {
			by = " by " + e.By
		}
		return fmt.Sprintf("%d%% set%s until %s", e.Limit, by, e.Until.Format("Mon 15:04"))
	}
	return fmt.Sprintf("%d%%, your limit outside the schedule", e.Limit)
}
