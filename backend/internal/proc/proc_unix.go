//go:build !windows

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

// Alive reports whether process pid exists. A process that belongs to
// another user still counts (signal 0 is refused with EPERM).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// NoWindow keeps a console program the service starts from opening a window
// of its own. Only Windows does that; elsewhere this does nothing.
func NoWindow(cmd *exec.Cmd) {}
