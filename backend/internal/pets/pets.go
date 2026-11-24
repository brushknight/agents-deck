// Package pets keeps a little pet for every agent on the deck: it hatches
// from an egg, eats the agent's tokens (input plus output, not cache reads),
// grows a level at a time and gets hungry when it is left alone. Nothing here
// asks a model: food is the token count the deck already reads, and the rest
// is arithmetic on a clock. An egg that has eaten enough waits, ready, until
// someone opens its agent and sees it hatch (Hatch). Pets never spend
// tokens. They live in pets.json in the data folder, and only while Agent
// pets is on: it is on unless config.json says "pets": {"on": false} (or
// the older "pets": false).
package pets

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// The numbers of the game.
const (
	FirstLevel  = 50_000         // food for level 1 by default: the egg is ready to hatch
	Growth      = 1.25           // by default each level needs 1.25 times the food of the one before
	HatAt       = 5              // the mystery hat shows from this level on (a kid)
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

// PetHats are the hats a pet may find when it hatches: the deck's hat shapes
// plus one only pets wear. The project hats (hats.Shapes) don't change.
var PetHats = append(append([]string{}, hats.Shapes...), "devil horns")

// ErrNotReady is Hatch on a pet that isn't an egg waiting to hatch.
var ErrNotReady = errors.New("this pet isn't an egg ready to hatch")

// Config is Agent pets in config.json: "pets": {"on": true, "firstLevel":
// 50000, "step": 1.25}. The older "pets": true is on with the defaults.
type Config struct {
	On         bool    `json:"on"`
	FirstLevel int64   `json:"firstLevel"` // tokens for level 1 (the egg is ready to hatch)
	Step       float64 `json:"step"`       // each level needs this many times the tokens of the one before
}

// The limits of the level settings.
const (
	MinFirstLevel = 1_000
	MaxFirstLevel = 100_000_000
	MinStep       = 1.05
	MaxStep       = 3.0
)

// AlwaysOn: Agent pets can't be turned off for now. The setting is still
// read and written ("pets": {"on": false} stays in config.json), but the
// effective value is on; false brings the switch back.
const AlwaysOn = true

// Defaults is Agent pets on (it is opt-out), with the default levels.
func Defaults() Config { return Config{On: true, FirstLevel: FirstLevel, Step: Growth} }

// ConfigOf reads config.json's "pets" (true, false, an object or nothing:
// on unless it says off); a level setting outside the limits falls back to
// its default. While AlwaysOn, On is always true: Stored reads what the
// file says.
func ConfigOf(v any) Config {
	c := Stored(v)
	if AlwaysOn {
		c.On = true
	}
	return c
}

// Stored is ConfigOf as config.json has it, "on" as written.
func Stored(v any) Config {
	c := Defaults()
	switch v := v.(type) {
	case bool:
		c.On = v
	case map[string]any:
		if on, ok := v["on"].(bool); ok {
			c.On = on
		}
		if n, ok := v["firstLevel"].(float64); ok && n >= MinFirstLevel && n <= MaxFirstLevel {
			c.FirstLevel = int64(n)
		}
		if n, ok := v["step"].(float64); ok && n >= MinStep && n <= MaxStep {
			c.Step = n
		}
	}
	return c
}

// Check says what is wrong with the level settings, if anything.
func (c Config) Check() error {
	if c.FirstLevel < MinFirstLevel || c.FirstLevel > MaxFirstLevel {
		return fmt.Errorf("firstLevel must be %d to %d tokens", MinFirstLevel, MaxFirstLevel)
	}
	if math.IsNaN(c.Step) || c.Step < MinStep || c.Step > MaxStep {
		return fmt.Errorf("step must be %g to %g", MinStep, MaxStep)
	}
	return nil
}

// Threshold is the food a pet needs to reach level n; level 0 starts at 0.
func (c Config) Threshold(n int) int64 {
	if n < 1 {
		return 0
	}
	return int64(math.Round(float64(c.FirstLevel) * math.Pow(c.Step, float64(n-1))))
}

// LevelOf is the level for the food eaten so far, and the food that reaches
// the next one.
func (c Config) LevelOf(ate int64) (level int, next int64) {
	for level < 1000 {
		next = c.Threshold(level + 1)
		if ate < next {
			return level, next
		}
		level++
	}
	return level, next
}

// Threshold is Config.Threshold with the default levels.
func Threshold(n int) int64 { return Defaults().Threshold(n) }

// LevelOf is Config.LevelOf with the default levels.
func LevelOf(ate int64) (level int, next int64) { return Defaults().LevelOf(ate) }

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
	Name      string    `json:"name,omitempty"` // "" while an egg
	HatchedAt time.Time `json:"hatchedAt,omitzero"`
	Ate       int64     `json:"ate"`
	// Level is the highest level it reached, so a change of the level
	// settings never takes one away. Hat is the mystery hat it hatched with.
	Level int        `json:"level,omitempty"`
	Hat   *model.Hat `json:"hat,omitempty"`
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
	Path   string        // pets.json; "" keeps them in memory (demo, tests)
	Config func() Config // Agent pets and its levels; nil: on, with the default levels

	mu     sync.Mutex
	pets   map[string]*pet
	loaded bool
	wasOn  bool
	dirty  bool
	cfg    Config // as of the last look, under mu
	now    func() time.Time
}

// Open reads pets.json (best effort) and returns its book.
func Open(path string, config func() Config) *Book {
	b := &Book{Path: path, Config: config}
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

// on reads the settings and says whether pets are on, under b.mu. Time with
// pets off, or with the service stopped, doesn't count: when they come on,
// every clock restarts now.
func (b *Book) on(now time.Time) bool {
	b.load()
	b.cfg = Defaults()
	if b.Config != nil {
		b.cfg = b.Config()
	}
	on := b.cfg.On
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
			b.eat(s.ID, p, food)
			changed = true
		} else if p.Name != "" && p.Hat == nil {
			b.grow(s.ID, p) // hatched before mystery hats
			b.dirty = true
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
// tokens bring one more back, and a hatched pet grows. An egg doesn't hatch
// by itself: it gets ready and waits for Hatch, eating all the while.
func (b *Book) eat(id string, p *pet, food int64) {
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
	if p.Name != "" {
		b.grow(id, p)
	}
	b.dirty = true
}

// grow raises a hatched pet's level to what it has eaten (never lower), and
// gives a pet that hatched before mystery hats its hat.
func (b *Book) grow(id string, p *pet) {
	lv, _ := b.cfg.LevelOf(p.Ate)
	p.Level = max(p.Level, lv, 1)
	if p.Hat == nil {
		p.Hat = mysteryHat(id)
	}
}

// ready: an egg that has eaten enough for level 1.
func (b *Book) ready(p *pet) bool {
	return p.Name == "" && p.Ate >= b.cfg.Threshold(1)
}

// Hatch hatches an agent's egg that is ready, once someone has seen it: it
// gets its name, its level and its mystery hat, and pets.json is saved.
// ErrNotReady when there is no such egg, or while pets are off.
func (b *Book) Hatch(id string) error {
	b.mu.Lock()
	now := b.clock()
	p := b.pets[id]
	if !b.on(now) || p == nil || !b.ready(p) {
		b.mu.Unlock()
		return ErrNotReady
	}
	p.Name, p.HatchedAt = b.pickName(id), now.UTC()
	b.grow(id, p)
	b.dirty = true
	b.mu.Unlock()
	return b.Save()
}

// mysteryHat rolls a pet's hat from its agent's id, once, when it hatches.
func mysteryHat(id string) *model.Hat {
	h := fnv.New32a()
	_, _ = h.Write([]byte("pet hat " + id))
	v := h.Sum32()
	return &model.Hat{Shape: PetHats[v%uint32(len(PetHats))], Color: hats.Colors[(v>>8)%uint32(len(hats.Colors))]}
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
	v := &model.Pet{Name: p.Name, Ate: p.Ate, Hearts: h, Mood: mood(h, starve), HatchedAt: p.HatchedAt}
	if p.Name == "" {
		v.NextAt, v.Ready = b.cfg.Threshold(1), b.ready(p)
	} else {
		lv, _ := b.cfg.LevelOf(p.Ate)
		v.Level = max(p.Level, lv, 1)
		v.LevelFrom, v.NextAt = b.cfg.Threshold(v.Level), b.cfg.Threshold(v.Level+1)
		if v.Level >= HatAt && p.Hat != nil {
			v.Hat = &model.PetHat{Shape: p.Hat.Shape, Color: p.Hat.Color}
		}
	}
	v.Stage = StageOf(v.Level)
	return v
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

// Settings reads Agent pets (config.json "pets", on by default), reading
// the file again only when it changes.
func Settings(config string) func() Config {
	var mu sync.Mutex
	var mtime time.Time
	var size int64 = -1
	c := Defaults()
	return func() Config {
		mu.Lock()
		defer mu.Unlock()
		st, err := os.Stat(config)
		if err != nil {
			mtime, size, c = time.Time{}, -1, Defaults()
			return c
		}
		if st.ModTime().Equal(mtime) && st.Size() == size {
			return c
		}
		mtime, size = st.ModTime(), st.Size()
		var f map[string]any
		data, _ := os.ReadFile(config)
		_ = json.Unmarshal(data, &f)
		c = ConfigOf(f["pets"])
		return c
	}
}
