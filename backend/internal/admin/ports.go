package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// Port states.
const (
	PortNew        = "new"        // a likely board, not registered, never opened
	PortRegistered = "registered" // registered; the deck opens it, no board has said hello yet
	PortConnected  = "connected"  // a board runs our firmware on it
	PortBusy       = "busy"       // another program has it open, or it can't be opened
	PortFlashing   = "flashing"   // the service is writing or reading the board
	PortUnplugged  = "unplugged"  // registered, not there now
	PortIgnored    = "ignored"    // you said it isn't yours
	PortOther      = "other"      // a USB serial port that doesn't look like a board
)

// PortRow is one serial port in GET /admin/v1/ports.
type PortRow struct {
	ID         string `json:"id"` // the last part of its path: cu.usbmodem1101, ttyACM0, COM5
	Path       string `json:"path"`
	VID        string `json:"vid,omitempty"`
	PID        string `json:"pid,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Product    string `json:"product,omitempty"`
	Likely     string `json:"likely,omitempty"`     // esp32-usb, ch340, cp210x, ftdi
	LikelyName string `json:"likelyName,omitempty"` // in words
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"` // the deck's own words for a registered port
	Registered bool   `json:"registered"`
	Device     string `json:"device,omitempty"` // the board on it (its id)
	Name       string `json:"name,omitempty"`   // that board's name
	Job        string `json:"job,omitempty"`
}

// portID is how a port is named in URLs.
func portID(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(strings.ReplaceAll(path, `\`, "/"))
}

// portHolds are the registered ports the service took back from the Deck
// Link manager to read or write a board itself.
type portHolds struct {
	mu   sync.Mutex
	held map[string]bool
	lent map[string]*time.Timer // ports lent to another program (borrow): handed back when it ends
}

// LinkPorts is what the Deck Link manager opens: the registered ports minus
// the ones the service holds right now (the daemon wires Manager.Ports here).
func (a *Admin) LinkPorts(ps []link.PortConfig) []link.PortConfig {
	a.ports.mu.Lock()
	defer a.ports.mu.Unlock()
	out := make([]link.PortConfig, 0, len(ps))
	for _, p := range ps {
		if !a.ports.held[p.Path] {
			out = append(out, p)
		}
	}
	return out
}

func (a *Admin) held(path string) bool {
	a.ports.mu.Lock()
	defer a.ports.mu.Unlock()
	return a.ports.held[path]
}

// release takes a port from the Deck Link manager and waits until its
// session has closed the port (the manager checks every 2 seconds).
func (a *Admin) release(ctx context.Context, path string) error {
	a.ports.mu.Lock()
	if a.ports.held == nil {
		a.ports.held = map[string]bool{}
	}
	if a.ports.held[path] {
		a.ports.mu.Unlock()
		return errors.New("the service is already using this port")
	}
	a.ports.held[path] = true
	a.ports.mu.Unlock()
	if a.PortStatus == nil {
		return nil
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		open := false
		for _, st := range a.PortStatus() {
			if st.Path == path {
				open = true
			}
		}
		if !open {
			return nil
		}
		if time.Now().After(deadline) {
			a.handBack(path)
			return errors.New("the deck didn't let go of the port: try again in a moment")
		}
		select {
		case <-ctx.Done():
			a.handBack(path)
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// handBack returns a port to the Deck Link manager, which opens it again
// within 2 seconds so the board can say hello and pair.
func (a *Admin) handBack(path string) {
	a.ports.mu.Lock()
	delete(a.ports.held, path)
	if t := a.ports.lent[path]; t != nil {
		t.Stop()
		delete(a.ports.lent, path)
	}
	a.ports.mu.Unlock()
}

// maxBorrow: how long a port can be lent (borrow) before the deck takes it back.
const maxBorrow = 30 * time.Minute

// lend takes path from the Deck Link manager for another program on this
// computer (`agentctl flash`), for d at most.
func (a *Admin) lend(ctx context.Context, path string, d time.Duration) error {
	if err := a.release(ctx, path); err != nil {
		return err
	}
	a.ports.mu.Lock()
	if a.ports.lent == nil {
		a.ports.lent = map[string]*time.Timer{}
	}
	a.ports.lent[path] = time.AfterFunc(d, func() { a.handBack(path) })
	a.ports.mu.Unlock()
	return nil
}

func (a *Admin) ignored() map[string]bool {
	out := map[string]bool{}
	if l, ok := a.readConfig()["ignoredPorts"].([]any); ok {
		for _, v := range l {
			if s, ok := v.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

func (a *Admin) setIgnored(path string, on bool) error {
	return a.editConfig(func(cfg map[string]any) error {
		var keep []string
		if l, ok := cfg["ignoredPorts"].([]any); ok {
			for _, v := range l {
				if s, ok := v.(string); ok && s != path {
					keep = append(keep, s)
				}
			}
		}
		if on {
			keep = append(keep, path)
		}
		if len(keep) == 0 {
			delete(cfg, "ignoredPorts")
		} else {
			cfg["ignoredPorts"] = keep
		}
		return nil
	})
}

// portRows lists the serial ports from their USB ids, without opening any,
// merged with the registered ports and the boards on them.
func (a *Admin) portRows() []PortRow {
	var listed []serial.Info
	if a.ListPorts != nil {
		listed, _ = a.ListPorts()
	}
	registered := map[string]bool{}
	for _, p := range a.registeredPorts() {
		registered[p.Path] = true
	}
	status := map[string]link.PortStatus{}
	if a.PortStatus != nil {
		for _, s := range a.PortStatus() {
			status[s.Path] = s
		}
	}
	boards := map[string]string{} // port path → board id
	for _, l := range a.linksNow() {
		if l.Transport == link.OverSerial {
			boards[l.Where] = l.Board
		}
	}
	ignored := a.ignored()
	labels := a.Labels()
	seen := map[string]bool{}
	var out []PortRow
	add := func(i serial.Info, present bool) {
		seen[i.Path] = true
		row := PortRow{ID: portID(i.Path), Path: i.Path, VID: i.VID, PID: i.PID, Serial: i.Serial, Product: i.Product,
			Likely: i.Likely, LikelyName: serial.LikelyName(i.Likely), Registered: registered[i.Path]}
		st, hasSt := status[i.Path]
		if hasSt {
			row.Detail = st.State
			if st.Problem != "" {
				row.Detail += ": " + st.Problem
			}
		}
		row.Device = boards[i.Path]
		if row.Device == "" && st.Board != "" {
			row.Device = st.Board
		}
		if row.Device != "" {
			row.Name = labels[row.Device]
			if row.Name == "" {
				row.Name = row.Device
			}
		}
		if j, ok := a.jobs.running(func(j Job) bool { return j.Port == row.ID }); ok {
			row.Job = j.ID
		}
		switch {
		case row.Job != "" || a.held(i.Path):
			row.State = PortFlashing
		case !present:
			row.State = PortUnplugged
		case row.Registered && row.Device != "":
			row.State = PortConnected
		case row.Registered && hasSt && (strings.HasPrefix(st.State, "in use") || st.State == "error"):
			row.State = PortBusy
		case row.Registered:
			row.State = PortRegistered
		case ignored[i.Path]:
			row.State = PortIgnored
		case i.Likely != "":
			row.State = PortNew
		default:
			row.State = PortOther
		}
		out = append(out, row)
	}
	for _, i := range listed {
		if i.VID == "" && !registered[i.Path] {
			continue // not USB (Bluetooth, the debug console): never a board
		}
		add(i, true)
	}
	for _, p := range a.registeredPorts() {
		if !seen[p.Path] {
			_, err := os.Stat(p.Path)
			present := err == nil && a.ListPorts == nil
			add(serial.Info{Path: p.Path}, present)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (a *Admin) listPorts(w http.ResponseWriter, r *http.Request) {
	rows := a.portRows()
	if rows == nil {
		rows = []PortRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ports": rows})
}

// portByID finds a listed or registered port by its id.
func (a *Admin) portByID(id string) (PortRow, bool) {
	for _, p := range a.portRows() {
		if p.ID == id {
			return p, true
		}
	}
	return PortRow{}, false
}

func (a *Admin) portByPath(path string) (serial.Info, bool) {
	for _, p := range a.portRows() {
		if p.Path == path {
			return p.info(), true
		}
	}
	return serial.Info{}, false
}

func (p PortRow) info() serial.Info {
	return serial.Info{Path: p.Path, VID: p.VID, PID: p.PID, Serial: p.Serial, Product: p.Product, Likely: p.Likely}
}

// portAction: POST /admin/v1/ports/{id}/{register|identify|flash|forget}.
// flash with "source": "local" writes a build made on this computer
// (builds.go); without, a release's, downloaded and checked.
func (a *Admin) portAction(w http.ResponseWriter, r *http.Request) {
	p, ok := a.portByID(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such port: is the board still plugged in?")
		return
	}
	switch r.PathValue("action") {
	case "register":
		// "Yes, it's mine": from now on the deck opens it.
		var b struct {
			Baud int `json:"baud"`
		}
		_ = readBody(r, 1024, &b)
		if b.Baud <= 0 {
			b.Baud = 115200
		}
		err := a.setRegisteredPorts(func(ps []link.PortConfig) []link.PortConfig {
			for i := range ps {
				if ps[i].Path == p.Path {
					return ps
				}
			}
			return append(ps, link.PortConfig{Path: p.Path, Baud: b.Baud})
		})
		if err == nil {
			err = a.setIgnored(p.Path, false)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		p, _ = a.portByID(p.ID)
		writeJSON(w, http.StatusOK, p)
	case "forget":
		// Registered: the deck stops using it. New: "not mine", don't ask again.
		var err error
		if p.Registered {
			err = a.setRegisteredPorts(func(ps []link.PortConfig) []link.PortConfig {
				out := ps[:0]
				for _, q := range ps {
					if q.Path != p.Path {
						out = append(out, q)
					}
				}
				return out
			})
		}
		if err == nil && p.State != PortUnplugged {
			err = a.setIgnored(p.Path, true)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "identify":
		a.identify(w, r, p)
	case "flash":
		var b struct {
			Board   string `json:"board"`
			Version string `json:"version"`
			Source  string `json:"source"` // release (the default) | local
		}
		if err := readBody(r, 1024, &b); err != nil || b.Board == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"board": "<board>", "version": "<version>", "source": "release"|"local"} (version optional: the latest)`)
			return
		}
		if b.Source == SourceLocal {
			a.portFlashLocal(w, p, b.Board, b.Version)
			return
		}
		if !p.Registered {
			writeErr(w, http.StatusForbidden, "register the port first: the service only opens ports you said are yours")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		cat, err := a.catalogFor(ctx, b.Version)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "couldn't read the firmware list: "+err.Error())
			return
		}
		board, ok := cat.Board(b.Board)
		if !ok {
			writeErr(w, http.StatusNotFound, "release "+cat.Version+" has no firmware for "+b.Board)
			return
		}
		job, code, err := a.startFlash(p.info(), board, cat.Version, p.Device, "flash")
		if err != nil {
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	case "borrow":
		// Another program on this computer (agentctl flash) opens the port
		// itself: the deck lets go until "return", or until "seconds" pass.
		var b struct {
			Seconds int `json:"seconds"`
		}
		_ = readBody(r, 1024, &b)
		d := time.Duration(b.Seconds) * time.Second
		if d <= 0 || d > maxBorrow {
			d = maxBorrow
		}
		if err := a.lend(r.Context(), p.Path, d); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "return":
		a.ports.mu.Lock()
		lent := a.ports.lent[p.Path] != nil
		a.ports.mu.Unlock()
		if lent {
			a.handBack(p.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusNotFound, "unknown action: register, identify, flash, forget, borrow, return, firmware-file or install-file")
	}
}

func (a *Admin) catalogFor(ctx context.Context, version string) (firmware.Catalog, error) {
	if c, ok := a.Firmware.(interface {
		Catalog(context.Context, string) (firmware.Catalog, error)
	}); ok && version != "" && version != "latest" {
		return c.Catalog(ctx, version)
	}
	return a.loadCatalog(ctx)
}

// Identity is what POST /admin/v1/ports/{id}/identify found.
type Identity struct {
	Source     string           `json:"source"` // session (our firmware said hello) | probe (the bootloader answered)
	Device     *DeviceRow       `json:"device,omitempty"`
	Board      *firmware.Board  `json:"board,omitempty"` // the catalog board, when known
	Chip       string           `json:"chip,omitempty"`
	FlashSize  int64            `json:"flashSize,omitempty"`
	MAC        string           `json:"mac,omitempty"`
	Candidates []firmware.Board `json:"candidates"`
	// Local: builds made on this computer that fit (the newest of each
	// board), installed with flash {"source": "local"}.
	Local      []firmware.Board `json:"local"`
	Latest     string           `json:"latest,omitempty"`
	ReleaseKey bool             `json:"releaseKey"`
	Note       string           `json:"note,omitempty"`
}

// identify answers from the board's Deck Link session when one is live on
// the port; otherwise it borrows the port and asks the ROM bootloader.
func (a *Admin) identify(w http.ResponseWriter, r *http.Request, p PortRow) {
	if !p.Registered {
		writeErr(w, http.StatusForbidden, "register the port first: the service only opens ports you said are yours")
		return
	}
	if p.Job != "" {
		writeErr(w, http.StatusConflict, "the service is writing to this board right now")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	id := Identity{Candidates: []firmware.Board{}, Local: []firmware.Board{}, ReleaseKey: a.releaseKey()}
	cat, catErr := a.loadCatalog(ctx)
	if catErr == nil {
		id.Latest = cat.Version
	} else {
		id.Note = "couldn't read the firmware list: " + catErr.Error()
	}
	if p.Device != "" {
		a.probes.forget(p.Path)
		id.Source = "session"
		for _, d := range a.deviceRows() {
			if d.ID == p.Device {
				d := d
				id.Device = &d
				var board *firmware.Board
				if b, ok := cat.BoardForType(d.Type); ok && catErr == nil {
					id.Board, board = &b, &b
					id.Candidates = append(id.Candidates, b)
				}
				if lb := localFor(a.builds().Latest(), d.Type, board); lb != nil {
					id.Local = append(id.Local, *lb)
				}
			}
		}
		writeJSON(w, http.StatusOK, id)
		return
	}
	if a.Flasher == nil {
		writeErr(w, http.StatusNotImplemented, "this service can't talk to a board's bootloader")
		return
	}
	if err := a.release(ctx, p.Path); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	a.probes.forget(p.Path)
	chip, err := a.Flasher.Probe(ctx, p.info())
	a.handBack(p.Path)
	if err != nil {
		res := map[string]any{"error": plainFlashError(err)}
		if errors.Is(err, flash.ErrNoBootloader) {
			res["bootHelp"] = true
		}
		writeJSON(w, http.StatusBadGateway, res)
		return
	}
	id.Source, id.Chip, id.FlashSize, id.MAC = "probe", chip.Name, chip.FlashSize, chip.MAC
	a.probes.put(p.Path, probed{chip: chip, vid: p.VID, pid: p.PID, serialNum: p.Serial, at: time.Now()})
	id.Local = append(id.Local, a.localCandidates(chip.Name, chip.FlashSize)...)
	if catErr == nil {
		id.Candidates = append(id.Candidates, cat.Candidates(chip.Name, chip.FlashSize)...)
		if len(id.Candidates) == 0 && len(id.Local) == 0 {
			id.Note = fmt.Sprintf("no board in release %s uses an %s", cat.Version, chip.Name)
		}
	}
	writeJSON(w, http.StatusOK, id)
}

func plainFlashError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the board took too long to answer: hold BOOT, press and release RST, let go of BOOT, then try again"
	case errors.Is(err, context.Canceled):
		return "stopped"
	}
	return err.Error()
}

// startFlash downloads and checks a board's image, borrows the port, writes
// it at the manifest's offset and hands the port back so the board pairs.
// One at a time per port. It answers the job, or an HTTP status and error.
func (a *Admin) startFlash(port serial.Info, board firmware.Board, version, device, kind string) (Job, int, error) {
	if a.Flasher == nil || a.Firmware == nil {
		return Job{}, http.StatusNotImplemented, errors.New("this service can't flash boards")
	}
	if !a.releaseKey() {
		return Job{}, http.StatusConflict, errors.New("this copy of agents deck was built from source, so it can't check firmware downloaded from a release and won't install it. " +
			"Firmware built on this computer installs in one click: put the build there with agentctl firmware add <folder>, or pick a file with Install a firmware file… in Settings, Boards")
	}
	if j, ok := a.jobs.running(func(j Job) bool { return j.Port == portID(port.Path) }); ok {
		return j, http.StatusConflict, errors.New("this board is being written already")
	}
	if version == "" {
		version = board.Version
	}
	job := a.flashJob(port, Job{Kind: kind, Device: device, Board: board.Board, Version: version,
		Stage: "download", Message: "Downloading firmware " + version}, board.Offset,
		func(ctx context.Context) (string, []string, error) {
			img, err := a.Firmware.Image(ctx, board.Board, version)
			if err != nil {
				return "", nil, err
			}
			fi, err := os.Stat(img)
			if err != nil {
				return "", nil, err
			}
			return img, []string{fmt.Sprintf("Downloaded firmware %s (%s)", version, megabytes(fi.Size())),
				"Checked its signature and checksum"}, nil
		})
	return job, http.StatusAccepted, nil
}

// flashJob runs a flash of the image that image gives (a path, and the
// steps that took in words), borrowing the port and writing at offset, as
// a job. The caller checked that nothing else writes to the port.
func (a *Admin) flashJob(port serial.Info, base Job, offset uint32, image func(ctx context.Context) (string, []string, error)) Job {
	base.Port = portID(port.Path)
	if base.Stage == "" {
		base.Stage, base.Message = "connect", "Waiting for the board to answer in install mode"
	}
	return a.jobs.start(base,
		func(ctx context.Context, up func(func(*Job))) error {
			img, steps, err := image(ctx)
			if err != nil {
				return err
			}
			f, err := os.Open(img)
			if err != nil {
				return err
			}
			defer f.Close()
			fi, err := f.Stat()
			if err != nil {
				return err
			}
			size := fi.Size()
			up(func(j *Job) {
				j.Percent = 5
				j.Steps = append(j.Steps, steps...)
				j.Stage, j.Message = "connect", "Waiting for the board to answer in install mode"
			})
			if err := a.release(ctx, port.Path); err != nil {
				return err
			}
			defer a.handBack(port.Path)
			answered := false
			err = a.Flasher.Write(ctx, port, f, size, offset, func(stage string, done, total int64) {
				up(func(j *Job) {
					if stage != "connect" && !answered {
						answered = true
						j.Steps = append(j.Steps, "Board answered in install mode")
					}
					j.Stage, j.Done, j.Total = stage, done, total
					j.Percent = flashPercent(stage, done, total)
					j.Message = map[string]string{
						"connect": "Waiting for the board to answer in install mode",
						"erase":   "Erasing",
						"write":   "Writing",
						"verify":  "Checking what was written",
						"reset":   "Restarting the board",
					}[stage]
					if stage == "reset" && !contains(j.Steps, "Verified what was written") {
						j.Steps = append(j.Steps, "Wrote the firmware", "Verified what was written")
					}
				})
			})
			if err != nil {
				up(func(j *Job) {
					j.BootHelp = errors.Is(err, flash.ErrNoBootloader)
				})
				return errors.New(plainFlashError(err))
			}
			up(func(j *Job) {
				j.Stage, j.Message = "reset", "Done. The board restarts and pairs in a few seconds."
				if !contains(j.Steps, "Verified what was written") {
					j.Steps = append(j.Steps, "Wrote the firmware", "Verified what was written")
				}
				j.Steps = append(j.Steps, "Restarted the board")
			})
			return nil
		})
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// flashPercent spreads the flasher's stages over 5..100: writing is most of it.
func flashPercent(stage string, done, total int64) int {
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	switch stage {
	case "connect":
		return 5
	case "erase":
		return 8
	case "write":
		return 10 + int(frac*80)
	case "verify":
		return 90 + int(frac*7)
	case "reset":
		return 98
	}
	return 5
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
