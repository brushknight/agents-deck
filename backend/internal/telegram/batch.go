package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Batch states, in the order a batch goes through them.
const (
	stateOpen      = "open"      // messages still arriving
	stateReady     = "ready"     // closed, files downloaded, waiting to go to the agent
	stateHeld      = "held"      // the linked agent is gone: waits for a re-link
	stateDelivered = "delivered" // the deck took the prompt
	stateDropped   = "dropped"   // a tombstone: held batches dropped by you (deleted), how many messages
)

// Batch is one burst of messages: one folder in the inbox, one prompt. Its
// batch.json is written before Telegram is told the messages were read, so
// nothing is lost if the service stops.
type Batch struct {
	ID        string       `json:"id"`
	State     string       `json:"state"`
	Opened    time.Time    `json:"opened"`
	Chat      int64        `json:"chat"`
	Messages  []Message    `json:"messages"`
	Files     []FileResult `json:"files,omitempty"`
	Agent     string       `json:"agent,omitempty"` // delivered to, or held for
	Error     string       `json:"error,omitempty"` // why it hasn't gone yet
	Delivered time.Time    `json:"delivered,omitempty"`
	Queued    bool         `json:"queued,omitempty"`   // delivered into the agent's queue (it was busy)
	Position  int          `json:"position,omitempty"` // its place there then (1 = next)
	WasHeld   bool         `json:"wasHeld,omitempty"`  // delivered after being held
	Dropped   int          `json:"dropped,omitempty"`  // a tombstone: how many held messages were dropped then

	dir string
}

// FileResult is one file of a batch after downloading.
type FileResult struct {
	Msg    int    `json:"msg"` // index in Messages
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`  // saved here
	Status string `json:"status"`          // ok, too big, failed
	Size   int64  `json:"size,omitempty"`  // as reported
	Error  string `json:"error,omitempty"` // failed: why
}

// Dir is the batch's folder.
func (b *Batch) Dir() string { return b.dir }

func (b *Batch) save() error {
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(b.dir, "batch.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// newBatch is an empty open batch for a message that arrived at t.
func newBatch(inbox string, t time.Time, first *Message) *Batch {
	id := fmt.Sprintf("%s-%d", t.Format("20060102-150405"), first.ID)
	return &Batch{ID: id, State: stateOpen, Opened: t, Chat: first.Chat.ID,
		dir: filepath.Join(inbox, t.Format("2006-01-02"), id)}
}

// loadBatches reads every batch in the inbox that hasn't been delivered,
// oldest first.
func loadBatches(inbox string) ([]*Batch, error) {
	files, err := filepath.Glob(filepath.Join(inbox, "*", "*", "batch.json"))
	if err != nil {
		return nil, err
	}
	var out []*Batch
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var b Batch
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if b.State == stateDelivered || b.State == stateDropped {
			continue
		}
		b.dir = filepath.Dir(f)
		out = append(out, &b)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Opened.Equal(out[j].Opened) {
			return out[i].Opened.Before(out[j].Opened)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (b *Batch) has(msgID int64) bool {
	for _, m := range b.Messages {
		if m.ID == msgID {
			return true
		}
	}
	return false
}

// Delivery is one delivered batch, for the app's list of recent deliveries.
type Delivery struct {
	At       time.Time `json:"at"`
	Agent    string    `json:"agent"`
	Summary  string    `json:"summary"`            // "4 messages, 3 images"
	Snippet  string    `json:"snippet,omitempty"`  // the start of its first text
	From     string    `json:"from,omitempty"`     // a forward's author, when the first message is one
	Files    []string  `json:"files,omitempty"`    // file names, in order
	Problems []string  `json:"problems,omitempty"` // too big or failed, with names
	Queued   bool      `json:"queued,omitempty"`
	Position int       `json:"position,omitempty"`
	WasHeld  bool      `json:"wasHeld,omitempty"`
	Dropped  int       `json:"dropped,omitempty"` // not a delivery: held messages dropped then (deleted from this computer)
}

// recentDeliveries reads the newest delivered batches in the inbox, newest
// first, at most n. It looks at the newest days first.
func recentDeliveries(inbox string, n int) []Delivery {
	days, _ := filepath.Glob(filepath.Join(inbox, "*"))
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	var got []*Batch
	for _, day := range days {
		files, _ := filepath.Glob(filepath.Join(day, "*", "batch.json"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var b Batch
			if json.Unmarshal(data, &b) == nil && (b.State == stateDelivered || b.State == stateDropped && b.Dropped > 0) {
				got = append(got, &b)
			}
		}
		if len(got) >= n {
			break
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Delivered.After(got[j].Delivered) })
	if len(got) > n {
		got = got[:n]
	}
	out := make([]Delivery, 0, len(got))
	for _, b := range got {
		if b.State == stateDropped {
			out = append(out, Delivery{At: b.Delivered, Dropped: b.Dropped, Summary: "dropped " + plural(b.Dropped, "message")})
			continue
		}
		d := Delivery{At: b.Delivered, Agent: b.Agent, Queued: b.Queued, Position: b.Position, WasHeld: b.WasHeld}
		sum := summarize(b)
		d.Summary, d.Problems = sum.short, sum.list
		for i := range b.Messages {
			if t := clean(b.Messages[i].Text + b.Messages[i].Caption); t != "" {
				d.Snippet = clip(strings.Join(strings.Fields(t), " "), 120)
				break
			}
		}
		if len(b.Messages) > 0 {
			if from, _, ok := forwarded(&b.Messages[0]); ok {
				d.From = from
			}
		}
		for _, f := range b.Files {
			if f.Status == "ok" {
				d.Files = append(d.Files, f.Name)
			}
		}
		out = append(out, d)
	}
	return out
}
