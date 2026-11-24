package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/paths"
)

func TestNextStatusLine(t *testing.T) {
	acct, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", acct)
	write := func(path, cmd string) {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":`+cmd+`}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := []byte(`{"cwd":"` + proj + `","workspace":{"project_dir":"` + proj + `"}}`)
	if got := nextStatusLine(in); got != "" {
		t.Errorf("no status line anywhere: got %q", got)
	}
	write(filepath.Join(acct, "settings.json"), `"~/bin/my-line.sh"`)
	if got := nextStatusLine(in); got != "~/bin/my-line.sh" {
		t.Errorf("the account's: got %q", got)
	}
	write(filepath.Join(proj, ".claude", "settings.json"), `"echo project"`)
	if got := nextStatusLine(in); got != "echo project" {
		t.Errorf("the project's wins over the account's: got %q", got)
	}
	write(filepath.Join(proj, ".claude", "settings.local.json"), `"\"/Applications/x/agentctl\" statusline"`)
	if got := nextStatusLine(in); got != "echo project" {
		t.Errorf("the deck's own is skipped: got %q", got)
	}
}

// The deck hears of the limits only when they change for that account, or
// after reportEvery; other input (the time, the cost) doesn't count.
func TestStatusLineReportsOnChange(t *testing.T) {
	data := t.TempDir()
	at := func(used string) []byte {
		return []byte(`{"cost":{"total_cost_usd":` + used + `},"rate_limits":{"seven_day": {"used_percentage":` + used + `,"resets_at":1790000000}}}`)
	}
	if _, ok := rateLimitsSum([]byte(`{"model":{"id":"x"}}`)); ok {
		t.Error("input without rate_limits has a sum")
	}
	if _, ok := rateLimitsSum([]byte(`{"rate_limits":null}`)); ok {
		t.Error("rate_limits null has a sum")
	}
	a, _ := rateLimitsSum(at("12"))
	a2, _ := rateLimitsSum([]byte(`{"rate_limits":{"seven_day":{"used_percentage":12,"resets_at":1790000000}},"cost":{"total_cost_usd":3}}`))
	if a != a2 {
		t.Error("spacing or other fields changed the sum")
	}
	b, _ := rateLimitsSum(at("13"))
	if a == b {
		t.Error("a new percentage has the same sum")
	}

	now := time.Unix(1790000000, 0)
	work, personal := "/home/u/.claude-work", ""
	if !reportDue(data, work, a, now) {
		t.Error("the first report isn't due")
	}
	reported(data, work, a, now)
	if reportDue(data, work, a, now.Add(time.Minute)) {
		t.Error("the same limits a minute later are due")
	}
	if !reportDue(data, work, b, now.Add(time.Minute)) {
		t.Error("changed limits aren't due")
	}
	if !reportDue(data, personal, a, now.Add(time.Minute)) {
		t.Error("another account's first report isn't due")
	}
	if !reportDue(data, work, a, now.Add(reportEvery)) {
		t.Error("the same limits after reportEvery aren't due")
	}
}

// With the deck's status line in the account's settings.json, the one it
// replaced still runs after it.
func TestNextStatusLineSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(home, "data"))
	acct := filepath.Join(home, "acct")
	os.MkdirAll(acct, 0o700)
	t.Setenv("CLAUDE_CONFIG_DIR", acct)
	os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"statusLine":{"type":"command","command":"~/bin/mine.sh"}}`), 0o600)
	if err := claude.AddDeckStatusLine(acct, "/x/agentctl", paths.StatusLineSaved()); err != nil {
		t.Fatal(err)
	}
	if got := nextStatusLine([]byte(`{"cwd":"` + home + `"}`)); got != "~/bin/mine.sh" {
		t.Errorf("got %q, want the account's own status line", got)
	}
}
