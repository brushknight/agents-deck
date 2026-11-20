package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A screen that takes agents keeps seeing them; every other screen (other
// devices, this computer) doesn't, until the taker gives them back.
func TestStash(t *testing.T) {
	s := &Server{Store: store.New("", "test deck", "0.0.0")}
	st := model.State{Agents: []model.Agent{{ID: "a1"}, {ID: "a2"}, {ID: "a3"}}}
	ids := func(st model.State) string {
		var out []string
		for _, a := range st.Agents {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	post := func(who, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/stash", strings.NewReader(body))
		if who != "" {
			r = withCaller(r, who)
		}
		w := httptest.NewRecorder()
		s.postStash(w, r)
		return w.Code
	}

	if got := ids(s.View(st, "wall-panel")); got != "a1,a2,a3" {
		t.Fatalf("nothing taken: %s", got)
	}
	if code := post("small-board", `{"ids": ["a1", "a3"]}`); code != http.StatusNoContent {
		t.Fatalf("take: %d", code)
	}
	if got := ids(s.View(st, "small-board")); got != "a1,a2,a3" {
		t.Errorf("the taker sees them all: %s", got)
	}
	for _, other := range []string{"wall-panel", "local"} {
		if got := ids(s.View(st, other)); got != "a2" {
			t.Errorf("%s: %s, want a2", other, got)
		}
	}
	if code := post("small-board", `{"ids": []}`); code != http.StatusNoContent {
		t.Fatalf("give back: %d", code)
	}
	if got := ids(s.View(st, "wall-panel")); got != "a1,a2,a3" {
		t.Errorf("given back: %s", got)
	}
	// No ids at all, or too many: refused.
	if code := post("small-board", `{}`); code != http.StatusBadRequest {
		t.Errorf("no ids: %d", code)
	}
	many := `{"ids": ["` + strings.Repeat(`x", "`, 16) + `x"]}`
	if code := post("small-board", many); code != http.StatusBadRequest {
		t.Errorf("17 ids: %d", code)
	}
	// All of them: the others see none, the taker all.
	if code := post("small-board", `{"all": true}`); code != http.StatusNoContent {
		t.Fatalf("all: %d", code)
	}
	if got := ids(s.View(st, "wall-panel")); got != "" {
		t.Errorf("all taken: %q", got)
	}
	if got := ids(s.View(st, "small-board")); got != "a1,a2,a3" {
		t.Errorf("the taker of all: %s", got)
	}
	post("small-board", `{"ids": []}`)
	if got := ids(s.View(st, "wall-panel")); got != "a1,a2,a3" {
		t.Errorf("all given back: %s", got)
	}
	// From this computer (no device): the holder is "local".
	post("", `{"ids": ["a2"]}`)
	if got := ids(s.View(st, "local")); got != "a1,a2,a3" {
		t.Errorf("local taker: %s", got)
	}
	if got := ids(s.View(st, "wall-panel")); got != "a1,a3" {
		t.Errorf("local took a2: %s", got)
	}
}
