package pki

import (
	"errors"
	"os"
)

// RemoveCA forgets the deck's certificate authority: the certificate at
// certPath and its key in ks. The next start makes a new one, and every
// certificate the old one signed stops working. Missing pieces are no error.
func RemoveCA(certPath string, ks Keystore) error {
	if err := ks.Delete(caKeyName); err != nil {
		return err
	}
	if err := os.Remove(certPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
