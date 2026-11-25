package proc

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"time"
)

// SlowHelper is how long a background helper may take before the log says
// so; a slow ps, lsof or app CLI is the usual reason the deck lags.
var SlowHelper = 3 * time.Second

// Tidy makes a command the service runs in the background clean up after
// itself: it runs in its own process group, and when its context ends (a
// timeout) the whole group is killed, so nothing it started lives on. Wait
// gives up on its output shortly after. Call it after exec.CommandContext.
func Tidy(cmd *exec.Cmd) {
	tidy(cmd)
	cmd.WaitDelay = 2 * time.Second
}

// Output runs a helper with a time limit (see Tidy) and returns its
// standard output.
func Output(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	Tidy(cmd)
	return Timed(cmd, (*exec.Cmd).Output)
}

// Timed runs cmd with run (Output, CombinedOutput, …), counts its CPU time
// (CPUMeter), and logs it when it was slow or was stopped by its time limit.
func Timed(cmd *exec.Cmd, run func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	start := time.Now()
	out, err := run(cmd)
	noteHelper(cmd)
	if d := time.Since(start); d >= SlowHelper {
		what := "took"
		if cmd.ProcessState != nil && !cmd.ProcessState.Exited() {
			what = "stopped after"
		}
		log.Printf("helper %s %s %s", filepath.Base(cmd.Path), what, d.Round(100*time.Millisecond))
	}
	return out, err
}
