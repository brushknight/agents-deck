// Package node joins machines into one workspace (multi-node): each machine
// is a full deck, and the machines that joined trust each other.
//
// Joining works like pairing a phone, both ways. `agentctl machines invite`
// on a member prints a one-time code that carries the start of the member's
// CA fingerprint. `agentctl machines join <address> <code>` on the new
// machine fetches the member's CA over TLS, checks it against the code, and
// then each machine's CA signs a node certificate for the other: each one is
// a device of kind node in the other's devices.json, with the scope of
// devices.NodeJoinScope. Afterwards each machine keeps, in machines.json,
// how to reach the other (address, CA, the client certificate it holds
// there); the link that uses it comes later (PR 3).
package node

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// JoinCodeTTL is how long a join code works. One use.
const JoinCodeTTL = 5 * time.Minute

// Machine is another machine of the workspace, as this one knows it
// (machines.json, 0600: it holds this machine's key for that machine).
type Machine struct {
	Name          string    `json:"name"`
	Address       string    `json:"address"`       // host:port of its device listener
	CAFingerprint string    `json:"caFingerprint"` // its CA, pinned
	CA            string    `json:"ca"`            // its CA certificate (PEM): verifies its listener
	Cert          string    `json:"cert"`          // this machine's client certificate there (PEM, signed by its CA)
	Key           string    `json:"key"`           // and its key (PEM)
	Joined        time.Time `json:"joined"`
}

// Manager is this machine's side of the workspace.
type Manager struct {
	Name       string // this machine's name (MachineName)
	Version    string
	CA         *pki.CA
	Devices    *devices.Registry
	Audit      *devices.Audit
	Path       string // machines.json
	DevicePort string // the port other machines reach this one on, e.g. "7341"

	linkState // link.go
	outboxes  // outbox.go

	mu      sync.Mutex
	codes   map[string]time.Time  // join code secret → expiry
	pending map[string]pendingKey // a joining machine's name → the key this machine sent it a request for
	now     func() time.Time
}

type pendingKey struct {
	keyPEM  []byte
	expires time.Time
}

// SetClock makes the manager see another time (tests: an expired code);
// nil is the real clock.
func (m *Manager) SetClock(now func() time.Time) { m.now = now }

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// MachineName turns a host name into a machine name: lowercase letters,
// digits and dashes, at most 32 (devices.ValidName), without ".local".
func MachineName(host string) string {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local"))
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	h = strings.Trim(nonName.ReplaceAllString(h, "-"), "-")
	if len(h) > 32 {
		h = strings.TrimRight(h[:32], "-")
	}
	if h == "" {
		return "machine"
	}
	return h
}

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// ---- join codes --------------------------------------------------------------------------------

// codeFP is how much of the CA fingerprint a code carries: 16 hex digits.
const codeFP = 16

// Invite makes a one-time join code: a random secret and the start of this
// machine's CA fingerprint, "ABCDE-FGHIJ-3D1F9A0C55E2B7A1".
func (m *Manager) Invite() (string, time.Time, error) {
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:10]
	secret := s[:5] + "-" + s[5:]
	exp := m.clock().Add(JoinCodeTTL)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string]time.Time{}
	}
	for c, e := range m.codes {
		if m.clock().After(e) {
			delete(m.codes, c)
		}
	}
	m.codes[secret] = exp
	return secret + "-" + caShort(m.CA.Cert.Raw), exp, nil
}

// redeem spends a code's secret: true once, while it's valid.
func (m *Manager) redeem(secret string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for c, exp := range m.codes {
		if subtle.ConstantTimeCompare([]byte(c), []byte(strings.ToUpper(secret))) == 1 {
			delete(m.codes, c)
			return !m.clock().After(exp)
		}
	}
	return false
}

// caShort is the part of a CA's SHA-256 a code carries, upper-case hex.
func caShort(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))[:codeFP]
}

// parseCode splits "ABCDE-FGHIJ-3D1F9A0C55E2B7A1" into its secret and the
// CA fingerprint part.
func parseCode(code string) (secret, fp string, err error) {
	c := strings.ToUpper(strings.TrimSpace(code))
	i := strings.LastIndexByte(c, '-')
	if i < 0 || len(c)-i-1 != codeFP {
		return "", "", errors.New("that isn't a join code: run `agentctl machines invite` on a machine of the workspace")
	}
	return c[:i], c[i+1:], nil
}

// ---- machines.json -----------------------------------------------------------------------------

// List is every machine this one has joined with, by name.
func (m *Manager) List() []Machine {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.load()
	out := make([]Machine, 0, len(ms))
	for _, x := range ms {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get is one machine.
func (m *Manager) Get(name string) (Machine, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.load()[name]
	return x, ok
}

func (m *Manager) load() map[string]Machine {
	ms := map[string]Machine{}
	if b, err := os.ReadFile(m.Path); err == nil {
		_ = json.Unmarshal(b, &ms)
	}
	return ms
}

func (m *Manager) save(ms map[string]Machine) error {
	b, err := json.MarshalIndent(ms, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.Path)
}

func (m *Manager) put(x Machine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.load()
	ms[x.Name] = x
	return m.save(ms)
}

func (m *Manager) drop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.load()
	delete(ms, name)
	return m.save(ms)
}

// SetAddress changes where a machine is reached (its address changed).
func (m *Manager) SetAddress(name, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.load()
	x, ok := ms[name]
	if !ok {
		return fmt.Errorf("no machine called %q", name)
	}
	x.Address = address
	ms[name] = x
	return m.save(ms)
}

// LastSeen is when the machine last made a request here (its device record).
func (m *Manager) LastSeen(name string) time.Time {
	if d, ok := m.Devices.Get(name); ok {
		return d.LastSeen
	}
	return time.Time{}
}

func (m *Manager) audit(device, action string, code int, detail string) {
	m.Audit.Log(devices.AuditEntry{Device: device, Action: action, Status: code, Detail: detail})
}

// errorf is an error with the text the other side or the CLI shows.
func errorf(format string, a ...any) error { return fmt.Errorf(format, a...) }
