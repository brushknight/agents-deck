package admin

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// fakeLocal is the hub's local images: it records what it was given.
type fakeLocal struct {
	mu    sync.Mutex
	added []string
	data  []byte
}

func (l *fakeLocal) AddLocalImage(board, version, path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.added = append(l.added, board+" "+version)
	l.data = b
	return int64(len(b)), nil
}

// espApp is a made-up ESP32 app image for chip (9: ESP32-S3) with version
// in its app description.
func espApp(chip uint16, version string) []byte {
	b := make([]byte, 8192)
	b[0], b[1] = 0xE9, 1
	binary.LittleEndian.PutUint16(b[12:14], chip)
	binary.LittleEndian.PutUint32(b[32:], 0xABCD5432)
	copy(b[48:80], version)
	copy(b[80:112], "agents-deck")
	return b
}

// espFull is a full image: bootloader, partition table, app at 0x10000.
func espFull(chip uint16, app []byte) []byte {
	b := make([]byte, 0x10000+len(app))
	b[0], b[1] = 0xE9, 3
	binary.LittleEndian.PutUint16(b[12:14], chip)
	e := b[0x8000:]
	binary.LittleEndian.PutUint16(e[0:2], 0x50AA)
	e[2] = 0 // app
	binary.LittleEndian.PutUint32(e[4:8], 0x10000)
	copy(b[0x10000:], app)
	return b
}

func (r *rig) upload(t *testing.T, dev string, body io.Reader, size int64) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/admin/v1/devices/"+dev+"/firmware-file", body)
	req.ContentLength = size
	req.Header.Set("X-File-Name", "../../firmware.bin")
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func fileRig(t *testing.T) (*rig, *fakeOTA, *fakeLocal, string) {
	r := newRig(t)
	ota := &fakeOTA{a: r.a, end: link.FirmwareDone}
	loc := &fakeLocal{}
	r.a.OTA, r.a.Local = ota, loc
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:09", "t4s3-panel", "0.3.0-dev.4")
	r.a.Devices.Approve(d.Name)
	if _, err := r.a.loadCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r, ota, loc, d.Name
}

func TestInstallFileOverWifi(t *testing.T) {
	r, ota, loc, dev := fileRig(t)
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverWifi, Where: "192.0.2.9", Approved: true}}
	_, list := r.do(t, "GET", "/admin/v1/devices", "")
	row := list["devices"].([]any)[0].(map[string]any)
	st := row["status"].(map[string]any)
	if st["state"] != FwNewer || !strings.Contains(st["text"].(string), "0.2.0") || row["fileVia"].([]any)[0] != "wifi" {
		t.Errorf("row: status %v fileVia %v", st, row["fileVia"])
	}

	img := espApp(9, "0.2.0-dev.5")
	code, up := r.upload(t, dev, bytes.NewReader(img), int64(len(img)))
	if code != 200 || up["via"] != "wifi" || up["kind"] != "app" || up["version"] != "0.2.0-dev.5" || up["chip"] != "ESP32-S3" ||
		up["chipChecked"] != true || up["board"] != "t4s3" || up["name"] != "firmware.bin" || up["size"].(float64) != float64(len(img)) {
		t.Fatalf("upload: %d %v", code, up)
	}
	if len(ota.offered) != 0 || len(loc.added) != 0 {
		t.Fatal("something was sent before the install was confirmed")
	}
	code, job := r.do(t, "POST", "/admin/v1/devices/"+dev+"/install-file", `{"file":"`+up["file"].(string)+`"}`)
	if code != 202 || job["file"] != "firmware.bin" {
		t.Fatalf("install: %d %v", code, job)
	}
	if j := r.waitJob(t, job["id"].(string)); j.State != JobDone || j.Version != "0.2.0-dev.5" {
		t.Errorf("job: %+v", j)
	}
	if len(loc.added) != 1 || loc.added[0] != "t4s3 0.2.0-dev.5" || !bytes.Equal(loc.data, img) {
		t.Errorf("local image: %v", loc.added)
	}
	if len(ota.offered) != 1 || ota.offered[0] != dev+" t4s3 0.2.0-dev.5" {
		t.Errorf("offered: %v", ota.offered)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+dev+"/install-file", `{"file":"nope"}`); code != 404 {
		t.Errorf("an unknown file: %d", code)
	}

	// An image whose app description holds no version: the person gives it.
	img = espApp(9, "esp-idf v5.3")
	_, up = r.upload(t, dev, bytes.NewReader(img), int64(len(img)))
	if up["version"] != nil {
		t.Errorf("version read from a non-version: %v", up["version"])
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+dev+"/install-file", `{"file":"`+up["file"].(string)+`"}`); code != 400 || !strings.Contains(out["error"].(string), "version") {
		t.Errorf("no version: %d %v", code, out)
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+dev+"/install-file", `{"file":"`+up["file"].(string)+`","version":"../x"}`); code != 400 {
		t.Errorf("a version that isn't one: %d %v", code, out)
	}
}

func TestInstallFileRefusesWrongFiles(t *testing.T) {
	r, ota, loc, dev := fileRig(t)
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverWifi, Where: "192.0.2.9", Approved: true}}
	for name, tc := range map[string]struct {
		body []byte
		code int
		want string
	}{
		"not an image":   {[]byte(strings.Repeat("not firmware ", 1000)), 400, "isn't an ESP32 firmware image"},
		"empty":          {nil, 400, "empty"},
		"another chip":   {espApp(5, "0.2.0"), 400, "this file is for an ESP32-C3, and"},
		"full over wifi": {espFull(9, espApp(9, "0.2.0")), 400, "full image, which is written over USB"},
	} {
		code, out := r.upload(t, dev, bytes.NewReader(tc.body), int64(len(tc.body)))
		if code != tc.code || !strings.Contains(out["error"].(string), tc.want) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	// Too large: said up front, or found while reading.
	if code, _ := r.upload(t, dev, bytes.NewReader(nil), firmware.MaxFileImage+1); code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large by Content-Length: %d", code)
	}
	big := io.MultiReader(bytes.NewReader(espApp(9, "0.2.0")), io.LimitReader(zeros{}, firmware.MaxFileImage))
	if code, _ := r.upload(t, dev, big, -1); code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large while reading: %d", code)
	}
	// An app image bigger than an app slot.
	slot := append(espApp(9, "0.2.0"), make([]byte, link.MaxAppImage)...)
	if code, out := r.upload(t, dev, bytes.NewReader(slot), int64(len(slot))); code != 400 || !strings.Contains(out["error"].(string), "app slot") {
		t.Errorf("bigger than a slot: %d %v", code, out)
	}
	// Not connected, or only on Bluetooth.
	r.links = nil
	img := espApp(9, "0.2.0")
	if code, out := r.upload(t, dev, bytes.NewReader(img), int64(len(img))); code != 409 || !strings.Contains(out["error"].(string), "connect it") {
		t.Errorf("unplugged: %d %v", code, out)
	}
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverBLE, Where: "bluetooth", Approved: true}}
	if code, out := r.upload(t, dev, bytes.NewReader(img), int64(len(img))); code != 409 || !strings.Contains(out["error"].(string), "Bluetooth") {
		t.Errorf("bluetooth: %d %v", code, out)
	}
	if len(ota.offered) != 0 || len(loc.added) != 0 || r.fl.writes != 0 {
		t.Error("a refused file reached the board")
	}
	if ents, _ := os.ReadDir(r.a.DataDir + "/firmware-files"); len(ents) != 0 {
		t.Errorf("refused files kept: %d", len(ents))
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestInstallFileOverUSB(t *testing.T) {
	r, ota, _, dev := fileRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9002/register", "")
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverSerial, Where: usbPort, Approved: true}}
	// No release key: a file picked here installs all the same.
	rel := r.rel.Releases(r.dir + "/nokey")
	rel.Key = nil
	r.a.Firmware = &firmware.Client{Releases: rel}
	// An app image needs Wi-Fi.
	app := espApp(9, "0.2.0-dev.5")
	if code, out := r.upload(t, dev, bytes.NewReader(app), int64(len(app))); code != 400 || !strings.Contains(out["error"].(string), "pick the full image") {
		t.Errorf("app over USB: %d %v", code, out)
	}
	full := espFull(9, app)
	code, up := r.upload(t, dev, bytes.NewReader(full), int64(len(full)))
	if code != 200 || up["via"] != "usb" || up["kind"] != "full" {
		t.Fatalf("upload: %d %v", code, up)
	}
	code, job := r.do(t, "POST", "/admin/v1/devices/"+dev+"/install-file", `{"file":"`+up["file"].(string)+`"}`)
	if code != 202 {
		t.Fatalf("install: %d %v", code, job)
	}
	j := r.waitJob(t, job["id"].(string))
	if j.State != JobDone || j.Port != "cu.usbmodem9002" || !strings.Contains(strings.Join(j.Steps, "|"), "not checked against a release") {
		t.Errorf("job: %+v", j)
	}
	if !bytes.Equal(r.fl.image, full) || r.fl.offset != 0 {
		t.Errorf("wrote %d bytes at %#x", len(r.fl.image), r.fl.offset)
	}
	if len(ota.offered) != 0 {
		t.Error("offered over the air")
	}
}
