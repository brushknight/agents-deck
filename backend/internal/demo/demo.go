// Package demo serves a fixed, gently animated fleet for UI development.
package demo

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

//go:embed fixture.json
var fixture []byte

type Demo struct{ St *store.Store }

func Load(st *store.Store) (*Demo, error) {
	var s model.State
	if err := json.Unmarshal(fixture, &s); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for i := range s.Agents {
		a := s.Agents[i]
		slot := a.Slot
		e := &store.Entry{A: a}
		st.Add(e)
		st.Update(a.ID, func(e *store.Entry) bool {
			e.A.Slot = slot
			e.A.StartedAt = now.Add(-time.Duration(20+i*7) * time.Minute)
			e.A.StatusSince = now.Add(-time.Duration(1+i*3) * time.Minute)
			e.PromptN = 100
			return true
		})
	}
	return &Demo{St: st}, nil
}

var activities = []model.Activity{
	{Tool: "Read", Detail: "internal/auth/session.go"},
	{Tool: "Edit", Detail: "internal/auth/session.go"},
	{Tool: "Bash", Detail: "go test ./..."},
	{Tool: "Grep", Detail: "refreshToken"},
	{Tool: "Write", Detail: "internal/auth/session_test.go"},
	{Tool: "TodoWrite", Detail: "planning 5 steps"},
}

// Run nudges the fleet every few seconds: activity changes, tokens grow, and
// now and then a running agent asks for permission or finishes.
func (d *Demo) Run() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for range time.Tick(3 * time.Second) {
		d.St.Each(func(e *store.Entry) bool {
			if e.A.Status != model.Running {
				return false
			}
			act := activities[r.Intn(len(activities))]
			e.A.Activity = &act
			grow := int64(2000 + r.Intn(9000))
			e.A.Tokens.Output += grow / 8
			e.A.Tokens.CacheRead += grow * 4
			e.A.Context.Used = min(e.A.Context.Used+grow, e.A.Context.Window)
			e.A.CostUSD += float64(grow) * 0.000004
			switch r.Intn(40) {
			case 0:
				e.SetStatus(model.Waiting)
				e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "permission", Title: "run this command?",
					Detail: "rm -rf node_modules && npm ci", Context: "bash · ~/dev/" + e.A.Folder,
					Options: []model.Option{{Key: "1", Label: "yes", Primary: true}, {Key: "2", Label: "yes, and don't ask again for npm ci"}, {Key: "3", Label: "no"}}}
			case 1:
				e.SetStatus(model.Idle)
				e.A.Unseen = true // finished: hungry until focused
			}
			return true
		})
	}
}

func (d *Demo) Answer(id, promptID, key string) error {
	var err error
	ok := d.St.Update(id, func(e *store.Entry) bool {
		p := e.A.Waiting
		if p == nil || p.ID != promptID {
			err = server.ErrStalePrompt
			return false
		}
		valid := false
		for _, o := range p.Options {
			if o.Key == key {
				valid = true
			}
		}
		if !valid {
			err = server.ErrBadKey
			return false
		}
		if p.Kind == "permission" && key == fmt.Sprint(len(p.Options)) {
			e.SetStatus(model.Idle)
		} else {
			e.SetStatus(model.Running)
			e.A.Activity = &activities[0]
		}
		return true
	})
	if !ok {
		return server.ErrNotFound
	}
	return err
}

func (d *Demo) Focus(id string) error {
	if _, ok := d.St.Get(id); !ok {
		return server.ErrNotFound
	}
	d.St.Each(func(e *store.Entry) bool {
		f := e.A.ID == id
		if e.A.Focused == f && !(f && e.A.Unseen) {
			return false
		}
		e.A.Focused, e.A.Attached = f, e.A.Attached || f
		if f {
			e.A.Unseen = false
		}
		return true
	})
	return nil
}

func (d *Demo) Interrupt(id string) error {
	if !d.St.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Idle); return true }) {
		return server.ErrNotFound
	}
	return nil
}

func (d *Demo) Resume(id string) error {
	if !d.St.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Idle); e.A.Resumable = false; return true }) {
		return server.ErrNotFound
	}
	return nil
}

func (d *Demo) Dismiss(id string) error {
	if !d.St.Remove(id) {
		return server.ErrNotFound
	}
	return nil
}
