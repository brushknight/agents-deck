package demo

import (
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A still fleet keeps each agent's last activity the fixture's time ago,
// however much time passes.
func TestHoldStill(t *testing.T) {
	st := store.New("", "test host", "0.0.0")
	d, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().UTC().Add(10 * time.Minute)
	d.holdStill(later)
	for id, ago := range lastActive {
		e, ok := st.Get(id)
		if !ok {
			t.Fatalf("%s is not in the fixture", id)
		}
		if !e.A.LastActive.Equal(later.Add(-ago)) {
			t.Errorf("%s: last active %s, want %s", id, e.A.LastActive, later.Add(-ago))
		}
	}
}

// The demo fleet has the "Needs you" grid too: the agents that need you, in
// the order the fixture lists them.
func TestDemoNeedsYouGrid(t *testing.T) {
	st := store.New("", "test host", "0.0.0")
	if _, err := Load(st); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"k3f9a2": 1, "h0m3l4": 2, "d0cs1t": 3, "t3st5a": 4, "k8sn0d": 5, "4uthfl": 6, "plug1n": 7, "fw0t4x": 8}
	for _, a := range st.Snapshot().Agents {
		if a.AttentionSlot != want[a.ID] {
			t.Errorf("%s (%s): attentionSlot %d, want %d", a.ID, a.Status, a.AttentionSlot, want[a.ID])
		}
	}
}
