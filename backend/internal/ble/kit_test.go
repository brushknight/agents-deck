package ble

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// End to end over Bluetooth with the firmware kit's own code: board_wifi
// --ble (firmware/deck-link/test/host) is a board whose Bluetooth link is a
// Unix socket, run through deck_ble.c, deck_link.c and deck_pair.c as on a
// board. The fake helper (fakehelper_test.go) is the shim between the two: it
// speaks the helper's JSON lines to the service and carries the bytes to the
// board's socket in pieces of the link's MTU - 3, as deck-ble does over the
// air. What it can't play is the radio itself: pairing and encryption happen
// in macOS and the board's Bluetooth stack, so the passkey step is only the
// event the person sees.

// kitBoard builds board_wifi into a folder of this test's own (the link
// package's tests build theirs at the same time), or skips.
func kitBoard(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("no make")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler")
	}
	host, _ := filepath.Abs("../../../firmware/deck-link/test/host")
	out := t.TempDir()
	if b, err := exec.Command("make", "-s", "-C", host, "OUT="+out, "board_wifi").CombinedOutput(); err != nil {
		t.Skipf("can't build board_wifi (needs a C compiler and mbedTLS 3): %v\n%s", err, b)
	}
	return filepath.Join(out, "board_wifi")
}

// boardLog is a running board's stderr, one event per line.
type boardLog struct {
	t     *testing.T
	mu    sync.Mutex
	lines []string
}

func (l *boardLog) read(r *bufio.Scanner) {
	for r.Scan() {
		l.mu.Lock()
		l.lines = append(l.lines, r.Text())
		l.mu.Unlock()
	}
}

func (l *boardLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// count is how many lines start with prefix.
func (l *boardLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// wait returns the first line holding want, waiting up to d.
func (l *boardLog) wait(want string, d time.Duration) string {
	l.t.Helper()
	deadline := time.Now().Add(d)
	for {
		l.mu.Lock()
		for _, s := range l.lines {
			if strings.Contains(s, want) {
				l.mu.Unlock()
				return s
			}
		}
		l.mu.Unlock()
		if time.Now().After(deadline) {
			l.t.Fatalf("the board didn't say %q:\n%s", want, l.all())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startBoard runs board_wifi with args until the test ends.
func startBoard(t *testing.T, bin string, args ...string) (*boardLog, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	errp, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	l := &boardLog{t: t}
	done := make(chan struct{})
	go func() { l.read(bufio.NewScanner(errp)); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done; _ = cmd.Wait() })
	return l, cmd
}

func TestBluetoothEndToEndWithTheKit(t *testing.T) {
	bin := kitBoard(t)
	dir, err := os.MkdirTemp("/tmp", "blekit") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// The deck: 32 agents, a CA (boards pair for Wi-Fi), a default network,
	// the network listener, and the Bluetooth helper (the shim).
	st := store.New("", "test-mac", "9.9")
	for i := 0; i < 32; i++ {
		a := model.Agent{ID: fmt.Sprintf("a%02d", i), Title: fmt.Sprintf("checkout-api %02d: a title long enough to span pieces", i),
			Tool: "claude", Status: model.Running, Cwd: "/work/checkout-api", Folder: "checkout-api", Branch: "main",
			Model: "claude-test", ModelLabel: "Test", LastPrompt: strings.Repeat("make the tests pass ", 6)}
		if i%5 == 0 {
			a.Status = model.Waiting
			a.Waiting = &model.Prompt{ID: "p" + a.ID, Kind: "permission", Title: "run this command?", Detail: "go test ./...",
				Options: []model.Option{{Key: "1", Label: "yes", Primary: true}, {Key: "3", Label: "no"}}}
		}
		st.Add(&store.Entry{A: a})
		st.Place(a.ID, i)
	}
	ctl := &fakeCtl{}
	srv := &server.Server{Store: st, Ctl: ctl, Static: fstest.MapFS{},
		Devices: &devices.Registry{Path: filepath.Join(dir, "devices.json")},
		Audit:   &devices.Audit{Path: filepath.Join(dir, "audit.log")}}
	ca, err := pki.LoadOrCreateCA(filepath.Join(dir, "ca.pem"), pki.FileKeystore{Dir: filepath.Join(dir, "keys")}, "test")
	if err != nil {
		t.Fatal(err)
	}
	srvCert, err := ca.ServerCert(filepath.Join(dir, "srv.pem"), filepath.Join(dir, "srv.key"), []string{"deck.local", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	srv.CA, srv.ServerFingerprint = ca, pki.Fingerprint(srvCert.Certificate[0])
	wd := &link.WifiDefault{Store: pki.FileKeystore{Dir: filepath.Join(dir, "wifi-keys")}}
	if err := wd.Set("lab-net", "correct horse"); err != nil {
		t.Fatal(err)
	}
	hub := &link.Hub{Wifi: wd}
	srv.DeviceLink = link.DeviceUpgradeHandler(link.ServerDeck{S: srv}, hub)
	ts := httptest.NewUnstartedServer(srv.DeviceHandler())
	ts.TLS = srv.TLSConfig(srvCert)
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	srv.DeviceHosts = []string{"127.0.0.1"}
	srv.DevicePort, _ = strconv.Atoi(port)

	// The board, in setup: it listens on its "radio" (the socket).
	boardStore := filepath.Join(dir, "nvs")
	if err := os.Mkdir(boardStore, 0o700); err != nil {
		t.Fatal(err)
	}
	ble, _ := startBoard(t, bin, "--ble", filepath.Join(dir, "k.sock"), "--store", boardStore, "--mtu", "185",
		"--focus", "a03", "--seconds", "90")
	adv := ble.wait("ble name=", 5*time.Second)
	name, info := "", ""
	for _, f := range strings.Fields(adv) {
		if v, ok := strings.CutPrefix(f, "name="); ok {
			name = v
		}
		if v, ok := strings.CutPrefix(f, "info="); ok {
			info = v
		}
	}
	if name != "deck-000077" || !strings.Contains(info, "id=AA:BB:CC:00:00:77") {
		t.Fatalf("advertised %q with Info %q", name, info)
	}
	boards := []fakeBoard{{ID: periph1, Name: name, RSSI: -48, Info: info, Sock: "k.sock", MTU: 185}}
	writeBoards := func() {
		b, _ := json.Marshal(boards)
		if err := os.WriteFile(filepath.Join(dir, "boards.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeBoards()

	exe, _ := os.Executable()
	central := &Central{Path: exe, Env: []string{fakeEnv + "=" + dir}, MinBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond}
	up := &Uplinks{Central: central, Deck: link.ServerDeck{S: srv}, Hub: hub, Devices: srv.Devices, Audit: srv.Audit.Log,
		Every: 50 * time.Millisecond, Tune: func(s *link.Session) { s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond }}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go central.Run(ctx)
	go up.Run(ctx)
	r := &rig{t: t, dir: dir, srv: srv, ctl: ctl, hub: hub, central: central, up: up}
	r.waitFor("the helper is ready", func() bool { return central.Status().Available })

	// 1. A scan shows the board, in setup, with what its Info says.
	if err := up.Scan(true); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the board in the scan", func() bool { n, ok := up.Seen(periph1); return ok && n.HasInfo })
	if n, _ := up.Seen(periph1); n.Name != name || n.BoardID != "AA:BB:CC:00:00:77" || n.Board != "board-wifi" || n.Firmware != "0.1.0" || !n.Setup {
		t.Fatalf("seen: %+v", n)
	}
	_ = up.Scan(false)

	// 2. Add it (the passkey step), its Hello, approved with the scope chosen.
	var stages []string
	d, err := up.Add(context.Background(), periph1, devices.Act, func(stage, _ string) { stages = append(stages, stage) })
	if err != nil {
		t.Fatalf("add: %v\n%s", err, ble.all())
	}
	if strings.Join(stages, ",") != "connect,passkey,connected,done" {
		t.Fatalf("stages: %v", stages)
	}
	const dev = "board-wifi-000077"
	if d.Name != dev || !d.Approved || d.Scope != devices.Act || d.BLE != periph1 {
		t.Fatalf("device: %+v", d)
	}
	ble.wait("ble connected", time.Second)
	ble.wait("welcome name="+dev+" scope=2 approved=1", 5*time.Second)

	// 3. Paired for Wi-Fi over Bluetooth, then given the default network.
	ble.wait("paired serial=", 5*time.Second)
	ble.wait("wifi ssid=lab-net forget=0", 5*time.Second)
	if d, _ = srv.Devices.Get(dev); d.CertSerial == "" || !strings.Contains(ble.all(), "paired serial="+d.CertSerial) {
		t.Fatalf("certificate: registry %q\n%s", d.CertSerial, ble.all())
	}
	r.waitFor("the default network noted", func() bool { return strings.Contains(wd.Status(dev), "lab-net") })
	if strings.Contains(ble.all(), "correct horse") {
		t.Fatal("the password reached the board's log")
	}

	// 4. The 32-agent snapshot, through the MTU's pieces both ways.
	ble.wait("snapshot_end", 5*time.Second)
	if !strings.Contains(ble.all(), "snapshot_begin agents=32") {
		t.Fatalf("snapshot:\n%s", ble.all())
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(ble.all(), "\n") {
		if v, ok := strings.CutPrefix(l, "agent id="); ok {
			seen[strings.Fields(v)[0]] = true
		}
	}
	if len(seen) != 32 {
		t.Fatalf("agents on the board: %d\n%s", len(seen), ble.all())
	}

	// 5. A command from the board.
	focus := ble.wait("focus seq=", 5*time.Second)
	seq := strings.Fields(strings.TrimPrefix(focus, "focus "))[0]
	ble.wait("result "+seq+" code=204", 5*time.Second)
	r.waitFor("focus reached the deck", func() bool { return ctl.has("focus a03") })
	if links := hub.Links(); len(links) != 1 || links[0].Transport != link.OverBLE || !links[0].Approved || links[0].Where != "bluetooth "+name {
		t.Fatalf("links over Bluetooth: %+v", links)
	}

	// 6. The uplink preference, there and back.
	_ = srv.Devices.SetUplink(dev, "ble")
	ble.wait("uplink value=2", 5*time.Second)
	r.waitFor("the board holds ble", func() bool { l := hub.Links(); return len(l) == 1 && l[0].Net != nil && l[0].Net.Uplink == "ble" })
	_ = srv.Devices.SetUplink(dev, "auto")
	ble.wait("uplink value=0", 5*time.Second)
	r.waitFor("the board holds auto", func() bool { l := hub.Links(); return len(l) == 1 && l[0].Net != nil && l[0].Net.Uplink == "auto" })
	if n := hub.Links()[0].Net; n.BLE != "connected" || !n.HasCert || n.SSID != "lab-net" {
		t.Fatalf("what the board says over Bluetooth: %+v", n)
	}

	// 7. Hand-over: the board comes up on Wi-Fi (auto prefers it) and the
	// Bluetooth link closes; Wi-Fi gone, Bluetooth comes back by itself, with
	// no passkey (bonded now).
	boards[0].Paired = true
	writeBoards()
	wifi, wifiCmd := startBoard(t, bin, "--wifi", "--store", boardStore, "--seconds", "3")
	wifi.wait("tls connected host=127.0.0.1", 5*time.Second)
	wifi.wait("welcome name="+dev+" scope=2 approved=1 cert="+d.CertSerial, 5*time.Second)
	wifi.wait("snapshot_begin agents=32", 5*time.Second)
	ble.wait("ble disconnected", 5*time.Second)
	r.waitFor("Bluetooth stands by", func() bool { ls := up.State(d); return ls.State == "standing by" && ls.Uses == link.OverWifi })
	if l := hub.Links(); len(l) != 1 || l[0].Transport != link.OverWifi {
		t.Fatalf("links on Wi-Fi: %+v", l)
	}
	_ = wifiCmd.Wait()
	r.waitFor("back on Bluetooth", func() bool {
		l := hub.Links()
		return ble.count("ble connected") == 2 && len(l) == 1 && l[0].Transport == link.OverBLE && l[0].Approved
	})
	if n := r.count("connect " + periph1); n != 2 {
		t.Fatalf("connects: %d\n%s", n, r.commands())
	}

	// 8. Revoked: the link drops and the helper lets the board go.
	if err := srv.Devices.Revoke(dev); err != nil {
		t.Fatal(err)
	}
	r.waitFor("disconnected after revoke", func() bool { return ble.count("ble disconnected") == 2 })
	r.waitFor("forgotten", func() bool { return r.count("forget "+periph1) >= 1 })
	if d, _ := srv.Devices.Get(dev); !d.Revoked || d.BLE != "" {
		t.Fatalf("after revoke: %+v", d)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	for _, want := range []string{`"action":"bluetooth add ` + dev + `"`, `"action":"POST /v1/agents/a03/focus","status":204`} {
		if !strings.Contains(string(audit), want) {
			t.Errorf("audit lacks %s", want)
		}
	}
}
