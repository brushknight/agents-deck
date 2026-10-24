package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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
