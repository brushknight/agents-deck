package link

import (
	"os"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// ErrUnsupported: serial ports on this platform aren't implemented yet.
var ErrUnsupported = serial.ErrUnsupported

// OpenPort opens a serial port raw, 8N1, no flow control, at baud, for
// blocking reads. The descriptor is close-on-exec.
func OpenPort(path string, baud int) (*os.File, error) { return serial.OpenFile(path, baud) }
