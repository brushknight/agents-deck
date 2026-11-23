package cpuguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Slowing the heaviest agent while the computer is busy (macOS only): every
// process on the agent's terminal goes in background mode (efficiency
// cores, slower disk) until the computer is calm. Only agents whose
// terminal the deck knows (proc.AgentTerminal): never another program,
// never the deck's own processes. Never a nice value, a stop signal or a
// kill.
const (
	SlowEvery = 30 * time.Second // look again, and slow the processes the agent started since
	SlowCap   = 3                // the guard slows at most this many agents in one busy spell
	SlowFrom  = 5.0              // an agent using less of the CPU (percent of the whole computer) isn't worth slowing
)

// Errors of SlowNow and FullSpeed.
var (
	ErrCantSlow   = errors.New("background mode is macOS only")
	ErrNotBusy    = errors.New("the computer isn't busy")
	ErrNoTerminal = errors.New("the deck doesn't know this agent's terminal (an agent in another app or a background session), or it has ended")
	ErrNotSlowed  = errors.New("this agent isn't slowed")
)

// Priority puts a process in background mode, or takes it out.
type Priority interface {
	Background(pid int, on bool) error
}

// Spell is the guard as the slower sees it (*Guard).
type Spell interface {
	Busy() bool
	Setting() Setting
}

// Slower slows the heaviest agents while the guard is busy, and undoes it.
type Slower struct {
	Guard  Spell
	Agents func() []proc.AgentTerminal // agents with a terminal the deck knows
	List   func() ([]proc.PS, error)   // every process (proc.Processes)
	Prio   Priority                    // nil: background mode on macOS, nothing elsewhere
	// File records every slowed process (pid and start time), so a start
	// after a crash puts them back to full speed (Recover). "": not kept.
	File    string
	Self    int // this service's pid: never slowed, nor its children
	Cores   int // 0: runtime.NumCPU
	Now     func() time.Time
	Logf    func(format string, args ...any)
	Changed func() // what is slowed, or an agent's CPU use, changed

	mu       sync.Mutex
	busy     bool
	stopped  bool
	next     time.Time          // the next look while busy
	picks    int                // agents the guard slowed this busy spell
	lastPick time.Time          // when it slowed the last one
	slowed   map[string]*slowed // by agent id
	full     map[string]bool    // Full speed for the rest of this busy spell
	cpu      map[string]float64 // each agent's CPU use at the last look, while busy
	left     map[int]string     // slowed processes not undone yet (ps failed): pid to start
	listErr  string
	saved    string
}

type slowed struct {
	title  string
	byHand bool
	pids   map[int]string // pid to its start time
}

// view is one look at the processes.
type view struct {
	agents map[string]proc.AgentTerminal
	procs  map[string][]proc.PS // each agent's processes, the deck's left out
	cpu    map[string]float64
	alive  map[int]string // every process: pid to start time
}

func (s *Slower) can() bool { return s.Prio != nil || CanSlow }

func (s *Slower) prio() Priority {
	if s.Prio != nil {
		return s.Prio
	}
	return osPriority{}
}

func (s *Slower) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Slower) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf("cpu guard: "+format, args...)
	}
}

func (s *Slower) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

// look lists the agents and every process, outside the lock.
func (s *Slower) look() (*view, error) {
	var agents []proc.AgentTerminal
	if s.Agents != nil {
		agents = s.Agents()
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	cores := s.Cores
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	v := &view{agents: map[string]proc.AgentTerminal{}, procs: map[string][]proc.PS{}, cpu: map[string]float64{}, alive: map[int]string{}}
	byTTY := map[string]proc.AgentTerminal{}
	for _, a := range agents {
		byTTY[strings.TrimPrefix(a.TTY, "/dev/")] = a
		v.agents[a.ID] = a
		v.cpu[a.ID] = 0
	}
	for _, p := range list {
		v.alive[p.PID] = p.Start
		a, ok := byTTY[p.TTY]
		if !ok || p.PID <= 1 || proc.IsDeck(p.PID, p.PPID, p.Comm, s.Self) {
			continue
		}
		v.procs[a.ID] = append(v.procs[a.ID], p)
		v.cpu[a.ID] += p.CPU / float64(cores)
	}
	for id, c := range v.cpu {
		v.cpu[id] = math.Round(c*10) / 10
	}
	return v, nil
}

// Run looks every 5 s until ctx ends, as the guard does.
func (s *Slower) Run(ctx context.Context) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Step()
		}
	}
}

// Step follows the guard: on entering busy, and every 30 s while busy, it
// looks at the agents' CPU use, slows the next agent when it is time, and
// puts each slowed agent's new processes in background mode too. On
// leaving busy it undoes it all.
func (s *Slower) Step() {
	busy, set, now := s.Guard.Busy(), s.Guard.Setting(), s.now()
	s.mu.Lock()
	switch {
	case s.stopped:
		s.mu.Unlock()
		return
	case !busy:
		was, left := s.busy, len(s.left) > 0
		s.busy = false
		s.mu.Unlock()
		if was || left {
			s.undoAll(fmt.Sprintf("the %s is calm", Machine()))
		}
		return
	}
	s.startSpell(now)
	due := !now.Before(s.next)
	if due {
		s.next = now.Add(SlowEvery)
	}
	s.mu.Unlock()
	if due {
		s.apply(set, now)
	}
}

// startSpell begins a busy spell: nobody is at Full speed, nothing picked.
func (s *Slower) startSpell(now time.Time) {
	if !s.busy {
		s.busy, s.next, s.picks, s.full = true, now, 0, map[string]bool{}
	}
	if s.slowed == nil {
		s.slowed = map[string]*slowed{}
	}
}

func (s *Slower) apply(set Setting, now time.Time) {
	v, err := s.look()
	s.mu.Lock()
	if err != nil {
		if e := err.Error(); e != s.listErr {
			s.listErr = e
			s.mu.Unlock()
			s.logf("can't list processes: %v", err)
			return
		}
		s.mu.Unlock()
		return
	}
	s.listErr = ""
	if !s.busy || s.stopped { // calm, or stopping, while we looked
		s.mu.Unlock()
		return
	}
	var msgs []string
	s.cpu = v.cpu
	s.undoLeft(v)
	for id, sa := range s.slowed {
		if _, ok := v.agents[id]; !ok {
			s.undo(id, v)
			msgs = append(msgs, fmt.Sprintf("%q ended: background mode undone", sa.title))
		}
	}
	if !set.Slow {
		for id, sa := range s.slowed {
			if !sa.byHand {
				s.undo(id, v)
				msgs = append(msgs, fmt.Sprintf("%q back to full speed (Slow is off)", sa.title))
			}
		}
	} else if id := s.pick(set, now, v); id != "" && s.can() {
		s.picks, s.lastPick = s.picks+1, now
		s.slowed[id] = &slowed{title: v.agents[id].Title, pids: map[int]string{}}
		msgs = append(msgs, fmt.Sprintf("background mode for %q (%.0f%% of the CPU): efficiency cores and slower disk until the %s is calm",
			v.agents[id].Title, v.cpu[id], Machine()))
	}
	for id, sa := range s.slowed {
		s.background(sa, v.procs[id], v)
	}
	s.save()
	s.mu.Unlock()
	for _, m := range msgs {
		s.logf("%s", m)
	}
	s.changed()
}

// pick is the next agent to slow, or "": the heaviest one not slowed or at
// Full speed, using at least SlowFrom; the first on entering busy, then one
// more each window the computer stays busy, SlowCap in all.
func (s *Slower) pick(set Setting, now time.Time, v *view) string {
	if s.picks >= SlowCap || (s.picks > 0 && now.Sub(s.lastPick) < set.Window()) {
		return ""
	}
	best := ""
	for id, c := range v.cpu {
		if c < SlowFrom || s.slowed[id] != nil || s.full[id] {
			continue
		}
		if best == "" || c > v.cpu[best] || (c == v.cpu[best] && id < best) {
			best = id
		}
	}
	return best
}

// background puts an agent's processes in background mode and records
// them; it forgets the ones that have ended.
func (s *Slower) background(sa *slowed, procs []proc.PS, v *view) {
	for pid, start := range sa.pids {
		if v.alive[pid] != start {
			delete(sa.pids, pid)
		}
	}
	for _, p := range procs {
		if err := s.prio().Background(p.PID, true); err == nil {
			sa.pids[p.PID] = p.Start
		}
	}
}

// undo puts an agent back to full speed: the processes recorded for it
// that still run (same pid, same start), and every process on its terminal
// now (one it started while slowed inherited background mode). With no
// look (ps failed) they wait in left.
func (s *Slower) undo(id string, v *view) {
	sa := s.slowed[id]
	delete(s.slowed, id)
	if sa == nil {
		return
	}
	if v == nil {
		if s.left == nil {
			s.left = map[int]string{}
		}
		for pid, start := range sa.pids {
			s.left[pid] = start
		}
		return
	}
	pids := map[int]bool{}
	for pid, start := range sa.pids {
		if v.alive[pid] == start {
			pids[pid] = true
		}
	}
	for _, p := range v.procs[id] {
		pids[p.PID] = true
	}
	for pid := range pids {
		_ = s.prio().Background(pid, false)
	}
}

// undoLeft undoes what a failed look left: the processes that still run.
func (s *Slower) undoLeft(v *view) {
	for pid, start := range s.left {
		if v.alive[pid] == start {
			_ = s.prio().Background(pid, false)
		}
	}
	s.left = nil
}

// undoAll puts every slowed agent back to full speed (calm, or the service
// stops), and anything left from a failed undo.
func (s *Slower) undoAll(why string) {
	v, err := s.look()
	if err != nil {
		v = nil
	}
	s.mu.Lock()
	var names []string
	for id, sa := range s.slowed {
		names = append(names, fmt.Sprintf("%q", sa.title))
		s.undo(id, v)
	}
	if v != nil {
		s.undoLeft(v)
	}
	s.cpu, s.full = nil, nil
	s.save()
	s.mu.Unlock()
	sort.Strings(names)
	if len(names) > 0 {
		s.logf("%s back to full speed (%s)", strings.Join(names, ", "), why)
	}
	if err != nil && len(names) > 0 {
		s.logf("can't list processes (%v): their background mode is undone at the next look", err)
	}
	s.changed()
}

// SlowNow is Slow it: the agent goes in background mode now, by hand,
// while the computer is busy, until it is calm (or Full speed).
func (s *Slower) SlowNow(id string) error {
	if !s.can() {
		return ErrCantSlow
	}
	if !s.Guard.Busy() {
		return ErrNotBusy
	}
	v, err := s.look()
	if err != nil {
		return err
	}
	s.mu.Lock()
	a, ok := v.agents[id]
	if !ok || s.stopped {
		s.mu.Unlock()
		return ErrNoTerminal
	}
	s.startSpell(s.now())
	delete(s.full, id)
	sa := s.slowed[id]
	if sa == nil {
		sa = &slowed{title: a.Title, byHand: true, pids: map[int]string{}}
		s.slowed[id] = sa
	}
	s.cpu = v.cpu
	s.background(sa, v.procs[id], v)
	s.save()
	s.mu.Unlock()
	s.logf("Slow it: background mode for %q until the %s is calm", a.Title, Machine())
	s.changed()
	return nil
}

// FullSpeed takes the agent out of background mode for the rest of this
// busy spell.
func (s *Slower) FullSpeed(id string) error {
	s.mu.Lock()
	sa := s.slowed[id]
	s.mu.Unlock()
	if sa == nil {
		return ErrNotSlowed
	}
	v, err := s.look()
	if err != nil {
		v = nil
	}
	s.mu.Lock()
	if s.full == nil {
		s.full = map[string]bool{}
	}
	s.full[id] = true
	s.undo(id, v)
	s.save()
	s.mu.Unlock()
	s.logf("Full speed: %q out of background mode until the next busy spell", sa.title)
	if err != nil {
		s.logf("can't list processes (%v): undone at the next look", err)
	}
	s.changed()
	return nil
}

// Stop undoes everything, when the service stops.
func (s *Slower) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.undoAll("the service stops")
}

// Recover, on start, puts back to full speed every process the file says
// is slowed (the service stopped without undoing it), if it still runs:
// same pid and start time, so a reused pid is left alone.
func (s *Slower) Recover() {
	if s.File == "" {
		return
	}
	b, err := os.ReadFile(s.File)
	if err != nil {
		return
	}
	var f slowedFile
	if json.Unmarshal(b, &f) != nil || len(f.Slowed) == 0 {
		_ = os.Remove(s.File)
		return
	}
	list, err := s.List()
	if err != nil {
		s.logf("can't list processes (%v): %d slowed processes stay as they are", err, len(f.Slowed))
		return
	}
	alive := map[int]proc.PS{}
	for _, p := range list {
		alive[p.PID] = p
	}
	n := 0
	for _, r := range f.Slowed {
		p, ok := alive[r.PID]
		if !ok || p.Start != r.Start || r.PID <= 1 || proc.IsDeck(p.PID, p.PPID, p.Comm, s.Self) {
			continue
		}
		if s.prio().Background(r.PID, false) == nil {
			n++
		}
	}
	_ = os.Remove(s.File)
	if n > 0 {
		s.logf("%d processes back to full speed: the service stopped while they were slowed", n)
	}
}

type slowedFile struct {
	Slowed []slowedPID `json:"slowed"`
}

type slowedPID struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Agent string `json:"agent,omitempty"`
}

// save writes the slowed processes to File, or removes it when there are
// none. Under the lock.
func (s *Slower) save() {
	if s.File == "" {
		return
	}
	var f slowedFile
	for id, sa := range s.slowed {
		for pid, start := range sa.pids {
			f.Slowed = append(f.Slowed, slowedPID{pid, start, id})
		}
	}
	for pid, start := range s.left {
		f.Slowed = append(f.Slowed, slowedPID{PID: pid, Start: start})
	}
	sort.Slice(f.Slowed, func(i, j int) bool { return f.Slowed[i].PID < f.Slowed[j].PID })
	b, _ := json.Marshal(f)
	if string(b) == s.saved {
		return
	}
	if len(f.Slowed) == 0 {
		if err := os.Remove(s.File); err == nil || errors.Is(err, os.ErrNotExist) {
			s.saved = string(b)
		}
		return
	}
	tmp := s.File + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, s.File) == nil {
		s.saved = string(b)
	}
}

// Slowed are the ids of the agents in background mode now, sorted.
func (s *Slower) Slowed() []string { return s.ids(false) }

// SlowedByYou are the ids of the agents slowed with Slow it, sorted: a
// subset of Slowed.
func (s *Slower) SlowedByYou() []string { return s.ids(true) }

func (s *Slower) ids(byHand bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := []string{}
	for id, sa := range s.slowed {
		if sa.byHand || !byHand {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Who slowed an agent, in Use.
const (
	ByGuard = "guard" // the guard's own pick
	ByYou   = "you"   // Slow it
)

// Use says which agents are slowed and by whom (ByGuard or ByYou), and
// each agent's CPU use at the last look (only while the computer is busy),
// for /v1 Agent.slowed, slowedByYou and cpu.
func (s *Slower) Use() (slowedBy map[string]string, cpu map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slowedBy = map[string]string{}
	for id, sa := range s.slowed {
		slowedBy[id] = ByGuard
		if sa.byHand {
			slowedBy[id] = ByYou
		}
	}
	cpu = map[string]float64{}
	for id, c := range s.cpu {
		cpu[id] = c
	}
	return slowedBy, cpu
}
