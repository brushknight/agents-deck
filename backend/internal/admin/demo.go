package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/firmware"
	"github.com/brushknight/agents-deck/backend/internal/flash"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/serial"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// UseDemo turns a into the pretend deck of serve --demo: its own folder for
// settings and devices (never the real config.json or devices.json), made-up
// boards and USB ports, a fake flasher and a fake release. Nothing touches a
// real serial port or the network.
func UseDemo(a *Admin) error {
	dir, err := os.MkdirTemp("", "agents-deck-demo-")
	if err != nil {
		return err
	}
	a.Demo = true
	a.DataDir, a.Config = dir, filepath.Join(dir, "config.json")
	a.LogFile = filepath.Join(dir, "agentd.log")
	a.Login, a.Open, a.CLIDir = nil, nil, ""
	a.NetworkOn = true
	now := time.Now().UTC()
	devs := []devices.Device{
		{Name: "round-28-a1b2c3", Kind: devices.Serial, Scope: devices.Act, Created: now.Add(-30 * 24 * time.Hour), LastSeen: now,
			BoardID: "a1b2c3", Type: "round-28", Firmware: "0.1.1", Approved: true, CertSerial: "01"},
		{Name: "t4s3-panel-d4e5f6", Kind: devices.Serial, Scope: devices.Act, Created: now.Add(-9 * 24 * time.Hour), LastSeen: now,
			BoardID: "d4e5f6", Type: "t4s3-panel", Firmware: "0.2.0", Approved: true},
		{Name: "strip-147-0a0b0c", Kind: devices.Serial, Scope: devices.View, Created: now.Add(-4 * 24 * time.Hour), LastSeen: now,
			BoardID: "0a0b0c", Type: "strip-147", Firmware: "0.2.0", Approved: true},
		{Name: "square-40-123456", Kind: devices.Serial, Scope: devices.Act, Created: now.Add(-60 * 24 * time.Hour), LastSeen: now.Add(-26 * time.Hour),
			BoardID: "123456", Type: "square-40", Firmware: "0.1.1", Approved: true},
		{Name: "phone", Kind: devices.Network, Scope: devices.View, Created: now.Add(-7 * 24 * time.Hour), LastSeen: now.Add(-50 * time.Hour), CertSerial: "02"},
	}
	b, _ := json.MarshalIndent(devs, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), b, 0o600); err != nil {
		return err
	}
	a.Devices = &devices.Registry{Path: filepath.Join(dir, "devices.json")}
	for id, name := range map[string]string{"round-28-a1b2c3": "Desk round", "t4s3-panel-d4e5f6": "Tall one",
		"strip-147-0a0b0c": "Strip", "square-40-123456": "Wall panel", "phone": "Phone"} {
		_ = a.setLabel(id, name)
	}
	cfg := map[string]any{"link": map[string]any{"ports": []link.PortConfig{
		{Path: "/dev/cu.demo-tall", Baud: 115200}, {Path: "/dev/cu.demo-strip", Baud: 115200}}}}
	b, _ = json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(a.Config, b, 0o600); err != nil {
		return err
	}
	_ = os.WriteFile(a.LogFile, []byte(strings.Join([]string{
		now.Format("2006/01/02 15:04:05") + ` link: board "round-28-a1b2c3" connected over Wi-Fi from 192.0.2.40`,
		now.Format("2006/01/02 15:04:05") + " link /dev/cu.demo-tall: open at 115200 baud",
		now.Format("2006/01/02 15:04:05") + " new USB port, looks like an ESP32-S3 (not opened)",
		now.Format("2006/01/02 15:04:05") + ` agentctl demo serving web on http://127.0.0.1`,
	}, "\n")+"\n"), 0o600)

	wifi := &demoWifi{ssid: "Home 2.4"}
	a.WifiSSID = wifi.SSID
	a.WifiDefault = wifi.Set
	a.SendWifi = func(board, ssid, password string, forget bool) (int, string) {
		return 200, "sent: the board is trying " + ssid
	}
	a.SendDefault = func(board string) (int, string) { return 200, "sent" }
	a.PairInfo = func() (string, string) { return "https://deck.example:7341", "SHA256:demo" }
	ports := &demoPorts{}
	a.ListPorts = ports.list
	a.Links = func() []link.LinkInfo {
		return []link.LinkInfo{
			{Board: "round-28-a1b2c3", Transport: link.OverWifi, Where: "192.0.2.40", Since: now.Add(-3 * time.Hour), Approved: true,
				Net: &link.NetInfo{Wifi: "connected", SSID: "Home 2.4", IP: "192.0.2.40", RSSI: -52, HasCert: true}},
			{Board: "t4s3-panel-d4e5f6", Transport: link.OverSerial, Where: "/dev/cu.demo-tall", Since: now.Add(-time.Hour), Approved: true,
				Net: &link.NetInfo{Wifi: "off"}},
			{Board: "strip-147-0a0b0c", Transport: link.OverSerial, Where: "/dev/cu.demo-strip", Since: now.Add(-2 * time.Hour), Approved: true},
		}
	}
	a.PortStatus = func() []link.PortStatus { return nil }
	a.Flasher = &demoFlasher{}
	a.Firmware = &demoFirmware{dir: dir}
	a.Updater = &demoUpdater{current: a.Version}
	a.OTA = demoOTA{a}
	return nil
}

// demoOTA pretends a board on Wi-Fi takes an update in about twenty seconds.
type demoOTA struct{ a *Admin }

func (o demoOTA) OfferFirmware(device, board, version string) error {
	go func() {
		j := link.FirmwareJob{Device: device, Board: board, Version: version, From: "0.1.1", State: link.FirmwareOffered}
		o.a.FirmwareChanged(j)
		time.Sleep(time.Second)
		for p := 0; p <= 100; p += 10 {
			j.State, j.Percent = link.FirmwareDownloading, p
			o.a.FirmwareChanged(j)
			time.Sleep(1200 * time.Millisecond)
		}
		for _, st := range []string{link.FirmwareVerifying, link.FirmwareRestarting, link.FirmwareDone} {
			j.State = st
			o.a.FirmwareChanged(j)
			time.Sleep(2 * time.Second)
		}
	}()
	return nil
}

type demoWifi struct {
	mu   sync.Mutex
	ssid string
}

func (w *demoWifi) SSID() string { w.mu.Lock(); defer w.mu.Unlock(); return w.ssid }
func (w *demoWifi) Set(ssid, _ string, forget bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if forget {
		ssid = ""
	}
	w.ssid = ssid
	return nil
}

type demoPorts struct{}

func (demoPorts) list() ([]serial.Info, error) {
	return []serial.Info{
		{Path: "/dev/cu.demo-new", VID: "303a", PID: "1001", Product: "USB JTAG/serial debug unit", Likely: "esp32-usb"},
		{Path: "/dev/cu.demo-strip", VID: "1a86", PID: "55d3", Product: "USB Single Serial", Likely: "ch340"},
		{Path: "/dev/cu.demo-tall", VID: "303a", PID: "1001", Product: "USB JTAG/serial debug unit", Likely: "esp32-usb"},
	}, nil
}

// demoFlasher pretends: a probe finds an ESP32-S3 with 16 MB, a write takes
// about fifteen seconds.
type demoFlasher struct{}

func (demoFlasher) Probe(ctx context.Context, port serial.Info) (flash.Chip, error) {
	select {
	case <-ctx.Done():
		return flash.Chip{}, ctx.Err()
	case <-time.After(1500 * time.Millisecond):
	}
	return flash.Chip{Name: "ESP32-S3", MAC: "02:00:00:00:00:01", FlashSize: 16 << 20}, nil
}

func (demoFlasher) Write(ctx context.Context, port serial.Info, image io.Reader, size int64, offset uint32, prog flash.Progress) error {
	step := func(d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	prog("connect", 0, 0)
	if err := step(1500 * time.Millisecond); err != nil {
		return err
	}
	prog("erase", 0, size)
	if err := step(1500 * time.Millisecond); err != nil {
		return err
	}
	for done := int64(0); done < size; done += size / 40 {
		prog("write", done, size)
		if err := step(250 * time.Millisecond); err != nil {
			return err
		}
	}
	prog("write", size, size)
	prog("verify", 0, size)
	if err := step(time.Second); err != nil {
		return err
	}
	prog("verify", size, size)
	prog("reset", 0, 0)
	return step(500 * time.Millisecond)
}

// demoFirmware is a made-up release 0.2.0 with today's boards.
type demoFirmware struct {
	dir string
}

var demoCatalog = firmware.Catalog{Version: "0.2.0", Boards: []firmware.Board{
	{Board: "t4s3-panel", Name: "LilyGO T4-S3", Chip: "ESP32-S3", Types: []string{"t4s3-panel"}, Wifi: true, FlashSize: 16 << 20, Size: 1887436, Version: "0.2.0", OTA: &firmware.OTA{File: "t4s3.app.bin"}},
	{Board: "round-28", Name: "Waveshare 2.8″ round", Chip: "ESP32-S3", Types: []string{"round-28"}, Wifi: true, FlashSize: 16 << 20, Size: 1700000, Version: "0.2.0", OTA: &firmware.OTA{File: "round-28.app.bin"}},
	{Board: "round-21", Name: "Waveshare 2.1″ round", Chip: "ESP32-S3", Types: []string{"round-21"}, Wifi: true, FlashSize: 16 << 20, Size: 1700000, Version: "0.2.0"},
	{Board: "square-40", Name: "Waveshare 4″ square", Chip: "ESP32-S3", Types: []string{"square-40"}, Wifi: false, FlashSize: 16 << 20, Size: 1600000, Version: "0.2.0"},
	{Board: "amoled-18", Name: "Waveshare 1.8″ AMOLED", Chip: "ESP32-S3", Types: []string{"amoled-18"}, Wifi: true, FlashSize: 16 << 20, Size: 1650000, Version: "0.2.0"},
	{Board: "strip-147", Name: "Waveshare 1.47″ strip", Chip: "ESP32-S3", Types: []string{"strip-147"}, Wifi: false, FlashSize: 16 << 20, Size: 1200000, Version: "0.2.0"},
}}

func (f *demoFirmware) Latest(ctx context.Context) (firmware.Catalog, error) { return demoCatalog, nil }

func (f *demoFirmware) Image(ctx context.Context, board, version string) (string, error) {
	b, ok := demoCatalog.Board(board)
	if !ok {
		return "", errors.New("no such board")
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(1200 * time.Millisecond):
	}
	p := filepath.Join(f.dir, board+".bin")
	return p, os.WriteFile(p, make([]byte, b.Size), 0o600)
}

// demoUpdater has 0.2.1 downloaded and ready; installing it says it's a demo.
type demoUpdater struct{ current string }

func (u *demoUpdater) State() update.State {
	return update.State{State: update.StateReady, Current: u.current, Version: "0.2.1", Install: "app",
		Notes: "Boards on Wi-Fi reconnect faster after sleep\nFix: the status line showed a removed board\nNew board: Waveshare 3.49″ strip"}
}
func (u *demoUpdater) Check(context.Context) (update.State, error)    { return u.State(), nil }
func (u *demoUpdater) Download(context.Context) (update.State, error) { return u.State(), nil }
func (u *demoUpdater) Apply(context.Context) error {
	return errors.New("demo mode: nothing is installed")
}
