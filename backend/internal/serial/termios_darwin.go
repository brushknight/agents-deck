package serial

import (
	"syscall"
	"unsafe"
)

const (
	ioctlGet = syscall.TIOCGETA
	ioctlSet = syscall.TIOCSETA
	// iossiospeed is IOSSIOSPEED, _IOW('T', 2, speed_t): any baud rate.
	iossiospeed = 0x80085402
	crtscts     = 0x30000 // CCTS_OFLOW | CRTS_IFLOW: hardware flow control
)

var standardBauds = map[int]bool{
	9600: true, 19200: true, 38400: true, 57600: true, 115200: true, 230400: true,
}

// setSpeed writes t with the rate, then sets the exact rate with IOSSIOSPEED
// (macOS takes any rate that way). Pseudo-terminals refuse IOSSIOSPEED; with
// strict off a standard rate set through termios is enough.
func setSpeed(fd int, t *syscall.Termios, baud int, strict bool) error {
	std := standardBauds[baud]
	t.Ispeed, t.Ospeed = syscall.B115200, syscall.B115200
	if std && !strict {
		t.Ispeed, t.Ospeed = uint64(baud), uint64(baud)
	}
	if err := ioctl(fd, ioctlSet, uintptr(unsafe.Pointer(t))); err != nil {
		return err
	}
	speed := uint64(baud)
	if err := ioctl(fd, iossiospeed, uintptr(unsafe.Pointer(&speed))); err != nil && (strict || !std) {
		return err
	}
	return nil
}
