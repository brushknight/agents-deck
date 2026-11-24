package proc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// iostat: the header names the columns, the first row (since the computer
// started) is skipped, a repeated header doesn't skip again, busy = 100 - id.
func TestIostatParser(t *testing.T) {
	out := `      cpu    load average
 us sy id   1m   5m   15m
 23 12 65  66.53 118.98 148.54
 59 26 15  66.53 118.98 148.54
 54 25 21  68.51 117.54 147.90
      cpu    load average
 us sy id   1m   5m   15m
  3  2 95   1.51   2.54   3.90
garbage
`
	p := newIostatParser()
	var got []float64
	for _, l := range strings.Split(out, "\n") {
		if busy, ok := p.line(l); ok {
			got = append(got, busy)
		}
	}
	if want := []float64{85, 79, 5}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("busy %v, want %v", got, want)
	}
	// Rows before any header aren't read.
	if _, ok := newIostatParser().line(" 23 12 65  66.53 118.98 148.54"); ok {
		t.Error("a row before the header counted")
	}
}

// /proc/stat: idle is idle + iowait, total the first eight counters; busy
// is the share of the time between two readings that wasn't idle.
func TestProcStat(t *testing.T) {
	a, err := parseProcStat("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 50 0 25 400 25 0 0 0 0 0\nintr 1 2 3\n")
	if err != nil || a.idle != 850 || a.total != 1000 {
		t.Fatalf("first %+v %v", a, err)
	}
	b, err := parseProcStat("cpu  400 10 140 1200 50 0 0 0 0 0\n")
	if err != nil || b.idle != 1250 || b.total != 1800 {
		t.Fatalf("second %+v %v", b, err)
	}
	if busy, ok := busyBetween(a, b); !ok || busy != 50 { // 400 of 800 not idle
		t.Errorf("busy %v %v", busy, ok)
	}
	if _, ok := busyBetween(b, b); ok {
		t.Error("no time passed, yet a reading")
	}
	if _, err := parseProcStat("intr 1 2 3\n"); err == nil {
		t.Error("no cpu line, no error")
	}
}

// Averages over 1, 2 and 10 minutes, each only once the readings reach
// that far back; a stale meter is not available.
func TestMachineAverages(t *testing.T) {
	m := NewMachineMeter()
	t0 := time.Unix(1_700_000_000, 0)
	at := t0
	// 9 minutes at 20%, then 1 minute at 80% (a reading every 5 s).
	for i := 1; i <= 120; i++ {
		at = t0.Add(time.Duration(i) * machineEvery)
		busy := 20.0
		if i > 108 {
			busy = 80
		}
		m.add(at, busy)
	}
	r := m.reportAt(at)
	if !r.Available || r.Now != 80 || r.Last1 == nil || *r.Last1 != 80 || r.Last2 == nil || *r.Last2 != 50 || r.Last10 == nil || *r.Last10 != 26 {
		t.Fatalf("report %+v", r)
	}

	// Any span: 5 minutes is 4 at 20% and 1 at 80%; 15 minutes is too far back.
	if avg, ok := m.averageAt(at, 5*time.Minute); !ok || avg != 32 {
		t.Errorf("5 minutes: %v %v", avg, ok)
	}
	if _, ok := m.averageAt(at, 15*time.Minute); ok {
		t.Error("15 minutes of readings from 10")
	}
	if _, ok := m.averageAt(at.Add(time.Minute), time.Minute); ok {
		t.Error("an average from a stale meter")
	}

	young := NewMachineMeter()
	young.add(t0.Add(5*time.Second), 10)
	young.add(t0.Add(60*time.Second), 30)
	r = young.reportAt(t0.Add(60 * time.Second))
	if !r.Available || r.Last1 == nil || *r.Last1 != 20 || r.Last2 != nil || r.Last10 != nil {
		t.Fatalf("young %+v", r)
	}

	r = young.reportAt(t0.Add(2 * time.Minute))
	if r.Available || r.Why == "" || r.Last1 != nil {
		t.Fatalf("stale %+v", r)
	}
	young.setWhy("iostat stopped: it ended")
	if r = young.reportAt(t0.Add(2 * time.Minute)); r.Why != "iostat stopped: it ended" {
		t.Fatalf("why %q", r.Why)
	}
	if r = NewMachineMeter().Report(); r.Available || r.Why != "no reading yet" {
		t.Fatalf("empty %+v", r)
	}

	// An hour of readings at most.
	for i := 1; i <= 2000; i++ {
		m.add(at.Add(time.Duration(i)*machineEvery), 1)
	}
	if n := len(m.samples); n > 721 {
		t.Errorf("%d readings kept", n)
	}
}

// AGENTSDECK_LIVE_CPU=1 go test ./internal/proc -run TestMachineLive -v:
// reads this computer for 30 s and prints each reading, to compare with
// iostat or top.
func TestMachineLive(t *testing.T) {
	if os.Getenv("AGENTSDECK_LIVE_CPU") == "" {
		t.Skip("set AGENTSDECK_LIVE_CPU=1 to read this computer's CPU for 30 s")
	}
	m := NewMachineMeter()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	for i := 0; i < 6; i++ {
		time.Sleep(machineEvery)
		r := m.Report()
		t.Logf("%s now %.0f%% available %v %s", time.Now().Format(time.TimeOnly), r.Now, r.Available, r.Why)
	}
	stop()
	<-done
	top := TopUsers(nil, 10)
	for _, u := range top.Users {
		t.Logf("%5.1f%% %-8s %s (%d)", u.Percent, u.Kind, u.Name, u.Processes)
	}
}
