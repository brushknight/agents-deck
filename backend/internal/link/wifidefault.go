package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// WifiDefault is the deck's default Wi-Fi network (`agentctl link wifi
// --default`): the owner sets it once, and every approved board that comes
// up on a registered USB port, paired for Wi-Fi and without that network,
// gets it over the cable. The password lives in the OS secret store (the
// macOS Keychain; a 0600 file elsewhere) and is never logged or audited.
type WifiDefault struct {
	Store pki.Keystore

	mu     sync.Mutex
	loaded bool
	cur    *wifiCreds
	gen    int                     // counts changes of the default, to tell an old one from the current
	boards map[string]*wifiOutcome // by board name: what this daemon last sent it
}

type wifiCreds struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type wifiOutcome struct {
	gen     int
	ssid    string
	state   string // sending | testing | ok | the failure, in words
	failed  bool
	settled bool
	at      time.Time
	owner   any // the session that sent it: only its unplugging counts
}

// wifiDefaultKey names the default network in the secret store.
const wifiDefaultKey = "wifi-default"

func (w *WifiDefault) load() {
	if w.loaded {
		return
	}
	w.loaded = true
	w.boards = map[string]*wifiOutcome{}
	if w.Store == nil {
		return
	}
	b, err := w.Store.Load(wifiDefaultKey)
	if err != nil {
		if !errors.Is(err, pki.ErrNoKey) {
			log.Printf("wifi: can't read the default network: %v", err)
		}
		return
	}
	var c wifiCreds
	if json.Unmarshal(b, &c) == nil && c.SSID != "" {
		w.cur = &c
	}
}

// SSID is the default network's name ("" when none is set).
func (w *WifiDefault) SSID() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	if w.cur == nil {
		return ""
	}
	return w.cur.SSID
}

// get returns the default and its generation.
func (w *WifiDefault) get() (wifiCreds, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	if w.cur == nil {
		return wifiCreds{}, 0, false
	}
	return *w.cur, w.gen, true
}

// Set stores the default network (the caller checks it with server.CheckWifi).
func (w *WifiDefault) Set(ssid, password string) error {
	if w == nil || w.Store == nil {
		return errors.New("no secret store for the default network")
	}
	b, _ := json.Marshal(wifiCreds{SSID: ssid, Password: password})
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	if err := w.Store.Save(wifiDefaultKey, b); err != nil {
		return err
	}
	w.cur = &wifiCreds{SSID: ssid, Password: password}
	w.gen++
	log.Printf("wifi: default network set to %q", ssid)
	return nil
}

// Clear forgets the default network. Boards keep what they were given.
func (w *WifiDefault) Clear() error {
	if w == nil || w.Store == nil {
		return errors.New("no secret store for the default network")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	if err := w.Store.Delete(wifiDefaultKey); err != nil {
		return err
	}
	w.cur = nil
	w.gen++
	log.Printf("wifi: default network cleared")
	return nil
}

// Status is what happened to the default on a board, for `agentctl link`
// ("" if this daemon never sent it there).
func (w *WifiDefault) Status(board string) string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	o := w.boards[board]
	if o == nil {
		return ""
	}
	return fmt.Sprintf("default %s: %s", o.ssid, o.state)
}

// wanted: should a board reporting n (on USB, approved, paired) get the
// default now? Not when it has it already, unless it failed with it or the
// default changed since this daemon sent it (a new password).
func (w *WifiDefault) wanted(board string, n *linkpb.NetStatus) (wifiCreds, int, bool) {
	c, gen, ok := w.get()
	if !ok || !n.HasCert {
		return c, gen, false
	}
	switch n.Wifi {
	case linkpb.WifiState_WIFI_UNSUPPORTED, linkpb.WifiState_WIFI_UNSPECIFIED, linkpb.WifiState_WIFI_JOINING:
		return c, gen, false
	case linkpb.WifiState_WIFI_NOT_SET:
		return c, gen, true
	}
	if n.Ssid != c.SSID {
		return c, gen, true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	o := w.boards[board]
	return c, gen, o != nil && o.settled && (o.failed || o.gen != gen)
}

// note records what happened with the default on a board, and logs it.
func (w *WifiDefault) note(board, ssid string, gen int, state string, failed, settled bool) {
	w.mu.Lock()
	o := &wifiOutcome{gen: gen, ssid: ssid, state: state, failed: failed, settled: settled, at: time.Now()}
	w.boards[board] = o
	w.mu.Unlock()
	if state != "sending" {
		log.Printf("wifi: sent default network %q to %s: %s", ssid, board, state)
	}
}

// start records that a session is sending the default to a board.
func (w *WifiDefault) start(board, ssid string, gen int, owner any) {
	w.mu.Lock()
	w.boards[board] = &wifiOutcome{gen: gen, ssid: ssid, state: "sending", owner: owner, at: time.Now()}
	w.mu.Unlock()
}

// stored: the board took the network and tests it. The board's own report
// may have come first; that one stays.
func (w *WifiDefault) stored(board, ssid string, gen int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if o := w.boards[board]; o != nil && !o.settled && o.ssid == ssid && o.gen == gen {
		o.state = "stored, testing"
	}
}

// gone: the session that sent the default ended before the board said how it
// went; a later session's attempt is left alone.
func (w *WifiDefault) gone(board string, owner any) {
	w.mu.Lock()
	o := w.boards[board]
	if o == nil || o.settled || o.owner != owner {
		w.mu.Unlock()
		return
	}
	ssid, gen := o.ssid, o.gen
	w.mu.Unlock()
	w.note(board, ssid, gen, "no answer: unplugged while testing", true, true)
}

// pending: the board was sent the default and hasn't said how it went.
func (w *WifiDefault) pending(board string) (string, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.load()
	o := w.boards[board]
	if o == nil || o.settled {
		return "", 0, false
	}
	return o.ssid, o.gen, true
}

// outcome reads the board's NetStatus after it was sent the default: "" while
// it is still trying.
func outcome(n *linkpb.NetStatus) (state string, failed bool) {
	switch n.Wifi {
	case linkpb.WifiState_WIFI_IDLE, linkpb.WifiState_WIFI_CONNECTED:
		if n.Ip == "" { // stored, not tried yet
			return "", false
		}
		return "ok", false
	case linkpb.WifiState_WIFI_WRONG_PASSWORD, linkpb.WifiState_WIFI_NO_NETWORK, linkpb.WifiState_WIFI_FAILED:
		return WifiStateName(n.Wifi), true
	}
	return "", false
}
