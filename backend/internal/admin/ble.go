package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/ble"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// Bluetooth in the management API: whether this computer can use it, scanning
// for boards nearby, adding one (the computer pairs with it while the person
// types the code shown on the board), and each board's uplink preference.

// UplinkRow is one way a board reaches this computer right now.
type UplinkRow struct {
	Via    string `json:"via"`              // usb | wifi | bluetooth
	State  string `json:"state"`            // in use | standing by | connecting | not used | off
	Detail string `json:"detail,omitempty"` // the port, the address, or why it stands by
}

// BluetoothBoard is a board seen in the scan.
type BluetoothBoard struct {
	ID       string `json:"id"`   // this computer's identifier for it: the {id} to add
	Name     string `json:"name"` // what it advertises: deck-<last 6 of its board id>
	RSSI     int    `json:"rssi,omitempty"`
	Board    string `json:"board,omitempty"` // the catalog board
	Model    string `json:"model,omitempty"` // that board's name
	BoardID  string `json:"boardId,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Setup    bool   `json:"setup"`            // ready to be added (not bonded to a computer)
	Device   string `json:"device,omitempty"` // already one of your devices: its id
	Added    bool   `json:"added"`            // and added over Bluetooth on this computer
}

func (a *Admin) btStatus() ble.Status {
	if a.Bluetooth == nil {
		return ble.Status{Helper: "none", Why: ble.NotHere}
	}
	return a.Bluetooth.Status()
}

// bluetoothStatus: GET /admin/v1/bluetooth.
func (a *Admin) bluetoothStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.btStatus())
}

// bluetoothScan: POST /admin/v1/bluetooth/scan {"on": true|false} (on by
// default).
func (a *Admin) bluetoothScan(w http.ResponseWriter, r *http.Request) {
	b := struct {
		On *bool `json:"on"`
	}{}
	if err := readBody(r, 1024, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	on := b.On == nil || *b.On
	if a.Bluetooth == nil {
		if !on {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, ble.NotHere)
		return
	}
	if err := a.Bluetooth.Scan(on); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bluetoothBoards: GET /admin/v1/bluetooth/boards, the boards seen since the
// scan started.
func (a *Admin) bluetoothBoards(w http.ResponseWriter, r *http.Request) {
	st := a.btStatus()
	rows := []BluetoothBoard{}
	if a.Bluetooth != nil {
		cat, _ := a.cachedCatalog()
		for _, n := range a.Bluetooth.Nearby() {
			row := BluetoothBoard{ID: n.ID, Name: n.Name, RSSI: n.RSSI, Board: n.Board, BoardID: n.BoardID, Firmware: n.Firmware, Setup: n.Setup}
			if cat != nil {
				for _, b := range cat.Boards {
					if b.Board == n.Board {
						row.Model = b.Name
					}
				}
			}
			if a.Devices != nil {
				if d, ok := a.Devices.ByBLE(n.ID); ok {
					row.Device, row.Added = d.Name, true
				} else if d, ok := a.Devices.ByBoardID(n.BoardID); ok {
					row.Device = d.Name
				}
			}
			rows = append(rows, row)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": rows, "scanning": st.Scanning, "available": st.Available, "why": st.Why})
}

// bluetoothAdd: POST /admin/v1/bluetooth/boards/{id}/add {"scope": "view" |
// "act"}. A job: connect, passkey (type the code shown on the board into the
// window the computer opens), connected, done; or failed with the reason.
func (a *Admin) bluetoothAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Scope string `json:"scope"`
	}
	if err := readBody(r, 1024, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Bluetooth == nil || a.Devices == nil {
		writeErr(w, http.StatusServiceUnavailable, ble.NotHere)
		return
	}
	scope := a.Devices.Settings().AutoApprove.Scope
	if b.Scope != "" {
		sc, err := devices.ParseScope(b.Scope)
		if err != nil || sc == devices.Admin {
			writeErr(w, http.StatusBadRequest, "a board added over Bluetooth gets view or act; admin is given by hand")
			return
		}
		scope = sc
	}
	id := r.PathValue("id")
	if st := a.btStatus(); !st.Available {
		writeErr(w, http.StatusServiceUnavailable, st.Why)
		return
	}
	n, ok := a.Bluetooth.Seen(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no such board nearby: scan first")
		return
	}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Kind == "bluetooth-add" && j.Port == id }); ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "that board is being added already", "job": j.ID})
		return
	}
	job := a.jobs.start(Job{Kind: "bluetooth-add", Port: id, Board: n.Board, Stage: "connect",
		Message: "Connecting to " + orWord(n.Name, "the board")},
		func(ctx context.Context, up func(func(*Job))) error {
			steps := map[string]int{"connect": 10, "passkey": 30, "connected": 80, "done": 100}
			d, err := a.Bluetooth.Add(ctx, id, scope, func(stage, msg string) {
				up(func(j *Job) {
					j.Stage, j.Message, j.Percent = stage, msg, steps[stage]
					if stage == "connected" {
						j.Steps = append(j.Steps, "Paired")
					}
				})
			})
			if err != nil {
				up(func(j *Job) { j.Message = "Not added: " + err.Error() })
				return err
			}
			up(func(j *Job) { j.Device = d.Name })
			if a.Store != nil {
				a.Store.Touch()
			}
			return nil
		})
	writeJSON(w, http.StatusAccepted, job)
}

// deviceUplink: {"uplink": "auto" | "wifi" | "ble"}.
func (a *Admin) deviceUplink(r *http.Request, d devices.Device) error {
	var b struct {
		Uplink string `json:"uplink"`
	}
	if err := readBody(r, 1024, &b); err != nil {
		return err
	}
	pref := strings.TrimSpace(b.Uplink)
	if pref == "" || !link.ValidUplink(pref) {
		return errors.New("the uplink is auto, wifi or ble")
	}
	if pref == link.UplinkBLE && d.BLE == "" {
		return errors.New(d.Name + " isn't added over Bluetooth on this computer: add it first (agentctl bluetooth add)")
	}
	return a.Devices.SetUplink(d.Name, pref)
}

// uplinkRows fills a board's uplinks: in use, standing by, connecting.
func (a *Admin) uplinkRows(row *DeviceRow, d devices.Device, links []link.LinkInfo) {
	row.UplinkPref = d.Uplink
	if row.UplinkPref == "" {
		row.UplinkPref = link.UplinkAuto
	}
	row.Bluetooth = d.BLE != ""
	row.HasBluetooth = row.Bluetooth || (row.BLEState != "" && row.BLEState != "not supported")
	have := map[string]bool{}
	for _, l := range links {
		via := map[string]string{link.OverSerial: "usb", link.OverWifi: "wifi", link.OverBLE: "bluetooth"}[l.Transport]
		if via == "" || have[via] {
			continue
		}
		have[via] = true
		row.Uplinks = append(row.Uplinks, UplinkRow{Via: via, State: "in use", Detail: l.Where})
	}
	if d.BLE != "" && !have["bluetooth"] && a.Bluetooth != nil {
		ls := a.Bluetooth.State(d)
		r := UplinkRow{Via: "bluetooth", State: ls.State}
		switch ls.State {
		case "standing by":
			r.Detail = "it uses " + map[string]string{link.OverSerial: "USB", link.OverWifi: "Wi-Fi"}[ls.Uses]
		case "connecting":
			r.Detail = "waiting for it to come in range"
		case "not used":
			r.Detail = "its uplink preference is " + row.UplinkPref
		case "off":
			r.Detail = a.btStatus().Why
		}
		if ls.Problem != "" && r.State != "standing by" && r.State != "not used" {
			r.Detail = ls.Problem
		}
		if r.State != "" && r.State != "connected" {
			row.Uplinks = append(row.Uplinks, r)
		}
	}
	// Wi-Fi, for a board paired for it but not on it now.
	if d.CertSerial != "" && !have["wifi"] {
		r := UplinkRow{Via: "wifi", State: "not connected", Detail: "it isn't on the network now"}
		switch {
		case !link.UplinkAllowed(d, link.OverWifi):
			r.State, r.Detail = "not used", "its uplink preference is "+row.UplinkPref
		case have["usb"]:
			r.State, r.Detail = "standing by", "it uses USB"
		}
		row.Uplinks = append(row.Uplinks, r)
	}
}
