// Package accounts knows the Claude Code accounts on this Mac. Each account
// is a Claude config folder (CLAUDE_CONFIG_DIR; ~/.claude when unset) with
// its own login and its own session transcripts. config.json "accounts"
// names them and gives each a colour; "account" is the one new agents use.
//
// An agent's account is the folder its transcript lives in, so sessions
// started by hand are placed too. With fewer than two accounts configured
// nothing is shown: one account needs no label.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// Account is one Claude config folder.
type Account struct {
	Name  string `json:"name"`
	Dir   string `json:"dir"`   // as written in config.json ("~/" allowed)
	Color string `json:"color"` // a hat colour
}

// Home is ~/.claude: Claude Code's folder when CLAUDE_CONFIG_DIR is unset.
func Home() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// Path is the account's folder, absolute and clean.
func (a Account) Path() string { return expand(a.Dir) }

func expand(dir string) string {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, dir[1:])
	}
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

// IsHome reports whether the account is Claude Code's default folder, which
// must be reached by leaving CLAUDE_CONFIG_DIR unset: Claude Code keeps that
// account's settings in ~/.claude.json, outside the folder.
func (a Account) IsHome() bool { return a.Path() == Home() }

// ProjectsDir holds the account's session transcripts.
func (a Account) ProjectsDir() string { return filepath.Join(a.Path(), "projects") }

// ConfigDir is the CLAUDE_CONFIG_DIR value for the account ("" = unset).
func (a Account) ConfigDir() string {
	if a.IsHome() {
		return ""
	}
	return a.Path()
}

// Model is how agents carry the account (nil when accounts aren't in use).
func (a Account) Model() *model.Account {
	return &model.Account{Name: a.Name, Color: a.Color}
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,15}$`)

// Validate checks a name, folder and colour.
func Validate(a Account) error {
	if !nameRe.MatchString(a.Name) {
		return fmt.Errorf("account name %q: use 1-16 lower-case letters, digits, - or _", a.Name)
	}
	if !filepath.IsAbs(a.Path()) {
		return fmt.Errorf("account folder %q: give a full path (or ~/…)", a.Dir)
	}
	if !contains(hats.Colors, a.Color) {
		return fmt.Errorf("unknown colour %q (one of: %s)", a.Color, strings.Join(hats.Colors, ", "))
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- config.json ----------------------------------------------------------

type config struct {
	Accounts []Account `json:"accounts"`
	Default  string    `json:"account"`
}

var cache struct {
	sync.Mutex
	mod  time.Time
	size int64
	cfg  config
}

// read returns the accounts part of config.json, re-read only when the file
// changed (the state snapshot asks on every update).
func read() config {
	st, err := os.Stat(paths.Config())
	cache.Lock()
	defer cache.Unlock()
	if err != nil {
		cache.cfg, cache.mod, cache.size = config{}, time.Time{}, 0
		return cache.cfg
	}
	if st.ModTime().Equal(cache.mod) && st.Size() == cache.size {
		return cache.cfg
	}
	var c config
	if b, err := os.ReadFile(paths.Config()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	valid := c.Accounts[:0]
	for _, a := range c.Accounts {
		if Validate(a) == nil {
			valid = append(valid, a)
		}
	}
	c.Accounts = valid
	cache.cfg, cache.mod, cache.size = c, st.ModTime(), st.Size()
	return c
}

// List is the configured accounts, in config order. Without any, it is the
// one account Claude Code uses by default.
func List() []Account {
	if c := read(); len(c.Accounts) > 0 {
		return append([]Account(nil), c.Accounts...)
	}
	return []Account{{Name: "default", Dir: Home(), Color: "ink"}}
}

// Configured is config.json's account list as written (empty: none set up).
func Configured() []Account { return append([]Account(nil), read().Accounts...) }

// InUse reports whether more than one account is configured (then agents
// show theirs).
func InUse() bool { return len(read().Accounts) > 1 }

// Find looks an account up by name.
func Find(name string) (Account, bool) {
	for _, a := range List() {
		if a.Name == name {
			return a, true
		}
	}
	return Account{}, false
}

// Default is the account new agents start in: config.json "account", else
// the one in Claude Code's default folder, else the first.
func Default() Account {
	list := List()
	if a, ok := Find(read().Default); ok {
		return a
	}
	for _, a := range list {
		if a.IsHome() {
			return a
		}
	}
	return list[0]
}

// ForTranscript is the account whose folder holds a transcript.
func ForTranscript(path string) (Account, bool) {
	if path == "" {
		return Account{}, false
	}
	path = filepath.Clean(path)
	best, n := Account{}, -1
	for _, a := range List() {
		p := a.Path() + string(filepath.Separator)
		if strings.HasPrefix(path, p) && len(p) > n {
			best, n = a, len(p)
		}
	}
	return best, n >= 0
}

// Of is the account an agent belongs to: the one it was started in (name),
// else the one holding its transcript. nil when accounts aren't in use or
// the agent isn't a Claude session.
func Of(name, transcript string) *model.Account {
	if !InUse() {
		return nil
	}
	if a, ok := Find(name); ok {
		return a.Model()
	}
	if a, ok := ForTranscript(transcript); ok {
		return a.Model()
	}
	return nil
}

// Save writes the account list and default into config.json, keeping every
// other key.
func Save(list []Account, def string) error {
	for _, a := range list {
		if err := Validate(a); err != nil {
			return err
		}
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON: %v", err)
		}
	}
	if len(list) == 0 {
		delete(cfg, "accounts")
	} else {
		cfg["accounts"] = list
	}
	if def == "" {
		delete(cfg, "account")
	} else {
		cfg["account"] = def
	}
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

// ---- handing a session over -------------------------------------------------

// CopySession copies a session's transcript (and its folder of subagent
// transcripts and tool results, if any) into another account, at the same
// place under that account's projects folder, and returns the new path. A
// copy left there by an earlier handover is replaced: the source is newer.
func CopySession(transcript string, from, to Account) (string, error) {
	rel, err := filepath.Rel(from.ProjectsDir(), transcript)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("the transcript is not in the " + from.Name + " account's folder")
	}
	dst := filepath.Join(to.ProjectsDir(), rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	if err := copyFile(transcript, dst); err != nil {
		return "", err
	}
	side := strings.TrimSuffix(transcript, ".jsonl")
	if st, err := os.Stat(side); err == nil && st.IsDir() {
		if err := copyTree(side, strings.TrimSuffix(dst, ".jsonl")); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			return copyFile(p, target)
		}
		return nil // links and the like stay behind
	})
}
