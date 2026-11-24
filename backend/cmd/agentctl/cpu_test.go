package main

import (
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/cpuguard"
)

// agentctl cpu's limit line says where the limit comes from and until when.
func TestLimitLine(t *testing.T) {
	until := time.Now().Add(time.Hour).Truncate(time.Minute)
	at := time.Now().Add(-time.Hour)
	for _, c := range []struct {
		s    cpuguard.State
		want string
	}{
		{cpuguard.State{Limit: 80, LimitFrom: "limit"}, "80%"},
		{cpuguard.State{Limit: 80, LimitFrom: "limit", Scheduled: true, Until: &until, Next: 60},
			"80%, your limit outside the schedule, until " + when(until) + ", then 60%"},
		{cpuguard.State{Limit: 60, LimitFrom: "schedule", Block: "Mon 09:00-18:00", Scheduled: true, Until: &until, Next: 80},
			"60% from the schedule (Mon 09:00-18:00), until " + when(until) + ", then 80%"},
		{cpuguard.State{Limit: 70, LimitFrom: "override", By: "agentctl", SetAt: &at, Until: &until, Next: 60},
			"70% picked by hand in agentctl at " + when(at) + ", until " + when(until) + ", then 60% (agentctl cpu limit schedule: back to the schedule)"},
	} {
		if got := limitLine(&c.s); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

// agentctl cpu schedule lists the blocks in the order of the week.
func TestScheduleText(t *testing.T) {
	g := cpuguard.Default()
	g.Schedule = cpuguard.Schedule{On: true, Blocks: []cpuguard.Block{
		{Days: []string{"sat", "sun"}, From: "00:00", To: "24:00", Limit: 95},
		{Days: []string{"mon", "tue", "wed", "thu", "fri"}, From: "09:00", To: "18:00", Limit: 60},
	}}
	got := scheduleText(g)
	want := "schedule (every week, on this computer's clock):\n" +
		"  Weekdays              09:00-18:00  60%\n" +
		"  Weekends              00:00-24:00  95%\n" +
		"  the rest of the week               80%\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	g.Schedule.On = false
	if got := scheduleText(g); !strings.HasPrefix(got, "no schedule: the limit is 80% all week\n") || !strings.Contains(got, "not followed") {
		t.Errorf("off:\n%s", got)
	}
}
