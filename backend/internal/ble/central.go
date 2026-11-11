// Package ble is the computer's side of Bluetooth (BLE) for Deck Link boards.
// Go here has no Bluetooth of its own (standard library, no cgo), so a small
// native helper per platform does the radio work (clients/ble: deck-ble on
// macOS) and the service talks to it with JSON lines on its stdin and stdout.
//
// Central runs and supervises the helper and turns a connected board into a
// byte stream; Uplinks (uplink.go) runs Deck Link sessions on those streams
// for the boards you added.
package ble

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// EnvHelper names the helper to run instead of the one next to agentctl
// (tests, development). "none" turns Bluetooth off.
const EnvHelper = "AGENTS_DECK_BLE_HELPER"

// NotHere is what every Bluetooth call says on a computer without a helper.
const NotHere = "Bluetooth isn't available on this computer yet"

// ErrUnavailable: no helper, the helper isn't running, or the radio is off.
var ErrUnavailable = errors.New(NotHere)

// Find is the helper to run: $AGENTS_DECK_BLE_HELPER, else deck-ble next to
// this executable (inside the app on macOS), else "".
func Find() string {
	if p, ok := os.LookupEnv(EnvHelper); ok {
		if p == "none" {
			return ""
		}
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, _ = filepath.EvalSymlinks(exe)
	name := "deck-ble"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(filepath.Dir(exe), name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// Status is Bluetooth on this computer, for the management API.
type Status struct {
	Available bool   `json:"available"`     // boards can be scanned for and connected now
	Why       string `json:"why,omitempty"` // why not, in words
	// Helper: none (this computer has no Bluetooth helper) | starting |
	// running | restarting (it stopped; it is started again shortly).
	Helper    string `json:"helper"`
	Radio     string `json:"radio,omitempty"` // on | off | unauthorized | unsupported | resetting | unknown
	Scanning  bool   `json:"scanning"`
	Restarts  int    `json:"restarts,omitempty"`  // times the helper stopped and was started again
	LastError string `json:"lastError,omitempty"` // the helper's last problem
}

// Nearby is a board seen in a scan.
type Nearby struct {
	ID       string    `json:"id"`   // this computer's identifier for it
	Name     string    `json:"name"` // what it advertises: deck-<last 6 of its board id>
	RSSI     int       `json:"rssi,omitempty"`
	Board    string    `json:"board,omitempty"`   // from its Info: the catalog board
	BoardID  string    `json:"boardId,omitempty"` // its board id (what it says in Hello)
	Firmware string    `json:"firmware,omitempty"`
	Setup    bool      `json:"setup"` // not bonded to any computer: ready to be added
	HasInfo  bool      `json:"hasInfo"`
	Seen     time.Time `json:"seen"`
}

// msg is one line from the helper (clients/ble/README.md).
type msg struct {
	Ev       string `json:"ev"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	RSSI     int    `json:"rssi"`
	Board    string `json:"board"`
	BoardID  string `json:"boardId"`
	Firmware string `json:"firmware"`
	Paired   *bool  `json:"paired"`
	Data     []byte `json:"data"` // base64 in the line
	N        int    `json:"n"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	State    string `json:"state"`
	On       bool   `json:"on"`
	MTU      int    `json:"mtu"`
	Window   int    `json:"window"`
	Protocol int    `json:"protocol"`
}

// Central runs the helper and keeps what it reported.
type Central struct {
	Path string   // the helper ("" : no Bluetooth here)
	Args []string // its arguments
	Env  []string // added to its environment
	// Back-off between restarts after the helper stops: MinBackoff doubling
	// up to MaxBackoff (1 s and 30 s by default).
	MinBackoff, MaxBackoff time.Duration

	mu       sync.Mutex
	stdin    io.WriteCloser
	proc     *os.Process
	wmu      sync.Mutex
	status   Status
	wantScan bool
	nearby   map[string]*Nearby
	links    map[string]*Stream
	window   int
	ready    chan struct{} // closed when the running helper said "ready"
}

func (c *Central) init() {
	if c.nearby == nil {
		c.nearby, c.links = map[string]*Nearby{}, map[string]*Stream{}
	}
}

// Run starts the helper and starts it again whenever it stops, until ctx
// ends. Without a helper it returns at once.
func (c *Central) Run(ctx context.Context) {
	c.mu.Lock()
	c.init()
	if c.Path == "" {
		c.status = Status{Helper: "none", Why: NotHere}
		c.mu.Unlock()
		return
	}
	c.status.Helper = "starting"
	c.mu.Unlock()
	lo, hi := c.MinBackoff, c.MaxBackoff
	if lo <= 0 {
		lo = time.Second
	}
	if hi <= 0 {
		hi = 30 * time.Second
	}
	wait := lo
	for {
		started := time.Now()
		err := c.runOnce(ctx)
		c.lost(err)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			wait = lo
		}
		log.Printf("bluetooth: the helper stopped (%v): starting it again in %s", err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, hi)
	}
}

// runOnce runs the helper until it exits or ctx ends.
func (c *Central) runOnce(ctx context.Context) error {
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stderr = logWriter{}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	c.mu.Lock()
	c.stdin, c.proc, c.ready = stdin, cmd.Process, make(chan struct{})
	c.status.Helper = "starting"
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		_ = stdin.Close() // the helper exits when its stdin closes
		time.AfterFunc(3*time.Second, func() { _ = cmd.Process.Kill() })
	})
	defer stop()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var m msg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			log.Printf("bluetooth: the helper said something that isn't JSON: %.80q", sc.Text())
			continue
		}
		c.handle(m)
	}
	_ = stdin.Close()
	err = cmd.Wait()
	if err == nil {
		err = errors.New("it exited")
	}
	return err
}

// lost: the helper is gone; every link with it.
func (c *Central) lost(err error) {
	c.mu.Lock()
	c.stdin, c.proc = nil, nil
	c.status.Helper, c.status.Scanning, c.status.Radio = "restarting", false, ""
	c.status.Restarts++
	if err != nil {
		c.status.LastError = "the Bluetooth helper stopped: " + err.Error()
	}
	links := c.links
	c.links = map[string]*Stream{}
	c.mu.Unlock()
	for _, s := range links {
		s.fail(errors.New("the Bluetooth helper stopped"))
	}
}

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	log.Printf("bluetooth helper: %s", trimNL(p))
	return len(p), nil
}

func trimNL(p []byte) string {
	for len(p) > 0 && (p[len(p)-1] == '\n' || p[len(p)-1] == '\r') {
		p = p[:len(p)-1]
	}
	return string(p)
}

func (c *Central) handle(m msg) {
	c.mu.Lock()
	s := c.links[m.ID]
	switch m.Ev {
	case "ready":
		c.status.Helper = "running"
		c.window = m.Window
		if c.window <= 0 {
			c.window = 4096
		}
		select {
		case <-c.ready:
		default:
			close(c.ready)
		}
		c.mu.Unlock()
		_ = c.send(map[string]any{"op": "state"})
		return
	case "state":
		was := c.status.Radio
		c.status.Radio = m.State
		if m.State != "on" {
			c.status.Scanning = false
		}
		c.mu.Unlock()
		if m.State != was {
			log.Printf("bluetooth: %s", m.Message)
		}
		if m.State == "on" && was != "on" && c.scanWanted() {
			_ = c.send(map[string]any{"op": "scan", "on": true})
		}
		return
	case "scanning":
		c.status.Scanning = m.On
	case "found":
		n := c.nearby[m.ID]
		if n == nil {
			n = &Nearby{ID: m.ID}
			c.nearby[m.ID] = n
		}
		if m.Name != "" {
			n.Name = m.Name
		}
		if m.RSSI != 0 {
			n.RSSI = m.RSSI
		}
		n.Seen = time.Now()
		if m.BoardID != "" || m.Paired != nil {
			n.Board, n.BoardID, n.Firmware, n.HasInfo = m.Board, m.BoardID, m.Firmware, true
			n.Setup = m.Paired != nil && !*m.Paired
		}
	case "error":
		if m.Message != "" {
			c.status.LastError = m.Message
		}
	}
	c.mu.Unlock()
	if s == nil {
		return
	}
	switch m.Ev {
	case "passkey":
		s.onPasskey(m.Message)
	case "connected":
		s.onConnected(m)
	case "data":
		s.feed(m.Data)
	case "sent":
		s.credit(m.N)
	case "disconnected":
		c.drop(m.ID, s)
		s.fail(errors.New(orText(m.Reason, "the board disconnected")))
	case "error":
		if m.Message != "" {
			c.drop(m.ID, s)
			s.fail(errors.New(m.Message))
		}
	}
}

func orText(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (c *Central) drop(id string, s *Stream) {
	c.mu.Lock()
	if c.links[id] == s {
		delete(c.links, id)
	}
	c.mu.Unlock()
}

func (c *Central) scanWanted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wantScan
}

// send writes one command line to the helper.
func (c *Central) send(v any) error {
	c.mu.Lock()
	w := c.stdin
	c.mu.Unlock()
	if w == nil {
		return ErrUnavailable
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = w.Write(append(b, '\n'))
	return err
}

// Status is what the helper last said.
func (c *Central) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	if c.Path == "" {
		return Status{Helper: "none", Why: NotHere}
	}
	if st.Helper == "" {
		st.Helper = "starting"
	}
	st.Available = st.Helper == "running" && st.Radio == "on"
	if !st.Available {
		switch {
		case st.Helper != "running":
			st.Why = "the Bluetooth helper is " + st.Helper
		case st.Radio == "off":
			st.Why = "Bluetooth is off: turn it on in Control Centre or System Settings"
		case st.Radio == "unauthorized":
			st.Why = "agents deck may not use Bluetooth: allow \"agents deck Bluetooth\" in System Settings, Privacy & Security, Bluetooth"
		case st.Radio == "unsupported":
			st.Why = "this computer has no Bluetooth Low Energy"
		default:
			st.Why = "Bluetooth is starting"
		}
	}
	return st
}

func (c *Central) usable() error {
	if st := c.Status(); !st.Available {
		if c.Path == "" {
			return ErrUnavailable
		}
		return errors.New(st.Why)
	}
	return nil
}

// Scan starts or stops looking for boards. Starting forgets the boards seen
// before.
func (c *Central) Scan(on bool) error {
	if on {
		if err := c.usable(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.init()
	c.wantScan = on
	if on {
		c.nearby = map[string]*Nearby{}
	}
	c.mu.Unlock()
	if c.Path == "" {
		return nil
	}
	return c.send(map[string]any{"op": "scan", "on": on})
}

// Nearby lists the boards seen since the scan started, strongest first.
func (c *Central) Nearby() []Nearby {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Nearby, 0, len(c.nearby))
	for _, n := range c.nearby {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RSSI != out[j].RSSI {
			return out[i].RSSI > out[j].RSSI
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Seen is a board from the scan, by identifier.
func (c *Central) Seen(id string) (Nearby, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nearby[id]
	if !ok {
		return Nearby{}, false
	}
	return *n, true
}

// Connect connects to a board and returns its stream once it is paired and
// subscribed. A board that needs pairing makes the computer ask for the code
// on its screen: passkey is told so (it may be nil). A board out of range is
// waited for until ctx ends.
func (c *Central) Connect(ctx context.Context, id string, passkey func(msg string)) (*Stream, error) {
	if err := c.usable(); err != nil {
		return nil, err
	}
	s := &Stream{c: c, ID: id, connected: make(chan struct{}), done: make(chan struct{}), passkey: passkey}
	s.cond = sync.NewCond(&s.mu)
	c.mu.Lock()
	c.init()
	if c.links[id] != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("already connecting to %s", id)
	}
	c.links[id] = s
	c.mu.Unlock()
	if err := c.send(map[string]any{"op": "connect", "id": id}); err != nil {
		c.drop(id, s)
		return nil, err
	}
	select {
	case <-s.connected:
		return s, nil
	case <-s.done:
		c.drop(id, s)
		return nil, s.Err()
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	}
}

// Forget drops a board: its link ends and the helper lets it go. The
// computer's own bond stays until it is removed in its Bluetooth settings
// (the helper can't remove it).
func (c *Central) Forget(id string) {
	c.mu.Lock()
	s := c.links[id]
	delete(c.links, id)
	delete(c.nearby, id)
	c.mu.Unlock()
	if s != nil {
		s.fail(errors.New("forgotten"))
	}
	_ = c.send(map[string]any{"op": "forget", "id": id})
}

func (c *Central) windowSize() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.window <= 0 {
		return 4096
	}
	return c.window
}
