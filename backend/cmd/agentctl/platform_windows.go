package main

import "errors"

// platformSupports stops, with a plain message, the commands that need tmux
// or iTerm, which Windows doesn't have. Watching sessions started by hand
// (live, add), the dashboard, boards and the service all work.
func platformSupports(cmd string) error {
	switch cmd {
	case "new", "attach", "a", "reopen", "restore", "resume", "continue", "handoff", "sim", "sim-agent", "open-link":
		return errors.New(cmd + " is not available on Windows yet: agentctl runs its own agents in tmux. " +
			"Start Claude Code or Codex yourself; agentctl live lists the sessions to put on the deck")
	}
	return nil
}
