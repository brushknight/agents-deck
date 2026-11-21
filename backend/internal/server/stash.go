package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The magic trick (docs/features.md): switched on in the deck's settings
// (devices.Magic), a knock on the receiver takes the agents the giver shows,
// and the next knock gives them back. While they are taken the giver leaves
// them out, so their places on it go empty; every other screen, the
// receiver included, sees everything as usual. Each screen in the trick
// learns its part from model.State.Magic (Deck Link: the Magic frame) and
// flies its tiles to and from its corners. With the trick off nothing
// happens: takes are refused and nobody gets a part.
type stash struct {
	mu     sync.Mutex
	holder string          // who took them
	ids    map[string]bool // what was taken (none: nothing is held)
	at     time.Time       // when the holder last said so (it repeats itself every few seconds)
	view   []string        // what the giver last said it shows
	watch  sync.Once       // the timeout watcher (expire)
}

const (
	stashTTL = 30 * time.Second // the receiver repeats itself every 5 s: quiet this long, it is gone
	stashMax = 16               // agents a take may hold
	pageSize = 16               // slots on a page of the 4×4 deck
)

type callerKey struct{}

// withCaller notes who sent the request (a device's name).
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

// magic is the trick's settings now (off without a device registry).
func (s *Server) magic() devices.Magic {
	if s.Devices == nil {
		return devices.Magic{}
	}
	return s.Devices.Settings().Magic.WithDefaults()
}

// setStash makes holder the one holding ids (none: nothing is held). It
// says whether anything changed.
func (s *Server) setStash(holder string, ids []string) bool {
	s.held.mu.Lock()
	defer s.held.mu.Unlock()
	same := len(ids) == len(s.held.ids) && (len(ids) == 0 || holder == s.held.holder)
	for _, id := range ids {
		same = same && s.held.ids[id]
	}
	s.held.at = time.Now()
	if same {
		return false
	}
	s.held.holder, s.held.ids = holder, nil
	if len(ids) > 0 {
		s.held.ids = make(map[string]bool, len(ids))
		for _, id := range ids {
			s.held.ids[id] = true
		}
	}
	return true
}

// MagicChanged drops what is held after the settings change (a new giver
// or receiver, or the trick switched off) and tells every screen.
func (s *Server) MagicChanged() {
	s.held.mu.Lock()
	s.held.ids, s.held.view = nil, nil
	s.held.mu.Unlock()
	if s.Store != nil {
		s.Store.Touch()
	}
}

// View is the state as viewer sees it: the giver without the taken agents,
// and the giver and the receiver with their part in the trick.
func (s *Server) View(st model.State, viewer string) model.State {
	m := s.magic()
	if !m.On || viewer != m.Giver && viewer != m.Receiver {
		return st
	}
	s.held.mu.Lock()
	ids, at := s.held.ids, s.held.at
	s.held.mu.Unlock()
	taken := len(ids) > 0 && time.Since(at) <= stashTTL
	out := st
	if viewer == m.Receiver {
		out.Magic = &model.Magic{Role: "receiver", Taken: taken, Arrive: m.ReceiverArrive, Leave: m.ReceiverLeave}
		return out
	}
	out.Magic = &model.Magic{Role: "giver", Taken: taken, Arrive: m.GiverArrive, Leave: m.GiverLeave}
	if taken {
		out.Agents = make([]model.Agent, 0, len(st.Agents))
		for _, a := range st.Agents {
			if !ids[a.ID] {
				out.Agents = append(out.Agents, a)
			}
		}
	}
	return out
}

// postStash: POST /v1/stash {"ids": [...]} or {"all": true}, from the
// receiver: take those agents, or the ones the giver shows; {"ids": []}
// gives them back (this computer may give them back too).
func (s *Server) postStash(w http.ResponseWriter, r *http.Request) {
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
	m := s.magic()
	who := callerOf(r)
	take := body.All || len(body.IDs) > 0
	switch {
	case !m.On && take:
		writeErr(w, http.StatusConflict, "the magic trick is off (Settings, Magic trick)")
		return
	case take && who != m.Receiver:
		writeErr(w, http.StatusForbidden, who+" isn't the magic trick's receiver (Settings, Magic trick)")
		return
	case !take && who != m.Receiver && who != "local":
		w.WriteHeader(http.StatusNoContent) // nothing of theirs to give back
		return
	}
	ids := body.IDs
	if body.All {
		ids = s.giverShows()
	}
	if s.setStash(who, ids) {
		if len(ids) > 0 {
			log.Printf("magic trick: %s took %d agents from %s", who, len(ids), m.Giver)
		} else {
			log.Printf("magic trick: %s gave the agents back to %s", who, m.Giver)
		}
		s.Store.Touch() // the screens get the state again
	}
	s.held.watch.Do(func() { go s.expire() })
	w.WriteHeader(http.StatusNoContent)
}

// giverShows is what a take takes: the agents the giver last said it
// shows, still on the deck; before it said anything, the first page.
func (s *Server) giverShows() []string {
	s.held.mu.Lock()
	view := s.held.view
	s.held.mu.Unlock()
	st := s.Store.Snapshot()
	have := map[string]bool{}
	for _, a := range st.Agents {
		have[a.ID] = true
	}
	var ids []string
	for _, id := range view {
		if have[id] && len(ids) < stashMax {
			ids = append(ids, id)
		}
	}
	if view == nil {
		for _, a := range st.Agents {
			if a.Slot < pageSize && len(ids) < stashMax {
				ids = append(ids, a.ID)
			}
		}
	}
	return ids
}

// postMagicView: POST /v1/magic/view {"ids": [...]}, from the giver: the
// agents it shows now. From anyone else, ignored.
func (s *Server) postMagicView(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.IDs == nil {
		writeErr(w, http.StatusBadRequest, "body must be {\"ids\": [...]}")
		return
	}
	if len(body.IDs) > stashMax {
		body.IDs = body.IDs[:stashMax]
	}
	if m := s.magic(); m.On && callerOf(r) == m.Giver {
		s.held.mu.Lock()
		s.held.view = append([]string{}, body.IDs...)
		s.held.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}

// expire gives the agents back when the receiver has gone quiet for
// stashTTL (unplugged mid-trick), and tells the screens.
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
			log.Printf("magic trick: nothing from %s for %s: the agents go back", holder, stashTTL)
			s.Store.Touch()
		}
	}
}
