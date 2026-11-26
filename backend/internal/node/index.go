package node

// The agent index: every machine shows every agent. Each machine is the only
// writer of its own agents' records; it pushes them to the other machines
// over its own dial (the link), and they keep a read-only copy. Transcripts,
// queues and screens are not copied: they are asked of the owner when opened.
//
// The owner numbers its changes (seq). A first push after the link (or this
// machine) started is the full snapshot; after that only what changed since
// the seq the other machine acknowledged, with removals. So a reconnect sends
// only what was missed, and rejoining is the same code as a first connect.
//
// Received agents are shown as <id>@<tag> (tag: the machine's letter, see
// Tags), with machine, machineTag and reachable (false while their owner is
// out of reach: they stay, grey, with their last known state).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

type indexState struct {
	imu   sync.Mutex
	epoch string // this run's random id: a receiver that saw another one wants the full snapshot
	seq   int64
	recs  map[string]indexRec // local agent id → its last pushed form
	tombs map[string]int64    // removed local agent id → the seq of its removal
	acked map[string]int64    // machine → the seq it has from this run
	// LocalAgents is this machine's own agents (store, without the remote
	// ones); nil: nothing to push.
	LocalAgents func() []model.Agent

	// Received counts the records this machine took in (tests).
	Received atomic.Int64
	xmu      sync.Mutex
	remote   map[string]*remoteSet // machine → its agents, as received
	// Changed is told when a received index changed what screens show.
	Changed func()
}

type indexRec struct {
	hash  string
	seq   int64
	agent json.RawMessage
}

// remoteSet is one machine's agents as this machine has them (on disk:
// remote-agents/<machine>.json).
type remoteSet struct {
	Epoch  string                 `json:"epoch"`
	Seq    int64                  `json:"seq"`
	Agents map[string]model.Agent `json:"agents"`
}

// indexPush is POST /node/v1/index.
type indexPush struct {
	Epoch   string            `json:"epoch"`
	From    int64             `json:"from"` // what the sender believes the receiver has
	Seq     int64             `json:"seq"`  // what the receiver has after this
	Full    bool              `json:"full"` // the whole set: replace
	Agents  []json.RawMessage `json:"agents,omitempty"`
	Removed []string          `json:"removed,omitempty"`
}

// ReindexLocal looks at this machine's agents and numbers what changed.
// The server calls it when the store changes; it wakes the links that need
// to push.
func (m *Manager) ReindexLocal() {
	if m.LocalAgents == nil {
		return
	}
	agents := m.LocalAgents()
	m.imu.Lock()
	if m.epoch == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		m.epoch = hex.EncodeToString(b)
	}
	if m.recs == nil {
		m.recs, m.tombs, m.acked = map[string]indexRec{}, map[string]int64{}, map[string]int64{}
	}
	seen := map[string]bool{}
	changed := false
	for _, a := range agents {
		if a.MachineTag != "" {
			continue // someone else's
		}
		seen[a.ID] = true
		a.Machine, a.MachineTag, a.Reachable = "", "", false // each receiver fills these
		b, _ := json.Marshal(a)
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:8])
		if r, ok := m.recs[a.ID]; ok && r.hash == h {
			continue
		}
		m.seq++
		m.recs[a.ID] = indexRec{hash: h, seq: m.seq, agent: b}
		delete(m.tombs, a.ID)
		changed = true
	}
	for id := range m.recs {
		if !seen[id] {
			m.seq++
			m.tombs[id] = m.seq
			delete(m.recs, id)
			changed = true
		}
	}
	m.imu.Unlock()
	if changed {
		for _, x := range m.List() {
			m.Wake(x.Name)
		}
	}
}

// pushIndex sends a machine what it hasn't acknowledged, if anything.
// It returns how many records went (tests count them).
func (m *Manager) pushIndex(ctx context.Context, name string) (int, error) {
	if m.LocalAgents == nil {
		return 0, nil
	}
	m.imu.Lock()
	if m.recs == nil {
		m.imu.Unlock()
		m.ReindexLocal()
		m.imu.Lock()
	}
	from, ok := m.acked[name]
	if ok && from == m.seq {
		m.imu.Unlock()
		return 0, nil
	}
	p := indexPush{Epoch: m.epoch, From: from, Seq: m.seq, Full: !ok || from == 0}
	for _, r := range m.recs {
		if p.Full || r.seq > from {
			p.Agents = append(p.Agents, r.agent)
		}
	}
	if !p.Full {
		for id, s := range m.tombs {
			if s > from {
				p.Removed = append(p.Removed, id)
			}
		}
	}
	m.imu.Unlock()

	c, x, err := m.linkClient(name)
	if err != nil {
		return 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var ans struct {
		Epoch string `json:"epoch"`
		Seq   int64  `json:"seq"`
	}
	if _, err := post(cctx, c, "https://"+x.Address+"/node/v1/index", p, &ans); err != nil {
		return 0, err
	}
	m.imu.Lock()
	if ans.Epoch == m.epoch {
		m.acked[name] = ans.Seq // what it has: the next push starts there
	} else {
		delete(m.acked, name) // it has another run's: the full snapshot next
	}
	m.imu.Unlock()
	return len(p.Agents) + len(p.Removed), nil
}

// ServeIndex: POST /node/v1/index from a node: its agents.
func (m *Manager) ServeIndex(w http.ResponseWriter, r *http.Request, from devices.Device) {
	var p indexPush
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	local := map[string]bool{}
	if m.LocalAgents != nil {
		for _, a := range m.LocalAgents() {
			local[a.ID] = true
		}
	}
	m.xmu.Lock()
	set := m.remoteSetLocked(from.Name)
	if !p.Full && (set.Epoch != p.Epoch || set.Seq != p.From) {
		// Not what this machine has: tell the sender, which starts over.
		have := set.Seq
		if set.Epoch != p.Epoch {
			have = 0
		}
		m.xmu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"epoch": p.Epoch, "seq": have})
		return
	}
	if p.Full {
		set.Agents = map[string]model.Agent{}
	}
	for _, raw := range p.Agents {
		var a model.Agent
		if json.Unmarshal(raw, &a) != nil || a.ID == "" {
			continue
		}
		if local[a.ID] {
			log.Printf("node: %s sent a record for %s, an agent of this machine: ignored", from.Name, a.ID)
			continue
		}
		set.Agents[a.ID] = a
	}
	for _, id := range p.Removed {
		delete(set.Agents, id)
	}
	m.Received.Add(int64(len(p.Agents) + len(p.Removed)))
	set.Epoch, set.Seq = p.Epoch, p.Seq
	m.saveRemote(from.Name, set)
	m.xmu.Unlock()
	if m.Changed != nil {
		m.Changed()
	}
	writeJSON(w, http.StatusOK, map[string]any{"epoch": p.Epoch, "seq": p.Seq})
}

func (m *Manager) remoteSetLocked(name string) *remoteSet {
	if m.remote == nil {
		m.remote = map[string]*remoteSet{}
	}
	set := m.remote[name]
	if set == nil {
		set = &remoteSet{Agents: map[string]model.Agent{}}
		if b, err := os.ReadFile(m.remotePath(name)); err == nil {
			_ = json.Unmarshal(b, set)
			if set.Agents == nil {
				set.Agents = map[string]model.Agent{}
			}
		}
		m.remote[name] = set
	}
	return set
}

func (m *Manager) remotePath(name string) string {
	return filepath.Join(m.Dir, "remote-agents", name+".json")
}

func (m *Manager) saveRemote(name string, set *remoteSet) {
	_ = os.MkdirAll(filepath.Dir(m.remotePath(name)), 0o700)
	_ = writeJSONFile(m.remotePath(name), set)
}

// forgetRemote drops a machine's agents (it left).
func (m *Manager) forgetRemote(name string) {
	m.xmu.Lock()
	delete(m.remote, name)
	m.xmu.Unlock()
	_ = os.Remove(m.remotePath(name))
	m.imu.Lock()
	delete(m.acked, name)
	m.imu.Unlock()
}

// Tags gives every machine a short tag for its agents' ids: its first
// letter, or more letters when two machines share one. This machine's own
// agents have none.
func Tags(self string, machines []string) map[string]string {
	names := append([]string(nil), machines...)
	sort.Strings(names)
	out := map[string]string{}
	for n := 1; len(out) < len(names); n++ {
		count := map[string]int{}
		for _, name := range names {
			if _, done := out[name]; !done {
				count[prefix(name, n)]++
			}
		}
		for _, name := range names {
			if _, done := out[name]; !done && (count[prefix(name, n)] == 1 || n >= len(name)) {
				out[name] = prefix(name, n)
			}
		}
	}
	return out
}

func prefix(s string, n int) string {
	if n > len(s) {
		return s
	}
	return s[:n]
}

// RemoteAgents is every other machine's agents as this machine shows them:
// <id>@<tag>, with machine, machineTag and reachable.
func (m *Manager) RemoteAgents() []model.Agent {
	machines := m.List()
	names := make([]string, 0, len(machines))
	for _, x := range machines {
		names = append(names, x.Name)
	}
	tags := Tags(m.Name, names)
	reach := map[string]bool{}
	for _, s := range m.States() {
		reach[s.Name] = s.Reachable
	}
	m.xmu.Lock()
	defer m.xmu.Unlock()
	var out []model.Agent
	for _, name := range names {
		set := m.remoteSetLocked(name)
		ids := make([]string, 0, len(set.Agents))
		for id := range set.Agents {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return set.Agents[ids[i]].Slot < set.Agents[ids[j]].Slot })
		for _, id := range ids {
			a := set.Agents[id]
			a.ID = id + "@" + tags[name]
			a.Machine, a.MachineTag, a.Reachable = name, tags[name], reach[name]
			out = append(out, a)
		}
	}
	return out
}

// RemoteOwner says which machine an id@tag agent belongs to, and its id there.
func (m *Manager) RemoteOwner(id string) (machine, localID string, ok bool) {
	at := strings.LastIndexByte(id, '@')
	if at < 0 {
		return "", "", false
	}
	names := []string{}
	for _, x := range m.List() {
		names = append(names, x.Name)
	}
	for name, tag := range Tags(m.Name, names) {
		if tag == id[at+1:] {
			return name, id[:at], true
		}
	}
	return "", "", false
}
