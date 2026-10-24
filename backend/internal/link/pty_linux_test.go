package link

import (
	"os"
	"strconv"
	"unsafe"
)

// openPTY opens a pseudo-terminal: its master, and the slave's path.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, "", err
	}
	const tiocsptlck, tiocgptn = 0x40045431, 0x80045430
	var unlock, n int32
	if err := ioctl(int(m.Fd()), tiocsptlck, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		return nil, "", err
	}
	if err := ioctl(int(m.Fd()), tiocgptn, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		return nil, "", err
	}
	return m, "/dev/pts/" + strconv.Itoa(int(n)), nil
}
