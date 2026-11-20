package link

import (
	"io"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

// ErrUnsupported: serial ports on this platform aren't implemented yet.
var ErrUnsupported = serial.ErrUnsupported

// OpenPort opens a serial port raw, 8N1, no flow control, at baud, for
// blocking reads; Close may end a pending read on Windows. The descriptor is
// close-on-exec (an *os.File on macOS and Linux).
func OpenPort(path string, baud int) (io.ReadWriteCloser, error) { return serial.OpenFile(path, baud) }
