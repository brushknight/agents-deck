package admin

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

func (r *rig) portUpload(t *testing.T, port string, body []byte) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/admin/v1/ports/"+port+"/firmware-file", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("X-File-Name", "agents-deck-t4s3-0.2.0.bin")
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// A blank board on a USB cable takes a full image picked on this computer,
// written at 0x0, also in a build without a release key.
func TestPortInstallFile(t *testing.T) {
	r := newRig(t)
	rel := r.rel.Releases(filepath.Join(r.dir, "nokey"))
	rel.Key = nil
	r.a.Firmware = &firmware.Client{Releases: rel}
	const port = "cu.usbmodem9001"
	full := espFull(9, espApp(9, "0.2.0"))

	if code, out := r.portUpload(t, port, full); code != 403 {
		t.Errorf("an unregistered port: %d %v", code, out)
	}
	r.do(t, "POST", "/admin/v1/ports/"+port+"/register", "")
	if code, out := r.portUpload(t, port, full); code != 409 || !strings.Contains(out["error"].(string), "look at the board first") {
		t.Errorf("before identify: %d %v", code, out)
	}
	if code, out := r.do(t, "POST", "/admin/v1/ports/"+port+"/identify", ""); code != 200 || out["source"] != "probe" || out["releaseKey"] != false {
		t.Fatalf("identify: %d %v", code, out)
	}
	code, up := r.portUpload(t, port, full)
	if code != 200 || up["kind"] != "full" || up["via"] != "usb" || up["port"] != port || up["chip"] != "ESP32-S3" || up["chipChecked"] != true || up["device"] != "" {
		t.Fatalf("upload: %d %v", code, up)
	}
	if r.fl.writes != 0 {
		t.Fatal("written before the install was confirmed")
	}
	// The file is this port's: a device can't install it, and an unknown id isn't one.
	if code, _ := r.do(t, "POST", "/admin/v1/ports/"+port+"/install-file", `{"file":"nope"}`); code != 404 {
		t.Errorf("an unknown file: %d", code)
	}
	code, job := r.do(t, "POST", "/admin/v1/ports/"+port+"/install-file", `{"file":"`+up["file"].(string)+`"}`)
	if code != 202 || job["file"] != "agents-deck-t4s3-0.2.0.bin" || job["port"] != port {
		t.Fatalf("install: %d %v", code, job)
	}
	j := r.waitJob(t, job["id"].(string))
	if j.State != JobDone || !strings.Contains(strings.Join(j.Steps, "|"), "not checked against a release") {
		t.Errorf("job: %+v", j)
	}
	if !bytes.Equal(r.fl.image, full) || r.fl.offset != 0 {
		t.Errorf("wrote %d bytes at %#x, want the file at 0x0", len(r.fl.image), r.fl.offset)
	}
	if r.a.held(newPort) {
		t.Error("the port wasn't handed back, so the board can't pair")
	}
	// The network path still refuses without the key.
	if code, _ := r.do(t, "POST", "/admin/v1/ports/"+port+"/flash", `{"board":"t4s3"}`); code != 409 {
		t.Errorf("a release download without the key: %d", code)
	}
}

func TestPortInstallFileRefuses(t *testing.T) {
	r := newRig(t)
	const port = "cu.usbmodem9001"
	r.do(t, "POST", "/admin/v1/ports/"+port+"/register", "")
	r.do(t, "POST", "/admin/v1/ports/"+port+"/identify", "") // an ESP32-S3 with 16 MB
	app := espApp(9, "0.2.0")
	big := espFull(9, app)
	big[3] = 0x50 // built for 32 MB of flash
	for name, tc := range map[string]struct {
		body []byte
		want string
	}{
		"app image":    {app, "app image (the firmware alone), which only starts on a board that already has a bootloader"},
		"another chip": {espFull(5, espApp(5, "0.2.0")), "this file is for an ESP32-C3, and the board on this port is an ESP32-S3"},
		"flash size":   {big, "built for 32 MB of flash and the board has 16 MB"},
		"not an image": {[]byte(strings.Repeat("not firmware ", 1000)), "isn't an ESP32 firmware image"},
	} {
		if code, out := r.portUpload(t, port, tc.body); code != 400 || !strings.Contains(out["error"].(string), tc.want) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(r.a.DataDir, "firmware-files")); len(ents) != 0 {
		t.Errorf("refused files kept: %d", len(ents))
	}

	// Another USB device on the port since it was looked at: look again.
	full := espFull(9, app)
	_, up := r.portUpload(t, port, full)
	list := r.a.ListPorts
	r.a.ListPorts = func() ([]serial.Info, error) {
		return []serial.Info{{Path: newPort, VID: "303a", PID: "1001", Serial: "OTHER", Likely: "esp32-usb"}}, nil
	}
	if code, out := r.do(t, "POST", "/admin/v1/ports/"+port+"/install-file", `{"file":"`+up["file"].(string)+`"}`); code != 409 || !strings.Contains(out["error"].(string), "look at the board first") {
		t.Errorf("another board on the port: %d %v", code, out)
	}
	r.a.ListPorts = list

	// A board that runs agents deck takes files through its own row.
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:ff", "t4s3-panel", "0.1.0")
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverSerial, Where: newPort, Approved: true}}
	if code, out := r.portUpload(t, port, full); code != 409 || !strings.Contains(out["error"].(string), "already runs agents deck") {
		t.Errorf("a board with a session: %d %v", code, out)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/ports/"+port+"/install-file", `{"file":"`+up["file"].(string)+`"}`); code != 409 {
		t.Errorf("install on a board with a session: %d", code)
	}
	// And the port's file never goes to a device.
	r.a.Devices.Approve(d.Name)
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/install-file", `{"file":"`+up["file"].(string)+`"}`); code != 404 {
		t.Errorf("a port's file installed on a device: %d", code)
	}
	if r.fl.writes != 0 {
		t.Error("a refused file reached the board")
	}
}
