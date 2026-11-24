package limits

import (
	"regexp"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// StatusLine is the part of Claude Code's status line input the deck reads
// (code.claude.com/docs/en/statusline, "rate_limits"). Each window can be
// absent on its own, and the whole object is absent for API key logins.
type StatusLine struct {
	SessionID  string `json:"session_id"`
	RateLimits *struct {
		FiveHour *ClaudeWindow `json:"five_hour"`
		SevenDay *ClaudeWindow `json:"seven_day"`
		Spend    *ClaudeWindow `json:"spend_limit"`
	} `json:"rate_limits"`
}

// ClaudeWindow: percent used (0..100; a spend limit can pass 100) and the
// reset time in Unix seconds.
type ClaudeWindow struct {
	Used     *float64 `json:"used_percentage"`
	ResetsAt float64  `json:"resets_at"`
}

var claudeWindows = []struct{ id, label string }{{"five_hour", "5 hours"}, {"seven_day", "week"}, {"spend", "spend"}}

// Claude records a status line report from the Claude account in place (its
// config folder). The windows it carries replace the ones from earlier
// reports; per-model hits from transcripts stay.
func (b *Book) Claude(place string, sl StatusLine, at time.Time) bool {
	if sl.RateLimits == nil {
		return false
	}
	got := []*ClaudeWindow{sl.RateLimits.FiveHour, sl.RateLimits.SevenDay, sl.RateLimits.Spend}
	var ws []model.LimitWindow
	present := map[string]bool{}
	for i, c := range got {
		if c == nil || c.Used == nil {
			continue
		}
		id := claudeWindows[i].id
		w := model.LimitWindow{ID: id, Label: claudeWindows[i].label, Used: pct(*c.Used), ResetsAt: unix(c.ResetsAt)}
		switch id {
		case "five_hour":
			w.Minutes = 300
		case "seven_day":
			w.Minutes = 7 * 24 * 60
		}
		present[id] = true
		ws = append(ws, w)
	}
	if len(ws) == 0 {
		return false
	}
	return b.put(key{"claude", place}, "statusline", "", at, func(id string) bool { return present[id] }, ws)
}

// hitRe matches Claude Code's limit message: "You've hit your Fable limit ·
// resets Nov 26 at 11pm (Europe/Lisbon). /model to switch models." The text
// is for people; only the limit's name and the reset time are taken.
var hitRe = regexp.MustCompile(`(?i)hit your (.+?) limit\s*[·•-]\s*resets\s+(.+?)\s*(?:\(([^)]+)\))?\s*(?:[.,]|$)`)

// ParseHit reads a limit message written at at. ok is false for text that
// isn't one, or whose time doesn't parse.
func ParseHit(text string, at time.Time) (Hit, bool) {
	m := hitRe.FindStringSubmatch(text)
	if m == nil {
		return Hit{}, false
	}
	what := strings.TrimSpace(strings.ToLower(m[1]))
	loc := time.Local
	if m[3] != "" {
		if l, err := time.LoadLocation(m[3]); err == nil {
			loc = l
		}
	}
	when, ok := parseReset(m[2], at.In(loc))
	if !ok {
		return Hit{}, false
	}
	h := Hit{ResetsAt: when.UTC()}
	switch what = strings.TrimSpace(strings.TrimSuffix(what, " weekly")); what {
	case "weekly", "week", "7-day":
		h.ID, h.Label = "seven_day", "week"
	case "5-hour", "session", "five-hour":
		h.ID, h.Label = "five_hour", "5 hours"
	case "spend", "monthly spend", "spend limit":
		h.ID, h.Label = "spend", "spend"
	default:
		f := strings.Fields(what)
		if len(f) == 0 {
			return Hit{}, false
		}
		h.Model = f[0]
		h.ID, h.Label = "model_week:"+h.Model, strings.ToUpper(h.Model[:1])+h.Model[1:]+" week"
	}
	return h, true
}

var dayRe = regexp.MustCompile(`(?i)^([a-z]{3})[a-z]*\.?\s+(\d{1,2})(?:,?\s+(?:at\s+)?)?`)
var clockRe = regexp.MustCompile(`(?i)^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)

// parseReset reads "Nov 26 at 11pm", "Nov 26, 11:30pm", "3:10pm" or "15:10"
// as the first such time after now (minus a minute), in now's zone.
func parseReset(s string, now time.Time) (time.Time, bool) {
	s = strings.TrimSpace(s)
	month, day := time.Month(0), 0
	if m := dayRe.FindStringSubmatch(s); m != nil {
		if t, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:])); err == nil {
			month = t.Month()
			day = atoi(m[2])
			s = strings.TrimSpace(s[len(m[0]):])
		}
	}
	c := clockRe.FindStringSubmatch(strings.ReplaceAll(s, " ", ""))
	if c == nil {
		if month == 0 || s != "" {
			return time.Time{}, false
		}
		c = []string{"", "0", "", "am"} // a date alone: its midnight
	}
	h, mi := atoi(c[1]), atoi(c[2])
	switch strings.ToLower(c[3]) {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	if h > 23 || mi > 59 {
		return time.Time{}, false
	}
	floor := now.Add(-time.Minute)
	if month == 0 {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, now.Location())
		if t.Before(floor) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	t := time.Date(now.Year(), month, day, h, mi, 0, 0, now.Location())
	if t.Before(floor) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
