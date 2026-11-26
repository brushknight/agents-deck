package node

// The outbox: calls for another machine wait on disk until it is reachable,
// then go out in order, one key each. Grigorii (decided): everything is sent,
// no review step. The receiving machine remembers each key it ran, so a call
// sent twice (the connection dropped before the answer came back) runs once,
// and the second time gets the first answer. Which calls use it comes later
// (PR 5); this moves opaque calls.
//
// On disk, under the data folder:
//
//	outbox/<machine>/<seq>.json          a call waiting, oldest first
//	outbox/<machine>/results/<key>.json  its answer, kept for the caller (ResultTTL)
//	outbox-seen/<machine>.json           keys this machine ran for <machine>, with the answers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// Call is one call for another machine.
type Call struct {
	Key    string          `json:"key"` // unique: the receiver runs it once
	Seq    int64           `json:"seq"`
	By     string          `json:"by"` // who asked: "local", a device's name, "mcp:<token>"
	At     time.Time       `json:"at"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Answer is what the other machine said to a call.
type Answer struct {
	Key  string          `json:"key"`
	Code int             `json:"code"`
	Body json.RawMessage `json:"body,omitempty"`
	At   time.Time       `json:"at"`
}

// Tunable.
var (
	DeliverBatch = 50                 // calls per delivery request
	ResultTTL    = 24 * time.Hour     // answers are kept this long for their callers
	SeenTTL      = 7 * 24 * time.Hour // a receiver remembers keys this long,
	SeenMax      = 10000              // and at most this many per sender
)

type outboxes struct {
	omu      sync.Mutex                 // this machine's outboxes
	rmu      sync.Mutex                 // calls coming in: one batch at a time
	inflight map[string]map[string]bool // machine → keys being sent now
	// Dir is the data folder (outbox/ and outbox-seen/ go in it).
	Dir string
	// Exec runs a call that came from another machine (from: its device), as
	// its own /v1 API would: the same scope check and audit. nil refuses.
	Exec func(from devices.Device, c Call) (int, []byte)
}

func (m *Manager) outboxDir(name string) string { return filepath.Join(m.Dir, "outbox", name) }

// Enqueue keeps a call for a machine and wakes its link. It returns the call
// with its key and place.
func (m *Manager) Enqueue(machine, by, method, path string, body []byte) (Call, error) {
	k := make([]byte, 8)
	if _, err := rand.Read(k); err != nil {
		return Call{}, err
	}
	return m.enqueue(machine, hex.EncodeToString(k), by, method, path, body)
}

func (m *Manager) enqueue(machine, key, by, method, path string, body []byte) (Call, error) {
	if _, ok := m.Get(machine); !ok {
		return Call{}, fmt.Errorf("no machine called %q", machine)
	}
	m.omu.Lock()
	dir := m.outboxDir(machine)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.omu.Unlock()
		return Call{}, err
	}
	c := Call{Key: key, Seq: m.nextSeq(dir), By: by, At: m.clock().UTC(), Method: method, Path: path, Body: body}
	err := writeJSONFile(filepath.Join(dir, fmt.Sprintf("%012d.json", c.Seq)), c)
	m.omu.Unlock()
	if err != nil {
		return Call{}, err
	}
	m.Wake(machine)
	return c, nil
}

// nextSeq is one after the newest call waiting (or answered: seq numbers
// never go back while calls wait). omu held.
func (m *Manager) nextSeq(dir string) int64 {
	var hi int64
	for _, c := range m.waiting(dir) {
		hi = max(hi, c.Seq)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "seq")); err == nil {
		var n int64
		_, _ = fmt.Sscan(string(b), &n)
		hi = max(hi, n)
	}
	_ = os.WriteFile(filepath.Join(dir, "seq"), []byte(fmt.Sprint(hi+1)), 0o600)
	return hi + 1
}

// waiting reads a machine's waiting calls, oldest first. omu held.
func (m *Manager) waiting(dir string) []Call {
	ents, _ := os.ReadDir(dir)
	var out []Call
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var c Call
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && json.Unmarshal(b, &c) == nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// Waiting is the calls waiting for a machine, oldest first.
func (m *Manager) Waiting(machine string) []Call {
	m.omu.Lock()
	defer m.omu.Unlock()
	return m.waiting(m.outboxDir(machine))
}

// Pending is how many calls wait for a machine.
func (m *Manager) Pending(machine string) int { return len(m.Waiting(machine)) }

// Cancel takes a call back before it goes. false: it went already (or is
// going now), or there's no such call.
func (m *Manager) Cancel(machine, key string) bool {
	m.omu.Lock()
	defer m.omu.Unlock()
	if m.inflight[machine][key] {
		return false
	}
	dir := m.outboxDir(machine)
	for _, c := range m.waiting(dir) {
		if c.Key == key {
			return os.Remove(filepath.Join(dir, fmt.Sprintf("%012d.json", c.Seq))) == nil
		}
	}
	return false
}

// Answers is a machine's recent answers to this machine's calls, newest first.
func (m *Manager) Answers(machine string) []Answer {
	dir := filepath.Join(m.outboxDir(machine), "results")
	ents, _ := os.ReadDir(dir)
	var out []Answer
	for _, e := range ents {
		var a Answer
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && json.Unmarshal(b, &a) == nil {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Result is the other machine's answer to a call, once it came.
func (m *Manager) Result(machine, key string) (Answer, bool) {
	var a Answer
	b, err := os.ReadFile(filepath.Join(m.outboxDir(machine), "results", key+".json"))
	if err != nil || json.Unmarshal(b, &a) != nil {
		return Answer{}, false
	}
	return a, true
}

// deliver sends a machine's waiting calls, in order, in batches, until none
// wait or a request fails (they stay, and go on the next try).
func (m *Manager) deliver(ctx context.Context, name string) {
	for ctx.Err() == nil {
		m.omu.Lock()
		calls := m.waiting(m.outboxDir(name))
		if len(calls) > DeliverBatch {
			calls = calls[:DeliverBatch]
		}
		if m.inflight == nil {
			m.inflight = map[string]map[string]bool{}
		}
		m.inflight[name] = map[string]bool{}
		for _, c := range calls {
			m.inflight[name][c.Key] = true
		}
		m.omu.Unlock()
		if len(calls) == 0 {
			return
		}
		answers, err := m.send(ctx, name, calls)
		m.omu.Lock()
		dir := m.outboxDir(name)
		_ = os.MkdirAll(filepath.Join(dir, "results"), 0o700)
		for _, a := range answers {
			_ = writeJSONFile(filepath.Join(dir, "results", a.Key+".json"), a)
			for _, c := range calls {
				if c.Key == a.Key {
					_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%012d.json", c.Seq)))
				}
			}
		}
		m.inflight[name] = nil
		pruneResults(filepath.Join(dir, "results"), m.clock())
		m.omu.Unlock()
		if err != nil || len(answers) < len(calls) {
			return
		}
	}
}

func (m *Manager) send(ctx context.Context, name string, calls []Call) ([]Answer, error) {
	c, x, err := m.linkClient(name)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var out struct{ Answers []Answer }
	_, err = post(cctx, c, "https://"+x.Address+"/node/v1/outbox", map[string]any{"calls": calls}, &out)
	return out.Answers, err
}

// ServeOutbox: POST /node/v1/outbox from a node: its calls for this
// machine, run in order; a key seen before gets its first answer again.
func (m *Manager) ServeOutbox(w http.ResponseWriter, r *http.Request, from devices.Device) {
	var in struct{ Calls []Call }
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	m.rmu.Lock() // one batch at a time keeps the order
	defer m.rmu.Unlock()
	seen := m.loadSeen(from.Name)
	var out []Answer
	for _, c := range in.Calls {
		out = append(out, m.runOnce(from, seen, c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"answers": out})
}

// ServeAct: POST /node/v1/act from a node: one call, now (the owner was
// reachable). The same keys as the outbox, so a call that was sent live,
// timed out, and then went through the outbox runs once.
func (m *Manager) ServeAct(w http.ResponseWriter, r *http.Request, from devices.Device) {
	var c Call
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&c); err != nil || c.Key == "" {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	m.rmu.Lock()
	a := m.runOnce(from, m.loadSeen(from.Name), c)
	m.rmu.Unlock()
	writeJSON(w, http.StatusOK, a)
}

// runOnce runs a call from a machine unless its key ran before, and keeps
// the answer under the key (before anything else runs: a crash never runs
// one twice). rmu held.
func (m *Manager) runOnce(from devices.Device, seen map[string]Answer, c Call) Answer {
	if a, ok := seen[c.Key]; ok {
		return a
	}
	code, body := http.StatusNotImplemented, []byte(`{"error":"this machine runs no calls from others"}`)
	if m.Exec != nil {
		code, body = m.Exec(from, c)
	}
	a := Answer{Key: c.Key, Code: code, At: m.clock().UTC()}
	if json.Valid(body) {
		a.Body = body
	}
	seen[c.Key] = a
	m.saveSeen(from.Name, seen)
	return a
}

// Forward runs a call on another machine: now, over this machine's dial,
// when it is reachable; else (or when the live call doesn't get through) it
// waits in the outbox, with the same key so it can't run twice. queued is
// the waiting call (and its place in line) when it didn't run now.
func (m *Manager) Forward(ctx context.Context, machine, by, method, path string, body []byte) (a Answer, queued *Call, position int, err error) {
	k := make([]byte, 8)
	if _, err := rand.Read(k); err != nil {
		return Answer{}, nil, 0, err
	}
	key := hex.EncodeToString(k)
	if m.reachable(machine) {
		c, x, err := m.linkClient(machine)
		if err == nil {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			call := Call{Key: key, By: by, At: m.clock().UTC(), Method: method, Path: path, Body: body}
			_, err = post(cctx, c, "https://"+x.Address+"/node/v1/act", call, &a)
			cancel()
			if err == nil {
				return a, nil, 0, nil
			}
		}
	}
	if method == http.MethodGet { // reading waits for nobody
		return Answer{}, nil, 0, fmt.Errorf("%s is %s", machine, AwayWord)
	}
	c, err := m.enqueue(machine, key, by, method, path, body)
	if err != nil {
		return Answer{}, nil, 0, err
	}
	return Answer{}, &c, m.Pending(machine), nil
}

func (m *Manager) seenPath(from string) string {
	return filepath.Join(m.Dir, "outbox-seen", from+".json")
}

func (m *Manager) loadSeen(from string) map[string]Answer {
	seen := map[string]Answer{}
	if b, err := os.ReadFile(m.seenPath(from)); err == nil {
		_ = json.Unmarshal(b, &seen)
	}
	return seen
}

// saveSeen keeps the keys of SeenTTL, at most SeenMax (the newest).
func (m *Manager) saveSeen(from string, seen map[string]Answer) {
	now := m.clock()
	var keys []string
	for k, a := range seen {
		if now.Sub(a.At) > SeenTTL {
			delete(seen, k)
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) > SeenMax {
		sort.Slice(keys, func(i, j int) bool { return seen[keys[i]].At.Before(seen[keys[j]].At) })
		for _, k := range keys[:len(keys)-SeenMax] {
			delete(seen, k)
		}
	}
	_ = os.MkdirAll(filepath.Dir(m.seenPath(from)), 0o700)
	_ = writeJSONFile(m.seenPath(from), seen)
}

func pruneResults(dir string, now time.Time) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > ResultTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func writeJSONFile(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
