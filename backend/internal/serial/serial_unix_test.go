//go:build darwin || linux

package serial

import (
	"errors"
	"testing"
	"time"
)

// A pseudo-terminal stands in for a board: bytes both ways, and a read
// timeout that ends an empty read with ErrTimeout.
func TestOpenPTY(t *testing.T) {
	ptm, name, err := openPTY()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptm.Close()
	p, err := Open(name, 115200)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.SetReadTimeout(150 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := p.Read(make([]byte, 8)); !errors.Is(err, ErrTimeout) {
		t.Fatalf("empty read: %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
		t.Errorf("timeout took %v", d)
	}
	if _, err := ptm.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	n, err := p.Read(b)
	if err != nil || string(b[:n]) != "hi" {
		t.Fatalf("read %q, %v", b[:n], err)
	}
	if _, err := p.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	n, _ = ptm.Read(b)
	if string(b[:n]) != "ok" {
		t.Fatalf("board got %q", b[:n])
	}
	// Modem lines on a pty: Linux takes them, macOS refuses; either way no panic.
	_ = p.SetDTR(true)
	_ = p.SetRTS(false)
	if err := p.SetBaud(115200); err != nil {
		t.Errorf("SetBaud: %v", err)
	}
}
