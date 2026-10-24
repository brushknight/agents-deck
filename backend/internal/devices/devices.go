// Package devices is the registry of everything paired with the deck: network
// devices (the desk panel; later a phone) that hold a client certificate, and
// serial boards on Deck Link. Each has a name, a scope and can be revoked.
package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scope is what a device may do.
type Scope string

const (
	View  Scope = "view"  // state and events
	Act   Scope = "act"   // also answer, focus, interrupt, move, order, hats
	Admin Scope = "admin" // also dismiss, resume, restore, add live sessions
)

func (s Scope) rank() int {
	switch s {
	case View:
		return 1
	case Act:
		return 2
	case Admin:
		return 3
	}
	return 0
}

// Allows reports whether s covers need.
func (s Scope) Allows(need Scope) bool { return s.rank() >= need.rank() && need.rank() > 0 }

func ParseScope(v string) (Scope, error) {
	switch s := Scope(v); s {
	case View, Act, Admin:
		return s, nil
	}
	return "", fmt.Errorf("scope must be view, act or admin")
}

// Kinds of device.
const (
	Network = "network" // TLS with a client certificate (panel, phone)
	Serial  = "serial"  // Deck Link over USB serial
)

// Device is one registry entry.
type Device struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Scope    Scope     `json:"scope"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen,omitzero"`
	Revoked  bool      `json:"revoked,omitempty"`

	// Network devices: the client certificate's serial number and SHA-256.
	CertSerial      string `json:"certSerial,omitempty"`
	CertFingerprint string `json:"certFingerprint,omitempty"`

	// Serial boards: what they report in Hello, and whether you approved them.
	BoardID  string `json:"boardId,omitempty"`  // stable id (MAC or USB serial number)
	Type     string `json:"type,omitempty"`     // device type, e.g. "oled-tile"
	Firmware string `json:"firmware,omitempty"` // firmware version
	Approved bool   `json:"approved,omitempty"`
}

// Active: may connect right now.
func (d Device) Active() bool { return !d.Revoked && (d.Kind != Serial || d.Approved) }

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidName: lowercase letters, digits and dashes, up to 32.
func ValidName(n string) error {
	if !nameRe.MatchString(n) {
		return errors.New("device names are lowercase letters, digits and dashes (up to 32), e.g. desk-panel")
	}
	return nil
}

var ErrUnknown = errors.New("no such device")

// Registry is devices.json, guarded for concurrent use.
type Registry struct {
	Path string

	mu      sync.Mutex
	loaded  bool
	devices map[string]*Device
	codes   map[string]pairCode
	seenAt  map[string]time.Time // last saved LastSeen, to save at most once a minute
}

type pairCode struct {
	name    string
	scope   Scope
	expires time.Time
}

func (r *Registry) load() {
	if r.loaded {
		return
	}
	r.loaded = true
	r.devices = map[string]*Device{}
	r.codes = map[string]pairCode{}
	r.seenAt = map[string]time.Time{}
	var list []*Device
	if b, err := os.ReadFile(r.Path); err == nil {
		_ = json.Unmarshal(b, &list)
	}
	for _, d := range list {
		r.devices[d.Name] = d
	}
}

func (r *Registry) save() error {
	list := make([]*Device, 0, len(r.devices))
	for _, d := range r.devices {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.Path)
}

// List returns every device, sorted by name.
func (r *Registry) List() []Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	out := make([]Device, 0, len(r.devices))
	for _, d := range r.devices {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Get(name string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	d, ok := r.devices[name]
	if !ok {
		return Device{}, false
	}
	return *d, true
}

// AddNetwork records a network device's new certificate. Pairing a name again
// replaces its certificate (the old one stops working).
func (r *Registry) AddNetwork(name string, scope Scope, certSerial, certFingerprint string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if d, ok := r.devices[name]; ok && d.Kind != Network {
		return fmt.Errorf("%q is a %s device: pick another name", name, d.Kind)
	}
	r.devices[name] = &Device{Name: name, Kind: Network, Scope: scope, Created: time.Now().UTC(),
		CertSerial: certSerial, CertFingerprint: certFingerprint}
	return r.save()
}

// ByCertSerial finds the active network device holding a certificate.
func (r *Registry) ByCertSerial(serial string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	for _, d := range r.devices {
		if d.Kind == Network && d.CertSerial == serial {
			return *d, d.Active()
		}
	}
	return Device{}, false
}

// Board registers a serial board that said Hello. A new board is added
// unapproved (scope view), named from its type and id; a known one gets its
// type and firmware refreshed. It returns the entry.
func (r *Registry) Board(boardID, typ, firmware string) (Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	for _, d := range r.devices {
		if d.Kind == Serial && d.BoardID == boardID {
			if d.Type != typ || d.Firmware != firmware {
				d.Type, d.Firmware = typ, firmware
				_ = r.save()
			}
			return *d, nil
		}
	}
	name := boardName(typ, boardID)
	for i := 2; r.devices[name] != nil; i++ {
		name = fmt.Sprintf("%s-%d", boardName(typ, boardID), i)
	}
	d := &Device{Name: name, Kind: Serial, Scope: View, Created: time.Now().UTC(), BoardID: boardID, Type: typ, Firmware: firmware}
	r.devices[name] = d
	return *d, r.save()
}

// boardName: "oled-tile-3fa9c1" from the type and the id's last characters.
func boardName(typ, id string) string {
	clean := func(s string) string {
		s = strings.ToLower(s)
		var b strings.Builder
		for _, c := range s {
			switch {
			case c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-':
				b.WriteRune(c)
			case c == ' ' || c == '_':
				b.WriteRune('-')
			}
		}
		return b.String()
	}
	t, i := clean(typ), strings.ReplaceAll(clean(id), "-", "")
	if t == "" {
		t = "board"
	}
	if len(t) > 32 { // Hello.type's limit in link.options
		t = t[:32]
	}
	t = strings.Trim(t, "-")
	if t == "" {
		t = "board"
	}
	if len(i) > 8 { // a long id (a MAC): its last 6 characters
		i = i[len(i)-6:]
	}
	if i == "" {
		return t
	}
	return t + "-" + i
}

// Approve lets a serial board connect.
func (r *Registry) Approve(name string) error {
	return r.update(name, func(d *Device) error {
		if d.Kind != Serial {
			return fmt.Errorf("%s is a %s device: only serial boards need approval", name, d.Kind)
		}
		d.Approved, d.Revoked = true, false
		return nil
	})
}

// Revoke cuts a device off at once (its certificate or board id stops working).
func (r *Registry) Revoke(name string) error {
	return r.update(name, func(d *Device) error { d.Revoked = true; return nil })
}

// SetScope changes what a device may do.
func (r *Registry) SetScope(name string, s Scope) error {
	return r.update(name, func(d *Device) error { d.Scope = s; return nil })
}

// Remove forgets a device entirely.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if _, ok := r.devices[name]; !ok {
		return ErrUnknown
	}
	delete(r.devices, name)
	return r.save()
}

func (r *Registry) update(name string, fn func(*Device) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	d, ok := r.devices[name]
	if !ok {
		return ErrUnknown
	}
	if err := fn(d); err != nil {
		return err
	}
	return r.save()
}

// Seen notes a device's activity (saved at most once a minute).
func (r *Registry) Seen(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	d, ok := r.devices[name]
	if !ok {
		return
	}
	now := time.Now().UTC()
	d.LastSeen = now
	if now.Sub(r.seenAt[name]) > time.Minute {
		r.seenAt[name] = now
		_ = r.save()
	}
}

// PairingCodeTTL is how long a one-time pairing code works.
const PairingCodeTTL = 2 * time.Minute

// NewPairingCode returns a one-time code that lets a device ask for a
// certificate under name, with scope.
func (r *Registry) NewPairingCode(name string, scope Scope, code string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	now := time.Now()
	for c, p := range r.codes {
		if now.After(p.expires) {
			delete(r.codes, c)
		}
	}
	r.codes[code] = pairCode{name: name, scope: scope, expires: now.Add(PairingCodeTTL)}
	return nil
}

// RedeemPairingCode spends a code: the name and scope it was issued for.
func (r *Registry) RedeemPairingCode(code string, equal func(a, b string) bool) (string, Scope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	for c, p := range r.codes {
		if equal(c, code) {
			delete(r.codes, c)
			if time.Now().After(p.expires) {
				return "", "", false
			}
			return p.name, p.scope, true
		}
	}
	return "", "", false
}
