package link

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// Board is the board side of Deck Link in Go: `agentctl link sim` and tests.
// It keeps the agents it was sent and calls Event for everything.
type Board struct {
	Hello  *linkpb.Hello
	Fields uint32
	Mode   Mode
	Event  func(f *linkpb.Frame) // every frame from the deck (after bookkeeping)
	// Wifi answers the deck's WifiConfig (nil: 501, no Wi-Fi on this board).
	Wifi func(w *linkpb.WifiConfig) (code uint32, err string)

	rw     io.ReadWriter
	wmu    sync.Mutex
	seq    uint32
	mu     sync.Mutex
	agents map[string]*linkpb.Agent
	scope  linkpb.Scope
}

// NewBoard prepares a board on rw.
func NewBoard(rw io.ReadWriter, hello *linkpb.Hello, fields uint32) *Board {
	return &Board{rw: rw, Hello: hello, Fields: fields, agents: map[string]*linkpb.Agent{}}
}

// Send sends one frame with the next seq and returns it.
func (b *Board) Send(f *linkpb.Frame) uint32 {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	b.seq++
	if f.Seq == 0 {
		f.Seq = b.seq
	}
	enc := Encode
	if b.Mode == JSON {
		enc = EncodeJSON
	}
	if w, err := enc(f); err == nil {
		_, _ = b.rw.Write(w)
	}
	return f.Seq
}

// Run says Hello, subscribes once approved, answers pings and keeps the
// agent table until the stream ends.
func (b *Board) Run() error {
	b.Send(&linkpb.Frame{Body: &linkpb.Frame_Hello{Hello: b.Hello}})
	d := &Decoder{}
	buf := make([]byte, 2048)
	for {
		n, err := b.rw.Read(buf)
		for _, in := range d.Feed(buf[:n]) {
			b.handle(in.Frame)
		}
		if err != nil {
			return err
		}
	}
}

func (b *Board) handle(f *linkpb.Frame) {
	switch x := f.Body.(type) {
	case *linkpb.Frame_Welcome:
		b.mu.Lock()
		b.scope = x.Welcome.Scope
		b.mu.Unlock()
		if x.Welcome.Approved {
			b.Send(&linkpb.Frame{Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{Fields: b.Fields}}})
		}
	case *linkpb.Frame_Ping:
		b.Send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Pong{Pong: &linkpb.Pong{TimeMs: x.Ping.TimeMs}}})
	case *linkpb.Frame_WifiConfig:
		code, msg := uint32(501), "no Wi-Fi on this board"
		if b.Wifi != nil {
			code, msg = b.Wifi(x.WifiConfig)
		}
		b.Send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Result{Result: &linkpb.Result{Code: code, Error: msg}}})
	case *linkpb.Frame_SnapshotBegin:
		b.mu.Lock()
		b.agents = map[string]*linkpb.Agent{}
		b.mu.Unlock()
	case *linkpb.Frame_AgentChanged:
		b.mu.Lock()
		b.agents[x.AgentChanged.Agent.Id] = x.AgentChanged.Agent
		b.mu.Unlock()
	case *linkpb.Frame_AgentRemoved:
		b.mu.Lock()
		delete(b.agents, x.AgentRemoved.Id)
		b.mu.Unlock()
	}
	if b.Event != nil {
		b.Event(f)
	}
}

// Agent returns the board's copy of an agent by id or title prefix.
func (b *Board) Agent(ref string) *linkpb.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	if a, ok := b.agents[ref]; ok {
		return a
	}
	for _, a := range b.agents {
		if strings.HasPrefix(strings.ToLower(a.Title), strings.ToLower(ref)) {
			return a
		}
	}
	return nil
}

// Agents lists the board's agents by slot.
func (b *Board) Agents() []*linkpb.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*linkpb.Agent, 0, len(b.agents))
	for _, a := range b.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// Describe renders a frame as one readable line.
func Describe(f *linkpb.Frame) string {
	switch x := f.Body.(type) {
	case *linkpb.Frame_Welcome:
		w := x.Welcome
		if !w.Approved {
			return fmt.Sprintf("welcome: %s on %s, waiting for approval (agentctl devices approve %s)", w.DeviceName, w.ServerName, w.DeviceName)
		}
		return fmt.Sprintf("welcome: %s on %s, scope %s", w.DeviceName, w.ServerName, strings.ToLower(strings.TrimPrefix(w.Scope.String(), "SCOPE_")))
	case *linkpb.Frame_SnapshotBegin:
		return fmt.Sprintf("snapshot: %d agents on the deck", x.SnapshotBegin.AgentCount)
	case *linkpb.Frame_SnapshotEnd:
		return "snapshot end"
	case *linkpb.Frame_AgentChanged:
		a := x.AgentChanged.Agent
		line := fmt.Sprintf("agent %s #%d %-24s %s", a.Id, a.Slot, a.Title, strings.ToLower(strings.TrimPrefix(a.Status.String(), "STATUS_")))
		if a.Waiting != nil {
			var opts []string
			for _, o := range a.Waiting.Options {
				opts = append(opts, o.Key+"="+o.Label)
			}
			line += fmt.Sprintf(" · %s [%s] (prompt %s)", a.Waiting.Title, strings.Join(opts, ", "), a.Waiting.Id)
		}
		return line
	case *linkpb.Frame_AgentRemoved:
		return "removed " + x.AgentRemoved.Id
	case *linkpb.Frame_Result:
		if x.Result.Error != "" {
			return fmt.Sprintf("result #%d: %d %s", f.Seq, x.Result.Code, x.Result.Error)
		}
		return fmt.Sprintf("result #%d: %d", f.Seq, x.Result.Code)
	case *linkpb.Frame_Ping, *linkpb.Frame_Pong:
		return ""
	case *linkpb.Frame_WifiConfig:
		if x.WifiConfig.Forget {
			return "wifi: forget the network"
		}
		return "wifi: join " + x.WifiConfig.Ssid // never the password
	case *linkpb.Frame_CertIssued:
		return fmt.Sprintf("paired: certificate %s for %s, deck at %v port %d", x.CertIssued.CertSerial, x.CertIssued.DeviceName, x.CertIssued.Hosts, x.CertIssued.Port)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", f))
}

// Ago: a short "3m" for status lines.
func Ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
