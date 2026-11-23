// Package queue keeps prompts waiting for a busy agent: sent with
// "mode": "queue", they are typed in one at a time, each once the agent's
// turn before it has ended (server/queue.go delivers them). The queue is
// prompt-queue.json in the data folder (0600), so it survives a restart.
package queue

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// MaxPerAgent is how many prompts one agent can have waiting.
const MaxPerAgent = 20

// ErrFull: the agent already has MaxPerAgent prompts waiting.
var ErrFull = errors.New("this agent already has 20 prompts waiting: cancel some, or wait until they are sent")

// Item is one waiting prompt.
type Item struct {
	ID     string    `json:"id"`      // "q" and 6 characters, unique on this deck
	Agent  string    `json:"agentId"` // the agent it is for
	Text   string    `json:"text"`
	By     string    `json:"by"` // who queued it: "mcp:<token>", a device's name, "web" or "local"
	Queued time.Time `json:"queuedAt"`
}

// Queue is every agent's waiting prompts, in the order they will be sent.
// Path "" keeps it in memory only (demo mode).
type Queue struct {
	Path string

	mu     sync.Mutex
	loaded bool
	items  []Item // per agent, in delivery order
}

// Open reads the queue at path (none yet is an empty queue).
func Open(path string) *Queue {
	q := &Queue{Path: path}
	q.mu.Lock()
	q.load()
	q.mu.Unlock()
	return q
}

func (q *Queue) load() {
	if q.loaded {
		return
	}
	q.loaded = true
	if q.Path == "" {
		return
	}
	b, err := os.ReadFile(q.Path)
	if err != nil {
		return
	}
	var l []Item
	if json.Unmarshal(b, &l) == nil {
		q.items = l
	}
}

func (q *Queue) save() error {
	if q.Path == "" {
		return nil
	}
	l := q.items
	if l == nil {
		l = []Item{}
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := q.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, q.Path)
}

// Add queues text for agent, last (or first, with front). It returns the
// item and its place in the agent's queue (1 = next).
func (q *Queue) Add(agent, text, by string, front bool) (Item, int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	n := 0
	first := -1
	for i, it := range q.items {
		if it.Agent == agent {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	if n >= MaxPerAgent {
		return Item{}, 0, ErrFull
	}
	it := Item{ID: q.newID(), Agent: agent, Text: text, By: by, Queued: time.Now().UTC()}
	pos := n + 1
	switch {
	case front && first >= 0:
		q.items = append(q.items[:first], append([]Item{it}, q.items[first:]...)...)
		pos = 1
	default:
		q.items = append(q.items, it)
	}
	if err := q.save(); err != nil {
		q.items = q.without(it.ID)
		return Item{}, 0, err
	}
	return it, pos, nil
}

// newID is an id no waiting prompt has.
func (q *Queue) newID() string {
	const al = "abcdefghijklmnopqrstuvwxyz0123456789"
	for {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		for i := range b {
			b[i] = al[int(b[i])%len(al)]
		}
		id := "q" + string(b)
		taken := false
		for _, it := range q.items {
			taken = taken || it.ID == id
		}
		if !taken {
			return id
		}
	}
}

func (q *Queue) without(id string) []Item {
	out := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	return out
}

// List is agent's waiting prompts, next first.
func (q *Queue) List(agent string) []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	out := []Item{}
	for _, it := range q.items {
		if it.Agent == agent {
			out = append(out, it)
		}
	}
	return out
}

// Count is how many prompts agent has waiting.
func (q *Queue) Count(agent string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	n := 0
	for _, it := range q.items {
		if it.Agent == agent {
			n++
		}
	}
	return n
}

// Agents is every agent with prompts waiting, with how many.
func (q *Queue) Agents() map[string]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	out := map[string]int{}
	for _, it := range q.items {
		out[it.Agent]++
	}
	return out
}

// Cancel takes one waiting prompt of agent out of the queue.
func (q *Queue) Cancel(agent, id string) (Item, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for _, it := range q.items {
		if it.ID == id && it.Agent == agent {
			old := q.items
			q.items = q.without(id)
			if err := q.save(); err != nil {
				q.items = old
				return Item{}, false, err
			}
			return it, true, nil
		}
	}
	return Item{}, false, nil
}

// Next is agent's next prompt, still in the queue.
func (q *Queue) Next(agent string) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for _, it := range q.items {
		if it.Agent == agent {
			return it, true
		}
	}
	return Item{}, false
}

// Take removes a prompt that is being sent now (Next gave it). It reports
// false when it was cancelled in between.
func (q *Queue) Take(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for _, it := range q.items {
		if it.ID == id {
			q.items = q.without(id)
			_ = q.save()
			return true
		}
	}
	return false
}

// PutBack returns a prompt whose sending failed to the front of its agent's
// queue.
func (q *Queue) PutBack(it Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for i, x := range q.items {
		if x.Agent == it.Agent {
			q.items = append(q.items[:i], append([]Item{it}, q.items[i:]...)...)
			_ = q.save()
			return
		}
	}
	q.items = append(q.items, it)
	_ = q.save()
}

// Drop removes every prompt waiting for agent (it left the deck), and
// returns them.
func (q *Queue) Drop(agent string) []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	var gone []Item
	keep := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.Agent == agent {
			gone = append(gone, it)
		} else {
			keep = append(keep, it)
		}
	}
	if len(gone) > 0 {
		q.items = keep
		_ = q.save()
	}
	return gone
}
