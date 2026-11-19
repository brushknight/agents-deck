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
	FwLocal    = "local"    // a newer build made on this computer (firmware-builds)
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
	// Source: where Latest comes from, release (signed, from GitHub) or
	// local (built on this computer); for update and local.
	Source string `json:"source,omitempty"`
}

// Where an update comes from.
const (
	SourceRelease = "release"
	SourceLocal   = "local"
)

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
	// Local is the newest build of this board made on this computer (nil:
	// none); LocalOTA that this service can send one over Wi-Fi.
	Local    *firmware.Board
	LocalOTA bool
}

// firmwareStatus decides a board's one line about updates: the latest
// release's, or a build made on this computer when that one is newer than
// what the board runs and than a release this build can install.
func firmwareStatus(in fwInput) FirmwareStatus {
	rel := releaseStatus(in)
	if in.Local == nil || in.Running == "" {
		return rel
	}
	lv, err := update.ParseVersion(in.Local.Version)
	if err != nil {
		return rel
	}
	run, runErr := update.ParseVersion(in.Running)
	if runErr == nil && lv.Compare(run) <= 0 {
		return rel // it runs this build, or a newer one
	}
	if rel.State == FwUpdate {
		if v, err := update.ParseVersion(rel.Latest); err == nil && v.Compare(lv) >= 0 {
			return rel // the release is as new or newer, and signed
		}
	}
	return localStatus(in, rel)
}

// localStatus: install the build made on this computer.
func localStatus(in fwInput, rel FirmwareStatus) FirmwareStatus {
	b := in.Local
	s := FirmwareStatus{State: FwLocal, Source: SourceLocal, Latest: b.Version, Text: b.Version + " built on this computer"}
	if _, err := update.ParseVersion(in.Running); err != nil {
		s.Text = "Runs a build of its own (" + in.Running + "); " + b.Version + " built on this computer can go on it"
	} else if rel.State == FwNoKey {
		s.Text += ". Release " + rel.Latest + " is newer, and this copy of agents deck can't install releases"
	}
	switch in.Connection {
	case "usb":
		s.How, s.Ready, s.Note = "usb", in.Registered, "over USB"
		if !in.Registered {
			s.Note = "its USB port isn't registered"
		}
	case "wifi":
		s.How = "wifi"
		switch {
		case !in.LocalOTA:
			s.Note = "plug it in with a USB cable to install it: this copy can't send firmware over Wi-Fi"
		case b.OTA == nil:
			s.Note = "plug it in with a USB cable to install it: this build has no app image for Wi-Fi (.app.bin)"
		default:
			s.Ready, s.Note = true, "over Wi-Fi"
		}
	case "bluetooth":
		s.How, s.Note = "bluetooth", link.ErrOnBluetooth.Error()
	default:
		s.How, s.Note = "unplugged", "plug it in with a USB cable to install it"
		if in.LocalOTA && b.OTA != nil && b.Wifi {
			s.Note = "plug it in with a USB cable, or bring it back on Wi-Fi, to install it"
		}
	}
	return s
}

// releaseStatus decides the line from the latest release alone.
func releaseStatus(in fwInput) FirmwareStatus {
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
		return FirmwareStatus{State: FwNoKey, Latest: latest, Text: "Release " + latest + " is out; this copy installs only firmware built on this computer (no release key)"}
	}
	s := FirmwareStatus{State: FwUpdate, Source: SourceRelease, Latest: latest, Text: "Update to " + latest + " available"}
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
