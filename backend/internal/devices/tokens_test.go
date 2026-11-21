package devices

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-tokens.json")
	s := &Tokens{Path: path}
	secret, tok, err := s.Create("claude-main", Act)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, TokenPrefix) || len(secret) != len(TokenPrefix)+64 || tok.Scope != Act {
		t.Fatalf("token %q %+v", secret, tok)
	}
	// Only the hash is stored.
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), secret) || !strings.Contains(string(b), HashToken(secret)) {
		t.Fatalf("stored: %s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	// The listing never carries hashes.
	for _, l := range s.List() {
		if l.Hash != "" {
			t.Error("hash in the list")
		}
	}
	// Found again from a fresh load, and only with the right secret.
	s2 := &Tokens{Path: path}
	if got, ok := s2.Check(secret); !ok || got.Name != "claude-main" || got.Identity().Name != "mcp:claude-main" || got.Identity().Scope != Act {
		t.Fatalf("check: %+v %v", got, ok)
	}
	for _, bad := range []string{"", secret[:len(secret)-1] + "0", strings.TrimPrefix(secret, TokenPrefix), HashToken(secret)} {
		if _, ok := s2.Check(bad); ok && bad != secret {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, _, err := s2.Create("claude-main", View); err == nil {
		t.Error("a second active token with the same name")
	}
	for _, bad := range []string{"x/y", "", "   "} {
		if _, _, err := s2.Create(bad, View); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, _, err := s2.Create("other", Scope("root")); err == nil {
		t.Error("scope root accepted")
	}
	// Revoked: refused at once, listed as revoked, the name free again.
	if err := s2.Revoke("claude-main"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Check(secret); ok {
		t.Fatal("revoked token works")
	}
	if _, ok := (&Tokens{Path: path}).Check(secret); ok {
		t.Fatal("revoked token works after a reload")
	}
	if l := s2.List(); len(l) != 1 || !l[0].Revoked {
		t.Fatalf("list: %+v", l)
	}
	if _, _, err := s2.Create("claude-main", Admin); err != nil {
		t.Fatalf("name after revoke: %v", err)
	}
	if err := s2.Remove("claude-main"); err != nil || len(s2.List()) != 0 {
		t.Fatalf("remove: %v %+v", err, s2.List())
	}
	if err := s2.Revoke("nope"); err != ErrUnknown {
		t.Errorf("revoke unknown: %v", err)
	}
	s2.Used("nope") // no panic
}

// A token's name is free text: spaces, capitals, dots; no control
// characters or "/"; unique ignoring capitals.
func TestTokenNames(t *testing.T) {
	s := &Tokens{Path: filepath.Join(t.TempDir(), "mcp-tokens.json")}
	if _, tok, err := s.Create("  Fable 5.2 orchestrator ", Act); err != nil || tok.Name != "Fable 5.2 orchestrator" {
		t.Fatalf("free text: %+v %v", tok, err)
	}
	if _, _, err := s.Create("fable 5.2 ORCHESTRATOR", Act); err == nil {
		t.Error("same name in other capitals was accepted")
	}
	for _, bad := range []string{"", "a/b", "bell\x07", strings.Repeat("x", 65)} {
		if _, _, err := s.Create(bad, Act); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if err := s.Revoke("FABLE 5.2 orchestrator"); err != nil {
		t.Errorf("revoke ignoring capitals: %v", err)
	}
}
