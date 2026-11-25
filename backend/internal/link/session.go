package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Deck is what a session needs from the daemon: the state, its changes, the
// device registry and the web API (for commands, with the board's scope).
type Deck interface {
	Snapshot() model.State
	Changes() (<-chan struct{}, func())
	Board(boardID, typ, firmware string) (devices.Device, error)
	Device(name string) (devices.Device, bool)
	Do(d devices.Device, method, path string, body []byte) (int, []byte)
	// Pair issues an approved board its Wi-Fi certificate from a CSR (the
	// reply to CertRequest), or returns an HTTP status and error.
	Pair(d devices.Device, csr []byte) (*linkpb.CertIssued, int, string)
}

// AutoApprover is a Deck that approves boards plugged in over USB by itself
// (devices.Registry.AutoApproveUSB): it returns the entry and whether it
// approved it just now.
type AutoApprover interface {
	AutoApprove(d devices.Device, port string) (devices.Device, bool)
}

// Default timing; each Session has its own copy (tests and link check
// shorten theirs).
const (
	PingEvery    = 5 * time.Second
	RecheckEvery = 2 * time.Second // registry: approval, scope, revocation
	Debounce     = 150 * time.Millisecond
)

// Transports a session runs on.
const (
	OverSerial = "serial" // USB serial (and `agentctl link sim`)
	OverWifi   = "wifi"   // TLS 1.3 on the network listener (GET /v1/link)
	OverBLE    = "ble"    // Bluetooth LE, paired with the passkey on the board's screen (internal/ble)
)

// Session talks Deck Link with one board over rw until ctx ends or rw fails.
type Session struct {
	Deck Deck
	Name string // the port, for logs

	// Set before Run.
	PingEvery, RecheckEvery, Debounce time.Duration
	Hub                               *Hub   // where `agentctl link` and `link wifi` find the session (optional)
	Transport                         string // OverSerial (default), OverWifi or OverBLE
	Where                             string // the port, or the board's network address
	// Peer is the device whose certificate opened a Wi-Fi session: the board
	// must say Hello with its board id, and the session ends when the device
	// is revoked or pairs again.
	Peer *devices.Device
	// IdleTimeout ends the session after this long without a frame from the
	// board (0: never). A board answers every Ping, so 30 s is plenty.
	IdleTimeout time.Duration
	// USBPort is the registered serial port the session runs on (Manager
	// sets it; never for `agentctl link sim` or Wi-Fi). A board there is in
	// the owner's hands: a new one may be approved at once (AutoApprover),
	// and an approved, paired one gets the default Wi-Fi network (Wifi).
	USBPort string
	// Wifi is the default network, sent to an approved, paired board on a
	// registered USB port or on Bluetooth (paired with the passkey on its
	// screen, which counts as in hand), never over Wi-Fi.
	Wifi *WifiDefault

	rw       io.ReadWriter
	wmu      sync.Mutex
	mode     Mode
	maxFrame int

	hello    *linkpb.Hello
	dev      devices.Device
	welcomed bool // a Welcome reflecting dev has been sent
	sentAs   devices.Device

	sub       *linkpb.Subscribe
	sent      map[string][]byte // agent id -> the AgentChanged it last got
	sentMagic []byte            // the Magic it last got…
	magicSent bool              // …if any since its last snapshot
	sentLims  []byte            // the Limits it last got (FIELDS_LIMITS)…
	limsSent  bool              // …if any since its last snapshot
	ping      uint32

	board     atomic.Pointer[string]       // the board's device name, for status (any goroutine)
	helloSeen atomic.Pointer[linkpb.Hello] // its Hello, for Hub (any goroutine)
	live      atomic.Bool                  // approved, for Hub (any goroutine)
	net       atomic.Pointer[linkpb.NetStatus]
	since     time.Time

	// Requests from the deck to the board (WifiConfig), from other
	// goroutines: sent by Run, answered by the board's Result.
	outbox  chan *linkpb.Frame
	pmu     sync.Mutex
	pending map[uint32]chan *linkpb.Frame
	reqSeq  atomic.Uint32
	done    chan struct{}
	lastRx  time.Time

	wifiTried bool // the default network was offered in this session (once at most)

	uplink         uplinkSync      // sending the board its uplink preference (linkconfig.go)
	boardBle       linkpb.BleState // what its last NetStatus said (kept when Net is cleared)
	boardUplink    linkpb.Uplink
	rxAt           atomic.Int64 // when the board last sent a frame (unix ns), for Hub (any goroutine)
	wrongTransport atomic.Bool  // its NetStatus named another transport than this link's
	bleSecureAt    atomic.Int64 // when its NetStatus last said a secure Bluetooth central is connected (unix ns)

	// stop ends Run when the board moves to an uplink the deck prefers
	// (Hub.arbitrate, uplink.go).
	stop     chan struct{}
	stopOnce sync.Once
}

// Board is the connected board's device name ("" before its Hello).
func (s *Session) Board() string {
	if p := s.board.Load(); p != nil {
		return *p
	}
	return ""
}

func (s *Session) setDevice(d devices.Device) {
	s.dev = d
	name := d.Name
	s.board.Store(&name)
	s.live.Store(s.hello != nil && d.Active())
}

// NewSession prepares a session; Run drives it.
func NewSession(deck Deck, rw io.ReadWriter, name string) *Session {
	return &Session{Deck: deck, rw: rw, Name: name, maxFrame: MaxFrame, Transport: OverSerial,
		PingEvery: PingEvery, RecheckEvery: RecheckEvery, Debounce: Debounce,
		outbox: make(chan *linkpb.Frame, 4), pending: map[uint32]chan *linkpb.Frame{}, done: make(chan struct{}), stop: make(chan struct{})}
}

// Net is what the board last said about its network (nil before it did).
func (s *Session) Net() *linkpb.NetStatus { return s.net.Load() }

// errCutOff ends a Wi-Fi session whose certificate no longer counts.
var errCutOff = errors.New("the board's certificate was revoked or replaced")

// request sends a frame to the board from any goroutine and waits for its
// Result (same seq).
func (s *Session) request(ctx context.Context, f *linkpb.Frame) (*linkpb.Result, error) {
	seq := s.reqSeq.Add(1) | 1<<30 // apart from the deck's ping seqs
	f.Seq = seq
	ch := make(chan *linkpb.Frame, 1)
	s.pmu.Lock()
	s.pending[seq] = ch
	s.pmu.Unlock()
	defer func() {
		s.pmu.Lock()
		delete(s.pending, seq)
		s.pmu.Unlock()
	}()
	select {
	case s.outbox <- f:
	case <-s.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-ch:
		return r.GetResult(), nil
	case <-s.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// post sends a frame to the board from any goroutine (no reply expected).
func (s *Session) post(ctx context.Context, f *linkpb.Frame) error {
	select {
	case s.outbox <- f:
		return nil
	case <-s.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendWifi gives the board Wi-Fi credentials (or makes it forget them) and
// returns its answer: 204 stored, or an error code and text.
func (s *Session) SendWifi(ctx context.Context, ssid, password string, forget bool) (int, string, error) {
	r, err := s.request(ctx, &linkpb.Frame{Body: &linkpb.Frame_WifiConfig{WifiConfig: &linkpb.WifiConfig{Ssid: ssid, Password: password, Forget: forget}}})
	if err != nil {
		return 0, "", err
	}
	if r == nil {
		return http.StatusBadGateway, "the board answered with something else", nil
	}
	if r.Code < 300 {
		s.net.Store(nil) // what it said before is stale: it reports again after trying the network
	}
	return int(r.Code), r.Error, nil
}

func (s *Session) active() bool { return s.hello != nil && s.dev.Active() }

// Run reads frames and pushes state until ctx is done or the stream ends.
func (s *Session) Run(ctx context.Context) error {
	s.since, s.lastRx = time.Now(), time.Now()
	s.heard(s.lastRx)
	if s.Where == "" {
		s.Where = s.Name
	}
	if s.Hub != nil {
		s.Hub.add(s)
		defer s.Hub.remove(s)
	}
	defer close(s.done)
	defer s.wifiGone()
	frames := make(chan Decoded, 16)
	readErr := make(chan error, 1)
	go func() {
		d := &Decoder{}
		buf := make([]byte, 1024)
		for {
			n, err := s.rw.Read(buf)
			for _, f := range d.Feed(buf[:n]) {
				frames <- f
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()
	changes, unsub := s.Deck.Changes()
	defer unsub()
	ping := time.NewTicker(s.PingEvery)
	defer ping.Stop()
	recheck := time.NewTicker(s.RecheckEvery)
	defer recheck.Stop()
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.stop:
			return ErrHandedOver
		case err := <-readErr:
			return err
		case f := <-frames:
			s.lastRx = time.Now()
			s.heard(s.lastRx)
			if err := s.handle(f); err != nil {
				return err
			}
		case f := <-s.outbox:
			if err := s.send(f); err != nil {
				return err
			}
		case <-changes:
			if debounce == nil {
				debounce = time.After(s.Debounce)
			}
		case <-debounce:
			debounce = nil
			if s.active() && s.sub != nil {
				if err := s.push(false); err != nil {
					return err
				}
			}
		case <-ping.C:
			if s.IdleTimeout > 0 && time.Since(s.lastRx) > s.IdleTimeout {
				return fmt.Errorf("nothing from the board for %s", s.IdleTimeout)
			}
			if s.hello != nil {
				s.ping++
				if err := s.send(&linkpb.Frame{Seq: s.ping, Body: &linkpb.Frame_Ping{Ping: &linkpb.Ping{TimeMs: uint64(time.Now().UnixMilli())}}}); err != nil {
					return err
				}
			}
		case <-recheck.C:
			if err := s.recheck(); err != nil {
				return err
			}
		}
	}
}

// recheck follows the registry: approval, scope and revocation take effect
// at once, with a new Welcome.
func (s *Session) recheck() error {
	if s.hello == nil {
		return nil
	}
	if s.Peer != nil { // Wi-Fi: only while its certificate counts
		d, ok := s.Deck.Device(s.Peer.Name)
		if !ok || !d.Active() || d.CertSerial != s.Peer.CertSerial {
			log.Printf("link %s: %s cut off: revoked, removed or paired again", s.Name, s.Peer.Name)
			return errCutOff
		}
	}
	d, ok := s.Deck.Device(s.dev.Name)
	if !ok { // removed: it registers again as new
		var err error
		if d, err = s.Deck.Board(s.hello.BoardId, s.hello.Type, s.hello.Firmware); err != nil {
			return nil
		}
	}
	was, oldUplink := s.active(), s.dev.Uplink
	s.setDevice(d)
	if d.Uplink != oldUplink || (!was && s.active()) {
		s.syncUplink() // the preference changed, or it may be sent now
	}
	if d.Active() == s.sentAs.Active() && d.Scope == s.sentAs.Scope && d.Name == s.sentAs.Name && d.CertSerial == s.sentAs.CertSerial {
		return nil
	}
	if err := s.welcome(); err != nil {
		return err
	}
	if !was && s.active() {
		log.Printf("link %s: %s approved", s.Name, d.Name)
		if s.sub != nil {
			return s.push(true)
		}
	}
	if was && !s.active() {
		s.sent = nil
	}
	return nil
}

func (s *Session) welcome() error {
	st := s.snapshot()
	s.sentAs = s.dev
	return s.send(&linkpb.Frame{Body: &linkpb.Frame_Welcome{Welcome: &linkpb.Welcome{
		Protocol: 1, ServerName: st.Server.Name, ServerVersion: st.Server.Version,
		DeviceName: s.dev.Name, Scope: scopes[s.dev.Scope], Approved: s.dev.Active(), CertSerial: s.dev.CertSerial,
	}}})
}

var scopes = map[devices.Scope]linkpb.Scope{devices.View: linkpb.Scope_SCOPE_VIEW, devices.Act: linkpb.Scope_SCOPE_ACT, devices.Admin: linkpb.Scope_SCOPE_ADMIN}

func (s *Session) handle(in Decoded) error {
	f := in.Frame
	switch b := f.Body.(type) {
	case *linkpb.Frame_Hello:
		return s.onHello(f.Seq, b.Hello, in.Mode)
	case *linkpb.Frame_Ping:
		return s.send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Pong{Pong: &linkpb.Pong{TimeMs: b.Ping.TimeMs}}})
	case *linkpb.Frame_Pong, nil:
		return nil
	case *linkpb.Frame_Result: // the board's answer to a request of ours (WifiConfig)
		s.pmu.Lock()
		ch := s.pending[f.Seq]
		s.pmu.Unlock()
		if ch != nil {
			ch <- f
		}
		return nil
	}
	if s.hello == nil {
		return nil // nothing before Hello
	}
	switch b := f.Body.(type) {
	case *linkpb.Frame_NetStatus:
		s.onNetStatus(b.NetStatus)
		return nil
	case *linkpb.Frame_CertRequest:
		return s.onCertRequest(f.Seq, b.CertRequest)
	case *linkpb.Frame_FirmwareStatus:
		if s.Hub != nil && s.dev.Name != "" {
			s.Hub.firmwareStatus(s.dev.Name, b.FirmwareStatus)
		}
		return nil
	}
	if sub, ok := f.Body.(*linkpb.Frame_Subscribe); ok {
		s.sub = sub.Subscribe
		if s.active() {
			return s.push(true)
		}
		return nil
	}
	if !s.active() {
		return s.result(f.Seq, http.StatusForbidden, s.dev.Name+" is waiting for approval: agentctl devices approve "+s.dev.Name)
	}
	return s.command(f)
}

func (s *Session) onHello(seq uint32, h *linkpb.Hello, mode Mode) error {
	if h.Protocol != 1 || h.BoardId == "" {
		log.Printf("link %s: hello with protocol %d, board id %q: ignored", s.Name, h.Protocol, h.BoardId)
		return nil
	}
	if s.Peer != nil && h.BoardId != s.Peer.BoardID {
		// A certificate is one board's: it can't speak for another.
		log.Printf("link %s: %s's certificate said Hello as board %q: refused", s.Name, s.Peer.Name, h.BoardId)
		s.mode = mode
		_ = s.result(seq, http.StatusForbidden, "this certificate belongs to "+s.Peer.Name+", not to this board")
		return errors.New("board id doesn't match the certificate")
	}
	d, err := s.Deck.Board(h.BoardId, h.Type, h.Firmware)
	if err != nil {
		return fmt.Errorf("registering board: %w", err)
	}
	if a, ok := s.Deck.(AutoApprover); ok && s.USBPort != "" && s.Peer == nil && s.Transport == OverSerial && !d.Active() {
		d, _ = a.AutoApprove(d, s.USBPort)
	}
	if s.hello == nil || s.hello.BoardId != h.BoardId {
		state := "approved"
		if !d.Active() {
			state = "waiting for approval: agentctl devices approve " + d.Name
		}
		log.Printf("link %s: %s (%s %s) over %s, %s", s.Name, d.Name, h.Type, h.Firmware, s.Transport, state)
	}
	s.hello, s.mode, s.sub, s.sent, s.magicSent = h, mode, nil, nil, false
	s.setDevice(d)
	if m, ok := s.Deck.(interface{ Motion(name string, on bool) }); ok {
		m.Motion(d.Name, slices.Contains(h.Inputs, linkpb.Input_INPUT_MOTION)) // it can receive the magic trick
	}
	s.helloSeen.Store(h)
	if s.Hub != nil {
		s.Hub.firmwareHello(d.Name, h.Firmware)
	}
	s.maxFrame = MaxFrame
	if m := int(h.MaxFrame); m >= MinFrame && m < MaxFrame {
		s.maxFrame = m
	}
	if s.Hub != nil {
		if err := s.Hub.arbitrate(s); err != nil { // one session per board (uplink.go)
			return err
		}
	}
	return s.welcome()
}

// onCertRequest pairs the board for Wi-Fi: over USB serial or Bluetooth
// (paired with the board's passkey), approved boards only. The certificate
// goes on the board's own registry entry.
func (s *Session) onCertRequest(seq uint32, req *linkpb.CertRequest) error {
	if !s.Hands() {
		return s.result(seq, http.StatusForbidden, "pair over USB or Bluetooth: plug the board into this computer")
	}
	if !s.active() {
		return s.result(seq, http.StatusForbidden, s.dev.Name+" is waiting for approval: agentctl devices approve "+s.dev.Name)
	}
	ci, code, msg := s.Deck.Pair(s.dev, req.Csr)
	if code != http.StatusOK || ci == nil {
		return s.result(seq, code, msg)
	}
	if d, ok := s.Deck.Device(s.dev.Name); ok {
		s.setDevice(d)
		s.sentAs.CertSerial = d.CertSerial // the board learns it from CertIssued: no new Welcome
	}
	f := &linkpb.Frame{Seq: seq, Body: &linkpb.Frame_CertIssued{CertIssued: ci}}
	clipMessage(f.ProtoReflect())
	if len(ci.Cert) > limits["CertIssued.cert"].size || len(ci.Ca) > limits["CertIssued.ca"].size {
		return s.result(seq, http.StatusInternalServerError, "certificate larger than link.options allows")
	}
	if EncodedSize(f) > s.maxFrame {
		return s.result(seq, http.StatusRequestEntityTooLarge, "pairing needs frames of 2048 bytes (Hello.max_frame)")
	}
	return s.send(f)
}

// onNetStatus keeps what the board says about its network, for `agentctl
// link` (never its password: the board doesn't send it).
func (s *Session) onNetStatus(n *linkpb.NetStatus) {
	old := s.net.Swap(n)
	if old == nil || old.Wifi != n.Wifi || old.Ip != n.Ip {
		line := WifiStateName(n.Wifi)
		if n.Ip != "" {
			line += " " + n.Ip
		}
		log.Printf("link %s: %s Wi-Fi %s", s.Name, s.dev.Name, line)
	}
	s.boardBle, s.boardUplink = n.Ble, n.Uplink
	if n.Ble == linkpb.BleState_BLE_CONNECTED {
		s.bleSecureAt.Store(time.Now().UnixNano())
	}
	s.checkTransport(n)
	s.syncUplink()
	s.defaultWifi(n)
}

// getsDefault: the board is where the default network may be sent: a
// registered USB port, or Bluetooth (paired with the passkey on its screen).
// Never Wi-Fi, never `agentctl link sim`.
func (s *Session) getsDefault() bool {
	if s.Wifi == nil || !s.Hands() {
		return false
	}
	return s.Transport == OverBLE || (s.Transport == OverSerial && s.USBPort != "")
}

// defaultWifi gives an approved board on a registered USB port or on
// Bluetooth the deck's default Wi-Fi network once it is paired
// (NetStatus.has_cert) and lacks it, at most once per session; and notes how
// the board's test of it went.
func (s *Session) defaultWifi(n *linkpb.NetStatus) {
	if !s.getsDefault() || !s.active() {
		return
	}
	name := s.dev.Name
	if ssid, gen, ok := s.Wifi.pending(name); ok {
		if n.Ssid == ssid {
			if state, failed := outcome(n); state != "" {
				s.Wifi.note(name, ssid, gen, state, failed, true)
			}
		}
		return
	}
	if s.wifiTried || s.dev.WifiOwn {
		return
	}
	c, gen, ok := s.Wifi.wanted(name, n)
	if !ok {
		return
	}
	s.wifiTried = true
	s.Wifi.start(name, c.SSID, gen, s)
	go func() { // the answer comes through Run: wait for it elsewhere
		ctx, cancel := context.WithTimeout(context.Background(), WifiTimeout)
		defer cancel()
		code, msg, err := s.SendWifi(ctx, c.SSID, c.Password, false)
		switch {
		case err != nil:
			s.Wifi.note(name, c.SSID, gen, "no answer ("+err.Error()+")", true, true)
		case code >= 300:
			if msg == "" {
				msg = http.StatusText(code)
			}
			s.Wifi.note(name, c.SSID, gen, fmt.Sprintf("refused: %d %s", code, msg), true, true)
		default:
			s.Wifi.stored(name, c.SSID, gen)
		}
	}()
}

// wifiGone: a board unplugged (or out of Bluetooth range) while it tested the
// default tries it again next time.
func (s *Session) wifiGone() {
	if !s.getsDefault() || s.dev.Name == "" {
		return
	}
	s.Wifi.gone(s.dev.Name, s)
}

// snapshot is the state as this board sees it: without the agents another
// screen took (the deck's View, when it has one).
func (s *Session) snapshot() model.State {
	st := s.Deck.Snapshot()
	if v, ok := s.Deck.(interface {
		View(model.State, string) model.State
	}); ok {
		st = v.View(st, s.dev.Name)
	}
	return st
}

// push sends the subscribed agents: everything (a snapshot) or only what
// changed since the last push.
func (s *Session) push(full bool) error {
	st := s.snapshot()
	sub := s.sub
	var agents []model.Agent
	for _, a := range st.Agents {
		if sub.SlotCount == 0 || a.Slot >= int(sub.FirstSlot) && a.Slot < int(sub.FirstSlot+sub.SlotCount) {
			agents = append(agents, a)
		}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Slot < agents[j].Slot })
	if full {
		s.sent = nil
		if err := s.send(&linkpb.Frame{Body: &linkpb.Frame_SnapshotBegin{SnapshotBegin: &linkpb.SnapshotBegin{
			Server:     &linkpb.Server{Name: st.Server.Name, Version: st.Server.Version, TimeMs: st.Server.Time.UnixMilli()},
			AgentCount: uint32(len(st.Agents)),
		}}}); err != nil {
			return err
		}
	}
	if !full { // its part in the trick first: a giver flies the tiles out before they go
		if err := s.pushMagic(st); err != nil {
			return err
		}
	}
	next := map[string][]byte{}
	for _, a := range agents {
		p := toPB(a, sub.Fields)
		key, _ := proto.MarshalOptions{Deterministic: true}.Marshal(p)
		next[a.ID] = key
		if bytes.Equal(s.sent[a.ID], key) {
			continue
		}
		if err := s.sendAgent(p); err != nil {
			return err
		}
	}
	for id := range s.sent {
		if _, ok := next[id]; !ok {
			if err := s.send(&linkpb.Frame{Body: &linkpb.Frame_AgentRemoved{AgentRemoved: &linkpb.AgentRemoved{Id: id}}}); err != nil {
				return err
			}
		}
	}
	s.sent = next
	if full {
		if err := s.send(&linkpb.Frame{Body: &linkpb.Frame_SnapshotEnd{SnapshotEnd: &linkpb.SnapshotEnd{}}}); err != nil {
			return err
		}
		s.magicSent, s.limsSent = false, false
		if err := s.pushMagic(st); err != nil {
			return err
		}
	}
	return s.pushLimits(st)
}

// pushLimits sends the accounts' usage limits to a board that subscribed to
// them, when they changed, and after a snapshot when there are any.
func (s *Session) pushLimits(st model.State) error {
	if s.sub == nil || s.sub.Fields&uint32(linkpb.FieldGroup_FIELDS_LIMITS) == 0 {
		return nil
	}
	m := limitsPB(st.Limits)
	key, _ := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if s.limsSent && bytes.Equal(s.sentLims, key) {
		return nil
	}
	first := !s.limsSent
	s.sentLims, s.limsSent = key, true
	if first && len(m.Sets) == 0 { // nothing to say yet
		return nil
	}
	return s.send(&linkpb.Frame{Body: &linkpb.Frame_Limits{Limits: m}})
}

// pushMagic sends the board its part in the magic trick when that changed,
// and after a snapshot when it has one: giver, receiver or none, whether the
// agents are taken, and its corners.
func (s *Session) pushMagic(st model.State) error {
	m := &linkpb.Magic{}
	if st.Magic != nil {
		m.Taken, m.Arrive, m.Leave = st.Magic.Taken, cornerPB(st.Magic.Arrive), cornerPB(st.Magic.Leave)
		switch st.Magic.Role {
		case "giver":
			m.Role = linkpb.MagicRole_MAGIC_GIVER
		case "receiver":
			m.Role = linkpb.MagicRole_MAGIC_RECEIVER
		}
	}
	key, _ := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if s.magicSent && bytes.Equal(s.sentMagic, key) {
		return nil
	}
	if !s.magicSent && m.Role == linkpb.MagicRole_MAGIC_NONE { // no part: a board starts out with none, nothing to say
		s.sentMagic, s.magicSent = key, true
		return nil
	}
	s.sentMagic, s.magicSent = key, true
	return s.send(&linkpb.Frame{Body: &linkpb.Frame_Magic{Magic: m}})
}

func cornerPB(c string) linkpb.Corner {
	switch c {
	case "top-right":
		return linkpb.Corner_CORNER_TOP_RIGHT
	case "bottom-left":
		return linkpb.Corner_CORNER_BOTTOM_LEFT
	case "bottom-right":
		return linkpb.Corner_CORNER_BOTTOM_RIGHT
	}
	return linkpb.Corner_CORNER_TOP_LEFT
}

// sendAgent sends one agent, trimming its biggest parts when the board's
// frames are too small for it.
func (s *Session) sendAgent(p *linkpb.Agent) error {
	f := &linkpb.Frame{Body: &linkpb.Frame_AgentChanged{AgentChanged: &linkpb.AgentChanged{Agent: p}}}
	shrink := []func(){
		func() { p.Subagents = nil },
		func() { p.LastPrompt, p.LastAnswer, p.AiTitle, p.Cwd = "", "", "", "" },
		func() {
			if p.Waiting != nil {
				p.Waiting.Detail = clipString(p.Waiting.Detail, 128)
			}
		},
		func() {
			if p.Waiting != nil {
				p.Waiting.Detail, p.Waiting.Context = "", ""
				for _, o := range p.Waiting.Options {
					o.Label = clipString(o.Label, 24)
				}
			}
		},
	}
	for i := 0; EncodedSize(f) > s.maxFrame && i < len(shrink); i++ {
		shrink[i]()
	}
	if EncodedSize(f) > s.maxFrame {
		log.Printf("link %s: agent %s doesn't fit in %d bytes: skipped", s.Name, p.Id, s.maxFrame)
		return nil
	}
	return s.send(f)
}

func (s *Session) send(f *linkpb.Frame) error {
	var b []byte
	var err error
	if s.mode == JSON {
		b, err = EncodeJSON(f)
	} else {
		b, err = Encode(f)
	}
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = s.rw.Write(b)
	return err
}

func (s *Session) result(seq uint32, code int, msg string) error {
	return s.send(&linkpb.Frame{Seq: seq, Body: &linkpb.Frame_Result{Result: &linkpb.Result{Code: uint32(code), Error: clipString(msg, 96)}}})
}

// ---- commands -------------------------------------------------------------

// Routes: the web API endpoint behind each command (Frame field name). The
// parity test checks this against docs/protocol.md and link.proto.
var Routes = map[string]string{
	"get_state":      "GET /v1/state",
	"list_hats":      "GET /v1/hats",
	"list_live":      "GET /v1/live",
	"answer":         "POST /v1/agents/{id}/answer",
	"focus":          "POST /v1/agents/{id}/focus",
	"interrupt":      "POST /v1/agents/{id}/interrupt",
	"send_prompt":    "POST /v1/agents/{id}/prompt",
	"queue":          "GET /v1/agents/{id}/queue | POST /v1/agents/{id}/queue/{qid}/cancel",
	"dismiss":        "POST /v1/agents/{id}/dismiss",
	"resume":         "POST /v1/agents/{id}/resume",
	"move":           "POST /v1/agents/{id}/move",
	"move_attention": "POST /v1/agents/{id}/attention",
	"stash":          "POST /v1/stash",
	"magic_view":     "POST /v1/magic/view",
	"order":          "POST /v1/order",
	"restore":        "POST /v1/restore",
	"set_hat":        "POST /v1/agents/{id}/hat | POST /v1/hats",
	"add_live":       "POST /v1/live/{session}/add",
	"handoff":        "POST /v1/agents/{id}/handoff",
	"hatch_pet":      "POST /v1/agents/{id}/pet/hatch",

	"list_accounts": "GET /v1/accounts",
}

func agentPath(id, action string) string { return "/v1/agents/" + url.PathEscape(id) + "/" + action }

func (s *Session) command(f *linkpb.Frame) error {
	var method, path string
	var body any
	switch b := f.Body.(type) {
	case *linkpb.Frame_GetState:
		if code, out := s.Deck.Do(s.dev, http.MethodGet, "/v1/state", nil); code != http.StatusOK {
			return s.result(f.Seq, code, errText(out))
		}
		if s.sub != nil {
			if err := s.push(true); err != nil {
				return err
			}
		}
		return s.result(f.Seq, http.StatusNoContent, "")
	case *linkpb.Frame_ListHats:
		return s.typed(f.Seq, "/v1/hats", func(raw []byte) *linkpb.Frame {
			var r struct {
				Shapes, Colors []string
				Overrides      map[string]struct{ Shape, Color string }
			}
			_ = json.Unmarshal(raw, &r)
			h := &linkpb.HatList{Shapes: r.Shapes, Colors: r.Colors}
			folders := make([]string, 0, len(r.Overrides))
			for k := range r.Overrides {
				folders = append(folders, k)
			}
			sort.Strings(folders)
			for _, k := range folders {
				h.Overrides = append(h.Overrides, &linkpb.HatOverride{Folder: k, Shape: r.Overrides[k].Shape, Color: r.Overrides[k].Color})
			}
			return &linkpb.Frame{Body: &linkpb.Frame_HatList{HatList: h}}
		})
	case *linkpb.Frame_ListLive:
		return s.typed(f.Seq, "/v1/live", func(raw []byte) *linkpb.Frame {
			var r struct {
				Sessions []struct {
					SessionID, Title, Cwd, Folder string
					Started                       time.Time
					OnBoard, Background           bool
				}
			}
			_ = json.Unmarshal(raw, &r)
			l := &linkpb.LiveSessions{}
			for _, x := range r.Sessions {
				l.Sessions = append(l.Sessions, &linkpb.LiveSession{SessionId: x.SessionID, Title: x.Title, Cwd: x.Cwd,
					Folder: x.Folder, StartedMs: ms(x.Started), OnBoard: x.OnBoard, Background: x.Background})
			}
			return &linkpb.Frame{Body: &linkpb.Frame_LiveSessions{LiveSessions: l}}
		})
	case *linkpb.Frame_ListAccounts:
		return s.typed(f.Seq, "/v1/accounts", func(raw []byte) *linkpb.Frame {
			var r struct {
				Accounts []struct {
					Name, Color string
					Default     bool
				}
			}
			_ = json.Unmarshal(raw, &r)
			l := &linkpb.AccountList{}
			for _, x := range r.Accounts {
				l.Accounts = append(l.Accounts, &linkpb.AccountInfo{Name: x.Name, Color: x.Color, IsDefault: x.Default})
			}
			return &linkpb.Frame{Body: &linkpb.Frame_AccountList{AccountList: l}}
		})
	case *linkpb.Frame_Restore:
		var in []byte
		if ids := b.Restore.AgentIds; len(ids) > 0 {
			in, _ = json.Marshal(map[string][]string{"ids": ids})
		}
		code, out := s.Deck.Do(s.dev, http.MethodPost, "/v1/restore", in)
		if code != http.StatusOK {
			return s.result(f.Seq, code, errText(out))
		}
		var r struct {
			Results []struct{ ID, Title, Action, Error string }
		}
		_ = json.Unmarshal(out, &r)
		rr := &linkpb.RestoreResult{}
		for _, x := range r.Results {
			rr.Results = append(rr.Results, &linkpb.Restored{Id: x.ID, Title: x.Title, Action: x.Action, Error: x.Error})
		}
		return s.sendReply(f.Seq, &linkpb.Frame{Body: &linkpb.Frame_RestoreResult{RestoreResult: rr}})
	case *linkpb.Frame_Answer:
		method, path, body = http.MethodPost, agentPath(b.Answer.AgentId, "answer"), map[string]string{"promptId": b.Answer.PromptId, "key": b.Answer.Key}
	case *linkpb.Frame_SendPrompt:
		sp := b.SendPrompt
		pb := map[string]any{"text": sp.Text, "interrupt": sp.Interrupt}
		switch sp.Mode {
		case linkpb.SendMode_SEND_INTERRUPT:
			pb["interrupt"] = true
		case linkpb.SendMode_SEND_QUEUE:
			pb["mode"] = "queue"
			if sp.Front {
				pb["position"] = "front"
			}
		case linkpb.SendMode_SEND_TYPE:
			pb["mode"] = "type"
		}
		method, path, body = http.MethodPost, agentPath(sp.AgentId, "prompt"), pb
	case *linkpb.Frame_Queue:
		if b.Queue.CancelId != "" {
			method, path = http.MethodPost, agentPath(b.Queue.AgentId, "queue/"+url.PathEscape(b.Queue.CancelId)+"/cancel")
			break
		}
		return s.typed(f.Seq, agentPath(b.Queue.AgentId, "queue"), func(raw []byte) *linkpb.Frame {
			var r struct {
				Queue []struct {
					ID, Text, By string
					QueuedAt     time.Time
				}
			}
			_ = json.Unmarshal(raw, &r)
			l := &linkpb.QueueList{Total: uint32(len(r.Queue))}
			for _, x := range r.Queue {
				l.Prompts = append(l.Prompts, &linkpb.QueuedPrompt{Id: x.ID, Text: x.Text, By: x.By, QueuedAtMs: ms(x.QueuedAt)})
			}
			return &linkpb.Frame{Body: &linkpb.Frame_QueueList{QueueList: l}}
		})
	case *linkpb.Frame_Focus:
		method, path = http.MethodPost, agentPath(b.Focus.AgentId, "focus")
	case *linkpb.Frame_Interrupt:
		method, path = http.MethodPost, agentPath(b.Interrupt.AgentId, "interrupt")
	case *linkpb.Frame_Dismiss:
		method, path = http.MethodPost, agentPath(b.Dismiss.AgentId, "dismiss")
	case *linkpb.Frame_HatchPet:
		method, path = http.MethodPost, agentPath(b.HatchPet.AgentId, "pet/hatch")
	case *linkpb.Frame_Resume:
		method, path = http.MethodPost, agentPath(b.Resume.AgentId, "resume")
	case *linkpb.Frame_Handoff:
		method, path, body = http.MethodPost, agentPath(b.Handoff.AgentId, "handoff"), map[string]string{"account": b.Handoff.Account}
	case *linkpb.Frame_Move:
		method, path, body = http.MethodPost, agentPath(b.Move.AgentId, "move"), map[string]int{"slot": int(b.Move.Slot)}
	case *linkpb.Frame_MoveAttention:
		method, path, body = http.MethodPost, agentPath(b.MoveAttention.AgentId, "attention"), map[string]int{"slot": int(b.MoveAttention.Slot)}
	case *linkpb.Frame_Order:
		method, path, body = http.MethodPost, "/v1/order", map[string][]string{"ids": b.Order.AgentIds}
	case *linkpb.Frame_MagicView:
		ids := b.MagicView.AgentIds
		if ids == nil {
			ids = []string{}
		}
		method, path, body = http.MethodPost, "/v1/magic/view", map[string][]string{"ids": ids}
	case *linkpb.Frame_Stash:
		ids := b.Stash.AgentIds
		if ids == nil {
			ids = []string{} // none: give them back
		}
		method, path, body = http.MethodPost, "/v1/stash", map[string]any{"ids": ids, "all": b.Stash.All}
	case *linkpb.Frame_SetHat:
		h := b.SetHat
		hb := map[string]any{"shape": h.Shape, "color": h.Color, "auto": h.Automatic}
		if h.AgentId != "" {
			method, path = http.MethodPost, agentPath(h.AgentId, "hat")
		} else {
			method, path = http.MethodPost, "/v1/hats"
			hb["folder"] = h.Folder
		}
		body = hb
	case *linkpb.Frame_AddLive:
		method, path = http.MethodPost, "/v1/live/"+url.PathEscape(b.AddLive.SessionId)+"/add"
		if b.AddLive.Slot >= 0 {
			body = map[string]int{"slot": int(b.AddLive.Slot)}
		}
	default:
		return s.result(f.Seq, http.StatusBadRequest, "not a command")
	}
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	code, out := s.Deck.Do(s.dev, method, path, raw)
	return s.result(f.Seq, code, errText(out))
}

// typed runs a GET and sends its typed reply (or a Result on failure).
func (s *Session) typed(seq uint32, path string, build func([]byte) *linkpb.Frame) error {
	code, out := s.Deck.Do(s.dev, http.MethodGet, path, nil)
	if code != http.StatusOK {
		return s.result(seq, code, errText(out))
	}
	return s.sendReply(seq, build(out))
}

func (s *Session) sendReply(seq uint32, f *linkpb.Frame) error {
	f.Seq = seq
	clipMessage(f.ProtoReflect())
	if EncodedSize(f) > s.maxFrame {
		return s.result(seq, http.StatusInternalServerError, "reply too large for this board")
	}
	return s.send(f)
}

func errText(body []byte) string {
	var e struct{ Error string }
	if json.Unmarshal(body, &e) == nil {
		return e.Error
	}
	return ""
}

// ErrClosed is returned when the board's stream ends.
var ErrClosed = errors.New("link closed")
