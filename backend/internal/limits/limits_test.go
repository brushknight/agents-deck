package limits

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestLevel(t *testing.T) {
	for _, c := range []struct {
		used int
		hit  bool
		want string
	}{{0, false, "ok"}, {79, false, "ok"}, {80, false, "near"}, {94, false, "near"}, {95, false, "low"}, {99, false, "low"}, {100, false, "out"}, {12, true, "out"}} {
		if got := Level(c.used, c.hit); got != c.want {
			t.Errorf("Level(%d, %v) = %q, want %q", c.used, c.hit, got, c.want)
		}
	}
}

func TestParseHit(t *testing.T) {
	lisbon, _ := time.LoadLocation("Europe/Lisbon")
	at := time.Date(2026, 11, 24, 14, 12, 0, 0, lisbon)
	for _, c := range []struct {
		text, id, label, model string
		want                   time.Time
	}{
		{"You've hit your Fable limit · resets Nov 26 at 11pm (Europe/Lisbon). /model to switch models.", "model_week:fable", "Fable week", "fable", time.Date(2026, 11, 26, 23, 0, 0, 0, lisbon)},
		{"You've hit your weekly limit · resets Nov 26 at 11:30pm (Europe/Lisbon)", "seven_day", "week", "", time.Date(2026, 11, 26, 23, 30, 0, 0, lisbon)},
		{"You've hit your 5-hour limit · resets 4:40pm (Europe/Lisbon)", "five_hour", "5 hours", "", time.Date(2026, 11, 24, 16, 40, 0, 0, lisbon)},
		{"You've hit your 5-hour limit · resets 1pm (Europe/Lisbon)", "five_hour", "5 hours", "", time.Date(2026, 11, 25, 13, 0, 0, 0, lisbon)},    // already past today
		{"You've hit your weekly limit · resets Jan 2 at 9am (Europe/Lisbon)", "seven_day", "week", "", time.Date(2027, 1, 2, 9, 0, 0, 0, lisbon)}, // next year
	} {
		h, ok := ParseHit(c.text, at)
		if !ok {
			t.Errorf("ParseHit(%q) failed", c.text)
			continue
		}
		if h.ID != c.id || h.Label != c.label || h.Model != c.model || !h.ResetsAt.Equal(c.want) {
			t.Errorf("ParseHit(%q) = %+v, want %s %q %q %v", c.text, h, c.id, c.label, c.model, c.want)
		}
	}
	for _, bad := range []string{"API Error: 529 overloaded", "You've hit your limit", "You've hit your weekly limit · resets soon"} {
		if _, ok := ParseHit(bad, at); ok {
			t.Errorf("ParseHit(%q) should fail", bad)
		}
	}
}

func book(now time.Time) *Book { return &Book{Now: func() time.Time { return now }} }

func TestClaudeStatusLine(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	b := book(now)
	var sl StatusLine
	in := `{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1800003600},"seven_day":{"used_percentage":86.4,"resets_at":1800200000}}}`
	if err := json.Unmarshal([]byte(in), &sl); err != nil {
		t.Fatal(err)
	}
	if !b.Claude("/home/u/.claude-work", sl, now) {
		t.Fatal("first report should change the book")
	}
	if b.Claude("/home/u/.claude-work", sl, now.Add(time.Second)) {
		t.Error("the same numbers again should not")
	}
	got := b.State(func(tool, place string) string { return "work" })
	if len(got) != 1 {
		t.Fatalf("state: %+v", got)
	}
	l := got[0]
	if l.Tool != "claude" || l.Account != "work" || l.Level != "near" || l.Source != "statusline" || len(l.Windows) != 2 {
		t.Fatalf("set: %+v", l)
	}
	if w := l.Windows[0]; w.ID != "seven_day" || w.Used != 86 || w.Level != "near" || w.Hidden {
		t.Errorf("week first: %+v", w)
	}
	if w := l.Windows[1]; w.ID != "five_hour" || !w.Hidden {
		t.Errorf("five_hour is kept, hidden: %+v", w)
	}
	// No rate_limits (an API key login): nothing.
	if b.Claude("/x", StatusLine{}, now) {
		t.Error("no rate_limits should not change anything")
	}
}

func TestResetAndHits(t *testing.T) {
	now := time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC)
	b := book(now)
	used := 97.0
	sl := StatusLine{}
	sl.RateLimits = &struct {
		FiveHour *ClaudeWindow `json:"five_hour"`
		SevenDay *ClaudeWindow `json:"seven_day"`
		Spend    *ClaudeWindow `json:"spend_limit"`
	}{SevenDay: &ClaudeWindow{Used: &used, ResetsAt: float64(now.Add(-time.Minute).Unix())}}
	b.Claude("p", sl, now.Add(-time.Hour))
	fable := Hit{ID: "model_week:fable", Label: "Fable week", Model: "fable", ResetsAt: now.Add(48 * time.Hour)}
	if !b.Hit("claude", "p", fable, now) {
		t.Fatal("a hit should change the book")
	}
	if b.Hit("claude", "p", Hit{ID: "seven_day", ResetsAt: now.Add(-time.Hour)}, now) {
		t.Error("an old hit from history should be ignored")
	}
	l := b.State(nil)[0]
	if w := l.Windows[0]; w.ID != "seven_day" || !w.Reset || w.Used != 0 || w.Level != "ok" {
		t.Errorf("past its reset the week shows reset: %+v", w)
	}
	if w := l.Windows[1]; w.ID != "model_week:fable" || !w.Hit || w.Level != "out" {
		t.Errorf("fable: %+v", w)
	}
	if l.Level != "near" {
		t.Errorf("a per-model hit makes the set near, not out: %q", l.Level)
	}
	fableAgent := model.Agent{Tool: "claude", Model: "claude-fable-5"}
	if got := OutUntil([]model.Limits{l}, fableAgent, now); !got.Equal(fable.ResetsAt) {
		t.Errorf("an agent on fable is out until %v, got %v", fable.ResetsAt, got)
	}
	if got := OutUntil([]model.Limits{l}, model.Agent{Tool: "claude", Model: "claude-opus-5-5"}, now); !got.IsZero() {
		t.Errorf("other models still work, got %v", got)
	}
	if got := OutUntil([]model.Limits{l}, model.Agent{Tool: "claude", Model: "claude-fable-5", Account: &model.Account{Name: "personal"}}, now); !got.IsZero() {
		t.Errorf("another account is not out, got %v", got)
	}
	// After its reset the hit is gone.
	b.Now = func() time.Time { return fable.ResetsAt.Add(time.Second) }
	if l := b.State(nil)[0]; len(l.Windows) != 1 {
		t.Errorf("the fable hit should be gone: %+v", l.Windows)
	}
}

func TestCodex(t *testing.T) {
	now := time.Unix(1_795_000_000, 0).UTC()
	b := book(now)
	var rl CodexRateLimits
	in := `{"limit_id":"codex","limit_name":null,"primary":{"used_percent":7.0,"window_minutes":10080,"resets_at":1795614898},"secondary":null,"credits":{"has_credits":false,"unlimited":false,"balance":"0"},"plan_type":"pro"}`
	if err := json.Unmarshal([]byte(in), &rl); err != nil {
		t.Fatal(err)
	}
	b.Codex("/h/.codex", rl, now)
	b.Codex("/h/.codex", CodexRateLimits{LimitID: "premium"}, now) // a bucket with no windows
	b.Codex("/h/.codex", CodexRateLimits{LimitID: "other", LimitName: "Other", Primary: &CodexWindow{UsedPercent: 100, WindowMinutes: 300, ResetsAt: float64(now.Unix() + 60)}}, now)
	ls := b.State(nil)
	if len(ls) != 1 {
		t.Fatalf("state: %+v", ls)
	}
	l := ls[0]
	if l.Tool != "codex" || l.Plan != "pro" || l.Level != "ok" || len(l.Windows) != 2 {
		t.Fatalf("set: %+v", l)
	}
	if w := l.Windows[0]; w.ID != "seven_day" || w.Used != 7 || w.Minutes != 10080 || w.ResetsAt.Unix() != 1795614898 {
		t.Errorf("week: %+v", w)
	}
	if w := l.Windows[1]; w.ID != "other:five_hour" || !w.Hidden || !w.Hit || w.Label != "Other 5 hours" {
		t.Errorf("other bucket: %+v", w)
	}
	if got := OutUntil(ls, model.Agent{Tool: "codex"}, now); got.IsZero() {
		t.Error("a hit hidden window still stops the agent")
	}
}

func TestPct(t *testing.T) {
	for in, want := range map[float64]int{0: 0, 41.2: 41, 99.6: 99, 100: 100, 112.4: 112, -3: 0} {
		if got := pct(in); got != want {
			t.Errorf("pct(%v) = %d, want %d", in, got, want)
		}
	}
}
