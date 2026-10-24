package orca

import (
	"os"
	"testing"
)

// ORCA_LIVE=1 go test -run TestLive -v ./internal/orca (reads the running Orca; changes nothing).
func TestLive(t *testing.T) {
	if os.Getenv("ORCA_LIVE") == "" {
		t.Skip("set ORCA_LIVE=1 to read the running Orca")
	}
	c := Client{Bin: Bin()}
	if c.Bin == "" || !Running() {
		t.Skip("Orca isn't installed or running")
	}
	s, err := c.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Worktrees {
		t.Logf("worktree %s (%s) agents %d", w.DisplayName, w.Branch, len(w.Agents))
		for _, a := range w.Agents {
			t.Logf("  agent %s %s %s tool=%s", a.PaneKey, a.AgentType, a.State, a.ToolName)
		}
	}
	for _, term := range s.Terminals {
		scr, err := c.Screen(term.Handle)
		t.Logf("terminal %s %q pane %s: %d bytes of screen, %v", term.Handle, term.Title, term.PaneKey(), len(scr), err)
	}
}
