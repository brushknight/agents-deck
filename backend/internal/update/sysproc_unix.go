//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

// detach: a session of its own, so stopping the service (launchd stops the
// job's process group) doesn't take the restarter with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
