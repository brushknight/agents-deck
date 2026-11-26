package store

import (
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// What every screen gets of a Telegram prompt (the snapshot's lastPrompt)
// names its files without a path; the agent got the full paths.
func TestTelegramPromptShownWithoutPaths(t *testing.T) {
	s := New("", "test", "0")
	e := &Entry{A: model.Agent{ID: "k3f9a2", Status: model.Idle}}
	s.Add(e)
	prompt := "[Telegram] 1 message, 1 file 1. 26 Nov 2026 14:58: the logs " +
		"file: /home/someone/.local/share/agents-terminal/telegram/default/inbox/2026-11-26/145800-7/01-payments-1105.log"
	s.Update("k3f9a2", func(e *Entry) bool { e.A.LastPrompt = prompt; return true })
	for _, a := range s.Snapshot().Agents {
		if a.ID != "k3f9a2" {
			continue
		}
		if !strings.HasSuffix(a.LastPrompt, "file: 01-payments-1105.log") || strings.Contains(a.LastPrompt, "/") {
			t.Fatalf("lastPrompt %q", a.LastPrompt)
		}
		return
	}
	t.Fatal("agent not in the snapshot")
}
