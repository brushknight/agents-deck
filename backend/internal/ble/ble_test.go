package ble

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeEnv); dir != "" {
		fakeHelper(dir) // this process is the fake Bluetooth helper
		return
	}
	home, _ := os.MkdirTemp("", "ble-test-home")
	os.Setenv("AGENTSTERM_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// ---- the rig ----------------------------------------------------------------------

type fakeCtl struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCtl) add(s string)                 { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeCtl) Answer(id, p, k string) error { f.add("answer " + id); return nil }
func (f *fakeCtl) Focus(id string) error        { f.add("focus " + id); return nil }
func (f *fakeCtl) Interrupt(id string) error    { f.add("interrupt " + id); return nil }
func (f *fakeCtl) Dismiss(id string) error      { f.add("dismiss " + id); return nil }
func (f *fakeCtl) Resume(id string) error       { f.add("resume " + id); return nil }
func (f *fakeCtl) has(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == s {
			return true
		}
	}
	return false
}

const (
	periph1 = "6B1F2C3D-0000-4000-8000-0000000000A1"
	periph2 = "6B1F2C3D-0000-4000-8000-0000000000A2"
	board1  = "AA:BB:CC:3F:A9:C1"
	board2  = "AA:BB:CC:3F:A9:C2"
)

type rig struct {
	t       *testing.T
	dir     string
	srv     *server.Server
	ctl     *fakeCtl
	hub     *link.Hub
	central *Central
	up      *Uplinks
	socks   map[string]chan net.Conn
}

func newRig(t *testing.T, boards []fakeBoard) *rig {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ble") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	st := store.New("", "test-mac", "9.9")
	st.Add(&store.Entry{A: model.Agent{ID: "a1", Slot: 0, Title: "checkout-api", Tool: "claude", Status: model.Running}})
	ctl := &fakeCtl{}
	srv := &server.Server{Store: st, Ctl: ctl, Static: fstest.MapFS{},
		Devices: &devices.Registry{Path: filepath.Join(dir, "devices.json")},
		Audit:   &devices.Audit{Path: filepath.Join(dir, "audit.log")}}
	r := &rig{t: t, dir: dir, srv: srv, ctl: ctl, hub: &link.Hub{}, socks: map[string]chan net.Conn{}}
	r.setBoards(boards)
	for _, b := range boards {
		l, err := net.Listen("unix", filepath.Join(dir, b.Sock))
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan net.Conn, 4)
		r.socks[b.ID] = ch
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				ch <- c
			}
		}()
		t.Cleanup(func() { l.Close() })
	}
	exe, _ := os.Executable()
	r.central = &Central{Path: exe, Env: []string{fakeEnv + "=" + dir}, MinBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond}
	r.up = &Uplinks{Central: r.central, Deck: link.ServerDeck{S: srv}, Hub: r.hub, Devices: srv.Devices,
		Audit: srv.Audit.Log, Every: 50 * time.Millisecond,
		Tune: func(s *link.Session) { s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond }}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.central.Run(ctx)
	go r.up.Run(ctx)
	r.waitFor("the helper is ready", func() bool { return r.central.Status().Available })
	return r
}

func (r *rig) setBoards(boards []fakeBoard) {
	b, _ := json.Marshal(boards)
	if err := os.WriteFile(filepath.Join(r.dir, "boards.json"), b, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) waitFor(what string, ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *rig) commands() string {
	b, _ := os.ReadFile(filepath.Join(r.dir, "commands.log"))
	return string(b)
}

// count is how many times the helper was sent this command line.
func (r *rig) count(line string) int {
	n := 0
	for _, l := range strings.Split(r.commands(), "\n") {
		if l == line {
			n++
		}
	}
	return n
}

// accept is the board's end of its next Bluetooth connection.
func (r *rig) accept(id string) *board {
	r.t.Helper()
	select {
	case c := <-r.socks[id]:
		r.t.Cleanup(func() { c.Close() })
		return &board{t: r.t, conn: c}
	case <-time.After(5 * time.Second):
		r.t.Fatalf("no Bluetooth connection to %s", id)
	}
	return nil
}

func (r *rig) scan() {
	r.t.Helper()
	if err := r.central.Scan(true); err != nil {
		r.t.Fatal(err)
	}
	r.waitFor("boards with their Info", func() bool {
		n := 0
		for _, b := range r.central.Nearby() {
			if b.HasInfo {
				n++
			}
		}
		return n == len(r.socks)
	})
}

// board is the test's side of a board's Bluetooth stream.
type board struct {
	t    *testing.T
	conn net.Conn
	dec  link.Decoder
	q    []link.Decoded
}

func (b *board) send(f *linkpb.Frame) {
	b.t.Helper()
	w, err := link.Encode(f)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.conn.Write(w); err != nil {
		b.t.Fatal(err)
	}
}

func (b *board) next() *linkpb.Frame {
	b.t.Helper()
	buf := make([]byte, 4096)
	for {
		for len(b.q) > 0 {
			f := b.q[0]
			b.q = b.q[1:]
			if f.Frame.GetPing() == nil {
				return f.Frame
			}
		}
		_ = b.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := b.conn.Read(buf)
		if err != nil {
			b.t.Fatalf("waiting for a frame: %v", err)
		}
		b.q = append(b.q, b.dec.Feed(buf[:n])...)
	}
}

// closed: the deck hung up on the board.
func (b *board) closed() bool {
	_ = b.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	for {
		if _, err := b.conn.Read(buf); err != nil {
			return !errors.Is(err, os.ErrDeadlineExceeded)
		}
	}
}

func hello(id string) *linkpb.Frame {
	return &linkpb.Frame{Seq: 1, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: id, Type: "round", Firmware: "0.4.0"}}}
}

func setupBoards() []fakeBoard {
	return []fakeBoard{
		{ID: periph1, Name: "deck-3fa9c1", RSSI: -50, Info: "board=round;id=" + board1 + ";fw=0.4.0;paired=0", Sock: "1.sock"},
		{ID: periph2, Name: "deck-3fa9c2", RSSI: -70, Info: "board=round;id=" + board2 + ";fw=0.4.0;paired=0", Sock: "2.sock"},
	}
}

// add adds periph1 and plays its board through Hello and the first Welcome.
func (r *rig) add() (*board, devices.Device) {
	r.t.Helper()
	r.scan()
	var mu sync.Mutex
	var stages []string
	type res struct {
		d   devices.Device
		err error
	}
	done := make(chan res, 1)
	go func() {
		d, err := r.up.Add(context.Background(), periph1, devices.Act, func(stage, msg string) {
			mu.Lock()
			stages = append(stages, stage)
			mu.Unlock()
		})
		done <- res{d, err}
	}()
	b := r.accept(periph1)
	b.send(hello(board1))
	w := b.next().GetWelcome()
	if w == nil {
		r.t.Fatal("no welcome")
	}
	got := <-done
	if got.err != nil {
		r.t.Fatal(got.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(stages, ",") != "connect,passkey,connected,done" {
		r.t.Fatalf("stages: %v", stages)
	}
	return b, got.d
}

// ---- tests ------------------------------------------------------------------------

func TestNoHelper(t *testing.T) {
	c := &Central{}
	c.Run(context.Background()) // returns at once
	st := c.Status()
	if st.Available || st.Helper != "none" || st.Why != NotHere {
		t.Fatalf("status: %+v", st)
	}
	if err := c.Scan(true); err == nil || err.Error() != NotHere {
		t.Fatalf("scan: %v", err)
	}
	if _, err := c.Connect(context.Background(), periph1, nil); err == nil || err.Error() != NotHere {
		t.Fatalf("connect: %v", err)
	}
	u := &Uplinks{Central: c, Devices: &devices.Registry{Path: filepath.Join(t.TempDir(), "d.json")}}
	if _, err := u.Add(context.Background(), periph1, devices.Act, nil); err == nil || err.Error() != NotHere {
		t.Fatalf("add: %v", err)
	}
	u.reconcile(context.Background()) // nothing to do, nothing breaks
	if (*Uplinks)(nil).Status().Why != NotHere {
		t.Fatal("nil uplinks")
	}
	t.Setenv(EnvHelper, "none")
	if Find() != "" {
		t.Fatal("none turns it off")
	}
}

func TestScanAndList(t *testing.T) {
	r := newRig(t, setupBoards())
	r.scan()
	n := r.central.Nearby()
	if len(n) != 2 || n[0].Name != "deck-3fa9c1" || n[0].BoardID != board1 || !n[0].Setup || n[0].RSSI != -50 || n[1].ID != periph2 {
		t.Fatalf("nearby: %+v", n)
	}
	if !r.central.Status().Scanning {
		t.Fatal("not scanning")
	}
	_ = r.central.Scan(false)
	r.waitFor("scan stopped", func() bool { return !r.central.Status().Scanning })
	// Seeing a board connects to nothing and adds nothing.
	time.Sleep(150 * time.Millisecond)
	if len(r.srv.Devices.List()) != 0 || r.count("connect "+periph1)+r.count("connect "+periph2) != 0 {
		t.Fatalf("a board seen was connected to or added: %v\n%s", r.srv.Devices.List(), r.commands())
	}
}

func TestAddAndSession(t *testing.T) {
	r := newRig(t, setupBoards())
	b, d := r.add()
	if d.Name != "round-3fa9c1" || !d.Approved || d.Scope != devices.Act || d.BLE != periph1 || d.BoardID != board1 {
		t.Fatalf("device: %+v", d)
	}
	// The usual session: subscribe, snapshot, a command, the audit log.
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{SlotCount: 16, Fields: 1}}})
	if sb := b.next().GetSnapshotBegin(); sb == nil || sb.AgentCount != 1 {
		t.Fatalf("snapshot: %+v", sb)
	}
	if a := b.next().GetAgentChanged().GetAgent(); a.GetId() != "a1" {
		t.Fatalf("agent: %+v", a)
	}
	if b.next().GetSnapshotEnd() == nil {
		t.Fatal("no snapshot end")
	}
	b.send(&linkpb.Frame{Seq: 3, Body: &linkpb.Frame_Focus{Focus: &linkpb.Focus{AgentId: "a1"}}})
	if res := b.next().GetResult(); res.GetCode() != 204 {
		t.Fatalf("focus: %+v", res)
	}
	if !r.ctl.has("focus a1") {
		t.Fatal("focus didn't reach the controller")
	}
	links := r.hub.Links()
	if len(links) != 1 || links[0].Transport != link.OverBLE || links[0].Where != "bluetooth deck-3fa9c1" || !links[0].Approved {
		t.Fatalf("links: %+v", links)
	}
	if ls := r.up.State(d); ls.State != "connected" {
		t.Fatalf("state: %+v", ls)
	}
	audit, _ := os.ReadFile(r.srv.Audit.Path)
	for _, want := range []string{`"device":"round-3fa9c1","action":"approve"`, `"action":"bluetooth add round-3fa9c1"`, `"action":"POST /v1/agents/a1/focus","status":204`} {
		if !strings.Contains(string(audit), want) {
			t.Errorf("audit lacks %s:\n%s", want, audit)
		}
	}
	// Firmware isn't offered over Bluetooth.
	r.hub.SetFirmwareSource(noImages{})
	err := r.hub.OfferFirmware(d.Name, "round", "0.5.0")
	if !errors.Is(err, link.ErrOnBluetooth) || !errors.Is(err, link.ErrNotOnWifi) || !strings.Contains(err.Error(), "Bluetooth") {
		t.Fatalf("offer over Bluetooth: %v", err)
	}
	// Adding it again is refused.
	if _, err := r.up.Add(context.Background(), periph1, devices.Act, nil); err == nil {
		t.Fatal("added twice")
	}
}

type noImages struct{}

func (noImages) OTAImage(context.Context, string, string) (string, int64, string, error) {
	return "", 0, "", errors.New("no images in this test")
}

func TestAddPasskeyFails(t *testing.T) {
	boards := setupBoards()
	boards[0].Passkey = "fail"
	r := newRig(t, boards)
	r.scan()
	var stages []string
	_, err := r.up.Add(context.Background(), periph1, devices.Act, func(stage, msg string) { stages = append(stages, stage) })
	if err == nil || !strings.Contains(err.Error(), "pairing failed") {
		t.Fatalf("add: %v", err)
	}
	if strings.Join(stages, ",") != "connect,passkey" {
		t.Fatalf("stages: %v", stages)
	}
	if len(r.srv.Devices.List()) != 0 {
		t.Fatalf("a board that failed pairing was registered: %v", r.srv.Devices.List())
	}
	if !strings.Contains(r.commands(), "forget "+periph1) {
		t.Fatalf("not forgotten:\n%s", r.commands())
	}
	// Admin scope is never given here; unknown boards are refused.
	if _, err := r.up.Add(context.Background(), periph2, devices.Admin, nil); err == nil {
		t.Fatal("admin scope accepted")
	}
	if _, err := r.up.Add(context.Background(), "6B1F2C3D-0000-4000-8000-0000000000FF", devices.Act, nil); err == nil || !strings.Contains(err.Error(), "scan first") {
		t.Fatalf("unknown board: %v", err)
	}
}

func TestAddRefusesAnotherBoardsHello(t *testing.T) {
	r := newRig(t, setupBoards())
	r.scan()
	done := make(chan error, 1)
	go func() {
		_, err := r.up.Add(context.Background(), periph1, devices.Act, nil)
		done <- err
	}()
	b := r.accept(periph1)
	b.send(hello(board2)) // its Info said board1
	if err := <-done; err == nil || !strings.Contains(err.Error(), "said Hello as") {
		t.Fatalf("add: %v", err)
	}
	for _, d := range r.srv.Devices.List() {
		if d.Approved || d.BLE != "" {
			t.Fatalf("approved or bound: %+v", d)
		}
	}
}

func TestRevokeDropsAndForgets(t *testing.T) {
	r := newRig(t, setupBoards())
	b, d := r.add()
	if err := r.srv.Devices.Revoke(d.Name); err != nil {
		t.Fatal(err)
	}
	if !b.closed() {
		t.Fatal("the Bluetooth link stayed up after revoke")
	}
	r.waitFor("forgotten", func() bool { return r.count("forget "+periph1) == 1 })
	if nd, _ := r.srv.Devices.Get(d.Name); nd.BLE != "" || !nd.Revoked {
		t.Fatalf("after revoke: %+v", nd)
	}
	// Approving it again doesn't bring Bluetooth back: it is added again, with the passkey.
	_ = r.srv.Devices.Approve(d.Name)
	time.Sleep(200 * time.Millisecond)
	if r.count("connect "+periph1) != 1 {
		t.Fatalf("reconnected without being added again:\n%s", r.commands())
	}
}

func TestHelperCrashAndRestart(t *testing.T) {
	r := newRig(t, setupBoards())
	b, d := r.add()
	r.central.mu.Lock()
	p := r.central.proc
	r.central.mu.Unlock()
	_ = p.Kill()
	if !b.closed() {
		t.Fatal("the link outlived the helper")
	}
	r.waitFor("restarted", func() bool { st := r.central.Status(); return st.Restarts >= 1 && st.Available })
	// The board is known: it reconnects by itself, without a passkey prompt or approval.
	r.setBoards(func() []fakeBoard { bs := setupBoards(); bs[0].Paired = true; return bs }())
	b2 := r.accept(periph1)
	b2.send(hello(board1))
	if w := b2.next().GetWelcome(); w == nil || !w.Approved || w.DeviceName != d.Name {
		t.Fatalf("welcome after restart: %+v", w)
	}
	// Speaking for another board on a known link is refused.
	b2.send(hello(board2))
	if !b2.closed() {
		t.Fatal("a known link said Hello as another board and stayed")
	}
}

// wifi plays a Wi-Fi session of the board on the same hub.
func (r *rig) wifi(ctx context.Context, boardID string) (*board, chan error) {
	deckEnd, boardEnd := net.Pipe()
	s := link.NewSession(link.ServerDeck{S: r.srv}, deckEnd, "test@wifi")
	s.Hub, s.Transport, s.Where = r.hub, link.OverWifi, "192.0.2.7"
	s.RecheckEvery = 50 * time.Millisecond
	ended := make(chan error, 1)
	go func() { ended <- s.Run(ctx); deckEnd.Close() }()
	b := &board{t: r.t, conn: boardEnd}
	r.t.Cleanup(func() { boardEnd.Close() })
	b.send(hello(boardID))
	return b, ended
}

func TestUplinkPreferenceAndHandOver(t *testing.T) {
	r := newRig(t, setupBoards())
	ble, d := r.add()
	r.setBoards(func() []fakeBoard { bs := setupBoards(); bs[0].Paired = true; return bs }())

	// auto: Wi-Fi beats Bluetooth. The Bluetooth link closes and stays closed.
	ctx, cancel := context.WithCancel(context.Background())
	wifi, wifiEnded := r.wifi(ctx, board1)
	if w := wifi.next().GetWelcome(); w == nil || !w.Approved {
		t.Fatalf("wifi welcome: %+v", w)
	}
	if !ble.closed() {
		t.Fatal("Bluetooth stayed up beside Wi-Fi")
	}
	r.waitFor("standing by", func() bool { return r.up.State(d).State == "standing by" && r.up.State(d).Uses == link.OverWifi })
	time.Sleep(150 * time.Millisecond)
	if n := r.count("connect " + periph1); n != 1 {
		t.Fatalf("Bluetooth reconnected while Wi-Fi is in use (%d connects)", n)
	}

	// ble: Bluetooth comes back and Wi-Fi is closed.
	if err := r.srv.Devices.SetUplink(d.Name, "ble"); err != nil {
		t.Fatal(err)
	}
	ble2 := r.accept(periph1)
	ble2.send(hello(board1))
	if w := ble2.next().GetWelcome(); w == nil || !w.Approved {
		t.Fatalf("ble welcome: %+v", w)
	}
	select {
	case err := <-wifiEnded:
		if !errors.Is(err, link.ErrHandedOver) {
			t.Fatalf("wifi ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wi-Fi stayed up with uplink ble")
	}
	// A Wi-Fi Hello while Bluetooth is preferred is turned away.
	_, ended := r.wifi(ctx, board1)
	if err := <-ended; !errors.Is(err, link.ErrHandedOver) {
		t.Fatalf("second wifi: %v", err)
	}

	// auto again, Wi-Fi gone: Bluetooth stays (it is the only uplink).
	_ = r.srv.Devices.SetUplink(d.Name, "auto")
	time.Sleep(150 * time.Millisecond)
	if ls := r.up.State(d); ls.State != "connected" {
		t.Fatalf("state: %+v", ls)
	}
	cancel()
	if links := r.hub.Links(); len(links) != 1 || links[0].Transport != link.OverBLE {
		t.Fatalf("one session per board: %+v", links)
	}
}
