package admin

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Tool is one place the deck finds agents (GET /admin/v1/agents).
type Tool struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Found  bool   `json:"found"`  // installed on this computer
	On     bool   `json:"on"`     // the deck watches it (config.json)
	Agents int    `json:"agents"` // on the deck now
	Note   string `json:"note,omitempty"`
}

// AccountRow is one Claude account.
type AccountRow struct {
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Default bool    `json:"default"`
	Agents  int     `json:"agents"`
	CostUSD float64 `json:"costUsd"` // what its agents on the deck have cost
}

var tools = []struct{ id, name, bin, app, cfg string }{
	{"claude", "Claude Code", "claude", "", ""},
	{"codex", "Codex", "codex", "", "codex"},
	{"orca", "Orca", "", "Orca.app", "orca"},
	{"tmux", "tmux", "tmux", "", ""},
	{"herdr", "herdr", "herdr", "", "herdr"},
	{"cmux", "cmux", "cmux", "cmux.app", "cmux"},
}

// found: the program is on PATH (or in the usual places a service's short
// PATH misses), or the app is in /Applications on macOS.
func found(bin, app string) bool {
	if bin != "" {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
		for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			if fi, err := os.Stat(d + "/" + bin); err == nil && !fi.IsDir() {
				return true
			}
		}
		if home, err := os.UserHomeDir(); err == nil {
			for _, d := range []string{".local/bin", ".claude/local"} {
				if fi, err := os.Stat(home + "/" + d + "/" + bin); err == nil && !fi.IsDir() {
					return true
				}
			}
		}
	}
	if app != "" && runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/" + app); err == nil {
			return true
		}
	}
	return false
}

func (a *Admin) agents(w http.ResponseWriter, r *http.Request) {
	var agents []model.Agent
	if a.Store != nil {
		agents = a.Store.Snapshot().Agents
	}
	count := map[string]int{}
	byAccount := map[string]int{}
	cost := map[string]float64{}
	for _, ag := range agents {
		switch {
		case ag.App != "":
			count[ag.App]++
		case ag.Tool == "codex":
			count["codex"]++
		case ag.Tool == "claude":
			count["claude"]++
		}
		if !ag.External {
			count["tmux"]++
		}
		if ag.Account != nil {
			byAccount[ag.Account.Name]++
			cost[ag.Account.Name] += ag.CostUSD
		}
	}
	cfg := a.readConfig()
	out := []Tool{}
	for _, t := range tools {
		row := Tool{ID: t.id, Name: t.name, Agents: count[t.id], On: true}
		if t.cfg != "" {
			row.On = boolean(cfg, t.cfg, true)
		}
		row.Found = row.Agents > 0 || (a.Demo && t.id != "herdr") || found(t.bin, t.app)
		switch {
		case !row.On:
			row.Note = fmt.Sprintf("turned off (config.json %q: false)", t.cfg)
		case t.id == "tmux":
			row.Note = "for agents you start with agentctl"
		}
		out = append(out, row)
	}
	accs := []AccountRow{}
	if accounts.InUse() {
		def := accounts.Default().Name
		for _, ac := range accounts.List() {
			accs = append(accs, AccountRow{Name: ac.Name, Color: ac.Color, Default: ac.Name == def,
				Agents: byAccount[ac.Name], CostUSD: cost[ac.Name]})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": out, "accounts": accs, "colors": hats.Colors})
}

// accountColor: POST /admin/v1/accounts/{name}/color {"color"}.
func (a *Admin) accountColor(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Color string `json:"color"`
	}
	if err := readBody(r, 1024, &b); err != nil || !oneOf(b.Color, hats.Colors...) {
		writeErr(w, http.StatusBadRequest, "color must be one of the hat colours")
		return
	}
	list := accounts.Configured()
	found := false
	for i := range list {
		if list[i].Name == r.PathValue("name") {
			list[i].Color, found = b.Color, true
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, "no account called "+r.PathValue("name"))
		return
	}
	def := ""
	if d := a.readConfig()["account"]; d != nil {
		def, _ = d.(string)
	}
	a.cfgMu.Lock()
	err := accounts.Save(list, def)
	a.cfgMu.Unlock()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Store != nil {
		a.Store.Touch()
	}
	w.WriteHeader(http.StatusNoContent)
}
