package main

import "testing"

// A real deck never takes the stand-in Bot API (it would hand a real token
// to whatever listens there); a demo does, on this computer only.
func TestStandInOnlyInADemo(t *testing.T) {
	t.Setenv("AGENTS_DECK_TELEGRAM_API", "http://127.0.0.1:28197")
	if base := liveAPI(); base != "" {
		t.Fatalf("a real deck took the stand-in %q", base)
	}
	if base, set := standInAPI(); !set || base == "" {
		t.Fatalf("the demo didn't take a stand-in on this computer: %q %v", base, set)
	}
	t.Setenv("AGENTS_DECK_TELEGRAM_API", "https://api.telegram.org")
	if base, set := standInAPI(); set || base != "" {
		t.Fatalf("the demo took a host that isn't this computer: %q", base)
	}
}
