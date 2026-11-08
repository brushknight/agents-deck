package orca

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStructuredWorkspaceExactIdentity(t *testing.T) {
	rows := []structuredThread{{SessionID: "chat-one", ThreadID: "thread-one"}, {SessionID: "chat-two", ThreadID: "thread-two"}}
	worktrees := []Worktree{
		{WorktreeID: "first", Path: "/work/shared", Agents: []Agent{{PaneKey: "structured-agent-session-chat-one:leaf", StructuredHostOwned: true}}},
		{WorktreeID: "second", Path: "/work/shared", Agents: []Agent{{PaneKey: "structured-agent-session-chat-two:leaf", StructuredHostOwned: true}}},
	}
	for _, tc := range []struct{ thread, want string }{{"thread-one", "first"}, {"thread-two", "second"}, {"unknown", ""}, {"", ""}} {
		got, err := structuredWorkspace(rows, worktrees, tc.thread)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, %v; want %q", tc.thread, got, err, tc.want)
		}
	}
	worktrees[0].Agents[0].StructuredHostOwned = false
	if got, err := structuredWorkspace(rows, worktrees, "thread-one"); got != "" || err != nil {
		t.Fatalf("unowned: %q %v", got, err)
	}
	worktrees[0].Agents[0].StructuredHostOwned = true
	worktrees = append(worktrees, Worktree{WorktreeID: "duplicate", Agents: worktrees[0].Agents})
	if _, err := structuredWorkspace(rows, worktrees, "thread-one"); err == nil {
		t.Fatal("ambiguous ownership accepted")
	}
	if got, err := structuredWorkspace(rows, nil, "thread-one"); got != "" || err != nil {
		t.Fatalf("closed chat: %q %v", got, err)
	}
}

func TestStructuredIdentityJournalReadOnly(t *testing.T) {
	if _, err := os.Stat("/usr/bin/sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	path := filepath.Join(t.TempDir(), "journal.db")
	schema := `CREATE TABLE agent_session_records(session_id TEXT, record_json TEXT);
INSERT INTO agent_session_records VALUES ('chat-one','{"providerHandleChain":[{"handle":{"provider":"codex","threadId":"thread-one"}},{"handle":{"provider":"codex","threadId":"thread-next"}}]}');
INSERT INTO agent_session_records VALUES ('other','{"providerHandleChain":[{"handle":{"provider":"claude","threadId":"wrong-provider"}}]}');`
	if out, err := exec.Command("/usr/bin/sqlite3", path, schema).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	before, _ := os.ReadFile(path)
	rows, err := structuredThreads(path)
	after, _ := os.ReadFile(path)
	if err != nil || len(rows) != 2 || rows[0].SessionID != "chat-one" || rows[0].ThreadID != "thread-one" || rows[1].ThreadID != "thread-next" {
		t.Fatalf("identity: %+v %v", rows, err)
	}
	if string(before) != string(after) {
		t.Fatal("journal was modified")
	}
	if rows, err := structuredThreads(filepath.Join(t.TempDir(), "missing.db")); err != nil || len(rows) != 0 {
		t.Fatalf("missing: %+v %v", rows, err)
	}
	empty := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if rows, err := structuredThreads(empty); err != nil || len(rows) != 0 {
		t.Fatalf("legacy: %+v %v", rows, err)
	}
}
