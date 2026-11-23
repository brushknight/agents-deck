package serial

import (
	"io/fs"
	"os"
	"syscall"
	"testing"
)

type fakeDev struct {
	fs.FileInfo
	rdev int32
}

func (f fakeDev) Sys() any { return &syscall.Stat_t{Rdev: f.rdev} }

// ioreg's answer is kept while the same device nodes are there; plugging a
// device in or out, or into the same port again (a new device number), asks
// again.
func TestDevicesKey(t *testing.T) {
	devs := map[string]int32{"/dev/cu.usbmodem101": 0x900002b, "/dev/cu.Bluetooth-Incoming-Port": 0x9000003}
	stat := func(p string) (os.FileInfo, error) {
		if d, ok := devs[p]; ok {
			return fakeDev{rdev: d}, nil
		}
		return nil, os.ErrNotExist
	}
	both := []string{"/dev/cu.usbmodem101", "/dev/cu.Bluetooth-Incoming-Port"}
	a := devicesKey(both, stat)
	if b := devicesKey([]string{both[1], both[0]}, stat); a != b {
		t.Error("the order changed the key")
	}
	devs["/dev/cu.usbmodem101"] = 0x900002f // plugged in again
	if b := devicesKey(both, stat); a == b {
		t.Error("a device plugged in again kept the key")
	}
	if b := devicesKey(both[1:], stat); a == b {
		t.Error("a device unplugged kept the key")
	}
}

// The real device nodes: the key holds still while ports are written to.
func TestDevicesKeyStable(t *testing.T) {
	paths := []string{"/dev/null", "/dev/zero"}
	a := devicesKey(paths, os.Stat)
	_ = os.WriteFile("/dev/null", []byte("x"), 0)
	if b := devicesKey(paths, os.Stat); a != b {
		t.Errorf("key changed after a write: %q, %q", a, b)
	}
}
