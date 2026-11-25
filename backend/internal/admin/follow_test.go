package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// Made-up boards: an ESP32 on its own USB (its MAC as the serial) and an
// adapter with no serial number.
const (
	fakeMAC = "02:00:00:AA:BB:01"
	fakeKey = "303a:1001:" + fakeMAC
)

func followRig(t *testing.T, ports []link.PortConfig, listed *[]serial.Info) *Admin {
	t.Helper()
	old := followEvery
	followEvery = 0
	t.Cleanup(func() { followEvery = old })
	dir := t.TempDir()
	a := &Admin{Config: filepath.Join(dir, "config.json"), DataDir: dir,
		ListPorts: func() ([]serial.Info, error) { return *listed, nil }}
	b, _ := json.Marshal(map[string]any{"link": map[string]any{"ports": ports}, "keep": "me"})
	if err := os.WriteFile(a.Config, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFollowPortsMovesARegisteredBoard(t *testing.T) {
	listed := []serial.Info{{Path: "/dev/cu.usbmodem111401", VID: "303a", PID: "1001", Serial: fakeMAC, Likely: "esp32-usb"}}
	a := followRig(t, []link.PortConfig{{Path: "/dev/cu.usbmodem11401", Baud: 115200, USB: fakeKey}}, &listed)

	got := a.LinkPorts(a.registeredPorts())
	if len(got) != 1 || got[0].Path != "/dev/cu.usbmodem111401" || got[0].USB != fakeKey || got[0].Baud != 115200 {
		t.Fatalf("not followed: %+v", got)
	}
	if reg := a.registeredPorts(); len(reg) != 1 || reg[0].Path != "/dev/cu.usbmodem111401" {
		t.Fatalf("config.json not updated: %+v", reg)
	}
	if cfg := a.readConfig(); cfg["keep"] != "me" {
		t.Errorf("other settings lost: %v", cfg)
	}
	// The ports list shows it registered on its new path, not as a new board.
	for _, r := range a.portRows() {
		if r.Path == "/dev/cu.usbmodem111401" && (!r.Registered || r.State == PortNew) {
			t.Errorf("new path not registered: %+v", r)
		}
		if r.Path == "/dev/cu.usbmodem11401" {
			t.Errorf("old path still listed: %+v", r)
		}
	}
}

func TestFollowPortsLearnsOldRegistrations(t *testing.T) {
	listed := []serial.Info{
		{Path: "/dev/cu.usbmodem1101", VID: "303a", PID: "1001", Serial: fakeMAC},
		{Path: "/dev/cu.usbserial-10", VID: "1a86", PID: "7523"}, // no serial number
	}
	a := followRig(t, []link.PortConfig{{Path: "/dev/cu.usbmodem1101"}, {Path: "/dev/cu.usbserial-10"}}, &listed)

	got := a.LinkPorts(a.registeredPorts())
	if len(got) != 2 || got[0].Path != "/dev/cu.usbmodem1101" || got[1].Path != "/dev/cu.usbserial-10" {
		t.Fatalf("paths changed: %+v", got)
	}
	reg := a.registeredPorts()
	if reg[0].USB != fakeKey || reg[1].USB != "" {
		t.Fatalf("identity not learned (or made up): %+v", reg)
	}
	// Then the board moves: it is followed by the identity it learned.
	listed = []serial.Info{{Path: "/dev/cu.usbmodem2101", VID: "303a", PID: "1001", Serial: fakeMAC}}
	if got := a.LinkPorts(a.registeredPorts()); got[0].Path != "/dev/cu.usbmodem2101" || got[1].Path != "/dev/cu.usbserial-10" {
		t.Fatalf("after the move: %+v", got)
	}
}

func TestFollowPortsLeavesAmbiguousAndTakenPorts(t *testing.T) {
	cases := map[string]struct {
		ports  []link.PortConfig
		listed []serial.Info
	}{
		"two ports with the same identity": {
			[]link.PortConfig{{Path: "/dev/cu.usbmodem11401", USB: fakeKey}},
			[]serial.Info{{Path: "/dev/cu.usbmodem2101", VID: "303a", PID: "1001", Serial: fakeMAC},
				{Path: "/dev/cu.usbmodem3101", VID: "303a", PID: "1001", Serial: fakeMAC}},
		},
		"its new path is registered already": {
			[]link.PortConfig{{Path: "/dev/cu.usbmodem11401", USB: fakeKey}, {Path: "/dev/cu.usbmodem2101"}},
			[]serial.Info{{Path: "/dev/cu.usbmodem2101", VID: "303a", PID: "1001", Serial: fakeMAC}},
		},
		"not plugged in": {
			[]link.PortConfig{{Path: "/dev/cu.usbmodem11401", USB: fakeKey}},
			[]serial.Info{{Path: "/dev/cu.usbmodem2101", VID: "303a", PID: "1001", Serial: "02:00:00:AA:BB:99"}},
		},
	}
	for name, c := range cases {
		listed := c.listed
		a := followRig(t, c.ports, &listed)
		if got := a.LinkPorts(a.registeredPorts()); got[0].Path != "/dev/cu.usbmodem11401" {
			t.Errorf("%s: moved to %s", name, got[0].Path)
		}
	}
}

func TestRegisterKeepsTheUSBIdentity(t *testing.T) {
	r := newRig(t)
	code, _ := r.do(t, "POST", "/admin/v1/ports/"+portID(newPort)+"/register", `{}`)
	if code != 200 {
		t.Fatalf("register: %d", code)
	}
	for _, p := range r.a.registeredPorts() {
		if p.Path == newPort && p.USB != "" {
			t.Errorf("a port with no serial number got an identity: %+v", p)
		}
	}
}
