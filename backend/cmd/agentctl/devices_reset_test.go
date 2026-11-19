package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// noKeystore fails every call: a test that reaches for the platform's
// keystore (the macOS Keychain) fails instead of touching it.
type noKeystore struct{}

var errRealKeystore = errors.New("a test reached for the real keystore")

func (noKeystore) Load(string) ([]byte, error) { return nil, errRealKeystore }
func (noKeystore) Save(string, []byte) error   { return errRealKeystore }
func (noKeystore) Delete(string) error         { return errRealKeystore }
func (noKeystore) Where() string               { return "nowhere (tests)" }

func TestMain(m *testing.M) {
	// Nothing here may use the real data folder or the real Keychain.
	dir, _ := os.MkdirTemp("", "agentctl-test-home")
	os.Setenv("AGENTSTERM_HOME", dir)
	resetKeystore = func(string) pki.Keystore { return noKeystore{} }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testFiles(dir string) resetFiles {
	return resetFiles{Data: dir, CA: filepath.Join(dir, "ca.pem"), ServerCert: filepath.Join(dir, "server-cert.pem"),
		ServerKey: filepath.Join(dir, "server-key.pem"), Devices: filepath.Join(dir, "devices.json")}
}

func TestDevicesResetStartsOver(t *testing.T) {
	dir := t.TempDir()
	f := testFiles(dir)
	ks := pki.FileKeystore{Dir: filepath.Join(dir, "keys")}
	ca, err := pki.LoadOrCreateCA(f.CA, ks, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.ServerCert(f.ServerCert, f.ServerKey, []string{"localhost", "192.0.2.10"}); err != nil {
		t.Fatal(err)
	}
	reg := &devices.Registry{Path: f.Devices}
	if err := reg.AddNetwork("phone", devices.View, "aa01", "fp1"); err != nil {
		t.Fatal(err)
	}
	board, err := reg.Board("b0001", "t4s3-panel", "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Approve(board.Name); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBoardCert(board.Name, "bb02", "fp2"); err != nil {
		t.Fatal(err)
	}
	other, err := reg.Board("b0002", "oled-tile", "0.2.0") // USB only: nothing to do
	if err != nil {
		t.Fatal(err)
	}
	oldCA, _ := os.ReadFile(f.CA)
	oldDevices, _ := os.ReadFile(f.Devices)
	oldKey, _ := ks.Load(pki.CAKeyName)

	now := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	rep, err := resetCertificates(f, ks, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Backup != filepath.Join(dir, "backups", "certificates-2026-11-11-093000") {
		t.Errorf("backup in %s", rep.Backup)
	}
	for name, want := range map[string][]byte{"ca.pem": oldCA, "devices.json": oldDevices} {
		got, err := os.ReadFile(filepath.Join(rep.Backup, name))
		if err != nil || string(got) != string(want) {
			t.Errorf("backup of %s: %v", name, err)
		}
	}
	// A CA key kept as a file is backed up too, readable by you only.
	if got, err := os.ReadFile(filepath.Join(rep.Backup, pki.CAKeyName)); err != nil || len(oldKey) == 0 || string(got) != string(oldKey) {
		t.Errorf("backup of the CA key file: %v", err)
	}
	for _, name := range []string{"server-cert.pem", "server-key.pem", pki.CAKeyName} {
		fi, err := os.Stat(filepath.Join(rep.Backup, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("backup of %s: %v %v", name, err, fi)
		}
	}
	// The CA, its key and the listener's certificate are gone.
	for _, p := range []string{f.CA, f.ServerCert, f.ServerKey} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still there", filepath.Base(p))
		}
	}
	if _, err := ks.Load("ca-key"); !errors.Is(err, pki.ErrNoKey) {
		t.Errorf("CA key still stored: %v", err)
	}
	// What each device needs next.
	if strings.Join(rep.Network, ",") != "phone" || strings.Join(rep.Boards, ",") != board.Name {
		t.Errorf("report %+v", rep)
	}
	reg = &devices.Registry{Path: f.Devices}
	if _, ok := reg.Get("phone"); ok {
		t.Error("the phone is still paired")
	}
	if d, ok := reg.Get(board.Name); !ok || d.CertSerial != "" || d.BoardID != "b0001" {
		t.Errorf("board after reset: %+v %v", d, ok)
	}
	if _, ok := reg.Get(other.Name); !ok {
		t.Error("the USB-only board is gone")
	}
	// The next start makes a new CA without complaint.
	ca2, err := pki.LoadOrCreateCA(f.CA, ks, "test")
	if err != nil || ca2.Fingerprint() == ca.Fingerprint() {
		t.Fatalf("new CA: %v", err)
	}
	// A second reset the same second gets a folder of its own.
	rep2, err := resetCertificates(f, ks, now)
	if err != nil || rep2.Backup == rep.Backup {
		t.Fatalf("second reset: %v %s", err, rep2.Backup)
	}
}

// The case the service's own error message points at: the certificate is
// there, its key is not.
func TestDevicesResetAfterLostKey(t *testing.T) {
	dir := t.TempDir()
	f := testFiles(dir)
	ks := pki.FileKeystore{Dir: filepath.Join(dir, "keys")}
	if _, err := pki.LoadOrCreateCA(f.CA, ks, "test"); err != nil {
		t.Fatal(err)
	}
	_ = ks.Delete("ca-key")
	if _, err := pki.LoadOrCreateCA(f.CA, ks, "test"); err == nil || !strings.Contains(err.Error(), "agentctl devices reset") {
		t.Fatalf("lost key: %v", err)
	}
	if _, err := resetCertificates(f, ks, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := pki.LoadOrCreateCA(f.CA, ks, "test"); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}

// Tests can't reach the real keystore through the command.
func TestDevicesResetKeystoreIsFake(t *testing.T) {
	if _, ok := resetKeystore(t.TempDir()).(noKeystore); !ok {
		t.Fatal("resetKeystore is the real one in tests")
	}
}
