package serial

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// list globs the callout devices and asks ioreg (no cgo, no port opened)
// which USB device each belongs to.
func list() ([]Info, error) {
	paths, err := filepath.Glob("/dev/cu.*")
	if err != nil {
		return nil, err
	}
	usb := usbIDs(paths)
	out := make([]Info, 0, len(paths))
	for _, p := range paths {
		info, ok := usb[p]
		if !ok {
			info = Info{}
		}
		info.Path = p
		out = append(out, info)
	}
	return out, nil
}

// ioreg reads the whole USB tree: a few hundred ms of CPU, and Settings
// and the tray app ask for the ports every 10 seconds. Its answer is kept
// until a serial device comes or goes: the device nodes and their device
// numbers (each device plugged in gets a new one, even on the same port;
// the nodes' times change with every write, so they can't tell).
var ioregCache struct {
	sync.Mutex
	key string
	ids map[string]Info
}

func usbIDs(paths []string) map[string]Info {
	key := devicesKey(paths, os.Stat)
	ioregCache.Lock()
	defer ioregCache.Unlock()
	if ioregCache.ids != nil && key == ioregCache.key {
		return ioregCache.ids
	}
	usb := map[string]Info{}
	out, err := proc.Output(5*time.Second, "/usr/sbin/ioreg", "-r", "-c", "IOUSBHostDevice", "-l")
	if err == nil {
		usb = parseIoreg(string(out))
		ioregCache.key, ioregCache.ids = key, usb
	}
	return usb
}

// devicesKey names a set of device nodes and their device numbers.
func devicesKey(paths []string, stat func(string) (os.FileInfo, error)) string {
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		dev := ""
		if st, err := stat(p); err == nil {
			if s, ok := st.Sys().(*syscall.Stat_t); ok {
				dev = strconv.FormatInt(int64(s.Rdev), 16)
			}
		}
		parts = append(parts, p+"@"+dev)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
