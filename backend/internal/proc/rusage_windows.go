package proc

import (
	"syscall"
	"time"
)

// rusage is the CPU time of this process; Windows doesn't add up ended
// children, so only the helpers run through Timed count there.
func rusage() (self, children time.Duration) {
	var c, e, k, u syscall.Filetime
	h, err := syscall.GetCurrentProcess()
	if err == nil && syscall.GetProcessTimes(h, &c, &e, &k, &u) == nil {
		self = time.Duration(k.Nanoseconds() + u.Nanoseconds())
	}
	for _, v := range helpersNow() {
		children += v.CPU
	}
	return self, children
}
