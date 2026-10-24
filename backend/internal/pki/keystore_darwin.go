package pki

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// keychainService names the deck's items in the login Keychain.
const keychainService = "dev.agents-deck"

// Keychain keeps secrets in the macOS login Keychain (as generic passwords,
// hex-encoded), through /usr/bin/security: a copied data folder can't mint
// device certificates.
type Keychain struct{}

// DefaultKeystore is the platform's store: the Keychain on macOS.
func DefaultKeystore(dataDir string) Keystore { return Keychain{} }

func (Keychain) Load(name string) ([]byte, error) {
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

func (Keychain) Save(name string, secret []byte) error {
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

func (Keychain) Delete(name string) error {
	err := exec.Command("/usr/bin/security", "delete-generic-password", "-s", keychainService, "-a", name).Run()
	var ee *exec.ExitError
	if err == nil || errors.As(err, &ee) && ee.ExitCode() == 44 { // errSecItemNotFound
		return nil
	}
	return fmt.Errorf("keychain: %v", err)
}

func (Keychain) Where() string { return "macOS Keychain (" + keychainService + ")" }
