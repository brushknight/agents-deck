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
	Hold  bool   `json:"hold"`  // while busy, hold queued prompts and start_agent over MCP
	Slow  bool   `json:"slow"`  // while busy, run the heaviest agent in background mode (macOS; Slower)
}

// Default is the setting when config.json has none.
func Default() Setting { return Setting{On: true, Limit: 80, For: "2m", Hold: true, Slow: true} }

// Windows are the lengths For may take.
var Windows = []string{"1m", "2m", "5m"}

// Check says what is wrong with a setting given to PATCH /admin/v1/settings.
func (s Setting) Check() error {
	if s.Limit < 60 || s.Limit > 95 {
		return errors.New("cpuGuard.limit must be 60 to 95")
	}
	for _, w := range Windows {
		if s.For == w {
			return nil
		}
	}
	return errors.New(`cpuGuard.for must be "1m", "2m" or "5m"`)
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
	Limit     int        `json:"limit"`
	For       string     `json:"for"`
	Hold      bool       `json:"hold"`           // queued prompts and start_agent are held now
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
	Logf        func(format string, args ...any)
	Now         func() time.Time // the clock; nil: time.Now

	mu       sync.Mutex
	set      Setting
	busy     bool
	since    time.Time
	released bool
	read     proc.MachineCPU
	percent  float64
	peak     Peak
}

// New is a guard with the default setting.
func New(m Meter) *Guard { return &Guard{Meter: m, set: Default()} }

// Set changes the setting; it applies at the next look.
func (g *Guard) Set(s Setting) {
	g.mu.Lock()
	g.set = s.Normal()
	g.mu.Unlock()
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
	now := g.now()
	r := g.Meter.Report()
	last1, ok1 := g.Meter.Average(ClearFor)
	g.mu.Lock()
	set := g.set
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
		if avg, ok := g.Meter.Average(set.Window()); ok && avg > float64(set.Limit) {
			g.busy, g.released, g.since = true, false, now.Add(-set.Window())
			change = fmt.Sprintf("the %s is busy: %.0f%% of the CPU for %s, over your limit of %d%%", Machine(), avg, set.For, set.Limit)
		}
	default:
		if ok1 && last1 < float64(set.Limit-ClearGap) {
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
			top = append(top, fmt.Sprintf("%s %.0f%%", u.Name, u.Percent))
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
// for whoever asked (start_agent over MCP).
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
		Limit: g.set.Limit, For: g.set.For, Hold: g.holding(), Released: g.busy && g.released}
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
