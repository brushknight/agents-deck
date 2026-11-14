// Package paths locates the daemon's private files. Everything lives in one
// 0700 directory; secrets inside it are 0600.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
)

func Data() string {
	if d := os.Getenv("AGENTSTERM_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agents-terminal")
}

func Ensure() error { return os.MkdirAll(Data(), 0o700) }

// Socket is the daemon's unix socket. macOS caps socket paths at 104 bytes, so
// a deep data dir falls back to the per-user private $TMPDIR.
func Socket() string {
	p := filepath.Join(Data(), "agentd.sock")
	if len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(Data()))
	return filepath.Join(os.TempDir(), "agentsterm-"+hex.EncodeToString(sum[:4])+".sock")
}

func Agents() string     { return filepath.Join(Data(), "agents.json") }
func Config() string     { return filepath.Join(Data(), "config.json") }
func WebToken() string   { return filepath.Join(Data(), "web-session") }
func CACert() string     { return filepath.Join(Data(), "ca.pem") }          // the deck's CA (public)
func ServerCert() string { return filepath.Join(Data(), "server-cert.pem") } // network listener, signed by the CA
func ServerKey() string  { return filepath.Join(Data(), "server-key.pem") }
func Devices() string    { return filepath.Join(Data(), "devices.json") } // paired devices
func Audit() string      { return filepath.Join(Data(), "audit.log") }    // one line per action, by device
func HooksFile() string  { return filepath.Join(Data(), "claude-hooks.json") }
func Log() string        { return filepath.Join(Data(), "agentd.log") }

// Lsof is the lsof binary: macOS keeps it in /usr/sbin, most Linux
// distributions in /usr/bin. The daemon may run with a minimal PATH (launchd,
// systemd), so the usual places come first.
func Lsof() string {
	for _, p := range []string{"/usr/sbin/lsof", "/usr/bin/lsof"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("lsof"); err == nil {
		return p
	}
	return "lsof"
}
