package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// New is a startup entry in the user's Run key.
func New(c Config) Manager {
	return &Startup{Exe: c.Exe, Log: c.Log, SystemRoot: os.Getenv("SystemRoot"), Reg: runKey{},
		Start: startDetached, Stop: func() (bool, error) { return stopService(c.PIDFile, c.Exe) }}
}

const (
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
	createNoWindow         = 0x08000000
)

// startDetached starts argv with no console window, in its own process
// group (Ctrl+C in this terminal doesn't reach it) and, when the terminal's
// job allows it, outside that job (closing the terminal doesn't end it).
func startDetached(argv []string) error {
	start := func(flags uint32) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = filepath.Dir(argv[0])
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	base := uint32(createNoWindow | createNewProcessGroup)
	if err := start(base | createBreakawayFromJob); err == nil {
		return nil
	}
	return start(base) // the job doesn't allow breaking away
}

// stopService ends the service whose process id is in pidFile. Windows has
// no signal for a clean stop of a windowless process, so it is terminated;
// the service saves its state within half a second of every change, so
// nothing of note is lost. A process id now used by some other program is
// left alone: the image must be an agentctl.
func stopService(pidFile, exe string) (bool, error) {
	pid, ok := ReadPID(pidFile)
	if !ok {
		return false, nil
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE|syscall.SYNCHRONIZE|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		_ = os.Remove(pidFile) // gone (or not ours to stop)
		return false, nil
	}
	defer syscall.CloseHandle(h)
	img, err := imageName(h)
	if err != nil || !isAgentctl(img, exe) {
		_ = os.Remove(pidFile)
		return false, nil
	}
	if err := syscall.TerminateProcess(h, 1); err != nil {
		return false, err
	}
	_, _ = syscall.WaitForSingleObject(h, uint32((5 * time.Second).Milliseconds()))
	_ = os.Remove(pidFile)
	return true, nil
}

// isAgentctl: the running image is this agentctl, or one by the same name
// (an older copy elsewhere).
func isAgentctl(img, exe string) bool {
	return strings.EqualFold(img, exe) || strings.EqualFold(filepath.Base(img), filepath.Base(exe)) ||
		strings.EqualFold(filepath.Base(img), "agentctl.exe")
}

const processQueryLimitedInformation = 0x1000

var (
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	advapi32                      = syscall.NewLazyDLL("advapi32.dll")
	procRegSetValueEx             = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue            = advapi32.NewProc("RegDeleteValueW")
)

func imageName(h syscall.Handle) (string, error) {
	buf := make([]uint16, syscall.MAX_LONG_PATH)
	n := uint32(len(buf))
	r, _, err := procQueryFullProcessImageName.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// runKey is HKCU\Software\Microsoft\Windows\CurrentVersion\Run, through
// the Win32 registry calls (no administrator rights needed).
type runKey struct{}

const runPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func (runKey) open(access uint32) (syscall.Handle, error) {
	var k syscall.Handle
	p, _ := syscall.UTF16PtrFromString(runPath)
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, p, 0, access, &k); err != nil {
		return 0, err
	}
	return k, nil
}

func (r runKey) Get(name string) (string, bool, error) {
	k, err := r.open(syscall.KEY_QUERY_VALUE)
	if err != nil {
		return "", false, err
	}
	defer syscall.RegCloseKey(k)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32
	err = syscall.RegQueryValueEx(k, n, nil, &typ, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return "", true, nil
	}
	buf := make([]uint16, size/2+1)
	size = uint32(len(buf) * 2)
	if err := syscall.RegQueryValueEx(k, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return "", false, err
	}
	return syscall.UTF16ToString(buf), true, nil
}

func (r runKey) Set(name, value string) error {
	k, err := r.open(syscall.KEY_SET_VALUE)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(k)
	n, _ := syscall.UTF16PtrFromString(name)
	v, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	ret, _, _ := procRegSetValueEx.Call(uintptr(k), uintptr(unsafe.Pointer(n)), 0, syscall.REG_SZ,
		uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	if ret != 0 {
		return syscall.Errno(ret)
	}
	return nil
}

func (r runKey) Delete(name string) error {
	k, err := r.open(syscall.KEY_SET_VALUE)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(k)
	n, _ := syscall.UTF16PtrFromString(name)
	ret, _, _ := procRegDeleteValue.Call(uintptr(k), uintptr(unsafe.Pointer(n)))
	if ret != 0 && syscall.Errno(ret) != syscall.ERROR_FILE_NOT_FOUND {
		return syscall.Errno(ret)
	}
	return nil
}
