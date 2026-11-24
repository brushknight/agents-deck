package proc

import (
	"context"
	"errors"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// machineEvery is how often the machine's CPU use is read.
const machineEvery = 5 * time.Second

// MachineMeter keeps how busy the whole computer's CPU was, a reading every
// 5 s for the last hour. 100% means every core busy. Reading it costs no new
// processes: on macOS one iostat runs for the service's life, on Linux it
// reads /proc/stat, on Windows it asks the kernel (GetSystemTimes).
type MachineMeter struct {
	mu      sync.Mutex
	samples []machineSample // oldest first, the last hour
	why     string          // the last reason it couldn't read
}

type machineSample struct {
	at   time.Time
	busy float64 // percent of the whole machine, over the 5 s before at
}

// NewMachineMeter has no readings yet; Run takes them.
func NewMachineMeter() *MachineMeter { return &MachineMeter{} }

// Run reads the machine's CPU use until ctx ends.
func (m *MachineMeter) Run(ctx context.Context) { readMachine(ctx, m) }

func (m *MachineMeter) add(at time.Time, busy float64) {
	busy = math.Max(0, math.Min(100, busy))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, machineSample{at, busy})
	cut := 0
	for cut < len(m.samples) && at.Sub(m.samples[cut].at) > time.Hour {
		cut++
	}
	m.samples = m.samples[cut:]
}

func (m *MachineMeter) setWhy(why string) {
	m.mu.Lock()
	m.why = why
	m.mu.Unlock()
}

// MachineCPU is how busy the whole computer is, in percent of all its cores.
type MachineCPU struct {
	Available bool     `json:"available"`        // there is a reading from the last 15 s
	Why       string   `json:"why,omitempty"`    // when not available
	Cores     int      `json:"cores"`            // logical cores
	Now       float64  `json:"now"`              // the last 5 s
	Last1     *float64 `json:"last1,omitempty"`  // the last minute, once there is a minute of readings
	Last2     *float64 `json:"last2,omitempty"`  // 2 minutes
	Last10    *float64 `json:"last10,omitempty"` // 10 minutes
}

// Report is the machine's CPU use now and over the last 1, 2 and 10 minutes.
func (m *MachineMeter) Report() MachineCPU { return m.reportAt(time.Now()) }

func (m *MachineMeter) reportAt(now time.Time) MachineCPU {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := MachineCPU{Cores: runtime.NumCPU()}
	n := len(m.samples)
	if n == 0 || now.Sub(m.samples[n-1].at) > 3*machineEvery {
		r.Why = m.why
		if r.Why == "" && n > 0 {
			r.Why = "no reading since " + m.samples[n-1].at.Format(time.TimeOnly)
		} else if r.Why == "" {
			r.Why = "no reading yet"
		}
		return r
	}
	r.Available, r.Now = true, m.samples[n-1].busy
	r.Last1, r.Last2, r.Last10 = m.average(now, time.Minute), m.average(now, 2*time.Minute), m.average(now, 10*time.Minute)
	return r
}

// Average is the machine's CPU use over the span before now, for any span
// (the CPU guard's window). ok is false when the readings don't reach that
// far back yet, or the last one is older than 15 s.
func (m *MachineMeter) Average(span time.Duration) (avg float64, ok bool) {
	return m.averageAt(time.Now(), span)
}

func (m *MachineMeter) averageAt(now time.Time, span time.Duration) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.samples); n == 0 || now.Sub(m.samples[n-1].at) > 3*machineEvery {
		return 0, false
	}
	if a := m.average(now, span); a != nil {
		return *a, true
	}
	return 0, false
}

// average is the mean of the readings in the span before now, or nil when
// the readings don't reach that far back yet.
func (m *MachineMeter) average(now time.Time, span time.Duration) *float64 {
	if len(m.samples) == 0 || m.samples[0].at.After(now.Add(-span+machineEvery)) {
		return nil
	}
	sum, n := 0.0, 0
	for i := len(m.samples) - 1; i >= 0 && m.samples[i].at.After(now.Add(-span)); i-- {
		sum += m.samples[i].busy
		n++
	}
	if n == 0 {
		return nil
	}
	avg := math.Round(sum/float64(n)*10) / 10
	return &avg
}

// iostatParser reads the lines of `iostat -n0 -w 5`: a header naming the
// columns (repeated every 20 rows), then a row per interval. The first row
// is the average since the computer started, so it is skipped.
type iostatParser struct {
	idle  int // the "id" column; -1 before the first header
	first bool
}

func newIostatParser() *iostatParser { return &iostatParser{idle: -1} }

// line returns the busy percent (100 - idle) a data row gives.
func (p *iostatParser) line(s string) (busy float64, ok bool) {
	f := strings.Fields(s)
	for i, c := range f {
		if c == "id" {
			p.idle = i
			return 0, false
		}
	}
	if p.idle < 0 || p.idle >= len(f) {
		return 0, false
	}
	idle, err := strconv.ParseFloat(f[p.idle], 64)
	if err != nil {
		return 0, false // the "cpu  load average" line
	}
	if !p.first {
		p.first = true
		return 0, false
	}
	return 100 - idle, true
}

// ticks are a CPU's counters since start: time idle, and time in total.
type ticks struct{ idle, total uint64 }

// busyBetween is the busy percent between two readings of the counters.
func busyBetween(a, b ticks) (float64, bool) {
	if b.total <= a.total || b.idle < a.idle {
		return 0, false
	}
	total, idle := float64(b.total-a.total), float64(b.idle-a.idle)
	return (total - idle) / total * 100, true
}

// parseProcStat reads the counters on the "cpu" line of Linux's /proc/stat:
// user nice system idle iowait irq softirq steal (guest time is already in
// user). Idle is idle plus iowait.
func parseProcStat(stat string) (ticks, error) {
	for _, l := range strings.Split(stat, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var t ticks
		for i, v := range f[1:min(len(f), 9)] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return ticks{}, err
			}
			t.total += n
			if i == 3 || i == 4 {
				t.idle += n
			}
		}
		return t, nil
	}
	return ticks{}, errors.New("no cpu line in /proc/stat")
}

// pollTicks reads the counters every 5 s and adds the busy percent between
// readings, until ctx ends (Linux and Windows).
func pollTicks(ctx context.Context, m *MachineMeter, read func() (ticks, error)) {
	prev, err := read()
	if err != nil {
		m.setWhy(err.Error())
	}
	t := time.NewTicker(machineEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur, err := read()
		if err != nil {
			m.setWhy(err.Error())
			continue
		}
		if busy, ok := busyBetween(prev, cur); ok {
			m.add(time.Now(), busy)
		}
		prev = cur
	}
}
