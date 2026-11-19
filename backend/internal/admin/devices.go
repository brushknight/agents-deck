package admin

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// DeviceRow is one paired board or device in GET /admin/v1/devices.
type DeviceRow struct {
	ID         string    `json:"id"`   // its id in the registry (agentctl devices)
	Name       string    `json:"name"` // what you called it in Settings, else the id
	Kind       string    `json:"kind"` // serial (a Deck Link board) | network (a phone, a panel)
	Scope      string    `json:"scope"`
	State      string    `json:"state"` // active | waiting (for your approval) | revoked
	Online     bool      `json:"online"`
	Connection string    `json:"connection,omitempty"` // usb | wifi | bluetooth | network
	Port       string    `json:"port,omitempty"`       // the port's id, on USB
	LastSeen   time.Time `json:"lastSeen,omitzero"`
	Created    time.Time `json:"created"`
	Type       string    `json:"type,omitempty"`     // what its firmware reports
	Board      string    `json:"board,omitempty"`    // the catalog board it is
	Model      string    `json:"model,omitempty"`    // that board's name
	CanWifi    bool      `json:"canWifi,omitempty"`  // its firmware has Wi-Fi
	Firmware   string    `json:"firmware,omitempty"` // its version
	Wifi       *WifiRow  `json:"wifi,omitempty"`
	Update     *Pending  `json:"update,omitempty"` // a newer firmware in the latest release
	Job        string    `json:"job,omitempty"`    // a flash or update that runs
	// Boards: the uplink the deck uses now (usb | wifi | bluetooth, as
	// connection), the preference that picks it, and every uplink it has.
	UplinkPref string      `json:"uplinkPref,omitempty"` // auto | wifi | ble
	Uplinks    []UplinkRow `json:"uplinks,omitempty"`
	Bluetooth  bool        `json:"bluetooth,omitempty"` // added over Bluetooth on this computer
	// What the board itself says while connected (NetStatus): its Bluetooth
	// (not supported, in setup, paired, connected) and the uplink preference
	// it holds (auto, wifi, ble); and what went wrong sending it the
	// preference.
	BLEState    string `json:"bleState,omitempty"`
	BoardUplink string `json:"boardUplink,omitempty"`
	UplinkNote  string `json:"uplinkNote,omitempty"`
	// HasBluetooth: its firmware has Bluetooth (it said so, or it was added
	// over Bluetooth), so its uplink preference means something.
	HasBluetooth bool `json:"hasBluetooth,omitempty"`
	// Chip is the catalog board's chip ("ESP32-S3"); Status the one line
	// about its firmware updates. FileVia: how a firmware file picked on this
	// computer would reach it now (usb, wifi), FileNote why it can't.
	Chip     string          `json:"chip,omitempty"`
	Status   *FirmwareStatus `json:"status,omitempty"`
	FileVia  []string        `json:"fileVia,omitempty"`
	FileNote string          `json:"fileNote,omitempty"`
}

// WifiRow is a board's Wi-Fi as it reports it.
type WifiRow struct {
	State   string `json:"state,omitempty"` // connected, connecting, off, …
	SSID    string `json:"ssid,omitempty"`
	Own     bool   `json:"own"`               // given its own network, not the default one
	HasCert bool   `json:"hasCert"`           // paired for Wi-Fi
	Note    string `json:"note,omitempty"`    // what happened with the default network
	Address string `json:"address,omitempty"` // where it connects from on Wi-Fi
}

// Pending is a firmware update for a board.
type Pending struct {
	Version string `json:"version"`
	How     string `json:"how"`            // usb | wifi | unplugged
	Ready   bool   `json:"ready"`          // it can be updated now
	Note    string `json:"note,omitempty"` // why not, in words
}

func (a *Admin) linksNow() []link.LinkInfo {
	if a.Links == nil {
		return nil
	}
	return a.Links()
}

// deviceRows: every device in the registry, with how it is connected now.
func (a *Admin) deviceRows() []DeviceRow {
	if a.Devices == nil {
		return nil
	}
	a.refreshCatalog()
	cat, catErr := a.cachedCatalog()
	key := a.releaseKey()
	labels := a.Labels()
	links := map[string][]link.LinkInfo{}
	for _, l := range a.linksNow() {
		links[l.Board] = append(links[l.Board], l)
	}
	registered := map[string]bool{}
	for _, p := range a.registeredPorts() {
		registered[p.Path] = true
	}
	var out []DeviceRow
	for _, d := range a.Devices.List() {
		row := DeviceRow{ID: d.Name, Name: d.Name, Kind: d.Kind, Scope: string(d.Scope), LastSeen: d.LastSeen,
			Created: d.Created, Type: d.Type, Firmware: d.Firmware}
		if n := labels[d.Name]; n != "" {
			row.Name = n
		}
		switch {
		case d.Revoked:
			row.State = "revoked"
		case d.Kind == devices.Serial && !d.Approved:
			row.State = "waiting"
		default:
			row.State = "active"
		}
		usbPath := ""
		for _, l := range links[d.Name] {
			row.Online = true
			switch l.Transport {
			case link.OverSerial:
				row.Connection, usbPath = "usb", l.Where
				row.Port = portID(l.Where)
			case link.OverWifi:
				if row.Connection == "" {
					row.Connection = "wifi"
				}
			case link.OverBLE:
				if row.Connection == "" {
					row.Connection = "bluetooth"
				}
			}
			if l.Net != nil && l.Net.BLE != "" && (row.BLEState == "" || l.Transport == link.OverBLE) {
				row.BLEState, row.BoardUplink = l.Net.BLE, l.Net.Uplink
			}
			if l.UplinkNote != "" {
				row.UplinkNote = l.UplinkNote
			}
			if l.Net != nil || d.Kind == devices.Serial {
				w := row.Wifi
				if w == nil {
					w = &WifiRow{Own: d.WifiOwn, HasCert: d.CertSerial != ""}
				}
				if l.Net != nil {
					w.State, w.SSID, w.HasCert = l.Net.Wifi, l.Net.SSID, w.HasCert || l.Net.HasCert
				}
				if l.Transport == link.OverWifi {
					w.Address = l.Where
				}
				if l.DefaultWifi != "" {
					w.Note = l.DefaultWifi
				}
				row.Wifi = w
			}
		}
		if d.Kind == devices.Network && !row.Online && !d.LastSeen.IsZero() && time.Since(d.LastSeen) < 90*time.Second {
			row.Online, row.Connection = true, "network"
		}
		if d.Kind == devices.Serial && row.Wifi == nil {
			row.Wifi = &WifiRow{Own: d.WifiOwn, HasCert: d.CertSerial != ""}
		}
		if d.Kind == devices.Serial {
			a.uplinkRows(&row, d, links[d.Name])
		}
		var board *firmware.Board
		if cat != nil && d.Type != "" {
			if b, ok := cat.BoardForType(d.Type); ok {
				row.Board, row.Model, row.CanWifi, row.Chip = b.Board, b.Name, b.Wifi, b.Chip
				board = &b
			}
		}
		if d.Kind == devices.Serial && d.Type != "" && !d.Revoked {
			in := fwInput{Running: d.Firmware, Catalog: cat, CatalogErr: catErr, Board: board, ReleaseKey: key,
				Registered: registered[usbPath], OTA: a.OTA != nil}
			if row.Online {
				in.Connection = row.Connection
			}
			st := firmwareStatus(in)
			row.Status = &st
			if st.State == FwUpdate || st.State == FwNoKey {
				in.ReleaseKey = true
				u := firmwareStatus(in)
				row.Update = &Pending{Version: u.Latest, How: u.How, Ready: u.Ready, Note: u.Note}
				if st.State == FwNoKey {
					row.Update.Ready, row.Update.Note = false, "this build has no release key, so it can't install firmware from a release"
				}
			}
			row.FileVia, row.FileNote = a.fileWays(row)
		}
		if j, ok := a.jobs.running(func(j Job) bool { return j.Device == d.Name || (row.Port != "" && j.Port == row.Port) }); ok {
			row.Job = j.ID
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (a *Admin) listDevices(w http.ResponseWriter, r *http.Request) {
	if a.Devices == nil {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no devices")
		return
	}
	rows := a.deviceRows()
	if rows == nil {
		rows = []DeviceRow{}
	}
	cat, catErr := a.cachedCatalog()
	res := map[string]any{"devices": rows, "releaseKey": a.releaseKey(), "ota": a.OTA != nil}
	if cat != nil {
		res["latest"] = cat.Version
	}
	if catErr != "" {
		res["catalogError"] = catErr
	}
	if a.WifiSSID != nil {
		res["wifiDefault"] = a.WifiSSID()
	}
	writeJSON(w, http.StatusOK, res)
}

// deviceAction: POST /admin/v1/devices/{id}/{approve|scope|rename|revoke|remove|wifi|update|uplink}.
func (a *Admin) deviceAction(w http.ResponseWriter, r *http.Request) {
	if a.Devices == nil {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no devices")
		return
	}
	id := r.PathValue("id")
	d, ok := a.Devices.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no device called "+id)
		return
	}
	var err error
	switch r.PathValue("action") {
	case "approve":
		err = a.Devices.Approve(id)
	case "revoke":
		err = a.Devices.Revoke(id)
	case "remove":
		err = a.Devices.Remove(id)
		if err == nil {
			_ = a.setLabel(id, "")
		}
	case "scope":
		var b struct {
			Scope string `json:"scope"`
		}
		if err = readBody(r, 1024, &b); err == nil {
			var sc devices.Scope
			if sc, err = devices.ParseScope(b.Scope); err == nil {
				err = a.Devices.SetScope(id, sc)
			}
		}
	case "rename":
		var b struct {
			Name string `json:"name"`
		}
		if err = readBody(r, 1024, &b); err == nil {
			if err = checkLabel(b.Name); err == nil {
				err = a.setLabel(id, strings.TrimSpace(b.Name))
			}
		}
	case "wifi":
		a.deviceWifi(w, r, d)
		return
	case "update":
		a.deviceUpdate(w, r, d)
		return
	case "uplink":
		err = a.deviceUplink(r, d)
	default:
		writeErr(w, http.StatusNotFound, "unknown action: approve, scope, rename, revoke, remove, wifi, update or uplink")
		return
	}
	switch {
	case errors.Is(err, devices.ErrUnknown):
		writeErr(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Store != nil {
		a.Store.Touch() // the devices list on the dashboard
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkLabel: a name for people, 1 to 40 characters, printable.
func checkLabel(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 40 {
		return errors.New("a name has 1 to 40 characters")
	}
	for _, c := range s {
		if !unicode.IsPrint(c) {
			return errors.New("a name can't hold control characters")
		}
	}
	return nil
}

// deviceWifi: {"ssid", "password"}, {"forget": true} or {"useDefault": true}
// for a board on USB. The password goes over the cable and is never kept,
// logged or sent back.
func (a *Admin) deviceWifi(w http.ResponseWriter, r *http.Request, d devices.Device) {
	var b struct {
		SSID       string `json:"ssid"`
		Password   string `json:"password"`
		Forget     bool   `json:"forget"`
		UseDefault bool   `json:"useDefault"`
	}
	if err := readBody(r, 4096, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Kind != devices.Serial {
		writeErr(w, http.StatusBadRequest, d.Name+" isn't a board: only boards get Wi-Fi from here")
		return
	}
	if b.UseDefault {
		if err := a.Devices.SetWifiOwn(d.Name, false); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		sent, msg := false, "It gets the default network next time it is plugged in."
		if a.SendDefault != nil {
			code, m := a.SendDefault(d.Name)
			switch {
			case code < 300:
				sent, msg = true, "Sent the default network. The board is trying it."
			case code == http.StatusConflict || code == http.StatusNotFound:
				msg = m
			default:
				writeErr(w, code, m)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "message": msg})
		return
	}
	if !b.Forget {
		if err := server.CheckWifi(b.SSID, b.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if a.SendWifi == nil {
		writeErr(w, http.StatusServiceUnavailable, "Deck Link is off, so boards can't be given Wi-Fi here")
		return
	}
	code, msg := a.SendWifi(d.Name, b.SSID, b.Password, b.Forget)
	if code >= 300 {
		writeErr(w, code, msg)
		return
	}
	_ = a.Devices.SetWifiOwn(d.Name, true)
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "message": msg})
}

// deviceUpdate: {"version"} ("" is the latest). A board on USB is flashed
// through its registered port; a board on Wi-Fi is offered the update over
// the air when this build can (OTA).
func (a *Admin) deviceUpdate(w http.ResponseWriter, r *http.Request, d devices.Device) {
	var b struct {
		Version string `json:"version"`
	}
	if err := readBody(r, 1024, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Kind != devices.Serial || d.Type == "" {
		writeErr(w, http.StatusBadRequest, d.Name+" isn't a board with our firmware")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	cat, err := a.loadCatalog(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't read the firmware list: "+err.Error())
		return
	}
	board, ok := cat.BoardForType(d.Type)
	if !ok {
		writeErr(w, http.StatusNotFound, "the latest release has no firmware for "+d.Type)
		return
	}
	var usb, wifi, onBLE bool
	var usbPath string
	for _, l := range a.linksNow() {
		if l.Board != d.Name {
			continue
		}
		switch l.Transport {
		case link.OverSerial:
			usb, usbPath = true, l.Where
		case link.OverWifi:
			wifi = true
		case link.OverBLE:
			onBLE = true
		}
	}
	switch {
	case usb:
		info, ok := a.portByPath(usbPath)
		if !ok {
			writeErr(w, http.StatusConflict, "the board's port isn't there any more")
			return
		}
		job, code, err := a.startFlash(info, board, b.Version, d.Name, "board-update")
		if err != nil {
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	case wifi:
		if a.OTA == nil {
			writeErr(w, http.StatusNotImplemented, ErrNoOTA.Error())
			return
		}
		if board.OTA == nil {
			writeErr(w, http.StatusConflict, board.Name+" has no over-the-air image in this release: plug it in to update it")
			return
		}
		version := b.Version
		if version == "" {
			version = board.Version
		}
		if j, ok := a.jobs.running(func(j Job) bool { return j.Device == d.Name }); ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "the board is already updating", "job": j.ID})
			return
		}
		job := a.jobs.start(Job{Kind: "board-update", Device: d.Name, Board: board.Board, Version: version,
			Stage: "offered", Message: "Offering " + version + " to the board over Wi-Fi"},
			func(ctx context.Context, up func(func(*Job))) error {
				return a.runOTA(ctx, up, d.Name, board.Board, version)
			})
		writeJSON(w, http.StatusAccepted, job)
	case onBLE:
		writeErr(w, http.StatusConflict, link.ErrOnBluetooth.Error())
	default:
		writeErr(w, http.StatusConflict, "the board isn't connected: plug it in to update it")
	}
}
