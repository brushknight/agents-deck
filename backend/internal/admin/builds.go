package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// Firmware built on this computer (firmware.Builds, the firmware-builds
// folder in the data folder): a second catalog next to the releases, which
// installs with one click on a board that runs an older version, also in a
// copy without a release key. It never comes from the network: `agentctl
// firmware add` puts a build there through POST /admin/v1/firmware/builds,
// which only the management API takes (the unix socket and the loopback
// dashboard behind its login). Every install checks the files again
// (firmware.Builds.Open), and installs go through the same jobs as a
// release: flashJob over USB, runOTA over Wi-Fi.

// buildsMu keeps adding and removing builds one at a time.
var buildsMu sync.Mutex

func (a *Admin) builds() firmware.Builds {
	if a.DataDir == "" {
		return firmware.Builds{}
	}
	return firmware.Builds{Dir: filepath.Join(a.DataDir, firmware.LocalDir)}
}

// localFor is the newest build made here for a board whose firmware says
// Hello as helloType (board: that board in the release, when it is there).
func localFor(cat firmware.Catalog, helloType string, board *firmware.Board) *firmware.Board {
	if board != nil {
		if b, ok := cat.Board(board.Board); ok {
			return &b
		}
	}
	if helloType == "" {
		return nil
	}
	if b, ok := cat.BoardForType(helloType); ok {
		return &b
	}
	if b, ok := cat.Board(link.BoardOfType(helloType)); ok {
		return &b
	}
	return nil
}

// deviceRow is one board's row now.
func (a *Admin) deviceRow(id string) (DeviceRow, bool) {
	for _, row := range a.deviceRows() {
		if row.ID == id {
			return row, true
		}
	}
	return DeviceRow{}, false
}

// BuildList is GET /admin/v1/firmware/builds.
type BuildList struct {
	Dir     string                `json:"dir"`
	Builds  []firmware.LocalBuild `json:"builds"`
	Skipped []string              `json:"skipped,omitempty"`
}

func (a *Admin) listBuilds(w http.ResponseWriter, r *http.Request) {
	b := a.builds()
	if b.Dir == "" {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no firmware builds")
		return
	}
	list, skipped := b.List()
	if list == nil {
		list = []firmware.LocalBuild{}
	}
	writeJSON(w, http.StatusOK, BuildList{Dir: b.Dir, Builds: list, Skipped: skipped})
}

// addBuild: POST /admin/v1/firmware/builds, multipart/form-data with the
// parts manifest (the board's .manifest.json), full (the .bin) and, for a
// board with Wi-Fi, app (the .app.bin).
func (a *Admin) addBuild(w http.ResponseWriter, r *http.Request) {
	b := a.builds()
	if b.Dir == "" {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no firmware builds")
		return
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		writeErr(w, http.StatusBadRequest, "send the build as multipart/form-data: manifest, full and app")
		return
	}
	if r.ContentLength > maxBuildUpload {
		writeErr(w, http.StatusRequestEntityTooLarge, firmware.ErrTooBig.Error())
		return
	}
	mr := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxBuildUpload), params["boundary"])
	if err := os.MkdirAll(b.Dir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var manifest []byte
	files := map[string]string{}
	defer func() {
		for _, p := range files {
			os.Remove(p) // what Add didn't move into the folder
		}
	}()
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, firmware.ErrTooBig.Error())
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "couldn't read the upload: "+err.Error())
			return
		}
		name := part.FormName()
		switch name {
		case "manifest":
			if manifest != nil {
				writeErr(w, http.StatusBadRequest, "one manifest per build")
				return
			}
			manifest, err = io.ReadAll(io.LimitReader(part, 1<<20+1))
			if err == nil && len(manifest) > 1<<20 {
				err = errors.New("the manifest is too big")
			}
		case "full", "app":
			if files[name] != "" {
				writeErr(w, http.StatusBadRequest, "one "+name+" image per build")
				return
			}
			files[name], err = receivePart(b.Dir, part)
		default:
			err = fmt.Errorf("unknown part %q: manifest, full and app", name)
		}
		part.Close()
		if errors.As(err, &tooBig) || errors.Is(err, firmware.ErrTooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, firmware.ErrTooBig.Error())
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if manifest == nil || files["full"] == "" {
		writeErr(w, http.StatusBadRequest, "a build needs its manifest and its full image (.bin)")
		return
	}
	buildsMu.Lock()
	lb, err := b.Add(manifest, files["full"], files["app"])
	buildsMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Store != nil {
		a.Store.Touch()
	}
	writeJSON(w, http.StatusOK, lb)
}

// maxBuildUpload: two images and a manifest.
const maxBuildUpload = 2*firmware.MaxFileImage + 2<<20

// receivePart writes an uploaded image to a temporary file in dir.
func receivePart(dir string, part io.Reader) (string, error) {
	f, err := os.CreateTemp(dir, ".incoming-*.bin")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(part, firmware.MaxFileImage+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > firmware.MaxFileImage {
		err = firmware.ErrTooBig
	}
	if err == nil && n == 0 {
		err = errors.New("an image is empty")
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// removeBuild: DELETE /admin/v1/firmware/builds/{board}/{version}; version
// "all" removes every build of the board.
func (a *Admin) removeBuild(w http.ResponseWriter, r *http.Request) {
	b := a.builds()
	if b.Dir == "" {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no firmware builds")
		return
	}
	version := r.PathValue("version")
	if version == "all" {
		version = ""
	}
	buildsMu.Lock()
	removed, err := b.Remove(r.PathValue("board"), version)
	buildsMu.Unlock()
	if err != nil {
		code := http.StatusNotFound
		if len(removed) > 0 {
			code = http.StatusInternalServerError
		}
		writeErr(w, code, err.Error())
		return
	}
	if a.Store != nil {
		a.Store.Touch()
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// openLocal finds and checks a build made here for board (version "" the
// newest), answering an HTTP status with the error.
func (a *Admin) openLocal(board, version string) (firmware.Verified, int, error) {
	b := a.builds()
	if version == "" {
		lb, ok := b.Latest().Board(board)
		if !ok {
			return firmware.Verified{}, http.StatusNotFound, fmt.Errorf("no build of %s on this computer: agentctl firmware add <folder> puts one here", board)
		}
		version = lb.Version
	}
	v, err := b.Open(board, version)
	if err != nil {
		return v, http.StatusConflict, err
	}
	return v, 0, nil
}

// usedLocal is the job's first step in words.
func usedLocal(v firmware.Verified, file string, size int64) []string {
	return []string{fmt.Sprintf("Using %s, built on this computer (%s, checked against its manifest; not a release)", file, megabytes(size))}
}

// deviceInstallLocal installs the build made here on a board of yours:
// over its USB cable when it is plugged in (the full image at 0x0), else
// over Wi-Fi (the app image), as the board's status said.
func (a *Admin) deviceInstallLocal(w http.ResponseWriter, d devices.Device, version string) {
	row, ok := a.deviceRow(d.Name)
	if !ok || row.State != "active" {
		writeErr(w, http.StatusConflict, "approve the board first")
		return
	}
	// The same match as the board's line (deviceRows): its board in the
	// release, else the Hello types the build's manifest lists.
	board := ""
	if lb := localFor(a.builds().Latest(), d.Type, &firmware.Board{Board: row.Board}); lb != nil {
		board = lb.Board
	}
	if board == "" {
		writeErr(w, http.StatusNotFound, "no build on this computer is for "+row.Name+" ("+d.Type+")")
		return
	}
	v, code, err := a.openLocal(board, version)
	if err != nil {
		writeErr(w, code, err.Error())
		return
	}
	if row.Chip != "" && !firmware.SameChip(row.Chip, v.Chip) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("the build of %s on this computer is for an %s, and %s is an %s", board, v.Chip, row.Name, row.Chip))
		return
	}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Device == d.Name || (row.Port != "" && j.Port == row.Port) }); ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": row.Name + " is already updating", "job": j.ID})
		return
	}
	usbPath, wifi, ble := a.boardLinks(d.Name)
	switch {
	case usbPath != "" && a.Flasher != nil:
		if !a.registered(usbPath) {
			writeErr(w, http.StatusConflict, "its USB port isn't registered: register it in Boards first")
			return
		}
		info, ok := a.portByPath(usbPath)
		if !ok {
			writeErr(w, http.StatusConflict, "the board's port isn't there any more")
			return
		}
		job := a.localFlash(info, Job{Kind: "board-update", Device: d.Name}, v)
		writeJSON(w, http.StatusAccepted, job)
	case wifi:
		switch {
		case a.OTA == nil || a.Local == nil:
			writeErr(w, http.StatusConflict, "this copy can't send firmware over Wi-Fi: plug the board in with a USB cable")
			return
		case v.AppPath == "":
			writeErr(w, http.StatusConflict, "this build has no app image for Wi-Fi (.app.bin): plug the board in with a USB cable")
			return
		case v.App.Size > link.MaxAppImage:
			writeErr(w, http.StatusConflict, fmt.Sprintf("this build's app image is %s: an app slot holds %s", megabytes(v.App.Size), megabytes(link.MaxAppImage)))
			return
		}
		if _, err := a.Local.AddLocalImage(v.Board.Board, v.Version, v.AppPath); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		job := a.jobs.start(Job{Kind: "board-update", Device: d.Name, Board: v.Board.Board, Version: v.Version, Source: SourceLocal,
			Stage: "offered", Message: "Offering " + v.Version + ", built on this computer, to the board over Wi-Fi"},
			func(ctx context.Context, upd func(func(*Job))) error {
				upd(func(j *Job) { j.Steps = append(j.Steps, usedLocal(v, v.OTA.File, v.App.Size)...) })
				return a.runOTA(ctx, upd, d.Name, v.Board.Board, v.Version)
			})
		writeJSON(w, http.StatusAccepted, job)
	case ble:
		writeErr(w, http.StatusConflict, link.ErrOnBluetooth.Error())
	default:
		writeErr(w, http.StatusConflict, "the board isn't connected: plug it in with a USB cable to install it")
	}
}

// localFlash writes a checked build's full image over USB as a job.
func (a *Admin) localFlash(port serial.Info, base Job, v firmware.Verified) Job {
	base.Board, base.Version, base.Source = v.Board.Board, v.Version, SourceLocal
	return a.flashJob(port, base, v.Offset, func(ctx context.Context) (string, []string, error) {
		return v.FullPath, usedLocal(v, v.File, v.Full.Size), nil
	})
}

func (a *Admin) registered(path string) bool {
	for _, p := range a.registeredPorts() {
		if p.Path == path {
			return true
		}
	}
	return false
}

// portFlashLocal: POST /admin/v1/ports/{id}/flash {"board", "version",
// "source": "local"}: a build made here, its full image at 0x0. The board's
// chip must be known: from identify (the bootloader answered), or from the
// agents deck firmware on it.
func (a *Admin) portFlashLocal(w http.ResponseWriter, p PortRow, board, version string) {
	switch {
	case !p.Registered:
		writeErr(w, http.StatusForbidden, "register the port first: the service only opens ports you said are yours")
		return
	case a.Flasher == nil:
		writeErr(w, http.StatusNotImplemented, "this service can't flash boards")
		return
	case p.Job != "" || a.held(p.Path):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "the service is writing to this board right now", "job": p.Job})
		return
	case p.State == PortUnplugged:
		writeErr(w, http.StatusConflict, "the board isn't plugged in")
		return
	}
	v, code, err := a.openLocal(board, version)
	if err != nil {
		writeErr(w, code, err.Error())
		return
	}
	if p.Device != "" {
		row, ok := a.deviceRow(p.Device)
		if ok && row.Chip != "" && !firmware.SameChip(row.Chip, v.Chip) {
			writeErr(w, http.StatusConflict, fmt.Sprintf("the build of %s is for an %s, and %s is an %s", board, v.Chip, row.Name, row.Chip))
			return
		}
		if !ok || row.Chip == "" {
			writeErr(w, http.StatusConflict, "agents deck doesn't know this board's chip: look at it again (identify)")
			return
		}
	} else {
		chip, ok := a.chipOn(p)
		if !ok {
			writeErr(w, http.StatusConflict, "look at the board first (identify), so agents deck knows its chip")
			return
		}
		if err := blankFit(v.Full, chip); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
	}
	job := a.localFlash(p.info(), Job{Kind: "flash", Device: p.Device}, v)
	writeJSON(w, http.StatusAccepted, job)
}

// localCandidates: the newest builds made here that fit a chip with this
// much flash.
func (a *Admin) localCandidates(chip string, flashSize int64) []firmware.Board {
	return a.builds().Latest().Candidates(chip, flashSize)
}
