package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDefaultsKeepSelectionAndPassKeys(t *testing.T) {
	if _, err := exec.LookPath(Bin); err != nil {
		t.Skip("no tmux")
	}
	old := Socket
	Socket = fmt.Sprintf("agentsdeck-test-%d", os.Getpid())
	t.Cleanup(func() { _, _ = run("kill-server"); Socket = old })
	if err := NewSession("t", t.TempDir(), nil, []string{"cat"}); err != nil {
		t.Fatal(err)
	}
	keys, err := run("list-keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"copy-mode MouseDragEnd1Pane send-keys -X copy-pipe-no-clear /usr/bin/pbcopy",
		"copy-mode-vi MouseDragEnd1Pane send-keys -X copy-pipe-no-clear /usr/bin/pbcopy",
		`copy-mode a send-keys -X cancel \; send-keys -H 61`,
		`copy-mode \; send-keys -X cancel \; send-keys -H 3b`,
		`copy-mode-vi Enter send-keys -X cancel \; send-keys Enter`,
		"root C-q detach-client",
		`root MouseUp1Pane if-shell -F "#{mouse_any_flag}" { send-keys -M } { run-shell -b "'`,
	} {
		found := false
		for _, l := range strings.Split(keys, "\n") {
			if strings.HasPrefix(strings.Join(strings.Fields(l), " "), "bind-key -T "+want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing binding: %s", want)
		}
	}
	// Scrolling the history still works in copy mode.
	if !strings.Contains(strings.Join(strings.Fields(keys), " "), "copy-mode WheelUpPane select-pane") {
		t.Error("wheel binding lost")
	}
}

func TestServerDefaultsMouseModes(t *testing.T) {
	if _, err := exec.LookPath(Bin); err != nil {
		t.Skip("no tmux")
	}
	old, oldMouse := Socket, NativeMouse
	Socket = fmt.Sprintf("agentsdeck-test-m%d", os.Getpid())
	t.Cleanup(func() { _, _ = run("kill-server"); Socket, NativeMouse = old, oldMouse })
	NativeMouse = func() bool { return true }
	dir := t.TempDir()
	if err := NewSession("t", dir, nil, []string{"cat"}); err != nil {
		t.Fatal(err)
	}
	show := func(args ...string) string { out, _ := run(append([]string{"show-options"}, args...)...); return out }
	if v := show("-gv", "set-titles-string"); v != TitleFormat || show("-gv", "set-titles") != "on" {
		t.Errorf("terminal title should follow the pane title: %q", v)
	}
	for title, want := range map[string]string{"✳ CMM server": "CMM server", "◐ Agents Deck": "Agents Deck", "✳": filepath.Base(dir), "/var space usage": "/var space usage"} {
		_, _ = run("select-pane", "-t", "t", "-T", title)
		if got, _ := run("display-message", "-p", "-t", "t", TitleFormat); strings.TrimSpace(got) != want {
			t.Errorf("tab title for %q = %q, want %q", title, got, want)
		}
	}
	if m := show("-gv", "mouse"); m != "off" {
		t.Errorf("native: mouse = %q", m)
	}
	if f := show("-s", "terminal-features"); !strings.Contains(f, "*:hyperlinks") {
		t.Errorf("hyperlinks not passed through: %s", f)
	}
	if o := show("-s", "terminal-overrides"); !strings.Contains(o, "*:smcup@:rmcup@") {
		t.Errorf("alternate screen not skipped: %s", o)
	} else if strings.Contains(o, "Hls@") {
		t.Errorf("native: links to iTerm still removed: %s", o)
	}
	NativeMouse = func() bool { return false }
	ServerDefaults()
	if m := show("-gv", "mouse"); m != "on" {
		t.Errorf("tmux: mouse = %q", m)
	}
	if o := show("-s", "terminal-overrides"); strings.Contains(o, "smcup@") {
		t.Errorf("tmux mode keeps the alternate screen: %s", o)
	}
	if f := show("-s", "terminal-features"); strings.Contains(f, "*:hyperlinks") {
		t.Errorf("tmux mode passes hyperlinks to the terminal: %s", f)
	}
	// tmux turns hyperlinks on by itself for iTerm: the capability must go.
	if o := show("-s", "terminal-overrides"); !strings.Contains(o, "*:Hls@") {
		t.Errorf("tmux mode: link capability not removed: %s", o)
	}
	NativeMouse = func() bool { return true }
	ServerDefaults()
	ServerDefaults() // idempotent: no duplicate entries
	if f := show("-s", "terminal-features"); strings.Count(f, "*:hyperlinks") != 1 {
		t.Errorf("duplicated: %s", f)
	}
}
