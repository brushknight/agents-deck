// Package paths locates the daemon's private files. Everything lives in one
// 0700 directory; secrets inside it are 0600.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
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

func Agents() string      { return filepath.Join(Data(), "agents.json") }
func Config() string      { return filepath.Join(Data(), "config.json") }
func DeviceToken() string { return filepath.Join(Data(), "device-token") }
func WebToken() string    { return filepath.Join(Data(), "web-session") }
func TLSCert() string     { return filepath.Join(Data(), "tls-cert.pem") }
func TLSKey() string      { return filepath.Join(Data(), "tls-key.pem") }
func HooksFile() string   { return filepath.Join(Data(), "claude-hooks.json") }
func Log() string         { return filepath.Join(Data(), "agentd.log") }
