package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// The magic trick: a board takes agents from one screen. That screen loses
// them, everyone else (the board too) sees everything.
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

	// Off unless a screen is named.
	if code := post("small-board", `{"ids": ["a1"]}`); code != http.StatusNotFound {
		t.Fatalf("off: %d", code)
	}
	s.MagicPanel = "wall-panel"
	if code := post("small-board", `{"ids": ["a1", "a3"]}`); code != http.StatusNoContent {
		t.Fatalf("take: %d", code)
	}
	for viewer, want := range map[string]string{"small-board": "a1,a2,a3", "wall-panel": "a2", "other-board": "a1,a2,a3", "local": "a1,a2,a3"} {
		if got := ids(s.View(st, viewer)); got != want {
			t.Errorf("%s sees %q, want %q", viewer, got, want)
		}
	}
	if code := post("small-board", `{"ids": []}`); code != http.StatusNoContent {
		t.Fatalf("give back: %d", code)
	}
	if got := ids(s.View(st, "wall-panel")); got != "a1,a2,a3" {
		t.Errorf("given back: %s", got)
	}
	if code := post("small-board", `{}`); code != http.StatusBadRequest {
		t.Errorf("no ids: %d", code)
	}
	many := `{"ids": ["` + strings.Repeat(`x", "`, 16) + `x"]}`
	if code := post("small-board", many); code != http.StatusBadRequest {
		t.Errorf("17 ids: %d", code)
	}
}
