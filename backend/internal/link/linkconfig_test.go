package link

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// bleSession runs a Bluetooth session (as internal/ble does) for an approved
// board and plays the board through its Welcome.
func bleSession(t *testing.T, deck Deck, hub *Hub, wifi *WifiDefault, id string) (*board, *Session, chan error) {
	t.Helper()
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); deckEnd.Close(); boardEnd.Close() })
	s := NewSession(deck, deckEnd, "test@ble")
	s.Hub, s.Transport, s.Where, s.Wifi = hub, OverBLE, "bluetooth deck-test", wifi
	s.RecheckEvery = 50 * time.Millisecond
	ended := make(chan error, 1)
	go func() { ended <- s.Run(ctx); deckEnd.Close() }()
	b := &board{t: t, conn: boardEnd}
	b.send(hi(id))
	if w := b.next().GetWelcome(); w == nil || !w.Approved {
		t.Fatalf("welcome: %+v", w)
	}
	return b, s, ended
}

func approved(t *testing.T, devs *devices.Registry, id string) devices.Device {
	t.Helper()
	d, err := devs.Board(id, "oled-tile", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := devs.Approve(d.Name); err != nil {
		t.Fatal(err)
	}
	d, _ = devs.Get(d.Name)
	return d
}

func netStatus(tr linkpb.Transport, ble linkpb.BleState, up linkpb.Uplink) *linkpb.Frame {
	return &linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: &linkpb.NetStatus{Transport: tr, Wifi: linkpb.WifiState_WIFI_NOT_SET, Ble: ble, Uplink: up}}}
}

// silent: the deck sends the board nothing (but pings) for a while.
func (b *board) silent(d time.Duration) bool {
	b.t.Helper()
	buf := make([]byte, 4096)
	deadline := time.Now().Add(d)
	for {
		for _, f := range b.q {
			if f.Frame.GetPing() == nil {
				return false
			}
		}
		b.q = nil
		_ = b.conn.SetReadDeadline(deadline)
		n, err := b.conn.Read(buf)
		if err != nil {
			return errors.Is(err, os.ErrDeadlineExceeded)
		}
		b.q = append(b.q, b.dec.Feed(buf[:n])...)
	}
}

// The deck sends LinkConfig when the board holds another preference than the
// device's: at the start (its NetStatus) and on every change; a 501 (no
// Bluetooth in the firmware) stops it for the session.
func TestLinkConfigFollowsThePreference(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	const id = "AA:BB:CC:00:44:01"
	d := approved(t, srv.Devices, id)
	b, _, _ := bleSession(t, ServerDeck{srv}, hub, nil, id)

	// Both auto: nothing to send.
	b.send(netStatus(linkpb.Transport_TRANSPORT_BLE, linkpb.BleState_BLE_CONNECTED, linkpb.Uplink_UPLINK_AUTO))
	if !b.silent(200 * time.Millisecond) {
		t.Fatal("LinkConfig sent to a board that holds the preference")
	}
	// ble: sent; the board keeps it and says so.
	if err := srv.Devices.SetUplink(d.Name, UplinkBLE); err != nil {
		t.Fatal(err)
	}
	f := b.next()
	if f.GetLinkConfig() == nil || f.GetLinkConfig().Uplink != linkpb.Uplink_UPLINK_BLE {
		t.Fatalf("want LinkConfig ble, got %v", f)
	}
	b.send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Result{Result: &linkpb.Result{Code: 204}}})
	b.send(netStatus(linkpb.Transport_TRANSPORT_BLE, linkpb.BleState_BLE_CONNECTED, linkpb.Uplink_UPLINK_BLE))
	waitFor(t, func() bool {
		l := hub.Links()
		return len(l) == 1 && l[0].Net != nil && l[0].Net.Uplink == "ble" && l[0].Net.BLE == "connected" && l[0].UplinkNote == ""
	})
	if !b.silent(200 * time.Millisecond) {
		t.Fatal("sent again after the board kept it")
	}
	// wifi: this firmware answers 501. The note says so; nothing more is sent.
	_ = srv.Devices.SetUplink(d.Name, UplinkWifi)
	f = b.next()
	if f.GetLinkConfig().GetUplink() != linkpb.Uplink_UPLINK_WIFI {
		t.Fatalf("want LinkConfig wifi, got %v", f)
	}
	b.send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Result{Result: &linkpb.Result{Code: 501}}})
	waitFor(t, func() bool { l := hub.Links(); return len(l) == 1 && strings.Contains(l[0].UplinkNote, "no Bluetooth") })
	_ = srv.Devices.SetUplink(d.Name, UplinkAuto)
	_ = srv.Devices.SetUplink(d.Name, UplinkBLE)
	if !b.silent(300 * time.Millisecond) {
		t.Fatal("LinkConfig sent again after a 501")
	}
}

// A board whose firmware has no Bluetooth (or is older than it) is never
// sent the preference.
func TestLinkConfigNotWithoutBluetooth(t *testing.T) {
	srv, _ := testDeck(t)
	const id = "AA:BB:CC:00:44:02"
	d := approved(t, srv.Devices, id)
	_ = srv.Devices.SetUplink(d.Name, UplinkWifi)
	b, _, _ := uplinkSession(t, ServerDeck{srv}, &Hub{}, OverSerial, "/dev/cu.test", id)
	if w := b.next().GetWelcome(); w == nil || !w.Approved {
		t.Fatalf("welcome: %+v", w)
	}
	b.send(netStatus(linkpb.Transport_TRANSPORT_SERIAL, linkpb.BleState_BLE_UNSUPPORTED, linkpb.Uplink_UPLINK_AUTO))
	b.send(netStatus(linkpb.Transport_TRANSPORT_SERIAL, linkpb.BleState_BLE_UNSPECIFIED, linkpb.Uplink_UPLINK_AUTO))
	if !b.silent(300 * time.Millisecond) {
		t.Fatal("LinkConfig sent to a board without Bluetooth")
	}
}

// A board on Bluetooth that says it is on another transport is trusted less:
// no certificate and no Wi-Fi credentials over that link.
func TestTransportMismatchOverBluetooth(t *testing.T) {
	srv, _ := testDeck(t)
	withCA(t, srv)
	hub := &Hub{}
	const id = "AA:BB:CC:00:44:03"
	d := approved(t, srv.Devices, id)
	b, s, _ := bleSession(t, ServerDeck{srv}, hub, nil, id)
	b.send(netStatus(linkpb.Transport_TRANSPORT_SERIAL, linkpb.BleState_BLE_CONNECTED, linkpb.Uplink_UPLINK_AUTO))
	waitFor(t, func() bool { return !s.Hands() })
	_, csr := csrFor(t)
	b.send(certReq(9, csr))
	if r := b.next().GetResult(); r.GetCode() != 403 {
		t.Fatalf("certificate over a link that says it is USB: %+v", r)
	}
	if code, _ := hub.SendWifi(d.Name, "lab", "secret-pass", false); code != 409 {
		t.Fatalf("Wi-Fi over that link: %d", code)
	}
}

// Bluetooth (paired with the passkey on the board's screen) counts as in
// hand: an approved, paired board there gets the default network.
func TestDefaultNetworkOverBluetooth(t *testing.T) {
	srv, m, wd, _ := provisionDeck(t)
	if err := wd.Set("lab-net", "correct horse"); err != nil {
		t.Fatal(err)
	}
	const id = "AA:BB:CC:00:44:04"
	d := approved(t, srv.Devices, id)
	b, _, _ := bleSession(t, ServerDeck{srv}, m.Hub, wd, id)
	b.send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: &linkpb.NetStatus{Transport: linkpb.Transport_TRANSPORT_BLE,
		Wifi: linkpb.WifiState_WIFI_NOT_SET, HasCert: true, Ble: linkpb.BleState_BLE_CONNECTED}}})
	f := b.next()
	wc := f.GetWifiConfig()
	if wc == nil || wc.Ssid != "lab-net" || wc.Password != "correct horse" {
		t.Fatalf("want the default network, got %v", f)
	}
	b.send(&linkpb.Frame{Seq: f.Seq, Body: &linkpb.Frame_Result{Result: &linkpb.Result{Code: 204}}})
	b.send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: &linkpb.NetStatus{Transport: linkpb.Transport_TRANSPORT_BLE,
		Wifi: linkpb.WifiState_WIFI_IDLE, Ssid: "lab-net", Ip: "192.0.2.50", HasCert: true, Ble: linkpb.BleState_BLE_CONNECTED}}})
	waitFor(t, func() bool { return wd.Status(d.Name) == "default lab-net: ok" })
}

// A session whose board has gone quiet doesn't hold a better uplink: the
// board's Hello elsewhere closes it instead of being turned away.
func TestQuietSessionGivesWay(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	const id = "AA:BB:CC:00:44:05"
	approved(t, srv.Devices, id)
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); deckEnd.Close(); boardEnd.Close() })
	usb := NewSession(ServerDeck{srv}, deckEnd, "usb")
	usb.Hub, usb.PingEvery = hub, 100*time.Millisecond
	usbEnded := make(chan error, 1)
	go func() { usbEnded <- usb.Run(ctx) }()
	ub := &board{t: t, conn: boardEnd}
	ub.send(hi(id))
	if w := ub.next().GetWelcome(); w == nil {
		t.Fatal("no welcome on USB")
	}
	// Fresh, USB wins: the Bluetooth Hello is turned away.
	_, ended, _ := uplinkSession(t, ServerDeck{srv}, hub, OverBLE, "bluetooth", id)
	endedWith(t, ended, ErrHandedOver)
	// The board stops answering on USB (it left the cable's silent deck).
	go func() { // keep reading so the deck's pings don't block the pipe
		buf := make([]byte, 1024)
		for {
			if _, err := boardEnd.Read(buf); err != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	if p := hub.Preferred(usb.Board(), OverBLE, ""); p != "" {
		t.Fatalf("a quiet USB session still counts: %q", p)
	}
	bb, _, _ := uplinkSession(t, ServerDeck{srv}, hub, OverBLE, "bluetooth", id)
	if w := bb.next().GetWelcome(); w == nil || !w.Approved {
		t.Fatalf("welcome over Bluetooth: %+v", w)
	}
	endedWith(t, usbEnded, ErrHandedOver)
}
