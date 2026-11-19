package admin

import (
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// Firmware status states: one per board, the line Settings and agentctl
// show about its updates.
const (
	FwUpdate   = "update"   // a newer release this build can install
	FwCurrent  = "current"  // runs the latest release
	FwNewer    = "newer"    // runs something newer than the latest release (a build of its own)
	FwOwn      = "own"      // runs a version that isn't a release's ("dev")
	FwNoKey    = "nokey"    // a newer release, but this build has no release key
	FwError    = "error"    // the release list couldn't be read
	FwUnlisted = "unlisted" // the latest release has no firmware for this board
	FwUnknown  = "unknown"  // the board hasn't said which firmware it runs
	FwChecking = "checking" // the release list is being read for the first time
)

// FirmwareStatus is a board's firmware against the latest release.
type FirmwareStatus struct {
	State  string `json:"state"`
	Text   string `json:"text"`             // the line in words
	Latest string `json:"latest,omitempty"` // the latest release's firmware for it
	How    string `json:"how,omitempty"`    // update: usb | wifi | bluetooth | unplugged
	Ready  bool   `json:"ready,omitempty"`  // update: it can be installed now
	Note   string `json:"note,omitempty"`   // update: how, or why not now
}

// fwInput is what the status is decided from.
type fwInput struct {
	Running    string            // what the board runs ("" unknown)
	Catalog    *firmware.Catalog // the latest release's, nil when not read
	CatalogErr string            // why it couldn't be read
	Board      *firmware.Board   // this board in the catalog, nil when it isn't there
	ReleaseKey bool              // this build can check (and so install) downloads
	Connection string            // usb | wifi | bluetooth | "" (not connected)
	Registered bool              // on USB: its port is registered
	OTA        bool              // this service can update boards over Wi-Fi
}

// firmwareStatus decides a board's one line about updates.
func firmwareStatus(in fwInput) FirmwareStatus {
	switch {
	case in.Catalog == nil && in.CatalogErr != "":
		return FirmwareStatus{State: FwError, Text: "Couldn't check for updates: " + in.CatalogErr}
	case in.Catalog == nil:
		return FirmwareStatus{State: FwChecking, Text: "Checking for updates…"}
	case in.Board == nil:
		return FirmwareStatus{State: FwUnlisted, Latest: in.Catalog.Version,
			Text: "The latest release (" + in.Catalog.Version + ") has no firmware for this board"}
	case in.Running == "":
		return FirmwareStatus{State: FwUnknown, Latest: in.Board.Version, Text: "It hasn't said which firmware it runs yet"}
	}
	latest := in.Board.Version
	run, err := update.ParseVersion(in.Running)
	lv, lerr := update.ParseVersion(latest)
	switch {
	case err != nil:
		return FirmwareStatus{State: FwOwn, Latest: latest, Text: "Runs a build of its own (" + in.Running + "), not a release; the latest release is " + latest}
	case lerr != nil:
		return FirmwareStatus{State: FwError, Text: "Couldn't check for updates: the release's version " + latest + " isn't one"}
	}
	switch c := run.Compare(lv); {
	case c == 0:
		return FirmwareStatus{State: FwCurrent, Latest: latest, Text: "Up to date"}
	case c > 0:
		return FirmwareStatus{State: FwNewer, Latest: latest, Text: "Newer than the latest release (" + latest + ")"}
	}
	if !in.ReleaseKey {
		return FirmwareStatus{State: FwNoKey, Latest: latest, Text: latest + " is out, but updates are switched off in this build (no release key)"}
	}
	s := FirmwareStatus{State: FwUpdate, Latest: latest, Text: "Update to " + latest + " available"}
	switch in.Connection {
	case "usb":
		s.How, s.Ready, s.Note = "usb", in.Registered, "over USB"
		if !in.Registered {
			s.Note = "its USB port isn't registered"
		}
	case "wifi":
		s.How = "wifi"
		switch {
		case !in.OTA:
			s.Note = "plug it in with a USB cable to update it: this build can't update boards over Wi-Fi"
		case in.Board.OTA == nil:
			s.Note = "plug it in with a USB cable to update it: this release has no over-the-air image for it"
		default:
			s.Ready, s.Note = true, "over Wi-Fi"
		}
	case "bluetooth":
		s.How, s.Note = "bluetooth", link.ErrOnBluetooth.Error()
	default:
		s.How, s.Note = "unplugged", "plug it in with a USB cable to update it"
		if in.OTA && in.Board.OTA != nil && in.Board.Wifi {
			s.Note = "plug it in with a USB cable, or bring it back on Wi-Fi, to update it"
		}
	}
	return s
}
