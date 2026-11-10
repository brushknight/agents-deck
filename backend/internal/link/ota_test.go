package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

// fakeSource is the release catalog: one app image per board and version.
type fakeSource struct {
	dir    string
	images map[string][]byte // "board/version"
	types  map[string][]string
}

func newFakeSource(t *testing.T) *fakeSource {
	return &fakeSource{dir: t.TempDir(), images: map[string][]byte{}, types: map[string][]string{}}
}

func (f *fakeSource) add(t *testing.T, board, version string, size int, types ...string) []byte {
	t.Helper()
	img := make([]byte, size)
	_, _ = rand.Read(img)
	img[0] = 0xE9 // an ESP app image starts so
	f.images[board+"/"+version] = img
	if err := os.WriteFile(filepath.Join(f.dir, board+"-"+version+".app.bin"), img, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(types) > 0 {
		f.types[board] = types
	}
	return img
}

func (f *fakeSource) OTAImage(_ context.Context, board, version string) (string, int64, string, error) {
	img, ok := f.images[board+"/"+version]
	if !ok {
		return "", 0, "", errors.New("not in the catalog")
	}
	sum := sha256.Sum256(img)
	return filepath.Join(f.dir, board+"-"+version+".app.bin"), int64(len(img)), hex.EncodeToString(sum[:]), nil
}

func (f *fakeSource) BoardTypes(_ context.Context, board string) ([]string, error) {
	return f.types[board], nil
}

// otaListener: the network listener with /v1/link and /v1/firmware.
func otaListener(t *testing.T, srv *server.Server, hub *Hub) string {
	t.Helper()
	cert := withCA(t, srv)
	srv.DeviceFirmware = FirmwareHandler(ServerDeck{srv}, hub)
	return listener(t, srv, cert, hub)
}

// httpsAs is a board's HTTPS client: TLS 1.3 with its certificate (nil: none).
func httpsAs(srv *server.Server, cert *tls.Certificate) *http.Client {
	cfg := &tls.Config{RootCAs: srv.CA.Pool(), ServerName: "deck.local", MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 10 * time.Second}
}

func get(c *http.Client, addr, path, rng string) (int, []byte, http.Header, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://"+addr+path, nil)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header, err
}

// wifiBoard is a Go board on Wi-Fi: TLS with its certificate, /v1/link, Hello.
func wifiBoard(t *testing.T, addr string, srv *server.Server, cert *tls.Certificate, boardID, version string,
	firmware func(b *Board, o *linkpb.FirmwareOffer)) (*Board, func()) {
	t.Helper()
	conn, br, err := dialLink(t, addr, srv, cert)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoard(struct {
		io.Reader
		io.Writer
	}{br, conn}, &linkpb.Hello{Protocol: 1, BoardId: boardID, Type: "round", Firmware: version}, 1)
	if firmware != nil {
		b.Firmware = func(o *linkpb.FirmwareOffer) { go firmware(b, o) }
	}
	go func() { _ = b.Run() }()
	return b, func() { conn.Close() }
}

func status(b *Board, st linkpb.FirmwareState, pct uint32, msg string) {
	b.Send(&linkpb.Frame{Body: &linkpb.Frame_FirmwareStatus{FirmwareStatus: &linkpb.FirmwareStatus{State: st, Percent: pct, Error: msg}}})
}

func waitJob(t *testing.T, hub *Hub, name, state string) FirmwareJob {
	t.Helper()
	var j FirmwareJob
	for i := 0; i < 250; i++ {
		if j, _ = hub.FirmwareJob(name); j.State == state {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s's job: %+v, want %s", name, j, state)
	return j
}

// The whole update over Wi-Fi: the offer, the download with the board's
// certificate (resumed with Range), the status, and the new version's Hello.
func TestFirmwareOverWifi(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	addr := otaListener(t, srv, hub)
	name, cert := pairedBoard(t, srv, "AA:BB:CC:00:00:51", devices.View)
	src := newFakeSource(t)
	img := src.add(t, "round-board", "0.2.0", 300<<10, "round")
	var events []FirmwareJob
	var emu sync.Mutex
	hub.OnFirmware = func(j FirmwareJob) { emu.Lock(); events = append(events, j); emu.Unlock() }

	// No source yet: a clean "no firmware source", on both sides.
	got := make(chan []byte, 1)
	b, closeB := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:51", "0.1.0", func(b *Board, o *linkpb.FirmwareOffer) {
		c := httpsAs(srv, &cert)
		// Half, then the rest with Range, as a board resumes after a drop.
		_, first, _, err := get(c, addr, o.Path, "bytes=0-99999")
		if err != nil || len(first) != 100000 {
			status(b, linkpb.FirmwareState_FIRMWARE_FAILED, 0, fmt.Sprintf("first part: %d %v", len(first), err))
			return
		}
		status(b, linkpb.FirmwareState_FIRMWARE_DOWNLOADING, 33, "")
		code, rest, h, err := get(c, addr, o.Path, "bytes=100000-")
		if err != nil || code != http.StatusPartialContent {
			status(b, linkpb.FirmwareState_FIRMWARE_FAILED, 33, fmt.Sprintf("rest: %d %v", code, err))
			return
		}
		all := append(first, rest...)
		sum := sha256.Sum256(all)
		if uint64(len(all)) != o.Size || !bytes.Equal(sum[:], o.Sha256) || h.Get("X-Firmware-SHA256") != hex.EncodeToString(sum[:]) {
			status(b, linkpb.FirmwareState_FIRMWARE_FAILED, 100, "checksum")
			return
		}
		status(b, linkpb.FirmwareState_FIRMWARE_VERIFYING, 100, "")
		status(b, linkpb.FirmwareState_FIRMWARE_RESTARTING, 100, "")
		got <- all
	})
	waitFor(t, func() bool { s, _ := hub.wifiSession(name); return s != nil })
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); !errors.Is(err, ErrNoFirmwareSource) {
		t.Fatalf("offer without a source: %v", err)
	}
	if code, body, _, _ := get(httpsAs(srv, &cert), addr, "/v1/firmware/round-board/0.2.0", ""); code != 503 || !strings.Contains(string(body), "no firmware source") {
		t.Fatalf("download without a source: %d %s", code, body)
	}

	hub.SetFirmwareSource(src)
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	select {
	case all := <-got:
		if !bytes.Equal(all, img) {
			t.Fatal("the board got another image")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no download: %+v", hub.Firmware())
	}
	j := waitJob(t, hub, name, FirmwareRestarting)
	if j.Percent != 100 || j.From != "0.1.0" || j.Board != "round-board" {
		t.Fatalf("job: %+v", j)
	}
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); !errors.Is(err, ErrUpdating) {
		t.Fatalf("a second offer while it restarts: %v", err)
	}
	// It restarts into 0.2.0 and says Hello: done.
	closeB()
	_ = b
	_, closeB2 := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:51", "0.2.0", nil)
	defer closeB2()
	waitJob(t, hub, name, FirmwareDone)
	fi := hub.Firmware()
	if len(fi) != 1 || fi[0].Version != "0.2.0" || fi[0].Transport != OverWifi || fi[0].Type != "round" || fi[0].Job.State != FirmwareDone {
		t.Fatalf("firmware: %+v", fi)
	}
	emu.Lock()
	var states []string
	for _, e := range events {
		states = append(states, e.State)
	}
	emu.Unlock()
	if want := "offered downloading verifying restarting done"; strings.Join(states, " ") != want {
		t.Fatalf("events %v, want %s", states, want)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"device":"`+name+`","action":"GET /v1/firmware/round-board/0.2.0","status":206,"detail":"307200 bytes, bytes=100000-"`) {
		t.Fatalf("audit: %s", audit)
	}

	// The board now runs firmware that refuses (the kit without a handler).
	time.Sleep(50 * time.Millisecond)
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, hub, name, FirmwareRefused); !strings.Contains(j.Error, "can't update") {
		t.Fatalf("refused: %+v", j)
	}
	// Not in the catalog.
	if err := hub.OfferFirmware(name, "round-board", "9.9.9"); err == nil || !strings.Contains(err.Error(), "not in the catalog") {
		t.Fatalf("unknown version: %v", err)
	}
}

// After RESTARTING, a Hello with the old version: it went back.
func TestFirmwareRolledBack(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	addr := otaListener(t, srv, hub)
	name, cert := pairedBoard(t, srv, "AA:BB:CC:00:00:52", devices.View)
	src := newFakeSource(t)
	src.add(t, "round-board", "0.2.0", 4096, "round")
	hub.SetFirmwareSource(src)
	_, closeB := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:52", "0.1.0", func(b *Board, o *linkpb.FirmwareOffer) {
		status(b, linkpb.FirmwareState_FIRMWARE_RESTARTING, 100, "")
	})
	waitFor(t, func() bool { s, _ := hub.wifiSession(name); return s != nil })
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	waitJob(t, hub, name, FirmwareRestarting)
	closeB()
	_, closeB2 := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:52", "0.1.0", nil)
	defer closeB2()
	if j := waitJob(t, hub, name, FirmwareRolledBack); !strings.Contains(j.Error, "0.1.0") {
		t.Fatalf("job: %+v", j)
	}
}

// Who may download what, and to whom the deck offers.
func TestFirmwareRefusals(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	addr := otaListener(t, srv, hub)
	name, cert := pairedBoard(t, srv, "AA:BB:CC:00:00:53", devices.View)
	src := newFakeSource(t)
	src.add(t, "round-board", "0.2.0", 4096, "round")
	src.add(t, "other-board", "0.2.0", 4096, "other")
	hub.SetFirmwareSource(src)
	c := httpsAs(srv, &cert)

	// Its own board's image: yes. Another board's: no, both ways.
	if code, body, _, err := get(c, addr, "/v1/firmware/round-board/0.2.0", ""); code != 200 || len(body) != 4096 {
		t.Fatalf("own image: %d %d %v", code, len(body), err)
	}
	if code, body, _, _ := get(c, addr, "/v1/firmware/other-board/0.2.0", ""); code != 403 || !strings.Contains(string(body), "isn't for it") {
		t.Fatalf("another board's image: %d %s", code, body)
	}
	if code, _, _, _ := get(c, addr, "/v1/firmware/round-board/0.3.0", ""); code != 404 {
		t.Fatalf("unknown version: %d", code)
	}
	if code, _, _, _ := get(c, addr, "/v1/firmware/round-board", ""); code != 404 {
		t.Fatalf("no version: %d", code)
	}
	// Not connected, then on Wi-Fi with the wrong board.
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("offline: %v", err)
	}
	_, closeB := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:53", "0.1.0", nil)
	defer closeB()
	waitFor(t, func() bool { s, _ := hub.wifiSession(name); return s != nil })
	if err := hub.OfferFirmware(name, "other-board", "0.2.0"); !errors.Is(err, ErrWrongBoard) {
		t.Fatalf("wrong board: %v", err)
	}
	if err := hub.OfferFirmware(name, "../x", "0.2.0"); err == nil {
		t.Fatal("a path in the board name")
	}

	// No certificate: 401. A network device's certificate: not a board.
	if code, _, _, _ := get(httpsAs(srv, nil), addr, "/v1/firmware/round-board/0.2.0", ""); code != 401 {
		t.Fatalf("no certificate: %d", code)
	}
	pcert, pcertPEM, pkeyPEM, _ := srv.CA.NewDeviceKey("panel")
	_ = srv.Devices.AddNetwork("panel", devices.Admin, pki.SerialHex(pcert), pki.Fingerprint(pcert.Raw))
	panel, _ := tls.X509KeyPair(pcertPEM, pkeyPEM)
	if code, _, _, _ := get(httpsAs(srv, &panel), addr, "/v1/firmware/round-board/0.2.0", ""); code != 403 {
		t.Fatalf("a network device: %d", code)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"device":"panel","action":"GET /v1/firmware/round-board/0.2.0","status":403`) ||
		!strings.Contains(string(audit), `"action":"GET /v1/firmware/other-board/0.2.0","status":403`) {
		t.Fatalf("audit: %s", audit)
	}

	// Revoked: cut off, on the open connection and at the next handshake.
	_ = srv.Devices.Revoke(name)
	if code, _, _, err := get(c, addr, "/v1/firmware/round-board/0.2.0", ""); err == nil && code == 200 {
		t.Fatal("a revoked board downloaded")
	}
	if _, _, _, err := get(httpsAs(srv, &cert), addr, "/v1/firmware/round-board/0.2.0", ""); err == nil {
		t.Fatal("a revoked board's handshake")
	}
}

// A board on USB only is updated by the flasher, never offered.
func TestFirmwareNotOverSerial(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	src := newFakeSource(t)
	src.add(t, "round-board", "0.2.0", 4096, "round")
	hub.SetFirmwareSource(src)
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); deckEnd.Close(); boardEnd.Close() }()
	s := NewSession(ServerDeck{srv}, deckEnd, "test")
	s.Hub, s.RecheckEvery = hub, 50*time.Millisecond
	go func() { _ = s.Run(ctx) }()
	gb := NewBoard(boardEnd, &linkpb.Hello{Protocol: 1, BoardId: "AA:BB:CC:00:00:54", Type: "round", Firmware: "0.1.0"}, 1)
	go func() { _ = gb.Run() }()
	waitFor(t, func() bool { return len(hub.Links()) == 1 })
	name := hub.Links()[0].Board
	_ = srv.Devices.Approve(name)
	waitFor(t, func() bool { return hub.serial(name) != nil })
	if err := hub.OfferFirmware(name, "round-board", "0.2.0"); !errors.Is(err, ErrNotOnWifi) {
		t.Fatalf("over USB: %v", err)
	}
}

// The stream stops when the board's certificate stops counting.
func TestRevocableBody(t *testing.T) {
	ok := true
	r := &revocable{ReadSeeker: bytes.NewReader(make([]byte, 3*FirmwareCheckEvery)), check: func() bool { return ok }}
	buf := make([]byte, FirmwareCheckEvery)
	if n, err := r.Read(buf); n != FirmwareCheckEvery || err != nil {
		t.Fatal(n, err)
	}
	ok = false
	if _, err := r.Read(buf); err == nil || !r.cut {
		t.Fatal("read on after revocation")
	}
}

// End to end with the firmware kit: a board paired over USB takes the deck's
// offer over Wi-Fi, checks it, downloads the image with deck_fw.c (TLS 1.3,
// its certificate, a Range request), and reports; another board's image is
// refused by the board itself.
func TestFirmwareKitUpdateOverWifi(t *testing.T) {
	bin := boardWifi(t)
	srv, _ := testDeck(t)
	hub := &Hub{}
	addr := otaListener(t, srv, hub)
	_, port, _ := net.SplitHostPort(addr)
	srv.DeviceHosts = []string{"127.0.0.1"}
	fmt.Sscan(port, &srv.DevicePort)
	store := t.TempDir()
	const name = "board-wifi-000078"
	src := newFakeSource(t)
	img := src.add(t, "board-wifi", "0.2.0", 200<<10) // no BoardTyper entry: the type is the board's name
	src.add(t, "other-board", "0.2.0", 1024, "board-wifi")
	hub.SetFirmwareSource(src)

	// Paired over "USB serial".
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log, _ := runBoard(t, bin, func(in io.WriteCloser, out io.ReadCloser) {
		s := NewSession(ServerDeck{srv}, struct {
			io.Reader
			io.Writer
		}{out, in}, "board_wifi")
		s.Hub, s.RecheckEvery = hub, 50*time.Millisecond
		go func() { _ = s.Run(ctx) }()
	}, func(l string) {
		if strings.HasPrefix(l, "welcome ") && strings.Contains(l, "approved=0") {
			go func() { _ = srv.Devices.Approve(name) }()
		}
	}, "--serial", "--store", store, "--board-id", "AA:BB:CC:00:00:78", "--seconds", "2")
	if !strings.Contains(log, "paired serial=") {
		t.Fatalf("pairing:\n%s", log)
	}

	out := filepath.Join(t.TempDir(), "image.bin")
	offer := func(board string) func(string) {
		return func(l string) {
			if strings.HasPrefix(l, "snapshot_end") {
				go func() {
					if err := hub.OfferFirmware(name, board, "0.2.0"); err != nil {
						t.Errorf("offer: %v", err)
					}
				}()
			}
		}
	}
	log, code := runBoard(t, bin, nil, offer("board-wifi"), "--wifi", "--store", store, "--board-id", "AA:BB:CC:00:00:78",
		"--firmware-board", "board-wifi", "--firmware-out", out, "--seconds", "4")
	for _, want := range []string{"firmware offer board=board-wifi version=0.2.0 size=204800",
		"firmware range from=102400 status=206 length=102400 error=0", "firmware written bytes=204800"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log lacks %q (exit %d):\n%s", want, code, log)
		}
	}
	if b, _ := os.ReadFile(out); !bytes.Equal(b, img) {
		t.Fatal("the board wrote another image")
	}
	if j := waitJob(t, hub, name, FirmwareRestarting); j.Percent != 100 {
		t.Fatalf("job: %+v", j)
	}

	// The deck thinks other-board's image fits (its catalog says so); the board knows better.
	hub.noteJob(FirmwareJob{Device: name, State: FirmwareDone})
	log, _ = runBoard(t, bin, nil, offer("other-board"), "--wifi", "--store", store, "--board-id", "AA:BB:CC:00:00:78",
		"--firmware-board", "board-wifi", "--firmware-out", out, "--seconds", "2")
	if !strings.Contains(log, "firmware refused this is board-wifi, not other-board") {
		t.Fatalf("another board's image:\n%s", log)
	}
	if j := waitJob(t, hub, name, FirmwareRefused); !strings.Contains(j.Error, "not other-board") {
		t.Fatalf("job: %+v", j)
	}
}

// `agentctl link update`: an image file from this Mac, through the unix socket.
func TestFirmwareFromFile(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	addr := otaListener(t, srv, hub)
	srv.LinkFirmware, srv.LinkFirmwareStatus = hub.UpdateFromFile, func() any { return hub.Firmware() }
	unix := srv.UnixHandler(fakeLocal{&fakeCtl{}})
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		unix.ServeHTTP(w, httptest.NewRequest("POST", "/local/link/firmware", strings.NewReader(body)))
		return w
	}
	name, cert := pairedBoard(t, srv, "AA:BB:CC:00:00:55", devices.View)
	dir := t.TempDir()
	good := filepath.Join(dir, "firmware.bin")
	img := append([]byte{0xE9}, bytes.Repeat([]byte{7}, 5000)...)
	_ = os.WriteFile(good, img, 0o600)
	full := filepath.Join(dir, "full.bin")
	_ = os.WriteFile(full, []byte{0xFF, 1, 2}, 0o600)
	body := func(file string) string {
		return `{"device":"` + name + `","board":"round","version":"0.3.0","file":"` + file + `"}`
	}

	if w := post(body(good)); w.Code != 404 || !strings.Contains(w.Body.String(), "isn't connected") {
		t.Fatalf("offline: %d %s", w.Code, w.Body)
	}
	got := make(chan []byte, 1)
	_, closeB := wifiBoard(t, addr, srv, &cert, "AA:BB:CC:00:00:55", "0.1.0", func(b *Board, o *linkpb.FirmwareOffer) {
		_, data, _, _ := get(httpsAs(srv, &cert), addr, o.Path, "")
		got <- data
		status(b, linkpb.FirmwareState_FIRMWARE_RESTARTING, 100, "")
	})
	defer closeB()
	waitFor(t, func() bool { s, _ := hub.wifiSession(name); return s != nil })
	if w := post(body(full)); w.Code != 400 || !strings.Contains(w.Body.String(), "app image") {
		t.Fatalf("the full image: %d %s", w.Code, w.Body)
	}
	if w := post(body(good)); w.Code != 202 {
		t.Fatalf("offer: %d %s", w.Code, w.Body)
	}
	if data := <-got; !bytes.Equal(data, img) {
		t.Fatal("the board got another image")
	}
	waitJob(t, hub, name, FirmwareRestarting)
	w := httptest.NewRecorder()
	unix.ServeHTTP(w, httptest.NewRequest("GET", "/local/link/firmware", nil))
	if !strings.Contains(w.Body.String(), `"state":"restarting"`) {
		t.Fatalf("status: %s", w.Body)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"device":"local","action":"firmware `+name+`","status":202,"detail":"round 0.3.0"`) {
		t.Fatalf("audit: %s", audit)
	}
}
