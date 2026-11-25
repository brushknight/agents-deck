package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// aBuild is a build as the release script writes it for the rig's board
// (t4s3, Hello type t4s3-panel): its manifest and images.
type aBuild struct {
	manifest, full, app []byte
}

func makeBuild(board, version string, withApp bool) aBuild {
	base := firmware.BaseName(board, version)
	app := espApp(9, "v0.1.1-108-gd574bf3-dirty") // the build system's version, not the release's
	full := espFull(9, app)
	full[3] = 4 << 4 // 16 MB of flash
	ad := map[string]any{"board": board, "version": version, "chip": "esp32s3", "file": base + ".bin", "offset": "0x0",
		"size": len(full), "sha256": sum(full), "env": "idf5", "name": "LilyGO T4-S3", "types": []string{"t4s3-panel"},
		"flash_size": 16 << 20, "wifi": withApp, "ota": nil}
	b := aBuild{full: full}
	if withApp {
		ad["ota"] = map[string]any{"file": base + ".app.bin", "size": len(app), "sha256": sum(app)}
		b.app = app
	}
	b.manifest, _ = json.Marshal(map[string]any{"name": "LilyGO T4-S3", "version": version, "agents_deck": ad})
	return b
}

// addBuild sends a build as agentctl firmware add does.
func (r *rig) addBuild(t *testing.T, b aBuild) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	put := func(name string, data []byte) {
		if data != nil {
			w, _ := mw.CreateFormFile(name, name+".bin")
			w.Write(data)
		}
	}
	put("manifest", b.manifest)
	put("full", b.full)
	put("app", b.app)
	mw.Close()
	req := httptest.NewRequest("POST", "/admin/v1/firmware/builds", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (r *rig) noKey() {
	rel := r.rel.Releases(filepath.Join(r.dir, "nokey"))
	rel.Key = nil
	r.a.Firmware = &firmware.Client{Releases: rel}
	r.a.catalog = nil
}

// setFirmware: the rig's board says hello with another version.
func (r *rig) setFirmware(t *testing.T, version string) {
	t.Helper()
	if _, err := r.a.Devices.Board("aa:bb:cc:dd:ee:09", "t4s3-panel", version); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) row(t *testing.T) map[string]any {
	t.Helper()
	_, list := r.do(t, "GET", "/admin/v1/devices", "")
	return list["devices"].([]any)[0].(map[string]any)
}

func TestBuildsAddListRemove(t *testing.T) {
	r := newRig(t)
	if code, out := r.addBuild(t, makeBuild("t4s3", "0.3.0-dev.7", true)); code != 200 || out["version"] != "0.3.0-dev.7" || out["board"] != "t4s3" {
		t.Fatalf("add: %d %v", code, out)
	}
	code, out := r.do(t, "GET", "/admin/v1/firmware/builds", "")
	if code != 200 || len(out["builds"].([]any)) != 1 || out["dir"] != filepath.Join(r.dir, firmware.LocalDir) {
		t.Fatalf("list: %d %v", code, out)
	}
	// The same checks as a file picked in Settings, and the manifest's.
	bad := makeBuild("t4s3", "0.3.0-dev.8", true)
	bad.full, bad.app = bad.app, bad.full
	if code, out := r.addBuild(t, bad); code != 400 {
		t.Errorf("swapped images: %d %v", code, out)
	}
	bad = makeBuild("t4s3", "0.3.0-dev.8", true)
	bad.app = nil
	if code, out := r.addBuild(t, bad); code != 400 || !strings.Contains(out["error"].(string), "add it too") {
		t.Errorf("no app image: %d %v", code, out)
	}
	bad = makeBuild("t4s3", "0.3.0-dev.8", false)
	bad.full = espApp(9, "0.3.0")
	if code, _ := r.addBuild(t, bad); code != 400 {
		t.Errorf("an app image as the full one: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/firmware/builds", `{"manifest":"x"}`); code != 400 {
		t.Errorf("not multipart: %d", code)
	}
	// Removing names a board and a version, never a path.
	for _, p := range []string{"/admin/v1/firmware/builds/..%2F..%2Fx/all", "/admin/v1/firmware/builds/t4s3/..", "/admin/v1/firmware/builds/t4s3/9.9.9"} {
		if code, _ := r.do(t, "DELETE", p, ""); code < 300 { // 301: the mux cleans the path, nothing is removed
			t.Errorf("DELETE %s: %d", p, code)
		}
	}
	if code, out := r.do(t, "DELETE", "/admin/v1/firmware/builds/t4s3/0.3.0-dev.7", ""); code != 200 || len(out["removed"].([]any)) != 1 {
		t.Errorf("remove: %d %v", code, out)
	}
	if _, out := r.do(t, "GET", "/admin/v1/firmware/builds", ""); len(out["builds"].([]any)) != 0 {
		t.Errorf("after remove: %v", out)
	}
}

// The owner's case: a copy built from source (no release key), a board on
// the USB cable, a build made here that is newer than what it runs. One
// click installs it over the cable: the full image at 0x0.
func TestLocalBuildOverUSB(t *testing.T) {
	r, ota, _, dev := fileRig(t)
	r.noKey()
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9002/register", "")
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverSerial, Where: usbPort, Approved: true}}
	b := makeBuild("t4s3", "0.3.0-dev.7", true)
	if code, out := r.addBuild(t, b); code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	row := r.row(t)
	st := row["status"].(map[string]any)
	if st["state"] != FwLocal || st["source"] != "local" || st["latest"] != "0.3.0-dev.7" || st["how"] != "usb" || st["ready"] != true ||
		st["text"] != "0.3.0-dev.7 built on this computer" {
		t.Fatalf("status: %v", st)
	}
	if u := row["update"].(map[string]any); u["source"] != "local" || u["ready"] != true {
		t.Errorf("update: %v", u)
	}
	if s := r.a.SystemNow(); s.FirmwareUpdates != 1 {
		t.Errorf("firmware updates: %d", s.FirmwareUpdates)
	}
	code, job := r.do(t, "POST", "/admin/v1/devices/"+dev+"/update", `{}`)
	if code != 202 || job["source"] != "local" || job["port"] != "cu.usbmodem9002" || job["version"] != "0.3.0-dev.7" {
		t.Fatalf("install: %d %v", code, job)
	}
	j := r.waitJob(t, job["id"].(string))
	if j.State != JobDone || !strings.Contains(strings.Join(j.Steps, "|"), "built on this computer") {
		t.Errorf("job: %+v", j)
	}
	if !bytes.Equal(r.fl.image, b.full) || r.fl.offset != 0 {
		t.Errorf("wrote %d bytes at %#x, want the full image at 0x0", len(r.fl.image), r.fl.offset)
	}
	if len(ota.offered) != 0 {
		t.Error("offered over the air while on USB")
	}
	// The board now runs it: nothing more to install.
	r.setFirmware(t, "0.3.0-dev.7")
	if st := r.row(t)["status"].(map[string]any); st["state"] == FwLocal {
		t.Errorf("after the install: %v", st)
	}
}

// Off the cable, on Wi-Fi: the app image, offered over the air.
func TestLocalBuildOverWifi(t *testing.T) {
	r, ota, loc, dev := fileRig(t)
	r.noKey()
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverWifi, Where: "192.0.2.9", Approved: true}}
	b := makeBuild("t4s3", "0.3.0-dev.7", true)
	r.addBuild(t, b)
	if st := r.row(t)["status"].(map[string]any); st["state"] != FwLocal || st["how"] != "wifi" || st["ready"] != true {
		t.Fatalf("status: %v", st)
	}
	code, job := r.do(t, "POST", "/admin/v1/devices/"+dev+"/update", `{"source":"local"}`)
	if code != 202 || job["source"] != "local" || job["port"] != nil {
		t.Fatalf("install: %d %v", code, job)
	}
	if j := r.waitJob(t, job["id"].(string)); j.State != JobDone {
		t.Errorf("job: %+v", j)
	}
	if len(loc.added) != 1 || loc.added[0] != "t4s3 0.3.0-dev.7" || !bytes.Equal(loc.data, b.app) {
		t.Errorf("local image: %v", loc.added)
	}
	if len(ota.offered) != 1 || ota.offered[0] != dev+" t4s3 0.3.0-dev.7" {
		t.Errorf("offered: %v", ota.offered)
	}

	// A build without an app image can't go over Wi-Fi: the line says to plug it in.
	r.do(t, "DELETE", "/admin/v1/firmware/builds/t4s3/all", "")
	r.addBuild(t, makeBuild("t4s3", "0.3.0-dev.8", false))
	st := r.row(t)["status"].(map[string]any)
	if st["state"] != FwLocal || st["ready"] == true || !strings.Contains(st["note"].(string), "plug it in") {
		t.Errorf("no app image: %v", st)
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+dev+"/update", `{"source":"local"}`); code != 409 || !strings.Contains(out["error"].(string), "USB cable") {
		t.Errorf("install without an app image: %d %v", code, out)
	}
}

// A file changed in the folder after it was added is refused at install.
func TestLocalBuildCheckedAgain(t *testing.T) {
	r, _, _, dev := fileRig(t)
	r.do(t, "POST", "/admin/v1/ports/cu.usbmodem9002/register", "")
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverSerial, Where: usbPort, Approved: true}}
	b := makeBuild("t4s3", "0.3.0-dev.7", true)
	r.addBuild(t, b)
	p := filepath.Join(r.dir, firmware.LocalDir, firmware.BaseName("t4s3", "0.3.0-dev.7")+".bin")
	changed := append([]byte(nil), b.full...)
	changed[0x10000+200] ^= 0xff
	if err := os.WriteFile(p, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+dev+"/update", `{"source":"local"}`); code != 409 || !strings.Contains(out["error"].(string), "SHA-256") {
		t.Errorf("a changed image: %d %v", code, out)
	}
	if r.fl.writes != 0 {
		t.Error("written")
	}
}

// Release and build: the newer one is offered, and says where it comes from.
func TestLocalBuildAgainstRelease(t *testing.T) {
	r, _, _, dev := fileRig(t) // release 0.2.0 with a key; the board runs 0.3.0-dev.4
	r.links = []link.LinkInfo{{Board: dev, Transport: link.OverWifi, Where: "192.0.2.9", Approved: true}}
	r.setFirmware(t, "0.1.0")
	r.addBuild(t, makeBuild("t4s3", "0.1.5", true))
	if st := r.row(t)["status"].(map[string]any); st["state"] != FwUpdate || st["source"] != "release" || st["latest"] != "0.2.0" {
		t.Errorf("an older build than the release: %v", st)
	}
	r.addBuild(t, makeBuild("t4s3", "0.2.1-dev.1", true))
	if st := r.row(t)["status"].(map[string]any); st["state"] != FwLocal || st["latest"] != "0.2.1-dev.1" {
		t.Errorf("a newer build than the release: %v", st)
	}
	// Without the key, the build is offered even when the release is newer.
	r.noKey()
	r.do(t, "DELETE", "/admin/v1/firmware/builds/t4s3/0.2.1-dev.1", "")
	r.a.loadCatalog(t.Context())
	st := r.row(t)["status"].(map[string]any)
	if st["state"] != FwLocal || st["latest"] != "0.1.5" || !strings.Contains(st["text"].(string), "Release 0.2.0 is newer") {
		t.Errorf("no key, an older build: %v", st)
	}
}

// Set up a new board: a blank chip on a USB port is offered the build made
// here for a board with that chip, and one click writes its full image.
func TestLocalBuildOnBlankPort(t *testing.T) {
	r := newRig(t)
	r.noKey()
	const port = "cu.usbmodem9001"
	b := makeBuild("t4s3", "0.3.0-dev.7", true)
	r.addBuild(t, b)
	r.do(t, "POST", "/admin/v1/ports/"+port+"/register", "")
	if code, out := r.do(t, "POST", "/admin/v1/ports/"+port+"/flash", `{"board":"t4s3","source":"local"}`); code != 409 || !strings.Contains(out["error"].(string), "look at the board first") {
		t.Errorf("before identify: %d %v", code, out)
	}
	code, id := r.do(t, "POST", "/admin/v1/ports/"+port+"/identify", "")
	if code != 200 || id["source"] != "probe" || len(id["local"].([]any)) != 1 {
		t.Fatalf("identify: %d %v", code, id)
	}
	if l := id["local"].([]any)[0].(map[string]any); l["board"] != "t4s3" || l["version"] != "0.3.0-dev.7" {
		t.Errorf("local: %v", l)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/"+port+"/flash", `{"board":"nope","source":"local"}`); code != 404 {
		t.Errorf("a board with no build: %d", code)
	}
	code, job := r.do(t, "POST", "/admin/v1/ports/"+port+"/flash", `{"board":"t4s3","source":"local"}`)
	if code != 202 || job["source"] != "local" || job["kind"] != "flash" {
		t.Fatalf("flash: %d %v", code, job)
	}
	if j := r.waitJob(t, job["id"].(string)); j.State != JobDone {
		t.Errorf("job: %+v", j)
	}
	if !bytes.Equal(r.fl.image, b.full) || r.fl.offset != 0 {
		t.Errorf("wrote %d bytes at %#x", len(r.fl.image), r.fl.offset)
	}
	// A chip the build isn't for is refused.
	r.fl.chip.Name = "ESP32-C3"
	r.do(t, "POST", "/admin/v1/ports/"+port+"/identify", "")
	if code, out := r.do(t, "POST", "/admin/v1/ports/"+port+"/flash", `{"board":"t4s3","source":"local"}`); code != 409 || !strings.Contains(out["error"].(string), "ESP32-C3") {
		t.Errorf("another chip: %d %v", code, out)
	}
}
