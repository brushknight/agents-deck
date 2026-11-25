package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const caKeyName = "ca-key"

// CAKeyName is the CA key's name in a Keystore (a file of that name in a
// FileKeystore's folder).
const CAKeyName = caKeyName

// CA is the deck's certificate authority. Its certificate is public (every
// client pins it); its key stays in a Keystore.
type CA struct {
	Cert *x509.Certificate
	PEM  []byte
	key  crypto.Signer
}

// LoadOrCreateCA reads the CA certificate from certPath and its key from ks,
// creating both on first use. A certificate without its key (a copied data
// folder) is an error, not a reason to start over.
func LoadOrCreateCA(certPath string, ks Keystore, name string) (*CA, error) {
	certPEM, certErr := os.ReadFile(certPath)
	keyDER, keyErr := ks.Load(caKeyName)
	switch {
	case certErr == nil && keyErr == nil:
		return parseCA(certPEM, keyDER)
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, ErrNoKey):
		return createCA(certPath, ks, name)
	case certErr != nil && !errors.Is(certErr, os.ErrNotExist):
		return nil, certErr
	case keyErr != nil && !errors.Is(keyErr, ErrNoKey):
		return nil, keyErr
	case certErr == nil:
		return nil, fmt.Errorf("the CA key is missing from the %s: run `agentctl devices reset` to start over (every device must pair again)", ks.Where())
	default:
		// A key but no certificate. The key may be the one every paired
		// device trusts (ca.pem deleted by hand, or another data folder that
		// shares the store): never replace it with a new one.
		return nil, fmt.Errorf("%s is missing but the %s still holds a CA key: put ca.pem back, or run `agentctl devices reset` to start over (every device must pair again)", certPath, ks.Where())
	}
}

// ErrCAExists: another process made the CA first.
var ErrCAExists = errors.New("another agentctl made the certificate authority at the same time: start the service again")

func createCA(certPath string, ks Keystore, name string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "agents deck CA " + name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	// Claim ca.pem first, and only if nobody has it: of two services
	// starting at once on a new data folder, only one saves a key.
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := createFile(certPath, certPEM, 0o644); errors.Is(err, os.ErrExist) {
		return nil, ErrCAExists
	} else if err != nil {
		return nil, err
	}
	if err := ks.Save(caKeyName, kb); err != nil {
		_ = os.Remove(certPath) // ours, and nothing was signed with it
		return nil, err
	}
	return parseCA(certPEM, kb)
}

// createFile writes path whole, and only if it doesn't exist yet: the
// content goes to a temporary file that is then linked into place, which
// fails when path is already there.
func createFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".new-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Link(tmp.Name(), path)
}

func parseCA(certPEM, keyDER []byte) (*CA, error) {
	b, _ := pem.Decode(certPEM)
	if b == nil {
		return nil, errors.New("CA certificate: not PEM")
	}
	cert, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("CA key: %v", err)
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, errors.New("CA key: not a signing key")
	}
	if !publicKeysEqual(signer.Public(), cert.PublicKey) {
		return nil, errors.New("the CA key in the keystore doesn't match ca.pem")
	}
	return &CA{Cert: cert, PEM: certPEM, key: signer}, nil
}

// Pool is a cert pool holding just this CA.
func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

// Fingerprint is the SHA-256 of the CA certificate, "sha256:AB:CD:…".
func (ca *CA) Fingerprint() string { return Fingerprint(ca.Cert.Raw) }

// ServerCert returns the network listener's certificate, signed by the CA,
// from certPath/keyPath; a missing, foreign or expiring one, or one that
// doesn't cover every name and IP in hosts, is reissued.
func (ca *CA) ServerCert(certPath, keyPath string, hosts []string) (tls.Certificate, error) {
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil &&
			leaf.CheckSignatureFrom(ca.Cert) == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour && covers(leaf, hosts) {
			c.Leaf = leaf
			return c, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "agents deck " + firstOr(hosts, "server")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if h != "" {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}

func covers(leaf *x509.Certificate, hosts []string) bool {
	for _, h := range hosts {
		if h != "" && leaf.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

// DeviceCertLifetime: device certificates last long; revocation, not expiry,
// is how a device loses access.
const DeviceCertLifetime = 10 * 365 * 24 * time.Hour

// SignDevice issues a client certificate for a device's public key. The
// device's name goes in the subject; the serial number identifies it.
func (ca *CA) SignDevice(name string, pub crypto.PublicKey) (*x509.Certificate, []byte, error) {
	tpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: name, OrganizationalUnit: []string{"agents deck device"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(DeviceCertLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.Cert, pub, ca.key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// SignCSR issues a client certificate from a device's certificate request
// (PEM), after checking the request's own signature.
func (ca *CA) SignCSR(name string, csrPEM []byte) (*x509.Certificate, []byte, error) {
	b, _ := pem.Decode(csrPEM)
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		return nil, nil, errors.New("not a PEM certificate request")
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, nil, fmt.Errorf("certificate request: %v", err)
	}
	switch k := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() && k.Curve != elliptic.P384() {
			return nil, nil, errors.New("use an EC P-256 or P-384 key")
		}
	default:
		return nil, nil, errors.New("use an EC P-256 or P-384 key")
	}
	return ca.SignDevice(name, csr.PublicKey)
}

// NewDeviceKey makes a key pair and certificate for a device provisioned by
// the deck itself (the bundle is copied to the device once, over SSH).
func (ca *CA) NewDeviceKey(name string) (cert *x509.Certificate, certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, certPEM, err = ca.SignDevice(name, &key.PublicKey)
	if err != nil {
		return nil, nil, nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	return cert, certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), nil
}

// Fingerprint formats a SHA-256 over DER bytes as "sha256:AB:CD:…".
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return "sha256:" + strings.Join(parts, ":")
}

// SerialHex is how the registry names a certificate.
func SerialHex(c *x509.Certificate) string { return fmt.Sprintf("%x", c.SerialNumber) }

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	return n
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	type eq interface{ Equal(crypto.PublicKey) bool }
	if x, ok := a.(eq); ok {
		return x.Equal(b)
	}
	return false
}

func firstOr(s []string, d string) string {
	if len(s) > 0 && s[0] != "" {
		return s[0]
	}
	return d
}

func writeFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
