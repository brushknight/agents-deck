package daemon

import (
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// AgentTerminals are the terminals of the agents whose terminal the deck
// knows, for CPU use by agent (`agentctl cpu --top`): an agent the deck
// started (its tmux pane; sessions maps tmux session to terminal) and a
// Claude session started by hand in a terminal. Agents in other apps
// (Codex, Orca, herdr, cmux) and background Claude sessions have no
// terminal the deck knows, so they are left out.
func AgentTerminals(entries []store.Entry, sessions map[string]string) []proc.AgentTerminal {
	var out []proc.AgentTerminal
	for _, e := range entries {
		tty := e.LiveTTY
		if !e.External && e.TmuxName != "" {
			tty = sessions[e.TmuxName]
		}
		if tty == "" {
			continue
		}
		title := e.A.Title
		if title == "" {
			title = e.A.AITitle
		}
		if title == "" {
			title = e.A.ID
		}
		out = append(out, proc.AgentTerminal{TTY: tty, ID: e.A.ID, Title: title})
	}
	return out
}
