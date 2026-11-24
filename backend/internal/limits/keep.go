package limits

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The Book on disk (limits.json), so the numbers survive a restart of the
// service: they would otherwise be gone until an agent in each account runs
// again. Windows past their reset still show as reset when read back.

const keepVersion = 1

type kept struct {
	Version int       `json:"version"`
	Sets    []keptSet `json:"sets"`
}

type keptSet struct {
	Tool    string              `json:"tool"`
	Place   string              `json:"place"`
	Plan    string              `json:"plan,omitempty"`
	Source  string              `json:"source,omitempty"`
	AsOf    time.Time           `json:"asOf"`
	Windows []model.LimitWindow `json:"windows"`
}

// Marshal is the Book as limits.json holds it, in a stable order.
func (b *Book) Marshal() ([]byte, error) {
	b.mu.Lock()
	k := kept{Version: keepVersion}
	for key, s := range b.sets {
		ks := keptSet{Tool: key.tool, Place: key.place, Plan: s.plan, Source: s.source, AsOf: s.asOf}
		for _, w := range s.win {
			ks.Windows = append(ks.Windows, w)
		}
		sortWindows(ks.Windows)
		k.Sets = append(k.Sets, ks)
	}
	b.mu.Unlock()
	sortSets(k.Sets)
	return json.MarshalIndent(k, "", "  ")
}

// Load fills an empty Book from limits.json. A file from another version, or
// one that doesn't parse, is ignored: the numbers come back with the next
// report anyway.
func (b *Book) Load(data []byte) error {
	var k kept
	if err := json.Unmarshal(data, &k); err != nil {
		return err
	}
	if k.Version != keepVersion {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sets == nil {
		b.sets = map[key]*set{}
	}
	for _, ks := range k.Sets {
		if ks.Tool == "" {
			continue
		}
		s := &set{plan: ks.Plan, source: ks.Source, asOf: ks.AsOf, win: map[string]model.LimitWindow{}}
		for _, w := range ks.Windows {
			if w.ID == "" {
				continue
			}
			// Level, Reset and Hidden are worked out again when shown.
			w.Level, w.Reset, w.Hidden = "", false, false
			s.win[w.ID] = w
		}
		b.sets[key{ks.Tool, ks.Place}] = s
	}
	return nil
}

// Keeper writes the Book to its file when it changed since the last write.
type Keeper struct {
	Book *Book
	Path string

	last []byte
}

// Load reads the file into the Book (a missing file is fine).
func (k *Keeper) Load() error {
	data, err := os.ReadFile(k.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := k.Book.Load(data); err != nil {
		return err
	}
	k.last, _ = k.Book.Marshal()
	return nil
}

// Save writes the Book when it changed, through a temporary file so a crash
// never leaves half a file.
func (k *Keeper) Save() error {
	data, err := k.Book.Marshal()
	if err != nil || bytes.Equal(data, k.last) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(k.Path), ".limits-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), k.Path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	k.last = data
	return nil
}
