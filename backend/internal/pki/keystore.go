// Package pki is the deck's own small certificate authority: it signs the
// network listener's certificate and one client certificate per paired
// device, so nothing reaches the deck over the network without a device key.
package pki

import (
	"errors"
	"os"
	"path/filepath"
)

// Keystore keeps the CA's private key. Each platform has the best place it
// can offer (the macOS Keychain); FileKeystore works everywhere.
type Keystore interface {
	// Load returns the stored secret, or ErrNoKey.
	Load(name string) ([]byte, error)
	Save(name string, secret []byte) error
	// Delete removes a secret; a missing one is no error.
	Delete(name string) error
	// Where says where secrets live, for `agentctl devices`.
	Where() string
}

var ErrNoKey = errors.New("no key stored")

// FileKeystore keeps secrets as 0600 files in Dir (itself 0700): the same
// protection as the daemon's other secrets. Used on Linux and Windows until
// they get a native store, and in tests.
type FileKeystore struct{ Dir string }

func (f FileKeystore) Load(name string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(f.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoKey
	}
	return b, err
}

func (f FileKeystore) Save(name string, secret []byte) error {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(f.Dir, name+".tmp")
	if err := os.WriteFile(tmp, secret, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(f.Dir, name))
}

func (f FileKeystore) Delete(name string) error {
	err := os.Remove(filepath.Join(f.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f FileKeystore) Where() string { return "file in " + f.Dir }
