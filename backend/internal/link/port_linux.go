package link

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	tcgets  = 0x5401
	tcsets  = 0x5402
	cbaud   = 0x100f
	crtscts = 0x80000000
)

var linuxBauds = map[int]uint32{
	9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400, 57600: syscall.B57600,
	115200: syscall.B115200, 230400: syscall.B230400, 460800: syscall.B460800, 921600: syscall.B921600,
}

func openPort(path string, baud int) (*os.File, error) {
	rate, ok := linuxBauds[baud]
	if !ok {
		return nil, fmt.Errorf("baud %d: use one of 9600 … 921600", baud)
	}
	// O_CLOEXEC: programs the daemon starts (tmux, CLIs) must not inherit the
	// port, or they hold it open and look like another user of it.
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var t syscall.Termios
	if err := ioctl(fd, tcgets, uintptr(unsafe.Pointer(&t))); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | crtscts | cbaud
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL | rate
	t.Ispeed, t.Ospeed = rate, rate
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	if err := ioctl(fd, tcsets, uintptr(unsafe.Pointer(&t))); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func ioctl(fd int, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg); e != 0 {
		return e
	}
	return nil
}
