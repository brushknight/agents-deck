package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/node"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

func runNode(t *testing.T, d *deck) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.s.RunNode(ctx)
}

// remote is deck d's view of another machine's agent with local id id.
func remote(d *deck, id string) (model.Agent, bool) {
	for _, a := range d.s.Store.Snapshot().Agents {
		if a.MachineTag != "" && len(a.ID) > len(id) && a.ID[:len(id)+1] == id+"@" {
			return a, true
		}
	}
	return model.Agent{}, false
}

func TestIndexReplicates(t *testing.T) {
	fast(t)
	a, b, ra, rb, _ := pair(t)
	a.s.Store.Add(&store.Entry{A: model.Agent{ID: "k3f9a2", Title: "shop api", Tool: "claude", Status: model.Running}})
	runNode(t, a)
	runNode(t, b)

	// Within 2 s on the other machine, labelled, with an @tag id.
	waitFor(t, "studio's agent on attic", 2*time.Second, func() bool { _, ok := remote(b, "k3f9a2"); return ok })
	got, _ := remote(b, "k3f9a2")
	if got.ID != "k3f9a2@s" || got.Machine != "studio" || got.MachineTag != "s" || !got.Reachable || got.Title != "shop api" {
		t.Fatalf("on attic: %+v", got)
	}
	// Its own agents carry its name now that a machine joined; reachable.
	for _, x := range a.s.Store.Snapshot().Agents {
		if x.ID == "k3f9a2" && (x.Machine != "studio" || !x.Reachable) {
			t.Errorf("on studio: %+v", x)
		}
	}
	// A change, then a removal, each within 2 s.
	a.s.Store.Update("k3f9a2", func(e *store.Entry) bool { e.A.Title = "shop api v2"; return true })
	waitFor(t, "change", 2*time.Second, func() bool { x, _ := remote(b, "k3f9a2"); return x.Title == "shop api v2" })
	slot := func() int { x, _ := remote(b, "k3f9a2"); return x.Slot }
	before := slot()

	// Relay cut: still there, not reachable, same slot.
	ra.down()
	rb.down()
	waitFor(t, "grey on attic", node.AwayAfter+time.Second, func() bool { x, ok := remote(b, "k3f9a2"); return ok && !x.Reachable })
	if slot() != before {
		t.Errorf("slot moved: %d → %d", before, slot())
	}
	// While apart, one agent changes and one starts; back, only those two records go.
	a.s.Store.Update("k3f9a2", func(e *store.Entry) bool { e.A.Status = model.Idle; return true })
	a.s.Store.Add(&store.Entry{A: model.Agent{ID: "n7aaaa", Title: "docs", Tool: "claude", Status: model.Idle}})
	time.Sleep(300 * time.Millisecond)
	n := b.s.Node.Received.Load()
	ra.up(t, ra.addr)
	rb.up(t, rb.addr)
	waitFor(t, "back and caught up", node.BackoffMax+2*time.Second, func() bool {
		x, ok := remote(b, "k3f9a2")
		_, ok2 := remote(b, "n7aaaa")
		return ok && ok2 && x.Reachable && x.Status == model.Idle
	})
	if sent := b.s.Node.Received.Load() - n; sent != 2 {
		t.Errorf("records sent after reconnect: %d, want 2 (only what was missed)", sent)
	}
	a.s.Store.Remove("n7aaaa")
	waitFor(t, "removal", 2*time.Second, func() bool { _, ok := remote(b, "n7aaaa"); return !ok })
}

// A record for an agent this machine owns, from another machine, is ignored.
func TestIndexForgedRecordIgnored(t *testing.T) {
	_, b, _, _, _ := pair(t)
	b.s.Store.Add(&store.Entry{A: model.Agent{ID: "mine01", Title: "attic's own", Tool: "claude"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.s.RunNode(ctx)
	studio, _ := b.s.Devices.Get("studio")
	forged, _ := json.Marshal(model.Agent{ID: "mine01", Title: "taken over"})
	body, _ := json.Marshal(map[string]any{"epoch": "e1", "seq": 1, "full": true, "agents": []json.RawMessage{forged}})
	w := httptest.NewRecorder()
	b.s.Node.ServeIndex(w, httptest.NewRequest("POST", "/node/v1/index", bytes.NewReader(body)), studio)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, a := range b.s.Store.Snapshot().Agents {
		if a.Title == "taken over" {
			t.Fatalf("forged record shown: %+v", a)
		}
	}
}

// One machine on its own: agents carry no machine (agentctl ls looks as before).
func TestIndexSingleMachineUnchanged(t *testing.T) {
	d := newDeck(t, "studio")
	d.s.Store.Add(&store.Entry{A: model.Agent{ID: "k3f9a2", Title: "x", Tool: "claude"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.s.RunNode(ctx)
	for _, a := range d.s.Store.Snapshot().Agents {
		if a.Machine != "" || a.MachineTag != "" || !a.Reachable {
			t.Fatalf("%+v", a)
		}
	}
}

// Acting on another machine's agent isn't possible from here yet: 501.
func TestRemoteAgentActionRefused(t *testing.T) {
	a, b, _, _, _ := pair(t)
	_ = a
	w := httptest.NewRecorder()
	b.s.UnixHandler(nil).ServeHTTP(w, httptest.NewRequest("POST", "/v1/agents/k3f9a2@s/interrupt", nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestTags(t *testing.T) {
	got := node.Tags("studio", []string{"attic", "archive", "home-1"})
	if got["home-1"] != "h" || got["attic"] == got["archive"] || len(got["attic"]) < 2 {
		t.Fatalf("%v", got)
	}
}
