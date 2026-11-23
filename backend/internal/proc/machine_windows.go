package proc

import (
	"context"
	"syscall"
	"unsafe"
)

var getSystemTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimes")

// readMachine asks the kernel for the CPU times every 5 s. Kernel time
// includes idle time.
func readMachine(ctx context.Context, m *MachineMeter) {
	pollTicks(ctx, m, func() (ticks, error) {
		var idle, kernel, user syscall.Filetime
		r, _, err := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
		if r == 0 {
			return ticks{}, err
		}
		n := func(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
		return ticks{idle: n(idle), total: n(kernel) + n(user)}, nil
	})
}
