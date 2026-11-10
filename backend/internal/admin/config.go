package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/brushknight/agents-deck/backend/internal/link"
)

// config.json is shared with the rest of the service and with agentctl set:
// every edit reads the whole file and writes it back with only its own keys
// changed.

func (a *Admin) readConfig() map[string]any {
	cfg := map[string]any{}
	if b, err := os.ReadFile(a.Config); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}

// editConfig changes config.json under the lock, keeping every other key.
func (a *Admin) editConfig(fn func(cfg map[string]any) error) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg := map[string]any{}
	if b, err := os.ReadFile(a.Config); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON: %v", err)
		}
	}
	if err := fn(cfg); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Config), 0o700); err != nil {
		return err
	}
	tmp := a.Config + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.Config)
}

func str(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return def
}

func boolean(cfg map[string]any, key string, def bool) bool {
	if v, ok := cfg[key].(bool); ok {
		return v
	}
	return def
}

// registeredPorts is config.json "link.ports" (agentctl link add).
func (a *Admin) registeredPorts() []link.PortConfig {
	var c struct {
		Link struct {
			Ports []link.PortConfig `json:"ports"`
		} `json:"link"`
	}
	if b, err := os.ReadFile(a.Config); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c.Link.Ports
}

// setRegisteredPorts rewrites config.json "link.ports" with fn's answer.
func (a *Admin) setRegisteredPorts(fn func([]link.PortConfig) []link.PortConfig) error {
	return a.editConfig(func(cfg map[string]any) error {
		ports := fn(a.registeredPortsFrom(cfg))
		l, _ := cfg["link"].(map[string]any)
		if l == nil {
			l = map[string]any{}
		}
		l["ports"] = ports
		cfg["link"] = l
		return nil
	})
}

func (a *Admin) registeredPortsFrom(cfg map[string]any) []link.PortConfig {
	var out []link.PortConfig
	l, _ := cfg["link"].(map[string]any)
	if l == nil {
		return out
	}
	b, _ := json.Marshal(l["ports"])
	_ = json.Unmarshal(b, &out)
	return out
}

// ---- names shown for devices ---------------------------------------------------

// labelBook keeps the names you give devices in Settings (rename), in
// device-names.json next to devices.json. A device keeps its id (what
// agentctl devices, the audit log and its certificate use); the name is
// only what Settings and agentctl show.
type labelBook struct {
	mu sync.Mutex
}

func (a *Admin) labelsPath() string { return filepath.Join(a.DataDir, "device-names.json") }

func (a *Admin) Labels() map[string]string {
	a.labels.mu.Lock()
	defer a.labels.mu.Unlock()
	out := map[string]string{}
	if b, err := os.ReadFile(a.labelsPath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (a *Admin) setLabel(id, name string) error {
	a.labels.mu.Lock()
	defer a.labels.mu.Unlock()
	m := map[string]string{}
	if b, err := os.ReadFile(a.labelsPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if name == "" || name == id {
		delete(m, id)
	} else {
		m[id] = name
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(a.DataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.labelsPath(), append(b, '\n'), 0o600)
}
