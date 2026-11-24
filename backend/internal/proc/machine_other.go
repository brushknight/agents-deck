//go:build !darwin && !linux && !windows

package proc

import (
	"context"
	"runtime"
)

// readMachine: nothing reads the machine's CPU use here.
func readMachine(ctx context.Context, m *MachineMeter) {
	m.setWhy("not measured on " + runtime.GOOS)
}
