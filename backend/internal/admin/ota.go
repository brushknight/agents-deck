package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// OTASource is the release catalog as the Deck Link hub reads it for updates
// over Wi-Fi: the over-the-air images (firmware.Client) and, from the
// manifest's "types", which Hello types each board's firmware reports.
type OTASource struct{ *firmware.Client }

var (
	_ link.FirmwareSource = OTASource{}
	_ link.BoardTyper     = OTASource{}
)

// BoardTypes: the Hello types of a board's firmware in the latest release.
func (s OTASource) BoardTypes(ctx context.Context, board string) ([]string, error) {
	cat, err := s.Latest(ctx)
	if err != nil {
		return nil, err
	}
	b, ok := cat.Board(board)
	if !ok {
		return nil, fmt.Errorf("release %s has no firmware for %q", cat.Version, board)
	}
	return b.Types, nil
}

// otaWatch passes the hub's job changes to the job that waits on that board.
type otaWatch struct {
	mu   sync.Mutex
	subs map[string]chan link.FirmwareJob // by device name
}

// FirmwareChanged is link.Hub.OnFirmware: a board update over Wi-Fi moved on.
func (a *Admin) FirmwareChanged(j link.FirmwareJob) {
	a.ota.mu.Lock()
	ch := a.ota.subs[j.Device]
	a.ota.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- j:
	default: // a full queue: the next change carries the state
	}
}

func (a *Admin) watchOTA(device string) (chan link.FirmwareJob, func()) {
	ch := make(chan link.FirmwareJob, 16)
	a.ota.mu.Lock()
	if a.ota.subs == nil {
		a.ota.subs = map[string]chan link.FirmwareJob{}
	}
	a.ota.subs[device] = ch
	a.ota.mu.Unlock()
	return ch, func() {
		a.ota.mu.Lock()
		if a.ota.subs[device] == ch {
			delete(a.ota.subs, device)
		}
		a.ota.mu.Unlock()
	}
}

// otaWait is how long a job follows a board after the offer.
var otaWait = 20 * time.Minute

// runOTA offers the board the version and follows it until it is done,
// failed, refused or rolled back.
func (a *Admin) runOTA(ctx context.Context, up func(func(*Job)), device, board, version string) error {
	ch, stop := a.watchOTA(device)
	defer stop()
	if err := a.OTA.OfferFirmware(device, board, version); err != nil {
		return plainOTAError(err)
	}
	up(func(j *Job) {
		j.Percent, j.Message = 3, "Offered "+version+": waiting for the board to start the download"
	})
	timeout := time.NewTimer(otaWait)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("stopped following the board; it may still finish the update by itself")
		case <-timeout.C:
			return errors.New("the board stopped reporting: if it doesn't come back, plug it in with a USB cable and install again")
		case fj := <-ch:
			if fj.Version != "" && fj.Version != version {
				continue // an older offer's news
			}
			pct, msg := otaProgress(fj)
			up(func(j *Job) { j.Stage, j.Percent, j.Message = fj.State, pct, msg })
			switch fj.State {
			case link.FirmwareDone:
				up(func(j *Job) { j.Steps = append(j.Steps, "The board runs "+version) })
				return nil
			case link.FirmwareFailed, link.FirmwareRefused, link.FirmwareRolledBack:
				return errors.New(msg)
			}
		}
	}
}

func otaProgress(j link.FirmwareJob) (int, string) {
	switch j.State {
	case link.FirmwareOffered:
		return 3, "Offered: waiting for the board to start the download"
	case link.FirmwareDownloading:
		return 5 + j.Percent*80/100, "The board is downloading the firmware"
	case link.FirmwareVerifying:
		return 88, "The board is checking the firmware"
	case link.FirmwareRestarting:
		return 94, "The board restarts into the new firmware"
	case link.FirmwareDone:
		return 100, "Done. The board runs " + j.Version + "."
	case link.FirmwareRefused:
		return j.Percent, "The board didn't take the update" + reason(j.Error)
	case link.FirmwareRolledBack:
		return j.Percent, "The new firmware didn't reach the deck, so the board went back to " + orWord(j.From, "its old firmware") + reason(j.Error)
	}
	return j.Percent, "The update didn't work; the board kept its firmware" + reason(j.Error)
}

func reason(s string) string {
	if s == "" {
		return "."
	}
	return ": " + s
}

func orWord(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func plainOTAError(err error) error {
	switch {
	case errors.Is(err, link.ErrNoFirmwareSource):
		return ErrNoOTA
	case errors.Is(err, link.ErrNotOnWifi):
		return errors.New("the board isn't on Wi-Fi right now: update it over the USB cable")
	case errors.Is(err, link.ErrNotConnected):
		return errors.New("the board isn't connected: plug it in or bring it back on Wi-Fi")
	case errors.Is(err, link.ErrUpdating):
		return errors.New("the board is already updating")
	}
	return err
}
