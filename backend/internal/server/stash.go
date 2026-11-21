package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The stash, for a demo (the 1.8 inch board's magic trick,
// firmware/panel-core/src/fun_motion.h): a board "takes" agents from one
// screen, MagicPanel (a device's name; empty, the default: the trick is off).
// That screen leaves them out, so their places on it go empty; the taker sees
// only them; every other screen sees everything as usual. An empty list gives
// them back. A stash nobody gives back ends by itself after stashTTL.
type stash struct {
	mu     sync.Mutex
	holder string
	target string // the screen they were taken from
	ids    map[string]bool
	at     time.Time
	watch  sync.Once // the timeout watcher (expire)
}

const (
	stashTTL = 30 * time.Second // the board repeats itself every 5 s: quiet this long, it is gone
	stashMax = 16               // agents one screen may take at once
	pageSize = 16               // slots on a page of the 4×4 deck
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

// setStash makes holder the one holding ids, taken from target (none:
// nothing is held). It says whether anything changed (a board repeats
// itself every few seconds).
func (s *Server) setStash(holder, target string, ids []string) bool {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	same := len(ids) == len(s.held.ids) && (len(ids) == 0 || holder == s.held.holder && target == s.held.target)
	for _, id := range ids {
		same = same && s.held.ids[id]
	}
	s.held.at = time.Now()
	if same {
		return false
	}
	s.held.holder, s.held.target, s.held.ids = holder, target, nil
	if len(ids) == 0 {
		return true
	}
	s.held.ids = make(map[string]bool, len(ids))
	for _, id := range ids {
		s.held.ids[id] = true
	}
	return true
}

// View is the state as viewer sees it: the screen they were taken from
// without them, everyone else (the taker too) as it is.
func (s *Server) View(st model.State, viewer string) model.State {
	s.held.mu.Lock()
	holder, target, ids, at := s.held.holder, s.held.target, s.held.ids, s.held.at
	s.held.mu.Unlock()
	// The taker sees everything as usual: the page it took comes first in its
	// list anyway, and a knock must not send it a burst of agents to redraw
	// while its tiles fly.
	_ = holder
	if len(ids) == 0 || time.Since(at) > stashTTL || viewer != target {
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

// postStash: POST /v1/stash {"ids": [...]} or {"all": true}: the caller
// takes those agents, or every agent on the first page, from MagicPanel;
// {"ids": []} gives them back.
func (s *Server) postStash(w http.ResponseWriter, r *http.Request) {
	if s.MagicPanel == "" {
		writeErr(w, http.StatusNotFound, "the magic trick is off (agentctl serve with AGENTSDECK_MAGIC_PANEL set to a screen's name)")
		return
	}
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.IDs == nil && !body.All {
		writeErr(w, http.StatusBadRequest, "body must be {\"ids\": [...]} or {\"all\": true} ({\"ids\": []} gives them back)")
		return
	}
	if len(body.IDs) > stashMax {
		writeErr(w, http.StatusBadRequest, "at most 16 agents at once")
		return
	}
	ids := body.IDs
	if body.All { // the first page: what the screen opens on
		ids = nil
		for _, a := range s.Store.Snapshot().Agents {
			if a.Slot < pageSize {
				ids = append(ids, a.ID)
			}
		}
	}
	who := callerOf(r)
	if s.setStash(who, s.MagicPanel, ids) {
		if len(ids) > 0 {
			log.Printf("magic trick: %s took %d agents from %s", who, len(ids), s.MagicPanel)
		} else {
			log.Printf("magic trick: %s gave the agents back to %s", who, s.MagicPanel)
		}
		s.Store.Touch() // the screens get the state again
	}
	s.held.watch.Do(func() { go s.expire() })
	w.WriteHeader(http.StatusNoContent)
}

// expire gives the agents back when their holder has gone quiet for
// stashTTL (unplugged mid-trick), and tells the screens: View alone would
// stop hiding them, but nobody would send the panel the state again.
func (s *Server) expire() {
	for range time.Tick(2 * time.Second) {
		s.held.mu.Lock()
		gone := len(s.held.ids) > 0 && time.Since(s.held.at) > stashTTL
		holder := s.held.holder
		if gone {
			s.held.ids = nil
		}
		s.held.mu.Unlock()
		if gone {
			log.Printf("magic trick: nothing from %s for %s: the agents go back to %s", holder, stashTTL, s.MagicPanel)
			s.Store.Touch()
		}
	}
}
