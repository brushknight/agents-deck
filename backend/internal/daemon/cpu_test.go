package daemon

import (
	"reflect"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// The deck's own agents by their tmux pane, hand-started Claude sessions by
// their terminal; agents in other apps, and ones whose pane is gone, have none.
func TestAgentTerminals(t *testing.T) {
	entries := []store.Entry{
		{A: model.Agent{ID: "a1", Title: "fix the login page"}, TmuxName: "a-a1"},
		{A: model.Agent{ID: "a2", AITitle: "tidy the tests"}, TmuxName: "a-a2"},
		{A: model.Agent{ID: "a3"}, TmuxName: "a-a3"}, // its pane is gone
		{A: model.Agent{ID: "l1", Title: "by hand"}, External: true, LiveTTY: "/dev/ttys021"},
		{A: model.Agent{ID: "c1", Title: "in the Codex app"}, External: true},
	}
	panes := map[string]string{"a-a1": "/dev/ttys003", "a-a2": "/dev/ttys004", "a-other": "/dev/ttys009"}
	want := []proc.AgentTerminal{
		{TTY: "/dev/ttys003", ID: "a1", Title: "fix the login page"},
		{TTY: "/dev/ttys004", ID: "a2", Title: "tidy the tests"},
		{TTY: "/dev/ttys021", ID: "l1", Title: "by hand"},
	}
	if got := AgentTerminals(entries, panes); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
