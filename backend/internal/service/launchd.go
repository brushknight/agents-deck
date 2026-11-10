package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Label is the launchd job of the service.
const Label = "dev.agents-deck.agentctl"

// Launchd starts the service at login on macOS with a LaunchAgent.
type Launchd struct {
	Exe, Home, Log string
	UID            int
	Run            Runner
	Sleep          func(time.Duration) // between bootout and bootstrap; tests skip it
}

// PlistPath is the LaunchAgent file.
func (l *Launchd) PlistPath() string {
	return filepath.Join(l.Home, "Library", "LaunchAgents", Label+".plist")
}

// Plist is the LaunchAgent. Keep it byte for byte as it is: a change makes
// macOS announce a new background item to every existing install.
func (l *Launchd) Plist() string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>serve</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>%s/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, l.Exe, l.Home, l.Log, l.Log)
}

func (l *Launchd) domain() string { return fmt.Sprintf("gui/%d", l.UID) }
func (l *Launchd) job() string    { return l.domain() + "/" + Label }

func (l *Launchd) loaded() bool {
	_, err := l.Run("launchctl", "print", l.job())
	return err == nil
}

// Install writes the LaunchAgent and loads it. When the same one is already
// loaded it only restarts the service (it picks up a rebuilt binary):
// registering again makes macOS announce a new background item every time.
func (l *Launchd) Install(out io.Writer) error {
	plist, content := l.PlistPath(), l.Plist()
	if cur, err := os.ReadFile(plist); err == nil && string(cur) == content && l.loaded() {
		if b, err := l.Run("launchctl", "kickstart", "-k", l.job()); err != nil {
			return fmt.Errorf("launchctl kickstart: %v: %s", err, b)
		}
		fmt.Fprintln(out, "restarted", Label)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return err
	}
	_, _ = l.Run("launchctl", "bootout", l.job())
	if l.Sleep != nil {
		l.Sleep(500 * time.Millisecond)
	}
	if b, err := l.Run("launchctl", "bootstrap", l.domain(), plist); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, b)
	}
	fmt.Fprintln(out, "installed", plist)
	return nil
}

// Uninstall stops the service and removes the LaunchAgent.
func (l *Launchd) Uninstall(out io.Writer) error {
	plist := l.PlistPath()
	_, statErr := os.Stat(plist)
	wasLoaded := l.loaded()
	if wasLoaded {
		if b, err := l.Run("launchctl", "bootout", l.job()); err != nil {
			return fmt.Errorf("launchctl bootout: %v: %s", err, b)
		}
	}
	if statErr == nil {
		if err := os.Remove(plist); err != nil {
			return err
		}
	}
	if statErr != nil && !wasLoaded {
		fmt.Fprintln(out, "not installed: nothing to remove")
		return nil
	}
	fmt.Fprintln(out, "stopped and removed", plist)
	return nil
}

// Registration: the LaunchAgent file, and whether launchd has it loaded.
func (l *Launchd) Registration() Registration {
	r := Registration{Kind: "launchd", Where: l.PlistPath()}
	cur, err := os.ReadFile(l.PlistPath())
	if err != nil {
		return r
	}
	r.Registered = true
	switch {
	case string(cur) != l.Plist():
		r.Note = "the LaunchAgent is for another agentctl or an older version: agentctl install updates it"
	case !l.loaded():
		r.Note = "the LaunchAgent is not loaded: agentctl install loads it"
	}
	return r
}
