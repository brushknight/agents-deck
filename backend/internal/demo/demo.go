// Package demo serves a fixed, gently animated fleet for UI development.
package demo

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

//go:embed fixture.json
var fixture []byte

type Demo struct {
	St *store.Store
	// Still keeps the fleet as the fixture has it: no activity, no status
	// changes, and each agent's last activity stays the same time ago (tests
	// that check the order by activity).
	Still bool
	// Steady keeps everything on the deck: working agents go on working
	// (their activity line changes, their context grows), but nobody
	// finishes, nobody starts asking and no subagent leaves by itself. A
	// waiting agent waits until a screen answers it. Set by a dataset file
	// ("steady": true).
	Steady bool
	// Name is the deck name a dataset file asks for ("" = the usual one).
	Name string

	ago  map[string]time.Duration    // by agent: how long ago it was last active when the fleet loaded
	hats map[string]model.Hat        // by working folder: the hat a dataset file gives it
	acts map[string][]model.Activity // by agent: what it does while it works, in order
	subs map[string][]string         // by subagent ("agent/subagent"): the tools it goes through

	mu   sync.Mutex
	step map[string]int // by agent or subagent: where it is in its list
}

// Load fills st with the built-in fleet (fixture.json).
func Load(st *store.Store) (*Demo, error) {
	var f File
	if err := json.Unmarshal(fixture, &f); err != nil {
		return nil, err
	}
	d := &Demo{St: st, ago: map[string]time.Duration{}}
	for id, ago := range lastActive {
		d.ago[id] = ago
	}
	d.fill(f)
	d.hats = nil // the built-in fleet wears the usual hats, by folder
	return d, nil
}

// File is a dataset: a fleet in the shape of GET /v1/state (docs/protocol.md),
// with a few more keys that say how the demo plays it.
type File struct {
	Server model.Server `json:"server"` // only the name is used
	Steady bool         `json:"steady"` // see Demo.Steady
	Agents []FileAgent  `json:"agents"`
}

// FileAgent is one agent of a dataset: an Agent as the API sends it, plus:
//
//   - "hat": the hat it wears (a hat picked by hand on a screen still wins);
//   - "lastActiveAgo", "statusAgo", "startedAgo": how long ago, counted from
//     the moment the service starts ("40s", "5m", "3h");
//   - "activities": the tool lines it goes through while it works, in order,
//     again and again;
//   - "subagents[].tools": the tools that subagent goes through.
type FileAgent struct {
	model.Agent
	Subagents     []FileSubagent   `json:"subagents"`
	LastActiveAgo string           `json:"lastActiveAgo"`
	StatusAgo     string           `json:"statusAgo"`
	StartedAgo    string           `json:"startedAgo"`
	Activities    []model.Activity `json:"activities"`
}

// FileSubagent is a subagent of a dataset.
type FileSubagent struct {
	model.Subagent
	Tools []string `json:"tools"`
}

// LoadFile fills st with the fleet of the dataset at path.
func LoadFile(st *store.Store, path string) (*Demo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // a misspelled key would be dropped without a word
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d := &Demo{St: st, Steady: f.Steady, Name: f.Server.Name, ago: map[string]time.Duration{}}
	d.fill(f)
	return d, nil
}

// check finds what would show up broken on a screen or a board.
func (f File) check() error {
	if len(f.Agents) == 0 {
		return fmt.Errorf("no agents")
	}
	seen := map[string]bool{}
	for i, a := range f.Agents {
		who := fmt.Sprintf("agent %d (%s)", i+1, a.ID)
		switch {
		case a.ID == "" || len(a.ID) > 12:
			return fmt.Errorf("%s: an id of 1 to 12 characters is needed", who)
		case seen[a.ID]:
			return fmt.Errorf("%s: the id is used twice", who)
		}
		seen[a.ID] = true
		switch a.Status {
		case model.Starting, model.Running, model.Waiting, model.Idle, model.Error, model.Exited:
		default:
			return fmt.Errorf("%s: unknown status %q", who, a.Status)
		}
		if (a.Status == model.Waiting) != (a.Waiting != nil) {
			return fmt.Errorf("%s: a waiting agent needs \"waiting\" (its prompt), and only a waiting agent has one", who)
		}
		if a.Waiting != nil && (a.Waiting.ID == "" || len(a.Waiting.Options) == 0) {
			return fmt.Errorf("%s: its prompt needs an id and options", who)
		}
		if a.Hat != nil {
			if a.Hat.Shape == hats.None || a.Hat.Color == "" {
				return fmt.Errorf("%s: a hat needs a shape and a colour", who)
			}
			if err := hats.Validate(a.Hat.Shape, a.Hat.Color); err != nil {
				return fmt.Errorf("%s: %w", who, err)
			}
		}
		for _, v := range []string{a.LastActiveAgo, a.StatusAgo, a.StartedAgo} {
			if _, err := parseAgo(v); err != nil {
				return fmt.Errorf("%s: %w", who, err)
			}
		}
		subs := map[string]bool{}
		for _, s := range a.Subagents {
			if s.ID == "" || subs[s.ID] {
				return fmt.Errorf("%s: every subagent needs an id of its own", who)
			}
			subs[s.ID] = true
		}
	}
	return nil
}

func parseAgo(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a time ago (40s, 5m, 3h)", v)
	}
	return d, nil
}

// fill puts the agents of f on the deck. Times are counted back from now.
func (d *Demo) fill(f File) {
	d.hats, d.acts, d.subs, d.step = map[string]model.Hat{}, map[string][]model.Activity{}, map[string][]string{}, map[string]int{}
	now := time.Now().UTC()
	for i, fa := range f.Agents {
		a := fa.Agent
		a.Subagents = nil
		for _, s := range fa.Subagents {
			a.Subagents = append(a.Subagents, s.Subagent)
			if len(s.Tools) > 0 {
				d.subs[a.ID+"/"+s.ID] = s.Tools
			}
		}
		if a.Hat != nil && a.Cwd != "" {
			d.hats[hats.Clean(a.Cwd)] = *a.Hat
		}
		if len(fa.Activities) > 0 {
			d.acts[a.ID] = fa.Activities
		}
		started, since := time.Duration(20+i*7)*time.Minute, time.Duration(1+i*3)*time.Minute
		if v, _ := parseAgo(fa.StartedAgo); v > 0 {
			started = v
		}
		if v, _ := parseAgo(fa.StatusAgo); v > 0 {
			since = v
		}
		if v, _ := parseAgo(fa.LastActiveAgo); v > 0 {
			d.ago[a.ID] = v
		}
		slot := a.Slot
		d.St.Add(&store.Entry{A: a})
		d.St.Update(a.ID, func(e *store.Entry) bool {
			e.A.Slot = slot
			e.A.StartedAt = now.Add(-started)
			e.A.StatusSince = now.Add(-since)
			e.PromptN = 100
			if ago, ok := d.ago[a.ID]; ok {
				e.A.LastActive = now.Add(-ago)
			}
			return true
		})
	}
}

// Hat is the hat a dataset file gives the agents working in cwd (nil: it
// names none, so the usual hat applies).
func (d *Demo) Hat(cwd string) *model.Hat {
	h, ok := d.hats[hats.Clean(cwd)]
	if !ok {
		return nil
	}
	return &h
}

// lastActive is how long ago each demo agent last sent or got a message
// (one is left unknown, as a new agent's would be).
var lastActive = map[string]time.Duration{
	"k3f9a2": 40 * time.Second, "h0m3l4": 4 * time.Minute, "d0cs1t": 3 * time.Hour, "t3st5a": 2 * time.Minute,
	"1nfr4t": 50 * time.Minute, "k8sn0d": time.Minute, "4uthfl": 6 * time.Minute, "h4ptic": 26 * time.Hour,
	"plug1n": 3 * time.Minute, "fw0t4x": 90 * time.Second,
	"r3l34s": 5 * time.Hour, "cl34nx": 2 * 24 * time.Hour,
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
		if d.Still {
			d.holdStill(time.Now().UTC())
			continue
		}
		if d.Steady {
			d.steadyTick(r, time.Now().UTC())
			continue
		}
		d.St.Each(func(e *store.Entry) bool {
			if e.A.Status != model.Running {
				return false
			}
			act := activities[r.Intn(len(activities))]
			e.A.Activity = &act
			e.MarkActive(time.Now().UTC())
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

// steadyTick is one step of a steady fleet: some working agents move to
// their next tool line and use a little more context, some subagents change
// tool. Nothing else changes, so nothing leaves the deck or an open card.
func (d *Demo) steadyTick(r *rand.Rand, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.St.Each(func(e *store.Entry) bool {
		if e.A.Status != model.Running {
			return false
		}
		changed := false
		if e.A.Activity == nil || r.Intn(5) < 3 {
			act := d.next(e.A.ID, r)
			e.A.Activity = &act
			e.MarkActive(now)
			grow := int64(400 + r.Intn(2000))
			e.A.Tokens.Output += grow / 8
			e.A.Tokens.CacheRead += grow * 4
			// Stops short of full: a long shoot never ends on "100% ctx".
			e.A.Context.Used = max(e.A.Context.Used, min(e.A.Context.Used+grow, e.A.Context.Window*9/10))
			e.A.CostUSD += float64(grow) * 0.000004
			changed = true
		}
		// A fresh list each time: the one sent out already is not touched.
		subs := slices.Clone(e.A.Subagents)
		for i := range subs {
			key := e.A.ID + "/" + subs[i].ID
			tools := d.subs[key]
			if len(tools) < 2 || r.Intn(5) != 0 {
				continue
			}
			d.step[key]++
			subs[i].Tool = tools[d.step[key]%len(tools)]
			changed = true
		}
		e.A.Subagents = subs
		return changed
	})
}

// next is the agent's next tool line: its own list in order, else any of
// the usual ones.
func (d *Demo) next(id string, r *rand.Rand) model.Activity {
	own := d.acts[id]
	if len(own) == 0 {
		return activities[r.Intn(len(activities))]
	}
	act := own[d.step[id]%len(own)]
	d.step[id]++
	return act
}

// holdStill sets every agent's last activity to the fixture's time ago,
// counted from now.
func (d *Demo) holdStill(now time.Time) {
	d.St.Each(func(e *store.Entry) bool {
		ago, ok := d.ago[e.A.ID]
		if !ok {
			return false
		}
		e.A.LastActive = now.Add(-ago)
		return true
	})
}

// Answer takes an answer from a screen. "No" to a permission prompt stops
// the agent (idle); any other answer sends it back to work.
func (d *Demo) Answer(id, promptID, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
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
			act := activities[0]
			if own := d.acts[id]; len(own) > 0 { // back to its own work, from the top
				act, d.step[id] = own[0], 1
			}
			e.A.Activity = &act
			e.MarkActive(time.Now().UTC())
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
	if !d.St.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Idle); e.A.Subagents = nil; return true }) {
		return server.ErrNotFound
	}
	return nil
}

// Type pretends the agent's own queue took the prompt: a working agent
// keeps working, an idle one starts.
func (d *Demo) Type(id, text string) error {
	if e, ok := d.St.Get(id); ok && e.A.Status == model.Running {
		d.St.Update(id, func(e *store.Entry) bool { e.A.LastPrompt = text; return true })
		return nil
	}
	return d.Prompt(id, text, false)
}

// Prompt pretends the agent took a new task: it starts running on it.
func (d *Demo) Prompt(id, text string, interrupt bool) error {
	e, ok := d.St.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	switch {
	case e.A.Status == model.Exited:
		return server.ErrExited
	case e.A.External && e.A.App == "":
		return fmt.Errorf("%w: this session runs in another terminal", server.ErrReadOnly)
	case e.A.Status == model.Waiting && !interrupt:
		return server.ErrAsking
	case e.A.Status == model.Running && !interrupt:
		return server.ErrBusy
	}
	d.St.Update(id, func(e *store.Entry) bool {
		e.SetStatus(model.Running)
		e.A.Waiting, e.A.LastPrompt = nil, text
		return true
	})
	return nil
}

func (d *Demo) Resume(id string) error {
	if !d.St.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Idle); e.A.Resumable = false; return true }) {
		return server.ErrNotFound
	}
	return nil
}

// RestoreJSON brings back the demo agents lost with the tmux server: the
// ones listed, or all of them (POST /v1/restore).
func (d *Demo) RestoreJSON(ids []string) any {
	type result struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Action string `json:"action"`
		Error  string `json:"error,omitempty"`
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := []result{}
	for _, e := range d.St.Entries() {
		if len(ids) > 0 && !want[e.A.ID] {
			continue
		}
		delete(want, e.A.ID)
		if !e.A.Lost {
			if len(ids) > 0 {
				out = append(out, result{ID: e.A.ID, Title: e.A.Title, Action: "failed", Error: "not lost with the tmux server"})
			}
			continue
		}
		d.St.Update(e.A.ID, func(x *store.Entry) bool {
			x.SetStatus(model.Idle)
			x.A.Lost, x.A.Resumable = false, false
			return true
		})
		out = append(out, result{ID: e.A.ID, Title: e.A.Title, Action: "resumed"})
	}
	for _, id := range ids {
		if want[id] {
			out = append(out, result{ID: id, Action: "failed", Error: "no such agent"})
		}
	}
	return out
}

func (d *Demo) Dismiss(id string) error {
	if !d.St.Remove(id) {
		return server.ErrNotFound
	}
	return nil
}
