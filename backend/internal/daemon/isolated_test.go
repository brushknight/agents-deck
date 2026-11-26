package daemon

import (
	"testing"
	"time"
)

// A test deck (Isolated) never watches, lists or offers other apps'
// sessions: every watcher returns at once and discovery is empty, so
// nothing it does can reach a real session.
func TestIsolatedDeckLeavesOtherAppsAlone(t *testing.T) {
	d := &Daemon{Isolated: true}
	for name, watch := range map[string]func(){
		"Orca": d.WatchOrca, "herdr": d.WatchHerdr, "cmux": d.WatchCmux, "Codex": d.WatchCodex,
		"Claude sessions started elsewhere": d.WatchLive,
	} {
		done := make(chan struct{})
		go func() { watch(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("a test deck watches %s", name)
		}
	}
	if got := d.discover(map[string]bool{}, map[string]bool{}); got != nil {
		t.Errorf("a test deck listed %d sessions it didn't start", len(got))
	}
}
