package proc

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Spans: percent of one core over the span, the service's and its helpers',
// and the helpers that ran in it, biggest first.
func TestCPUSpan(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	a := cpuSample{at: t0, self: time.Second, children: 2 * time.Second, helpers: map[string]HelperUse{
		"lsof": {Program: "lsof", Runs: 10, CPU: 10 * time.Second},
		"tmux": {Program: "tmux", Runs: 5, CPU: time.Second},
	}}
	b := cpuSample{at: t0.Add(10 * time.Minute), self: 7 * time.Second, children: 14 * time.Second, helpers: map[string]HelperUse{
		"lsof": {Program: "lsof", Runs: 10, CPU: 10 * time.Second}, // didn't run in the span
		"tmux": {Program: "tmux", Runs: 605, CPU: 4 * time.Second},
		"ps":   {Program: "ps", Runs: 200, CPU: 9 * time.Second},
	}}
	s := span(a, b)
	near := func(got, want float64) bool { return got > want-0.001 && got < want+0.001 }
	if !near(s.Service, 1) || !near(s.Children, 2) || !near(s.Percent, 3) || s.Minutes != 10 {
		t.Fatalf("span %+v", s)
	}
	if len(s.Helpers) != 2 || s.Helpers[0].Program != "ps" || !near(s.Helpers[0].Percent, 1.5) || s.Helpers[1].Runs != 600 {
		t.Fatalf("helpers %+v", s.Helpers)
	}
	if l := s.Line(); !strings.HasPrefix(l, "3.0% over 10 min (service 1.0%, helpers 2.0%): ps 1.5% (200 runs), tmux 0.5% (600 runs)") {
		t.Errorf("line %q", l)
	}
}

// A helper run through Timed counts under its program's name.
func TestTimedCountsHelpers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no true(1)")
	}
	before := helpersNow()["true"].Runs
	if _, err := Timed(exec.Command("true"), (*exec.Cmd).Output); err != nil {
		t.Skip(err)
	}
	if got := helpersNow()["true"].Runs; got != before+1 {
		t.Errorf("runs %d, want %d", got, before+1)
	}
	m := NewCPUMeter()
	if r := m.Report(); r.Since == nil || r.Last1 != nil || r.Budget != CPUBudget {
		t.Errorf("a new meter: %+v", r)
	}
}
