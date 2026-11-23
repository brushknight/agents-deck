//go:build !windows

package tmux

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Bin is the tmux binary; launchd's PATH is minimal, so look in the usual places.
var Bin = func() string {
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "tmux"
}()

func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, Bin, append([]string{"-L", Socket}, args...)...)
	proc.Tidy(cmd)
	out, err := proc.Timed(cmd, (*exec.Cmd).Output)
	return strings.TrimRight(string(out), "\n"), err
}
