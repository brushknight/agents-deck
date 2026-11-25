// Package store holds every agent's state, notifies subscribers on change and
// persists the registry so agents survive a daemon restart.
package store

import (
	"crypto/rand"
	"encoding/json"
	mrand "math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/limits"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Entry is one agent: the wire view plus private bookkeeping.
type Entry struct {
	A              model.Agent `json:"agent"`
	SessionID      string      `json:"sessionId"`  // Claude session uuid
	Transcript     string      `json:"transcript"` // Claude JSONL path, learned from hooks
	TmuxName       string      `json:"tmuxName"`
	Sim            bool        `json:"sim,omitempty"`      // simulated agent (agentctl sim): never resumable, no history
	TitleSet       bool        `json:"titleSet,omitempty"` // the user named it (-t): never auto-renamed
	Account        string      `json:"account,omitempty"`  // the Claude account the deck started it in ("" = see its transcript)
	CustomTitle    string      `json:"-"`                  // Claude's custom-title (/rename), from the transcript
	External       bool        `json:"-"`                  // mirrored from another app (Codex, Orca, herdr, cmux); never persisted
	Background     bool        `json:"-"`                  // shown running only because its subagents work (its main thread is between turns)
	WaitBackground bool        `json:"-"`                  // the open prompt is a subagent's, asked while the main thread was between turns
	WaitAgent      string      `json:"-"`                  // …and that subagent's id
	RunStart       time.Time   `json:"-"`                  // when Claude Code last started or resumed the session (SessionStart)
	CompactToIdle  bool        `json:"-"`                  // the running compaction started between turns: idle again after it
	LiveTTY        string      `json:"-"`                  // a watched hand-started session: its terminal
	LiveAttach     string      `json:"-"`                  // a watched background session: its `claude attach` id
	OrcaTerminal   string      `json:"-"`                  // an agent in Orca: its terminal handle
	OrcaPane       string      `json:"-"`                  // …and Orca's pane key (stable id)
	OrcaTurn       int64       `json:"-"`                  // …stateStartedAt of the state its prompt was built for
	HerdrSocket    string      `json:"-"`                  // an agent in herdr: its session's socket
	HerdrPane      string      `json:"-"`                  // …its pane ("w1:p2"; changes when panes move)
	HerdrTurn      int64       `json:"-"`                  // …state_change_seq of the state its prompt was built for
	CmuxSurface    string      `json:"-"`                  // an agent in cmux: its terminal (surface id)
	CmuxTurn       float64     `json:"-"`                  // …updatedAt of the hook report its prompt was built for

	// The "Needs you" grid (attention.go). AttnHome is the spot it had last
	// (1..16, 0 = none), kept while it is away and across restarts.
	AttnHome    int       `json:"attnHome,omitempty"`
	attnOn      bool      // on the grid now (in a spot, or waiting for one)
	attnJoined  time.Time // when it came on the grid
	attnLeaveAt time.Time // stopped needing you: leaves then (zero: not leaving)

	// Transcript accounting (rebuilt from the file on restart).
	Offset  int64               `json:"-"`
	Usage   map[string]msgUsage `json:"-"` // message id -> last usage seen
	Pending string              `json:"-"` // partial trailing line
	// The reply so far of the turn in progress: becomes the agent's
	// LastAnswer when the turn ends (claude.Tail).
	Answer   string    `json:"-"`
	AnswerID string    `json:"-"` // …the API message it comes from
	AnswerAt time.Time `json:"-"` // …and when it was written
	PromptN  int       `json:"promptN"`
	Menu     bool      `json:"-"`              // current prompt is an arrow-key menu, not numbered
	Form     *Form     `json:"form,omitempty"` // the open question form, walked one step at a time

	// How long it ran on WorkDay (YYYY-MM-DD, this computer's clock), up to
	// its last status change (worked.go).
	WorkDay string        `json:"workDay,omitempty"`
	Worked  time.Duration `json:"worked,omitempty"`
}

type msgUsage struct {
	Model  string
	Tokens model.Tokens
}

// SetUsage records the usage of one API message (re-sent for every content block).
func (e *Entry) SetUsage(id, modelID string, t model.Tokens) {
	if e.Usage == nil {
		e.Usage = map[string]msgUsage{}
	}
	e.Usage[id] = msgUsage{modelID, t}
}

// Totals sums usage across messages; cost needs the per-message model.
func (e *Entry) Totals(cost func(string, model.Tokens) float64) (model.Tokens, float64) {
	var sum model.Tokens
	var usd float64
	for _, u := range e.Usage {
		sum.Input += u.Tokens.Input
		sum.Output += u.Tokens.Output
		sum.CacheRead += u.Tokens.CacheRead
		sum.CacheWrite += u.Tokens.CacheWrite
		usd += cost(u.Model, u.Tokens)
	}
	return sum, usd
}

// NextPromptID returns a fresh id for a new blocking prompt.
func (e *Entry) NextPromptID() string {
	e.PromptN++
	return "p" + itoa(e.PromptN)
}

// DisplayTitle picks the name shown on tiles: a name the user gave (-t), else a
// rename inside Claude (unless it is just the folder name — older launches set
// that), else Claude's own session title, else the folder.
func (e *Entry) DisplayTitle() string {
	switch {
	case e.TitleSet && e.A.Title != "":
		return e.A.Title
	case e.CustomTitle != "" && e.CustomTitle != e.A.Folder:
		return e.CustomTitle
	case e.A.AITitle != "":
		return e.A.AITitle
	case e.A.Folder != "":
		return e.A.Folder
	}
	return e.A.Title
}

// SetStatus changes status, stamping statusSince only on a real change.
func (e *Entry) SetStatus(s model.Status) {
	if e.A.Status != s {
		now := time.Now()
		e.endStretch(now)
		e.A.Status = s
		e.A.StatusSince = now.UTC()
	}
	if s != model.Waiting {
		e.A.Waiting = nil
		e.Form = nil
	}
	if s != model.Error {
		e.A.Error = nil
	}
	if s != model.Running {
		e.A.Activity = nil
		e.Background = false
	}
	if s != model.Waiting {
		e.WaitBackground, e.WaitAgent = false, ""
	}
}

// RunSince is when the agent's current Claude Code process started, as far
// as is known: subagents launched before it belong to an earlier run.
func (e *Entry) RunSince() time.Time {
	if e.RunStart.After(e.A.StartedAt) {
		return e.RunStart
	}
	return e.A.StartedAt
}

// MarkActive records activity at t (a message sent to the agent or received
// from it) when t is newer than what is known. A known time never moves back,
// so re-reading a transcript after a restart can't make an agent look older,
// and nothing stamps "now" for activity it didn't see.
func (e *Entry) MarkActive(t time.Time) bool {
	if t.IsZero() || !t.After(e.A.LastActive) {
		return false
	}
	e.A.LastActive = t.UTC()
	return true
}

// ActiveStep is how far a polled source (an app's status, tmux) must move
// the last-active time before it counts as a change: an agent at work would
// otherwise change, and be sent to every screen, every second.
const ActiveStep = 30 * time.Second

// MarkActiveStep is MarkActive for polled sources: it moves the time only
// in ActiveStep steps.
func (e *Entry) MarkActiveStep(t time.Time) bool {
	if t.Sub(e.A.LastActive) < ActiveStep {
		return false
	}
	return e.MarkActive(t)
}

type Store struct {
	mu      sync.Mutex
	fileMu  sync.Mutex // serialises writes of the registry file
	entries map[string]*Entry
	subs    map[chan struct{}]struct{}
	path    string // "" = no persistence (demo)
	saveReq chan struct{}
	Name    string
	Version string

	// Hat gives each agent its hat by working folder (nil: no hats).
	Hat func(cwd string) *model.Hat
	// Account gives each agent its Claude account, from the account it was
	// started in or its transcript's folder (nil: accounts aren't shown).
	Account func(name, transcript string) *model.Account
	// Project gives each agent its project and the project folder it
	// matched, by working folder, from memory (nil: no projects).
	Project func(cwd string) (*model.Project, string)
	// Queued counts each agent's queued prompts (nil: no queue).
	Queued func() map[string]int
	// Pet gives each agent its pet (nil, or a nil answer: pets are off).
	Pet func(a model.Agent) *model.Pet

	// Slow says which agents are in background mode and by whom
	// (cpuguard.ByGuard or ByYou), and each agent's CPU use while the
	// computer is busy (cpuguard.Slower.Use). nil: neither.
	Slow func() (slowedBy map[string]string, cpu map[string]float64)

	// Limits is each account's usage limits, as the tools last reported
	// them (nil: none are kept).
	Limits func() []model.Limits

	// The "Needs you" grid (attention.go): the clock and the random free
	// spot (tests set both), how long an agent stays after it stops needing
	// you, and the timer for the next one to leave.
	now       func() time.Time
	randn     func(n int) int
	linger    time.Duration
	attnTimer *time.Timer

	gone goneWork // time worked today by agents removed today (worked.go)
}

func New(path, name, version string) *Store {
	s := &Store{entries: map[string]*Entry{}, subs: map[chan struct{}]struct{}{}, path: path,
		saveReq: make(chan struct{}, 1), Name: name, Version: version,
		now: time.Now, randn: mrand.IntN, linger: AttentionLinger}
	if path != "" {
		go s.saver()
	}
	return s
}

// Load restores persisted entries (best effort).
func (s *Store) Load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []*Entry
	if json.Unmarshal(b, &list) != nil {
		return
	}
	s.mu.Lock()
	for _, e := range list {
		e.A.AttentionSlot = 0 // placed again below, at its old spot (AttnHome) when it still needs you
		s.entries[e.A.ID] = e
	}
	s.arrange()
	s.mu.Unlock()
}

func (s *Store) saver() {
	for range s.saveReq {
		time.Sleep(500 * time.Millisecond)
		s.Flush()
	}
}

// Flush writes the registry now (also called on shutdown so a restart right
// after a change doesn't resurrect removed agents).
func (s *Store) Flush() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	list := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if !e.External {
			list = append(list, e)
		}
	}
	b, err := json.MarshalIndent(list, "", " ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// Touch tells every subscriber the state changed (e.g. a hat was picked).
func (s *Store) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed()
}

// changed runs after every change, under the lock: the "Needs you" grid is
// worked out again (every source of status passes here), then subscribers
// hear of it and the registry is saved.
func (s *Store) changed() {
	s.arrange()
	s.notify()
}

func (s *Store) notify() {
	for c := range s.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	if s.path != "" {
		select {
		case s.saveReq <- struct{}{}:
		default:
		}
	}
}

// Subscribe returns a channel that receives a tick after every change.
func (s *Store) Subscribe() (chan struct{}, func()) {
	c := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
	return c, func() {
		s.mu.Lock()
		delete(s.subs, c)
		s.mu.Unlock()
	}
}

// Add registers a new agent in the first free slot.
func (s *Store) Add(e *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	used := map[int]bool{}
	for _, x := range s.entries {
		used[x.A.Slot] = true
	}
	slot := 0
	for used[slot] {
		slot++
	}
	e.A.Slot = slot
	e.A.AttentionSlot, e.attnOn = 0, false // arrange places it, at AttnHome when the caller kept one
	now := time.Now().UTC()
	e.A.StartedAt, e.A.UpdatedAt, e.A.StatusSince = now, now, now
	s.entries[e.A.ID] = e
	s.changed()
}

// Reorder puts the listed agents in slots 0..n-1 in that order; agents not
// listed keep their relative order after them. Unknown ids are ignored.
func (s *Store) Reorder(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	listed := map[string]bool{}
	var order []*Entry
	for _, id := range ids {
		if e, ok := s.entries[id]; ok && !listed[id] {
			listed[id] = true
			order = append(order, e)
		}
	}
	var rest []*Entry
	for id, e := range s.entries {
		if !listed[id] {
			rest = append(rest, e)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].A.Slot < rest[j].A.Slot })
	order = append(order, rest...)
	changed := false
	for i, e := range order {
		if e.A.Slot != i {
			e.A.Slot = i
			changed = true
		}
	}
	if changed {
		s.changed()
	}
}

// MaxSlot bounds board positions (16 pages of the panel's 16 tiles).
const MaxSlot = 255

// Place puts an agent at any board position. Positions can be left empty, so
// the board can be arranged freely; an agent already there swaps into the
// mover's old position. It reports false for an unknown id or a bad slot.
func (s *Store) Place(id string, slot int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || slot < 0 || slot > MaxSlot {
		return false
	}
	if e.A.Slot == slot {
		return true
	}
	for _, x := range s.entries {
		if x != e && x.A.Slot == slot {
			x.A.Slot = e.A.Slot
		}
	}
	e.A.Slot = slot
	s.changed()
	return true
}

func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return false
	}
	now := s.now()
	s.gone.add(now, e.A.Cwd, e.WorkedToday(now))
	delete(s.entries, id)
	s.changed()
	return true
}

// Update mutates one agent under the lock; fn returns false to signal "no change".
func (s *Store) Update(id string, fn func(*Entry) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return false
	}
	if fn(e) {
		e.A.UpdatedAt = time.Now().UTC()
		s.changed()
	}
	return true
}

// Each visits every agent under the lock; fn returns true when it changed something.
func (s *Store) Each(fn func(*Entry) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dirty := false
	for _, e := range s.entries {
		if fn(e) {
			e.A.UpdatedAt = time.Now().UTC()
			dirty = true
		}
	}
	if dirty {
		s.changed()
	}
}

// Entries returns copies of every entry, in slot order.
func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].A.Slot < out[j].A.Slot })
	return out
}

// Get returns a copy of one entry's wire view and private fields.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// FindBySession maps a Claude session id to an agent id.
func (s *Store) FindBySession(sid string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		if e.SessionID == sid {
			return id
		}
	}
	return ""
}

func (s *Store) Snapshot() model.State {
	s.mu.Lock()
	agents := make([]model.Agent, 0, len(s.entries))
	type where struct{ account, transcript string }
	places := make([]where, 0, len(s.entries))
	for _, e := range s.entries {
		agents = append(agents, e.A)
		places = append(places, where{e.Account, e.Transcript})
	}
	hat, account, project, queued, slow, pet, lims := s.Hat, s.Account, s.Project, s.Queued, s.Slow, s.Pet, s.Limits
	s.mu.Unlock()
	var counts map[string]int
	if queued != nil {
		counts = queued()
	}
	var slowedBy map[string]string
	var cpu map[string]float64
	if slow != nil {
		slowedBy, cpu = slow()
	}
	var ls []model.Limits
	if lims != nil {
		ls = lims()
	}
	now := time.Now()
	for i := range agents {
		agents[i].Queued = counts[agents[i].ID]
		by := slowedBy[agents[i].ID]
		agents[i].Slowed, agents[i].SlowedByYou, agents[i].CPU = by != "", by == "you", cpu[agents[i].ID]
		if hat != nil {
			agents[i].Hat = hat(agents[i].Cwd)
		}
		if account != nil && agents[i].Tool == "claude" {
			agents[i].Account = account(places[i].account, places[i].transcript)
		}
		if pet != nil {
			agents[i].Pet = pet(agents[i])
		}
		if project != nil {
			agents[i].Project, agents[i].ProjectFolder = project(agents[i].Cwd)
		}
		agents[i].OutUntil = limits.OutUntil(ls, agents[i], now)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Slot < agents[j].Slot })
	return model.State{
		Server: model.Server{Name: s.Name, Version: s.Version, Time: time.Now().UTC()},
		Agents: agents,
		Limits: ls,
	}
}

// NewID returns a short random id ([a-z0-9]{6}).
func NewID() string {
	const al = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = al[int(b[i])%len(al)]
	}
	return string(b)
}

// NewUUID returns a random RFC 4122 v4 uuid.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hx = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hx[c>>4], hx[c&15])
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
