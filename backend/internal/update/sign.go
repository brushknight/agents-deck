// Package update finds, downloads, checks and installs releases of agents
// deck from GitHub, without GitHub's rate-limited API.
//
// Trust: every release carries SHA256SUMS and SHA256SUMS.sig, an Ed25519
// signature over the exact bytes of SHA256SUMS. The public key is built into
// the service (release.pub). A file from a release is used only when its
// SHA-256 matches its line in a SHA256SUMS whose signature verifies. A build
// with an empty release.pub still reports versions but never installs
// anything from the network.
package update

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// releasePub is one line of base64 (32 raw bytes), or empty: no release key.
//
//go:embed release.pub
var releasePub []byte

// ErrNoReleaseKey: this build has no release key, so nothing from the network
// can be checked, and nothing is installed.
var ErrNoReleaseKey = errors.New("this build has no release key, so it can't check that a download is genuine; " +
	"nothing is installed from the network (download the new version by hand)")

// ReleaseKey is the public key built into this binary, or nil when it has none.
func ReleaseKey() (ed25519.PublicKey, error) {
	s := strings.TrimSpace(string(releasePub))
	if s == "" {
		return nil, nil
	}
	return ParsePublicKey(s)
}

// ParsePublicKey reads one line of base64 holding the 32 raw bytes of an
// Ed25519 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("release key: not base64: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release key: %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// ParsePrivateKey reads base64 of either the 32-byte seed (what NewKey prints)
// or the 64-byte expanded private key.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("private key: not base64: %w", err)
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		k := ed25519.PrivateKey(b)
		if !bytes.Equal(ed25519.NewKeyFromSeed(k.Seed()), k) {
			return nil, errors.New("private key: the public half doesn't match the seed")
		}
		return k, nil
	}
	return nil, fmt.Errorf("private key: %d bytes, want %d (seed) or %d", len(b), ed25519.SeedSize, ed25519.PrivateKeySize)
}

// NewKey makes a release key pair: the private key as base64 of its 32-byte
// seed, the public key as base64 of its 32 bytes (the content of release.pub).
func NewKey() (private, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Seed()), base64.StdEncoding.EncodeToString(pub), nil
}

// PublicOf is the base64 public key that belongs to a private key.
func PublicOf(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
}

// Sign is the content of SHA256SUMS.sig for data: one line of standard base64
// of the raw 64-byte signature, with a newline.
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

// Verify checks sig (the content of a .sig file) against data.
func Verify(pub ed25519.PublicKey, data, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrNoReleaseKey
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("signature: not one line of base64 holding 64 bytes")
	}
	if !ed25519.Verify(pub, data, raw) {
		return errors.New("signature doesn't match: this isn't a genuine release")
	}
	return nil
}

// ParseSums reads a SHA256SUMS file as written by sha256sum: "<hex>  <name>"
// (or "<hex> *<name>" in binary mode). It returns file name to lower-case hex.
func ParseSums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		sum, name, ok := strings.Cut(line, " ")
		if !ok || len(sum) != 64 {
			return nil, fmt.Errorf("SHA256SUMS line %d: not \"<sha256>  <file>\"", n)
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return nil, fmt.Errorf("SHA256SUMS line %d: bad hex", n)
		}
		name = strings.TrimPrefix(strings.TrimPrefix(name, " "), "*")
		if !safeName(name) {
			return nil, fmt.Errorf("SHA256SUMS line %d: unusable file name %q", n, name)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("SHA256SUMS: %s listed twice", name)
		}
		out[name] = strings.ToLower(sum)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("SHA256SUMS is empty")
	}
	return out, nil
}

// safeName: a plain file name, nothing that could leave the cache folder.
func safeName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00") && !strings.HasPrefix(s, ".")
}
