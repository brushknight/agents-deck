package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Token is one MCP access token: a name, a scope, and the SHA-256 of the
// secret. The secret itself is shown once, when it is made, and never kept.
type Token struct {
	Name     string    `json:"name"`
	Scope    Scope     `json:"scope"`
	Hash     string    `json:"hash"` // hex SHA-256 of the token
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Revoked  bool      `json:"revoked,omitempty"`
}

// TokenPrefix starts every token, so it is easy to recognise (and to find
// in a file it shouldn't be in).
const TokenPrefix = "adk_"

// TokenKind is the Device.Kind of a token's identity in Dispatch.
const TokenKind = "token"

// Identity is who a token acts as in the API and the audit log: "mcp:<name>",
// with the token's scope.
func (t Token) Identity() Device {
	return Device{Name: "mcp:" + t.Name, Kind: TokenKind, Scope: t.Scope, Created: t.Created}
}

// Tokens is mcp-tokens.json (next to devices.json), guarded for concurrent use.
type Tokens struct {
	Path string

	mu     sync.Mutex
	loaded bool
	list   map[string]*Token
	seenAt map[string]time.Time // last saved LastUsed, to save at most once a minute
}

// HashToken is how a token is stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Tokens) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.list = map[string]*Token{}
	s.seenAt = map[string]time.Time{}
	var l []*Token
	if b, err := os.ReadFile(s.Path); err == nil {
		_ = json.Unmarshal(b, &l)
	}
	for _, t := range l {
		s.list[t.Name] = t
	}
}

func (s *Tokens) save() error {
	l := make([]*Token, 0, len(s.list))
	for _, t := range s.list {
		l = append(l, t)
	}
	sort.Slice(l, func(i, j int) bool { return l[i].Name < l[j].Name })
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Create makes a token named name with scope and returns it: the only time
// the secret exists outside the caller. A revoked token's name can be used
// again; an active one's can't.
func (s *Tokens) Create(name string, scope Scope) (string, Token, error) {
	name = strings.TrimSpace(name)
	if err := tokenName(name); err != nil {
		return "", Token{}, err
	}
	if _, err := ParseScope(string(scope)); err != nil {
		return "", Token{}, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Token{}, err
	}
	secret := TokenPrefix + hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if t := s.find(name); t != nil && !t.Revoked {
		return "", Token{}, fmt.Errorf("there is already a token named %s: revoke it first, or pick another name", t.Name)
	}
	if t := s.find(name); t != nil {
		delete(s.list, t.Name) // a revoked one under the same name gives way
	}
	t := &Token{Name: name, Scope: scope, Hash: HashToken(secret), Created: time.Now().UTC()}
	s.list[name] = t
	if err := s.save(); err != nil {
		delete(s.list, name)
		return "", Token{}, err
	}
	return secret, *t, nil
}

// List returns every token (hashes left out), sorted by name.
func (s *Tokens) List() []Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := make([]Token, 0, len(s.list))
	for _, t := range s.list {
		c := *t
		c.Hash = ""
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Check finds the active token whose secret this is. Every stored hash is
// compared in constant time.
func (s *Tokens) Check(secret string) (Token, bool) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, TokenPrefix) {
		return Token{}, false
	}
	h := []byte(HashToken(secret))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	var found *Token
	for _, t := range s.list {
		if subtle.ConstantTimeCompare(h, []byte(t.Hash)) == 1 {
			found = t
		}
	}
	if found == nil || found.Revoked {
		return Token{}, false
	}
	return *found, true
}

// Used notes a token's use (saved at most once a minute).
func (s *Tokens) Used(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	t, ok := s.list[name]
	if !ok {
		return
	}
	now := time.Now().UTC()
	t.LastUsed = now
	if now.Sub(s.seenAt[name]) > time.Minute {
		s.seenAt[name] = now
		_ = s.save()
	}
}

// Revoke stops a token working at once; it stays listed as revoked. Its
// hash is dropped, so nothing can bring it back.
func (s *Tokens) Revoke(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	t := s.find(name)
	if t == nil {
		return ErrUnknown
	}
	t.Revoked, t.Hash = true, ""
	return s.save()
}

// Remove forgets a token entirely (revoking it if it wasn't).
func (s *Tokens) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	t := s.find(name)
	if t == nil {
		return ErrUnknown
	}
	delete(s.list, t.Name)
	return s.save()
}

// tokenName: a token's name is a label for you (it shows in Settings and
// the audit log as mcp:<name>): any readable text up to 64 characters, but
// no control characters, and no "/" (it is part of the revoke address).
func tokenName(name string) error {
	switch {
	case name == "":
		return errors.New("give the token a name, e.g. Fable orchestrator")
	case len([]rune(name)) > 64:
		return errors.New("a token name is at most 64 characters")
	case strings.ContainsRune(name, '/'):
		return errors.New("a token name can't contain /")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("a token name can't contain control characters")
		}
	}
	return nil
}

// find is the token named name, ignoring capitals (nil: none); the caller
// holds s.mu.
func (s *Tokens) find(name string) *Token {
	name = strings.TrimSpace(name)
	if t, ok := s.list[name]; ok {
		return t
	}
	for k, t := range s.list {
		if strings.EqualFold(k, name) {
			return t
		}
	}
	return nil
}
