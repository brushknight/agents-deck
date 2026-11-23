package server

// Queued prompts: POST /v1/agents/{id}/prompt with "mode": "queue" keeps a
// prompt for a busy agent and types it in once the agent's turn ends (its
// status goes idle). One at a time: after each, the next waits until the
// turn that prompt started has ended too. An agent waiting on a question or
// permission holds its queue (nothing is ever answered for you); it goes on
// once the question is answered and the turn ends. Every prompt goes in
// through the Prompter, the same path as "now".

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/queue"
)

// Timings of delivery, variables so tests can shorten them.
var (
	// queueTick: how often the queue is looked at besides every change.
	queueTick = time.Second
	// queueTurnWait: how long a delivered prompt may take to start a turn
	// (the agent leaves idle) before the next one goes anyway.
	queueTurnWait = time.Minute
	// queueRetry: after a prompt couldn't be typed in, when to try again.
	queueRetry = 30 * time.Second
	// queueGone: how long an agent can be off the deck before its queue is dropped.
	queueGone = 10 * time.Minute
)

// delivery is the queue's bookkeeping, per agent.
type delivery struct {
	mu      sync.Mutex
	typing  map[string]*sync.Mutex // one prompt typed into an agent at a time
	sent    map[string]sentMark    // the last queued prompt typed in, until its turn ends
	retry   map[string]time.Time   // typing failed: not before then
	missing map[string]time.Time   // off the deck since
}

type sentMark struct {
	since time.Time // the agent's statusSince when it was typed in (idle since then)
	at    time.Time
}

func (d *delivery) init() {
	if d.typing == nil {
		d.typing, d.sent = map[string]*sync.Mutex{}, map[string]sentMark{}
		d.retry, d.missing = map[string]time.Time{}, map[string]time.Time{}
	}
}

// lock serialises typing into one agent (the queue and a direct prompt
// would otherwise interleave their keys).
func (d *delivery) lock(id string) func() {
	d.mu.Lock()
	d.init()
	m := d.typing[id]
	if m == nil {
		m = &sync.Mutex{}
		d.typing[id] = m
	}
	d.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// inFlight: a queued prompt was typed in and its turn hasn't ended yet.
func (d *delivery) inFlight(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.init()
	_, ok := d.sent[id]
	return ok
}

// prompt types text into the agent through the Prompter, one at a time per agent.
func (s *Server) prompt(p Prompter, id, text string, interrupt bool) error {
	defer s.queue.lock(id)()
	return p.Prompt(id, text, interrupt)
}

// waitable: the agent is busy in a way that ends by itself (or with your answer).
func waitable(err error) bool {
	return errors.Is(err, ErrBusy) || errors.Is(err, ErrAsking) || errors.Is(err, ErrStarting)
}

// QueuedPrompt is one row of GET /v1/agents/{id}/queue.
type QueuedPrompt struct {
	ID       string    `json:"id"`
	Text     string    `json:"text"`
	By       string    `json:"by"`
	QueuedAt time.Time `json:"queuedAt"`
}

// enqueue answers a prompt sent with "mode": "queue": typed in now when the
// agent is idle and nothing waits before it, else kept.
func (s *Server) enqueue(w http.ResponseWriter, r *http.Request, p Prompter, id, text string, front bool) {
	if s.Queue == nil {
		writeErr(w, http.StatusNotImplemented, "queueing prompts is not available here")
		return
	}
	if s.Queue.Count(id) == 0 && !s.queue.inFlight(id) {
		err := s.prompt(p, id, text, false)
		if err == nil {
			auditNote(r, "sent now")
			writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
			return
		}
		if !waitable(err) {
			s.act(w, err)
			return
		}
	} else {
		e, ok := s.Store.Get(id)
		switch {
		case !ok:
			s.act(w, ErrNotFound)
			return
		case e.A.Status == model.Exited:
			s.act(w, ErrExited)
			return
		}
	}
	it, pos, err := s.Queue.Add(id, text, callerOf(r), front)
	if errors.Is(err, queue.ErrFull) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	where := "back"
	if front {
		where = "front"
	}
	auditNote(r, "queued "+it.ID+" at the "+where)
	log.Printf("queue %s: %s queued %s (%d bytes, %s)", id, callerOf(r), it.ID, len(text), where)
	s.Store.Touch()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "id": it.ID, "position": pos})
}

func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	out := []QueuedPrompt{}
	if s.Queue != nil {
		for _, it := range s.Queue.List(id) {
			out = append(out, QueuedPrompt{ID: it.ID, Text: it.Text, By: it.By, QueuedAt: it.Queued})
		}
	}
	if _, ok := s.Store.Get(id); !ok && len(out) == 0 {
		writeErr(w, http.StatusNotFound, ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": out})
}

func (s *Server) cancelQueued(w http.ResponseWriter, r *http.Request) {
	if s.Queue == nil {
		writeErr(w, http.StatusNotFound, "no such queued prompt")
		return
	}
	id, qid := r.PathValue("id"), r.PathValue("qid")
	_, ok, err := s.Queue.Cancel(id, qid)
	switch {
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
	case !ok:
		writeErr(w, http.StatusNotFound, "no such queued prompt: it was sent or cancelled already")
	default:
		auditNote(r, "cancelled "+qid)
		log.Printf("queue %s: %s cancelled %s", id, callerOf(r), qid)
		s.Store.Touch()
		w.WriteHeader(http.StatusNoContent)
	}
}

// RunQueue types queued prompts in as their agents become idle, until ctx
// ends. Start it once, with the service.
func (s *Server) RunQueue(ctx context.Context) {
	if s.Queue == nil || s.Store == nil {
		return
	}
	ch, unsub := s.Store.Subscribe()
	defer unsub()
	tick := time.NewTicker(queueTick)
	defer tick.Stop()
	for {
		s.deliver(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ch:
		case <-tick.C:
		}
	}
}

// deliver types in the next queued prompt of every agent that is ready for it.
func (s *Server) deliver(now time.Time) {
	waiting := s.Queue.Agents()
	if len(waiting) == 0 {
		return
	}
	p, ok := s.Ctl.(Prompter)
	if !ok {
		return
	}
	for id := range waiting {
		if s.deliverOne(p, id, now) {
			s.Store.Touch()
		}
	}
}

// deliverOne types in agent id's next prompt if it is ready; it reports
// whether the queue changed.
func (s *Server) deliverOne(p Prompter, id string, now time.Time) bool {
	d := &s.queue
	e, ok := s.Store.Get(id)
	d.mu.Lock()
	d.init()
	if !ok {
		if d.missing[id].IsZero() {
			d.missing[id] = now
		}
		gone := now.Sub(d.missing[id]) >= queueGone
		d.mu.Unlock()
		if !gone {
			return false
		}
		for _, it := range s.Queue.Drop(id) {
			s.auditQueue(it, "drop queued prompt", http.StatusGone, "the agent left the deck")
		}
		return true
	}
	delete(d.missing, id)
	// Only an idle agent takes the next one: running finishes its turn,
	// waiting needs your answer first, starting gets ready.
	if e.A.Status != model.Idle {
		d.mu.Unlock()
		return false
	}
	if m, ok := d.sent[id]; ok {
		if e.A.StatusSince.Equal(m.since) && now.Sub(m.at) < queueTurnWait {
			d.mu.Unlock()
			return false // the last one hasn't started its turn yet
		}
		delete(d.sent, id) // its turn ended (idle since a later time)
	}
	if now.Before(d.retry[id]) {
		d.mu.Unlock()
		return false
	}
	d.mu.Unlock()

	it, ok := s.Queue.Next(id)
	if !ok || !s.Queue.Take(it.ID) {
		return false
	}
	err := s.prompt(p, id, it.Text, false)
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case err == nil:
		d.sent[id] = sentMark{since: e.A.StatusSince, at: now}
		delete(d.retry, id)
		log.Printf("queue %s: delivered %s", id, it.ID)
		s.auditQueue(it, "deliver queued prompt", http.StatusNoContent, "")
	case waitable(err):
		s.Queue.PutBack(it) // it got busy in between: next time it is idle
	default:
		s.Queue.PutBack(it)
		d.retry[id] = now.Add(queueRetry)
		log.Printf("queue %s: %s not delivered: %v", id, it.ID, err)
		s.auditQueue(it, "deliver queued prompt", http.StatusInternalServerError, err.Error())
	}
	return true
}

// auditQueue records what the queue did with a prompt, under who queued it.
func (s *Server) auditQueue(it queue.Item, action string, code int, why string) {
	if s.Audit == nil {
		return
	}
	detail := it.ID + " to " + it.Agent
	if why != "" {
		detail += ": " + why
	}
	s.Audit.Log(devices.AuditEntry{Device: it.By, Action: action, Status: code, Detail: detail})
}

// ---- audit notes -----------------------------------------------------------------

type noteKey struct{}

// withNote gives the request a place for a handler's audit detail.
func withNote(r *http.Request) (*http.Request, *string) {
	note := new(string)
	return r.WithContext(context.WithValue(r.Context(), noteKey{}, note)), note
}

// auditNote adds a detail to the request's audit line (the queued prompt's id…).
func auditNote(r *http.Request, s string) {
	if p, ok := r.Context().Value(noteKey{}).(*string); ok {
		*p = s
	}
}
