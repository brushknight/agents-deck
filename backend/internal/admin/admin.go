// Package admin is the management API, /admin/v1: what the Settings page,
// the tray app and the CLI use to run this computer's deck (settings, boards,
// USB ports and flashing, updates, diagnostics).
//
// It is served on the unix socket and on the loopback dashboard behind its
// login, and never on the network listener: no board or phone can reach it,
// whatever its scope. Boards never manage the computer, so these calls have
// no Deck Link counterpart (docs/protocol.md, "Management API").
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/ble"
	"github.com/brushknight/agents-deck/backend/internal/cpuguard"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/serial"
	"github.com/brushknight/agents-deck/backend/internal/service"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// Flasher talks to a board's ROM bootloader on a serial port (flash.Probe and
// flash.Write on a port it opens itself; tests and demo mode use fakes).
type Flasher interface {
	Probe(ctx context.Context, port serial.Info) (flash.Chip, error)
	Write(ctx context.Context, port serial.Info, image io.Reader, size int64, offset uint32, prog flash.Progress) error
}

// Firmware is the firmware catalog of releases (firmware.Client).
type Firmware interface {
	Latest(ctx context.Context) (firmware.Catalog, error)
	Image(ctx context.Context, board, version string) (string, error)
}

// Updater checks for and installs new versions of agents deck (update.Updater).
type Updater interface {
	State() update.State
	Check(ctx context.Context) (update.State, error)
	Download(ctx context.Context) (update.State, error)
	Apply(ctx context.Context) error
}

// OTA offers a board on Wi-Fi a new firmware version, which it then downloads
// from the service over its own link (link.Hub.OfferFirmware). Its progress
// comes back through FirmwareChanged (link.Hub.OnFirmware). nil: this
// service can't update boards over Wi-Fi, and a Wi-Fi board says so.
type OTA interface {
	OfferFirmware(deviceID, board, version string) error
}

// Bluetooth finds boards nearby, adds them (the person types the code shown
// on the board) and says how each board's Bluetooth link is (ble.Uplinks).
type Bluetooth interface {
	Status() ble.Status
	Scan(on bool) error
	Nearby() []ble.Nearby
	Seen(id string) (ble.Nearby, bool)
	Add(ctx context.Context, id string, scope devices.Scope, prog ble.Progress) (devices.Device, error)
	State(d devices.Device) ble.LinkState
}

// ErrNoOTA: this build can't update a board over Wi-Fi.
var ErrNoOTA = errors.New("over-the-air update is not available in this build: plug the board in with a USB cable to update it")

// Admin serves /admin/v1. Fields left nil turn their part off with a clear
// answer, so the page can say what this service can't do.
type Admin struct {
	Version  string    // this service's version
	Platform string    // GOOS/GOARCH
	Install  string    // update.InstallKind
	Started  time.Time // when the service started
	Demo     bool      // serve --demo: pretend ports, boards and releases

	Store   *store.Store
	Devices *devices.Registry
	Config  string // config.json
	DataDir string // the service's data folder
	LogFile string // the service's log

	// Deck Link: boards connected now, the registered ports' state, the
	// default Wi-Fi network and sending Wi-Fi to a board on USB.
	Links       func() []link.LinkInfo
	PortStatus  func() []link.PortStatus
	WifiSSID    func() string
	WifiDefault func(ssid, password string, forget bool) error
	SendWifi    func(board, ssid, password string, forget bool) (int, string)
	SendDefault func(board string) (int, string)
	// PairInfo is where a new device connects: the network listener's URL
	// and the CA fingerprint ("" when the listener is off).
	PairInfo func() (url, ca string)

	ListPorts func() ([]serial.Info, error) // serial.List
	// MagicChanged tells the screens the magic trick's settings changed.
	MagicChanged func()
	Flasher      Flasher
	Firmware     Firmware
	Updater      Updater
	Login        service.Manager // start at login; nil where there is none
	// Tray: the Linux tray app's autostart entry, switched with Start when I
	// log in (nil elsewhere).
	Tray *TrayAutostart
	OTA  OTA
	// Local takes firmware files picked on this computer for boards on
	// Wi-Fi (link.Hub). nil: files go over USB only.
	Local LocalFirmware
	// Bluetooth: boards over Bluetooth (ble.go; *ble.Uplinks, or the demo's).
	// nil: this computer has none.
	Bluetooth Bluetooth
	// CPU: how much CPU the service and its helpers use (proc.CPUMeter).
	// nil: 404.
	CPU func() proc.CPUReport
	// CPUTop: the n biggest users of the machine's CPU (proc.TopUsers), for
	// ?top=n. nil: the report says it isn't available.
	CPUTop func(n int) proc.CPUTop
	// Guard: the CPU guard (cpuguard), its state in GET /admin/v1/system
	// and its setting in /admin/v1/settings. nil: neither.
	Guard *cpuguard.Guard
	// Slower: background mode for agents while the computer is busy
	// (Slow it, Full speed). nil: neither.
	Slower *cpuguard.Slower

	// Tokens are the MCP access tokens (mcp.go); MCPURL is where MCP clients
	// connect over HTTP (http://127.0.0.1:<port>/mcp). nil: no MCP section.
	Tokens *devices.Tokens
	MCPURL string

	// NetworkOn: the network listener runs in this process (it starts or
	// stops only with the service).
	NetworkOn bool
	// Open shows a folder in the file manager (proc.Open).
	Open func(path string) error
	// CLIDir is where "install the agentctl command" puts its link
	// (~/.local/bin); Exe is the agentctl it links to.
	CLIDir, Exe string
	// FirstCheck is how long after start the first update check runs
	// (default 2 minutes); CheckEvery is the time between checks (a day).
	FirstCheck, CheckEvery time.Duration

	// Starter starts agents in a project's folder (the live daemon); nil:
	// projects can be kept but not started (demo mode).
	Starter Starter

	cfgMu    sync.Mutex
	projects projectStore
	jobs     jobs
	ports    portHolds
	catMu    sync.Mutex
	catalog  *firmware.Catalog
	catAt    time.Time
	catErr   string
	catBusy  bool
	updMu    sync.Mutex
	checked  time.Time
	labels   labelBook
	ota      otaWatch
	files    fileBook
	probes   probeBook
}

// Handler serves /admin/v1/… . The caller has already decided who may reach
// it: the unix socket, or the dashboard's session cookie.
func (a *Admin) Handler() http.Handler {
	a.jobs.init()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/v1/system", a.system)
	mux.HandleFunc("GET /admin/v1/settings", a.getSettings)
	mux.HandleFunc("PATCH /admin/v1/settings", a.patchSettings)
	mux.HandleFunc("POST /admin/v1/restart", a.restart)
	mux.HandleFunc("POST /admin/v1/cli", a.cli)
	mux.HandleFunc("POST /admin/v1/open-data-folder", a.openData)

	mux.HandleFunc("GET /admin/v1/agents", a.agents)
	mux.HandleFunc("POST /admin/v1/accounts/{name}/color", a.accountColor)
	mux.HandleFunc("POST /admin/v1/accounts/{name}/default", a.accountDefault)
	mux.HandleFunc("GET /admin/v1/projects", a.projectsList)
	mux.HandleFunc("POST /admin/v1/projects", a.projectsAdd)
	mux.HandleFunc("POST /admin/v1/projects/{name}/{action}", a.projectAction)

	mux.HandleFunc("GET /admin/v1/devices", a.listDevices)
	mux.HandleFunc("POST /admin/v1/devices/{id}/{action}", a.deviceAction)
	mux.HandleFunc("POST /admin/v1/devices/{id}/firmware-file", a.firmwareFile)
	mux.HandleFunc("POST /admin/v1/devices/{id}/install-file", a.installFile)
	mux.HandleFunc("POST /admin/v1/pair", a.pair)

	mux.HandleFunc("GET /admin/v1/ports", a.listPorts)
	mux.HandleFunc("POST /admin/v1/ports/{id}/{action}", a.portAction)
	mux.HandleFunc("POST /admin/v1/ports/{id}/firmware-file", a.portFirmwareFile)
	mux.HandleFunc("POST /admin/v1/ports/{id}/install-file", a.portInstallFile)

	mux.HandleFunc("GET /admin/v1/bluetooth", a.bluetoothStatus)
	mux.HandleFunc("POST /admin/v1/bluetooth/scan", a.bluetoothScan)
	mux.HandleFunc("GET /admin/v1/bluetooth/boards", a.bluetoothBoards)
	mux.HandleFunc("POST /admin/v1/bluetooth/boards/{id}/add", a.bluetoothAdd)

	mux.HandleFunc("GET /admin/v1/firmware", a.firmwareCatalog)
	mux.HandleFunc("GET /admin/v1/firmware/builds", a.listBuilds)
	mux.HandleFunc("POST /admin/v1/firmware/builds", a.addBuild)
	mux.HandleFunc("DELETE /admin/v1/firmware/builds/{board}/{version}", a.removeBuild)
	mux.HandleFunc("GET /admin/v1/jobs", a.listJobs)
	mux.HandleFunc("GET /admin/v1/jobs/{id}", a.getJob)
	mux.HandleFunc("POST /admin/v1/jobs/{id}/cancel", a.cancelJob)
	mux.HandleFunc("GET /admin/v1/events", a.events)

	mux.HandleFunc("GET /admin/v1/update", a.updateState)
	mux.HandleFunc("POST /admin/v1/update/check", a.updateCheck)
	mux.HandleFunc("POST /admin/v1/update/apply", a.updateApply)

	mux.HandleFunc("GET /admin/v1/mcp", a.mcpInfo)
	mux.HandleFunc("POST /admin/v1/mcp/tokens", a.mcpCreate)
	mux.HandleFunc("POST /admin/v1/mcp/tokens/{name}/{action}", a.mcpAction)

	mux.HandleFunc("GET /admin/v1/diagnostics", a.diagnostics)
	mux.HandleFunc("GET /admin/v1/cpu", a.cpu)
	mux.HandleFunc("POST /admin/v1/cpu/release", a.cpuRelease)
	mux.HandleFunc("POST /admin/v1/cpu/slow/{id}", a.cpuSlow(true))
	mux.HandleFunc("POST /admin/v1/cpu/full-speed/{id}", a.cpuSlow(false))
	mux.HandleFunc("/admin/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "no such call: see docs/protocol.md, Management API")
	})
	return mux
}

// ---- helpers ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// readBody decodes an optional JSON body (empty is fine).
func readBody(r *http.Request, max int64, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, max))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return errors.New("the body is not valid JSON")
	}
	return nil
}
