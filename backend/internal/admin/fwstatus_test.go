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
