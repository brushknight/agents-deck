package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/paths"
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
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if sum, ok := rateLimitsSum(in); ok && reportDue(paths.Data(), dir, sum, time.Now()) {
		c := unixClient(300 * time.Millisecond)
		resp, err := c.Post("http://agentd/statusline?dir="+url.QueryEscape(dir), "application/json", bytes.NewReader(in))
		if err == nil {
			ok := resp.StatusCode/100 == 2
			resp.Body.Close()
			if ok {
				reported(paths.Data(), dir, sum, time.Now())
			}
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
	// With "Report usage from every Claude session" on, the account's own
	// status line was moved aside for the deck's: it still runs.
	return claude.SavedStatusLine(paths.StatusLineSaved(), dir)
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
func isDeckStatusLine(c string) bool { return claude.IsDeckStatusLine(c) }

// Claude Code runs the status line often (several times a second while an
// agent works), but the limits in it change far less: the deck is told only
// when they changed for that account, or when the last report is older than
// reportEvery (so "as of" stays fresh while agents work).
const reportEvery = 5 * time.Minute

// rateLimitsSum is a hash of the rate_limits in the status line input (false
// when there are none).
func rateLimitsSum(in []byte) (string, bool) {
	var p struct {
		RateLimits json.RawMessage `json:"rate_limits"`
	}
	if json.Unmarshal(in, &p) != nil || len(p.RateLimits) == 0 || string(p.RateLimits) == "null" {
		return "", false
	}
	var buf bytes.Buffer
	if json.Compact(&buf, p.RateLimits) != nil {
		return "", false
	}
	h := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(h[:8]), true
}

// reportFile keeps the last report sent for one account's config folder.
func reportFile(data, dir string) string {
	h := sha256.Sum256([]byte(filepath.Clean(dir)))
	return filepath.Join(data, "statusline", hex.EncodeToString(h[:6]))
}

// reportDue: these limits weren't sent for this account yet, or not lately.
func reportDue(data, dir, sum string, now time.Time) bool {
	b, err := os.ReadFile(reportFile(data, dir))
	if err != nil {
		return true
	}
	var last string
	var at int64
	if n, _ := fmt.Sscanf(string(b), "%s %d", &last, &at); n != 2 {
		return true
	}
	return last != sum || now.Sub(time.Unix(at, 0)) >= reportEvery
}

// reported notes a report the deck took.
func reported(data, dir, sum string, now time.Time) {
	f := reportFile(data, dir)
	if os.MkdirAll(filepath.Dir(f), 0o700) != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d", f, os.Getpid())
	if os.WriteFile(tmp, []byte(fmt.Sprintf("%s %d\n", sum, now.Unix())), 0o600) == nil {
		_ = os.Rename(tmp, f)
	}
}
