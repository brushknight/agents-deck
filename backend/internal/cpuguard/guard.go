// Package cpuguard watches how busy the whole computer is. When the CPU
// stays over a limit for a while, the guard is busy: it warns, and holds
// what can wait (queued prompts, agents started over MCP) until the CPU is
// calm again. What you type or press always goes in at once.
package cpuguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Clearing busy: the CPU must be ClearGap points under the limit for
// ClearFor. The gap stops it flapping around the limit.
const (
	ClearGap = 10
	ClearFor = time.Minute
)

// every is how often the guard looks at the meter.
const every = 5 * time.Second

// Setting is config.json "cpuGuard".
type Setting struct {
	On    bool   `json:"on"`    // watch the CPU
	Limit int    `json:"limit"` // percent of the whole computer (100: every core), 60 to 95
	For   string `json:"for"`   // how long over the limit: "1m", "2m" or "5m"
	Hold  bool   `json:"hold"`  // while busy, hold queued prompts (starting agents is never held)
	Slow  bool   `json:"slow"`  // while busy, run the heaviest agent in background mode (macOS; Slower)
	// Schedule: other limits for parts of the week; Limit for the rest.
	Schedule Schedule `json:"schedule"`
	// Override: a limit picked by hand, until the schedule's next change
	// (or a time given). nil: none.
	Override *Override `json:"override,omitempty"`
}

// Default is the setting when config.json has none.
func Default() Setting {
	return Setting{On: true, Limit: 80, For: "2m", Hold: true, Slow: true, Schedule: Schedule{Blocks: []Block{}}}
}

// Windows are the lengths For may take.
var Windows = []string{"1m", "2m", "5m"}

// Check says what is wrong with a setting given to PATCH /admin/v1/settings.
func (s Setting) Check() error {
	if s.Limit < 60 || s.Limit > 95 {
		return errors.New("cpuGuard.limit must be 60 to 95")
	}
	if !slices.Contains(Windows, s.For) {
		return errors.New(`cpuGuard.for must be "1m", "2m" or "5m"`)
	}
	if err := s.Schedule.Check(); err != nil {
		return err
	}
	if o := s.Override; o != nil {
		if err := CheckLimit(o.Limit); err != nil {
			return fmt.Errorf("cpuGuard.override.limit %v", err)
		}
		if o.Until.IsZero() {
			return errors.New("cpuGuard.override.until: when it ends")
		}
	}
	return nil
}

// Window is For as a duration.
func (s Setting) Window() time.Duration {
	d, err := time.ParseDuration(s.For)
	if err != nil {
		return 2 * time.Minute
	}
	return d
}

// FromConfig reads config.json's "cpuGuard" value (as decoded into a map):
// keys left out keep their defaults, and a limit or window out of range
// falls back to the default one.
func FromConfig(v any) Setting {
	s := Default()
	if v != nil {
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &s)
	}
	return s.Normal()
}

// Normal puts a limit or window out of range back to the default.
func (s Setting) Normal() Setting {
	d := Default()
	if s.Limit < 60 || s.Limit > 95 {
		s.Limit = d.Limit
	}
	if err := (Setting{Limit: s.Limit, For: s.For}).Check(); err != nil {
		s.For = d.For
	}
	// A schedule edited by hand into something wrong is not followed.
	if s.Schedule.Blocks == nil {
		s.Schedule.Blocks = []Block{}
	}
	if s.Schedule.Check() != nil {
		s.Schedule.On = false
	}
	if o := s.Override; o != nil && (CheckLimit(o.Limit) != nil || o.Until.IsZero()) {
		s.Override = nil
	}
	return s
}

// Meter is the machine's CPU use (proc.MachineMeter).
type Meter interface {
	Report() proc.MachineCPU
	Average(span time.Duration) (float64, bool)
}

// State is the guard in GET /admin/v1/system ("cpu").
type State struct {
	Available bool       `json:"available"`       // the CPU can be read
	Why       string     `json:"why,omitempty"`   // when not
	Percent   float64    `json:"percent"`         // the last minute (the last 5 s until there is a minute)
	Busy      bool       `json:"busy"`            // over the limit for the window, not calm since
	Since     *time.Time `json:"since,omitempty"` // when busy: when the CPU went over the limit
	Limit     int        `json:"limit"`           // the limit in use now
	LimitFrom string     `json:"limitFrom"`       // where it comes from: "limit", "schedule" or "override"
	Block     string     `json:"block,omitempty"` // "schedule": the block, "Mon 09:00-18:00"
	Until     *time.Time `json:"until,omitempty"` // when the limit next changes by itself
	Next      int        `json:"next,omitempty"`  // the limit then
	By        string     `json:"by,omitempty"`    // "override": who picked it
	SetAt     *time.Time `json:"setAt,omitempty"` // "override": when
	Scheduled bool       `json:"scheduled"`       // the schedule is on
	For       string     `json:"for"`
	Hold      bool       `json:"hold"`           // queued prompts are held now
	Held      int        `json:"held"`           // queued prompts waiting because of the guard
	Released  bool       `json:"released"`       // Send them now: nothing is held until the CPU is calm
	Peak      *Peak      `json:"peak,omitempty"` // the busiest minute today
	Slowed    []string   `json:"slowed"`         // ids of the agents in background mode (Slower)
	ByYou     []string   `json:"slowedByYou"`    // those of them slowed with Slow it, not by the guard
}

// Peak is the busiest minute of the day so far.
type Peak struct {
	Percent float64   `json:"percent"`
	At      time.Time `json:"at"`
}

// Guard decides busy or calm from the meter's averages.
type Guard struct {
	Meter Meter
	// Queued counts the prompts in the queue, for State.Held. nil: none.
	Queued func() int
	// Top names the biggest users of the CPU for the log line on entering
	// and leaving busy. nil: the line names none.
	Top func() []proc.CPUUser
	// Changed is called when busy, or what is held, changes: the queue
	// looks again. nil: nothing is told.
	Changed func()
	// Slowed names the agents in background mode, for State. nil: none.
	Slowed func() []string
	// SlowedByYou names those slowed with Slow it, for State. nil: none.
	SlowedByYou func() []string
	// Ended is called when an override is over, to drop it from
	// config.json. nil: it stays there (and is ignored).
	Ended func(Override)
	Logf  func(format string, args ...any)
	Now   func() time.Time // the clock, local time; nil: time.Now

	mu       sync.Mutex
	set      Setting
	eff      Effective // the limit in use
	effAt    time.Time // the minute it was worked out
	effSet   bool      // it has been
	recheck  bool      // the limit changed: decide busy again at the next reading
	busy     bool
	since    time.Time
	released bool
	read     proc.MachineCPU
	percent  float64
	peak     Peak
}

// New is a guard with the default setting.
func New(m Meter) *Guard { return &Guard{Meter: m, set: Default()} }

// Set changes the setting; the limit applies at once, busy is decided again
// at the next look.
func (g *Guard) Set(s Setting) {
	g.mu.Lock()
	g.set = s.Normal()
	g.mu.Unlock()
	g.tick(true)
}

// tick works out the limit in use, every minute (or now, with force): an
// override not over yet, else the schedule's block, else the plain limit.
// One log line when it changes.
func (g *Guard) tick(force bool) {
	now := g.now()
	g.mu.Lock()
	line, ended := g.settle(now, force)
	g.mu.Unlock()
	if ended != nil {
		g.logf("cpu guard: your limit of %d%% is over (it was until %s)", ended.Limit, ended.Until.In(now.Location()).Format("Mon 15:04"))
		if g.Ended != nil {
			g.Ended(*ended)
		}
	}
	if line != "" {
		g.logf("cpu guard: limit %s", line)
		if g.Changed != nil {
			g.Changed()
		}
	}
}

// settle is tick under the lock: the log line, and the override that ended.
func (g *Guard) settle(now time.Time, force bool) (string, *Override) {
	minute := now.Truncate(time.Minute)
	if !force && g.effSet && minute.Equal(g.effAt) {
		return "", nil
	}
	g.effAt = minute
	var ended *Override
	if o := g.set.Override; o != nil && !now.Before(o.Until) {
		ended, g.set.Override = o, nil
	}
	e := g.set.EffectiveAt(now)
	old, first := g.eff, !g.effSet
	g.eff, g.effSet = e, true
	if !first && e.Limit == old.Limit && e.From == old.From && e.Block == old.Block && e.By == old.By && e.Until.Equal(old.Until) {
		return "", ended
	}
	if !first && e.Limit != old.Limit {
		g.recheck = true
	}
	if first && e.From == FromLimit {
		return "", ended
	}
	return e.Line(), ended
}

// Limit is the limit in use now and where it comes from.
func (g *Guard) Limit() Effective {
	g.tick(false)
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.eff
}

// Setting is the setting in use.
func (g *Guard) Setting() Setting {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.set
}

func (g *Guard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Guard) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

// Run looks at the meter every 5 s until ctx ends.
func (g *Guard) Run(ctx context.Context) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.Step()
		}
	}
}

// Step looks at the meter once and enters or leaves busy.
func (g *Guard) Step() {
	g.tick(false)
	now := g.now()
	r := g.Meter.Report()
	last1, ok1 := g.Meter.Average(ClearFor)
	g.mu.Lock()
	set := g.set
	limit, recheck := g.eff.Limit, g.recheck
	g.recheck = false
	g.read = r
	g.percent = r.Now
	if ok1 {
		g.percent = last1
	}
	if r.Available && ok1 {
		y, m, d := now.Date()
		py, pm, pd := g.peak.At.Date()
		if last1 > g.peak.Percent || y != py || m != pm || d != pd {
			g.peak = Peak{Percent: last1, At: now}
		}
	}
	var change string
	switch {
	case g.busy && (!set.On || !r.Available):
		// Off, or the CPU can't be read: nothing is ever held.
		g.busy, g.released = false, false
		if set.On {
			change = "can't read the CPU (" + r.Why + "), so nothing is held"
		}
	case !set.On || !r.Available:
	case !g.busy:
		if avg, ok := g.Meter.Average(set.Window()); ok && avg > float64(limit) {
			g.busy, g.released, g.since = true, false, now.Add(-set.Window())
			change = fmt.Sprintf("the %s is busy: %.0f%% of the CPU for %s, over your limit of %d%%", Machine(), avg, set.For, limit)
		}
	default:
		// The limit went up: under it for the window, the computer is calm.
		if avg, ok := g.Meter.Average(set.Window()); recheck && ok && avg <= float64(limit) {
			change = fmt.Sprintf("the %s is calm again: %.0f%% of the CPU for %s is under your limit, now %d%%, after %s busy", Machine(), avg, set.For, limit, minutes(now.Sub(g.since)))
			g.busy, g.released = false, false
		} else if ok1 && last1 < float64(limit-ClearGap) {
			change = fmt.Sprintf("the %s is calm again: %.0f%% of the CPU for a minute, after %s busy", Machine(), last1, minutes(now.Sub(g.since)))
			g.busy, g.released = false, false
		}
	}
	g.mu.Unlock()
	if change == "" {
		return
	}
	if g.Top != nil {
		var top []string
		for _, u := range g.Top() {
			line := fmt.Sprintf("%s %.0f%%", u.Name, u.Percent)
			var cmds []string
			for _, c := range u.Commands {
				cmds = append(cmds, fmt.Sprintf("%s %.0f%%", c.Name, c.Percent))
			}
			if len(cmds) > 0 {
				line += " (" + strings.Join(cmds, ", ") + ")"
			}
			top = append(top, line)
		}
		if len(top) > 0 {
			change += "; using it: " + strings.Join(top, ", ")
		}
	}
	g.logf("cpu guard: %s", change)
	if g.Changed != nil {
		g.Changed()
	}
}

// Holding says whether work that can wait is held now, and why, in words
// for whoever asked.
func (g *Guard) Holding() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.holding() {
		return false, ""
	}
	return true, fmt.Sprintf("The %s is busy (%.0f%% CPU for %s); try again in a few minutes", Machine(), g.percent, minutes(g.now().Sub(g.since)))
}

func (g *Guard) holding() bool { return g.busy && g.set.On && g.set.Hold && !g.released }

// Release is Send them now: nothing is held until the CPU is calm again.
// It reports whether anything was held.
func (g *Guard) Release() bool {
	g.mu.Lock()
	was := g.holding()
	if was {
		g.released = true
	}
	g.mu.Unlock()
	if was {
		g.logf("cpu guard: Send them now: queued prompts go in while the %s is busy, until it is calm again", Machine())
		if g.Changed != nil {
			g.Changed()
		}
	}
	return was
}

// State is the guard now; ok is false when it is turned off.
func (g *Guard) State() (State, bool) {
	g.tick(false)
	s, ok := g.state()
	if ok && s.Hold && g.Queued != nil {
		s.Held = g.Queued() // outside the lock: it asks the queue
	}
	s.Slowed, s.ByYou = []string{}, []string{}
	if ok && g.Slowed != nil {
		s.Slowed = g.Slowed()
	}
	if ok && g.SlowedByYou != nil {
		s.ByYou = g.SlowedByYou()
	}
	return s, ok
}

// Busy says whether the computer is busy now (the guard on, over the
// limit for the window and not calm since).
func (g *Guard) Busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.busy && g.set.On
}

func (g *Guard) state() (State, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.set.On {
		return State{}, false
	}
	s := State{Available: g.read.Available, Why: g.read.Why, Percent: math.Round(g.percent*10) / 10, Busy: g.busy,
		Limit: g.eff.Limit, LimitFrom: g.eff.From, Block: g.eff.Block, By: g.eff.By, Scheduled: g.set.Schedule.On,
		For: g.set.For, Hold: g.holding(), Released: g.busy && g.released}
	if !g.eff.Until.IsZero() {
		u := g.eff.Until.UTC()
		s.Until, s.Next = &u, g.eff.Next
	}
	if !g.eff.At.IsZero() {
		at := g.eff.At.UTC()
		s.SetAt = &at
	}
	if s.Why == "" && !s.Available {
		s.Why = "no reading yet"
	}
	if g.busy {
		since := g.since.UTC()
		s.Since = &since
	}
	if !g.peak.At.IsZero() {
		p := g.peak
		p.Percent = math.Round(p.Percent*10) / 10
		s.Peak = &p
	}
	return s, true
}

// Machine is what the computer is called in messages: "Mac" on macOS.
func Machine() string {
	if runtime.GOOS == "darwin" {
		return "Mac"
	}
	return "computer"
}

func minutes(d time.Duration) string {
	n := int(d.Round(time.Minute) / time.Minute)
	if n <= 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}
