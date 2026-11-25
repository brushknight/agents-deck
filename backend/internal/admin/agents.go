package admin

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/herdr"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/orca"
)

// Tool is one place the deck finds agents (GET /admin/v1/agents).
type Tool struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Found  bool   `json:"found"`  // on this computer: its program, its app or the data the deck reads
	On     bool   `json:"on"`     // the deck watches it (config.json)
	Agents int    `json:"agents"` // on the deck now
	// State, for people: "connected" (agents on the deck now), "found" (here,
	// nothing on the deck right now), "missing" (not on this computer) or
	// "off" (turned off in config.json).
	State string `json:"state"`
	// Detail says what was found ("~/.codex · /Applications/Codex.app") or,
	// when nothing was, where the deck looked.
	Detail string `json:"detail,omitempty"`
	// Note is what to know next: why nothing shows, or what is missing.
	Note string `json:"note,omitempty"`
}

// AccountRow is one Claude account.
type AccountRow struct {
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Default bool    `json:"default"`
	Agents  int     `json:"agents"`
	CostUSD float64 `json:"costUsd"` // what its agents on the deck have cost
}

var tools = []struct{ id, name, cfg string }{
	{"claude", "Claude Code", ""},
	{"codex", "Codex", "codex"},
	{"orca", "Orca", "orca"},
	{"tmux", "tmux", ""},
	{"herdr", "herdr", "herdr"},
	{"cmux", "cmux", "cmux"},
}

// probe looks for each tool the way the watcher that reads it does
// (internal/daemon: codexwatch.go, orcawatch.go, herdrwatch.go,
// cmuxwatch.go, live.go). Tests fill it with temp folders.
type probe struct {
	home      string
	goos      string
	lookPath  func(string) (string, error) // the service's own PATH
	loginPath func(string) string          // the login shell's PATH; "" when not there
	binDirs   []string                     // where programs live that a service's short PATH misses
	appDirs   []string                     // macOS app folders
	orcaBin   func() string                // orca.Bin
	herdrSock func() []string              // herdr.Sockets
	env       func(string) string
}

// newProbe is realProbe; tests never reach the real computer through it.
var newProbe = realProbe

func realProbe() probe {
	home, _ := os.UserHomeDir()
	return probe{
		home: home, goos: runtime.GOOS, lookPath: exec.LookPath, loginPath: loginLookPath,
		binDirs: []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local")},
		appDirs: []string{"/Applications", filepath.Join(home, "Applications")},
		orcaBin: orca.Bin, herdrSock: herdr.Sockets, env: os.Getenv,
	}
}

// loginLookPath asks the login shell once per program: the service may run
// with launchd's short PATH while your terminal finds the program.
var loginPaths sync.Map // name -> path ("" = not found)

func loginLookPath(name string) string {
	if v, ok := loginPaths.Load(name); ok {
		return v.(string)
	}
	p := ""
	if runtime.GOOS != "windows" {
		shell := os.Getenv("SHELL")
		if shell == "" || !filepath.IsAbs(shell) {
			shell = "/bin/sh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, shell, "-lc", "command -v "+name).Output()
		cancel()
		if err == nil {
			if line := strings.TrimSpace(string(out)); filepath.IsAbs(line) {
				p = line
			}
		}
	}
	loginPaths.Store(name, p)
	return p
}

// bin is where the program is, or "".
func (p probe) bin(name string) string {
	if path, err := p.lookPath(name); err == nil {
		return path
	}
	for _, d := range p.binDirs {
		if f := filepath.Join(d, name); isProgram(f) {
			return f
		}
	}
	if p.loginPath != nil {
		return p.loginPath(name)
	}
	return ""
}

func isProgram(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && (runtime.GOOS == "windows" || fi.Mode()&0o111 != 0)
}

// app is where the macOS app is, or "".
func (p probe) app(names ...string) string {
	if p.goos != "darwin" {
		return ""
	}
	for _, d := range p.appDirs {
		for _, n := range names {
			if f := filepath.Join(d, n); isDir(f) {
				return f
			}
		}
	}
	return ""
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// short writes a path under the home folder as ~/...
func (p probe) short(path string) string {
	if p.home != "" && strings.HasPrefix(path, p.home+string(filepath.Separator)) {
		return "~" + string(filepath.Separator) + path[len(p.home)+1:]
	}
	return path
}

// look decides one tool's row from what is on this computer. agents is how
// many of its agents are on the deck now; window is Codex's look-back.
func (p probe) look(id string, agents int, window float64) (found bool, detail, note string) {
	var have []string
	add := func(what string) {
		if what != "" {
			have = append(have, what)
		}
	}
	switch id {
	case "claude":
		data := filepath.Join(p.home, ".claude")
		if d := p.env("CLAUDE_CONFIG_DIR"); d != "" {
			data = d
		}
		bin := p.bin("claude")
		add(p.short(bin))
		if isDir(data) {
			add(p.short(data))
		}
		switch {
		case len(have) == 0:
			return false, "looked for the claude command and " + p.short(data), "Install Claude Code, or start agents with agentctl new once it is."
		case bin == "":
			note = "The claude command isn't on your PATH, so sessions started in a terminal can't be listed to add to the deck."
		}
	case "codex":
		data := filepath.Join(p.home, ".codex")
		if d := p.env("CODEX_HOME"); d != "" {
			data = d
		}
		hasData := isDir(data)
		if hasData {
			add(p.short(data))
		}
		add(p.short(p.bin("codex")))
		add(p.short(p.app("Codex.app")))
		switch {
		case len(have) == 0:
			return false, "looked for " + p.short(data) + ", the codex command and the Codex app", "Codex sessions show up here once Codex (the app or the CLI) has run on this computer."
		case !hasData:
			note = "Codex hasn't saved a session here yet (" + p.short(data) + " doesn't exist). Its sessions show up once it has."
		case agents == 0:
			note = fmt.Sprintf("No Codex session was active in the last %s. They show up here while you use Codex, in the app or the CLI.", hours(window))
		}
	case "orca":
		bin := p.orcaBin()
		add(p.short(bin))
		add(p.short(p.app("Orca.app")))
		switch {
		case len(have) == 0:
			return false, "looked for the orca command and the Orca app", ""
		case bin == "":
			note = "The deck reads Orca through its orca command, which isn't on this computer's PATH. Turn on Orca's command line tool to see its agents here."
		}
	case "tmux":
		bin := p.bin("tmux")
		add(p.short(bin))
		if bin == "" {
			return false, "looked for the tmux command", "agentctl new runs agents in tmux: install it to start agents from the deck."
		}
	case "herdr":
		bin := p.bin("herdr")
		add(p.short(bin))
		dir := filepath.Join(p.home, ".config", "herdr")
		if isDir(dir) {
			add(p.short(dir))
		}
		switch {
		case len(have) == 0:
			return false, "looked for the herdr command and " + p.short(dir), ""
		case agents == 0 && len(p.herdrSock()) == 0:
			note = "herdr isn't running. Its agents show up here while it runs."
		}
	case "cmux":
		add(p.short(p.app("cmux.app")))
		add(p.short(p.bin("cmux")))
		state := filepath.Join(p.home, ".cmuxterm")
		if isDir(state) {
			add(p.short(state))
		}
		switch {
		case len(have) == 0:
			return false, "looked for the cmux app and " + p.short(state), ""
		case agents == 0:
			note = "Sessions in cmux missing? In cmux, set Socket Control Mode to Automation."
		}
	}
	return true, strings.Join(have, " · "), note
}

func hours(h float64) string {
	if h == 1 {
		return "hour"
	}
	if h == float64(int(h)) {
		return fmt.Sprintf("%d hours", int(h))
	}
	return fmt.Sprintf("%g hours", h)
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
	writeJSON(w, http.StatusOK, map[string]any{"tools": toolRows(newProbe(), cfg, count, a.Demo), "accounts": a.accountRows(byAccount, cost), "colors": hats.Colors})
}

func toolRows(p probe, cfg map[string]any, count map[string]int, demo bool) []Tool {
	window := 6.0
	if v, ok := cfg["codexWindowHours"].(float64); ok && v > 0 {
		window = v
	}
	out := []Tool{}
	for _, t := range tools {
		row := Tool{ID: t.id, Name: t.name, Agents: count[t.id], On: true}
		if t.cfg != "" {
			row.On = boolean(cfg, t.cfg, true)
		}
		row.Found, row.Detail, row.Note = p.look(t.id, row.Agents, window)
		if demo && t.id != "herdr" && !row.Found {
			row.Found, row.Detail, row.Note = true, "demo", ""
		}
		switch {
		case !row.On:
			row.State = "off"
			row.Note = fmt.Sprintf("Turned off: config.json has %q: false.", t.cfg)
		case row.Agents > 0:
			row.State, row.Found = "connected", true
			if t.id != "tmux" {
				row.Note = ""
			}
		case row.Found:
			row.State = "found"
		default:
			row.State = "missing"
		}
		out = append(out, row)
	}
	return out
}

func (a *Admin) accountRows(byAccount map[string]int, cost map[string]float64) []AccountRow {
	accs := []AccountRow{}
	if accounts.InUse() {
		def := accounts.Default().Name
		for _, ac := range accounts.List() {
			accs = append(accs, AccountRow{Name: ac.Name, Color: ac.Color, Default: ac.Name == def,
				Agents: byAccount[ac.Name], CostUSD: cost[ac.Name]})
		}
	}
	return accs
}

// accountDefault: POST /admin/v1/accounts/{name}/default: new agents start
// in that account (agentctl accounts default <name>).
func (a *Admin) accountDefault(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	list := accounts.Configured()
	found := false
	for _, ac := range list {
		found = found || ac.Name == name
	}
	if !found {
		writeErr(w, http.StatusNotFound, "no account called "+name)
		return
	}
	a.cfgMu.Lock()
	err := accounts.Save(list, name)
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
