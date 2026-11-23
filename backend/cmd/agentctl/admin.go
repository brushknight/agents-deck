package main

// The management API (/admin/v1, internal/admin) as the service runs it.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/admin"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/serial"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// newAdmin makes the management API before the Deck Link manager, which asks
// it which registered ports it may open (the flasher borrows them).
func newAdmin(st *store.Store, srv *server.Server, self, logFile string) *admin.Admin {
	if logFile == "" {
		logFile = paths.Log()
	}
	upd := update.New(version)
	home, _ := os.UserHomeDir()
	a := &admin.Admin{
		Version: version, Platform: runtime.GOOS + "/" + runtime.GOARCH, Install: upd.Kind, Started: time.Now(),
		Store: st, Devices: srv.Devices, Config: paths.Config(), DataDir: paths.Data(), LogFile: logFile,
		ListPorts: serial.List, Flasher: admin.SerialFlasher{},
		// One release reader for both: the app's updates and the boards' firmware
		// share its cache and its one-download-at-a-time lock.
		Updater: upd, Firmware: &firmware.Client{Releases: upd.Releases},
		Open: proc.Open, Exe: self, MagicChanged: srv.MagicChanged,
		Tokens: srv.Tokens, MCPURL: "http://" + srv.LocalAddr + "/mcp",
	}
	if home != "" {
		a.CLIDir = filepath.Join(home, ".local", "bin")
		if runtime.GOOS == "linux" {
			a.Tray = &admin.TrayAutostart{Home: home}
		}
	}
	if m, _, err := serviceManager(); err == nil {
		a.Login = m
	}
	return a
}

// wireAdmin connects the management API to Deck Link and the listeners, and
// starts the update checks. demo: made-up boards, ports and releases.
func wireAdmin(a *admin.Admin, srv *server.Server, hub *link.Hub, links *link.Manager, wifi *link.WifiDefault, network, demo bool) {
	a.Links, a.PortStatus = hub.Links, links.Status
	a.WifiSSID, a.WifiDefault = wifi.SSID, srv.LinkWifiDefault
	a.SendWifi, a.SendDefault = hub.SendWifi, hub.SendDefault
	a.NetworkOn = network
	a.PairInfo = func() (string, string) {
		if srv.CA == nil {
			return "", ""
		}
		return srv.DeviceURL, srv.CA.Fingerprint()
	}

	// Over-the-air updates for boards on Wi-Fi: the hub offers them and serves
	// the images from the same release catalog the flasher uses; its progress
	// reaches the jobs through hub.OnFirmware (set where the hub is made).
	// Boards on USB update through the flasher.
	if c, ok := a.Firmware.(*firmware.Client); ok {
		hub.SetFirmwareSource(admin.OTASource{Client: c})
	}
	a.OTA, a.Local = hub, hub
	if st, ok := srv.Ctl.(admin.Starter); ok {
		a.Starter = st // Home starts agents in a project
	}

	if demo {
		if err := admin.UseDemo(a); err != nil {
			log.Printf("settings demo: %v", err)
		}
	}
	srv.Admin = a.Handler()
	if !demo {
		go a.RunUpdates(context.Background())
	}
}
