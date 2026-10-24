package link

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// A real board on a serial port, against a throwaway deck (its own CA and
// registry: nothing of the Mac's own deck is used or changed). Opt in:
//
//	DECK_LINK_HW_PORT=/dev/cu.usbmodem1101 go test ./internal/link -run TestHardware -v
//
// The daemon lets go of the port while this test holds it. The board pairs
// (a certificate from the throwaway CA: the real deck pairs it again, by
// itself, the next time it is on USB), keeps its certificate across a
// reconnect, and tries a network that doesn't exist (DECK_LINK_HW_SSID, by
// default one nobody has) to show the Wi-Fi side works; then it forgets it.
func TestHardwarePairsOverUSB(t *testing.T) {
	port := os.Getenv("DECK_LINK_HW_PORT")
	if port == "" {
		t.Skip("set DECK_LINK_HW_PORT to a board's serial port")
	}
	ssid := os.Getenv("DECK_LINK_HW_SSID")
	if ssid == "" {
		ssid = "agents-deck-no-such-net"
	}
	srv, _ := testDeck(t)
	withCA(t, srv)
	hub := &Hub{}
	open := func() (context.CancelFunc, *Session) {
		f, err := OpenPort(port, 115200)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		s := NewSession(ServerDeck{srv}, f, "hw")
		s.Hub, s.RecheckEvery = hub, 200*time.Millisecond
		go func() { _ = s.Run(ctx); f.Close() }()
		return func() { cancel(); f.Close() }, s
	}
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	board := func() string {
		if l := hub.Links(); len(l) > 0 {
			return l[0].Board
		}
		return ""
	}

	// 1. Hello (within 20 s: the board resends it while it hears nothing), approval, pairing.
	stop, s := open()
	until("the board's Hello", 25*time.Second, func() bool { return board() != "" })
	name := board()
	t.Logf("board %s", name)
	_ = srv.Devices.Approve(name)
	_ = srv.Devices.SetScope(name, devices.View)
	until("a certificate", 20*time.Second, func() bool { d, _ := srv.Devices.Get(name); return d.CertSerial != "" })
	d, _ := srv.Devices.Get(name)
	t.Logf("paired: certificate %s", d.CertSerial)
	until("NetStatus with the certificate", 10*time.Second, func() bool { n := s.Net(); return n != nil && n.HasCert })
	t.Logf("net: %+v", s.Net())

	// 2. The board keeps it: after the deck goes away and comes back, it doesn't ask again.
	stop()
	time.Sleep(time.Second)
	stop, s = open()
	defer func() { stop() }()
	until("the board's Hello again", 30*time.Second, func() bool { return s.Board() == name && s.Net() != nil })
	time.Sleep(3 * time.Second)
	if d2, _ := srv.Devices.Get(name); d2.CertSerial != d.CertSerial {
		t.Fatalf("the board paired again (%s → %s) though the deck holds its certificate", d.CertSerial, d2.CertSerial)
	}

	// 3. Wi-Fi credentials over USB: kept, tried (a network that isn't there), forgotten.
	code, msg := hub.SendWifi(name, ssid, "not-a-real-password", false)
	if code != 204 {
		t.Fatalf("wifi: %d %s", code, msg)
	}
	until("the board's Wi-Fi test", 30*time.Second, func() bool {
		n := s.Net()
		return n != nil && n.Wifi != linkpb.WifiState_WIFI_JOINING && n.Wifi != linkpb.WifiState_WIFI_IDLE
	})
	t.Logf("wifi test: %s", WifiStateName(s.Net().Wifi))
	if code, msg := hub.SendWifi(name, "", "", true); code != 204 {
		t.Fatalf("forget: %d %s", code, msg)
	}
	until("Wi-Fi forgotten", 10*time.Second, func() bool {
		n := s.Net()
		return n != nil && n.Wifi == linkpb.WifiState_WIFI_NOT_SET
	})
	if strings.Contains(WifiStateName(s.Net().Wifi), "password") {
		t.Log("(the board reported a password problem for a network that doesn't exist)")
	}
}
