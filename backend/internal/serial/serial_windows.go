package serial

import (
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCommState       = kernel32.NewProc("GetCommState")
	procSetCommState       = kernel32.NewProc("SetCommState")
	procSetCommTimeouts    = kernel32.NewProc("SetCommTimeouts")
	procEscapeCommFunction = kernel32.NewProc("EscapeCommFunction")
	procPurgeComm          = kernel32.NewProc("PurgeComm")

	advapi32         = syscall.NewLazyDLL("advapi32.dll")
	procRegEnumValue = advapi32.NewProc("RegEnumValueW")
)

// dcb is the Win32 DCB structure.
type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	wReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	wReserved1 uint16
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

const (
	fBinary          = 1 << 0
	dtrControlEnable = 1 << 4 // fDtrControl = DTR_CONTROL_ENABLE
	rtsControlEnable = 1 << 12
	setRTS           = 3
	clrRTS           = 4
	setDTR           = 5
	clrDTR           = 6
	purgeRxClear     = 0x0008
	purgeTxClear     = 0x0004
	maxDWORD         = 0xFFFFFFFF
	errorNoMoreItems = 259
)

func comPath(path string) string {
	if strings.HasPrefix(path, `\\.\`) {
		return path
	}
	return `\\.\` + path
}

func openHandle(path string, baud int) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(comPath(path))
	if err != nil {
		return syscall.InvalidHandle, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return syscall.InvalidHandle, e
	}
	// 8N1, binary, no flow control; DTR and RTS asserted, as macOS and Linux
	// leave them after open.
	d.BaudRate = uint32(baud)
	d.Flags = fBinary | dtrControlEnable | rtsControlEnable
	d.ByteSize, d.Parity, d.StopBits = 8, 0, 0
	if r, _, e := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return syscall.InvalidHandle, e
	}
	if err := setTimeouts(h, 0); err != nil {
		syscall.CloseHandle(h)
		return syscall.InvalidHandle, err
	}
	procPurgeComm.Call(uintptr(h), purgeRxClear|purgeTxClear)
	return h, nil
}

// setTimeouts: a read returns as soon as any byte is there, or empty after d
// (zero: wait as long as Windows allows, about 49 days).
func setTimeouts(h syscall.Handle, d time.Duration) error {
	ms := uint32(maxDWORD - 1)
	if d > 0 {
		ms = uint32((d + time.Millisecond - 1) / time.Millisecond)
	}
	t := commTimeouts{ReadIntervalTimeout: maxDWORD, ReadTotalTimeoutMultiplier: maxDWORD, ReadTotalTimeoutConstant: ms}
	if r, _, e := procSetCommTimeouts.Call(uintptr(h), uintptr(unsafe.Pointer(&t))); r == 0 {
		return e
	}
	return nil
}

// OpenFile opens the port for a reader that blocks until bytes come (the Deck
// Link session).
func OpenFile(path string, baud int) (*os.File, error) {
	h, err := openHandle(path, baud)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func open(path string, baud int) (Port, error) {
	h, err := openHandle(path, baud)
	if err != nil {
		return nil, err
	}
	return &winPort{h: h}, nil
}

type winPort struct {
	mu      sync.Mutex
	h       syscall.Handle
	timeout time.Duration
	closed  bool
}

func (p *winPort) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.mu.Lock()
	h, timeout, closed := p.h, p.timeout, p.closed
	p.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}
	for {
		var n uint32
		if err := syscall.ReadFile(h, b, &n, nil); err != nil {
			return 0, err
		}
		if n > 0 {
			return int(n), nil
		}
		if timeout > 0 {
			return 0, ErrTimeout
		}
	}
}

func (p *winPort) Write(b []byte) (int, error) {
	p.mu.Lock()
	h, closed := p.h, p.closed
	p.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}
	done := 0
	for done < len(b) {
		var n uint32
		if err := syscall.WriteFile(h, b[done:], &n, nil); err != nil {
			return done, err
		}
		if n == 0 {
			return done, errors.New("serial: write made no progress")
		}
		done += int(n)
	}
	return done, nil
}

func (p *winPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return syscall.CloseHandle(p.h)
}

func (p *winPort) escape(fn uintptr) error {
	if r, _, e := procEscapeCommFunction.Call(uintptr(p.h), fn); r == 0 {
		return e
	}
	return nil
}

func (p *winPort) SetDTR(on bool) error {
	if on {
		return p.escape(setDTR)
	}
	return p.escape(clrDTR)
}

func (p *winPort) SetRTS(on bool) error {
	if on {
		return p.escape(setRTS)
	}
	return p.escape(clrRTS)
}

func (p *winPort) SetBaud(baud int) error {
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(p.h), uintptr(unsafe.Pointer(&d))); r == 0 {
		return e
	}
	d.BaudRate = uint32(baud)
	if r, _, e := procSetCommState.Call(uintptr(p.h), uintptr(unsafe.Pointer(&d))); r == 0 {
		return e
	}
	return nil
}

func (p *winPort) SetReadTimeout(d time.Duration) error {
	if d < 0 {
		d = 0
	}
	if err := setTimeouts(p.h, d); err != nil {
		return err
	}
	p.mu.Lock()
	p.timeout = d
	p.mu.Unlock()
	return nil
}

// list reads HARDWARE\DEVICEMAP\SERIALCOMM (the COM ports there are now) and
// finds each one's USB ids under SYSTEM\CurrentControlSet\Enum.
func list() ([]Info, error) {
	h, err := regOpen(syscall.HKEY_LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`)
	if err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil, nil // no serial ports at all
		}
		return nil, err
	}
	names := regStringValues(h)
	syscall.RegCloseKey(h)
	usb := usbPorts()
	out := make([]Info, 0, len(names))
	for _, n := range names {
		info, ok := usb[strings.ToUpper(n)]
		if !ok {
			info = Info{}
		}
		info.Path = n
		out = append(out, info)
	}
	return out, nil
}

// usbPorts maps COMn to the USB device that has it, from the device keys of
// the usual USB serial drivers.
func usbPorts() map[string]Info {
	res := map[string]Info{}
	for _, bus := range []string{"USB", "FTDIBUS"} {
		root, err := regOpen(syscall.HKEY_LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\`+bus)
		if err != nil {
			continue
		}
		for _, hwid := range regSubkeys(root) {
			vid, pid, ftdiSerial := parseWinHardwareID(hwid)
			if vid == "" {
				continue
			}
			hk, err := regOpen(root, hwid)
			if err != nil {
				continue
			}
			for _, inst := range regSubkeys(hk) {
				ik, err := regOpen(hk, inst)
				if err != nil {
					continue
				}
				params, err := regOpen(ik, "Device Parameters")
				if err == nil {
					port := strings.ToUpper(regString(params, "PortName"))
					syscall.RegCloseKey(params)
					if strings.HasPrefix(port, "COM") {
						serial := ftdiSerial
						if serial == "" {
							serial = winInstanceSerial(inst)
						}
						product := winFriendly(regString(ik, "FriendlyName"), port)
						res[port] = Info{VID: vid, PID: pid, Serial: serial, Product: product}
					}
				}
				syscall.RegCloseKey(ik)
			}
			syscall.RegCloseKey(hk)
		}
		syscall.RegCloseKey(root)
	}
	return res
}

func regOpen(root syscall.Handle, path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &h); err != nil {
		return 0, err
	}
	return h, nil
}

func regSubkeys(h syscall.Handle) []string {
	var out []string
	for i := uint32(0); ; i++ {
		buf := make([]uint16, 256)
		n := uint32(len(buf))
		if err := syscall.RegEnumKeyEx(h, i, &buf[0], &n, nil, nil, nil, nil); err != nil {
			return out
		}
		out = append(out, syscall.UTF16ToString(buf[:n]))
	}
}

func regString(h syscall.Handle, name string) string {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ, n uint32
	if err := syscall.RegQueryValueEx(h, p, nil, &typ, nil, &n); err != nil || (typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ) || n == 0 {
		return ""
	}
	buf := make([]uint16, n/2+1)
	if err := syscall.RegQueryValueEx(h, p, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n); err != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// regStringValues returns the data of every string value under h.
func regStringValues(h syscall.Handle) []string {
	var out []string
	for i := uint32(0); i < 1024; i++ {
		name := make([]uint16, 512)
		nameLen := uint32(len(name))
		data := make([]uint16, 256)
		dataLen := uint32(len(data) * 2)
		var typ uint32
		r, _, _ := procRegEnumValue.Call(uintptr(h), uintptr(i),
			uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)), 0,
			uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dataLen)))
		if r != 0 { // ERROR_NO_MORE_ITEMS, or a value too long to be a port name
			if r == errorNoMoreItems {
				return out
			}
			continue
		}
		if typ == syscall.REG_SZ {
			out = append(out, syscall.UTF16ToString(data[:dataLen/2]))
		}
	}
	return out
}
