package admin

import (
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

func TestFirmwareStatus(t *testing.T) {
	ota := &firmware.OTA{File: "x.app.bin"}
	board := &firmware.Board{Board: "waveshare-touch-lcd-4", Name: "Waveshare 4", Version: "0.1.1", Wifi: true, OTA: ota}
	noOTA := &firmware.Board{Board: "waveshare-touch-lcd-4", Version: "0.1.1", Wifi: true}
	cat := &firmware.Catalog{Version: "0.1.1", Boards: []firmware.Board{*board}}
	for name, tc := range map[string]struct {
		in                fwInput
		state, how, words string
		ready             bool
	}{
		// The owner's case: a development build, newer than any release, on a build without a key.
		"dev build":         {in: fwInput{Running: "0.2.0-dev.4", Catalog: cat, Board: board, Connection: "wifi", OTA: true}, state: FwNewer, words: "Newer than the latest release (0.1.1)"},
		"dev build, key":    {in: fwInput{Running: "0.2.0-dev.4", Catalog: cat, Board: board, ReleaseKey: true, Connection: "wifi", OTA: true}, state: FwNewer},
		"pre-release of it": {in: fwInput{Running: "0.1.1-rc.1", Catalog: cat, Board: board, ReleaseKey: true, Connection: "wifi", OTA: true}, state: FwUpdate, how: "wifi", ready: true},
		"current":           {in: fwInput{Running: "0.1.1", Catalog: cat, Board: board, ReleaseKey: true}, state: FwCurrent, words: "Up to date"},
		"current, v":        {in: fwInput{Running: "v0.1.1", Catalog: cat, Board: board}, state: FwCurrent},
		"own build":         {in: fwInput{Running: "dev", Catalog: cat, Board: board}, state: FwOwn, words: "build of its own (dev)"},
		"old, no key":       {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, Connection: "wifi", OTA: true}, state: FwNoKey, words: "no release key"},
		"old, wifi":         {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, Connection: "wifi", OTA: true}, state: FwUpdate, how: "wifi", ready: true, words: "Update to 0.1.1 available"},
		"old, wifi no OTA":  {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, Connection: "wifi"}, state: FwUpdate, how: "wifi"},
		"old, no image":     {in: fwInput{Running: "0.1.0", Catalog: cat, Board: noOTA, ReleaseKey: true, Connection: "wifi", OTA: true}, state: FwUpdate, how: "wifi"},
		"old, usb":          {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, Connection: "usb", Registered: true}, state: FwUpdate, how: "usb", ready: true},
		"old, usb unreg":    {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, Connection: "usb"}, state: FwUpdate, how: "usb"},
		"old, bluetooth":    {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, Connection: "bluetooth", OTA: true}, state: FwUpdate, how: "bluetooth"},
		"old, unplugged":    {in: fwInput{Running: "0.1.0", Catalog: cat, Board: board, ReleaseKey: true, OTA: true}, state: FwUpdate, how: "unplugged"},
		"no catalog":        {in: fwInput{Running: "0.1.0", CatalogErr: "no such host"}, state: FwError, words: "Couldn't check for updates: no such host"},
		"first read":        {in: fwInput{Running: "0.1.0"}, state: FwChecking},
		"not in release":    {in: fwInput{Running: "0.1.0", Catalog: cat}, state: FwUnlisted, words: "has no firmware for this board"},
		"no version yet":    {in: fwInput{Catalog: cat, Board: board}, state: FwUnknown},
	} {
		got := firmwareStatus(tc.in)
		if got.State != tc.state || got.How != tc.how || got.Ready != tc.ready || !strings.Contains(got.Text, tc.words) {
			t.Errorf("%s: %+v, want %s how %q ready %v %q", name, got, tc.state, tc.how, tc.ready, tc.words)
		}
		if got.State == FwUpdate && !got.Ready && got.Note == "" {
			t.Errorf("%s: not ready and no reason", name)
		}
	}
}

// The version comparison the status relies on: semver precedence with
// pre-release tags.
func TestVersionOrderForBoards(t *testing.T) {
	order := []string{"0.1.0", "0.1.1-rc.1", "0.1.1", "0.2.0-dev.2", "0.2.0-dev.4", "0.2.0-dev.10", "0.2.0-rc.1", "0.2.0", "0.10.0"}
	for i := range order {
		for j := range order {
			a, _ := update.ParseVersion(order[i])
			b, _ := update.ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Compare(b); got != want {
				t.Errorf("%s vs %s: %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

// A build made on this computer is offered when it is newer than what the
// board runs and than a release this copy can install.
func TestFirmwareStatusLocal(t *testing.T) {
	ota := &firmware.OTA{File: "x.app.bin"}
	board := &firmware.Board{Board: "waveshare-touch-lcd-4", Version: "0.1.1", Wifi: true, OTA: ota}
	cat := &firmware.Catalog{Version: "0.1.1", Boards: []firmware.Board{*board}}
	dev7 := &firmware.Board{Board: "waveshare-touch-lcd-4", Version: "0.2.0-dev.7", Wifi: true, OTA: ota}
	noApp := &firmware.Board{Board: "waveshare-touch-lcd-4", Version: "0.2.0-dev.7", Wifi: true}
	old := &firmware.Board{Board: "waveshare-touch-lcd-4", Version: "0.1.0", OTA: ota}
	for name, tc := range map[string]struct {
		in                        fwInput
		state, how, latest, words string
		ready                     bool
	}{
		"no key, usb":            {in: fwInput{Running: "0.2.0-dev.4", Catalog: cat, Board: board, Local: dev7, Connection: "usb", Registered: true}, state: FwLocal, how: "usb", latest: "0.2.0-dev.7", ready: true, words: "0.2.0-dev.7 built on this computer"},
		"no key, wifi":           {in: fwInput{Running: "0.2.0-dev.4", Catalog: cat, Board: board, Local: dev7, Connection: "wifi", LocalOTA: true}, state: FwLocal, how: "wifi", latest: "0.2.0-dev.7", ready: true},
		"wifi, no app image":     {in: fwInput{Running: "0.2.0-dev.4", Catalog: cat, Board: board, Local: noApp, Connection: "wifi", LocalOTA: true}, state: FwLocal, how: "wifi", latest: "0.2.0-dev.7", words: "built on this computer"},
		"wifi, can't send":       {in: fwInput{Running: "0.2.0-dev.4", Local: dev7, Connection: "wifi"}, state: FwLocal, how: "wifi", latest: "0.2.0-dev.7"},
		"dev.10 runs, dev.7":     {in: fwInput{Running: "0.2.0-dev.10", Catalog: cat, Board: board, Local: dev7, Connection: "usb", Registered: true}, state: FwNewer, latest: "0.1.1"},
		"runs it":                {in: fwInput{Running: "0.2.0-dev.7", Catalog: cat, Board: board, Local: dev7, Connection: "usb", Registered: true}, state: FwNewer, latest: "0.1.1"},
		"release newer, key":     {in: fwInput{Running: "0.0.9", Catalog: cat, Board: board, ReleaseKey: true, Local: old, Connection: "usb", Registered: true}, state: FwUpdate, how: "usb", latest: "0.1.1", ready: true},
		"release newer, no key":  {in: fwInput{Running: "0.0.9", Catalog: cat, Board: board, Local: old, Connection: "usb", Registered: true}, state: FwLocal, how: "usb", latest: "0.1.0", ready: true, words: "Release 0.1.1 is newer"},
		"offline":                {in: fwInput{Running: "0.1.1", CatalogErr: "no such host", Local: dev7, Connection: "usb", Registered: true}, state: FwLocal, how: "usb", latest: "0.2.0-dev.7", ready: true},
		"not in the release":     {in: fwInput{Running: "0.1.1", Catalog: &firmware.Catalog{Version: "0.1.1"}, Local: dev7}, state: FwLocal, how: "unplugged", latest: "0.2.0-dev.7"},
		"own build":              {in: fwInput{Running: "dev", Catalog: cat, Board: board, Local: dev7, Connection: "usb", Registered: true}, state: FwLocal, how: "usb", latest: "0.2.0-dev.7", ready: true, words: "build of its own (dev)"},
		"newer than the release": {in: fwInput{Running: "0.3.0", Catalog: cat, Board: board, ReleaseKey: true, Local: dev7}, state: FwNewer, latest: "0.1.1"},
		"usb unregistered":       {in: fwInput{Running: "0.1.1", Catalog: cat, Board: board, Local: dev7, Connection: "usb"}, state: FwLocal, how: "usb", latest: "0.2.0-dev.7"},
	} {
		got := firmwareStatus(tc.in)
		if got.State != tc.state || got.How != tc.how || got.Ready != tc.ready || got.Latest != tc.latest || !strings.Contains(got.Text, tc.words) {
			t.Errorf("%s: %+v, want %s how %q ready %v latest %s %q", name, got, tc.state, tc.how, tc.ready, tc.latest, tc.words)
		}
		if got.State == FwLocal && (got.Source != SourceLocal || (!got.Ready && got.Note == "")) {
			t.Errorf("%s: %+v", name, got)
		}
		if strings.Contains(got.Text+got.Note, "—") {
			t.Errorf("%s: an em dash in %q", name, got.Text+got.Note)
		}
	}
}
