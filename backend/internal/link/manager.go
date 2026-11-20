package link

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// PortConfig is one registered serial port (config.json "link.ports").
type PortConfig struct {
	Path string `json:"path"`
	Baud int    `json:"baud,omitempty"` // default 115200
}

// PortStatus is what `agentctl link` shows for a port.
type PortStatus struct {
	Path    string    `json:"path"`
	Baud    int       `json:"baud"`
	State   string    `json:"state"` // connected | waiting for the board | not plugged in | in use by <program> | error
	Board   string    `json:"board,omitempty"`
	Since   time.Time `json:"since,omitzero"`
	Problem string    `json:"problem,omitempty"`
}

// Manager keeps a session on every registered port that is plugged in and
// not in use by another program.
type Manager struct {
	Deck  Deck
	Ports func() []PortConfig
	Hub   *Hub // optional: where `agentctl link wifi` finds the sessions
	// Wifi is the default network for boards plugged in here (optional).
	Wifi *WifiDefault

	mu      sync.Mutex
	running map[string]*portRun
	status  map[string]PortStatus
}

type portRun struct {
	cancel  context.CancelFunc
	file    io.Closer
	session *Session
	done    chan struct{}
}

// Run checks the registered ports every 2 seconds until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.running, m.status = map[string]*portRun{}, map[string]PortStatus{}
	m.mu.Unlock()
	retry := map[string]time.Time{}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		want := map[string]PortConfig{}
		for _, p := range m.Ports() {
			if p.Baud == 0 {
				p.Baud = 115200
			}
			want[p.Path] = p
		}
		m.mu.Lock()
		for path, r := range m.running {
			if _, ok := want[path]; !ok {
				r.stop()
				delete(m.running, path)
			}
		}
		for path := range m.status {
			if _, ok := want[path]; !ok {
				delete(m.status, path)
			}
		}
		m.mu.Unlock()
		for _, p := range want {
			m.check(ctx, p, retry)
		}
		select {
		case <-ctx.Done():
			m.mu.Lock()
			for _, r := range m.running {
				r.stop()
			}
			m.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

func (r *portRun) stop() {
	r.cancel()
	_ = r.file.Close()
	<-r.done
}

func (m *Manager) set(p PortConfig, state, problem string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status[p.Path]
	if st.State != state {
		st.Since = time.Now()
	}
	st.Path, st.Baud, st.State, st.Problem = p.Path, p.Baud, state, problem
	if r := m.running[p.Path]; r != nil && r.session.Board() != "" {
		st.Board = r.session.Board()
	} else if state != "connected" {
		st.Board = ""
	}
	m.status[p.Path] = st
}

func (m *Manager) check(ctx context.Context, p PortConfig, retry map[string]time.Time) {
	m.mu.Lock()
	r := m.running[p.Path]
	m.mu.Unlock()
	if r != nil {
		select {
		case <-r.done: // the session ended (unplugged, I/O error)
			m.mu.Lock()
			delete(m.running, p.Path)
			m.mu.Unlock()
			retry[p.Path] = time.Now().Add(3 * time.Second)
			return
		default:
		}
		if other := otherUsers(p.Path); other != "" {
			log.Printf("link %s: released for %s", p.Path, other)
			r.stop()
			m.mu.Lock()
			delete(m.running, p.Path)
			m.mu.Unlock()
			m.set(p, "in use by "+other, "")
			return
		}
		state := "waiting for the board"
		if r.session.Board() != "" {
			state = "connected"
		}
		m.set(p, state, "")
		return
	}
	if time.Now().Before(retry[p.Path]) {
		return
	}
	if !portPresent(runtime.GOOS, p.Path, os.Stat, serial.List) {
		m.set(p, "not plugged in", "")
		return
	}
	if other := otherUsers(p.Path); other != "" {
		m.set(p, "in use by "+other, "")
		return
	}
	f, err := OpenPort(p.Path, p.Baud)
	if err != nil {
		m.set(p, "error", err.Error())
		retry[p.Path] = time.Now().Add(10 * time.Second)
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	s := m.portSession(f, p)
	run := &portRun{cancel: cancel, file: f, session: s, done: make(chan struct{})}
	m.mu.Lock()
	m.running[p.Path] = run
	m.mu.Unlock()
	log.Printf("link %s: open at %d baud", p.Path, p.Baud)
	m.set(p, "waiting for the board", "")
	go func() {
		defer close(run.done)
		err := s.Run(sctx)
		_ = f.Close()
		if sctx.Err() == nil {
			log.Printf("link %s: closed (%v)", p.Path, err)
		}
	}()
}

// portSession is the session on a registered port: USB in the owner's hands
// (auto-approval, the default Wi-Fi network).
func (m *Manager) portSession(rw io.ReadWriter, p PortConfig) *Session {
	s := NewSession(m.Deck, rw, filepath.Base(p.Path))
	s.Hub, s.Where, s.USBPort, s.Wifi = m.Hub, p.Path, p.Path, m.Wifi
	return s
}

// Status lists every registered port.
func (m *Manager) Status() []PortStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PortStatus, 0, len(m.status))
	for _, s := range m.status {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// otherUsers names another program holding the port open ("" if none), so the
// deck steps aside while you flash or monitor a board. The daemon's own
// children don't count: between fork and exec they hold every descriptor.
//
// It needs lsof (macOS has it; on Linux it may not be installed). Windows has
// nothing like it: a port another program holds fails to open, and the port
// shows that error.
func otherUsers(path string) string {
	lsof := lsofPath(runtime.GOOS, exec.LookPath, os.Stat)
	if lsof == "" {
		return ""
	}
	out, err := exec.Command(lsof, "-F", "pcR", path).Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return otherUser(string(out), os.Getpid())
}

// lsofPath finds lsof: on the PATH, else where macOS (/usr/sbin) and Linux
// distributions (/usr/bin, /usr/sbin) put it; "" on Windows or when it isn't
// installed.
func lsofPath(goos string, look func(string) (string, error), stat func(string) (os.FileInfo, error)) string {
	if goos == "windows" {
		return ""
	}
	if p, err := look("lsof"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/sbin/lsof", "/usr/bin/lsof"} {
		if _, err := stat(p); err == nil {
			return p
		}
	}
	return ""
}

// portPresent: the port is there to open. On Windows a COM port is a device
// name, not a file (os.Stat fails on it, or on one another program holds), so
// the ports Windows lists right now decide.
func portPresent(goos, path string, stat func(string) (os.FileInfo, error), list func() ([]serial.Info, error)) bool {
	if goos != "windows" {
		_, err := stat(path)
		return err == nil
	}
	ports, err := list()
	if err != nil {
		return true // can't tell: try to open it, and show what fails
	}
	name := strings.TrimPrefix(path, `\\.\`)
	for _, p := range ports {
		if strings.EqualFold(p.Path, name) {
			return true
		}
	}
	return false
}

// otherUser reads `lsof -F pcR` output: the first holder that is neither
// this process nor its child.
func otherUser(lsof string, self int) string {
	me := strconv.Itoa(self)
	pid, parent := "", ""
	for _, l := range strings.Split(lsof, "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			pid, parent = l[1:], ""
		case strings.HasPrefix(l, "R"):
			parent = l[1:]
		case strings.HasPrefix(l, "c") && pid != me && parent != me:
			return l[1:]
		}
	}
	return ""
}

// CandidatePorts lists USB serial ports that look like boards.
func CandidatePorts() []string {
	patterns := []string{"/dev/cu.usbmodem*", "/dev/cu.usbserial*", "/dev/cu.SLAB_USBtoUART*", "/dev/cu.wchusbserial*", "/dev/ttyACM*", "/dev/ttyUSB*"}
	var out []string
	ports, err := serial.List()
	if err != nil {
		for _, g := range patterns {
			m, _ := filepath.Glob(g)
			out = append(out, m...)
		}
		sort.Strings(out)
		return out
	}
	for _, p := range ports {
		ok := strings.HasPrefix(p.Path, "COM") && p.VID != "" // Windows: USB COM ports
		for _, g := range patterns {
			if m, _ := filepath.Match(g, p.Path); m {
				ok = true
			}
		}
		if ok {
			out = append(out, p.Path)
		}
	}
	sort.Strings(out)
	return out
}
