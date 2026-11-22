package serial

import (
	"path/filepath"
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
	usb := map[string]Info{}
	if out, err := proc.Output(5*time.Second, "/usr/sbin/ioreg", "-r", "-c", "IOUSBHostDevice", "-l"); err == nil {
		usb = parseIoreg(string(out))
	}
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
