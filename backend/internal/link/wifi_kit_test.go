package link

import (
	"bufio"
	"context"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// boardWifi builds the firmware kit's board_wifi (C, mbedTLS 3): skipped
// without make, a C compiler or mbedTLS 3.
func boardWifi(t *testing.T) string {
	t.Helper()
	host, _ := filepath.Abs(repo + "/firmware/deck-link/test/host")
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("no make")
	}
	if out, err := exec.Command("make", "-s", "-C", host, "board_wifi").CombinedOutput(); err != nil {
		t.Skipf("can't build board_wifi (needs mbedTLS 3): %v\n%s", err, out)
	}
	return filepath.Join(host, "build", "board_wifi")
}

// runBoard runs board_wifi and hands each stderr line to see (in order) until it exits.
func runBoard(t *testing.T, bin string, rw func(in io.WriteCloser, out io.ReadCloser), see func(string), args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	errp, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if rw != nil {
		rw(in, out)
	}
	var log []string
	sc := bufio.NewScanner(errp)
	for sc.Scan() {
		log = append(log, sc.Text())
		if see != nil {
			see(sc.Text())
		}
	}
	err := cmd.Wait()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return strings.Join(log, "\n"), code
}

// End to end with the firmware kit: a board pairs over USB serial (its own
// key and CSR, the certificate kept in its store), takes Wi-Fi credentials,
// then connects over TLS 1.3 with that certificate and runs a session.
// Revoked, it can't connect again.
func TestFirmwareKitPairsThenConnectsOverWifi(t *testing.T) {
	bin := boardWifi(t)
	srv, _ := testDeck(t)
	srvCert := withCA(t, srv)
	hub := &Hub{}
	addr := listener(t, srv, srvCert, hub)
	_, port, _ := net.SplitHostPort(addr)
	// Where the board looks: a name that doesn't resolve first (the .local
	// name of a Mac on another network), then the address.
	srv.DeviceHosts = []string{"deck.invalid", "127.0.0.1"}
	srv.DevicePort, _ = strconv.Atoi(port)
	store := t.TempDir()
	const name = "board-wifi-000077"

	// 1. Over "USB serial": approval, pairing, Wi-Fi credentials.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sess *Session
	sentWifi := make(chan int, 1)
	log, _ := runBoard(t, bin, func(in io.WriteCloser, out io.ReadCloser) {
		sess = NewSession(ServerDeck{srv}, struct {
			io.Reader
			io.Writer
		}{out, in}, "board_wifi")
		sess.Hub, sess.RecheckEvery = hub, 50*time.Millisecond
		go func() { _ = sess.Run(ctx) }()
	}, func(l string) {
		switch {
		case strings.HasPrefix(l, "welcome ") && strings.Contains(l, "approved=0"):
			go func() {
				_ = srv.Devices.Approve(name)
				_ = srv.Devices.SetScope(name, devices.Act)
			}()
		case strings.HasPrefix(l, "paired "):
			go func() {
				code, msg := hub.SendWifi(name, "lab-net", "correct horse", false)
				if code != 204 {
					t.Errorf("wifi: %d %s", code, msg)
				}
				sentWifi <- code
			}()
		}
	}, "--serial", "--store", store, "--seconds", "3")
	<-sentWifi
	for _, want := range []string{"welcome name=" + name + " scope=1 approved=0", "pair request seq=",
		"paired serial=", "name=" + name + " hosts=2", "wifi ssid=lab-net forget=0"} {
		if !strings.Contains(log, want) {
			t.Fatalf("serial log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "correct horse") {
		t.Fatal("the password reached a log")
	}
	d, _ := srv.Devices.Get(name)
	if d.CertSerial == "" || !strings.Contains(log, "paired serial="+d.CertSerial) {
		t.Fatalf("the registry holds %q:\n%s", d.CertSerial, log)
	}

	// 2. Over the network, with the certificate from its store.
	log, code := runBoard(t, bin, nil, func(l string) {
		if strings.HasPrefix(l, "snapshot_end") {
			if links := hub.Links(); len(links) == 0 || links[0].Transport != OverWifi {
				t.Errorf("links while on Wi-Fi: %+v", links)
			}
		}
	}, "--wifi", "--store", store, "--seconds", "3")
	for _, want := range []string{"tls failed host=deck.invalid error=3", "tls connected host=127.0.0.1",
		"welcome name=" + name + " scope=2 approved=1 cert=" + d.CertSerial, "snapshot_begin agents=3", "agent id=a1 status=waiting",
		"snapshot_end"} {
		if !strings.Contains(log, want) {
			t.Fatalf("Wi-Fi log lacks %q (exit %d):\n%s", want, code, log)
		}
	}

	// 3. The listener's certificate doesn't name 127.0.0.1: its fingerprint stands in.
	other, err := srv.CA.ServerCert(filepath.Join(t.TempDir(), "s.pem"), filepath.Join(t.TempDir(), "s.key"), []string{"deck.local"})
	if err != nil {
		t.Fatal(err)
	}
	addr2 := listener(t, srv, other, hub)
	_, port2, _ := net.SplitHostPort(addr2)
	// Point the stored certificate at the second listener by pairing again over serial.
	srv.DevicePort, _ = strconv.Atoi(port2)
	srv.ServerFingerprint = pki.Fingerprint(other.Certificate[0])
	_ = srv.Devices.ClearBoardCert(name)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	log, _ = runBoard(t, bin, func(in io.WriteCloser, out io.ReadCloser) {
		s := NewSession(ServerDeck{srv}, struct {
			io.Reader
			io.Writer
		}{out, in}, "board_wifi")
		go func() { _ = s.Run(ctx2) }()
	}, nil, "--serial", "--store", store, "--seconds", "2")
	if !strings.Contains(log, "welcome name="+name+" scope=2 approved=1 cert=-") || !strings.Contains(log, "paired serial=") {
		t.Fatalf("pairing again after `agentctl link pair`:\n%s", log)
	}
	log, code = runBoard(t, bin, nil, nil, "--wifi", "--store", store, "--seconds", "2")
	if !strings.Contains(log, "tls connected host=127.0.0.1") || !strings.Contains(log, "snapshot_end") {
		t.Fatalf("fingerprint fallback (exit %d):\n%s", code, log)
	}

	// 4. Revoked: the deck refuses the certificate, and the board says so.
	_ = srv.Devices.Revoke(name)
	log, code = runBoard(t, bin, nil, nil, "--wifi", "--store", store, "--seconds", "2")
	if code != 4 || !strings.Contains(log, "error=7 the deck refused the certificate") {
		t.Fatalf("revoked board (exit %d):\n%s", code, log)
	}

	// 5. Approved again, it pairs again over USB. Without the listener's
	// fingerprint, a listener that doesn't name the host it dialled is refused.
	_ = srv.Devices.Approve(name)
	srv.ServerFingerprint = ""
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	log, _ = runBoard(t, bin, func(in io.WriteCloser, out io.ReadCloser) {
		s := NewSession(ServerDeck{srv}, struct {
			io.Reader
			io.Writer
		}{out, in}, "board_wifi")
		go func() { _ = s.Run(ctx3) }()
	}, nil, "--serial", "--store", store, "--seconds", "2")
	if !strings.Contains(log, "paired serial=") {
		t.Fatalf("pairing without a fingerprint:\n%s", log)
	}
	log, code = runBoard(t, bin, nil, nil, "--wifi", "--store", store, "--seconds", "1")
	if code != 3 || !strings.Contains(log, "tls failed host=127.0.0.1 error=6") {
		t.Fatalf("an unnamed host without a fingerprint (exit %d):\n%s", code, log)
	}

}
