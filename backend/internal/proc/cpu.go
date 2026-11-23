package proc

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CPUBudget is what the service and its helpers may use, idle, averaged
// over ten minutes: a light background app. Over it, the log says so.
const CPUBudget = 2.0 // percent of one core

// helperCPU adds up the CPU time of the helpers run through Timed, by
// program, since the service started.
var helperCPU = struct {
	sync.Mutex
	by map[string]*HelperUse
}{by: map[string]*HelperUse{}}

// HelperUse is one helper program's share.
type HelperUse struct {
	Program string        `json:"program"`
	Runs    int           `json:"runs"`
	CPU     time.Duration `json:"-"`
	Percent float64       `json:"percent"` // of one core, over the report's span
}

func noteHelper(cmd *exec.Cmd) {
	st := cmd.ProcessState
	if st == nil {
		return
	}
	name := filepath.Base(cmd.Path)
	helperCPU.Lock()
	h := helperCPU.by[name]
	if h == nil {
		h = &HelperUse{Program: name}
		helperCPU.by[name] = h
	}
	h.Runs++
	h.CPU += st.UserTime() + st.SystemTime()
	helperCPU.Unlock()
}

func helpersNow() map[string]HelperUse {
	helperCPU.Lock()
	defer helperCPU.Unlock()
	out := make(map[string]HelperUse, len(helperCPU.by))
	for k, v := range helperCPU.by {
		out[k] = *v
	}
	return out
}

// cpuSample is the service's CPU time so far, at one moment.
type cpuSample struct {
	at       time.Time
	self     time.Duration // the service itself
	children time.Duration // every helper that has ended (its own and theirs)
	helpers  map[string]HelperUse
}

func sampleNow() cpuSample {
	self, children := rusage()
	return cpuSample{at: time.Now(), self: self, children: children, helpers: helpersNow()}
}

// CPUMeter keeps a sample a minute for the last hour, to tell how much CPU
// the service and its helpers use (GET /admin/v1/cpu, `agentctl cpu`).
type CPUMeter struct {
	mu      sync.Mutex
	samples []cpuSample // oldest first, at most an hour and a minute
	started cpuSample
	logged  time.Time
}

// NewCPUMeter starts counting from now.
func NewCPUMeter() *CPUMeter {
	s := sampleNow()
	return &CPUMeter{samples: []cpuSample{s}, started: s}
}

// Run samples once a minute, forever. Over CPUBudget for the last ten
// minutes, it logs the use and its biggest helpers, at most every ten
// minutes.
func (m *CPUMeter) Run() {
	for range time.Tick(time.Minute) {
		m.add(sampleNow())
		if r := m.Report(); r.Last10 != nil && r.Last10.Percent > CPUBudget && time.Since(m.logged) >= 10*time.Minute {
			m.logged = time.Now()
			log.Printf("cpu: %s", r.Last10.Line())
		}
	}
}

func (m *CPUMeter) add(s cpuSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, s)
	if len(m.samples) > 61 {
		m.samples = m.samples[len(m.samples)-61:]
	}
}

// CPUSpan is the use over one span of time, in percent of one core.
type CPUSpan struct {
	Minutes  float64     `json:"minutes"`
	Percent  float64     `json:"percent"`  // the service and its helpers
	Service  float64     `json:"service"`  // the service itself
	Children float64     `json:"children"` // its helpers, all of them
	Helpers  []HelperUse `json:"helpers"`  // the helpers it runs with a time limit, biggest first
}

// Line is the span in one line, for the log.
func (s CPUSpan) Line() string {
	var top []string
	for i, h := range s.Helpers {
		if i == 4 {
			break
		}
		top = append(top, fmt.Sprintf("%s %.1f%% (%d runs)", h.Program, h.Percent, h.Runs))
	}
	line := fmt.Sprintf("%.1f%% over %.0f min (service %.1f%%, helpers %.1f%%)", s.Percent, s.Minutes, s.Service, s.Children)
	if len(top) > 0 {
		line += ": " + strings.Join(top, ", ")
	}
	return line
}

// CPUReport is what GET /admin/v1/cpu answers.
type CPUReport struct {
	Budget float64  `json:"budget"`
	Last1  *CPUSpan `json:"last1,omitempty"`
	Last10 *CPUSpan `json:"last10,omitempty"`
	Last60 *CPUSpan `json:"last60,omitempty"`
	Since  *CPUSpan `json:"sinceStart"`
}

// Report is the use over the last 1, 10 and 60 minutes (when the service
// has run that long) and since it started, up to now.
func (m *CPUMeter) Report() CPUReport {
	now := sampleNow()
	m.mu.Lock()
	defer m.mu.Unlock()
	back := func(min int) *CPUSpan {
		// the newest sample at least min minutes old
		for i := len(m.samples) - 1; i >= 0; i-- {
			if now.at.Sub(m.samples[i].at) >= time.Duration(min)*time.Minute-time.Second {
				s := span(m.samples[i], now)
				return &s
			}
		}
		return nil
	}
	start := span(m.started, now)
	return CPUReport{Budget: CPUBudget, Last1: back(1), Last10: back(10), Last60: back(60), Since: &start}
}

func span(a, b cpuSample) CPUSpan {
	wall := b.at.Sub(a.at)
	pct := func(d time.Duration) float64 {
		if wall <= 0 {
			return 0
		}
		return float64(d) / float64(wall) * 100
	}
	s := CPUSpan{Minutes: wall.Minutes(), Service: pct(b.self - a.self), Children: pct(b.children - a.children)}
	s.Percent = s.Service + s.Children
	for name, h := range b.helpers {
		d := h.CPU - a.helpers[name].CPU
		runs := h.Runs - a.helpers[name].Runs
		if runs > 0 {
			s.Helpers = append(s.Helpers, HelperUse{Program: name, Runs: runs, CPU: d, Percent: pct(d)})
		}
	}
	sort.Slice(s.Helpers, func(i, j int) bool {
		if s.Helpers[i].CPU != s.Helpers[j].CPU {
			return s.Helpers[i].CPU > s.Helpers[j].CPU
		}
		return s.Helpers[i].Program < s.Helpers[j].Program
	})
	return s
}
