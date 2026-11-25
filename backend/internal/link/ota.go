package link

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

// Firmware updates over Wi-Fi (docs/link/protocol.md, "Firmware updates over
// Wi-Fi"): the deck offers a board on Wi-Fi a new app image (FirmwareOffer),
// the board downloads it from GET /v1/firmware/{board}/{version} on the
// network listener with its certificate, and reports how it goes
// (FirmwareStatus). Boards on USB are updated by the flasher instead.

// FirmwareSource gives the app image (the over-the-air file) of a board's
// firmware version: a verified file on disk, its size and its SHA-256 (hex).
// The release catalog (internal/firmware's Client) satisfies it.
type FirmwareSource interface {
	OTAImage(ctx context.Context, board, version string) (path string, size int64, sha256 string, err error)
}

// BoardTyper is optional on a FirmwareSource: the Hello types a board's
// firmware reports (firmware-manifest.json "types"). Without it, knownTypes
// and "the type is the board's name" decide.
type BoardTyper interface {
	BoardTypes(ctx context.Context, board string) ([]string, error)
}

// knownTypes: boards whose firmware reports a type other than the board's
// folder name (firmware/devices/<board>).
var knownTypes = map[string][]string{
	"lilygo-t4-s3": {"t4s3-panel"}, // it keeps the earlier firmware's type
}

// Errors from OfferFirmware, for the management API to word.
var (
	ErrNoFirmwareSource = errors.New("no firmware source: this service can't update boards over Wi-Fi")
	ErrNotConnected     = errors.New("the board isn't connected")
	ErrNotOnWifi        = errors.New("the board isn't on Wi-Fi: update it over the USB cable")
	ErrWrongBoard       = errors.New("that firmware is for another board")
	ErrUpdating         = errors.New("the board is already updating")
)

// Firmware job states (FirmwareJob.State).
const (
	FirmwareOffered     = "offered"
	FirmwareDownloading = "downloading"
	FirmwareVerifying   = "verifying"
	FirmwareRestarting  = "restarting"
	FirmwareDone        = "done"        // the board came back with the new version
	FirmwareFailed      = "failed"      // the board kept its firmware
	FirmwareRefused     = "refused"     // the board didn't take the offer
	FirmwareRolledBack  = "rolled back" // it restarted, didn't reach the deck, and went back
)

// FirmwareJob is one board update over Wi-Fi.
type FirmwareJob struct {
	Device  string    `json:"device"`  // the board's name on the deck
	Board   string    `json:"board"`   // the image's board, e.g. "lilygo-t4-s3"
	Version string    `json:"version"` // the version offered
	From    string    `json:"from"`    // what it ran when offered
	State   string    `json:"state"`
	Percent int       `json:"percent"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
}

// Active: not finished yet.
func (j FirmwareJob) Active() bool {
	switch j.State {
	case FirmwareOffered, FirmwareDownloading, FirmwareVerifying, FirmwareRestarting:
		return true
	}
	return false
}

// FirmwareInfo is what the deck knows about a board's firmware.
type FirmwareInfo struct {
	Device    string       `json:"device"`
	Type      string       `json:"type"`                // Hello.type
	Version   string       `json:"version"`             // Hello.firmware: what it runs
	Transport string       `json:"transport,omitempty"` // serial | wifi while connected
	Job       *FirmwareJob `json:"job,omitempty"`       // the last update over Wi-Fi
}

// firmwareTimeouts: an active job with nothing new for this long no longer
// blocks another offer (the board went away, or runs firmware that ignores
// offers).
var (
	FirmwareStale       = 2 * time.Minute
	FirmwareRestartWait = 10 * time.Minute // how long RESTARTING waits for the board's Hello
)

type firmwareBook struct {
	mu   sync.Mutex
	jobs map[string]*FirmwareJob // by device name
}

func (h *Hub) fw() *firmwareBook {
	h.fwOnce.Do(func() { h.fwBook = &firmwareBook{jobs: map[string]*FirmwareJob{}} })
	return h.fwBook
}

// SetFirmwareSource plugs the release catalog in (nil: none).
func (h *Hub) SetFirmwareSource(src FirmwareSource) {
	h.mu.Lock()
	h.firmware = src
	h.mu.Unlock()
}

func (h *Hub) source() FirmwareSource {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.local) > 0 { // images from files on this Mac (ota_local.go) first
		local := make(map[string]localImage, len(h.local))
		for k, v := range h.local {
			local[k] = v
		}
		return layered{local: local, next: h.firmware}
	}
	return h.firmware
}

// note records a job change and tells OnFirmware.
func (h *Hub) noteJob(j FirmwareJob) {
	j.Updated = time.Now()
	b := h.fw()
	b.mu.Lock()
	b.jobs[j.Device] = &j
	b.mu.Unlock()
	if h.OnFirmware != nil {
		h.OnFirmware(j)
	}
}

func (h *Hub) job(device string) (FirmwareJob, bool) {
	b := h.fw()
	b.mu.Lock()
	defer b.mu.Unlock()
	if j, ok := b.jobs[device]; ok {
		return *j, true
	}
	return FirmwareJob{}, false
}

// FirmwareJob is the last update over Wi-Fi of a board (by device name).
func (h *Hub) FirmwareJob(device string) (FirmwareJob, bool) { return h.job(device) }

// Firmware lists the connected boards' firmware (version, type, transport)
// and every board's last update over Wi-Fi, by device name.
func (h *Hub) Firmware() []FirmwareInfo {
	byName := map[string]*FirmwareInfo{}
	h.mu.Lock()
	for s := range h.live {
		name := s.Board()
		hp := s.helloSeen.Load()
		if name == "" || hp == nil {
			continue
		}
		fi := byName[name]
		if fi == nil || s.Transport == OverWifi {
			byName[name] = &FirmwareInfo{Device: name, Type: hp.Type, Version: hp.Firmware, Transport: s.Transport}
		}
	}
	h.mu.Unlock()
	b := h.fw()
	b.mu.Lock()
	for name, j := range b.jobs {
		fi := byName[name]
		if fi == nil {
			fi = &FirmwareInfo{Device: name}
			byName[name] = fi
		}
		jj := *j
		fi.Job = &jj
	}
	b.mu.Unlock()
	out := make([]FirmwareInfo, 0, len(byName))
	for _, fi := range byName {
		out = append(out, *fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// wifiSession is the board's approved Deck Link session over Wi-Fi.
func (h *Hub) wifiSession(device string) (wifi *Session, other bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.live {
		if s.Board() != device || s.helloSeen.Load() == nil {
			continue
		}
		if s.Transport == OverWifi && s.live.Load() {
			return s, true
		}
		other = true
	}
	return nil, other
}

// boardFits: a board that says Hello as typ runs board's firmware.
func boardFits(ctx context.Context, src FirmwareSource, board, typ string) bool {
	if typ == "" || board == "" {
		return false
	}
	types := knownTypes[board]
	if bt, ok := src.(BoardTyper); ok {
		if t, err := bt.BoardTypes(ctx, board); err == nil && len(t) > 0 {
			types = t
		}
	}
	if len(types) == 0 {
		types = []string{board}
	}
	for _, t := range types {
		if t == typ {
			return true
		}
	}
	return false
}

// A board or version name as it may appear in a path and in a frame.
var firmwareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// FirmwarePath is where a board downloads a version's app image.
func FirmwarePath(board, version string) string {
	return "/v1/firmware/" + url.PathEscape(board) + "/" + url.PathEscape(version)
}

// image looks the app image up and checks what the source says about it.
func image(ctx context.Context, src FirmwareSource, board, version string) (string, int64, []byte, error) {
	path, size, sum, err := src.OTAImage(ctx, board, version)
	if err != nil {
		return "", 0, nil, err
	}
	raw, err := hex.DecodeString(sum)
	if err != nil || len(raw) != 32 {
		return "", 0, nil, fmt.Errorf("the catalog's SHA-256 for %s %s isn't one", board, version)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, nil, err
	}
	if size <= 0 || st.Size() != size {
		return "", 0, nil, fmt.Errorf("%s is %d bytes, the catalog says %d", path, st.Size(), size)
	}
	return path, size, raw, nil
}

// OfferTimeout bounds handing the offer to the board's session.
var OfferTimeout = 5 * time.Second

// OfferFirmware offers a board on Wi-Fi (by device name) a version of a
// board's firmware. It returns once the offer is on its way; the job
// (FirmwareJob, OnFirmware) follows the board's FirmwareStatus and its Hello
// after the restart. Refused: no source, the board not on Wi-Fi, not that
// board's firmware, already updating.
func (h *Hub) OfferFirmware(deviceID, board, version string) error {
	src := h.source()
	if src == nil {
		return ErrNoFirmwareSource
	}
	if !firmwareName.MatchString(board) || !firmwareName.MatchString(version) ||
		len(board) >= limits["FirmwareOffer.board"].size || len(version) >= limits["FirmwareOffer.version"].size {
		return fmt.Errorf("bad board or version name %q %q", board, version)
	}
	s, other := h.wifiSession(deviceID)
	if s == nil {
		if other {
			if h.onlyBLE(deviceID) {
				return ErrOnBluetooth
			}
			return ErrNotOnWifi
		}
		return ErrNotConnected
	}
	hello := s.helloSeen.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if !boardFits(ctx, src, board, hello.Type) {
		return fmt.Errorf("%w: %s is a %s", ErrWrongBoard, deviceID, hello.Type)
	}
	if j, ok := h.job(deviceID); ok && j.Active() && !j.stale() {
		return ErrUpdating
	}
	_, size, sum, err := image(ctx, src, board, version)
	if err != nil {
		return fmt.Errorf("firmware %s %s: %w", board, version, err)
	}
	path := FirmwarePath(board, version)
	if len(path) >= limits["FirmwareOffer.path"].size {
		return fmt.Errorf("firmware path too long: %s", path)
	}
	h.noteJob(FirmwareJob{Device: deviceID, Board: board, Version: version, From: hello.Firmware,
		State: FirmwareOffered, Started: time.Now()})
	f := &linkpb.Frame{Body: &linkpb.Frame_FirmwareOffer{FirmwareOffer: &linkpb.FirmwareOffer{
		Board: board, Version: version, Size: uint64(size), Sha256: sum, Path: path}}}
	sctx, scancel := context.WithTimeout(context.Background(), OfferTimeout)
	defer scancel()
	if err := s.post(sctx, f); err != nil {
		h.noteJob(FirmwareJob{Device: deviceID, Board: board, Version: version, From: hello.Firmware,
			State: FirmwareFailed, Error: "the offer didn't reach the board: " + err.Error(), Started: time.Now()})
		return fmt.Errorf("%w: %v", ErrNotConnected, err)
	}
	log.Printf("link: offered %s firmware %s %s (%d bytes)", deviceID, board, version, size)
	return nil
}

func (j FirmwareJob) stale() bool {
	wait := FirmwareStale
	if j.State == FirmwareRestarting {
		wait = FirmwareRestartWait
	}
	return time.Since(j.Updated) > wait
}

// firmwareStatus: the board's FirmwareStatus.
func (h *Hub) firmwareStatus(device string, st *linkpb.FirmwareStatus) {
	j, ok := h.job(device)
	if !ok {
		j = FirmwareJob{Device: device, Started: time.Now()}
	}
	switch st.State {
	case linkpb.FirmwareState_FIRMWARE_DOWNLOADING:
		j.State = FirmwareDownloading
	case linkpb.FirmwareState_FIRMWARE_VERIFYING:
		j.State = FirmwareVerifying
	case linkpb.FirmwareState_FIRMWARE_RESTARTING:
		j.State = FirmwareRestarting
	case linkpb.FirmwareState_FIRMWARE_FAILED:
		j.State = FirmwareFailed
	case linkpb.FirmwareState_FIRMWARE_REFUSED:
		j.State = FirmwareRefused
	default:
		return
	}
	j.Percent = min(int(st.Percent), 100)
	if st.State == linkpb.FirmwareState_FIRMWARE_RESTARTING {
		j.Percent = 100
	}
	j.Error = st.Error
	if j.State == FirmwareFailed || j.State == FirmwareRefused || j.State == FirmwareRestarting {
		line := j.State
		if j.Error != "" {
			line += ": " + j.Error
		}
		log.Printf("link: %s firmware %s %s", device, j.Version, line)
	}
	h.noteJob(j)
}

// firmwareHello: a board said Hello; after RESTARTING that tells how the
// update went.
func (h *Hub) firmwareHello(device, firmware string) {
	j, ok := h.job(device)
	if !ok || j.State != FirmwareRestarting {
		return
	}
	switch firmware {
	case j.Version:
		j.State, j.Error = FirmwareDone, ""
		log.Printf("link: %s now runs %s", device, firmware)
	case j.From:
		// The new firmware's first Hello may come before it went back: only
		// the old version after a restart means it went back.
		j.State, j.Error = FirmwareRolledBack, "the board went back to "+firmware
		log.Printf("link: %s went back to %s after the update to %s", device, firmware, j.Version)
	default:
		return
	}
	h.noteJob(j)
}

// ---- the download -----------------------------------------------------------

// Auditor is a Deck with an audit log (ServerDeck).
type Auditor interface {
	Audit(e devices.AuditEntry)
}

// FirmwareCheckEvery: while an image streams, the board's certificate is
// checked again after this many bytes (a revoked board is cut off).
const FirmwareCheckEvery = 256 << 10

// FirmwareHandler serves GET /v1/firmware/{board}/{version} on the network
// listener (TLS 1.3, a client certificate): the app image for a board on
// Wi-Fi. Only a Deck Link board whose type runs that board's firmware gets
// it; HTTP Range lets it resume; every request is audited.
func FirmwareHandler(deck Deck, hub *Hub) func(http.ResponseWriter, *http.Request, devices.Device) {
	return func(w http.ResponseWriter, r *http.Request, d devices.Device) {
		code, detail := serveFirmware(w, r, d, deck, hub)
		if a, ok := deck.(Auditor); ok {
			a.Audit(devices.AuditEntry{Device: d.Name, Action: r.Method + " " + r.URL.Path, Status: code, Detail: detail})
		}
	}
}

func serveFirmware(w http.ResponseWriter, r *http.Request, d devices.Device, deck Deck, hub *Hub) (int, string) {
	fail := func(code int, msg string) (int, string) {
		writeErr(w, code, msg)
		return code, msg
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return fail(http.StatusMethodNotAllowed, "GET /v1/firmware/{board}/{version}")
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/firmware/")
	board, version, ok := strings.Cut(rest, "/")
	if !ok || !firmwareName.MatchString(board) || !firmwareName.MatchString(version) {
		return fail(http.StatusNotFound, "GET /v1/firmware/{board}/{version}")
	}
	if d.Kind != devices.Serial || d.BoardID == "" || !d.Active() {
		return fail(http.StatusForbidden, "only Deck Link boards download firmware")
	}
	src := hub.source()
	if src == nil {
		return fail(http.StatusServiceUnavailable, ErrNoFirmwareSource.Error())
	}
	ctx := r.Context()
	if !boardFits(ctx, src, board, d.Type) {
		return fail(http.StatusForbidden, d.Name+" is a "+d.Type+": "+board+"'s firmware isn't for it")
	}
	path, size, sum, err := image(ctx, src, board, version)
	if err != nil {
		return fail(http.StatusNotFound, "no firmware "+board+" "+version+": "+err.Error())
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	defer f.Close()
	sumHex := hex.EncodeToString(sum)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+sumHex+`"`)
	w.Header().Set("X-Firmware-SHA256", sumHex)
	sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
	body := &revocable{ReadSeeker: io.NewSectionReader(f, 0, size), check: func() bool {
		cur, ok := deck.Device(d.Name)
		return ok && cur.Active() && cur.CertSerial == d.CertSerial
	}}
	http.ServeContent(sw, r, "", time.Time{}, body)
	detail := fmt.Sprintf("%d bytes", size)
	if rg := r.Header.Get("Range"); rg != "" {
		detail += ", " + rg
	}
	if body.cut {
		detail += ", cut off: revoked"
	}
	return sw.code, detail
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

// revocable stops reading once check fails (checked every FirmwareCheckEvery bytes).
type revocable struct {
	io.ReadSeeker
	check func() bool
	n     int
	cut   bool
}

func (r *revocable) Read(p []byte) (int, error) {
	if r.cut {
		return 0, errCutOff
	}
	if r.n >= FirmwareCheckEvery {
		r.n = 0
		if !r.check() {
			r.cut = true
			return 0, errCutOff
		}
	}
	n, err := r.ReadSeeker.Read(p)
	r.n += n
	return n, err
}
