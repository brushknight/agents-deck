package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// Focus modes (`agentctl set term …`).
const (
	TermITerm  = "iterm"  // select the agent's iTerm tab/pane (API) and bring the window up
	TermTmux   = "tmux"   // switch the terminal you used last to the agent (one-tab workflow)
	TermWindow = "window" // only bring the agent's own terminal window forward
)

// Settings that `agentctl set` can change, with their allowed values and help.
var Settings = map[string]struct {
	Values []string
	Help   string
}{
	"term":    {[]string{TermITerm, TermTmux, TermWindow}, "how focus shows an agent: iterm = select its iTerm tab, tmux = switch your last-used tab to it, window = just raise its window"},
	"web":     {[]string{"local", "lan"}, "local = dashboard on this Mac only; lan = also on your local network at :7342 (login link: agentctl web --phone)"},
	"restore": {[]string{RestoreAsk, RestoreAuto}, "agents lost with the tmux server (crash, reboot): ask = keep them for `agentctl restore`, auto = bring them back right away"},
	"mouse":   {[]string{MouseTmux, MouseNative}, "tmux = wheel scrolls tmux history, drag selects and copies, click opens links; native = your terminal handles the mouse (its own scrollback)"},
}

// Mouse modes (`agentctl set mouse …`).
const (
	MouseNative = "native"
	MouseTmux   = "tmux"
)

// CurrentRestore reads the restore mode from config.json (default ask).
func CurrentRestore() string {
	var c struct {
		Restore string `json:"restore"`
	}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Restore == RestoreAuto {
		return RestoreAuto
	}
	return RestoreAsk
}

// CurrentMouse reads the mouse mode from config.json (default tmux).
func CurrentMouse() string {
	var c struct {
		Mouse string `json:"mouse"`
	}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Mouse == MouseNative {
		return MouseNative
	}
	return MouseTmux
}

// CurrentTerm reads the focus mode from config.json (re-read on every focus,
// so `agentctl set term` applies immediately).
func CurrentTerm() string {
	var c struct {
		Term string `json:"term"`
	}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	switch c.Term {
	case TermTmux, TermWindow:
		return c.Term
	}
	return TermITerm
}

// Set validates and writes one setting into config.json, keeping every other key.
func Set(key, value string) error {
	s, ok := Settings[key]
	if !ok {
		keys := make([]string, 0, len(Settings))
		for k := range Settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(keys, ", "))
	}
	valid := false
	for _, v := range s.Values {
		valid = valid || v == value
	}
	if !valid {
		return fmt.Errorf("%s must be one of: %s", key, strings.Join(s.Values, ", "))
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON: %v", err)
		}
	}
	cfg[key] = value
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	tmp := paths.Config() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.Config())
}
