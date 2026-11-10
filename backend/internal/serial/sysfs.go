package serial

import (
	"os"
	"path/filepath"
	"strings"
)

// listSysfs reads <sys>/class/tty: every tty with a USB device above it
// (ttyACM*, ttyUSB*, and others behind USB), with the device's ids. Built
// for Linux, plain file reads, so it is tested everywhere.
func listSysfs(sys string) ([]Info, error) {
	if r, err := filepath.EvalSymlinks(sys); err == nil {
		sys = r
	}
	entries, err := os.ReadDir(filepath.Join(sys, "class", "tty"))
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		name := e.Name()
		dev, err := filepath.EvalSymlinks(filepath.Join(sys, "class", "tty", name, "device"))
		if err != nil {
			continue // virtual consoles and ptys have no device
		}
		info := Info{Path: "/dev/" + name}
		found := false
		for d := dev; strings.HasPrefix(d, sys) && len(d) > len(sys); d = filepath.Dir(d) {
			if vid := readSys(d, "idVendor"); vid != "" {
				info.VID, info.PID = vid, readSys(d, "idProduct")
				info.Serial, info.Product = readSys(d, "serial"), readSys(d, "product")
				found = true
				break
			}
		}
		if !found && !strings.HasPrefix(name, "ttyACM") && !strings.HasPrefix(name, "ttyUSB") {
			continue // ttyS*: built-in UARTs, mostly not there at all
		}
		out = append(out, info)
	}
	return out, nil
}

func readSys(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
