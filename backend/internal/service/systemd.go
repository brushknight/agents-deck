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
	User      string // the user's name, for linger and NixOS's per-user profile ("" skips both)
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
Environment=PATH=` + s.unitPath() + `

[Install]
WantedBy=default.target
`
}

// unitPath is the service's PATH: a systemd user service starts with almost
// none, and agents need claude, codex, tmux and git. NixOS keeps programs in
// profiles (no /usr/bin), so those come too; folders that don't exist on a
// machine are harmless.
func (s *Systemd) unitPath() string {
	dirs := []string{s.Home + "/.local/bin"}
	if s.User != "" {
		dirs = append(dirs, s.Home+"/.nix-profile/bin", "/etc/profiles/per-user/"+s.User+"/bin")
	}
	dirs = append(dirs, "/run/wrappers/bin", "/run/current-system/sw/bin", "/nix/var/nix/profiles/default/bin",
		"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	return strings.Join(dirs, ":")
}

// Linger reports whether systemd keeps this user's services running when
// they aren't logged in (loginctl enable-linger). Without it the service and
// every agent stop at logout, which a machine without a desktop session
// (a server) can't live with. known is false when loginctl can't say.
func (s *Systemd) Linger() (on, known bool) {
	if s.User == "" || s.Run == nil {
		return false, false
	}
	b, err := s.Run("loginctl", "show-user", s.User, "--property=Linger", "--value")
	if err != nil {
		return false, false
	}
	v := strings.TrimSpace(string(b))
	return v == "yes", v == "yes" || v == "no"
}

// LingerHint says how to turn linger on, with NixOS's declarative way too.
func (s *Systemd) LingerHint() string {
	return "the service and its agents stop when you log out: run `loginctl enable-linger " + s.User +
		"` (on NixOS set `users.users." + s.User + ".linger = true;` in your configuration)"
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
			if on, known := s.Linger(); known && !on {
				fmt.Fprintln(out, "note:", s.LingerHint())
			}
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
