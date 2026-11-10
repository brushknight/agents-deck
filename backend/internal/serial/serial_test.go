package serial

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLikely(t *testing.T) {
	cases := map[[2]string]string{
		{"303a", "1001"}: "esp32-usb",
		{"303A", "0009"}: "esp32-usb",
		{"1a86", "55d4"}: "ch340",
		{"10c4", "ea60"}: "cp210x",
		{"0403", "6001"}: "ftdi",
		{"05ac", "1234"}: "",
		{"", ""}:         "",
	}
	for in, want := range cases {
		if got := Likely(in[0], in[1]); got != want {
			t.Errorf("Likely(%s:%s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestTimeoutDeciseconds(t *testing.T) {
	for d, want := range map[time.Duration]uint8{time.Millisecond: 1, 100 * time.Millisecond: 1, 101 * time.Millisecond: 2, time.Minute: 255} {
		if got := timeoutDeciseconds(d); got != want {
			t.Errorf("%v: %d, want %d", d, got, want)
		}
	}
}

// Made-up ioreg output: a hub with a native-USB ESP32-S3 behind it and a
// CH340 board on its own. The port belongs to the nearest device above it,
// never to the hub.
const ioregFixture = `+-o Example Hub@00100000  <class IOUSBHostDevice, id 0x100000001, registered, matched, active, busy 0 (1 ms), retain 40>
  |   "idProduct" = 22529
  |   "USB Product Name" = "Example Hub"
  |   "idVendor" = 7516
  +-o AppleUSB20Hub@00100000  <class AppleUSB20Hub, id 0x100000002, registered, matched, active, busy 0 (1 ms), retain 30>
  | +-o AppleUSB20HubPort@00110000  <class AppleUSB20HubPort, id 0x100000003, registered, matched, active, busy 0 (1 ms), retain 15>
  | | +-o USB JTAG/serial debug unit@00110000  <class IOUSBHostDevice, id 0x100000004, registered, matched, active, busy 0 (1 ms), retain 60>
  | |   |   "idProduct" = 4097
  | |   |   "USB Product Name" = "USB JTAG_serial debug unit"
  | |   |   "idVendor" = 12346
  | |   |   "USB Serial Number" = "AA:BB:CC:00:11:22"
  | |   +-o IOUSBHostInterface@1  <class IOUSBHostInterface, id 0x100000005, registered, matched, active, busy 0 (1 ms), retain 13>
  | |     | |   "idProduct" = 4097
  | |     | |   "idVendor" = 12346
  | |     | +-o AppleUSBACMData  <class AppleUSBACMData, id 0x100000006, registered, matched, active, busy 0 (1 ms), retain 7>
  | |     | | +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x100000007, registered, matched, active, busy 0 (1 ms), retain 6>
  | |     | |     "IOCalloutDevice" = "/dev/cu.usbmodem1101"
  | |     | |     "IODialinDevice" = "/dev/tty.usbmodem1101"
+-o USB Serial@00200000  <class IOUSBHostDevice, id 0x100000010, registered, matched, active, busy 0 (1 ms), retain 30>
  |   "idProduct" = 29987
  |   "kUSBProductString" = "USB Serial"
  |   "idVendor" = 6790
  +-o IOUSBHostInterface@0  <class IOUSBHostInterface, id 0x100000011, registered, matched, active, busy 0 (1 ms), retain 10>
    +-o WCHSerialDriver  <class WCHSerialDriver, id 0x100000012, registered, matched, active, busy 0 (1 ms), retain 8>
      +-o IOSerialBSDClient  <class IOSerialBSDClient, id 0x100000013, registered, matched, active, busy 0 (1 ms), retain 6>
          "IOCalloutDevice" = "/dev/cu.wchusbserial210"
`

func TestParseIoreg(t *testing.T) {
	got := parseIoreg(ioregFixture)
	want := map[string]Info{
		"/dev/cu.usbmodem1101":    {Path: "/dev/cu.usbmodem1101", VID: "303a", PID: "1001", Serial: "AA:BB:CC:00:11:22", Product: "USB JTAG_serial debug unit"},
		"/dev/cu.wchusbserial210": {Path: "/dev/cu.wchusbserial210", VID: "1a86", PID: "7523", Product: "USB Serial"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// A made-up /sys: ttyACM0 behind an ESP32-S3, ttyUSB0 behind a CP2102 (one
// level deeper, as usb-serial drivers put it), ttyS0 without USB (left out),
// tty0 without a device.
func TestListSysfs(t *testing.T) {
	sys := t.TempDir()
	mk := func(dir string, files map[string]string) {
		if err := os.MkdirAll(filepath.Join(sys, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for k, v := range files {
			if err := os.WriteFile(filepath.Join(sys, dir, k), []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	link := func(tty, target string) {
		mk("class/tty/"+tty, nil)
		if target != "" {
			if err := os.Symlink(filepath.Join(sys, target), filepath.Join(sys, "class/tty", tty, "device")); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("devices/usb1/1-1", map[string]string{"idVendor": "303a", "idProduct": "1001", "serial": "AA:BB:CC:00:11:22", "product": "USB JTAG/serial debug unit"})
	mk("devices/usb1/1-1/1-1:1.0", nil)
	mk("devices/usb1/1-2", map[string]string{"idVendor": "10c4", "idProduct": "ea60", "product": "CP2102 USB to UART Bridge Controller"})
	mk("devices/usb1/1-2/1-2:1.0/ttyUSB0", nil)
	mk("devices/platform/serial8250", nil)
	link("ttyACM0", "devices/usb1/1-1/1-1:1.0")
	link("ttyUSB0", "devices/usb1/1-2/1-2:1.0/ttyUSB0")
	link("ttyS0", "devices/platform/serial8250")
	link("tty0", "")
	got, err := listSysfs(sys)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{Path: "/dev/ttyACM0", VID: "303a", PID: "1001", Serial: "AA:BB:CC:00:11:22", Product: "USB JTAG/serial debug unit"},
		{Path: "/dev/ttyUSB0", VID: "10c4", PID: "ea60", Product: "CP2102 USB to UART Bridge Controller"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseWinHardwareID(t *testing.T) {
	cases := []struct{ key, vid, pid, serial string }{
		{`VID_303A&PID_1001&MI_00`, "303a", "1001", ""},
		{`VID_1A86&PID_55D4`, "1a86", "55d4", ""},
		{`VID_0403+PID_6001+A1B2C3D4A`, "0403", "6001", "A1B2C3D4A"},
		{`ROOT_HUB30`, "", "", ""},
	}
	for _, c := range cases {
		v, p, s := parseWinHardwareID(c.key)
		if v != c.vid || p != c.pid || s != c.serial {
			t.Errorf("%s: %s %s %s", c.key, v, p, s)
		}
	}
	if s := winInstanceSerial("5&1a2b3c4d&0&1"); s != "" {
		t.Errorf("made-up instance id taken as serial: %q", s)
	}
	if s := winFriendly("USB Serial Device (COM5)", "COM5"); s != "USB Serial Device" {
		t.Errorf("friendly: %q", s)
	}
}
