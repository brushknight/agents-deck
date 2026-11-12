package link

import (
	"errors"
	"log"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// A board may reach the deck over USB, Wi-Fi and Bluetooth at once, but the
// deck runs one session per board: the one on the uplink its preference
// (devices.Device.Uplink) puts first. USB always comes first: a cable is in
// your hands, and the board quiets its radios while a deck answers there. The
// preference picks between the radios, with the same meaning the board gives
// it (link.proto, Uplink):
//
//	auto  USB, else Wi-Fi, else Bluetooth
//	wifi  USB, else Wi-Fi; Bluetooth only while the board isn't paired for
//	      Wi-Fi yet (to set it up)
//	ble   USB, else Bluetooth; Wi-Fi isn't used
//
// When a better uplink says Hello, the session on the worse one ends. A
// session that has heard nothing from its board for two pings doesn't count:
// the board has left it (it carries one session at a time).

// Uplink preferences (devices.Device.Uplink; "" is auto).
const (
	UplinkAuto = "auto"
	UplinkWifi = "wifi"
	UplinkBLE  = "ble"
)

// ValidUplink reports whether p is a preference the deck knows.
func ValidUplink(p string) bool {
	return p == "" || p == UplinkAuto || p == UplinkWifi || p == UplinkBLE
}

// UplinkOrder is the transports a preference uses, best first.
func UplinkOrder(pref string) []string {
	switch pref {
	case UplinkWifi:
		return []string{OverSerial, OverWifi}
	case UplinkBLE:
		return []string{OverSerial, OverBLE}
	}
	return []string{OverSerial, OverWifi, OverBLE}
}

// unused ranks a transport the preference doesn't use: after all others.
const unused = 99

func uplinkRank(pref, transport string) int {
	for i, t := range UplinkOrder(pref) {
		if t == transport {
			return i
		}
	}
	return unused
}

// UplinkAllowed reports whether the board's preference lets transport carry
// its session at all: Bluetooth under wifi only for setup (no Wi-Fi
// certificate yet), Wi-Fi never under ble.
func UplinkAllowed(d devices.Device, transport string) bool {
	if uplinkRank(d.Uplink, transport) != unused {
		return true
	}
	return transport == OverBLE && d.Uplink == UplinkWifi && d.CertSerial == ""
}

// ErrHandedOver ends a session whose board is on an uplink the deck prefers.
var ErrHandedOver = errors.New("the board uses a better uplink")

// ErrOnBluetooth: the board is connected over Bluetooth only, and firmware
// isn't sent over Bluetooth. errors.Is(ErrOnBluetooth, ErrNotOnWifi) holds.
var ErrOnBluetooth error = onBluetooth{}

type onBluetooth struct{}

func (onBluetooth) Error() string {
	return "the board is connected over Bluetooth only, which is too slow for firmware: let it join Wi-Fi, or plug it in with a USB cable, to update it"
}
func (onBluetooth) Is(target error) bool { return target == ErrNotOnWifi }

// Hands: the board is in the owner's hands, on a USB cable or a Bluetooth
// link paired with the passkey on its own screen. It may then be given Wi-Fi
// and paired for it. A board whose NetStatus names another transport than
// its link's isn't (checkTransport).
func (s *Session) Hands() bool {
	return (s.Transport == OverSerial || s.Transport == OverBLE) && !s.wrongTransport.Load()
}

// handOver ends the session (once) for a better uplink.
func (s *Session) handOver() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *Session) handedOver() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// arbitrate runs when s's board says Hello: the session on the uplink the
// board's preference puts first stays, the others end. An error ends s.
func (h *Hub) arbitrate(s *Session) error {
	name := s.Board()
	if name == "" {
		return nil
	}
	pref := ""
	if d, ok := s.Deck.Device(name); ok {
		pref = d.Uplink
	}
	mine := uplinkRank(pref, s.Transport)
	h.mu.Lock()
	var lose []*Session
	for o := range h.live {
		if o == s || o.Board() != name || o.helloSeen.Load() == nil || o.handedOver() {
			continue
		}
		theirs := uplinkRank(pref, o.Transport)
		switch {
		case theirs < mine && o.quiet():
			log.Printf("link %s: %s says Hello over %s; its %s link is silent: closing that one", s.Name, name, s.Transport, o.Transport)
			lose = append(lose, o)
		case theirs < mine:
			h.mu.Unlock()
			log.Printf("link %s: %s already uses %s, which it prefers: closing this %s link", s.Name, name, o.Transport, s.Transport)
			return ErrHandedOver
		case theirs > mine:
			lose = append(lose, o)
		}
	}
	h.mu.Unlock()
	for _, o := range lose {
		log.Printf("link %s: %s moved to %s: closing its %s link", o.Name, name, s.Transport, o.Transport)
		o.handOver()
	}
	return nil
}

// Preferred is the transport of the board's live session that its preference
// puts before transport, or "" when there is none (so transport may carry
// the session).
func (h *Hub) Preferred(board, transport, pref string) string {
	if h == nil {
		return ""
	}
	mine := uplinkRank(pref, transport)
	h.mu.Lock()
	defer h.mu.Unlock()
	best, bestRank := "", mine
	for s := range h.live {
		if s.Board() != board || s.helloSeen.Load() == nil || s.handedOver() || s.quiet() {
			continue
		}
		if r := uplinkRank(pref, s.Transport); r < bestRank {
			best, bestRank = s.Transport, r
		}
	}
	return best
}

// onlyBLE: the board's only live sessions are over Bluetooth.
func (h *Hub) onlyBLE(board string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	any := false
	for s := range h.live {
		if s.Board() != board || s.helloSeen.Load() == nil {
			continue
		}
		if s.Transport != OverBLE {
			return false
		}
		any = true
	}
	return any
}

// BLEConfirm is for adding a board over Bluetooth while another link carries
// its session. via is the transport of the board's live session that proves
// which board it is ("" when it has none): USB serial (in your hands), or
// Wi-Fi opened with its device certificate. confirmed: on that session the
// board said, at or after since, that a central is connected to it over an
// encrypted, passkey-authenticated Bluetooth link (NetStatus.ble CONNECTED).
func (h *Hub) BLEConfirm(board string, since time.Time) (via string, confirmed bool) {
	if h == nil {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.live {
		if s.Board() != board || s.helloSeen.Load() == nil || s.handedOver() || s.quiet() || s.wrongTransport.Load() {
			continue
		}
		if s.Transport != OverSerial && (s.Transport != OverWifi || s.Peer == nil) {
			continue
		}
		if via == "" {
			via = s.Transport
		}
		if at := s.bleSecureAt.Load(); at != 0 && !time.Unix(0, at).Before(since) {
			return s.Transport, true
		}
	}
	return via, false
}
