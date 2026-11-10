package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Unit is the systemd user unit of the service.
const Unit = "agents-deck.service"

// Systemd starts the service at login on Linux with a systemd user unit.
// Without systemd it says how to run the service by hand.
type Systemd struct {
	Exe, Home string
	Run       Runner
	LookPath  func(string) (string, error) // exec.LookPath; tests fake it
}

// UnitPath is the unit file.
func (s *Systemd) UnitPath() string {
	return filepath.Join(s.Home, ".config", "systemd", "user", Unit)
}

// UnitFile is the unit, as install.sh has written it.
func (s *Systemd) UnitFile() string {
	return `[Unit]
Description=agents deck daemon
After=default.target

[Service]
ExecStart=` + systemdArg(s.Exe) + ` serve
Restart=on-failure
Environment=PATH=` + s.Home + `/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

[Install]
WantedBy=default.target
`
}

// systemdArg quotes a path for ExecStart when it needs it (spaces, quotes)
// and doubles "%", which systemd would otherwise expand.
func systemdArg(p string) string {
	p = strings.ReplaceAll(p, "%", "%%")
	if !strings.ContainsAny(p, " \t\"'\\") {
		return p
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
}

func (s *Systemd) hasSystemd() bool {
	look := s.LookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look("systemctl")
	return err == nil
}

func (s *Systemd) ctl(args ...string) ([]byte, error) {
	return s.Run("systemctl", append([]string{"--user"}, args...)...)
}

// Install writes the unit, enables it and (re)starts the service. When
// systemd is missing or refuses (no user session, a container), it says how
// to start the service instead: that is not an error.
func (s *Systemd) Install(out io.Writer) error {
	if !s.hasSystemd() {
		fmt.Fprintln(out, "no systemd found: start the service yourself with: agentctl serve")
		return nil
	}
	path := s.UnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(s.UnitFile()), 0o644); err != nil {
		return err
	}
	_, _ = s.ctl("daemon-reload")
	if _, err := s.ctl("enable", Unit); err == nil {
		if _, err = s.ctl("restart", Unit); err == nil {
			fmt.Fprintf(out, "installed %s (systemd user service, running)\n", path)
			return nil
		}
	}
	fmt.Fprintf(out, "systemd user unit written to %s; start it with:\n", path)
	fmt.Fprintf(out, "  systemctl --user enable --now %s\n", Unit)
	fmt.Fprintln(out, "(or run it in a terminal: agentctl serve)")
	return nil
}

// Uninstall stops and disables the service and removes the unit.
func (s *Systemd) Uninstall(out io.Writer) error {
	path := s.UnitPath()
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(out, "not installed: nothing to remove")
		return nil
	}
	if s.hasSystemd() {
		_, _ = s.ctl("disable", "--now", Unit)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if s.hasSystemd() {
		_, _ = s.ctl("daemon-reload")
	}
	fmt.Fprintln(out, "stopped and removed", path)
	return nil
}

// Registration: the unit file, and whether systemd starts it at login.
func (s *Systemd) Registration() Registration {
	r := Registration{Kind: "systemd", Where: s.UnitPath()}
	cur, err := os.ReadFile(s.UnitPath())
	if err != nil {
		return r
	}
	r.Registered = true
	switch {
	case string(cur) != s.UnitFile():
		r.Note = "the unit is for another agentctl or an older version: agentctl install updates it"
	case !s.hasSystemd():
		r.Note = "systemd is not available here, so the unit does nothing"
	default:
		if b, err := s.ctl("is-enabled", Unit); err != nil {
			r.Note = "not enabled (" + strings.TrimSpace(string(b)) + "): agentctl install enables it"
		}
	}
	return r
}
