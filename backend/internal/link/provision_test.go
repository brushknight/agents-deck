package link

import (
	"bytes"
	"context"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

const testPort = "/dev/cu.usbmodem-test"

// usbBoard is a fake round board on a registered USB port, as the firmware
// behaves: once approved it pairs (CertRequest), then reports its network
// (NetStatus); given Wi-Fi credentials it answers 204, tries them and reports
// how it went (try).
type usbBoard struct {
	*Board
	t    *testing.T
	mu   sync.Mutex
	cert bool
	ssid string
	got  []*linkpb.WifiConfig
	try  func(w *linkpb.WifiConfig) linkpb.WifiState // the outcome of trying a network
}

func (b *usbBoard) status(w linkpb.WifiState, ip string) {
	b.mu.Lock()
	n := &linkpb.NetStatus{Transport: linkpb.Transport_TRANSPORT_SERIAL, Wifi: w, Ssid: b.ssid, Ip: ip, HasCert: b.cert}
	b.mu.Unlock()
	b.Send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: n}})
}

// resting: what it reports with no test going on.
func (b *usbBoard) resting() linkpb.WifiState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ssid == "" {
		return linkpb.WifiState_WIFI_NOT_SET
	}
	return linkpb.WifiState_WIFI_IDLE
}

func (b *usbBoard) configs() []*linkpb.WifiConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*linkpb.WifiConfig(nil), b.got...)
}

func (b *usbBoard) welcomed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cert
}

// plugIn runs a session on a registered port (as Manager does) with the fake
// board at the other end. It returns the board and a function that unplugs it.
func plugIn(t *testing.T, m *Manager, boardID string, startSSID string, try func(*linkpb.WifiConfig) linkpb.WifiState) (*usbBoard, func()) {
	t.Helper()
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	s := m.portSession(deckEnd, PortConfig{Path: testPort})
	s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond
	done := make(chan struct{})
	go func() { _ = s.Run(ctx); close(done) }()
	b := &usbBoard{t: t, ssid: startSSID, try: try}
	b.Board = NewBoard(boardEnd, &linkpb.Hello{Protocol: 1, BoardId: boardID, Type: "waveshare-touch-lcd-21", Firmware: "1"}, 1)
	b.Wifi = func(w *linkpb.WifiConfig) (uint32, string) {
		b.mu.Lock()
		b.got = append(b.got, w)
		b.ssid = w.Ssid
		b.mu.Unlock()
		return 204, ""
	}
	b.Event = func(f *linkpb.Frame) {
		switch x := f.Body.(type) {
		case *linkpb.Frame_Welcome:
			if x.Welcome.Approved && !b.welcomed() {
				_, csr := csrFor(t)
				b.Send(certReq(0, csr))
			}
			b.status(b.resting(), "")
		case *linkpb.Frame_CertIssued:
			b.mu.Lock()
			b.cert = true
			b.mu.Unlock()
			b.status(b.resting(), "")
		case *linkpb.Frame_WifiConfig: // the Result has gone: now it tries the network
			go func() {
				b.status(linkpb.WifiState_WIFI_JOINING, "")
				r := b.try(x.WifiConfig)
				ip := ""
				if r == linkpb.WifiState_WIFI_IDLE {
					ip = "192.0.2.50"
				}
				b.status(r, ip)
			}()
		}
	}
	go func() { _ = b.Run() }()
	return b, func() { cancel(); deckEnd.Close(); boardEnd.Close(); <-done }
}

func works(*linkpb.WifiConfig) linkpb.WifiState { return linkpb.WifiState_WIFI_IDLE }

// provisionDeck: a deck with a CA (boards can pair), a default network in a
// file keystore, and a manager for registered ports.
func provisionDeck(t *testing.T) (*server.Server, *Manager, *WifiDefault, string) {
	t.Helper()
	srv, _ := testDeck(t)
	withCA(t, srv)
	keys := filepath.Join(t.TempDir(), "keys")
	wd := &WifiDefault{Store: pki.FileKeystore{Dir: keys}}
	hub := &Hub{Wifi: wd}
	srv.LinkWifi = hub.SendWifi
	srv.LinkWifiUseDefault = hub.SendDefault
	srv.LinkWifiDefault = func(ssid, password string, forget bool) error {
		if forget {
			return wd.Clear()
		}
		return wd.Set(ssid, password)
	}
	return srv, &Manager{Deck: ServerDeck{srv}, Hub: hub, Wifi: wd}, wd, keys
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	var buf syncBuffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func boardLink(hub *Hub, name string) LinkInfo {
	for _, l := range hub.Links() {
		if l.Board == name {
			return l
		}
	}
	return LinkInfo{}
}

// The whole chain with no commands: a new board plugged into a registered
// port is approved (scope act), pairs for Wi-Fi and gets the default network;
// the password reaches the board and nowhere else.
func TestUSBBoardProvisionsItself(t *testing.T) {
	logs := captureLog(t)
	srv, m, _, keys := provisionDeck(t)
	unix := srv.UnixHandler(fakeLocal{&fakeCtl{}})
	w := httptest.NewRecorder()
	unix.ServeHTTP(w, httptest.NewRequest("POST", "/local/link/wifi-default", strings.NewReader(`{"ssid":"lab","password":"s3cret-pass"}`)))
	if w.Code != 204 {
		t.Fatalf("set default: %d %s", w.Code, w.Body)
	}
	if st, err := os.Stat(filepath.Join(keys, wifiDefaultKey)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the default's file: %v %v", st, err)
	}

	b, unplug := plugIn(t, m, "AA:BB:CC:00:01:01", "", works)
	defer unplug()
	waitFor(t, func() bool { return len(b.configs()) == 1 })
	if c := b.configs()[0]; c.Ssid != "lab" || c.Password != "s3cret-pass" || c.Forget {
		t.Fatalf("the board got %v", c)
	}
	name := hub(m).Links()[0].Board
	d, _ := srv.Devices.Get(name)
	if !d.Approved || d.Scope != devices.Act || d.CertSerial == "" {
		t.Fatalf("registry: %+v", d)
	}
	waitFor(t, func() bool { return boardLink(hub(m), name).DefaultWifi == "default lab: ok" })

	audit, _ := os.ReadFile(srv.Audit.Path)
	for _, want := range []string{`"device":"` + name + `","action":"approve","status":200,"detail":"approved automatically over USB on ` + testPort + `, scope act"`,
		`"action":"pair (deck link)"`, `"action":"wifi default set","status":204`} {
		if !strings.Contains(string(audit), want) {
			t.Errorf("audit lacks %s:\n%s", want, audit)
		}
	}
	if strings.Contains(string(audit), "s3cret") || strings.Contains(logs.String(), "s3cret") {
		t.Fatalf("the password leaked:\naudit %s\nlog %s", audit, logs)
	}
	if !strings.Contains(logs.String(), `wifi: sent default network "lab" to `+name+`: ok`) {
		t.Errorf("log: %s", logs)
	}
	// Only once: more reports in the same session send nothing more.
	b.status(linkpb.WifiState_WIFI_IDLE, "")
	time.Sleep(150 * time.Millisecond)
	if n := len(b.configs()); n != 1 {
		t.Fatalf("%d WifiConfigs", n)
	}
	unplug()
	// Plugged in again with the network: nothing to send.
	b2, unplug2 := plugIn(t, m, "AA:BB:CC:00:01:01", "lab", works)
	defer unplug2()
	waitFor(t, func() bool { return b2.welcomed() })
	time.Sleep(200 * time.Millisecond)
	if n := len(b2.configs()); n != 0 {
		t.Fatalf("a board with the default got it again (%d)", n)
	}
}

func hub(m *Manager) *Hub { return m.Hub }

// A board that fails with the default (wrong password) gets it once per
// connection, not in a loop; plugged in again it is tried again.
func TestUSBDefaultWifiNoLoop(t *testing.T) {
	srv, m, wd, _ := provisionDeck(t)
	_ = wd.Set("lab", "wrong-pass")
	_ = srv
	wrong := func(*linkpb.WifiConfig) linkpb.WifiState { return linkpb.WifiState_WIFI_WRONG_PASSWORD }
	b, unplug := plugIn(t, m, "AA:BB:CC:00:02:02", "", wrong)
	waitFor(t, func() bool { return len(b.configs()) == 1 })
	name := hub(m).Links()[0].Board
	waitFor(t, func() bool { return boardLink(hub(m), name).DefaultWifi == "default lab: wrong password" })
	for i := 0; i < 3; i++ {
		b.status(linkpb.WifiState_WIFI_WRONG_PASSWORD, "")
		b.status(linkpb.WifiState_WIFI_NOT_SET, "")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(b.configs()); n != 1 {
		t.Fatalf("%d WifiConfigs in one session", n)
	}
	unplug()
	// Next connection: tried again (and now it works: the owner fixed the password).
	_ = wd.Set("lab", "right-pass")
	b2, unplug2 := plugIn(t, m, "AA:BB:CC:00:02:02", "lab", works)
	defer unplug2()
	waitFor(t, func() bool { return len(b2.configs()) == 1 })
	if c := b2.configs()[0]; c.Password != "right-pass" {
		t.Fatalf("got %v", c)
	}
	waitFor(t, func() bool { return boardLink(hub(m), name).DefaultWifi == "default lab: ok" })
}

// A board given its own network keeps it; --use-default sends the default.
func TestUSBDefaultWifiOverride(t *testing.T) {
	srv, m, wd, _ := provisionDeck(t)
	unix := srv.UnixHandler(fakeLocal{&fakeCtl{}})
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		unix.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}
	d, _ := srv.Devices.Board("AA:BB:CC:00:03:03", "waveshare-touch-lcd-21", "1")
	_ = srv.Devices.Approve(d.Name)
	// Its own network, set by hand while plugged in.
	b, unplug := plugIn(t, m, "AA:BB:CC:00:03:03", "", works)
	waitFor(t, func() bool { return b.welcomed() && m.Hub.serial(d.Name) != nil })
	if w := post("/local/link/wifi", `{"device":"`+d.Name+`","ssid":"studio","password":"own-pass-1"}`); w.Code != 204 {
		t.Fatalf("own network: %d %s", w.Code, w.Body)
	}
	if got, _ := srv.Devices.Get(d.Name); !got.WifiOwn {
		t.Fatal("the override wasn't recorded")
	}
	unplug()
	_ = wd.Set("lab", "s3cret-pass") // the default comes after
	b2, unplug2 := plugIn(t, m, "AA:BB:CC:00:03:03", "studio", works)
	waitFor(t, func() bool { return b2.welcomed() })
	time.Sleep(200 * time.Millisecond)
	if len(b2.configs()) != 0 {
		t.Fatalf("the default replaced the board's own network: %v", b2.configs())
	}
	// Back to the default, while plugged in: sent at once.
	w := post("/local/link/wifi", `{"device":"`+d.Name+`","useDefault":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"sent":true`) {
		t.Fatalf("use default: %d %s", w.Code, w.Body)
	}
	waitFor(t, func() bool { return len(b2.configs()) == 1 && b2.configs()[0].Ssid == "lab" })
	waitFor(t, func() bool { return boardLink(hub(m), d.Name).DefaultWifi == "default lab: ok" })
	if got, _ := srv.Devices.Get(d.Name); got.WifiOwn {
		t.Fatal("still marked as its own network")
	}
	unplug2()
	audit, _ := os.ReadFile(srv.Audit.Path)
	if strings.Contains(string(audit), "s3cret") || strings.Contains(string(audit), "own-pass") {
		t.Fatalf("audit: %s", audit)
	}
}

// Approval stays required where it should: the setting off leaves a new
// board waiting (and with no Wi-Fi); a revoked board isn't approved again;
// a board that isn't on a registered port (link sim, Wi-Fi) never is.
func TestUSBAutoApproveLimits(t *testing.T) {
	srv, m, wd, _ := provisionDeck(t)
	_ = wd.Set("lab", "s3cret-pass")

	if err := srv.Devices.SetAutoApprove(false, devices.Act); err != nil {
		t.Fatal(err)
	}
	b, unplug := plugIn(t, m, "AA:BB:CC:00:04:04", "", works)
	waitFor(t, func() bool { return len(hub(m).Links()) == 1 })
	name := hub(m).Links()[0].Board
	b.mu.Lock()
	b.cert = true // claims a certificate: still nothing for an unapproved board
	b.mu.Unlock()
	b.status(linkpb.WifiState_WIFI_NOT_SET, "")
	time.Sleep(300 * time.Millisecond)
	if d, _ := srv.Devices.Get(name); d.Approved || len(b.configs()) != 0 {
		t.Fatalf("setting off: approved %v, wifi %v", d.Approved, b.configs())
	}
	unplug()
	// The setting persists with the registry.
	r2 := &devices.Registry{Path: srv.Devices.Path}
	if s := r2.Settings().AutoApprove; s.USB || s.Scope != devices.Act {
		t.Fatalf("settings after reload: %+v", s)
	}
	if err := srv.Devices.SetAutoApprove(true, devices.Admin); err == nil {
		t.Fatal("admin by auto-approval")
	}
	_ = srv.Devices.SetAutoApprove(true, devices.View)

	// Now on: the waiting board is approved when it says Hello again, with scope view.
	b2, unplug2 := plugIn(t, m, "AA:BB:CC:00:04:04", "", works)
	waitFor(t, func() bool { d, _ := srv.Devices.Get(name); return d.Approved })
	if d, _ := srv.Devices.Get(name); d.Scope != devices.View {
		t.Fatalf("scope %s", d.Scope)
	}
	_ = b2
	unplug2()

	// Revoked: it stays revoked.
	_ = srv.Devices.Revoke(name)
	b3, unplug3 := plugIn(t, m, "AA:BB:CC:00:04:04", "", works)
	waitFor(t, func() bool { return len(hub(m).Links()) == 1 })
	time.Sleep(200 * time.Millisecond)
	if d, _ := srv.Devices.Get(name); !d.Revoked || d.Active() || len(b3.configs()) != 0 {
		t.Fatalf("a revoked board came back: %+v", d)
	}
	unplug3()

	// Not a registered port: `agentctl link sim`, and Wi-Fi.
	for _, tc := range []struct {
		name string
		set  func(s *Session)
	}{
		{"sim", func(s *Session) {}},
		{"wifi", func(s *Session) { s.Transport, s.USBPort = OverWifi, testPort }},
	} {
		deckEnd, boardEnd := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		s := NewSession(ServerDeck{srv}, deckEnd, tc.name)
		tc.set(s)
		go func() { _ = s.Run(ctx) }()
		bb := &board{t: t, conn: boardEnd}
		bb.send(hi("AA:BB:CC:00:05:0" + tc.name[:1]))
		if w := bb.next().GetWelcome(); w == nil || w.Approved {
			t.Fatalf("%s: %v", tc.name, w)
		}
		cancel()
		deckEnd.Close()
		boardEnd.Close()
	}
}

// The manager's sessions are the USB ones.
func TestPortSessionIsUSB(t *testing.T) {
	m := &Manager{Hub: &Hub{}, Wifi: &WifiDefault{}}
	s := m.portSession(nil, PortConfig{Path: testPort})
	if s.USBPort != testPort || s.Wifi != m.Wifi || s.Transport != OverSerial || s.Hub != m.Hub {
		t.Fatalf("%+v", s)
	}
}
