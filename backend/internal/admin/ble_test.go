package admin

import (
	"context"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/ble"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// Without a Bluetooth helper every call says so plainly; the device list and
// the uplink preference still work.
func TestBluetoothUnavailable(t *testing.T) {
	r := newRig(t)
	for _, u := range []*ble.Uplinks{nil, {Central: &ble.Central{}}} {
		r.a.Bluetooth = u
		if u != nil {
			u.Central.Run(context.Background())
		}
		code, st := r.do(t, "GET", "/admin/v1/bluetooth", "")
		if code != 200 || st["available"] != false || st["why"] != ble.NotHere || st["helper"] != "none" {
			t.Errorf("status: %d %v", code, st)
		}
		if code, out := r.do(t, "POST", "/admin/v1/bluetooth/scan", `{"on":true}`); code != 503 || out["error"] != ble.NotHere {
			t.Errorf("scan: %d %v", code, out)
		}
		if code, _ := r.do(t, "POST", "/admin/v1/bluetooth/scan", `{"on":false}`); code != 204 {
			t.Errorf("stopping a scan: %d", code)
		}
		code, out := r.do(t, "GET", "/admin/v1/bluetooth/boards", "")
		if code != 200 || len(out["boards"].([]any)) != 0 || out["why"] != ble.NotHere {
			t.Errorf("boards: %d %v", code, out)
		}
		if code, out := r.do(t, "POST", "/admin/v1/bluetooth/boards/6B1F2C3D-0000-4000-8000-0000000000A1/add", ""); code != 503 || out["error"] != ble.NotHere {
			t.Errorf("add: %d %v", code, out)
		}
	}
}

func TestDeviceUplink(t *testing.T) {
	r := newRig(t)
	r.a.Bluetooth = &ble.Uplinks{Central: &ble.Central{}, Hub: &link.Hub{}}
	d, _ := r.a.Devices.Board("aa:bb:cc:dd:ee:07", "t4s3-panel", "0.1.0")
	_ = r.a.Devices.Approve(d.Name)
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/uplink", `{"uplink":"ble"}`); code != 400 {
		t.Errorf("ble before it was added over Bluetooth: %d %v", code, out)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/uplink", `{"uplink":"usb"}`); code != 400 {
		t.Error("an unknown uplink")
	}
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/uplink", `{"uplink":"wifi"}`); code != 204 {
		t.Fatalf("wifi: %d %v", code, out)
	}
	_ = r.a.Devices.SetBLE(d.Name, "6B1F2C3D-0000-4000-8000-0000000000A1")
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/uplink", `{"uplink":"ble"}`); code != 204 {
		t.Fatalf("ble: %d %v", code, out)
	}
	// Over Bluetooth now.
	r.links = []link.LinkInfo{{Board: d.Name, Transport: link.OverBLE, Where: "bluetooth deck-ddee07", Approved: true}}
	_, out := r.do(t, "GET", "/admin/v1/devices", "")
	row := out["devices"].([]any)[0].(map[string]any)
	if row["connection"] != "bluetooth" || row["uplinkPref"] != "ble" || row["bluetooth"] != true {
		t.Fatalf("row: %v", row)
	}
	ups := row["uplinks"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["via"] != "bluetooth" || ups[0].(map[string]any)["state"] != "in use" {
		t.Fatalf("uplinks: %v", ups)
	}
	// Firmware isn't sent over Bluetooth.
	r.a.loadCatalog(context.Background())
	if code, out := r.do(t, "POST", "/admin/v1/devices/"+d.Name+"/update", ""); code != 409 || out["error"] != link.ErrOnBluetooth.Error() {
		t.Errorf("update over Bluetooth: %d %v", code, out)
	}
	// Not on Bluetooth, Bluetooth off: it says so.
	r.links = nil
	_, out = r.do(t, "GET", "/admin/v1/devices", "")
	ups = out["devices"].([]any)[0].(map[string]any)["uplinks"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["state"] != "off" || ups[0].(map[string]any)["detail"] != ble.NotHere {
		t.Fatalf("uplinks when off: %v", ups)
	}
	// wifi, once it is paired for Wi-Fi: Bluetooth isn't used.
	_ = r.a.Devices.SetUplink(d.Name, "wifi")
	_ = r.a.Devices.SetBoardCert(d.Name, "0a", "ff")
	_, out = r.do(t, "GET", "/admin/v1/devices", "")
	ups = out["devices"].([]any)[0].(map[string]any)["uplinks"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["state"] != "not used" {
		t.Fatalf("uplinks with wifi: %v", ups)
	}
}
