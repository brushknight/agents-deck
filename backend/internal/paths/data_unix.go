//go:build !windows

package paths

import (
	"os"
	"path/filepath"
)

// defaultData on macOS and Linux: ~/.local/share/agents-terminal (the old
// name stays, so existing installs keep their devices and certificates).
func defaultData() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agents-terminal")
}
