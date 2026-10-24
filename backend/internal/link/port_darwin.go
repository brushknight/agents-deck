package link

import (
	"os"
	"syscall"
	"unsafe"
)

// iossiospeed is IOSSIOSPEED, _IOW('T', 2, speed_t): any baud rate.
const (
	iossiospeed = 0x80085402
	crtscts     = 0x30000 // CCTS_OFLOW | CRTS_IFLOW: hardware flow control
)

func openPort(path string, baud int) (*os.File, error) {
	// O_CLOEXEC: programs the daemon starts (tmux, CLIs) must not inherit the
	// port, or they hold it open and look like another user of it.
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var t syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t))); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	makeRaw(&t)
	t.Ispeed, t.Ospeed = syscall.B115200, syscall.B115200
	if err := ioctl(fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&t))); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	speed := uint64(baud)
	if err := ioctl(fd, iossiospeed, uintptr(unsafe.Pointer(&speed))); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	// Back to blocking reads: the session's reader goroutine waits for bytes.
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func makeRaw(t *syscall.Termios) {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | crtscts
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
}

func ioctl(fd int, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg); e != 0 {
		return e
	}
	return nil
}
