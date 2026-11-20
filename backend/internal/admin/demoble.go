package admin

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/ble"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// demoBluetooth is Bluetooth in serve --demo: boards appear one by one while
// it looks, adding one walks through the passkey step in about eight
// seconds, and one weak board never gets its code typed (it fails, to show
// that too). Nothing touches a radio. AGENTS_DECK_DEMO_BLUETOOTH=off,
// unauthorized or none shows Bluetooth unavailable instead.
type demoBluetooth struct {
	a       *Admin
	unavail string // "" | off | unauthorized | none

	mu       sync.Mutex
	scanning bool
	since    time.Time
	added    map[string]time.Time // board name -> when it was added here
}

// The demo's identifiers for boards nearby, as the helper would give them.
const (
	demoBleNew     = "6B1F2C3D-0000-4000-8000-00000000D001"
	demoBleOther   = "6B1F2C3D-0000-4000-8000-00000000D002"
	demoBleWeak    = "6B1F2C3D-0000-4000-8000-00000000D004"
	demoBleRound   = "6B1F2C3D-0000-4000-8000-00000000D005"
	demoBleSquare  = "6B1F2C3D-0000-4000-8000-00000000D007"
	demoBleAmoled  = "6B1F2C3D-0000-4000-8000-00000000D006"
	demoBleNewType = "t4s3-panel"
)

// demoNearby: every board in range, with when it shows up after the scan
// starts.
var demoNearby = []struct {
	after time.Duration
	n     ble.Nearby
}{
	{600 * time.Millisecond, ble.Nearby{ID: demoBleNew, Name: "deck-7c21f0", RSSI: -54, Board: "t4s3-panel", BoardID: "AA:BB:CC:7C:21:F0", Firmware: "0.2.0", Setup: true, HasInfo: true}},
	{1400 * time.Millisecond, ble.Nearby{ID: demoBleAmoled, Name: "deck-5e6f70", RSSI: -63, Board: "amoled-18", BoardID: "5e6f70", Firmware: "0.1.1", HasInfo: true}},
	{2200 * time.Millisecond, ble.Nearby{ID: demoBleOther, Name: "deck-3fa9c1", RSSI: -71, Board: "round-28", BoardID: "AA:BB:CC:3F:A9:C1", Firmware: "0.2.0", HasInfo: true}},
	{3000 * time.Millisecond, ble.Nearby{ID: demoBleWeak, Name: "deck-0e1f22", RSSI: -88, Board: "round-21", BoardID: "AA:BB:CC:0E:1F:22", Firmware: "0.2.0", Setup: true, HasInfo: true}},
}

func (b *demoBluetooth) Status() ble.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.unavail {
	case "none":
		return ble.Status{Helper: "none", Why: ble.NotHere}
	case "off":
		return ble.Status{Helper: "running", Radio: "off", Why: "Bluetooth is off: turn it on in Control Centre or System Settings"}
	case "unauthorized":
		return ble.Status{Helper: "running", Radio: "unauthorized",
			Why: "agents deck may not use Bluetooth: allow \"agents deck Bluetooth\" in System Settings, Privacy & Security, Bluetooth"}
	}
	return ble.Status{Available: true, Helper: "running", Radio: "on", Scanning: b.scanning}
}

func (b *demoBluetooth) Scan(on bool) error {
	if st := b.Status(); on && !st.Available {
		return errors.New(st.Why)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if on && !b.scanning {
		b.since = time.Now()
	}
	b.scanning = on
	return nil
}

func (b *demoBluetooth) Nearby() []ble.Nearby {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []ble.Nearby
	if b.since.IsZero() {
		return out
	}
	for _, x := range demoNearby {
		if time.Since(b.since) >= x.after || !b.scanning {
			n := x.n
			n.Seen = time.Now()
			if _, ok := b.a.Devices.ByBLE(n.ID); ok {
				n.Setup = false
			}
			out = append(out, n)
		}
	}
	return out
}

func (b *demoBluetooth) Seen(id string) (ble.Nearby, bool) {
	for _, n := range b.Nearby() {
		if n.ID == id {
			return n, true
		}
	}
	return ble.Nearby{}, false
}

func (b *demoBluetooth) Add(ctx context.Context, id string, scope devices.Scope, prog ble.Progress) (devices.Device, error) {
	n, ok := b.Seen(id)
	if !ok {
		return devices.Device{}, errors.New("no such board nearby: scan first")
	}
	wait := func(d time.Duration) error {
		select {
		case <-ctx.Done():
			return errors.New("cancelled")
		case <-time.After(d):
			return nil
		}
	}
	prog("connect", "Connecting to "+n.Name)
	if err := wait(1500 * time.Millisecond); err != nil {
		return devices.Device{}, err
	}
	prog("passkey", "Type the code shown on the board into the window this computer opens.")
	if id == demoBleWeak {
		if err := wait(7 * time.Second); err != nil {
			return devices.Device{}, err
		}
		return devices.Device{}, errors.New("pairing timed out: type the code shown on the board when macOS asks for it")
	}
	if err := wait(5 * time.Second); err != nil {
		return devices.Device{}, err
	}
	prog("connected", "Paired. Checking with the board.")
	if err := wait(1500 * time.Millisecond); err != nil {
		return devices.Device{}, err
	}
	d, err := b.a.Devices.Board(n.BoardID, demoBleNewType, n.Firmware)
	if err != nil {
		return d, err
	}
	if err := b.a.Devices.SetBLE(d.Name, id); err != nil {
		return d, err
	}
	d, _, err = b.a.Devices.ApproveAs(d.Name, scope)
	if err != nil {
		return d, err
	}
	b.mu.Lock()
	b.added[d.Name] = time.Now()
	b.mu.Unlock()
	prog("done", d.Name+" is one of your boards now, connected over Bluetooth.")
	return d, nil
}

func (b *demoBluetooth) State(d devices.Device) ble.LinkState {
	if d.BLE == "" {
		return ble.LinkState{}
	}
	ls := ble.LinkState{ID: d.BLE}
	switch {
	case d.Uplink == link.UplinkWifi && d.CertSerial != "":
		ls.State = "not used"
	case d.Name == "round-28-a1b2c3":
		ls.State, ls.Uses = "standing by", link.OverWifi
	case d.Name == "square-40-123456":
		ls.State, ls.Problem = "connecting", "the board no longer knows this computer: put it back in setup from its screen, remove it in System Settings, Bluetooth if it is listed there, and add it again"
	default:
		ls.State = "connected"
	}
	return ls
}

// links: the demo's boards on Bluetooth (the ones added in the demo too).
func (b *demoBluetooth) links(now time.Time) []link.LinkInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []link.LinkInfo
	for name, at := range b.added {
		if d, ok := b.a.Devices.Get(name); ok && d.Active() && d.BLE != "" {
			out = append(out, link.LinkInfo{Board: name, Transport: link.OverBLE, Where: "bluetooth deck-7c21f0", Since: at, Approved: true,
				Net: &link.NetInfo{Wifi: "not set", BLE: "connected", Uplink: link.UplinkName(0)}})
		}
	}
	return out
}
