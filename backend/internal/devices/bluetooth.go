package devices

import (
	"errors"
	"fmt"
)

// SetBLE binds a board to this computer's identifier for it over Bluetooth
// (after you added it and it was paired with the passkey on its screen), or
// unbinds it (""). One board per identifier: binding takes it from any other.
func (r *Registry) SetBLE(name, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	d, ok := r.devices[name]
	if !ok {
		return ErrUnknown
	}
	if d.Kind != Serial {
		return fmt.Errorf("%s is a %s device: only boards connect over Bluetooth", name, d.Kind)
	}
	if id != "" {
		for _, o := range r.devices {
			if o != d && o.BLE == id {
				o.BLE = ""
			}
		}
	}
	d.BLE = id
	return r.save()
}

// ByBLE finds the board bound to a Bluetooth identifier.
func (r *Registry) ByBLE(id string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if id == "" {
		return Device{}, false
	}
	for _, d := range r.devices {
		if d.BLE == id {
			return *d, true
		}
	}
	return Device{}, false
}

// ByBoardID finds a board by the id it reports in Hello.
func (r *Registry) ByBoardID(boardID string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	for _, d := range r.devices {
		if d.Kind == Serial && d.BoardID == boardID && boardID != "" {
			return *d, true
		}
	}
	return Device{}, false
}

// SetUplink sets which uplink a board prefers: auto (USB, else Wi-Fi, else
// Bluetooth), wifi or ble.
func (r *Registry) SetUplink(name, pref string) error {
	switch pref {
	case "auto":
		pref = ""
	case "", "wifi", "ble":
	default:
		return errors.New("the uplink is auto, wifi or ble")
	}
	return r.update(name, func(d *Device) error {
		if d.Kind != Serial {
			return fmt.Errorf("%s is a %s device: only boards have uplinks", name, d.Kind)
		}
		d.Uplink = pref
		return nil
	})
}

// ApproveAs approves a board with a scope, unless it was revoked (a revoked
// board stays revoked until agentctl devices approve). It returns the entry
// and whether it approved it just now.
func (r *Registry) ApproveAs(name string, scope Scope) (Device, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	d, ok := r.devices[name]
	if !ok {
		return Device{}, false, ErrUnknown
	}
	if d.Kind != Serial || d.Approved || d.Revoked {
		return *d, false, nil
	}
	d.Approved, d.Scope = true, scope
	if err := r.save(); err != nil {
		d.Approved, d.Scope = false, View
		return *d, false, err
	}
	return *d, true, nil
}
