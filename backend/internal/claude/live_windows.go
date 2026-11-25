package claude

import (
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// processTTYs: Windows consoles have no terminal device a process could be
// matched to, so jumping to a session's terminal is not available yet.
func processTTYs(pids []int) (map[int]string, bool) { return map[int]string{}, true }

var winLive struct {
	sync.Mutex
	at   time.Time
	last []Live
}

// LiveSessions lists interactive Claude Code processes (claude.exe, or node
// running Claude Code's cli.js) matched to their transcripts. It is the
// fallback for a Claude Code too old for `claude agents --json`. Listing
// processes goes through PowerShell, which takes a moment, so the answer is
// kept for 10 seconds. Sessions have no terminal here (TTY is empty).
func LiveSessions(skipTTYs map[string]bool) []Live {
	winLive.Lock()
	defer winLive.Unlock()
	if time.Since(winLive.at) < 10*time.Second {
		return winLive.last
	}
	winLive.at = time.Now()
	ps, err := proc.List("claude.exe", "node.exe")
	if err != nil {
		winLive.last = nil
		return nil
	}
	winLive.last = liveFromProcesses(ps, []string{ProjectsDir()})
	return winLive.last
}
