package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// Installing a firmware file picked on this computer (a build of your own),
// the same as `agentctl link update --image` (over Wi-Fi) and `agentctl
// flash <port> <image.bin>` (over USB) do. Two calls: the page uploads the
// file (POST /admin/v1/devices/{id}/firmware-file), which is read and
// refused right there when it is the wrong kind or for another chip; then,
// after the person confirmed, POST /admin/v1/devices/{id}/install-file
// installs it. Only the management API takes these, so only this computer
// does (the unix socket and the loopback dashboard behind its login).
//
// A file picked here is the owner's explicit choice and isn't checked
// against a release signature; the board checks what it always checks (its
// board name in the offer, the image's SHA-256, the bootloader's checks).

// LocalFirmware takes an app image from a file for an update over Wi-Fi
// (link.Hub.AddLocalImage): the next offer of board's version serves it.
type LocalFirmware interface {
	AddLocalImage(board, version, path string) (int64, error)
}

// fileTTL is how long an uploaded file waits for its install; fileKeep how
// long the file stays on disk (a board on Wi-Fi may resume its download).
const (
	fileTTL  = time.Hour
	fileKeep = 24 * time.Hour
)

// FileUpload is what POST /admin/v1/devices/{id}/firmware-file answers.
type FileUpload struct {
	File    string `json:"file"` // the id to install it with
	Name    string `json:"name"` // the file's name as picked
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Kind    string `json:"kind"`              // app | full
	Chip    string `json:"chip"`              // from the image header
	Version string `json:"version,omitempty"` // from the image, when it says one
	Project string `json:"project,omitempty"`
	Via     string `json:"via"`             // wifi | usb: how it would be installed
	Board   string `json:"board,omitempty"` // the board name the offer carries (wifi)
	// ChipChecked: the board's chip is known and matched the image's.
	ChipChecked bool   `json:"chipChecked"`
	Device      string `json:"device"`
	path        string
	at          time.Time
}

type fileBook struct {
	mu    sync.Mutex
	files map[string]FileUpload
}

// versionName: a version as Hello carries it (23 bytes) and as it goes into
// the download's path (build_info.py's rule).
var versionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,22}$`)

// boardLinks: how a board is connected now.
func (a *Admin) boardLinks(name string) (usbPath string, wifi, ble bool) {
	for _, l := range a.linksNow() {
		if l.Board != name {
			continue
		}
		switch l.Transport {
		case link.OverSerial:
			usbPath = l.Where
		case link.OverWifi:
			wifi = true
		case link.OverBLE:
			ble = true
		}
	}
	return
}

// fileWays: how a firmware file would reach a board row now, or why it can't.
func (a *Admin) fileWays(row DeviceRow) ([]string, string) {
	if row.State != "active" {
		return nil, "approve the board first"
	}
	usbPath, wifi, ble := a.boardLinks(row.ID)
	var ways []string
	if usbPath != "" && a.Flasher != nil {
		ways = append(ways, "usb")
	}
	if wifi && a.OTA != nil && a.Local != nil {
		ways = append(ways, "wifi")
	}
	switch {
	case len(ways) > 0:
		return ways, ""
	case wifi:
		return nil, "this build can't send a file over Wi-Fi: plug the board in with a USB cable"
	case ble:
		return nil, link.ErrOnBluetooth.Error()
	}
	return nil, "connect it over USB or Wi-Fi first"
}

// firmwareFile: POST /admin/v1/devices/{id}/firmware-file. The body is the
// file itself (at most firmware.MaxFileImage); X-File-Name names it.
func (a *Admin) firmwareFile(w http.ResponseWriter, r *http.Request) {
	d, row, ok := a.fileDevice(w, r)
	if !ok {
		return
	}
	if len(row.FileVia) == 0 {
		writeErr(w, http.StatusConflict, row.Name+": "+row.FileNote)
		return
	}
	if r.ContentLength > firmware.MaxFileImage {
		writeErr(w, http.StatusRequestEntityTooLarge, firmware.ErrTooBig.Error())
		return
	}
	name := cleanFileName(r.Header.Get("X-File-Name"))
	dir := filepath.Join(a.DataDir, "firmware-files")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.pruneFiles(dir)
	tmp, err := os.CreateTemp(dir, "upload-*.bin")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmp.Name())
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), http.MaxBytesReader(w, r.Body, firmware.MaxFileImage))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeErr(w, http.StatusRequestEntityTooLarge, firmware.ErrTooBig.Error())
		return
	case err != nil:
		writeErr(w, http.StatusBadRequest, "couldn't read the file: "+err.Error())
		return
	case size == 0:
		writeErr(w, http.StatusBadRequest, "the file is empty")
		return
	}
	info, err := firmware.InspectImage(tmp, size)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	up := FileUpload{Name: name, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), Kind: info.Kind, Chip: info.Chip,
		Version: info.Version, Project: info.Project, Device: d.Name}
	if row.Chip != "" {
		if !firmware.SameChip(row.Chip, info.Chip) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("this file is for an %s, and %s is an %s: pick the firmware built for this board", info.Chip, row.Name, row.Chip))
			return
		}
		up.ChipChecked = true
	}
	via, err := fileVia(info, row)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	up.Via = via
	if via == "wifi" {
		up.Board = otaBoard(row, d)
	}
	up.File = up.SHA256[:16]
	up.path = filepath.Join(dir, up.File+".bin")
	up.at = time.Now()
	tmp.Close()
	if err := os.Rename(tmp.Name(), up.path); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	keep = true
	a.files.mu.Lock()
	if a.files.files == nil {
		a.files.files = map[string]FileUpload{}
	}
	a.files.files[up.File] = up
	a.files.mu.Unlock()
	writeJSON(w, http.StatusOK, up)
}

// fileVia picks how a file reaches the board: an app image over Wi-Fi, a
// full image over USB.
func fileVia(info firmware.ImageInfo, row DeviceRow) (string, error) {
	has := func(w string) bool { return contains(row.FileVia, w) }
	switch info.Kind {
	case firmware.ImageApp:
		if !has("wifi") {
			return "", errors.New("this is an app image, which goes over Wi-Fi, and " + row.Name + " isn't on Wi-Fi now. Over USB, pick the full image (the release's agents-deck-<board>-<version>.bin, or a merged image written at 0x0)")
		}
		if info.Size > link.MaxAppImage {
			return "", fmt.Errorf("this app image is %s: an app slot holds %s", megabytes(info.Size), megabytes(link.MaxAppImage))
		}
		return "wifi", nil
	case firmware.ImageFull:
		if !has("usb") {
			return "", errors.New("this is a full image, which is written over USB, and " + row.Name + " isn't plugged in. Plug it in, or pick the app image (.app.bin, or .pio/build/<env>/firmware.bin) to send it over Wi-Fi")
		}
		return "usb", nil
	}
	return "", errors.New("an image of a kind agents deck doesn't install")
}

// otaBoard is the board name an offer carries: its firmware/devices folder,
// which the board compares with its own before it takes the update.
func otaBoard(row DeviceRow, d devices.Device) string {
	if row.Board != "" {
		return row.Board
	}
	return link.BoardOfType(d.Type)
}

// installFile: POST /admin/v1/devices/{id}/install-file {"file", "version"}.
func (a *Admin) installFile(w http.ResponseWriter, r *http.Request) {
	d, row, ok := a.fileDevice(w, r)
	if !ok {
		return
	}
	var b struct {
		File    string `json:"file"`
		Version string `json:"version"`
	}
	if err := readBody(r, 1024, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.files.mu.Lock()
	up, ok := a.files.files[b.File]
	a.files.mu.Unlock()
	if !ok || up.Device != d.Name || time.Since(up.at) > fileTTL {
		writeErr(w, http.StatusNotFound, "that file isn't here any more: pick it again")
		return
	}
	if _, err := os.Stat(up.path); err != nil {
		writeErr(w, http.StatusNotFound, "that file isn't here any more: pick it again")
		return
	}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Device == d.Name || (row.Port != "" && j.Port == row.Port) }); ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": row.Name + " is already updating", "job": j.ID})
		return
	}
	if !contains(row.FileVia, up.Via) {
		where := map[string]string{"wifi": "on Wi-Fi", "usb": "plugged in"}[up.Via]
		writeErr(w, http.StatusConflict, row.Name+" isn't "+where+" any more")
		return
	}
	switch up.Via {
	case "wifi":
		version := strings.TrimSpace(b.Version)
		if version == "" {
			version = up.Version
		}
		if !versionName.MatchString(version) {
			writeErr(w, http.StatusBadRequest, "give the version the firmware reports (at most 23 letters, digits, dots, dashes, plus signs or underscores, like 0.2.0-dev.5)")
			return
		}
		if _, err := a.Local.AddLocalImage(up.Board, version, up.path); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		job := a.jobs.start(Job{Kind: "board-update", Device: d.Name, Board: up.Board, Version: version, File: up.Name,
			Stage: "offered", Message: "Offering " + up.Name + " to the board over Wi-Fi"},
			func(ctx context.Context, upd func(func(*Job))) error {
				return a.runOTA(ctx, upd, d.Name, up.Board, version)
			})
		writeJSON(w, http.StatusAccepted, job)
	case "usb":
		usbPath, _, _ := a.boardLinks(d.Name)
		info, ok := a.portByPath(usbPath)
		if !ok {
			writeErr(w, http.StatusConflict, "the board's port isn't there any more")
			return
		}
		job := a.flashJob(info, Job{Kind: "board-update", Device: d.Name, Board: row.Board, Version: up.Version, File: up.Name},
			0, func(ctx context.Context) (string, []string, error) {
				return up.path, []string{fmt.Sprintf("Using %s (%s, not checked against a release)", up.Name, megabytes(up.Size))}, nil
			})
		writeJSON(w, http.StatusAccepted, job)
	default:
		writeErr(w, http.StatusBadRequest, "that file can't be installed")
	}
}

// fileDevice: the board both calls are about, with its row now.
func (a *Admin) fileDevice(w http.ResponseWriter, r *http.Request) (devices.Device, DeviceRow, bool) {
	if a.Devices == nil {
		writeErr(w, http.StatusServiceUnavailable, "this service keeps no devices")
		return devices.Device{}, DeviceRow{}, false
	}
	id := r.PathValue("id")
	d, ok := a.Devices.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no device called "+id)
		return d, DeviceRow{}, false
	}
	if d.Kind != devices.Serial || d.Revoked {
		writeErr(w, http.StatusBadRequest, id+" isn't a board that takes firmware")
		return d, DeviceRow{}, false
	}
	for _, row := range a.deviceRows() {
		if row.ID == id {
			return d, row, true
		}
	}
	writeErr(w, http.StatusNotFound, "no device called "+id)
	return d, DeviceRow{}, false
}

// cleanFileName keeps a file's base name in printable characters, for
// showing. The page sends it percent-encoded (a header carries no Unicode).
func cleanFileName(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	s = filepath.Base(strings.ReplaceAll(s, `\`, "/"))
	var b strings.Builder
	for _, c := range s {
		if c >= 0x20 && c != 0x7f && b.Len() < 120 {
			b.WriteRune(c)
		}
	}
	if out := b.String(); out != "" && out != "." && out != "/" {
		return out
	}
	return "firmware.bin"
}

// pruneFiles forgets uploads past their time and removes old files.
func (a *Admin) pruneFiles(dir string) {
	a.files.mu.Lock()
	for id, f := range a.files.files {
		if time.Since(f.at) > fileTTL {
			delete(a.files.files, id)
		}
	}
	a.files.mu.Unlock()
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > fileKeep {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
