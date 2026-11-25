//go:build !windows

package proc

import (
	"syscall"
	"time"
)

// rusage is the CPU time (user and system) of this process, and of every
// child it has waited for (and theirs).
func rusage() (self, children time.Duration) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		self = tv(ru.Utime) + tv(ru.Stime)
	}
	if syscall.Getrusage(syscall.RUSAGE_CHILDREN, &ru) == nil {
		children = tv(ru.Utime) + tv(ru.Stime)
	}
	return self, children
}

func tv(t syscall.Timeval) time.Duration { return time.Duration(t.Nano()) }
