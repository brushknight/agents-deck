package node

// The link: this machine keeps a connection to every machine of the
// workspace and knows, for each, whether it is reachable. Grigorii
// (decided): "generic connectivity between machines... Once it's back, we
// will be reconnecting to it and sending all the remaining work."
//
// Each machine dials every other one over HTTPS on its device listener
// (TLS 1.3, node certificates both ways) and keeps that connection open
// (HTTP keep-alive): a heartbeat every HeartbeatEvery, and the machine's own
// outbox for that machine (outbox.go) goes over it. So a pair of machines has
// two connections, one per direction: each carries what its dialer sends,
// with no channel back to hold open. A machine is reachable while this
// machine's own heartbeat to it answered within AwayAfter: that is when this
// machine's outbox for it can be delivered. A machine that calls this one
// while this one can't dial it is still out of reach, with OneWay set (on a
// routed network only one direction may be open: port 7341). Nothing on
// this machine waits on the link: its agents, screens and API go on alone.

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Timing, all tunable (tests shorten them).
var (
	HeartbeatEvery = 10 * time.Second // a heartbeat this often while reachable
	AwayAfter      = 40 * time.Second // this long without word: out of reach
	BackoffMin     = 1 * time.Second  // reconnecting: the first wait,
	BackoffMax     = 60 * time.Second // doubling up to this
	WatchEvery     = time.Second      // how often reachability is looked at
)

// AwayWord is what screens say about a machine that can't be reached. Every
// place that shows the state uses it; JSON and Deck Link carry `reachable`
// and `awaySince`, so the word can change in one line. Open question:
// "out of reach" (the canvas) or "offline".
const AwayWord = "out of reach"

// When a machine is out of reach (config.json "onAway"): "ask" (default) or
// "nothing". With "ask", one machine.ask event goes out per absence after
// AskAfter, for the screens to show a card. Open question: the wait and the
// default; both are kept here.
const (
	OnAwayAsk     = "ask"
	OnAwayNothing = "nothing"
)

var (
	DefaultOnAway = OnAwayAsk
	AskAfter      = 10 * time.Minute
)

// State is what this machine knows about another one right now
// (GET /v1/machines, Deck Link MachineList, `agentctl machines`).
type State struct {
	Name      string    `json:"name"`
	Reachable bool      `json:"reachable"`
	AwaySince time.Time `json:"awaySince,omitzero"` // since when it can't be reached
	RTTMs     int       `json:"rttMs,omitempty"`    // the last heartbeat's round trip
	Version   string    `json:"version,omitempty"`
	Waiting   int       `json:"waiting"` // calls in this machine's outbox for it
	// OneWay: it calls this machine, but this machine can't reach it (its
	// port 7341 closed, or no route): its calls arrive, ours wait.
	OneWay bool `json:"oneWay,omitempty"`
}

// Event is what the link tells subscribers (the /v1/events stream): Kind
// "machines" when any machine's state changed, "machine.ask" once per
// absence (see OnAwayAsk).
type Event struct {
	Kind    string `json:"kind"`
	Machine string `json:"machine,omitempty"`
	Since   string `json:"since,omitempty"`
}

type peer struct {
	lastOK, heard, awaySince time.Time
	rtt                      time.Duration
	version                  string
	reachable, asked         bool
	wake                     chan struct{}
	client                   *http.Client
}

// timings is a copy of the settings above, taken when Run starts, so the
// link's goroutines never read the shared ones.
type timings struct {
	heartbeat, away, backoffMin, backoffMax, watch, ask time.Duration
}

func current() timings {
	return timings{HeartbeatEvery, AwayAfter, BackoffMin, BackoffMax, WatchEvery, AskAfter}
}

// linkState is the link's part of the Manager.
type linkState struct {
	tm     timings // set by Run before anything starts
	lmu    sync.Mutex
	peers  map[string]*peer
	subs   map[chan Event]bool
	OnAway func() string // config.json "onAway"; nil: DefaultOnAway
}

func (m *Manager) peer(name string) *peer {
	if m.peers == nil {
		m.peers = map[string]*peer{}
	}
	p := m.peers[name]
	if p == nil {
		p = &peer{wake: make(chan struct{}, 1)}
		m.peers[name] = p
	}
	return p
}

// Heard notes a request from a machine. It doesn't make it reachable (this
// machine's own dial decides that); it tells a one-way link apart.
func (m *Manager) Heard(name string) {
	m.lmu.Lock()
	m.peer(name).heard = m.clock()
	m.lmu.Unlock()
}

// Subscribe receives the link's events until cancel.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	m.lmu.Lock()
	if m.subs == nil {
		m.subs = map[chan Event]bool{}
	}
	m.subs[ch] = true
	m.lmu.Unlock()
	return ch, func() {
		m.lmu.Lock()
		delete(m.subs, ch)
		m.lmu.Unlock()
	}
}

func (m *Manager) publish(e Event) {
	for ch := range m.subs { // lmu held
		select {
		case ch <- e:
		default: // a slow subscriber misses an event; the next one carries the state
		}
	}
}

// States is every machine's state, by name.
func (m *Manager) States() []State {
	names := m.List()
	m.lmu.Lock()
	defer m.lmu.Unlock()
	if m.tm == (timings{}) {
		m.tm = current()
	}
	out := make([]State, 0, len(names))
	for _, x := range names {
		p := m.peer(x.Name)
		s := State{Name: x.Name, Reachable: p.reachable, Version: p.version, Waiting: m.Pending(x.Name)}
		if p.reachable {
			s.RTTMs = int(p.rtt.Milliseconds())
		} else {
			s.AwaySince = p.awaySince
			s.OneWay = !p.heard.IsZero() && m.clock().Sub(p.heard) < m.tm.away
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run keeps the links up until ctx ends: one loop per machine (started and
// stopped as machines join and leave) and a watch on reachability.
func (m *Manager) Run(ctx context.Context) {
	m.lmu.Lock()
	m.tm = current()
	tm := m.tm
	m.lmu.Unlock()
	running := map[string]context.CancelFunc{}
	t := time.NewTicker(tm.watch)
	defer t.Stop()
	for {
		want := map[string]bool{}
		for _, x := range m.List() {
			want[x.Name] = true
			if running[x.Name] == nil {
				c, cancel := context.WithCancel(ctx)
				running[x.Name] = cancel
				go m.dial(c, x.Name)
			}
		}
		for name, cancel := range running {
			if !want[name] {
				cancel()
				delete(running, name)
				m.lmu.Lock()
				delete(m.peers, name)
				m.lmu.Unlock()
			}
		}
		m.watch()
		select {
		case <-ctx.Done():
			for _, cancel := range running {
				cancel()
			}
			return
		case <-t.C:
		}
	}
}

// dial is one machine's loop: a heartbeat, then its outbox, then wait; on a
// failure, wait with backoff and try again.
func (m *Manager) dial(ctx context.Context, name string) {
	m.lmu.Lock()
	tm := m.tm
	m.lmu.Unlock()
	backoff := time.Duration(0)
	for ctx.Err() == nil {
		wait := tm.heartbeat
		if err := m.beat(ctx, name); err != nil {
			backoff = min(max(backoff*2, tm.backoffMin), tm.backoffMax)
			wait = backoff
		} else {
			backoff = 0
			m.deliver(ctx, name)
			if _, err := m.pushIndex(ctx, name); err != nil {
				wait = min(wait, tm.backoffMin) // try again soon; the next beat decides reachability
			}
		}
		m.lmu.Lock()
		wake := m.peer(name).wake
		m.lmu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-wake: // something to send: go now
		}
	}
}

// Wake makes a machine's loop try at once (a call went into its outbox).
func (m *Manager) Wake(name string) {
	m.lmu.Lock()
	p := m.peer(name)
	m.lmu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// beat sends one heartbeat: GET /node/v1/hello on the kept-open connection.
func (m *Manager) beat(ctx context.Context, name string) error {
	c, x, err := m.linkClient(name)
	if err != nil {
		return err
	}
	m.lmu.Lock()
	away := m.tm.away
	m.lmu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, away/2)
	defer cancel()
	start := time.Now()
	var h Hello
	if _, err := get(cctx, c, "https://"+x.Address+"/node/v1/hello", &h); err != nil {
		return err
	}
	m.lmu.Lock()
	p := m.peer(name)
	p.lastOK, p.rtt, p.version = m.clock(), time.Since(start), h.Version
	m.lmu.Unlock()
	m.watch()
	return nil
}

// linkClient is the machine's client, kept so its connection stays open.
func (m *Manager) linkClient(name string) (*http.Client, Machine, error) {
	x, ok := m.Get(name)
	if !ok {
		return nil, Machine{}, errorf("no machine %q", name)
	}
	m.lmu.Lock()
	defer m.lmu.Unlock()
	p := m.peer(name)
	if p.client == nil {
		c, err := m.Client(x)
		if err != nil {
			return nil, x, err
		}
		c.Timeout = 0 // each call sets its own deadline
		p.client = c
	}
	return p.client, x, nil
}

// watch decides who is reachable, tells subscribers when that changed, and
// raises machine.ask once per absence.
func (m *Manager) watch() {
	onAway := DefaultOnAway
	if m.OnAway != nil {
		if v := m.OnAway(); v != "" {
			onAway = v
		}
	}
	now := m.clock()
	m.lmu.Lock()
	defer m.lmu.Unlock()
	if m.tm == (timings{}) { // not running (a test, or before Run)
		m.tm = current()
	}
	changed := false
	for name, p := range m.peers {
		last := p.lastOK // only this machine's own dial counts (OneWay says the rest)
		reach := !last.IsZero() && now.Sub(last) < m.tm.away
		flip := reach != p.reachable
		if flip || (!reach && p.awaySince.IsZero()) {
			changed = changed || flip
			p.reachable = reach
			if reach {
				p.awaySince, p.asked = time.Time{}, false
				log.Printf("node: %s is reachable", name)
			} else {
				p.awaySince = last
				if last.IsZero() {
					p.awaySince = now
				}
				if flip {
					log.Printf("node: %s is %s", name, AwayWord)
				}
			}
		}
		if !reach && !p.asked && onAway == OnAwayAsk && now.Sub(p.awaySince) >= m.tm.ask {
			p.asked = true
			m.publish(Event{Kind: "machine.ask", Machine: name, Since: p.awaySince.UTC().Format(time.RFC3339)})
		}
	}
	if changed {
		m.publish(Event{Kind: "machines"})
	}
}
