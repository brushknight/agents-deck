// Package service registers agentctl's service (`agentctl serve`) to start
// when the user logs in, starts and stops it, and says whether it is
// registered: launchd on macOS, a systemd user unit on Linux, a startup
// entry in the user's registry (HKCU ...\Run) on Windows. It never needs
// administrator rights: everything is per user.
package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// Manager is one platform's way of starting the service at login.
type Manager interface {
	// Install registers the service and starts it (restarts it when it
	// already runs, so a new agentctl binary takes over).
	Install(out io.Writer) error
	// Uninstall stops the service and removes its registration.
	Uninstall(out io.Writer) error
	// Registration says whether and how the service is registered.
	Registration() Registration
}

// Registration describes how the service starts at login.
type Registration struct {
	Kind       string `json:"kind"`           // "launchd", "systemd", "startup entry"
	Registered bool   `json:"registered"`     // set up to start at login
	Where      string `json:"where"`          // the plist, the unit file, the registry value
	Note       string `json:"note,omitempty"` // e.g. registered for another agentctl
}

// Config is what every platform needs to know.
type Config struct {
	Exe     string // the real path of the agentctl to run (see Executable)
	Home    string // the user's home folder
	Data    string // the service's data folder (paths.Data)
	Log     string // where the service's log goes (paths.Log)
	PIDFile string // where the running service writes its process id (paths.PID)
	// Exit shuts the running service down cleanly when it is this process
	// (Windows only: Settings restarting or stopping the service it runs in).
	Exit func()
}

// ErrUnsupported: this platform has no start-at-login support here.
var ErrUnsupported = errors.New("starting the service at login is not available on this platform: run agentctl serve")

// Runner runs a program and returns its combined output. Tests pass a fake;
// nothing in a test ever runs launchctl or systemctl.
type Runner func(name string, args ...string) ([]byte, error)

// Exec is the real Runner.
func Exec(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Executable is the real path of the running agentctl, symlinks resolved, so
// a copy bundled inside the app registers itself and not a link to it.
func Executable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Abs(p)
}

// WritePID records the running service's process id in path; the returned
// function removes it again (on a clean stop). Windows uses it to stop the
// service, which there has no launchd or systemd to do it.
//
// A file naming another live process is left as it is (a second serve that
// is about to fail on the busy ports must not take the first one's place).
func WritePID(path string) (remove func(), err error) {
	if pid, ok := ReadPID(path); ok && pid != os.Getpid() && proc.Alive(pid) {
		return func() {}, fmt.Errorf("%s names process %d, still running: is the service running twice?", path, pid)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return func() {}, err
	}
	return func() {
		if pid, ok := ReadPID(path); ok && pid == os.Getpid() {
			_ = os.Remove(path)
		}
	}, nil
}

// ReadPID reads a process id written by WritePID.
func ReadPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// WaitGone waits until process pid has exited, checking every tick, for at
// most timeout; it says whether the process is gone. A service started by
// the one it replaces (AfterFlag) waits like this before it starts.
func WaitGone(pid int, timeout, tick time.Duration, alive func(int) bool) bool {
	for end := time.Now().Add(timeout); alive(pid); time.Sleep(tick) {
		if time.Now().After(end) {
			return false
		}
	}
	return true
}
