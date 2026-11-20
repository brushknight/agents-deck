//go:build darwin || linux

package serial

import (
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// OpenFile opens path raw at baud for a reader that blocks until bytes come
// (the Deck Link session). The descriptor is close-on-exec: programs the
// daemon starts (tmux, CLIs) must not inherit the port, or they hold it open
// and look like another user of it.
//
// It is an *os.File.
func OpenFile(path string, baud int) (io.ReadWriteCloser, error) {
	fd, err := openFD(path, baud, true)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func open(path string, baud int) (Port, error) {
	fd, err := openFD(path, baud, false)
	if err != nil {
		return nil, err
	}
	return &unixPort{fd: fd, path: path}, nil
}

// openFD opens and configures the port, back in blocking mode. strict makes a
// driver that refuses the exact baud rate an error even for standard rates
// (the Deck Link behaviour); otherwise a standard rate set through termios is
// enough (pseudo-terminals in tests).
func openFD(path string, baud int, strict bool) (int, error) {
	// O_NONBLOCK: don't wait for carrier detect while opening.
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	fail := func(err error) (int, error) {
		syscall.Close(fd)
		return -1, err
	}
	var t syscall.Termios
	if err := ioctl(fd, ioctlGet, uintptr(unsafe.Pointer(&t))); err != nil {
		return fail(err)
	}
	makeRaw(&t)
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	if err := setSpeed(fd, &t, baud, strict); err != nil {
		return fail(err)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		return fail(err)
	}
	return fd, nil
}

func makeRaw(t *syscall.Termios) {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | crtscts
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL
}

func ioctl(fd int, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg); e != 0 {
		return e
	}
	return nil
}

type unixPort struct {
	mu      sync.Mutex
	fd      int
	path    string
	timeout time.Duration
	closed  bool
}

func (p *unixPort) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.mu.Lock()
	fd, timeout, closed := p.fd, p.timeout, p.closed
	p.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}
	// VTIME holds at most 25.5 s: longer timeouts read again until they pass.
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		n, err := syscall.Read(fd, b)
		if err == syscall.EINTR || err == syscall.EAGAIN {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n > 0 {
			return n, nil
		}
		if timeout == 0 {
			// VMIN=1: zero bytes means the device went away.
			return 0, errors.New("serial: the port was closed or unplugged")
		}
		if !time.Now().Before(deadline) {
			return 0, ErrTimeout
		}
	}
}

func (p *unixPort) Write(b []byte) (int, error) {
	p.mu.Lock()
	fd, closed := p.fd, p.closed
	p.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}
	done := 0
	for done < len(b) {
		n, err := syscall.Write(fd, b[done:])
		if err == syscall.EINTR || err == syscall.EAGAIN {
			continue
		}
		if err != nil {
			return done, err
		}
		done += n
	}
	return done, nil
}

func (p *unixPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return syscall.Close(p.fd)
}

func (p *unixPort) modem(bit int, on bool) error {
	req := uintptr(syscall.TIOCMBIC)
	if on {
		req = syscall.TIOCMBIS
	}
	v := int32(bit)
	return ioctl(p.fd, req, uintptr(unsafe.Pointer(&v)))
}

func (p *unixPort) SetDTR(on bool) error { return p.modem(syscall.TIOCM_DTR, on) }
func (p *unixPort) SetRTS(on bool) error { return p.modem(syscall.TIOCM_RTS, on) }

func (p *unixPort) SetBaud(baud int) error {
	var t syscall.Termios
	if err := ioctl(p.fd, ioctlGet, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	return setSpeed(p.fd, &t, baud, false)
}

// SetReadTimeout: VMIN=0 and VTIME in tenths of a second, so a read returns
// as soon as one byte is there, or empty once the time is up.
func (p *unixPort) SetReadTimeout(d time.Duration) error {
	if d < 0 {
		d = 0
	}
	var t syscall.Termios
	if err := ioctl(p.fd, ioctlGet, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	if d == 0 {
		t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	} else {
		t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 0, timeoutDeciseconds(d)
	}
	if err := ioctl(p.fd, ioctlSet, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	p.mu.Lock()
	p.timeout = d
	p.mu.Unlock()
	return nil
}
