package link

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

func TestHarnessToolsHaveWireValues(t *testing.T) {
	for name, want := range map[string]linkpb.Tool{
		"claude":   linkpb.Tool_TOOL_CLAUDE,
		"codex":    linkpb.Tool_TOOL_CODEX,
		"gemini":   linkpb.Tool_TOOL_GEMINI,
		"opencode": linkpb.Tool_TOOL_OPENCODE,
		"copilot":  linkpb.Tool_TOOL_COPILOT,
		"shell":    linkpb.Tool_TOOL_SHELL,
	} {
		if got, ok := tools[name]; !ok || got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	if linkpb.Tool_TOOL_COPILOT != 7 {
		t.Fatalf("copilot wire value changed: %d", linkpb.Tool_TOOL_COPILOT)
	}
}
