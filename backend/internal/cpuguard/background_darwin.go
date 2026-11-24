package cpuguard

import "syscall"

// From <sys/resource.h>: setpriority(PRIO_DARWIN_PROCESS, pid,
// PRIO_DARWIN_BG) puts a process in background mode, what `taskpolicy -b
// -p` sets: it runs on the efficiency cores and its disk use is throttled.
// 0 takes it out. A process of the same user needs no root. Never a nice
// value: lowering one again needs root.
const (
	prioDarwinProcess = 4      // PRIO_DARWIN_PROCESS: the second argument is a pid
	prioDarwinBG      = 0x1000 // PRIO_DARWIN_BG
)

// CanSlow says whether background mode exists here: macOS only.
const CanSlow = true

type osPriority struct{}

func (osPriority) Background(pid int, on bool) error {
	v := 0
	if on {
		v = prioDarwinBG
	}
	return syscall.Setpriority(prioDarwinProcess, pid, v)
}
