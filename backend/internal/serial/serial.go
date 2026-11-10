// Package serial lists USB serial ports and opens them raw (8N1, no flow
// control), on macOS, Linux and Windows, with the standard library only.
package serial

import (
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

// Info describes a serial port as the system lists it. Listing never opens
// the port.
type Info struct {
	Path     string // /dev/cu.usbmodem1101, /dev/ttyACM0, COM5
	VID, PID string // lower-case hex, 4 digits, "" when unknown
	Serial   string
	Product  string
	Likely   string // "esp32-usb" (Espressif native USB), "ch340", "cp210x", "ftdi", "" = not a likely board
}

// Port is an open serial port.
//
// Read returns ErrTimeout when no byte arrived within the read timeout (zero,
// the default, waits for ever). Close must not be called while a Read with
// no timeout is pending.
type Port interface {
	io.ReadWriteCloser
	SetDTR(bool) error
	SetRTS(bool) error
	SetBaud(int) error
	SetReadTimeout(time.Duration) error
}

// ErrTimeout is what Read returns when the read timeout passed with no data.
var ErrTimeout = errors.New("serial: read timed out")

// ErrUnsupported: serial ports on this platform aren't implemented.
var ErrUnsupported = errors.New("serial ports aren't supported on this platform yet")

// List enumerates the serial ports, sorted by path. It never opens a port.
func List() ([]Info, error) {
	out, err := list()
	for i := range out {
		out[i].VID, out[i].PID = normID(out[i].VID), normID(out[i].PID)
		out[i].Likely = Likely(out[i].VID, out[i].PID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// Open opens path raw at baud, 8N1, no flow control.
func Open(path string, baud int) (Port, error) { return open(path, baud) }

// Likely names what a USB id probably is when it is a board this project
// flashes, or "".
func Likely(vid, pid string) string {
	switch normID(vid) {
	case "303a":
		return "esp32-usb"
	case "1a86":
		return "ch340"
	case "10c4":
		return "cp210x"
	case "0403":
		return "ftdi"
	}
	return ""
}

// LikelyName is Likely in words, for people.
func LikelyName(likely string) string {
	return map[string]string{
		"esp32-usb": "ESP32 native USB",
		"ch340":     "CH340/CH9102 USB serial",
		"cp210x":    "CP210x USB serial",
		"ftdi":      "FTDI USB serial",
	}[likely]
}

// Lookup returns what List knows about path (only Path set when it isn't
// listed).
func Lookup(path string) Info {
	ports, _ := List()
	for _, p := range ports {
		if p.Path == path || strings.EqualFold(p.Path, path) {
			return p
		}
	}
	return Info{Path: path}
}

// normID turns "0x303A", "303A" or "12346" (decimal from ioreg is converted by
// the caller) into 4 lower-case hex digits.
func normID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return ""
	}
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// timeoutDeciseconds rounds d up to tenths of a second for termios VTIME,
// clamped to 1..255.
func timeoutDeciseconds(d time.Duration) uint8 {
	ds := (d + 100*time.Millisecond - 1) / (100 * time.Millisecond)
	if ds < 1 {
		ds = 1
	}
	if ds > 255 {
		ds = 255
	}
	return uint8(ds)
}
