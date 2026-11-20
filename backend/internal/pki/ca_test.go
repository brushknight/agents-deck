package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newCA(t *testing.T) (*CA, string, Keystore) {
	t.Helper()
	dir := t.TempDir()
	ks := FileKeystore{Dir: filepath.Join(dir, "keys")}
	ca, err := LoadOrCreateCA(filepath.Join(dir, "ca.pem"), ks, "test")
	if err != nil {
		t.Fatal(err)
	}
	return ca, dir, ks
}

func TestCAReloadsAndRefusesALostKey(t *testing.T) {
	ca, dir, ks := newCA(t)
	again, err := LoadOrCreateCA(filepath.Join(dir, "ca.pem"), ks, "test")
	if err != nil || !again.Cert.Equal(ca.Cert) {
		t.Fatalf("reload: %v", err)
	}
	// The certificate without its key: never silently start a new CA.
	if _, err := LoadOrCreateCA(filepath.Join(dir, "ca.pem"), FileKeystore{Dir: t.TempDir()}, "test"); err == nil ||
		!strings.Contains(err.Error(), "missing") {
		t.Fatalf("lost key: %v", err)
	}
}

func TestServerAndDeviceCertificatesVerify(t *testing.T) {
	ca, dir, _ := newCA(t)
	srv, err := ca.ServerCert(filepath.Join(dir, "s.pem"), filepath.Join(dir, "s.key"), []string{"deck.local", "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(srv.Certificate[0])
	for _, host := range []string{"deck.local", "192.0.2.10"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: ca.Pool(), DNSName: host}); err != nil {
			t.Errorf("server cert for %s: %v", host, err)
		}
	}
	// A second call keeps the same certificate; a new address reissues it.
	again, _ := ca.ServerCert(filepath.Join(dir, "s.pem"), filepath.Join(dir, "s.key"), []string{"192.0.2.10"})
	if string(again.Certificate[0]) != string(srv.Certificate[0]) {
		t.Error("server certificate reissued without reason")
	}
	moved, _ := ca.ServerCert(filepath.Join(dir, "s.pem"), filepath.Join(dir, "s.key"), []string{"deck.local", "192.0.2.77"})
	if string(moved.Certificate[0]) == string(srv.Certificate[0]) {
		t.Error("server certificate not reissued for a new address")
	}

	cert, _, keyPEM, err := ca.NewDeviceKey("panel")
	if err != nil || len(keyPEM) == 0 {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: ca.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("device cert: %v", err)
	}
	if cert.Subject.CommonName != "panel" {
		t.Errorf("subject %q", cert.Subject.CommonName)
	}
}

func TestSignCSR(t *testing.T) {
	ca, _, _ := newCA(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "ignored"}}, key)
	csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	cert, _, err := ca.SignCSR("phone", csr)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "phone" || !cert.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		t.Error("certificate doesn't carry the request's key and the deck's name")
	}
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ = x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, rk)
	if _, _, err := ca.SignCSR("x", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})); err == nil {
		t.Error("RSA request accepted")
	}
	if _, _, err := ca.SignCSR("x", []byte("garbage")); err == nil {
		t.Error("garbage accepted")
	}
}

// KEYCHAIN_TEST=1 go test -run TestKeychain ./internal/pki (writes and reads a test item).
func TestKeychain(t *testing.T) {
	if os.Getenv("KEYCHAIN_TEST") == "" {
		t.Skip("set KEYCHAIN_TEST=1 to use the login Keychain")
	}
	ks := DefaultKeystore("")
	if err := ks.Save("test-item", []byte{0, 1, 2, 255}); err != nil {
		t.Fatal(err)
	}
	b, err := ks.Load("test-item")
	if err != nil || string(b) != string([]byte{0, 1, 2, 255}) {
		t.Fatalf("load: %v %v", b, err)
	}
	if _, err := ks.Load("no-such-item"); err != ErrNoKey {
		t.Errorf("missing item: %v", err)
	}
	if err := ks.Delete("test-item"); err != nil {
		t.Fatal(err)
	}
	if _, err := ks.Load("test-item"); err != ErrNoKey {
		t.Errorf("deleted item: %v", err)
	}
	if err := ks.Delete("test-item"); err != nil {
		t.Errorf("deleting a missing item: %v", err)
	}
}

// A file keystore keeps its secrets 0600 in a 0700 folder, and forgets them.
func TestFileKeystore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	ks := FileKeystore{Dir: dir}
	if err := ks.Save("wifi-default", []byte("secret")); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "wifi-default"): 0o600} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != want {
			t.Fatalf("%s: %v %v, want %v", p, st.Mode().Perm(), err, want)
		}
	}
	if b, err := ks.Load("wifi-default"); err != nil || string(b) != "secret" {
		t.Fatalf("load: %q %v", b, err)
	}
	if err := ks.Delete("wifi-default"); err != nil {
		t.Fatal(err)
	}
	if _, err := ks.Load("wifi-default"); err != ErrNoKey {
		t.Fatalf("after delete: %v", err)
	}
	if err := ks.Delete("wifi-default"); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
}

// A key in the store without ca.pem may be the key every paired device
// trusts: the CA is never made again over it.
func TestCAKeepsAKeyWithoutItsCertificate(t *testing.T) {
	ca, dir, ks := newCA(t)
	certPath := filepath.Join(dir, "ca.pem")
	key, err := ks.Load(caKeyName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(certPath); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateCA(certPath, ks, "test"); err == nil || !strings.Contains(err.Error(), "devices reset") {
		t.Fatalf("a key without ca.pem: %v, want a refusal", err)
	}
	if after, _ := ks.Load(caKeyName); string(after) != string(key) {
		t.Fatal("the stored CA key was replaced")
	}
	if _, err := os.Stat(certPath); !os.IsNotExist(err) {
		t.Fatalf("ca.pem was written: %v", err)
	}
	// Put back, the same CA loads again.
	if err := os.WriteFile(certPath, ca.PEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if again, err := LoadOrCreateCA(certPath, ks, "test"); err != nil || !again.Cert.Equal(ca.Cert) {
		t.Fatalf("restored ca.pem: %v", err)
	}
}

// The upgrade from 0.1.1: same ca.pem, same stored key, so the same CA,
// and the listener keeps its certificate.
func TestCASurvivesARestartWithTheSameFolder(t *testing.T) {
	ca, dir, ks := newCA(t)
	hosts := []string{"deck.local", "192.0.2.10"}
	srv, err := ca.ServerCert(filepath.Join(dir, "server-cert.pem"), filepath.Join(dir, "server-key.pem"), hosts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateCA(filepath.Join(dir, "ca.pem"), ks, "another name")
	if err != nil || again.Fingerprint() != ca.Fingerprint() {
		t.Fatalf("reload: %v", err)
	}
	srv2, err := again.ServerCert(filepath.Join(dir, "server-cert.pem"), filepath.Join(dir, "server-key.pem"), hosts)
	if err != nil || string(srv2.Certificate[0]) != string(srv.Certificate[0]) {
		t.Fatalf("server certificate reissued on restart: %v", err)
	}
}

// A store that can't be read (a locked or denied Keychain) stops the
// start; it never counts as "no key".
func TestCARefusesAnUnreadableStore(t *testing.T) {
	_, dir, _ := newCA(t)
	if _, err := LoadOrCreateCA(filepath.Join(dir, "ca.pem"), failingStore{}, "test"); err == nil {
		t.Fatal("an unreadable store was taken for an empty one")
	}
	fresh := filepath.Join(t.TempDir(), "ca.pem")
	if _, err := LoadOrCreateCA(fresh, failingStore{}, "test"); err == nil {
		t.Fatal("made a CA without being able to read the store")
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("ca.pem written: %v", err)
	}
}

// A key that can't be saved leaves no ca.pem behind.
func TestCALeavesNoCertificateWhenTheKeyCantBeSaved(t *testing.T) {
	fresh := filepath.Join(t.TempDir(), "ca.pem")
	if _, err := LoadOrCreateCA(fresh, saveFails{}, "test"); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("ca.pem left behind: %v", err)
	}
}

// Two services starting together on a new folder: exactly one makes the CA,
// and the key in the store matches the ca.pem on disk.
func TestCAMadeOnceByTwoStartsAtOnce(t *testing.T) {
	for i := 0; i < 20; i++ {
		dir := t.TempDir()
		ks := FileKeystore{Dir: filepath.Join(dir, "keys")}
		certPath := filepath.Join(dir, "ca.pem")
		errs := make(chan error, 2)
		start := make(chan struct{})
		for j := 0; j < 2; j++ {
			go func() {
				<-start
				_, err := LoadOrCreateCA(certPath, ks, "test")
				errs <- err
			}()
		}
		close(start)
		<-errs
		<-errs
		if _, err := LoadOrCreateCA(certPath, ks, "test"); err != nil {
			t.Fatalf("round %d: after two starts at once: %v", i, err)
		}
	}
}

type failingStore struct{}

func (failingStore) Load(string) ([]byte, error) {
	return nil, errors.New("keychain: user interaction is not allowed")
}
func (failingStore) Save(string, []byte) error { return errors.New("keychain: denied") }
func (failingStore) Delete(string) error       { return errors.New("keychain: denied") }
func (failingStore) Where() string             { return "test store" }

type saveFails struct{}

func (saveFails) Load(string) ([]byte, error) { return nil, ErrNoKey }
func (saveFails) Save(string, []byte) error   { return errors.New("keychain: denied") }
func (saveFails) Delete(string) error         { return nil }
func (saveFails) Where() string               { return "test store" }
