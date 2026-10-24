package link

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// Hub knows every live Deck Link session, on either transport: for `agentctl
// link` (who is connected, and how) and `agentctl link wifi` (which needs the
// board on USB serial).
type Hub struct {
	Wifi *WifiDefault // the default network (optional): its status per board

	mu   sync.Mutex
	live map[*Session]bool
}

func (h *Hub) add(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live == nil {
		h.live = map[*Session]bool{}
	}
	h.live[s] = true
}

func (h *Hub) remove(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.live, s)
}

// LinkInfo is one connected board, for `agentctl link` and `agentctl devices`.
type LinkInfo struct {
	Board     string    `json:"board"`
	Transport string    `json:"transport"` // serial | wifi
	Where     string    `json:"where"`     // the port, or the board's address
	Since     time.Time `json:"since"`
	Approved  bool      `json:"approved"`
	Net       *NetInfo  `json:"net,omitempty"` // what the board reports
	// DefaultWifi: what happened when the deck sent this board its default
	// network ("default lab: ok", "default lab: wrong password"…).
	DefaultWifi string `json:"defaultWifi,omitempty"`
}

// NetInfo is a board's NetStatus in words.
type NetInfo struct {
	Wifi    string `json:"wifi"` // WifiStateName
	SSID    string `json:"ssid,omitempty"`
	IP      string `json:"ip,omitempty"`
	RSSI    int    `json:"rssi,omitempty"`
	HasCert bool   `json:"hasCert"`
}

// Links lists the connected boards (after their Hello), by board name.
func (h *Hub) Links() []LinkInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []LinkInfo{}
	for s := range h.live {
		name := s.Board()
		if name == "" {
			continue
		}
		li := LinkInfo{Board: name, Transport: s.Transport, Where: s.Where, Since: s.since, Approved: s.live.Load()}
		if s.Transport == OverSerial {
			li.DefaultWifi = h.Wifi.Status(name)
		}
		if n := s.Net(); n != nil {
			li.Net = &NetInfo{Wifi: WifiStateName(n.Wifi), SSID: n.Ssid, IP: n.Ip, RSSI: int(n.Rssi), HasCert: n.HasCert}
		}
		out = append(out, li)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Board != out[j].Board {
			return out[i].Board < out[j].Board
		}
		return out[i].Transport < out[j].Transport
	})
	return out
}

// serial is the board's approved USB serial session, if it has one.
func (h *Hub) serial(board string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.live {
		if s.Transport == OverSerial && s.Board() == board && s.live.Load() {
			return s
		}
	}
	return nil
}

// WifiTimeout is how long `agentctl link wifi` waits for the board's answer.
var WifiTimeout = 5 * time.Second

// SendWifi gives a board Wi-Fi credentials over USB serial, or makes it
// forget them. A board that isn't on USB right now gets nothing: the deck
// keeps no copy to send later.
func (h *Hub) SendWifi(board, ssid, password string, forget bool) (int, string) {
	s := h.serial(board)
	if s == nil {
		for _, l := range h.Links() {
			if l.Board == board && !l.Approved {
				return http.StatusForbidden, board + " is waiting for approval: agentctl devices approve " + board
			}
		}
		return http.StatusConflict, board + " isn't connected over USB serial right now: plug it into this Mac " +
			"(agentctl link shows it as serial) and try again. Nothing was stored."
	}
	ctx, cancel := context.WithTimeout(context.Background(), WifiTimeout)
	defer cancel()
	code, msg, err := s.SendWifi(ctx, ssid, password, forget)
	switch {
	case err != nil:
		return http.StatusGatewayTimeout, board + " didn't answer (" + err.Error() + "): its firmware may not support Wi-Fi"
	case code == http.StatusNotImplemented && msg == "":
		msg = "this board's firmware has no Wi-Fi"
	}
	return code, msg
}

// SendDefault gives a board on USB serial the default network now (after
// `agentctl link wifi <board> --use-default`). 409 when it isn't on USB, 404
// when there is no default.
func (h *Hub) SendDefault(board string) (int, string) {
	if h.Wifi == nil {
		return http.StatusNotFound, "no default network: agentctl link wifi --default --ssid NAME"
	}
	c, gen, ok := h.Wifi.get()
	if !ok {
		return http.StatusNotFound, "no default network: agentctl link wifi --default --ssid NAME"
	}
	s := h.serial(board)
	if s == nil {
		return http.StatusConflict, board + " isn't on USB right now: it gets the default network next time it is plugged in"
	}
	h.Wifi.start(board, c.SSID, gen, s)
	code, msg := h.SendWifi(board, c.SSID, c.Password, false)
	if code >= 300 {
		h.Wifi.note(board, c.SSID, gen, fmt.Sprintf("refused: %d %s", code, msg), true, true)
	} else {
		h.Wifi.stored(board, c.SSID, gen)
	}
	return code, msg
}

// WifiStateName is a NetStatus.wifi in words.
func WifiStateName(w linkpb.WifiState) string {
	switch w {
	case linkpb.WifiState_WIFI_UNSUPPORTED:
		return "not supported"
	case linkpb.WifiState_WIFI_NOT_SET:
		return "not set"
	case linkpb.WifiState_WIFI_IDLE:
		return "set, off while on USB"
	case linkpb.WifiState_WIFI_JOINING:
		return "joining"
	case linkpb.WifiState_WIFI_CONNECTED:
		return "connected"
	case linkpb.WifiState_WIFI_WRONG_PASSWORD:
		return "wrong password"
	case linkpb.WifiState_WIFI_NO_NETWORK:
		return "network not found"
	case linkpb.WifiState_WIFI_FAILED:
		return "failed"
	case linkpb.WifiState_WIFI_DECK_UNREACHABLE:
		return "joined, deck unreachable"
	case linkpb.WifiState_WIFI_CERT_REJECTED:
		return "certificate rejected: pair again over USB"
	}
	return strings.ToLower(strings.TrimPrefix(w.String(), "WIFI_"))
}
