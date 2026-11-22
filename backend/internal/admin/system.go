package admin

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// System is GET /admin/v1/system: what the tray app's status line shows.
// Keep the shape: the tray app reads exactly these keys.
type System struct {
	Version  string    `json:"version"`
	Platform string    `json:"platform"`
	Install  string    `json:"install"`
	Started  time.Time `json:"started"`
	Boards   struct {
		Online  int `json:"online"`
		Total   int `json:"total"`
		Pending int `json:"pending"`
	} `json:"boards"`
	Ports struct {
		New int `json:"new"`
	} `json:"ports"`
	Update struct {
		State   string `json:"state"`
		Version string `json:"version"`
	} `json:"update"`
	FirmwareUpdates int `json:"firmware_updates"`
}

// SystemNow is the status line's numbers.
func (a *Admin) SystemNow() System {
	var s System
	s.Version, s.Platform, s.Install, s.Started = a.Version, a.Platform, a.Install, a.Started.UTC().Truncate(time.Second)
	devs := a.deviceRows()
	for _, d := range devs {
		if d.State == "revoked" {
			continue
		}
		s.Boards.Total++
		if d.State == "waiting" {
			s.Boards.Pending++
		}
		if d.Online {
			s.Boards.Online++
		}
		if d.Update != nil && d.Status != nil && (d.Status.State == FwUpdate || d.Status.State == FwLocal) { // not when this build can't install it
			s.FirmwareUpdates++
		}
	}
	for _, p := range a.portRows() {
		if p.State == PortNew {
			s.Ports.New++
		}
	}
	u := a.updaterState()
	s.Update.State, s.Update.Version = u.State, u.Version
	return s
}

func (a *Admin) system(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.SystemNow())
}

func (a *Admin) updaterState() update.State {
	if a.Updater == nil {
		return update.State{State: update.StateDisabled, Current: a.Version, Install: a.Install,
			Message: "This service can't update itself."}
	}
	return a.Updater.State()
}

// ---- settings -------------------------------------------------------------------

// Notification and update modes.
const (
	NotifyOff       = "off"       // never
	NotifyAttention = "attention" // when an agent needs you
	NotifyFinished  = "finished"  // also when one finishes

	UpdatesNotify = "notify" // tell me
	UpdatesAuto   = "auto"   // install by itself
	UpdatesOff    = "off"    // never check

	RestoreAsk  = "ask"
	RestoreAuto = "auto"

	SessionsPick = "pick" // Claude sessions started elsewhere join when you add them
	SessionsAuto = "auto" // each one joins the deck by itself
)

// Settings is GET /admin/v1/settings: every setting as one document.
type Settings struct {
	// StartAtLogin is nil where this platform has no way to start the
	// service at login; LoginNote says what is off about it.
	StartAtLogin *bool  `json:"startAtLogin"`
	LoginKind    string `json:"loginKind,omitempty"`
	LoginNote    string `json:"loginNote,omitempty"`
	// TrayAtLogin: the tray app starts at login too (Linux, when it is
	// installed); absent where Start when I log in covers only the service.
	TrayAtLogin *bool `json:"trayAtLogin,omitempty"`

	Notifications string `json:"notifications"` // off | attention | finished
	Sound         bool   `json:"sound"`
	Restore       string `json:"restore"`  // ask | auto
	Sessions      string `json:"sessions"` // pick | auto: Claude sessions started outside the deck
	Updates       string `json:"updates"`  // notify | auto | off

	// Network: paired boards and phones may connect over the network
	// (config.json deviceAddr). NetworkRunning is what this run of the
	// service does: a change applies when it restarts.
	Network        bool `json:"network"`
	NetworkRunning bool `json:"networkRunning"`

	// Magic: the magic trick (off by default). The giver and the receiver
	// are device names (/admin/v1/devices; the receiver needs "motion").
	Magic devices.Magic `json:"magic"`

	// Wifi is the network boards get on USB. Its password is never sent back.
	Wifi struct {
		SSID string `json:"ssid"`
	} `json:"wifi"`

	CLI CLIState `json:"cli"`
}

// CLIState: whether the agentctl command is there for terminals.
type CLIState struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	CanChange bool   `json:"canChange"` // this service can add or remove its link
}

func (a *Admin) SettingsNow() Settings {
	cfg := a.readConfig()
	var s Settings
	if a.Login != nil {
		reg := a.Login.Registration()
		on := reg.Registered
		s.StartAtLogin, s.LoginKind, s.LoginNote = &on, reg.Kind, reg.Note
	}
	if a.Tray.Available() {
		on := a.Tray.Enabled()
		s.TrayAtLogin = &on
	}
	s.Notifications = str(cfg, "notifications", NotifyAttention)
	s.Sound = boolean(cfg, "sound", false)
	s.Restore = str(cfg, "restore", RestoreAsk)
	if s.Restore != RestoreAuto {
		s.Restore = RestoreAsk
	}
	if s.Sessions = str(cfg, "sessions", SessionsPick); s.Sessions != SessionsAuto {
		s.Sessions = SessionsPick
	}
	s.Updates = str(cfg, "updates", UpdatesNotify)
	s.Network = str(cfg, "deviceAddr", ":7341") != ""
	s.NetworkRunning = a.NetworkOn
	if a.WifiSSID != nil {
		s.Wifi.SSID = a.WifiSSID()
	}
	s.CLI = a.cliState()
	if a.Devices != nil {
		s.Magic = a.Devices.Settings().Magic.WithDefaults()
	}
	return s
}

func (a *Admin) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.SettingsNow())
}

// SettingsPatch is PATCH /admin/v1/settings: only the keys given change.
type SettingsPatch struct {
	StartAtLogin  *bool          `json:"startAtLogin"`
	Notifications *string        `json:"notifications"`
	Sound         *bool          `json:"sound"`
	Restore       *string        `json:"restore"`
	Sessions      *string        `json:"sessions"`
	Updates       *string        `json:"updates"`
	Network       *bool          `json:"network"`
	Magic         *devices.Magic `json:"magic"` // the whole setting
	Wifi          *struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
		Forget   bool   `json:"forget"`
		// Send: also give it now to the boards on USB that follow the
		// default network.
		Send bool `json:"send"`
	} `json:"wifi"`
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (a *Admin) patchSettings(w http.ResponseWriter, r *http.Request) {
	var p SettingsPatch
	if err := readBody(r, 8192, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	switch {
	case p.Notifications != nil && !oneOf(*p.Notifications, NotifyOff, NotifyAttention, NotifyFinished):
		writeErr(w, http.StatusBadRequest, `notifications must be "off", "attention" or "finished"`)
		return
	case p.Restore != nil && !oneOf(*p.Restore, RestoreAsk, RestoreAuto):
		writeErr(w, http.StatusBadRequest, `restore must be "ask" or "auto"`)
		return
	case p.Sessions != nil && !oneOf(*p.Sessions, SessionsPick, SessionsAuto):
		writeErr(w, http.StatusBadRequest, `sessions must be "pick" or "auto"`)
		return
	case p.Updates != nil && !oneOf(*p.Updates, UpdatesNotify, UpdatesAuto, UpdatesOff):
		writeErr(w, http.StatusBadRequest, `updates must be "notify", "auto" or "off"`)
		return
	case p.StartAtLogin != nil && a.Login == nil:
		writeErr(w, http.StatusNotImplemented, "starting at login is not available on this platform")
		return
	}
	if p.Magic != nil {
		if a.Devices == nil {
			writeErr(w, http.StatusServiceUnavailable, "no device registry here")
			return
		}
		if err := a.Devices.SetMagic(*p.Magic); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if a.MagicChanged != nil {
			a.MagicChanged() // the screens learn their new part (or that it is off)
		}
	}
	if p.Wifi != nil && !p.Wifi.Forget {
		if err := server.CheckWifi(p.Wifi.SSID, p.Wifi.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	err := a.editConfig(func(cfg map[string]any) error {
		if p.Notifications != nil {
			cfg["notifications"] = *p.Notifications
		}
		if p.Sound != nil {
			cfg["sound"] = *p.Sound
		}
		if p.Restore != nil {
			cfg["restore"] = *p.Restore
		}
		if p.Sessions != nil {
			cfg["sessions"] = *p.Sessions
		}
		if p.Updates != nil {
			cfg["updates"] = *p.Updates
		}
		if p.Network != nil {
			if *p.Network {
				cfg["deviceAddr"] = ":7341"
			} else {
				cfg["deviceAddr"] = ""
			}
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	res := map[string]any{}
	if p.Wifi != nil {
		if a.WifiDefault == nil {
			writeErr(w, http.StatusServiceUnavailable, "Deck Link is off, so boards can't be given Wi-Fi here")
			return
		}
		// The password goes to the OS secret store and to boards over USB;
		// it is never logged, audited or sent back.
		if err := a.WifiDefault(p.Wifi.SSID, p.Wifi.Password, p.Wifi.Forget); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if p.Wifi.Send && !p.Wifi.Forget {
			res["sent"] = a.sendDefaultToUSB()
		}
	}
	if p.StartAtLogin != nil && a.Tray.Available() {
		if err := a.Tray.Set(*p.StartAtLogin); err != nil {
			writeErr(w, http.StatusInternalServerError, "the tray app's autostart entry: "+err.Error())
			return
		}
	}
	if p.StartAtLogin != nil {
		reg := a.Login.Registration()
		if *p.StartAtLogin != reg.Registered || (*p.StartAtLogin && reg.Note != "") {
			// Both restart or stop this very service, so they run after the answer.
			if *p.StartAtLogin {
				res["message"] = "The service now starts when you log in. It restarts to apply this."
				a.later(func() error { return a.Login.Install(io.Discard) })
			} else {
				res["message"] = "The service no longer starts when you log in. It stops now: start it again from the app or with agentctl serve."
				a.later(func() error { return a.Login.Uninstall(io.Discard) })
			}
		}
	}
	res["settings"] = a.SettingsNow()
	if s := res["settings"].(Settings); s.Network != s.NetworkRunning {
		res["restartNeeded"] = true
	}
	writeJSON(w, http.StatusOK, res)
}

// later runs fn a moment after the answer went out: it may restart or stop
// this service.
func (a *Admin) later(fn func() error) {
	go func() {
		time.Sleep(400 * time.Millisecond)
		if err := fn(); err != nil {
			log.Printf("settings: %v", err)
		}
	}()
}

// sendDefaultToUSB gives the default network to every board on USB or Bluetooth that
// follows it (not one given its own network). It returns the board names.
func (a *Admin) sendDefaultToUSB() []string {
	sent := []string{}
	if a.SendDefault == nil || a.Links == nil {
		return sent
	}
	for _, l := range a.Links() {
		if (l.Transport != link.OverSerial && l.Transport != link.OverBLE) || !l.Approved { // in hand: never over Wi-Fi
			continue
		}
		if d, ok := a.Devices.Get(l.Board); ok && d.WifiOwn {
			continue
		}
		if code, _ := a.SendDefault(l.Board); code < 300 {
			sent = append(sent, l.Board)
		}
	}
	return sent
}

// restart: POST /admin/v1/restart, through what starts it at login.
func (a *Admin) restart(w http.ResponseWriter, r *http.Request) {
	if a.Demo {
		writeErr(w, http.StatusConflict, "demo mode: the service isn't restarted")
		return
	}
	if a.Login == nil {
		writeErr(w, http.StatusNotImplemented, "this platform has no service manager: stop agentctl serve and start it again")
		return
	}
	reg := a.Login.Registration()
	if !reg.Registered || reg.Note != "" {
		writeErr(w, http.StatusConflict, "the service isn't set to start at login, so it can't restart itself: turn on Start when I log in, or stop agentctl serve and start it again")
		return
	}
	log.Printf("restart asked from Settings")
	a.later(func() error { return a.Login.Install(io.Discard) })
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "The service restarts now. Agents keep running; boards reconnect in a few seconds."})
}

// ---- the agentctl command for terminals ------------------------------------------

func (a *Admin) cliLink() string {
	if a.CLIDir == "" {
		return ""
	}
	name := "agentctl"
	if runtime.GOOS == "windows" {
		return ""
	}
	return filepath.Join(a.CLIDir, name)
}

func (a *Admin) cliState() CLIState {
	st := CLIState{}
	link := a.cliLink()
	if p, err := exec.LookPath("agentctl"); err == nil {
		st.Installed, st.Path = true, p
	}
	if link != "" && a.Exe != "" {
		if t, err := os.Readlink(link); err == nil {
			st.Installed, st.Path = true, link
			st.CanChange = t == a.Exe
		} else if _, err := os.Lstat(link); errors.Is(err, os.ErrNotExist) {
			st.CanChange = true
		}
	}
	return st
}

// cli: POST /admin/v1/cli {"installed": true|false} adds or removes the link
// to this agentctl in CLIDir. It never removes a file it didn't make.
func (a *Admin) cli(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Installed *bool `json:"installed"`
	}
	if err := readBody(r, 1024, &b); err != nil || b.Installed == nil {
		writeErr(w, http.StatusBadRequest, `body must be {"installed": true|false}`)
		return
	}
	link := a.cliLink()
	if link == "" || a.Exe == "" {
		writeErr(w, http.StatusNotImplemented, "the installer put agentctl in place; it can't be changed from here")
		return
	}
	if *b.Installed {
		if t, err := os.Readlink(link); err == nil && t == a.Exe {
			writeJSON(w, http.StatusOK, a.cliState())
			return
		}
		if _, err := os.Lstat(link); err == nil {
			writeErr(w, http.StatusConflict, link+" is already there and isn't this agentctl: remove it first")
			return
		}
		if err := os.MkdirAll(a.CLIDir, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := os.Symlink(a.Exe, link); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		t, err := os.Readlink(link)
		if err != nil {
			writeJSON(w, http.StatusOK, a.cliState())
			return
		}
		if t != a.Exe {
			writeErr(w, http.StatusConflict, link+" points to another agentctl: it stays")
			return
		}
		if err := os.Remove(link); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, a.cliState())
}

// openData: POST /admin/v1/open-data-folder shows the service's folder (its
// log and settings) in the file manager on this computer.
func (a *Admin) openData(w http.ResponseWriter, r *http.Request) {
	if a.Open == nil {
		writeErr(w, http.StatusNotImplemented, "can't open folders here: the log is "+a.LogFile)
		return
	}
	if err := a.Open(a.DataDir); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pair: POST /admin/v1/pair {"name", "scope"}: a one-time code a phone or
// panel trades for its own certificate on the network listener.
func (a *Admin) pair(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := readBody(r, 4096, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.Scope == "" {
		b.Scope = string(devices.View)
	}
	sc, err := devices.ParseScope(b.Scope)
	if err == nil {
		err = devices.ValidName(b.Name)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	url, ca := "", ""
	if a.PairInfo != nil {
		url, ca = a.PairInfo()
	}
	if url == "" || a.Devices == nil {
		writeErr(w, http.StatusConflict, "devices can't connect over the network: turn on Let paired devices connect over the network, then restart the service")
		return
	}
	code := server.RandomToken()[:20]
	if err := a.Devices.NewPairingCode(b.Name, sc, code); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": b.Name, "scope": string(sc), "code": code, "url": url, "ca": ca,
		"expires": time.Now().Add(devices.PairingCodeTTL).UTC().Format(time.RFC3339)})
}
