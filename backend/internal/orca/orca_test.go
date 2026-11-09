package orca

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeCLI is an `orca` that logs its arguments and prints reply.
func fakeCLI(t *testing.T, reply string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "orca")
	script := "#!/bin/sh\necho \"$@\" >> '" + argsFile + "'\ncat <<'EOF'\n" + reply + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

const chatPS = `{"ok":true,"result":{"terminals":[],"worktrees":[{"worktreeId":"w1::/work/app","path":"/work/app",
"agents":[
{"paneKey":"structured-agent-session-claude_abc:uuid1","state":"working","agentType":"claude","conversationName":"login plan"},
{"paneKey":"tab1:leaf1","state":"done","agentType":"claude"}]}]}}`

func TestReadSeesChatAgents(t *testing.T) {
	bin, _ := fakeCLI(t, chatPS)
	snap, err := Client{Bin: bin}.Read()
	if err != nil {
		t.Fatal(err)
	}
	agents := snap.Worktrees[0].Agents
	if len(agents) != 2 || len(snap.Terminals) != 0 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if !agents[0].IsChat() || agents[0].ConversationName != "login plan" {
		t.Fatalf("chat agent: %+v", agents[0])
	}
	if agents[1].IsChat() {
		t.Fatalf("terminal agent read as a chat: %+v", agents[1])
	}
}

func TestActivateOpensTheWorktree(t *testing.T) {
	bin, argsFile := fakeCLI(t, `{"ok":true,"result":{}}`)
	if err := (Client{Bin: bin}).Activate("w1::/work/app"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(argsFile)
	if want := "worktree activate --worktree id:w1::/work/app --json\n"; string(got) != want {
		t.Fatalf("args %q, want %q", got, want)
	}
}
