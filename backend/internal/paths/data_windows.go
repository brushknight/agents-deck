package paths

import (
	"os"
	"path/filepath"
)

// defaultData on Windows: %LOCALAPPDATA%\agents-deck (local, not roaming:
// certificates and the socket belong to this computer).
func defaultData() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(dir, "agents-deck")
}
