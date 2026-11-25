package ble

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link"
)

// Adding a board over Bluetooth while it is live on USB or Wi-Fi, with the
// kit's own code: board_wifi (--serial | --wifi) --ble is a board with a
// cable (or Wi-Fi) and Bluetooth at once. The first link carries the session;
// a central that connects meanwhile gets no Hello, and the board reports it
// on the first link (NetStatus ble=CONNECTED), as a real board does during
// the introduction (firmware/panel-core/src/ble_model.h).

const kitDev = "board-wifi-000077"

// pipeRW is a board's stdin and stdout as the deck's serial line.
type pipeRW struct {
	io.Reader
	io.Writer
}

// startUSBBoard runs board_wifi with its stdin and stdout as a USB serial
// line, and a session of the deck on it until stop is called (the cable
// pulled out: the session ends and the board sees its stdin close).
func startUSBBoard(t *testing.T, k *kitDeck, bin string, args ...string) (l *boardLog, cmd *exec.Cmd, stop func()) {
	t.Helper()
	cmd = exec.Command(bin, args...)
	errp, _ := cmd.StderrPipe()
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	l = &boardLog{t: t}
	done := make(chan struct{})
	go func() { l.read(bufio.NewScanner(errp)); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done; _ = cmd.Wait() })
	ctx, cancel := context.WithCancel(context.Background())
	s := link.NewSession(link.ServerDeck{S: k.srv}, pipeRW{out, in}, "test-port")
	s.Hub, s.Where = k.hub, "test-port"
	s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond
	ended := make(chan struct{})
	go func() { _ = s.Run(ctx); close(ended) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			<-ended
			_ = in.Close()
		})
	}
	t.Cleanup(stop)
	return l, cmd, stop
}

// onlyLink waits until the board's one session is on transport, approved.
func onlyLink(r *rig, transport string) {
	r.t.Helper()
	r.waitFor("the session on "+transport, func() bool {
		l := r.hub.Links()
		return len(l) == 1 && l[0].Transport == transport && l[0].Approved
	})
}

func (k *kitDeck) scanFor(name string) {
	k.t.Helper()
	if err := k.up.Scan(true); err != nil {
		k.t.Fatal(err)
	}
	k.waitFor("the board in the scan", func() bool { n, ok := k.up.Seen(periph1); return ok && n.HasInfo && n.Name == name })
	_ = k.up.Scan(false)
}

func TestBluetoothAddWhileOnUSBWithTheKit(t *testing.T) {
	bin := kitBoard(t)
	k := newKitDeck(t)
	store := filepath.Join(k.dir, "nvs")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(k.dir, "k.sock")

	// 1. The board on USB, approved; it pairs for Wi-Fi over the cable.
	usb, usbCmd, unplug := startUSBBoard(t, k, bin, "--serial", "--ble", sock, "--store", store, "--mtu", "185", "--seconds", "90")
	name, info := advertised(usb)
	boards := []fakeBoard{{ID: periph1, Name: name, RSSI: -48, Info: info, Sock: "k.sock", MTU: 185}}
	k.setBoards(boards)
	k.waitFor("registered", func() bool { _, ok := k.srv.Devices.Get(kitDev); return ok })
	if err := k.srv.Devices.Approve(kitDev); err != nil {
		t.Fatal(err)
	}
	onlyLink(k.rig, link.OverSerial)
	usb.wait("paired serial=", 5*time.Second)

	// 2. Added over Bluetooth while USB carries it: the passkey, no Hello over
	// Bluetooth, the USB session confirms, and Bluetooth stands by.
	k.scanFor(name)
	var stages []string
	d, err := k.up.Add(context.Background(), periph1, devices.Act, func(stage, _ string) { stages = append(stages, stage) })
	if err != nil {
		t.Fatalf("add: %v\n%s", err, usb.all())
	}
	if strings.Join(stages, ",") != "connect,passkey,connected,done" {
		t.Fatalf("stages: %v", stages)
	}
	if d.Name != kitDev || d.BLE != periph1 || !d.Approved {
		t.Fatalf("device: %+v", d)
	}
	usb.wait("ble standby", time.Second)
	usb.wait("ble disconnected", 5*time.Second)
	if usb.count("ble carries") != 0 {
		t.Fatalf("Bluetooth carried the session beside USB:\n%s", usb.all())
	}
	k.waitFor("Bluetooth stands by", func() bool { ls := k.up.State(d); return ls.State == "standing by" && ls.Uses == link.OverSerial })
	onlyLink(k.rig, link.OverSerial)

	// 3. ble: the board holds it (over USB), and USB still wins while it is there.
	if err := k.srv.Devices.SetUplink(kitDev, "ble"); err != nil {
		t.Fatal(err)
	}
	usb.wait("uplink value=2", 5*time.Second)
	time.Sleep(200 * time.Millisecond)
	if n := k.count("connect " + periph1); n != 1 {
		t.Fatalf("Bluetooth connected beside USB (%d connects)", n)
	}

	// 4. The cable comes out: Bluetooth takes over, bonded (no passkey).
	boards[0].Paired = true
	k.setBoards(boards)
	unplug()
	usb.wait("serial unplugged", 5*time.Second)
	onlyLink(k.rig, link.OverBLE)
	usb.wait("ble carries", time.Second)
	usb.wait("snapshot_end", 5*time.Second)

	// 5. Back to auto: the board hears it over Bluetooth; it comes up on Wi-Fi
	// (auto prefers it) and the Bluetooth link closes; Wi-Fi gone, Bluetooth
	// comes back.
	if err := k.srv.Devices.SetUplink(kitDev, "auto"); err != nil {
		t.Fatal(err)
	}
	k.waitFor("the board holds auto", func() bool { return usb.count("uplink value=0") == 1 })
	wifi, wifiCmd := startBoard(t, bin, "--wifi", "--store", store, "--seconds", "3")
	wifi.wait("welcome name="+kitDev, 5*time.Second)
	k.waitFor("Bluetooth gave way", func() bool { return usb.count("ble disconnected") == 2 })
	k.waitFor("Bluetooth stands by for Wi-Fi", func() bool { ls := k.up.State(d); return ls.State == "standing by" && ls.Uses == link.OverWifi })
	_ = wifiCmd.Wait()
	k.waitFor("back on Bluetooth", func() bool { return usb.count("ble carries") == 2 })
	onlyLink(k.rig, link.OverBLE)

	// 6. The board drops Bluetooth (it restarts, or goes out of range): the
	// deck connects again when it is back.
	_ = usbCmd.Process.Kill()
	k.waitFor("the session over Bluetooth ended", func() bool { return len(k.hub.Links()) == 0 })
	again, _ := startBoard(t, bin, "--ble", sock, "--store", store, "--mtu", "185", "--seconds", "30")
	again.wait("ble carries", 15*time.Second)
	onlyLink(k.rig, link.OverBLE)
}

// A board on Wi-Fi that this computer is still paired with over Bluetooth
// (a bond left from an attempt that failed): added with no passkey, confirmed
// on its Wi-Fi session (its certificate).
func TestBluetoothAddWhileOnWifiWithALeftoverBondWithTheKit(t *testing.T) {
	bin := kitBoard(t)
	k := newKitDeck(t)
	store := filepath.Join(k.dir, "nvs")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(k.dir, "k.sock")

	// Paired for Wi-Fi over USB first.
	usb, _, unplug := startUSBBoard(t, k, bin, "--serial", "--store", store, "--seconds", "30")
	k.waitFor("registered", func() bool { _, ok := k.srv.Devices.Get(kitDev); return ok })
	if err := k.srv.Devices.Approve(kitDev); err != nil {
		t.Fatal(err)
	}
	usb.wait("paired serial=", 5*time.Second)
	unplug()
	k.waitFor("off USB", func() bool { return len(k.hub.Links()) == 0 })

	// The leftover bond: the board and this computer are paired; the deck
	// has no binding.
	if err := os.WriteFile(filepath.Join(store, "ble_bond"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	wifi, _ := startBoard(t, bin, "--wifi", "--ble", sock, "--store", store, "--mtu", "185", "--seconds", "60")
	name, info := advertised(wifi)
	k.setBoards([]fakeBoard{{ID: periph1, Name: name, RSSI: -48, Info: info, Sock: "k.sock", MTU: 185, Paired: true}})
	wifi.wait("welcome name="+kitDev, 5*time.Second)
	onlyLink(k.rig, link.OverWifi)

	k.scanFor(name)
	var stages []string
	d, err := k.up.Add(context.Background(), periph1, devices.Act, func(stage, _ string) { stages = append(stages, stage) })
	if err != nil {
		t.Fatalf("add: %v\n%s", err, wifi.all())
	}
	if strings.Join(stages, ",") != "connect,connected,done" {
		t.Fatalf("stages (no passkey with a bond): %v", stages)
	}
	if d.BLE != periph1 {
		t.Fatalf("device: %+v", d)
	}
	wifi.wait("ble standby", time.Second)
	wifi.wait("ble disconnected", 5*time.Second)
	if wifi.count("ble carries") != 0 {
		t.Fatalf("Bluetooth carried the session beside Wi-Fi:\n%s", wifi.all())
	}
	k.waitFor("Bluetooth stands by", func() bool { ls := k.up.State(d); return ls.State == "standing by" && ls.Uses == link.OverWifi })
	audit, _ := os.ReadFile(k.srv.Audit.Path)
	if !strings.Contains(string(audit), "confirmed by its Wi-Fi session") {
		t.Fatalf("audit:\n%s", audit)
	}
}
