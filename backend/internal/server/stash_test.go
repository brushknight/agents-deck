package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// The magic trick: off, nothing happens; on, the receiver takes what the
// giver shows, the giver loses those agents, everyone else (the receiver too)
// sees everything, and each of the two learns its part and its corners.
func TestMagicTrick(t *testing.T) {
	reg := &devices.Registry{Path: filepath.Join(t.TempDir(), "devices.json")}
	giver, err := reg.Board("aa:01", "wall-panel", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	receiver, _ := reg.Board("aa:02", "small-board", "1.0.0")
	other, _ := reg.Board("aa:03", "other-board", "1.0.0")
	s := &Server{Store: store.New("", "test deck", "0.0.0"), Devices: reg}
	st := model.State{Agents: []model.Agent{{ID: "a1", Slot: 0}, {ID: "a2", Slot: 1}, {ID: "a3", Slot: 20}}}
	ids := func(st model.State) string {
		var out []string
		for _, a := range st.Agents {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	post := func(path, who, body string) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if who != "" {
			r = withCaller(r, who)
		}
		w := httptest.NewRecorder()
		if path == "/v1/stash" {
			s.postStash(w, r)
		} else {
			s.postMagicView(w, r)
		}
		return w.Code
	}

	// Off: a take is refused, nobody gets a part.
	if code := post("/v1/stash", receiver.Name, `{"all": true}`); code != http.StatusConflict {
		t.Fatalf("off: %d", code)
	}
	if v := s.View(st, giver.Name); v.Magic != nil || ids(v) != "a1,a2,a3" {
		t.Fatalf("off, the giver: %+v %s", v.Magic, ids(v))
	}

	// The receiver needs a motion sensor; corners must be corners.
	if err := reg.SetMagic(devices.Magic{On: true, Giver: giver.Name, Receiver: receiver.Name}); err == nil {
		t.Fatal("a receiver without a motion sensor was accepted")
	}
	reg.SetMotion(receiver.Name, true)
	if err := reg.SetMagic(devices.Magic{On: true, Giver: giver.Name, Receiver: receiver.Name, GiverLeave: "middle"}); err == nil {
		t.Fatal("a corner that isn't one was accepted")
	}
	if err := reg.SetMagic(devices.Magic{On: true, Giver: receiver.Name, Receiver: receiver.Name}); err == nil {
		t.Fatal("the same device as giver and receiver was accepted")
	}
	if err := reg.SetMagic(devices.Magic{On: true, Giver: giver.Name, Receiver: receiver.Name}); err != nil {
		t.Fatal(err)
	}

	// On: parts and the default corners.
	if v := s.View(st, receiver.Name); v.Magic == nil || v.Magic.Role != "receiver" || v.Magic.Arrive != "top-right" || v.Magic.Leave != "bottom-right" {
		t.Fatalf("the receiver's part: %+v", v.Magic)
	}
	if v := s.View(st, giver.Name); v.Magic == nil || v.Magic.Role != "giver" || v.Magic.Taken || v.Magic.Leave != "bottom-left" || v.Magic.Arrive != "top-left" {
		t.Fatalf("the giver's part: %+v", v.Magic)
	}
	if v := s.View(st, other.Name); v.Magic != nil {
		t.Fatalf("another board got a part: %+v", v.Magic)
	}

	// Only the receiver takes; only the giver's view counts.
	if code := post("/v1/stash", other.Name, `{"ids": ["a1"]}`); code != http.StatusForbidden {
		t.Fatalf("another board took: %d", code)
	}
	post("/v1/magic/view", giver.Name, `{"ids": ["a2", "a3"]}`)
	post("/v1/magic/view", other.Name, `{"ids": ["a1"]}`)
	s.held.mu.Lock()
	view := strings.Join(s.held.view, ",")
	s.held.mu.Unlock()
	if view != "a2,a3" {
		t.Fatalf("the giver's view: %s", view)
	}
	if code := post("/v1/stash", receiver.Name, `{"ids": ["a2", "a3"]}`); code != http.StatusNoContent {
		t.Fatalf("take: %d", code)
	}
	for viewer, want := range map[string]string{giver.Name: "a1", receiver.Name: "a1,a2,a3", other.Name: "a1,a2,a3", "local": "a1,a2,a3"} {
		if got := ids(s.View(st, viewer)); got != want {
			t.Errorf("%s sees %q, want %q", viewer, got, want)
		}
	}
	if v := s.View(st, giver.Name); !v.Magic.Taken {
		t.Error("the giver isn't told they are taken")
	}

	// Given back.
	if code := post("/v1/stash", receiver.Name, `{"ids": []}`); code != http.StatusNoContent {
		t.Fatalf("give back: %d", code)
	}
	if v := s.View(st, giver.Name); ids(v) != "a1,a2,a3" || v.Magic.Taken {
		t.Errorf("given back: %s %+v", ids(v), v.Magic)
	}

	// Switched off: what was held is dropped, nobody has a part.
	post("/v1/stash", receiver.Name, `{"ids": ["a1"]}`)
	if err := reg.SetMagic(devices.Magic{}); err != nil {
		t.Fatal(err)
	}
	s.MagicChanged()
	if v := s.View(st, giver.Name); v.Magic != nil || ids(v) != "a1,a2,a3" {
		t.Errorf("switched off: %+v %s", v.Magic, ids(v))
	}

	// Bad bodies.
	if code := post("/v1/stash", receiver.Name, `{}`); code != http.StatusBadRequest {
		t.Errorf("no ids: %d", code)
	}
	many := `{"ids": ["` + strings.Repeat(`x", "`, 16) + `x"]}`
	if code := post("/v1/stash", receiver.Name, many); code != http.StatusBadRequest {
		t.Errorf("17 ids: %d", code)
	}
}
