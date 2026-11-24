package proc

import (
	"context"
	"os"
)

// readMachine reads /proc/stat every 5 s.
func readMachine(ctx context.Context, m *MachineMeter) {
	pollTicks(ctx, m, func() (ticks, error) {
		b, err := os.ReadFile("/proc/stat")
		if err != nil {
			return ticks{}, err
		}
		return parseProcStat(string(b))
	})
}
