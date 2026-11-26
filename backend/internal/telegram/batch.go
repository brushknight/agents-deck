package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Batch states, in the order a batch goes through them.
const (
	stateOpen      = "open"      // messages still arriving
	stateReady     = "ready"     // closed, files downloaded, waiting to go to the agent
	stateHeld      = "held"      // the linked agent is gone: waits for a re-link
	stateDelivered = "delivered" // the deck took the prompt
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
		if b.State == stateDelivered {
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
