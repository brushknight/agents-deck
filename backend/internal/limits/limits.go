// Package limits keeps the usage limits Claude Code and Codex report about
// their own accounts, for direct use of the two tools:
//
//   - Claude Code: the status line's rate_limits (agentctl statusline), and
//     the "You've hit your … limit" records in its transcripts, the only
//     place a per-model limit shows up;
//   - Codex: rate_limits on the token_count events of its rollouts.
//
// Nothing is fetched from anywhere: the numbers move only while an agent in
// that account runs, so every set carries the time of its last report.
package limits

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The levels: ok below Near, near from Near, low from Low, out at 100 or hit.
const (
	Near = 80
	Low  = 95
)

// Level is how a window looks.
func Level(used int, hit bool) string {
	switch {
	case hit || used >= 100:
		return "out"
	case used >= Low:
		return "low"
	case used >= Near:
		return "near"
	}
	return "ok"
}

var rank = map[string]int{"ok": 0, "near": 1, "low": 2, "out": 3}

// Hit is a limit a tool said stopped it.
type Hit struct {
	ID       string // seven_day, five_hour, spend, model_week:<model>
	Label    string
	Model    string
	ResetsAt time.Time
}

// Book holds the latest report per tool and place (a Claude config folder,
// a Codex home). Safe for concurrent use.
type Book struct {
	Now func() time.Time // tests set it; nil: time.Now

	mu   sync.Mutex
	sets map[key]*set
}

type key struct{ tool, place string }

type set struct {
	plan, source string
	asOf         time.Time
	win          map[string]model.LimitWindow
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// put stores windows for a place; replace drops the place's windows whose
// ids match it first. It reports whether anything shown changed.
func (b *Book) put(k key, source, plan string, at time.Time, replace func(id string) bool, ws []model.LimitWindow) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sets == nil {
		b.sets = map[key]*set{}
	}
	s := b.sets[k]
	if s == nil {
		s = &set{win: map[string]model.LimitWindow{}}
		b.sets[k] = s
	}
	if at.Before(s.asOf) && source != "transcript" {
		return false // an older report (a slow status line) never wins
	}
	before := fingerprint(s)
	for id := range s.win {
		if replace != nil && replace(id) {
			delete(s.win, id)
		}
	}
	for _, w := range ws {
		s.win[w.ID] = w
	}
	if plan != "" {
		s.plan = plan
	}
	if at.After(s.asOf) {
		s.asOf, s.source = at, source
	}
	return fingerprint(s) != before
}

func fingerprint(s *set) string {
	ids := make([]string, 0, len(s.win))
	for id := range s.win {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sb strings.Builder
	sb.WriteString(s.plan)
	for _, id := range ids {
		w := s.win[id]
		sb.WriteString("|" + id + ":" + strconv.Itoa(w.Used) + ":" + strconv.Itoa(int(w.ResetsAt.Unix())))
		if w.Hit {
			sb.WriteString("!")
		}
	}
	return sb.String()
}

// Hit records a limit hit, until its reset time.
func (b *Book) Hit(tool, place string, h Hit, at time.Time) bool {
	if h.ResetsAt.Before(b.now()) {
		return false // an old hit, read again from history
	}
	w := model.LimitWindow{ID: h.ID, Label: h.Label, Model: h.Model, Used: 100, ResetsAt: h.ResetsAt, Hit: true}
	return b.put(key{tool, place}, "transcript", "", at, nil, []model.LimitWindow{w})
}

// State is every set, named: name turns a tool and place into the account's
// name ("" for none). Windows past their reset show as reset; hits past
// theirs are gone; a set with nothing left is left out.
func (b *Book) State(name func(tool, place string) string) []model.Limits {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Limits
	for k, s := range b.sets {
		l := model.Limits{Tool: k.tool, Plan: s.plan, AsOf: s.asOf, Source: s.source, Level: "ok"}
		if name != nil {
			l.Account = name(k.tool, k.place)
		}
		for _, w := range s.win {
			if !w.ResetsAt.IsZero() && !now.Before(w.ResetsAt) {
				if w.Model != "" || w.Hit && w.ID != "seven_day" && w.ID != "five_hour" {
					continue // a hit is over at its reset
				}
				w.Reset, w.Hit, w.Used = true, false, 0
			}
			w.Hidden = w.ID == "five_hour" || strings.HasSuffix(w.ID, ":five_hour")
			w.Level = Level(w.Used, w.Hit)
			if w.Reset {
				w.Level = "ok"
			}
			l.Windows = append(l.Windows, w)
			lv := w.Level
			if w.Model != "" && lv == "out" {
				lv = "near" // only that model stopped; the others still work
			}
			if !w.Hidden && rank[lv] > rank[l.Level] {
				l.Level = lv
			}
		}
		if len(l.Windows) == 0 {
			continue
		}
		sort.Slice(l.Windows, func(i, j int) bool { return order(l.Windows[i]) < order(l.Windows[j]) })
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tool != out[j].Tool {
			return out[i].Tool == "claude"
		}
		return out[i].Account < out[j].Account
	})
	return out
}

func sortWindows(ws []model.LimitWindow) {
	sort.Slice(ws, func(i, j int) bool { return ws[i].ID < ws[j].ID })
}

func sortSets(ss []keptSet) {
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].Tool != ss[j].Tool {
			return ss[i].Tool < ss[j].Tool
		}
		return ss[i].Place < ss[j].Place
	})
}

// order: the week first, then per-model weeks, spend, the rest; hidden last.
func order(w model.LimitWindow) string {
	p := "3"
	switch {
	case w.Hidden:
		p = "9"
	case strings.HasSuffix(w.ID, "seven_day"):
		p = "0"
	case w.Model != "":
		p = "1"
	case w.ID == "spend":
		p = "2"
	}
	return p + w.ID
}

// OutUntil is when an agent can run again: the latest reset among the
// limits it is out of (zero when it isn't). A per-model limit only stops
// agents on that model.
func OutUntil(ls []model.Limits, a model.Agent, now time.Time) time.Time {
	acct := ""
	if a.Account != nil {
		acct = a.Account.Name
	}
	var until time.Time
	for _, l := range ls {
		if l.Tool != a.Tool || l.Account != acct {
			continue
		}
		for _, w := range l.Windows {
			if w.Reset || !(w.Hit || w.Used >= 100) || !w.ResetsAt.After(now) {
				continue
			}
			if w.Model != "" && !strings.Contains(strings.ToLower(a.Model), w.Model) {
				continue
			}
			if w.ResetsAt.After(until) {
				until = w.ResetsAt
			}
		}
	}
	return until
}

// pct turns a reported percentage into a whole one, rounding up from
// 99.5 only to 100 when it is 100 (99.6% is not out).
func pct(v float64) int {
	if v >= 100 {
		return int(math.Round(v))
	}
	n := int(math.Round(v))
	if n >= 100 {
		n = 99
	}
	if n < 0 {
		n = 0
	}
	return n
}

func unix(sec float64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}
