//go:build !windows

package daemon

import (
	"os/exec"
	"strings"
)

// LookPathLogin resolves a command through the user's login shell PATH.
func LookPathLogin(name string) string {
	out, err := exec.Command("/bin/zsh", "-lc", "command -v "+shellJoin([]string{name})).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
