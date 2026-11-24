package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
)

// statusline is Claude Code's status line command in the agents the deck
// starts: it hands the usage limits in Claude Code's input (rate_limits) to
// the deck, then runs the status line Claude Code would have run without
// it, with the same input, and passes its output and exit code on. With no
// other status line it prints nothing.
func statusline() int {
	in, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if os.Getenv("AGENTSTERM_STATUSLINE") != "" {
		return 0 // run by itself through a chain: stop here
	}
	if bytes.Contains(in, []byte(`"rate_limits"`)) {
		c := unixClient(300 * time.Millisecond)
		resp, err := c.Post("http://agentd/statusline?dir="+url.QueryEscape(os.Getenv("CLAUDE_CONFIG_DIR")), "application/json", bytes.NewReader(in))
		if err == nil {
			resp.Body.Close()
		}
	}
	next := nextStatusLine(in)
	if next == "" {
		return 0
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", next)
	} else {
		cmd = exec.Command("/bin/sh", "-c", next)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(in), os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "AGENTSTERM_STATUSLINE=1")
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// nextStatusLine is the status line the deck's stands in front of: the one
// Claude Code would pick from the settings below the deck's own (agent
// settings outrank them): the project's local settings, the project's, then
// the account's (settings.json in its config folder). "" for none.
func nextStatusLine(in []byte) string {
	var p struct {
		Cwd       string `json:"cwd"`
		Workspace struct {
			ProjectDir string `json:"project_dir"`
		} `json:"workspace"`
	}
	_ = json.Unmarshal(in, &p)
	proj := p.Workspace.ProjectDir
	if proj == "" {
		proj = p.Cwd
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = accounts.Home()
	}
	var files []string
	if proj != "" {
		files = append(files, filepath.Join(proj, ".claude", "settings.local.json"), filepath.Join(proj, ".claude", "settings.json"))
	}
	files = append(files, filepath.Join(dir, "settings.json"))
	for _, f := range files {
		if c := statusLineIn(f); c != "" && !isDeckStatusLine(c) {
			return c
		}
	}
	return ""
}

// statusLineIn is a settings file's status line command ("" for none).
func statusLineIn(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s struct {
		StatusLine *struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if json.Unmarshal(b, &s) != nil || s.StatusLine == nil || s.StatusLine.Type != "command" {
		return ""
	}
	return strings.TrimSpace(s.StatusLine.Command)
}

// isDeckStatusLine: a command that runs agentctl statusline (the deck's own).
func isDeckStatusLine(c string) bool {
	return strings.Contains(c, "agentctl") && strings.Contains(c, " statusline")
}
