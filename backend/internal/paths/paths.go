// Package paths locates the daemon's private files. Everything lives in one
// 0700 directory; secrets inside it are 0600.
//
// On Windows those modes mean little (Go only maps them to the read-only
// flag). The folder is %LOCALAPPDATA%\agents-deck, under the user's own
// profile, which Windows already keeps private to that user (and to
// administrators): we rely on that and set no ACLs of our own.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Data is the folder of the daemon's files: $AGENTSTERM_HOME when set, else
// the platform's default (see defaultData).
func Data() string {
	if d := os.Getenv("AGENTSTERM_HOME"); d != "" {
		return d
	}
	return defaultData()
}

func Ensure() error { return os.MkdirAll(Data(), 0o700) }

// Socket is the daemon's unix socket. macOS caps socket paths at 104 bytes
// (Linux and Windows at 108), so a deep data dir falls back to the per-user
// private temp folder ($TMPDIR on macOS, %TEMP% under the profile on Windows).
// Windows 10 (1803) and later have unix sockets; Go uses them the same way.
func Socket() string {
	p := filepath.Join(Data(), "agentd.sock")
	if len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(Data()))
	return filepath.Join(os.TempDir(), "agentsterm-"+hex.EncodeToString(sum[:4])+".sock")
}

func Agents() string          { return filepath.Join(Data(), "agents.json") }
func Config() string          { return filepath.Join(Data(), "config.json") }
func WebToken() string        { return filepath.Join(Data(), "web-session") }
func CACert() string          { return filepath.Join(Data(), "ca.pem") }          // the deck's CA (public)
func ServerCert() string      { return filepath.Join(Data(), "server-cert.pem") } // network listener, signed by the CA
func ServerKey() string       { return filepath.Join(Data(), "server-key.pem") }
func Devices() string         { return filepath.Join(Data(), "devices.json") }      // paired devices
func MCPTokens() string       { return filepath.Join(Data(), "mcp-tokens.json") }   // MCP tokens (hashes only)
func Audit() string           { return filepath.Join(Data(), "audit.log") }         // one line per action, by device
func Pets() string            { return filepath.Join(Data(), "pets.json") }         // agent pets (Settings > General)
func PromptQueue() string     { return filepath.Join(Data(), "prompt-queue.json") } // prompts waiting for a busy agent
func Slowed() string          { return filepath.Join(Data(), "slowed.json") }       // processes the CPU guard put in background mode
func Limits() string          { return filepath.Join(Data(), "limits.json") }       // usage limits, as last reported
func StatusLineSaved() string { return filepath.Join(Data(), "statusline-saved") }  // status lines the deck's replaced in accounts' settings
func HooksFile() string       { return filepath.Join(Data(), "claude-hooks.json") }
func Log() string             { return filepath.Join(Data(), "agentd.log") }
func PID() string             { return filepath.Join(Data(), "agentd.pid") } // the running service's process id
