package link

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// session runs a session of one transport on hub and says Hello as id.
func uplinkSession(t *testing.T, deck Deck, hub *Hub, transport, where, id string) (*board, chan error, context.CancelFunc) {
	t.Helper()
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); deckEnd.Close(); boardEnd.Close() })
	s := NewSession(deck, deckEnd, where)
	s.Hub, s.Transport, s.Where = hub, transport, where
	s.RecheckEvery = 50 * time.Millisecond
	ended := make(chan error, 1)
	go func() { ended <- s.Run(ctx); deckEnd.Close() }()
	b := &board{t: t, conn: boardEnd}
	b.send(hi(id))
	return b, ended, cancel
}

func endedWith(t *testing.T, ended chan error, want error) {
	t.Helper()
	select {
	case err := <-ended:
		if !errors.Is(err, want) {
			t.Fatalf("ended with %v, want %v", err, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("still running")
	}
}

func TestUplinkOrder(t *testing.T) {
	for pref, want := range map[string]string{"": "serial wifi ble", "auto": "serial wifi ble", "wifi": "serial wifi", "ble": "serial ble"} {
		if got := strings.Join(UplinkOrder(pref), " "); got != want {
			t.Errorf("%q: %s", pref, got)
		}
	}
	if ValidUplink("usb") || !ValidUplink("ble") {
		t.Error("ValidUplink")
	}
	for _, c := range []struct {
		d         devices.Device
		transport string
		want      bool
	}{
		{devices.Device{}, OverBLE, true},
		{devices.Device{Uplink: "wifi"}, OverBLE, true}, // setup: no Wi-Fi certificate yet
		{devices.Device{Uplink: "wifi", CertSerial: "0a"}, OverBLE, false},
		{devices.Device{Uplink: "ble"}, OverWifi, false},
		{devices.Device{Uplink: "ble"}, OverSerial, true},
	} {
		if got := UplinkAllowed(c.d, c.transport); got != c.want {
			t.Errorf("%+v over %s: %v", c.d, c.transport, got)
		}
	}
}

// One session per board: USB first; under auto Wi-Fi beats Bluetooth; under
// ble Bluetooth beats Wi-Fi; a better uplink's Hello ends the worse session.
func TestOneSessionPerBoard(t *testing.T) {
	srv, _ := testDeck(t)
	deck := ServerDeck{srv}
	hub := &Hub{}
	const id = "AA:BB:CC:00:00:77"
	name := "oled-tile-000077"

	blue, blueEnded, _ := uplinkSession(t, deck, hub, OverBLE, "bluetooth deck-000077", id)
	if blue.next().GetWelcome() == nil {
		t.Fatal("no welcome over Bluetooth")
	}
	_ = srv.Devices.Approve(name)

	// auto: Wi-Fi's Hello ends Bluetooth.
	wifi, wifiEnded, _ := uplinkSession(t, deck, hub, OverWifi, "192.0.2.9", id)
	if wifi.next().GetWelcome() == nil {
		t.Fatal("no welcome on Wi-Fi")
	}
	endedWith(t, blueEnded, ErrHandedOver)
	if hub.Preferred(name, OverBLE, "") != OverWifi || hub.Preferred(name, OverWifi, "") != "" {
		t.Fatal("Preferred under auto")
	}

	// USB beats Wi-Fi whatever the preference.
	if err := srv.Devices.SetUplink(name, "ble"); err != nil {
		t.Fatal(err)
	}
	usb, usbEnded, cancelUSB := uplinkSession(t, deck, hub, OverSerial, "/dev/cu.test-board", id)
	if usb.next().GetWelcome() == nil {
		t.Fatal("no welcome on USB")
	}
	endedWith(t, wifiEnded, ErrHandedOver)
	// While on USB, Bluetooth is turned away.
	_, ended, _ := uplinkSession(t, deck, hub, OverBLE, "bluetooth deck-000077", id)
	endedWith(t, ended, ErrHandedOver)

	// Unplugged, under ble: Bluetooth's Hello ends a Wi-Fi session.
	cancelUSB()
	endedWith(t, usbEnded, context.Canceled)
	wifi2, wifi2Ended, _ := uplinkSession(t, deck, hub, OverWifi, "192.0.2.9", id)
	if wifi2.next().GetWelcome() == nil { // the only uplink: it stays
		t.Fatal("no welcome on Wi-Fi")
	}
	blue2, _, _ := uplinkSession(t, deck, hub, OverBLE, "bluetooth deck-000077", id)
	if blue2.next().GetWelcome() == nil {
		t.Fatal("no welcome over Bluetooth")
	}
	endedWith(t, wifi2Ended, ErrHandedOver)
	if links := hub.Links(); len(links) != 1 || links[0].Transport != OverBLE {
		t.Fatalf("one session per board: %+v", links)
	}
}
