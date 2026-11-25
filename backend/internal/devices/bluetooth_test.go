package devices

import (
	"path/filepath"
	"testing"
)

func TestBluetoothBindingAndUplink(t *testing.T) {
	r := &Registry{Path: filepath.Join(t.TempDir(), "devices.json")}
	a, _ := r.Board("AA:BB:CC:00:00:01", "round", "1.0")
	b, _ := r.Board("AA:BB:CC:00:00:02", "round", "1.0")
	const id = "6B1F2C3D-0000-4000-8000-0000000000A1"
	if err := r.SetBLE(a.Name, id); err != nil {
		t.Fatal(err)
	}
	if err := r.SetBLE(b.Name, id); err != nil { // one board per identifier
		t.Fatal(err)
	}
	if d, ok := r.ByBLE(id); !ok || d.Name != b.Name {
		t.Fatalf("by identifier: %+v", d)
	}
	if d, _ := r.Get(a.Name); d.BLE != "" {
		t.Fatal("the identifier stayed on the first board")
	}
	if d, ok := r.ByBoardID("AA:BB:CC:00:00:02"); !ok || d.Name != b.Name {
		t.Fatal("by board id")
	}
	for _, p := range []string{"wifi", "ble", "auto"} {
		if err := r.SetUplink(b.Name, p); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := r.Get(b.Name); d.Uplink != "" {
		t.Fatalf("auto is stored as empty: %q", d.Uplink)
	}
	if r.SetUplink(b.Name, "usb") == nil {
		t.Fatal("unknown uplink accepted")
	}
	if d, ok, _ := r.ApproveAs(b.Name, Act); !ok || !d.Approved || d.Scope != Act {
		t.Fatalf("approve: %+v", d)
	}
	_ = r.Revoke(b.Name)
	if d, _ := r.Get(b.Name); d.BLE != "" || !d.Revoked {
		t.Fatalf("revoke keeps the Bluetooth binding: %+v", d)
	}
	if _, ok, _ := r.ApproveAs(b.Name, Act); ok {
		t.Fatal("a revoked board was approved by adding it")
	}
	_ = r.AddNetwork("phone", View, "01", "ff")
	if r.SetBLE("phone", id) == nil || r.SetUplink("phone", "wifi") == nil {
		t.Fatal("a network device took a Bluetooth binding or uplink")
	}
}
