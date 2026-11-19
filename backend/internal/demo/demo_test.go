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
