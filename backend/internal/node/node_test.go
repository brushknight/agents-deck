package node

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

func TestMachineName(t *testing.T) {
	for in, want := range map[string]string{
		"studio.local":      "studio",
		"Attic-Server":      "attic-server",
		"box.home.example":  "box",
		"Sam's MacBook Pro": "sam-s-macbook-pro",
		"":                  "machine",
		"a-very-long-host-name-that-goes-on-and-on": "a-very-long-host-name-that-goes",
	} {
		got := MachineName(in)
		if got != want || devices.ValidName(got) != nil {
			t.Errorf("%q: got %q (valid: %v), want %q", in, got, devices.ValidName(got), want)
		}
	}
}

func TestParseCode(t *testing.T) {
	if s, fp, err := parseCode(" abcde-fghij-3d1f9a0c55e2b7a1 "); err != nil || s != "ABCDE-FGHIJ" || fp != "3D1F9A0C55E2B7A1" {
		t.Fatalf("%q %q %v", s, fp, err)
	}
	for _, bad := range []string{"", "hello", "ABCDE-FGHIJ-3D1F"} {
		if _, _, err := parseCode(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
