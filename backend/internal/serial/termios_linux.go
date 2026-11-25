package serial

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	ioctlGet = syscall.TCGETS
	ioctlSet = syscall.TCSETS
	cbaud    = 0x100f
	crtscts  = 0x80000000
)

var linuxBauds = map[int]uint32{
	9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400, 57600: syscall.B57600,
	115200: syscall.B115200, 230400: syscall.B230400, 460800: syscall.B460800, 921600: syscall.B921600,
	1000000: syscall.B1000000, 1500000: syscall.B1500000, 2000000: syscall.B2000000,
}

func setSpeed(fd int, t *syscall.Termios, baud int, strict bool) error {
	rate, ok := linuxBauds[baud]
	if !ok {
		return fmt.Errorf("baud %d: use one of 9600 … 2000000", baud)
	}
	t.Cflag &^= cbaud
	t.Cflag |= rate
	t.Ispeed, t.Ospeed = rate, rate
	return ioctl(fd, ioctlSet, uintptr(unsafe.Pointer(t)))
}
