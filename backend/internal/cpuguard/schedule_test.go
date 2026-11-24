package cpuguard

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // America/New_York for the clock-change weeks, on any machine

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

var zone = time.FixedZone("test", 2*3600)

// at is a time in the test zone; 2026-03-02 is a Monday.
func at(day, hour, min int) time.Time { return time.Date(2026, 3, day, hour, min, 0, 0, zone) }

func weekdays(from, to string, limit int) Block {
	return Block{Days: []string{"mon", "tue", "wed", "thu", "fri"}, From: from, To: to, Limit: limit}
}

func scheduled(blocks ...Block) Setting {
	s := Default()
	s.Schedule = Schedule{On: true, Blocks: blocks}
	return s
}

func checkEff(t *testing.T, s Setting, now time.Time, limit int, from, block string, until time.Time, next int) {
	t.Helper()
	e := s.EffectiveAt(now)
	if e.Limit != limit || e.From != from || e.Block != block || !e.Until.Equal(until) || e.Next != next {
		t.Errorf("%s: got %d%% from %s (%s) until %s then %d%%; want %d%% from %s (%s) until %s then %d%%",
			now.Format("Mon 15:04"), e.Limit, e.From, e.Block, e.Until.Format("Mon 2 15:04"), e.Next,
			limit, from, block, until.Format("Mon 2 15:04"), next)
	}
}

// A block covers its days from From to To; outside it the plain limit.
func TestScheduleBlocks(t *testing.T) {
	s := scheduled(weekdays("09:00", "18:00", 60), Block{Days: []string{"sat", "sun"}, From: "00:00", To: "24:00", Limit: 95})
	checkEff(t, s, at(4, 8, 59), 80, FromLimit, "", at(4, 9, 0), 60)
	checkEff(t, s, at(4, 9, 0), 60, FromSchedule, "Wed 09:00-18:00", at(4, 18, 0), 80)
	checkEff(t, s, at(4, 17, 59), 60, FromSchedule, "Wed 09:00-18:00", at(4, 18, 0), 80)
	checkEff(t, s, at(4, 18, 0), 80, FromLimit, "", at(5, 9, 0), 60)
	checkEff(t, s, at(6, 20, 0), 80, FromLimit, "", at(7, 0, 0), 95) // Friday night, then the weekend
	checkEff(t, s, at(8, 12, 0), 95, FromSchedule, "Sun 00:00-24:00", at(9, 0, 0), 80)

	// Saturday and Sunday back to back with the same limit: one change, at
	// Monday's start.
	checkEff(t, s, at(7, 12, 0), 95, FromSchedule, "Sat 00:00-24:00", at(9, 0, 0), 80)

	// Off: the plain limit, never changing by itself.
	s.Schedule.On = false
	checkEff(t, s, at(4, 10, 0), 80, FromLimit, "", time.Time{}, 0)
}

// A block whose To is before its From runs past midnight, also from
// Sunday into Monday.
func TestSchedulePastMidnight(t *testing.T) {
	s := scheduled(Block{Days: []string{"fri", "sun"}, From: "22:00", To: "06:00", Limit: 90})
	checkEff(t, s, at(6, 23, 0), 90, FromSchedule, "Fri 22:00-06:00", at(7, 6, 0), 80)
	checkEff(t, s, at(7, 3, 0), 90, FromSchedule, "Fri 22:00-06:00", at(7, 6, 0), 80)
	checkEff(t, s, at(7, 6, 0), 80, FromLimit, "", at(8, 22, 0), 90)
	checkEff(t, s, at(9, 5, 30), 90, FromSchedule, "Sun 22:00-06:00", at(9, 6, 0), 80) // Monday early
	checkEff(t, s, at(2, 5, 30), 90, FromSchedule, "Sun 22:00-06:00", at(2, 6, 0), 80) // the week before too
}

// Blocks follow the wall clock across a change of the clocks: 09:00 is
// 09:00 on the Sunday the clocks go forward, and on the one they go back.
func TestScheduleClockChange(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	s := scheduled(Block{Days: []string{"sun"}, From: "01:00", To: "09:00", Limit: 60})
	// 2026-03-08: 02:00 becomes 03:00.
	sat := time.Date(2026, 3, 7, 20, 0, 0, 0, ny)
	checkEff(t, s, sat, 80, FromLimit, "", time.Date(2026, 3, 8, 1, 0, 0, 0, ny), 60)
	sun := time.Date(2026, 3, 8, 1, 30, 0, 0, ny)
	e := s.EffectiveAt(sun)
	if e.Limit != 60 || e.Until.Hour() != 9 || e.Until.Sub(sun) != 6*time.Hour+30*time.Minute {
		t.Errorf("forward: %d%% until %s, in %s", e.Limit, e.Until, e.Until.Sub(sun))
	}
	if e := s.EffectiveAt(time.Date(2026, 3, 8, 3, 30, 0, 0, ny)); e.From != FromSchedule {
		t.Errorf("after the jump: %+v", e)
	}
	// 2026-11-01: 02:00 becomes 01:00 again.
	sun = time.Date(2026, 11, 1, 1, 30, 0, 0, ny)
	e = s.EffectiveAt(sun)
	if e.Limit != 60 || e.Until.Hour() != 9 || e.Until.Sub(sun) < 8*time.Hour {
		t.Errorf("back: %d%% until %s, in %s", e.Limit, e.Until, e.Until.Sub(sun))
	}
	// Monday after: the plain limit, until the next Sunday 01:00.
	checkEff(t, s, time.Date(2026, 11, 2, 12, 0, 0, 0, ny), 80, FromLimit, "", time.Date(2026, 11, 8, 1, 0, 0, 0, ny), 60)
}

// Blocks may touch but not overlap; the message names both blocks.
func TestScheduleCheck(t *testing.T) {
	ok := []Schedule{
		{Blocks: []Block{weekdays("09:00", "12:00", 60), weekdays("12:00", "18:00", 70)}},
		{Blocks: []Block{{Days: []string{"sun"}, From: "22:00", To: "24:00", Limit: 90}, {Days: []string{"mon"}, From: "00:00", To: "06:00", Limit: 90}}},
		{Blocks: []Block{{Days: []string{"mon"}, From: "22:00", To: "02:00", Limit: 90}, {Days: []string{"tue"}, From: "02:00", To: "03:00", Limit: 60}}},
	}
	for _, s := range ok {
		if err := s.Check(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := map[string]Schedule{
		"block 2 (Mon, Wed 10:00-11:00) overlaps block 1 (Weekdays 09:00-18:00)": {Blocks: []Block{weekdays("09:00", "18:00", 60),
			{Days: []string{"wed", "mon"}, From: "10:00", To: "11:00", Limit: 70}}},
		"block 2 (Tue 01:00-03:00) overlaps block 1 (Mon 22:00-02:00)": {Blocks: []Block{{Days: []string{"mon"}, From: "22:00", To: "02:00", Limit: 90},
			{Days: []string{"tue"}, From: "01:00", To: "03:00", Limit: 60}}},
		"block 2 (Mon 00:00-00:30) overlaps block 1 (Sun 23:00-01:00)": {Blocks: []Block{{Days: []string{"sun"}, From: "23:00", To: "01:00", Limit: 90},
			{Days: []string{"mon"}, From: "00:00", To: "00:30", Limit: 60}}},
		"half hours":        {Blocks: []Block{weekdays("09:15", "18:00", 60)}},
		"60 to 95":          {Blocks: []Block{weekdays("09:00", "18:00", 62)}},
		"at least one day":  {Blocks: []Block{{From: "09:00", To: "10:00", Limit: 60}}},
		"same time":         {Blocks: []Block{weekdays("09:00", "09:00", 60)}},
		`"fun" isn't a day`: {Blocks: []Block{{Days: []string{"fun"}, From: "09:00", To: "10:00", Limit: 60}}},
		"at most 50":        {Blocks: make([]Block, 51)},
	}
	for want, s := range bad {
		if err := s.Check(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

// PATCH: a limit picked by hand while the schedule is on lasts until its
// next change; with the schedule in the same patch it is the plain limit.
func TestSchedulePatch(t *testing.T) {
	now := at(4, 10, 0) // Wednesday, in the 60% block
	s := scheduled(weekdays("09:00", "18:00", 60))
	patch := func(s Setting, body string) Setting {
		t.Helper()
		out, err := Patch(s, json.RawMessage(body), now)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return out
	}
	got := patch(s, `{"limit": 70}`)
	if o := got.Override; got.Limit != 80 || o == nil || o.Limit != 70 || !o.Until.Equal(at(4, 18, 0)) || o.By != "Settings" || !o.At.Equal(now) {
		t.Fatalf("limit while scheduled: %+v %+v", got, got.Override)
	}
	if len(s.Schedule.Blocks) != 1 || s.Override != nil {
		t.Fatal("the patch changed the old setting")
	}
	if e := got.EffectiveAt(now); e.Limit != 70 || e.From != FromOverride || e.Next != 80 {
		t.Errorf("effective %+v", e)
	}
	if back := patch(got, `{"override": null}`); back.Override != nil || back.EffectiveAt(now).Limit != 60 {
		t.Errorf("back to schedule: %+v", back)
	}
	if p := patch(s, `{"limit": 85, "schedule": {"on": true}}`); p.Limit != 85 || p.Override != nil || len(p.Schedule.Blocks) != 1 {
		t.Errorf("plain limit: %+v", p)
	}
	o := patch(s, `{"override": {"limit": 75, "until": "2026-03-04T15:00:00+02:00", "by": "agentctl"}}`)
	if o.Override == nil || o.Override.By != "agentctl" || o.EffectiveAt(now).Limit != 75 || o.EffectiveAt(at(4, 15, 0)).Limit != 60 {
		t.Errorf("for 5h: %+v", o.Override)
	}
	if off := patch(o, `{"schedule": {"on": false}}`); off.Override != nil {
		t.Error("turning the schedule off keeps the override")
	}
	plain := Default()
	if p := patch(plain, `{"override": {"limit": 65}}`); p.Limit != 65 || p.Override != nil {
		t.Errorf("override without a schedule or an end: %+v", p)
	}
	if p := patch(plain, `{"limit": 65}`); p.Limit != 65 || p.Override != nil {
		t.Errorf("limit without a schedule: %+v", p)
	}
	for _, bad := range []string{
		`{"override": {"limit": 70, "until": "2026-03-04T09:00:00+02:00"}}`,
		`{"override": {"limit": 70, "until": "2026-03-20T09:00:00+02:00"}}`,
		`{"override": {"limit": 71}}`,
		`{"limit": 72}`,
		`{"schedule": {"blocks": [{"days": ["mon"], "from": "09:00", "to": "18:00", "limit": 60}, {"days": ["mon"], "from": "17:00", "to": "19:00", "limit": 70}]}}`,
	} {
		if _, err := Patch(s, json.RawMessage(bad), now); err == nil {
			t.Errorf("%s passed", bad)
		}
	}
}

// The guard follows the schedule on its clock: one log line per change,
// busy decided again at the new limit, and an override that is over is
// dropped (Ended) for the schedule.
func TestGuardFollowsSchedule(t *testing.T) {
	t0 := at(4, 8, 50) // Wednesday
	m := &fakeMeter{now: t0, start: t0}
	var logs []string
	var ended []Override
	g := New(m)
	g.Now = func() time.Time { return m.now }
	g.Logf = func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	g.Ended = func(o Override) { ended = append(ended, o) }
	s := scheduled(weekdays("09:00", "18:00", 60))
	s.Limit = 90
	g.Set(s)
	if len(logs) != 0 {
		t.Fatalf("a line for the plain limit at the start: %q", logs)
	}
	m.run(9*time.Minute, 70, g) // 08:59, at 70%: under 90
	if busy(g) {
		t.Fatal("busy under the plain limit")
	}
	m.run(time.Minute, 70, g) // 09:00: the limit is 60
	if len(logs) != 2 || logs[0] != "cpu guard: limit 60% from the schedule (Wed 09:00-18:00)" || !strings.Contains(logs[1], "over your limit of 60%") {
		t.Fatalf("log %q", logs)
	}
	if st, _ := g.State(); !busy(g) || st.Limit != 60 || st.LimitFrom != FromSchedule || !st.Scheduled || st.Until == nil || !st.Until.Equal(at(4, 18, 0)) || st.Next != 90 {
		t.Fatalf("at 09:00: %+v", st)
	}

	// Picked by hand: 75% until 18:00; at 70% the Mac is calm at once.
	o := &Override{Limit: 75, Until: at(4, 12, 0), By: "agentctl", At: m.now}
	s.Override = o
	g.Set(s)
	m.run(5*time.Second, 70, g)
	if busy(g) {
		t.Fatal("still busy under the new limit")
	}
	st, _ := g.State()
	if st.LimitFrom != FromOverride || st.By != "agentctl" || st.SetAt == nil || !st.Until.Equal(at(4, 12, 0)) || st.Next != 60 {
		t.Fatalf("override: %+v", st)
	}
	if !strings.Contains(logs[len(logs)-1], "calm again: 70% of the CPU for 2m is under your limit, now 75%") ||
		!strings.Contains(logs[len(logs)-2], "limit 75% set by agentctl until Wed 12:00") {
		t.Fatalf("log %q", logs)
	}

	// Noon: the override is over, back to the schedule's 60%.
	m.run(3*time.Hour, 50, g)
	if len(ended) != 1 || ended[0].Limit != 75 || g.Setting().Override != nil {
		t.Fatalf("ended %+v, setting %+v", ended, g.Setting().Override)
	}
	if st, _ := g.State(); st.Limit != 60 || st.LimitFrom != FromSchedule {
		t.Fatalf("after noon %+v", st)
	}
	n := len(logs)
	m.run(10*time.Minute, 50, g)
	if len(logs) != n {
		t.Errorf("more lines without a change: %q", logs[n:])
	}
	m.now = at(4, 17, 59)
	m.run(time.Minute, 50, g)
	if last := logs[len(logs)-1]; last != "cpu guard: limit 90%, your limit outside the schedule" {
		t.Errorf("18:00: %q", last)
	}
}

// After a restart: an override not over yet is in use again; one that ended
// while the service was stopped is dropped.
func TestOverrideAfterRestart(t *testing.T) {
	now := at(4, 10, 0)
	var ended []Override
	g := New(steadyFake(50))
	g.Now = func() time.Time { return now }
	g.Ended = func(o Override) { ended = append(ended, o) }
	s := scheduled(weekdays("09:00", "18:00", 60))
	s.Override = &Override{Limit: 70, Until: at(4, 12, 0), By: "agentctl", At: at(4, 9, 30)}
	b, _ := json.Marshal(s)
	var cfg any
	_ = json.Unmarshal(b, &cfg)
	g.Set(FromConfig(cfg))
	if e := g.Limit(); e.Limit != 70 || e.From != FromOverride || len(ended) != 0 {
		t.Fatalf("still on: %+v", e)
	}
	now = at(4, 13, 0)
	g.Set(FromConfig(cfg))
	if e := g.Limit(); e.Limit != 60 || e.From != FromSchedule || len(ended) != 1 || g.Setting().Override != nil {
		t.Fatalf("over: %+v, ended %v", e, ended)
	}
}

type steadyFake float64

func (m steadyFake) Report() proc.MachineCPU {
	return proc.MachineCPU{Available: true, Now: float64(m)}
}
func (m steadyFake) Average(time.Duration) (float64, bool) { return float64(m), true }
