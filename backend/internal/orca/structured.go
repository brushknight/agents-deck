package orca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Read provider identity only, never chat content or credentials. The CLI
// reports live panes but not their provider thread IDs.
type structuredThread struct {
	SessionID string `json:"session_id"`
	ThreadID  string `json:"thread_id"`
}

func structuredThreads(path string) ([]structuredThread, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	const query = "SELECT session_id, json_extract(value, '$.handle.threadId') AS thread_id " +
		"FROM agent_session_records, json_each(record_json, '$.providerHandleChain') " +
		"WHERE json_extract(value, '$.handle.provider') = 'codex'"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "/usr/bin/sqlite3", "-readonly", "-json", path, query).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "no such table: agent_session_records") {
			return nil, nil // older Orca versions without structured chat records
		}
		return nil, fmt.Errorf("orca session identity: %w", err)
	}
	var rows []structuredThread
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Require an exact provider ID and a live Orca-owned chat. Folder/title
// matching could activate another agent's workspace.
func structuredWorkspace(rows []structuredThread, worktrees []Worktree, threadID string) (string, error) {
	sessions := map[string]bool{}
	for _, row := range rows {
		if threadID != "" && row.ThreadID == threadID && row.SessionID != "" {
			sessions[row.SessionID] = true
		}
	}
	target := ""
	for _, w := range worktrees {
		for _, a := range w.Agents {
			if !a.StructuredHostOwned {
				continue
			}
			pane, ok := strings.CutPrefix(a.PaneKey, "structured-agent-session-")
			if !ok {
				continue
			}
			session, _, ok := strings.Cut(pane, ":")
			if !ok || !sessions[session] {
				continue
			}
			if target != "" && target != w.WorktreeID {
				return "", errors.New("orca session has ambiguous live workspace ownership")
			}
			target = w.WorktreeID
		}
	}
	return target, nil
}

// The CLI can activate a chat's workspace, but cannot select its individual
// chat tab. Terminal-backed agents retain Switch. Genuine Codex-app threads
// return false and keep their deep link; errors never fall back to another app.
func (c Client) FocusCodex(threadID string) (bool, error) {
	if c.Bin == "" || !Running() {
		return false, nil
	}
	rows, err := structuredThreads(filepath.Join(filepath.Dir(RuntimeFile()), "agent-session-journal.db"))
	if err != nil {
		return false, err
	}
	matched := false
	for _, row := range rows {
		if threadID != "" && row.ThreadID == threadID {
			matched = true
			break
		}
	}
	if !matched {
		return false, nil
	}
	var snap struct {
		Worktrees []Worktree `json:"worktrees"`
	}
	err = c.call(&snap, "worktree", "ps")
	if err != nil {
		return false, err
	}
	workspace, err := structuredWorkspace(rows, snap.Worktrees, threadID)
	if err != nil {
		return false, err
	}
	if workspace == "" {
		return false, nil
	}
	if err := c.call(nil, "worktree", "activate", "--worktree", "id:"+workspace); err != nil {
		return true, err
	}
	return true, c.Front()
}
