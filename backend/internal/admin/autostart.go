package admin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TrayAutostart is the Linux tray app's XDG autostart entry, the one
// install.sh --autostart writes: Start when I log in turns it on and off
// with the service. (On macOS the app's own login item stays with the app.)
type TrayAutostart struct {
	Home string
	// Sources are the app's launcher entries to copy, the first one found
	// wins (default: install.sh's, then the .deb's).
	Sources []string
}

const trayDesktop = "dev.agents-deck.menubar.desktop"

func (t *TrayAutostart) path() string {
	return filepath.Join(t.Home, ".config", "autostart", trayDesktop)
}

func (t *TrayAutostart) source() string {
	srcs := t.Sources
	if srcs == nil {
		srcs = []string{filepath.Join(t.Home, ".local", "share", "applications", trayDesktop), "/usr/share/applications/" + trayDesktop}
	}
	for _, s := range srcs {
		if fi, err := os.Stat(s); err == nil && !fi.IsDir() {
			return s
		}
	}
	return ""
}

// Available: the tray app is installed here.
func (t *TrayAutostart) Available() bool { return t != nil && t.source() != "" }

// Enabled: it starts at login.
func (t *TrayAutostart) Enabled() bool {
	_, err := os.Stat(t.path())
	return err == nil
}

// Set writes or removes the autostart entry.
func (t *TrayAutostart) Set(on bool) error {
	if !on {
		if err := os.Remove(t.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	src := t.source()
	if src == "" {
		return errors.New("the tray app isn't installed")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.HasPrefix(l, "X-GNOME-Autostart-enabled=") {
			continue
		}
		if strings.HasPrefix(l, "Exec=") && !strings.Contains(l, "--background") {
			l += " --background" // at login the tray may come up after the app: it waits
		}
		out = append(out, l)
	}
	out = append(out, "X-GNOME-Autostart-enabled=true")
	if err := os.MkdirAll(filepath.Dir(t.path()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(t.path(), []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
