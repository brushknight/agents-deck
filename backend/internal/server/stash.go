package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The stash: a screen "takes" some agents (the 1.8 inch board's magic trick,
// firmware/panel-core/src/fun_motion.h), and every other screen leaves them
// out of the state it gets, so their places there go empty; the taker still
// sees them. An empty list gives them back. A stash nobody gives back ends by
// itself after stashTTL.
type stash struct {
	mu     sync.Mutex
	holder string
	ids    map[string]bool
	at     time.Time
}

const (
	stashTTL = 10 * time.Minute
	stashMax = 16 // agents one screen may take at once
)

type callerKey struct{}

// withCaller notes who sent the request (a device's name), for the stash.
func withCaller(r *http.Request, who string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), callerKey{}, who))
}

// callerOf is the device that sent the request, or "local" (this computer:
// the dashboard, the CLI).
func callerOf(r *http.Request) string {
	if v, _ := r.Context().Value(callerKey{}).(string); v != "" {
		return v
	}
	return "local"
}

// setStash makes holder the one holding ids (none: nothing is held).
func (s *Server) setStash(holder string, ids []string) {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	s.held.holder, s.held.ids, s.held.at = holder, nil, time.Now()
	if len(ids) == 0 {
		return
	}
	s.held.ids = make(map[string]bool, len(ids))
	for _, id := range ids {
		s.held.ids[id] = true
	}
}

// View is the state as viewer sees it: without the agents another screen
// holds.
func (s *Server) View(st model.State, viewer string) model.State {
	s.held.mu.Lock()
	holder, ids, at := s.held.holder, s.held.ids, s.held.at
	s.held.mu.Unlock()
	if len(ids) == 0 || viewer == holder || time.Since(at) > stashTTL {
		return st
	}
	out := st
	out.Agents = make([]model.Agent, 0, len(st.Agents))
	for _, a := range st.Agents {
		if !ids[a.ID] {
			out.Agents = append(out.Agents, a)
		}
	}
	return out
}

// postStash: POST /v1/stash {"ids": [...]}: the caller takes those agents
// from every other screen; [] gives them back.
func (s *Server) postStash(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.IDs == nil {
		writeErr(w, http.StatusBadRequest, "body must be {\"ids\": [...]} ([] gives them back)")
		return
	}
	if len(body.IDs) > stashMax {
		writeErr(w, http.StatusBadRequest, "at most 16 agents at once")
		return
	}
	s.setStash(callerOf(r), body.IDs)
	s.Store.Touch() // every screen gets the state again
	w.WriteHeader(http.StatusNoContent)
}
