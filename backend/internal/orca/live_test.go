//go:build !windows

package orca

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The live tests read the Orca app running on this machine, through its
// socket and through its CLI, and change nothing in it: they only call
// worktree.ps, terminal.list, terminal read, a method Orca doesn't have, and
// a wrong token. Skipped unless ORCA_LIVE=1 and Orca is running:
//
//	cd backend && ORCA_LIVE=1 go test -run Live -v ./internal/orca ./internal/daemon

func liveClient(t *testing.T) Client {
	t.Helper()
	if os.Getenv("ORCA_LIVE") == "" {
		t.Skip("set ORCA_LIVE=1 to read the running Orca")
	}
	c := Client{Bin: Bin()}
	if c.Bin == "" || !Running() {
		t.Skip("Orca isn't installed or running")
	}
	return c
}

// pinnedCLI is a copy of the CLI that always reads Orca's runtime runtime file:
// the CLI honours ORCA_USER_DATA_PATH too, so without it the fallback in a
// test that moves that variable would see the test's folder.
func pinnedCLI(t *testing.T, bin string) string {
	t.Helper()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	p := filepath.Join(t.TempDir(), "orca")
	script := "#!/bin/sh\nORCA_USER_DATA_PATH=" + q(filepath.Dir(RuntimeFile())) + " exec " + q(bin) + " \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// cliRead is Read through the CLI only (what the deck did before the socket).
func (c Client) cliRead() (Snapshot, error) {
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

// eventually retries f a few times: agents change state between two reads.
func eventually(t *testing.T, what string, f func() (bool, string)) {
	t.Helper()
	var diff string
	for i := 0; i < 5; i++ {
		ok, d := f()
		if ok {
			if i > 0 {
				t.Logf("%s: equal on try %d", what, i+1)
			}
			return
		}
		diff = d
		time.Sleep(300 * time.Millisecond)
	}
	t.Errorf("%s: still different after 5 tries:\n%s", what, diff)
}

func TestLive(t *testing.T) {
	c := liveClient(t)
	s, err := c.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Worktrees {
		t.Logf("worktree %s (%s) agents %d", w.DisplayName, w.Branch, len(w.Agents))
		for _, a := range w.Agents {
			t.Logf("  agent %s %s %s tool=%s", a.PaneKey, a.AgentType, a.State, a.ToolName)
		}
	}
	for _, term := range s.Terminals {
		scr, err := c.Screen(term.Handle)
		t.Logf("terminal %s %q pane %s: %d bytes of screen, %v", term.Handle, term.Title, term.PaneKey(), len(scr), err)
	}
}

// The socket answers what the CLI answers, field for field, except
// terminalVerdict: the CLI adds it to each worktree row itself.
func TestLiveSocketMatchesCLI(t *testing.T) {
	c := liveClient(t)
	if out, err := exec.Command(c.Bin, "--version").Output(); err == nil {
		t.Logf("orca %s", strings.TrimSpace(string(out)))
	}
	strip := func(v any) any {
		if m, ok := v.(map[string]any); ok {
			if ws, ok := m["worktrees"].([]any); ok {
				for _, w := range ws {
					delete(w.(map[string]any), "terminalVerdict")
				}
			}
		}
		return v
	}
	for _, m := range []struct {
		method string
		params map[string]any
		args   []string
	}{
		{"worktree.ps", nil, []string{"worktree", "ps"}},
		{"terminal.list", map[string]any{"includeVisualLayouts": false}, []string{"terminal", "list"}},
	} {
		eventually(t, m.method, func() (bool, string) {
			var sock, cli any
			if err := c.rpc(&sock, m.method, m.params); err != nil {
				t.Fatalf("socket %s: %v", m.method, err)
			}
			if err := c.call(&cli, m.args...); err != nil {
				t.Fatalf("CLI %s: %v", m.method, err)
			}
			strip(sock)
			strip(cli)
			if reflect.DeepEqual(sock, cli) {
				return true, ""
			}
			a, _ := json.MarshalIndent(sock, "", " ")
			b, _ := json.MarshalIndent(cli, "", " ")
			return false, "socket: " + string(a) + "\nCLI: " + string(b)
		})
	}
	eventually(t, "Snapshot", func() (bool, string) {
		sock, err := c.Read()
		if err != nil {
			t.Fatal(err)
		}
		cli, err := c.cliRead()
		if err != nil {
			t.Fatal(err)
		}
		return reflect.DeepEqual(sock, cli), "socket " + dump(sock) + "\nCLI " + dump(cli)
	})
}

// Each way the socket can't be used, against the runtime app: Read still works
// through the CLI.
func TestLiveFallback(t *testing.T) {
	c := liveClient(t)
	if err := c.rpc(nil, "deck.noSuchMethod", nil); !errors.Is(err, errTransport) || !strings.Contains(err.Error(), "method_not_found") {
		t.Errorf("unknown method: %v", err)
	} else {
		t.Logf("unknown method: %v", err)
	}

	runtime := RuntimeFile()
	b, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	meta["authToken"] = "not-the-token"
	wrong, _ := json.Marshal(meta)
	wrongDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongDir, "orca-runtime.json"), wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	pinned := Client{Bin: pinnedCLI(t, c.Bin)}

	for _, tc := range []struct{ name, dir, code string }{
		{"wrong token", wrongDir, "unauthorized"},
		{"no runtime file", t.TempDir(), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ORCA_USER_DATA_PATH", tc.dir)
			err := c.rpc(nil, "worktree.ps", nil)
			if !errors.Is(err, errTransport) || !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("socket: %v", err)
			}
			t.Logf("socket: %v", err)
			// The CLI reads ORCA_USER_DATA_PATH as well, so here it fails too.
			if _, err := c.cliRead(); err != nil {
				t.Logf("CLI with the same ORCA_USER_DATA_PATH: %v", err)
			} else {
				t.Logf("CLI with the same ORCA_USER_DATA_PATH: works (it ignores the variable)")
			}
			eventually(t, "Read through the CLI", func() (bool, string) {
				got, err := pinned.Read()
				if err != nil {
					t.Fatalf("Read: %v", err)
				}
				ref, err := pinned.cliRead()
				if err != nil {
					t.Fatal(err)
				}
				return reflect.DeepEqual(got, ref), "Read " + dump(got) + "\nCLI " + dump(ref)
			})
		})
	}
}

// Logged, not checked: what one deck poll costs each way.
func TestLiveTiming(t *testing.T) {
	c := liveClient(t)
	const n = 5
	time1 := func(f func() (Snapshot, error)) time.Duration {
		var total time.Duration
		for i := 0; i < n; i++ {
			start := time.Now()
			if _, err := f(); err != nil {
				t.Fatal(err)
			}
			total += time.Since(start)
		}
		return total / n
	}
	sock, cli := time1(c.Read), time1(c.cliRead)
	t.Logf("one poll (worktree.ps + terminal.list), mean of %d: socket %v, CLI %v (%.0fx)", n, sock, cli, float64(cli)/float64(sock))
}

func dump(s Snapshot) string {
	b, _ := json.MarshalIndent(s, "", " ")
	return string(b)
}
