package pki

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// keychainService names the deck's items in the login Keychain.
const keychainService = "dev.agents-deck"

// Keychain keeps secrets in the macOS login Keychain (as generic passwords,
// hex-encoded), through /usr/bin/security: a copied data folder can't mint
// device certificates.
//
// The login Keychain is one per user, not one per data folder. So only the
// usual data folder uses the plain item names; any other folder (a test, a
// demo, AGENTSTERM_HOME) gets names of its own and can never replace the
// real deck's keys.
type Keychain struct {
	suffix string // "" for the usual data folder, else "@<hash of the folder>"
}

// DefaultKeystore is the platform's store: the Keychain on macOS.
func DefaultKeystore(dataDir string) Keystore { return Keychain{suffix: keychainSuffix(dataDir)} }

func keychainSuffix(dataDir string) string {
	home, _ := os.UserHomeDir()
	usual := filepath.Join(home, ".local", "share", "agents-terminal")
	if clean := filepath.Clean(dataDir); dataDir == "" || clean == usual {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	return "@" + hex.EncodeToString(sum[:6])
}

func (k Keychain) Load(name string) ([]byte, error) {
	name += k.suffix
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", name, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
			return nil, ErrNoKey
		}
		return nil, fmt.Errorf("keychain: %v", err)
	}
	return hex.DecodeString(strings.TrimSpace(string(out)))
}

func (k Keychain) Save(name string, secret []byte) error {
	name += k.suffix
	// -U updates an existing item; the secret goes in as hex so any bytes fit.
	cmd := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", keychainService, "-a", name,
		"-l", "agents deck "+name, "-w", hex.EncodeToString(secret))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (k Keychain) Delete(name string) error {
	name += k.suffix
	err := exec.Command("/usr/bin/security", "delete-generic-password", "-s", keychainService, "-a", name).Run()
	var ee *exec.ExitError
	if err == nil || errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
		return nil
	}
	return fmt.Errorf("keychain: %v", err)
}

func (Keychain) Where() string { return "macOS Keychain (" + keychainService + ")" }
