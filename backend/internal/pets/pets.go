// Package pets keeps a little pet for every agent on the deck: it hatches
// from an egg, eats the agent's tokens (input plus output, not cache reads),
// grows a level at a time and gets hungry when it is left alone. Nothing here
// asks a model: food is the token count the deck already reads, and the rest
// is arithmetic on a clock. The pets live in pets.json in the data folder,
// and only while Agent pets is on (config.json "pets": true).
package pets

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"os"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The numbers of the game.
const (
	FirstLevel  = 50_000         // food for level 1: the egg hatches
	Growth      = 1.25           // each level needs 1.25 times the food of the one before
	CrackAt     = 30_000         // the egg shows cracks from here (the web draws it from Ate)
	HeartFood   = 20_000         // food that brings back one heart
	MaxHearts   = 4              // a fed pet has all four
	HeartEvery  = 12 * time.Hour // one heart drops per 12 h without food while the agent is idle
	WeakAfter   = 72 * time.Hour // weak: no hearts left and three days without food
	SleepAfter  = 7 * 24 * time.Hour
	forgetAfter = 30 * 24 * time.Hour // a pet whose agent left the deck is dropped after this
)

// Stages and moods, as the API spells them.
const (
	Egg    = "egg"
	Baby   = "baby"
	Kid    = "kid"
	Grown  = "grown"
	Strong = "strong"
	Mighty = "mighty"
	Legend = "legend"

	Fed    = "fed"
	Hungry = "hungry"
	Weak   = "weak"
	Asleep = "asleep" // hibernating
)

// Names a pet may get when it hatches: short and made up.
var Names = []string{"Nib", "Pip", "Bix", "Tup", "Mox", "Zib", "Fen", "Kip", "Lumo", "Tiko",
	"Wobs", "Quill", "Sprig", "Nubs", "Pogo", "Jot", "Fizz", "Tock", "Miso", "Dabs",
	"Rumi", "Pell", "Yuzu", "Bop", "Gus", "Ollo", "Taffy", "Zuzu", "Koda", "Plum"}

// Threshold is the food a pet needs to reach level n (n >= 1).
func Threshold(n int) int64 {
	return int64(math.Round(FirstLevel * math.Pow(Growth, float64(n-1))))
}

// LevelOf is the level for the food eaten so far, and the food that reaches
// the next one.
func LevelOf(ate int64) (level int, next int64) {
	for level < 1000 {
		next = Threshold(level + 1)
		if ate < next {
			return level, next
		}
		level++
	}
	return level, next
}

// StageOf names the stage of a level.
func StageOf(level int) string {
	switch {
	case level < 1:
		return Egg
	case level < 5:
		return Baby
	case level < 10:
		return Kid
	case level < 20:
		return Grown
	case level < 30:
		return Strong
	case level < 40:
		return Mighty
	}
	return Legend
}

// pet is one agent's pet as pets.json keeps it.
type pet struct {
	Name      string    `json:"name,omitempty"`
	HatchedAt time.Time `json:"hatchedAt,omitzero"`
	Ate       int64     `json:"ate"`
	// Session and Seen: the agent's session and its input plus output
	// tokens at the last look. Food is what Seen grew by; a total that
	// drops (a transcript read again after a restart) feeds nothing until
	// it passes Seen again, and a new session starts Seen over.
	Session string `json:"session,omitempty"`
	Seen    int64  `json:"seen"`
	// Hearts after its last meal, and food towards the next heart.
	Hearts int   `json:"hearts"`
	Credit int64 `json:"credit,omitempty"`
	// Starve: how long it has been idle, with nothing asked, since its last
	// meal. It grows only while Ticking, from Clock on.
	Starve  time.Duration `json:"starve"`
	Ticking bool          `json:"ticking,omitempty"`
	Clock   time.Time     `json:"clock"`
	// Last: when its agent was last on the deck.
	Last time.Time `json:"last"`

	shown string // hearts and mood at the last look, to tell screens when they change
}

// Sight is what the book needs to know about an agent at one look.
type Sight struct {
	ID      string
	Session string
	Tokens  model.Tokens
	Status  model.Status
}

// Food counts input and output tokens; cache reads and writes are the same
// context read again, so they don't feed it.
func Food(t model.Tokens) int64 { return t.Input + t.Output }

// ticks: the hunger clock runs while the agent is idle (or ended) and asks
// nothing; it stops while it works and while it waits on you.
func ticks(s model.Status) bool {
	return s != model.Running && s != model.Starting && s != model.Waiting
}

// Book holds every agent's pet.
type Book struct {
	Path    string      // pets.json; "" keeps them in memory (demo, tests)
	Enabled func() bool // Agent pets is on; nil: always on

	mu     sync.Mutex
	pets   map[string]*pet
	loaded bool
	wasOn  bool
	dirty  bool
	now    func() time.Time
}

// Open reads pets.json (best effort) and returns its book.
func Open(path string, enabled func() bool) *Book {
	b := &Book{Path: path, Enabled: enabled}
	b.load()
	return b
}

func (b *Book) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *Book) load() {
	if b.loaded {
		return
	}
	b.loaded, b.pets = true, map[string]*pet{}
	if b.Path == "" {
		return
	}
	if data, err := os.ReadFile(b.Path); err == nil {
		_ = json.Unmarshal(data, &b.pets)
		if b.pets == nil {
			b.pets = map[string]*pet{}
		}
	}
}

// on says whether pets are on, under b.mu. Time with pets off, or with the
// service stopped, doesn't count: when they come on, every clock restarts now.
func (b *Book) on(now time.Time) bool {
	b.load()
	on := b.Enabled == nil || b.Enabled()
	if on && !b.wasOn {
		for _, p := range b.pets {
			p.Clock = now
		}
	}
	b.wasOn = on
	return on
}

// Feed looks at every agent on the deck: food goes to their pets, hunger
// clocks start and stop with their status, and pets of agents gone for a
// month are dropped. It reports whether anything a viewer sees changed.
func (b *Book) Feed(agents []Sight) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	if !b.on(now) {
		return false
	}
	seen := map[string]bool{}
	changed := false
	for _, s := range agents {
		seen[s.ID] = true
		p := b.pets[s.ID]
		if p == nil {
			p = &pet{Hearts: MaxHearts, Session: s.Session, Clock: now, Ticking: ticks(s.Status)}
			b.pets[s.ID], b.dirty = p, true
		}
		p.Last = now
		if p.Ticking {
			p.Starve += now.Sub(p.Clock)
		}
		p.Clock = now
		if t := ticks(s.Status); t != p.Ticking {
			p.Ticking, b.dirty = t, true
		}
		if s.Session != p.Session {
			p.Session, p.Seen = s.Session, 0
		}
		total := Food(s.Tokens)
		if total > p.Seen {
			food := total - p.Seen
			p.Seen = total
			b.eat(s.ID, p, food, now)
			changed = true
		}
		h := hearts(p.Hearts, p.Starve)
		if shown := mood(h, p.Starve) + string(rune('0'+h)); shown != p.shown {
			changed = changed || p.shown != ""
			p.shown = shown
		}
	}
	for id, p := range b.pets {
		if !seen[id] && now.Sub(p.Last) > forgetAfter {
			delete(b.pets, id)
			b.dirty = true
		}
	}
	return changed
}

// eat gives a pet food: any meal wakes it to at least two hearts, every 20k
// tokens bring one more back, and it hatches once it reaches level 1.
func (b *Book) eat(id string, p *pet, food int64, now time.Time) {
	h := max(hearts(p.Hearts, p.Starve), 2)
	p.Credit += food
	for p.Credit >= HeartFood && h < MaxHearts {
		h++
		p.Credit -= HeartFood
	}
	if h >= MaxHearts {
		p.Credit = 0
	}
	p.Hearts, p.Starve, p.Ate = h, 0, p.Ate+food
	if lv, _ := LevelOf(p.Ate); lv >= 1 && p.Name == "" {
		p.Name, p.HatchedAt = b.pickName(id), now.UTC()
	}
	b.dirty = true
}

// pickName picks a name from the agent's id, skipping names other pets have.
func (b *Book) pickName(id string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	start := int(h.Sum32() % uint32(len(Names)))
	taken := map[string]bool{}
	for _, p := range b.pets {
		taken[p.Name] = true
	}
	for i := range Names {
		if n := Names[(start+i)%len(Names)]; !taken[n] {
			return n
		}
	}
	return Names[start]
}

func hearts(after int, starve time.Duration) int {
	return max(0, after-int(starve/HeartEvery))
}

func mood(h int, starve time.Duration) string {
	switch {
	case starve >= SleepAfter:
		return Asleep
	case h == 0 && starve >= WeakAfter:
		return Weak
	case h <= 2:
		return Hungry
	}
	return Fed
}

// View is an agent's pet as the API shows it, nil while pets are off. An
// agent the book hasn't seen yet has a fresh egg.
func (b *Book) View(a model.Agent) *model.Pet {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	if !b.on(now) {
		return nil
	}
	p := b.pets[a.ID]
	if p == nil {
		p = &pet{Hearts: MaxHearts}
	}
	starve := p.Starve
	if p.Ticking && !p.Clock.IsZero() {
		starve += now.Sub(p.Clock)
	}
	h := hearts(p.Hearts, starve)
	lv, next := LevelOf(p.Ate)
	return &model.Pet{Name: p.Name, Stage: StageOf(lv), Level: lv, Ate: p.Ate, NextAt: next,
		Hearts: h, Mood: mood(h, starve), HatchedAt: p.HatchedAt}
}

// Save writes pets.json when something changed since the last save.
func (b *Book) Save() error {
	b.mu.Lock()
	if b.Path == "" || !b.dirty {
		b.mu.Unlock()
		return nil
	}
	data, err := json.MarshalIndent(b.pets, "", " ")
	b.dirty = false
	b.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := b.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.Path)
}

// Follow feeds the pets after every change on the deck (and once a minute,
// so the clocks are saved), saving pets.json a moment later. sub is
// store.Subscribe, look lists the agents. It returns when stop closes.
func (b *Book) Follow(sub func() (chan struct{}, func()), look func() []Sight, touch func(), stop <-chan struct{}) {
	ch, cancel := sub()
	defer cancel()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if b.Feed(look()) && touch != nil {
			touch() // a meal: screens see the new numbers
		}
		_ = b.Save()
		select {
		case <-ch:
		case <-tick.C:
		case <-stop:
			return
		}
	}
}

// Switch reads Agent pets (config.json "pets", off by default), reading the
// file again only when it changes.
func Switch(config string) func() bool {
	var mu sync.Mutex
	var mtime time.Time
	var size int64 = -1
	on := false
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		st, err := os.Stat(config)
		if err != nil {
			mtime, size, on = time.Time{}, -1, false
			return false
		}
		if st.ModTime().Equal(mtime) && st.Size() == size {
			return on
		}
		mtime, size = st.ModTime(), st.Size()
		var c struct {
			Pets bool `json:"pets"`
		}
		data, _ := os.ReadFile(config)
		_ = json.Unmarshal(data, &c)
		on = c.Pets
		return on
	}
}
