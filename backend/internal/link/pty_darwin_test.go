package link

import (
	"bytes"
	"os"
	"unsafe"
)

// openPTY opens a pseudo-terminal: its master, and the slave's path.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, "", err
	}
	const tiocptygrant, tiocptyunlk, tiocptygname = 0x20007454, 0x20007452, 0x40807453
	var name [128]byte
	for _, req := range []uintptr{tiocptygrant, tiocptyunlk} {
		if err := ioctl(int(m.Fd()), req, 0); err != nil {
			m.Close()
			return nil, "", err
		}
	}
	if err := ioctl(int(m.Fd()), tiocptygname, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		m.Close()
		return nil, "", err
	}
	return m, string(name[:bytes.IndexByte(name[:], 0)]), nil
}
