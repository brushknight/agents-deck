package admin

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/flash"
)

// Installing a firmware file picked on this computer on a board with no
// agents deck firmware yet, over its USB cable: the counterpart of the
// device calls in filefw.go for the "Set up a new board" flow. Two calls
// again: POST /admin/v1/ports/{id}/firmware-file reads the file and refuses
// it right there, then POST /admin/v1/ports/{id}/install-file writes it.
//
// What a blank board needs decides what is taken: only a full image
// (bootloader, partition table and app, merged at 0x0, the kind the releases
// ship), written at 0x0. An app image alone is refused: it only runs where a
// bootloader and a partition table already are, and nothing here can tell
// whether this chip has them or where its app slot is (the ROM bootloader
// answers the chip and its flash, not what is in it). The file is checked
// against the chip the bootloader answered when the person looked at the
// board (identify): its chip, its bootloader offset, its flash size.
//
// Like the device calls, only the management API takes these, so only this
// computer does, and the file isn't checked against a release signature: it
// is the owner's explicit choice. Firmware from the network still never
// installs without the release key (startFlash).

// probeTTL is how long what the bootloader answered stands for a file
// picked for the board on that port.
const probeTTL = fileTTL

// probed is what the ROM bootloader on a port answered, and which USB
// device answered it.
type probed struct {
	chip                flash.Chip
	vid, pid, serialNum string
	at                  time.Time
}

type probeBook struct {
	mu   sync.Mutex
	byID map[string]probed // port path
}

func (b *probeBook) put(path string, p probed) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byID == nil {
		b.byID = map[string]probed{}
	}
	b.byID[path] = p
}

func (b *probeBook) forget(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byID, path)
}

// chipOn is the chip the port's bootloader answered lately, while the same
// USB device is on the port.
func (a *Admin) chipOn(p PortRow) (flash.Chip, bool) {
	a.probes.mu.Lock()
	pr, ok := a.probes.byID[p.Path]
	a.probes.mu.Unlock()
	if !ok || time.Since(pr.at) > probeTTL || pr.vid != p.VID || pr.pid != p.PID || pr.serialNum != p.Serial {
		return flash.Chip{}, false
	}
	return pr.chip, true
}

// blankPort: the port both calls are about, with a board on it that has no
// agents deck firmware yet and whose chip is known.
func (a *Admin) blankPort(w http.ResponseWriter, r *http.Request) (PortRow, flash.Chip, bool) {
	p, ok := a.portByID(r.PathValue("id"))
	switch {
	case !ok:
		writeErr(w, http.StatusNotFound, "no such port: is the board still plugged in?")
	case !p.Registered:
		writeErr(w, http.StatusForbidden, "register the port first: the service only opens ports you said are yours")
	case a.Flasher == nil:
		writeErr(w, http.StatusNotImplemented, "this service can't flash boards")
	case p.Job != "" || a.held(p.Path):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "the service is writing to this board right now", "job": p.Job})
	case p.Device != "":
		writeErr(w, http.StatusConflict, fmt.Sprintf("this board already runs agents deck, as %s: install the file from its row in Boards", p.Name))
	case p.State == PortUnplugged:
		writeErr(w, http.StatusConflict, "the board isn't plugged in")
	default:
		chip, ok := a.chipOn(p)
		if !ok {
			writeErr(w, http.StatusConflict, "look at the board first (identify), so agents deck knows its chip")
			return p, chip, false
		}
		return p, chip, true
	}
	return p, flash.Chip{}, false
}

// portFirmwareFile: POST /admin/v1/ports/{id}/firmware-file. The body is
// the file itself; X-File-Name names it.
func (a *Admin) portFirmwareFile(w http.ResponseWriter, r *http.Request) {
	p, chip, ok := a.blankPort(w, r)
	if !ok {
		return
	}
	up, info, ok := a.receiveFile(w, r)
	if !ok {
		return
	}
	defer os.Remove(up.path) // the upload, unless it was kept under its id
	if err := blankFit(info, chip); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	up.Port, up.Via, up.ChipChecked = p.ID, "usb", true
	a.keepFile(w, up)
}

// blankFit: whether a file brings up a board with nothing of ours on it,
// written at 0x0 over USB, and why not in words.
func blankFit(info firmware.ImageInfo, chip flash.Chip) error {
	if !firmware.SameChip(info.Chip, chip.Name) {
		return fmt.Errorf("this file is for an %s, and the board on this port is an %s: pick the firmware built for this board", info.Chip, chip.Name)
	}
	if info.Kind == firmware.ImageApp {
		return fmt.Errorf("this is an app image (the firmware alone), which only starts on a board that already has a bootloader and a partition table. " +
			"Pick the full image instead: the release's agents-deck-<board>-<version>.bin, or a merged image written at 0x0")
	}
	if info.Kind != firmware.ImageFull {
		return fmt.Errorf("an image of a kind agents deck doesn't install")
	}
	if chip.FlashSize > 0 {
		if info.Size > chip.FlashSize {
			return fmt.Errorf("this image is %s and the board's flash holds %s: pick the firmware built for this board", megabytes(info.Size), megabytes(chip.FlashSize))
		}
		if info.FlashSize > chip.FlashSize {
			return fmt.Errorf("this image was built for %d MB of flash and the board has %d MB, so it wouldn't start: pick the firmware built for this board", info.FlashSize>>20, chip.FlashSize>>20)
		}
	}
	return nil
}

// portInstallFile: POST /admin/v1/ports/{id}/install-file {"file"}: writes
// the full image at 0x0 and hands the port back, so the board says hello
// and pairs.
func (a *Admin) portInstallFile(w http.ResponseWriter, r *http.Request) {
	var b struct {
		File string `json:"file"`
	}
	if err := readBody(r, 1024, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, chip, ok := a.blankPort(w, r)
	if !ok {
		return
	}
	up, ok := a.upload(b.File, func(up FileUpload) bool { return up.Port == p.ID && up.Device == "" })
	if !ok {
		writeErr(w, http.StatusNotFound, "that file isn't here any more: pick it again")
		return
	}
	if !firmware.SameChip(up.Chip, chip.Name) || up.Kind != firmware.ImageFull {
		writeErr(w, http.StatusConflict, "the board on this port changed since the file was picked: look at it again")
		return
	}
	job := a.flashJob(p.info(), Job{Kind: "flash", Version: up.Version, File: up.Name, Chip: chip.Name},
		0, func(ctx context.Context) (string, []string, error) {
			return up.path, []string{fmt.Sprintf("Using %s (%s, not checked against a release)", up.Name, megabytes(up.Size))}, nil
		})
	writeJSON(w, http.StatusAccepted, job)
}
