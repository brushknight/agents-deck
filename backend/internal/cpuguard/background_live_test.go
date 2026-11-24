//go:build darwin

package cpuguard

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// Background mode on a sleep this test starts itself, and off again,
// checked with ps: its priority drops to 4 (the throttled band) and comes
// back; its nice value never changes. Opt in: AGENTSDECK_LIVE_SLOW=1 go test ./internal/cpuguard -run TestBackgroundLive -v
func TestBackgroundLive(t *testing.T) {
	if os.Getenv("AGENTSDECK_LIVE_SLOW") == "" {
		t.Skip("set AGENTSDECK_LIVE_SLOW=1 to run")
	}
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	look := func() string {
		out, err := exec.Command("/bin/ps", "-o", "pri=,nice=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(out)), " ")
	}
	before := look()
	if err := (osPriority{}).Background(pid, true); err != nil {
		t.Fatalf("on: %v", err)
	}
	during := look()
	if err := (osPriority{}).Background(pid, false); err != nil {
		t.Fatalf("off: %v", err)
	}
	after := look()
	t.Logf("pid %d (pri nice): before %s, in background mode %s, after %s", pid, before, during, after)
	if during == before || after != before {
		t.Errorf("ps doesn't show the change: before %s, during %s, after %s", before, during, after)
	}
}
