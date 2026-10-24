// Package orca reads the Orca agent orchestrator (github.com/stablyai/orca)
// through its CLI: the agents running in Orca's terminals, with the status
// Orca gets from their own hooks, and the terminal actions the deck needs
// (switch, send). Only the documented `orca … --json` commands are used.
package orca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BundleID is the macOS app, for bringing it to the front.
const BundleID = "com.stablyai.orca"

// Bin finds the CLI (`orca`; `orca-ide` on Linux), "" when not installed.
func Bin() string {
	for _, name := range []string{"orca-ide", "orca"} {
		if p, err := exec.LookPath(name); err == nil && isOrca(p) {
			return p
		}
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			if p := filepath.Join(dir, name); isOrca(p) {
				return p
			}
		}
	}
	return ""
}

// isOrca: an executable that isn't GNOME's Orca screen reader.
func isOrca(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return false
	}
	return filepath.Base(p) == "orca-ide" || !strings.HasPrefix(p, "/usr/bin/")
}

// Running reports whether the Orca app's runtime is up (its metadata file),
// without starting anything.
func Running() bool {
	_, err := os.Stat(RuntimeFile())
	return err == nil
}

// RuntimeFile is where Orca writes its runtime metadata.
func RuntimeFile() string {
	if d := os.Getenv("ORCA_USER_DATA_PATH"); d != "" {
		return filepath.Join(d, "orca-runtime.json")
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat("/Applications"); err == nil {
		return filepath.Join(home, "Library", "Application Support", "orca", "orca-runtime.json")
	}
	return filepath.Join(home, ".config", "orca", "orca-runtime.json")
}

// Agent is one agent row of `orca worktree ps`: status from the agent's hooks.
type Agent struct {
	PaneKey              string     `json:"paneKey"` // "<tabId>:<leafId>"
	State                string     `json:"state"`   // working | blocked | waiting | done
	AgentType            string     `json:"agentType"`
	Prompt               string     `json:"prompt"`
	LastAssistantMessage string     `json:"lastAssistantMessage"`
	ToolName             string     `json:"toolName"`
	ToolInput            string     `json:"toolInput"`
	Interrupted          bool       `json:"interrupted"`
	StateStartedAt       int64      `json:"stateStartedAt"`
	UpdatedAt            int64      `json:"updatedAt"`
	Subagents            []Subagent `json:"subagents"`
}

type Subagent struct {
	ID          string `json:"id"`
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	State       string `json:"state"`
}

// Worktree is one workspace of `orca worktree ps`.
type Worktree struct {
	WorktreeID  string  `json:"worktreeId"`
	Repo        string  `json:"repo"`
	Path        string  `json:"path"`
	Branch      string  `json:"branch"`
	DisplayName string  `json:"displayName"`
	Unread      bool    `json:"unread"`
	Agents      []Agent `json:"agents"`
}

// Terminal is one terminal of `orca terminal list`.
type Terminal struct {
	Handle       string `json:"handle"`
	TabID        string `json:"tabId"`
	LeafID       string `json:"leafId"`
	Title        string `json:"title"`
	WorktreeID   string `json:"worktreeId"`
	WorktreePath string `json:"worktreePath"`
	Connected    bool   `json:"connected"`
}

// PaneKey is the key agent rows use for this terminal.
func (t Terminal) PaneKey() string { return t.TabID + ":" + t.LeafID }

// Snapshot is everything the deck needs from one poll.
type Snapshot struct {
	Worktrees []Worktree
	Terminals []Terminal
}

// Client runs the CLI.
type Client struct {
	Bin     string
	Timeout time.Duration // per call; default 5 s
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call runs `orca <args> --json` and decodes its result into out.
func (c Client) call(out any, args ...string) error {
	t := c.Timeout
	if t == 0 {
		t = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, append(args, "--json")...)
	cmd.Dir = os.TempDir()
	raw, err := cmd.Output()
	var env envelope
	if jerr := json.Unmarshal(raw, &env); jerr != nil {
		if err != nil {
			return fmt.Errorf("orca %s: %v", args[0], err)
		}
		return fmt.Errorf("orca %s: %v", args[0], jerr)
	}
	if !env.OK {
		if env.Error != nil {
			return fmt.Errorf("orca %s: %s", strings.Join(args[:min(2, len(args))], " "), env.Error.Message)
		}
		return errors.New("orca: request failed")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// Read polls the workspaces (with their agents) and the terminals.
func (c Client) Read() (Snapshot, error) {
	var ps struct {
		Worktrees []Worktree `json:"worktrees"`
	}
	if err := c.call(&ps, "worktree", "ps"); err != nil {
		return Snapshot{}, err
	}
	var tl struct {
		Terminals []Terminal `json:"terminals"`
	}
	if err := c.call(&tl, "terminal", "list"); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Worktrees: ps.Worktrees, Terminals: tl.Terminals}, nil
}

// Screen is the terminal's current screen as text (its rendered rows).
func (c Client) Screen(handle string) (string, error) {
	var r struct {
		Terminal struct {
			Tail []string `json:"tail"`
		} `json:"terminal"`
	}
	if err := c.call(&r, "terminal", "read", "--terminal", handle, "--screen"); err != nil {
		return "", err
	}
	return strings.Join(r.Terminal.Tail, "\n"), nil
}

// Send types text into a terminal (with Enter when asked).
func (c Client) Send(handle, text string, enter bool) error {
	args := []string{"terminal", "send", "--terminal", handle, "--text", text}
	if enter {
		args = append(args, "--enter")
	}
	return c.call(nil, args...)
}

// Switch shows a terminal's tab in Orca.
func (c Client) Switch(handle string) error {
	return c.call(nil, "terminal", "switch", "--terminal", handle)
}

// Front brings the Orca app forward (macOS).
func (Client) Front() error { return Front() }

// Front brings the Orca app forward (macOS).
func Front() error { return exec.Command("/usr/bin/open", "-b", BundleID).Run() }
