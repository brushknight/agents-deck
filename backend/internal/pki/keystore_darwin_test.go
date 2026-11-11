package pki

import (
	"os"
	"path/filepath"
	"testing"
)

// Only the usual data folder may use the plain Keychain names: any other
// folder would otherwise replace the real deck's CA key.
func TestKeychainNamesArePerDataFolder(t *testing.T) {
	home, _ := os.UserHomeDir()
	usual := filepath.Join(home, ".local", "share", "agents-terminal")
	if s := keychainSuffix(usual); s != "" {
		t.Fatalf("usual folder: suffix %q, want none", s)
	}
	a, b := keychainSuffix(t.TempDir()), keychainSuffix(t.TempDir())
	if a == "" || b == "" || a == b {
		t.Fatalf("other folders need their own names: %q %q", a, b)
	}
}
