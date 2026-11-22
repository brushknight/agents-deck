//go:build !windows

package daemon

import (
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// LookPathLogin resolves a command through the user's login shell PATH.
func LookPathLogin(name string) string {
	out, err := proc.Output(10*time.Second, "/bin/zsh", "-lc", "command -v "+shellJoin([]string{name}))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
