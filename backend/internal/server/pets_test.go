package server

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pets"
)

// POST /v1/agents/{id}/pet/hatch hatches a ready egg, for a device that may
// act; 409 for an egg that isn't ready (or one that hatched already).
func TestHatchPet(t *testing.T) {
	s, _ := newTestServer()
	h := s.DeviceHandler()
	panel := map[string]string{"Device": "panel"}
	if c := do(h, "POST", "/v1/agents/a1/pet/hatch", "", panel).Code; c != 409 {
		t.Errorf("pets not available: %d", c)
	}
	book := pets.Open("", nil)
	s.Pets = book
	s.Store.Pet = book.View
	book.Feed([]pets.Sight{{ID: "a1", Session: "s1", Tokens: model.Tokens{Input: 10_000}}})
	if c := do(h, "POST", "/v1/agents/a1/pet/hatch", "", panel).Code; c != 409 {
		t.Errorf("an egg that isn't ready: %d", c)
	}
	book.Feed([]pets.Sight{{ID: "a1", Session: "s1", Tokens: model.Tokens{Input: 60_000}}})
	if p := s.Store.Snapshot().Agents[0].Pet; p == nil || !p.Ready {
		t.Fatalf("ready egg in the state: %+v", p)
	}
	if c := do(h, "POST", "/v1/agents/a1/pet/hatch", "", map[string]string{"Device": "viewer"}).Code; c != 403 {
		t.Errorf("a device that may only view: %d", c)
	}
	if c := do(h, "POST", "/v1/agents/nope/pet/hatch", "", panel).Code; c != 404 {
		t.Errorf("unknown agent: %d", c)
	}
	if c := do(h, "POST", "/v1/agents/a1/pet/hatch", "", panel).Code; c != 204 {
		t.Fatalf("hatch: %d", c)
	}
	if p := s.Store.Snapshot().Agents[0].Pet; p.Ready || p.Level != 1 || p.Name == "" {
		t.Errorf("hatched pet in the state: %+v", p)
	}
	if c := do(h, "POST", "/v1/agents/a1/pet/hatch", "", panel).Code; c != 409 {
		t.Errorf("hatching twice: %d", c)
	}
}
