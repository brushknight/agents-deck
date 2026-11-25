package link

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// The board's side of the uplink preference (docs/link/protocol.md,
// "Uplinks"): the deck sends LinkConfig when the board holds another
// preference than the device's, and reads back what the board says in
// NetStatus (its Bluetooth state, the preference it holds, the transport it
// is on). A session the board has left (quiet) doesn't count when it says
// Hello elsewhere.

// uplinkPB is a device's preference as the board knows it.
func uplinkPB(pref string) linkpb.Uplink {
	switch pref {
	case UplinkWifi:
		return linkpb.Uplink_UPLINK_WIFI
	case UplinkBLE:
		return linkpb.Uplink_UPLINK_BLE
	}
	return linkpb.Uplink_UPLINK_AUTO
}

// UplinkName is a board's preference in the deck's words (auto, wifi, ble).
func UplinkName(u linkpb.Uplink) string {
	switch u {
	case linkpb.Uplink_UPLINK_WIFI:
		return UplinkWifi
	case linkpb.Uplink_UPLINK_BLE:
		return UplinkBLE
	}
	return UplinkAuto
}

// BleStateName is a NetStatus.ble in words ("" from firmware older than
// Bluetooth).
func BleStateName(b linkpb.BleState) string {
	switch b {
	case linkpb.BleState_BLE_UNSUPPORTED:
		return "not supported"
	case linkpb.BleState_BLE_SETUP:
		return "in setup"
	case linkpb.BleState_BLE_BONDED:
		return "paired"
	case linkpb.BleState_BLE_CONNECTED:
		return "connected"
	}
	return ""
}

// transportPB is what a board on this transport says in NetStatus.transport.
func transportPB(t string) linkpb.Transport {
	switch t {
	case OverWifi:
		return linkpb.Transport_TRANSPORT_WIFI
	case OverBLE:
		return linkpb.Transport_TRANSPORT_BLE
	}
	return linkpb.Transport_TRANSPORT_SERIAL
}

func transportWords(t linkpb.Transport) string {
	switch t {
	case linkpb.Transport_TRANSPORT_SERIAL:
		return "USB serial"
	case linkpb.Transport_TRANSPORT_WIFI:
		return "Wi-Fi"
	case linkpb.Transport_TRANSPORT_BLE:
		return "Bluetooth"
	}
	return t.String()
}

// uplinkSync is how sending the preference to the board went, in this
// session.
type uplinkSync struct {
	mu          sync.Mutex
	busy        bool          // a LinkConfig waits for its Result
	sent        bool          // sentFor was sent in this session
	sentFor     linkpb.Uplink // (once per value: no loop with a board that keeps another)
	unsupported bool          // the board answered 501: no Bluetooth in its firmware
	note        string        // what went wrong, in words ("" when it holds the preference)
}

// UplinkNote says what went wrong sending the board its uplink preference
// ("" when nothing did).
func (s *Session) UplinkNote() string {
	s.uplink.mu.Lock()
	defer s.uplink.mu.Unlock()
	return s.uplink.note
}

// syncUplink sends the board the device's uplink preference when it holds
// another one: at the start of a session (a board says NetStatus after each
// Welcome) and whenever the preference changes. Only approved boards, and
// only boards with Bluetooth (NetStatus.ble set and not UNSUPPORTED): for the
// others the preference means nothing. Runs on Run's goroutine.
func (s *Session) syncUplink() {
	if !s.active() || s.boardBle == linkpb.BleState_BLE_UNSPECIFIED || s.boardBle == linkpb.BleState_BLE_UNSUPPORTED {
		return
	}
	want := uplinkPB(s.dev.Uplink)
	u := &s.uplink
	u.mu.Lock()
	if s.boardUplink == want {
		u.note = ""
		u.mu.Unlock()
		return
	}
	if u.busy || u.unsupported || (u.sent && u.sentFor == want) {
		u.mu.Unlock()
		return
	}
	u.busy, u.sent, u.sentFor = true, true, want
	u.mu.Unlock()
	name := s.dev.Name
	go func() { // the Result comes through Run: wait for it elsewhere
		ctx, cancel := context.WithTimeout(context.Background(), WifiTimeout)
		defer cancel()
		r, err := s.request(ctx, &linkpb.Frame{Body: &linkpb.Frame_LinkConfig{LinkConfig: &linkpb.LinkConfig{Uplink: want}}})
		note, unsupported := "", false
		switch {
		case err != nil:
			note = "no answer to its uplink preference (" + err.Error() + ")"
		case r == nil:
			note = "it answered its uplink preference with something else"
		case r.Code == http.StatusNotImplemented:
			note, unsupported = "its firmware has no Bluetooth, so it keeps no uplink preference", true
		case r.Code >= 300:
			msg := r.Error
			if msg == "" {
				msg = http.StatusText(int(r.Code))
			}
			note = fmt.Sprintf("it refused its uplink preference: %d %s", r.Code, msg)
		}
		u.mu.Lock()
		u.busy, u.note, u.unsupported = false, note, u.unsupported || unsupported
		u.mu.Unlock()
		if note != "" {
			log.Printf("link %s: %s: %s", s.Name, name, note)
		} else {
			log.Printf("link %s: %s keeps uplink %s", s.Name, name, UplinkName(want))
		}
	}()
}

// checkTransport: a board says in NetStatus which transport it is on. On a
// link it doesn't call by its name (a Bluetooth link whose board says USB),
// the deck trusts the link less: no pairing and no Wi-Fi credentials on it.
// (Its other links close at its Hello: Hub.arbitrate.)
func (s *Session) checkTransport(n *linkpb.NetStatus) {
	if n.Transport == linkpb.Transport_TRANSPORT_UNSPECIFIED {
		return
	}
	if want := transportPB(s.Transport); n.Transport != want && !s.wrongTransport.Swap(true) {
		log.Printf("link %s: %s says it is on %s, but this link is %s: no pairing or Wi-Fi credentials over it",
			s.Name, s.dev.Name, transportWords(n.Transport), transportWords(want))
	}
}

// heard notes a frame from the board (any goroutine reads it).
func (s *Session) heard(t time.Time) { s.rxAt.Store(t.UnixNano()) }

// quietFor: how long the board has sent nothing (0 before Run starts).
func (s *Session) quietFor(now time.Time) time.Duration {
	at := s.rxAt.Load()
	if at == 0 {
		return 0
	}
	return now.Sub(time.Unix(0, at))
}

// quiet: nothing from the board for two pings. A board answers every ping
// on the link it carries its session on, so a quiet session is one the board
// has left (a Wi-Fi network that went away, a cable whose deck went silent).
func (s *Session) quiet() bool {
	at := s.rxAt.Load()
	return at != 0 && time.Since(time.Unix(0, at)) > 2*s.PingEvery
}
