package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pets"
	"github.com/brushknight/agents-deck/backend/internal/serial"
	"github.com/brushknight/agents-deck/backend/internal/service"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/update"
	"github.com/brushknight/agents-deck/backend/internal/update/updatetest"
)

func TestMain(m *testing.M) {
	// Nothing here may read or write the real data folder (accounts read config.json there).
	dir, _ := os.MkdirTemp("", "admin-test-home")
	os.Setenv("AGENTSTERM_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// ---- fakes ---------------------------------------------------------------------------------

// fakeFlasher records what it was asked; it never touches a port.
type fakeFlasher struct {
	mu      sync.Mutex
	probes  int
	writes  int
	image   []byte
	offset  uint32
	heldNow func() bool // whether the service held the port while it ran
	held    []bool
	err     error
	block   bool // wait for the context to end
	chip    flash.Chip
}

func (f *fakeFlasher) Probe(ctx context.Context, p serial.Info) (flash.Chip, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	f.held = append(f.held, f.heldNow())
	if f.err != nil {
		return flash.Chip{}, f.err
	}
	return f.chip, nil
}

func (f *fakeFlasher) Write(ctx context.Context, p serial.Info, image io.Reader, size int64, offset uint32, prog flash.Progress) error {
	f.mu.Lock()
	f.writes++
	f.held = append(f.held, f.heldNow())
	block, err := f.block, f.err
	f.mu.Unlock()
	if err != nil {
		prog("connect", 0, 0)
		return err
	}
	b, _ := io.ReadAll(image)
	f.mu.Lock()
	f.image, f.offset = b, offset
	f.mu.Unlock()
	prog("connect", 0, 0)
	prog("erase", 0, size)
	prog("write", size/2, size)
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	prog("write", size, size)
	prog("verify", size, size)
	prog("reset", 0, 0)
	return nil
}

// fakeLogin stands in for launchd: it only counts.
type fakeLogin struct {
	mu                   sync.Mutex
	registered           bool
	installs, uninstalls int
}

func (l *fakeLogin) Install(io.Writer) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.installs++
	l.registered = true
	return nil
}
func (l *fakeLogin) Uninstall(io.Writer) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uninstalls++
	l.registered = false
	return nil
}
func (l *fakeLogin) Registration() service.Registration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return service.Registration{Kind: "launchd", Registered: l.registered}
}

type fakeUpdater struct {
	mu                       sync.Mutex
	st                       update.State
	checks, downloads, apply int
}

func (u *fakeUpdater) State() update.State { u.mu.Lock(); defer u.mu.Unlock(); return u.st }
func (u *fakeUpdater) Check(context.Context) (update.State, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checks++
	if u.st.State == update.StateNone {
		u.st.State, u.st.Version = update.StateAvailable, "0.3.0"
	}
	return u.st, nil
}
func (u *fakeUpdater) Download(context.Context) (update.State, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.downloads++
	u.st.State = update.StateReady
	return u.st, nil
}
func (u *fakeUpdater) Apply(context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.apply++
	u.st.State = update.StateInstalling
	return nil
}

// fakeOTA is the hub: it takes the offer and reports the board's progress.
type fakeOTA struct {
	a       *Admin
	offered []string
	end     string // the last state the board reports
}

func (o *fakeOTA) OfferFirmware(dev, board, version string) error {
	o.offered = append(o.offered, dev+" "+board+" "+version)
	go func() {
		for _, st := range []string{link.FirmwareOffered, link.FirmwareDownloading, link.FirmwareVerifying, link.FirmwareRestarting, o.end} {
			time.Sleep(5 * time.Millisecond)
			o.a.FirmwareChanged(link.FirmwareJob{Device: dev, Board: board, Version: version, State: st, Percent: 50, Error: "no room"})
		}
	}()
	return nil
}

// ---- a test service ---------------------------------------------------------------------------

const (
	newPort  = "/dev/cu.usbmodem9001"
	usbPort  = "/dev/cu.usbmodem9002"
	imageOff = 0x10000
)

type rig struct {
	a     *Admin
	h     http.Handler
	fl    *fakeFlasher
	links []link.LinkInfo
	image []byte
	dir   string
	rel   *updatetest.Server
	lmu   sync.Mutex
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// publish puts release 0.2.0 on the fake release server: one board
// (t4s3-panel, an ESP32-S3 with 16 MB, flashed at 0x10000) and one that
// needs a bigger flash.
func publish(s *updatetest.Server) []byte {
	img := bytes.Repeat([]byte{0xe9, 0x01}, 2048)
	big := bytes.Repeat([]byte{0xe8}, 1024)
	files := map[string][]byte{
		"agentctl_0.2.0_darwin_arm64.tar.gz": []byte("cli"),
		"agents-deck-t4s3-0.2.0.bin":         img,
		"agents-deck-big-0.2.0.bin":          big,
	}
	m, _ := json.Marshal(map[string]any{"version": "0.2.0", "boards": []map[string]any{
		{"board": "t4s3", "name": "LilyGO T4-S3", "chipFamily": "ESP32-S3", "file": "agents-deck-t4s3-0.2.0.bin",
			"offset": "0x10000", "size": len(img), "sha256": sum(img), "version": "0.2.0", "types": []string{"t4s3-panel"},
			"flash_size": 16 << 20, "wifi": true},
		{"board": "big", "name": "A big board", "chipFamily": "ESP32-S3", "file": "agents-deck-big-0.2.0.bin",
			"offset": 0, "size": len(big), "sha256": sum(big), "version": "0.2.0", "types": []string{"big"},
			"flash_size": 32 << 20, "wifi": false},
	}})
	files[firmware.ManifestFile] = m
	s.Add("0.2.0", files)
	return img
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{dir: dir, fl: &fakeFlasher{chip: flash.Chip{Name: "ESP32-S3", MAC: "02:00:00:00:00:01", FlashSize: 16 << 20}}}
	r.rel = updatetest.New(t)
	r.image = publish(r.rel)
	st := store.New("", "test-host", "0.1.0")
	reg := &devices.Registry{Path: filepath.Join(dir, "devices.json")}
	r.a = &Admin{
		Version: "0.1.0", Platform: "darwin/arm64", Install: "app", Started: time.Now().Add(-time.Hour),
		Store: st, Devices: reg, Config: filepath.Join(dir, "config.json"), DataDir: dir, LogFile: filepath.Join(dir, "agentd.log"),
		ListPorts: func() ([]serial.Info, error) {
			return []serial.Info{
				{Path: "/dev/cu.Bluetooth-Incoming-Port"},
				{Path: newPort, VID: "303a", PID: "1001", Likely: "esp32-usb"},
				{Path: usbPort, VID: "1a86", PID: "55d3", Likely: "ch340"},
			}, nil
		},
		Flasher:  r.fl,
		Firmware: &firmware.Client{Releases: r.rel.Releases(filepath.Join(dir, "releases"))},
		Updater:  &fakeUpdater{st: update.State{State: update.StateNone, Current: "0.1.0", Install: "app"}},
	}
	r.a.Links = func() []link.LinkInfo {
		r.lmu.Lock()
		defer r.lmu.Unlock()
		return append([]link.LinkInfo(nil), r.links...)
	}
	// The Deck Link manager: it keeps a session (and so a status) on every
	// registered port it is allowed to open.
	r.a.PortStatus = func() []link.PortStatus {
		var out []link.PortStatus
		for _, p := range r.a.LinkPorts(r.a.registeredPorts()) {
			out = append(out, link.PortStatus{Path: p.Path, Baud: 115200, State: "waiting for the board"})
		}
		return out
	}
	r.fl.heldNow = func() bool { return r.a.held(newPort) || r.a.held(usbPort) }
	r.h = r.a.Handler()
	return r
}

func (r *rig) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (r *rig) waitJob(t *testing.T, id string) Job {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if j, ok := r.a.jobs.get(id); ok && j.State != JobRunning {
			return j
		}
	}
	t.Fatalf("job %s still running", id)
	return Job{}
}

// ---- system and settings --------------------------------------------------------------------

func TestSystemShape(t *testing.T) {
	r := newRig(t)
	code, out := r.do(t, "GET", "/admin/v1/system", "")
	if code != 200 {
		t.Fatalf("system: %d", code)
	}
	// Exactly the keys the tray app reads (the contract).
	want := map[string][]string{"": {"version", "platform", "install", "started", "boards", "ports", "update", "firmware_updates"},
		"boards": {"online", "total", "pending"}, "ports": {"new"}, "update": {"state", "version"}}
	for obj, keys := range want {
		m := out
		if obj != "" {
			m = out[obj].(map[string]any)
		}
		if len(m) != len(keys) {
			t.Errorf("%q has keys %v, want %v", obj, m, keys)
		}
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				t.Errorf("%q has no %q", obj, k)
			}
		}
	}
	if out["ports"].(map[string]any)["new"].(float64) != 2 {
		t.Errorf("two likely boards are new: %v", out["ports"])
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	r := newRig(t)
	login := &fakeLogin{}
	r.a.Login = login
	r.a.NetworkOn = true
	var gotWifi []string
	r.a.WifiSSID = func() string { return "Lab" }
	r.a.WifiDefault = func(ssid, pw string, forget bool) error { gotWifi = append(gotWifi, ssid+"/"+pw); return nil }
	os.WriteFile(r.a.Config, []byte(`{"term":"tmux","link":{"ports":[]}}`), 0o600)

	code, out := r.do(t, "PATCH", "/admin/v1/settings",
		`{"notifications":"finished","sound":true,"restore":"auto","sessions":"auto","updates":"off","pets":true,"network":false,"wifi":{"ssid":"Lab","password":"correct horse"}}`)
	if code != 200 {
		t.Fatalf("patch: %d %v", code, out)
	}
	if out["restartNeeded"] != true {
		t.Errorf("turning the network off needs a restart: %v", out)
	}
	_, got := r.do(t, "GET", "/admin/v1/settings", "")
	for k, v := range map[string]any{"notifications": "finished", "sound": true, "restore": "auto", "sessions": "auto", "updates": "off", "pets": true, "network": false} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "correct horse") {
		t.Error("the Wi-Fi password came back")
	}
	if len(gotWifi) != 1 || gotWifi[0] != "Lab/correct horse" {
		t.Errorf("default Wi-Fi: %v", gotWifi)
	}
	cfg, _ := os.ReadFile(r.a.Config)
	if !strings.Contains(string(cfg), `"term": "tmux"`) || strings.Contains(string(cfg), "correct horse") {
		t.Errorf("config.json lost a key or holds the password:\n%s", cfg)
	}
	for _, bad := range []string{`{"notifications":"loud"}`, `{"updates":"weekly"}`, `{"restore":"never"}`, `{"sessions":"all"}`, `{"wifi":{"ssid":"x","password":"short"}}`, `nope`,
		`{"petFirstLevel":10}`, `{"petStep":1}`, `{"petStep":4.5}`} {
		if code, _ := r.do(t, "PATCH", "/admin/v1/settings", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	// Agent pets: on by default, with its levels, kept as one object.
	if got["petFirstLevel"] != 50000.0 || got["petStep"] != 1.25 {
		t.Errorf("pet levels: %v %v", got["petFirstLevel"], got["petStep"])
	}
	if code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"pets":false,"petFirstLevel":20000,"petStep":1.5}`); code != 200 {
		t.Fatalf("pet levels: %d %v", code, out)
	}
	_, got = r.do(t, "GET", "/admin/v1/settings", "")
	// Agent pets can't be turned off for now: "off" is kept in config.json,
	// but the effective value is on (pets.AlwaysOn).
	if got["pets"] != pets.AlwaysOn || got["petFirstLevel"] != 20000.0 || got["petStep"] != 1.5 {
		t.Errorf("pets after the patch: %v %v %v", got["pets"], got["petFirstLevel"], got["petStep"])
	}
	if cfg, _ := os.ReadFile(r.a.Config); !strings.Contains(string(cfg), `"firstLevel": 20000`) || !strings.Contains(string(cfg), `"on": false`) {
		t.Errorf("config.json pets:\n%s", cfg)
	}
	os.WriteFile(r.a.Config, []byte(`{"term":"tmux"}`), 0o600)
	if _, got = r.do(t, "GET", "/admin/v1/settings", ""); got["pets"] != true {
		t.Error("no pets key: on by default")
	}
	// Start at login: registered after the answer went out.
	if code, _ := r.do(t, "PATCH", "/admin/v1/settings", `{"startAtLogin":true}`); code != 200 {
		t.Fatalf("start at login: %d", code)
	}
	for deadline := time.Now().Add(3 * time.Second); login.Registration().Registered == false && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if !login.Registration().Registered {
		t.Error("start at login didn't register the service")
	}
}

// ---- ports -------------------------------------------------------------------------------------

func portState(t *testing.T, r *rig, id string) map[string]any {
	t.Helper()
	_, out := r.do(t, "GET", "/admin/v1/ports", "")
	for _, p := range out["ports"].([]any) {
		if m := p.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

func TestPortsListedWithoutOpening(t *testing.T) {
	r := newRig(t)
	_, out := r.do(t, "GET", "/admin/v1/ports", "")
	ports := out["ports"].([]any)
	if len(ports) != 2 {
		t.Fatalf("the Bluetooth port isn't USB and isn't listed: %v", ports)
	}
	if s := portState(t, r, "cu.usbmodem9001")["state"]; s != PortNew {
		t.Errorf("state %v, want new", s)
	}
	for _, action := range []string{"identify", "flash"} {
		if code, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/"+action, `{"board":"t4s3"}`); code != 403 {
			t.Errorf("%s before register: %d %v", action, code, out)
		}
	}
	if r.fl.probes+r.fl.writes != 0 {
		t.Fatal("a port was opened before it was registered")
	}
	// Not mine: ignored, not new any more.
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9002/forget", "")
	if s := portState(t, r, "cu.usbmodem9002")["state"]; s != PortIgnored {
		t.Errorf("after forget: %v", s)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", ""); code != 200 {
		t.Fatal("register")
	}
	if s := portState(t, r, "cu.usbmodem9001")["state"]; s != PortRegistered {
		t.Errorf("after register: %v", s)
	}
	if ps := r.a.registeredPorts(); len(ps) != 1 || ps[0].Path != newPort {
		t.Errorf("config.json link.ports: %v", ps)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/nope/register", ""); code != 404 {
		t.Errorf("unknown port: %d", code)
	}
}

func TestIdentifyProbe(t *testing.T) {
	r := newRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	code, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/identify", "")
	if code != 200 {
		t.Fatalf("identify: %d %v", code, out)
	}
	if out["source"] != "probe" || out["chip"] != "ESP32-S3" {
		t.Errorf("identify: %v", out)
	}
	c := out["candidates"].([]any)
	if len(c) != 1 || c[0].(map[string]any)["board"] != "t4s3" {
		t.Errorf("candidates for 16 MB: %v (the 32 MB board doesn't fit)", c)
	}
	if len(r.fl.held) != 1 || !r.fl.held[0] {
		t.Error("the probe ran while the Deck Link manager still had the port")
	}
	if r.a.held(newPort) {
		t.Error("the port wasn't handed back to the Deck Link manager")
	}
	// A board that doesn't answer: the BOOT/RST hint comes through.
	r.fl.err = flash.ErrNoBootloader
	code, out = r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/identify", "")
	if code != 502 || out["bootHelp"] != true || !strings.Contains(out["error"].(string), "BOOT") {
		t.Errorf("no bootloader: %d %v", code, out)
	}
}

func TestIdentifySession(t *testing.T) {
	r := newRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:ff", "t4s3-panel", "0.1.0")
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverSerial, Where: newPort, Approved: true}}
	code, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/identify", "")
	if code != 200 || out["source"] != "session" {
		t.Fatalf("identify: %d %v", code, out)
	}
	if b, _ := out["board"].(map[string]any); b == nil || b["board"] != "t4s3" {
		t.Errorf("the board's type maps to the catalog: %v", out["board"])
	}
	if r.fl.probes != 0 {
		t.Error("a live session answers; the bootloader isn't asked")
	}
}

func TestFlash(t *testing.T) {
	r := newRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	code, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`)
	if code != 202 {
		t.Fatalf("flash: %d %v", code, out)
	}
	j := r.waitJob(t, out["id"].(string))
	if j.State != JobDone || j.Percent != 100 {
		t.Fatalf("job: %+v", j)
	}
	if !bytes.Equal(r.fl.image, r.image) || r.fl.offset != imageOff {
		t.Errorf("wrote %d bytes at %#x, want the release image at %#x", len(r.fl.image), r.fl.offset, imageOff)
	}
	if !r.fl.held[0] || r.a.held(newPort) {
		t.Error("the port must be borrowed while writing and handed back after")
	}
	if code, _ := r.do(t, "GET", "/admin/v1/jobs/"+j.ID, ""); code != 200 {
		t.Error("GET job")
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"nope"}`); code != 404 {
		t.Errorf("unknown board: %d", code)
	}
}

func TestFlashOneAtATimeAndCancel(t *testing.T) {
	r := newRig(t)
	r.fl.block = true
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	_, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`)
	id := out["id"].(string)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if j, _ := r.a.jobs.get(id); j.Stage == "write" {
			break
		}
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`); code != 409 {
		t.Errorf("a second flash on the same port: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/identify", ""); code != 409 {
		t.Errorf("identify while writing: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/jobs/"+id+"/cancel", ""); code != 202 {
		t.Errorf("cancel: %d", code)
	}
	if j := r.waitJob(t, id); j.State != JobCancelled {
		t.Errorf("after cancel: %+v", j)
	}
	if r.a.held(newPort) {
		t.Error("a cancelled flash hands the port back")
	}
}

func TestFlashBootHelp(t *testing.T) {
	r := newRig(t)
	r.fl.err = flash.ErrNoBootloader
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	_, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`)
	j := r.waitJob(t, out["id"].(string))
	if j.State != JobFailed || !j.BootHelp || !strings.Contains(j.Error, "hold BOOT") {
		t.Errorf("job: %+v", j)
	}
}

func TestNoReleaseKeyNoFlash(t *testing.T) {
	r := newRig(t)
	rel := r.rel.Releases(filepath.Join(r.dir, "nokey"))
	rel.Key = nil
	r.a.Firmware = &firmware.Client{Releases: rel}
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	code, out := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`)
	if code != 409 || !strings.Contains(out["error"].(string), "built from source") || !strings.Contains(out["error"].(string), "firmware file") {
		t.Errorf("no key: %d %v", code, out)
	}
	_, fw := r.do(t, "GET", "/admin/v1/firmware", "")
	if fw["releaseKey"] != false {
		t.Errorf("catalog says releaseKey %v", fw["releaseKey"])
	}
	if r.fl.writes != 0 {
		t.Error("wrote without a release key")
	}
}

// ---- devices ------------------------------------------------------------------------------------

func TestDevices(t *testing.T) {
	r := newRig(t)
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:01", "t4s3-panel", "0.1.0")
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9002/register", "")
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverSerial, Where: usbPort, Approved: true}}
	r.a.loadCatalog(context.Background())
	_, sys := r.do(t, "GET", "/admin/v1/system", "")
	if sys["boards"].(map[string]any)["pending"].(float64) != 1 {
		t.Errorf("a new board waits for approval: %v", sys["boards"])
	}
	for _, a := range []struct{ action, body string }{
		{"approve", ""}, {"scope", `{"scope":"admin"}`}, {"rename", `{"name":"Tall one"}`},
	} {
		if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/"+a.action, a.body); code != 204 {
			t.Errorf("%s: %d %v", a.action, code, out)
		}
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/rename", `{"name":""}`); code != 400 {
		t.Error("an empty name")
	}
	_, out := r.do(t, "GET", "/admin/v1/devices", "")
	row := out["devices"].([]any)[0].(map[string]any)
	if row["name"] != "Tall one" || row["id"] != d.Name || row["scope"] != "admin" || row["state"] != "active" || row["connection"] != "usb" {
		t.Errorf("row: %v", row)
	}
	upd, _ := row["update"].(map[string]any)
	if upd == nil || upd["version"] != "0.2.0" || upd["how"] != "usb" || upd["ready"] != true {
		t.Fatalf("update: %v", row["update"])
	}
	// On USB: the flasher, through its port.
	code, job := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", "")
	if code != 202 {
		t.Fatalf("update over USB: %d %v", code, job)
	}
	if j := r.waitJob(t, job["id"].(string)); j.State != JobDone || j.Kind != "board-update" {
		t.Errorf("job: %+v", j)
	}
	// On Wi-Fi without over-the-air in this build: a plain answer.
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverWifi, Where: "192.0.2.7", Approved: true}}
	code, out = r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", "")
	if code != 501 || out["error"] != ErrNoOTA.Error() {
		t.Errorf("Wi-Fi without OTA: %d %v", code, out)
	}
	if !strings.Contains(ErrNoOTA.Error(), "over-the-air update is not available in this build") {
		t.Error("the wording the integrator looks for")
	}
	// Not connected.
	r.links = nil
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", ""); code != 409 {
		t.Errorf("unplugged: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/revoke", ""); code != 204 {
		t.Error("revoke")
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/nobody/approve", ""); code != 404 {
		t.Error("unknown device")
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/explode", ""); code != 404 {
		t.Error("unknown action")
	}
}

func TestDeviceUpdateOverTheAir(t *testing.T) {
	r := newRig(t)
	ota := &fakeOTA{a: r.a, end: link.FirmwareDone}
	r.a.OTA = ota
	// The release's board needs an over-the-air image for this.
	img := bytes.Repeat([]byte{1}, 100)
	app := bytes.Repeat([]byte{2}, 50)
	m, _ := json.Marshal(map[string]any{"version": "0.3.0", "boards": []map[string]any{
		{"board": "t4s3", "name": "LilyGO T4-S3", "chipFamily": "ESP32-S3", "file": "a.bin", "offset": 0, "size": len(img), "sha256": sum(img),
			"version": "0.3.0", "types": []string{"t4s3-panel"}, "wifi": true, "ota": map[string]any{"file": "a.app.bin", "size": len(app), "sha256": sum(app)}},
	}})
	r.rel.Add("0.3.0", map[string][]byte{"agentctl_0.3.0_darwin_arm64.tar.gz": []byte("x"), "a.bin": img, "a.app.bin": app, firmware.ManifestFile: m})
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:02", "t4s3-panel", "0.2.0")
	r.a.Devices.Approve(d.Name)
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverWifi, Where: "192.0.2.8", Approved: true}}
	code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", "")
	if code != 202 {
		t.Fatalf("OTA: %d %v", code, out)
	}
	if j := r.waitJob(t, out["id"].(string)); j.State != JobDone || j.Percent != 100 {
		t.Errorf("job: %+v", j)
	}
	if len(ota.offered) != 1 || ota.offered[0] != d.Name+" t4s3 0.3.0" {
		t.Errorf("offered: %v", ota.offered)
	}
	// A board that goes back to its old firmware: the job fails in words.
	ota.end = link.FirmwareRolledBack
	_, out = r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", "")
	if j := r.waitJob(t, out["id"].(string)); j.State != JobFailed || !strings.Contains(j.Error, "went back") {
		t.Errorf("rolled back: %+v", j)
	}
}

func TestDeviceWifi(t *testing.T) {
	r := newRig(t)
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:03", "t4s3-panel", "0.2.0")
	var sent []string
	r.a.SendWifi = func(board, ssid, pw string, forget bool) (int, string) {
		sent = append(sent, board+" "+ssid)
		return 200, "ok"
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/wifi", `{"ssid":"Lab","password":"12"}`); code != 400 {
		t.Error("a short password is refused before anything is sent")
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/wifi", `{"ssid":"Lab","password":"long enough"}`); code != 200 {
		t.Errorf("wifi: %d %v", code, out)
	}
	if len(sent) != 1 {
		t.Errorf("sent: %v", sent)
	}
	if got, _ := r.a.Devices.Get(d.Name); !got.WifiOwn {
		t.Error("a board given its own network keeps it")
	}
}

// ---- updates --------------------------------------------------------------------------------

func TestUpdateApplyAndLoop(t *testing.T) {
	r := newRig(t)
	u := r.a.Updater.(*fakeUpdater)
	code, out := r.do(t, "POST", "/admin/v1/update/check", "")
	if code != 200 || out["version"] != "0.3.0" {
		t.Fatalf("check: %d %v", code, out)
	}
	code, out = r.do(t, "POST", "/admin/v1/update/apply", "")
	if code != 202 {
		t.Fatalf("apply: %d %v", code, out)
	}
	if j := r.waitJob(t, out["id"].(string)); j.State != JobDone {
		t.Errorf("apply job: %+v", j)
	}
	// The check above may still be downloading in the background: read under the lock.
	u.mu.Lock()
	downloads, applies := u.downloads, u.apply
	u.mu.Unlock()
	if downloads < 1 || applies != 1 {
		t.Errorf("downloads %d, applies %d", downloads, applies)
	}

	// The timer: "off" never checks; "auto" checks, downloads and installs.
	r2 := newRig(t)
	u2 := r2.a.Updater.(*fakeUpdater)
	os.WriteFile(r2.a.Config, []byte(`{"updates":"off"}`), 0o600)
	r2.a.FirstCheck, r2.a.CheckEvery = 5*time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go r2.a.RunUpdates(ctx)
	time.Sleep(60 * time.Millisecond)
	u2.mu.Lock()
	checks := u2.checks
	u2.mu.Unlock()
	if checks != 0 {
		t.Errorf("updates off, still checked %d times", checks)
	}
	os.WriteFile(r2.a.Config, []byte(`{"updates":"auto"}`), 0o600)
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		u2.mu.Lock()
		done := u2.apply > 0
		u2.mu.Unlock()
		if done {
			break
		}
	}
	cancel()
	u2.mu.Lock()
	defer u2.mu.Unlock()
	if u2.apply == 0 {
		t.Error("updates auto didn't install")
	}
}

func TestUpdateRefusedWithoutReleaseKey(t *testing.T) {
	r := newRig(t)
	rel := r.rel.Releases(filepath.Join(r.dir, "nokey2"))
	rel.Key = nil
	up := &update.Updater{Current: "0.1.0", Kind: update.KindArchive, GOOS: "darwin", GOARCH: "arm64", Releases: rel}
	r.a.Updater = up
	if code, _ := r.do(t, "POST", "/admin/v1/update/check", ""); code != 200 {
		t.Fatal("check")
	}
	_, st := r.do(t, "GET", "/admin/v1/update", "")
	if st["state"] != update.StateDisabled || !strings.Contains(st["message"].(string), "release key") {
		t.Errorf("state: %v", st)
	}
	if code, out := r.do(t, "POST", "/admin/v1/update/apply", ""); code != 409 {
		t.Errorf("apply without a key: %d %v", code, out)
	}
}

// ---- diagnostics ------------------------------------------------------------------------------

func TestDiagnosticsLeaveOutNames(t *testing.T) {
	r := newRig(t)
	home, _ := os.UserHomeDir()
	r.a.Store.Add(&store.Entry{A: model.Agent{ID: "a1", Title: "payments-refactor", Tool: "claude", Status: model.Idle,
		Cwd: "/Users/alice/dev/acme-secret", Folder: "acme-secret", Branch: "fix-billing", LastPrompt: "rotate the stripe keys"}})
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:04", "t4s3-panel", "0.2.0")
	r.a.setLabel(d.Name, "Kitchen panel")
	r.a.Devices.AddNetwork("alices-phone", devices.View, "01", "ff")
	r.a.WifiSSID = func() string { return "AcmeGuest" }
	r.a.loadCatalog(context.Background())
	log := strings.Join([]string{
		`2026/11/10 10:42:07 board "Kitchen panel" connected over Wi-Fi from 192.0.2.40`,
		`2026/11/10 10:42:08 agent payments-refactor is waiting for an answer`,
		`2026/11/10 10:42:09 resumed in /Users/alice/dev/acme-secret on fix-billing`,
		`2026/11/10 10:42:10 hooks written to ` + home + `/.claude/settings.json`,
		`2026/11/10 10:42:11 paired alices-phone (view) from fe80::1c2d:3e4f:5a6b:7c8d`,
		`2026/11/10 10:42:12 default network AcmeGuest: ok`,
		`2026/11/10 10:42:13 prompt: rotate the stripe keys`,
		`2026/11/10 10:42:14 mac 02:00:00:00:00:99, mail someone@example.com`,
		`2026/11/10 10:42:15 web: GET /admin/v1/system 200`,
		`2026/11/10 10:42:16 C:\Users\alice\projects\acme-secret\notes.txt`,
	}, "\n")
	os.WriteFile(r.a.LogFile, []byte(log+"\n"), 0o600)
	_, out := r.do(t, "GET", "/admin/v1/diagnostics", "")
	report := out["report"].(string)
	logs, _ := json.Marshal(out["log"])
	for _, secret := range []string{"payments-refactor", "acme-secret", "alice", "Kitchen panel", "fix-billing", "stripe", "192.0.2.40",
		"fe80", "AcmeGuest", home, "02:00:00:00:00:99", "someone@example.com", d.Name, "test-host"} {
		if secret == "" {
			continue
		}
		if strings.Contains(strings.ToLower(report), strings.ToLower(secret)) || strings.Contains(strings.ToLower(string(logs)), strings.ToLower(secret)) {
			t.Errorf("the report holds %q:\n%s", secret, report)
		}
	}
	for _, want := range []string{"version:   0.1.0", "darwin/arm64", "LilyGO T4-S3", "/admin/v1/system", "2026/11/10 10:42:15"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lost %q:\n%s", want, report)
		}
	}
}

func TestUnknownCall(t *testing.T) {
	r := newRig(t)
	if code, out := r.do(t, "GET", "/admin/v1/nope", ""); code != 404 || out["error"] == nil {
		t.Errorf("%d %v", code, out)
	}
}

func TestJobsEventsStream(t *testing.T) {
	r := newRig(t)
	srv := httptest.NewServer(r.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/admin/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/flash", `{"board":"t4s3"}`)
	buf := make([]byte, 64<<10)
	var got strings.Builder
	for !strings.Contains(got.String(), `"state":"done"`) {
		n, err := resp.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("stream ended: %v\n%s", err, got.String())
		}
	}
	if !strings.Contains(got.String(), "event: system") || !strings.Contains(got.String(), "event: job") {
		t.Errorf("stream:\n%s", got.String())
	}
}

func TestOTASourceBoardTypes(t *testing.T) {
	r := newRig(t)
	src := OTASource{Client: r.a.Firmware.(*firmware.Client)}
	types, err := src.BoardTypes(context.Background(), "t4s3")
	if err != nil || len(types) != 1 || types[0] != "t4s3-panel" {
		t.Errorf("types %v, %v", types, err)
	}
	if _, err := src.BoardTypes(context.Background(), "nope"); err == nil {
		t.Error("an unknown board")
	}
}

func TestTrayAutostartFollowsStartAtLogin(t *testing.T) {
	r := newRig(t)
	r.a.Login = &fakeLogin{}
	home := t.TempDir()
	src := filepath.Join(home, "launcher.desktop")
	os.WriteFile(src, []byte("[Desktop Entry]\nType=Application\nName=agents deck\nExec=/opt/agents-deck/agents-deck\n"), 0o644)
	r.a.Tray = &TrayAutostart{Home: home, Sources: []string{src}}
	r.do(t, "PATCH", "/admin/v1/settings", `{"startAtLogin":true}`)
	b, err := os.ReadFile(filepath.Join(home, ".config", "autostart", trayDesktop))
	if err != nil || !strings.Contains(string(b), "Exec=/opt/agents-deck/agents-deck --background\n") || !strings.Contains(string(b), "X-GNOME-Autostart-enabled=true") {
		t.Fatalf("autostart entry: %v\n%s", err, b)
	}
	if _, s := r.do(t, "GET", "/admin/v1/settings", ""); s["trayAtLogin"] != true {
		t.Errorf("trayAtLogin: %v", s["trayAtLogin"])
	}
	r.do(t, "PATCH", "/admin/v1/settings", `{"startAtLogin":false}`)
	if _, err := os.Stat(filepath.Join(home, ".config", "autostart", trayDesktop)); err == nil {
		t.Error("turning it off leaves the tray app's entry")
	}
	r.a.Tray = &TrayAutostart{Home: home, Sources: []string{filepath.Join(home, "none")}}
	if _, s := r.do(t, "GET", "/admin/v1/settings", ""); s["trayAtLogin"] != nil {
		t.Error("no tray app installed: no trayAtLogin")
	}
}

// agentctl flash borrows a registered port: the Deck Link manager closes it
// until it is returned (or the time runs out), with no polling for it.
func TestPortBorrow(t *testing.T) {
	r := newRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/register", "")
	open := func() bool {
		for _, st := range r.a.PortStatus() {
			if st.Path == newPort {
				return true
			}
		}
		return false
	}
	if !open() {
		t.Fatal("the registered port isn't open")
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/borrow", `{"seconds": 60}`); code != 204 {
		t.Fatalf("borrow: %d", code)
	}
	if open() {
		t.Fatal("still open after borrow")
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/borrow", ""); code != 409 {
		t.Errorf("borrowed twice: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/return", ""); code != 204 || !open() {
		t.Fatalf("return: %d, open %v", code, open())
	}
	// A borrow runs out by itself.
	if err := r.a.lend(context.Background(), newPort, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !open(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a borrow that ran out wasn't handed back")
		}
	}
	// Return never ends a hold the service took for its own install.
	if err := r.a.release(context.Background(), newPort); err != nil {
		t.Fatal(err)
	}
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9001/return", "")
	if open() {
		t.Error("return handed back the service's own hold")
	}
}

// "Report usage from every Claude session": off by default, kept in
// config.json, and each change goes to the daemon; a service without it says so.
func TestSettingsUsageEverySession(t *testing.T) {
	r := newRig(t)
	_, got := r.do(t, "GET", "/admin/v1/settings", "")
	if got["usageEverySession"] != false {
		t.Errorf("default: %v", got["usageEverySession"])
	}
	if code, _ := r.do(t, "PATCH", "/admin/v1/settings", `{"usageEverySession":true}`); code != 501 {
		t.Errorf("without the hook: %d, want 501", code)
	}
	var calls []bool
	r.a.UsageEverySession = func(on bool) error { calls = append(calls, on); return nil }
	if code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"usageEverySession":true}`); code != 200 {
		t.Fatalf("on: %d %v", code, out)
	}
	if _, got = r.do(t, "GET", "/admin/v1/settings", ""); got["usageEverySession"] != true {
		t.Errorf("after on: %v", got["usageEverySession"])
	}
	r.a.UsageEverySession = func(on bool) error {
		calls = append(calls, on)
		return errors.New("settings.json: not a JSON object; left alone")
	}
	code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"usageEverySession":false}`)
	if code != 200 || !strings.Contains(fmt.Sprint(out["message"]), "left alone") {
		t.Errorf("off with an error: %d %v", code, out)
	}
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Errorf("calls %v", calls)
	}
}
