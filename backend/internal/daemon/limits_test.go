package daemon

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Status line reports and transcript hits land on the state, by account,
// and agents in an account that is out show until when.
func TestLimitsOnState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTSTERM_HOME", filepath.Join(home, "data"))
	if err := accounts.Save([]accounts.Account{{Name: "personal", Dir: "~/.claude", Color: "sky"}, {Name: "work", Dir: "~/.claude-work", Color: "yellow"}}, "work"); err != nil {
		t.Fatal(err)
	}
	st := store.New("", "test", "0")
	st.Account = accounts.Of
	d := &Daemon{Store: st}
	d.startLimits()
	defer func() { claude.LimitHit = nil }()

	resets := time.Now().Add(48 * time.Hour).Unix()
	body := []byte(`{"session_id":"s","rate_limits":{"seven_day":{"used_percentage":100,"resets_at":` + strconv.FormatInt(resets, 10) + `}}}`)
	if err := d.StatusLine(filepath.Join(home, ".claude-work"), body); err != nil {
		t.Fatal(err)
	}
	if err := d.StatusLine("", []byte(`{"rate_limits":{"seven_day":{"used_percentage":22,"resets_at":`+strconv.FormatInt(resets, 10)+`}}}`)); err != nil {
		t.Fatal(err)
	}
	// A Fable hit in the personal account, from its transcript.
	back := time.Now().UTC().Add(72 * time.Hour).Format("Jan 2 at 3pm")
	d.limitHit(filepath.Join(home, ".claude", "projects", "-p", "s.jsonl"), "You've hit your Fable limit · resets "+back+" (UTC). /model to switch models.", time.Now())

	work := &store.Entry{Account: "work"}
	work.A.ID, work.A.Tool, work.A.Model = "w1", "claude", "claude-opus-5-5"
	st.Add(work)
	snap := st.Snapshot()
	if len(snap.Limits) != 2 {
		t.Fatalf("limits: %+v", snap.Limits)
	}
	p, w := snap.Limits[0], snap.Limits[1]
	if p.Account != "personal" || p.Level != "near" || len(p.Windows) != 2 {
		t.Errorf("personal: %+v", p)
	}
	if w.Account != "work" || w.Level != "out" || w.Windows[0].Used != 100 {
		t.Errorf("work: %+v", w)
	}
	if got := snap.Agents[0].OutUntil; got.Unix() != resets {
		t.Errorf("the work agent is out until %d, got %v", resets, got)
	}
}
