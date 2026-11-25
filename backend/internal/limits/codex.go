package limits

import (
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// CodexRateLimits is rate_limits on a Codex token_count event. limit_id
// "codex" is the plan's own limit; other ids are separate buckets (one
// model's, for instance), often with no windows at all.
type CodexRateLimits struct {
	LimitID   string       `json:"limit_id"`
	LimitName string       `json:"limit_name"`
	Primary   *CodexWindow `json:"primary"`
	Secondary *CodexWindow `json:"secondary"`
	PlanType  string       `json:"plan_type"`
}

// CodexWindow: percent used, the window's length and its reset (Unix seconds).
type CodexWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      float64 `json:"resets_at"`
}

// Codex records a token_count report from the Codex home in place. It
// replaces that limit id's windows; a bucket with none is left out.
func (b *Book) Codex(place string, rl CodexRateLimits, at time.Time) bool {
	lid := rl.LimitID
	if lid == "" {
		lid = "codex"
	}
	prefix := ""
	if lid != "codex" {
		prefix = lid + ":"
	}
	var ws []model.LimitWindow
	for _, c := range []*CodexWindow{rl.Primary, rl.Secondary} {
		if c == nil {
			continue
		}
		w := model.LimitWindow{Minutes: c.WindowMinutes, Used: pct(c.UsedPercent), ResetsAt: unix(c.ResetsAt)}
		switch c.WindowMinutes {
		case 300:
			w.ID, w.Label = "five_hour", "5 hours"
		case 7 * 24 * 60:
			w.ID, w.Label = "seven_day", "week"
		default:
			w.ID, w.Label = "window_"+strconv.Itoa(c.WindowMinutes), duration(c.WindowMinutes)
		}
		w.ID = prefix + w.ID
		if prefix != "" {
			name := rl.LimitName
			if name == "" {
				name = lid
			}
			w.Label = name + " " + w.Label
		}
		w.Hit = w.Used >= 100
		ws = append(ws, w)
	}
	plan := ""
	if lid == "codex" {
		plan = rl.PlanType
	}
	return b.put(key{"codex", place}, "rollout", plan, at, func(id string) bool {
		if prefix == "" {
			return !strings.Contains(id, ":")
		}
		return strings.HasPrefix(id, prefix)
	}, ws)
}

func duration(min int) string {
	switch {
	case min%(24*60) == 0:
		return strconv.Itoa(min/(24*60)) + " days"
	case min%60 == 0:
		return strconv.Itoa(min/60) + " hours"
	}
	return strconv.Itoa(min) + " minutes"
}
