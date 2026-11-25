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
	// OnFirmware is told every change of a board update over Wi-Fi (ota.go),
	// for the management API's jobs and events (optional).
	OnFirmware func(FirmwareJob)

	mu       sync.Mutex
	live     map[*Session]bool
	firmware FirmwareSource        // nil until SetFirmwareSource: no updates over Wi-Fi
	local    map[string]localImage // "board/version": images from files (UpdateFromFile)
	fwOnce   sync.Once
	fwBook   *firmwareBook
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
	Transport string    `json:"transport"` // serial | wifi | ble
	Where     string    `json:"where"`     // the port, or the board's address
	Since     time.Time `json:"since"`
	Approved  bool      `json:"approved"`
	Net       *NetInfo  `json:"net,omitempty"` // what the board reports
	// DefaultWifi: what happened when the deck sent this board its default
	// network ("default lab: ok", "default lab: wrong password"…).
	DefaultWifi string `json:"defaultWifi,omitempty"`
	// UplinkNote: what went wrong sending the board its uplink preference.
	UplinkNote string `json:"uplinkNote,omitempty"`
}

// NetInfo is a board's NetStatus in words.
type NetInfo struct {
	Wifi    string `json:"wifi"` // WifiStateName
	SSID    string `json:"ssid,omitempty"`
	IP      string `json:"ip,omitempty"`
	RSSI    int    `json:"rssi,omitempty"`
	HasCert bool   `json:"hasCert"`
	// Bluetooth as the board reports it (BleStateName: not supported, in
	// setup, paired, connected; "" from firmware older than Bluetooth), and
	// the uplink preference it holds (auto, wifi, ble; "" without Bluetooth).
	BLE    string `json:"ble,omitempty"`
	Uplink string `json:"uplink,omitempty"`
}

// netInfo is a NetStatus in words.
func netInfo(n *linkpb.NetStatus) *NetInfo {
	ni := &NetInfo{Wifi: WifiStateName(n.Wifi), SSID: n.Ssid, IP: n.Ip, RSSI: int(n.Rssi), HasCert: n.HasCert, BLE: BleStateName(n.Ble)}
	if n.Ble != linkpb.BleState_BLE_UNSPECIFIED && n.Ble != linkpb.BleState_BLE_UNSUPPORTED {
		ni.Uplink = UplinkName(n.Uplink)
	}
	return ni
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
		li := LinkInfo{Board: name, Transport: s.Transport, Where: s.Where, Since: s.since, Approved: s.live.Load(), UplinkNote: s.UplinkNote()}
		if s.Hands() {
			li.DefaultWifi = h.Wifi.Status(name)
		}
		if n := s.Net(); n != nil {
			li.Net = netInfo(n)
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

// serial is the board's approved session over USB serial or Bluetooth (the
// links where the board is in the owner's hands), if it has one.
func (h *Hub) serial(board string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.live {
		if s.Hands() && s.Board() == board && s.live.Load() {
			return s
		}
	}
	return nil
}

// WifiTimeout is how long `agentctl link wifi` waits for the board's answer.
var WifiTimeout = 5 * time.Second

// SendWifi gives a board Wi-Fi credentials over USB serial or Bluetooth, or
// makes it forget them. A board that isn't on either right now gets nothing:
// the deck keeps no copy to send later.
func (h *Hub) SendWifi(board, ssid, password string, forget bool) (int, string) {
	s := h.serial(board)
	if s == nil {
		for _, l := range h.Links() {
			if l.Board == board && !l.Approved {
				return http.StatusForbidden, board + " is waiting for approval: agentctl devices approve " + board
			}
		}
		return http.StatusConflict, board + " isn't connected over USB serial or Bluetooth right now: plug it into this computer " +
			"(agentctl link shows it as usb or bluetooth) and try again. Nothing was stored."
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

// SendDefault gives a board on USB serial or Bluetooth the default network
// now (after `agentctl link wifi <board> --use-default`). 409 when it is on
// neither, 404 when there is no default.
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
		return http.StatusConflict, board + " isn't on USB or Bluetooth right now: it gets the default network next time it is plugged in or connects over Bluetooth"
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
