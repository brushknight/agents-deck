package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Updating a board from an image file on this Mac (`agentctl link update`,
// POST /local/link/firmware on the unix socket): for testing a build on a
// board before it is released. The file goes into the hub's local images,
// which come before the release catalog, and the board gets the usual offer.

// MaxAppImage is an app slot's size on the released boards (6.25 MB).
const MaxAppImage = 0x640000

type localImage struct {
	path, sha string
	size      int64
}

// layered is the local images, then the catalog.
type layered struct {
	local map[string]localImage
	next  FirmwareSource
}

func (l layered) OTAImage(ctx context.Context, board, version string) (string, int64, string, error) {
	if im, ok := l.local[board+"/"+version]; ok {
		return im.path, im.size, im.sha, nil
	}
	if l.next == nil {
		return "", 0, "", ErrNoFirmwareSource
	}
	return l.next.OTAImage(ctx, board, version)
}

func (l layered) BoardTypes(ctx context.Context, board string) ([]string, error) {
	if bt, ok := l.next.(BoardTyper); ok {
		return bt.BoardTypes(ctx, board)
	}
	return nil, nil
}

// UpdateFromFile offers a board on Wi-Fi the app image in file as board's
// firmware version. It returns an HTTP status and a message for the unix
// socket: 202 when the offer went out (the job then follows the board).
func (h *Hub) UpdateFromFile(device, board, version, file string) (int, string) {
	path, err := filepath.Abs(file)
	if err != nil {
		return http.StatusBadRequest, err.Error()
	}
	size, err := h.AddLocalImage(board, version, path)
	if err != nil {
		if errors.Is(err, errLocalImage) {
			return http.StatusBadRequest, err.Error()
		}
		return http.StatusInternalServerError, err.Error()
	}
	if err := h.OfferFirmware(device, board, version); err != nil {
		return firmwareErrCode(err), err.Error()
	}
	return http.StatusAccepted, fmt.Sprintf("offered %s %s (%d bytes) to %s", board, version, size, device)
}

var errLocalImage = errors.New("not an app image")

type localImageErr struct{ msg string }

func (e localImageErr) Error() string        { return e.msg }
func (e localImageErr) Is(target error) bool { return target == errLocalImage }

// AddLocalImage puts the app image at path (absolute) among the local
// images as board's firmware version, before the release catalog, so the
// next offer of that version serves it. It checks that the file starts like
// an ESP32 app image and fits an app slot, and answers its size.
func (h *Hub) AddLocalImage(board, version, path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, localImageErr{err.Error()}
	}
	defer f.Close()
	head := make([]byte, 1)
	if _, err := io.ReadFull(f, head); err != nil || head[0] != 0xE9 {
		return 0, localImageErr{path + " isn't an ESP32 app image (the .app.bin, not the full image flashed at 0x0)"}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return 0, err
	}
	if size > MaxAppImage {
		return 0, localImageErr{fmt.Sprintf("%s is %d bytes: an app slot holds %d", path, size, MaxAppImage)}
	}
	h.mu.Lock()
	if h.local == nil {
		h.local = map[string]localImage{}
	}
	h.local[board+"/"+version] = localImage{path: path, size: size, sha: hex.EncodeToString(hash.Sum(nil))}
	h.mu.Unlock()
	return size, nil
}

// BoardOfType is the firmware/devices folder whose firmware says Hello as
// typ, when the release catalog can't say: the boards that kept an older
// type (knownTypes), else the type itself.
func BoardOfType(typ string) string {
	for board, types := range knownTypes {
		for _, t := range types {
			if t == typ {
				return board
			}
		}
	}
	return typ
}

func firmwareErrCode(err error) int {
	switch {
	case errors.Is(err, ErrNoFirmwareSource):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrNotConnected):
		return http.StatusNotFound
	case errors.Is(err, ErrNotOnWifi), errors.Is(err, ErrUpdating):
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
