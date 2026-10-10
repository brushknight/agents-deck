package claude

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectDirFor(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if got := ProjectDirFor("/Users/sam/dev/my.app"); got != "/c/projects/-Users-sam-dev-my-app" {
		t.Fatal(got)
	}
}

func TestInteractiveClaude(t *testing.T) {
	for argv, want := range map[string]bool{
		"claude":                              true,
		"/opt/homebrew/bin/claude --resume x": true,
		"claude.exe":                          true,
		"claude -p hello":                     false,
		"claude bg-pty-host --bg-pty-host /tmp/s":                             false,
		"claude bg-spare --bg-spare /tmp/s":                                   false,
		"claude.exe daemon run --origin transient":                            false,
		"claude --settings /u/.local/share/agents-terminal/claude-hooks.json": false,
		"node server.mjs": false,
	} {
		if got := isInteractiveClaude(splitArgs(argv)); got != want {
			t.Errorf("%q: %v", argv, got)
		}
	}
	id := "0b5c2f4e-1111-4222-8333-944455556666"
	if sessionArg(splitArgs("claude --resume "+id)) != id || sessionArg(splitArgs("claude --session-id="+id)) != id || sessionArg(splitArgs("claude -c")) != "" {
		t.Fatal("session args")
	}
}

func splitArgs(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// A hand-started `claude` in a terminal is found with its session.
func TestLiveSessionsFindsAHandStartedClaude(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("no script(1)")
	}
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "cfg"))
	work := filepath.Join(root, "work")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{work, bin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	work, _ = filepath.EvalSymlinks(work) // lsof reports the real path (/private/var/…)
	// "claude" is /bin/sleep under that name (a symlink: copies of system
	// binaries may be killed by endpoint security).
	fake := filepath.Join(bin, "claude")
	if err := os.Symlink("/bin/sleep", fake); err != nil {
		t.Fatal(err)
	}
	// script(1) gives it a real terminal, like a tab would; its input stays
	// open (a pipe we never write to), or it would quit at once.
	cmd := exec.Command("script", "-q", "/dev/null", fake, "20")
	cmd.Dir = work
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	time.Sleep(500 * time.Millisecond)

	id := "0b5c2f4e-1111-4222-8333-944455556666"
	dir := ProjectDirFor(work)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var found *Live
	for _, l := range LiveSessions(nil) {
		if l.Cwd == work {
			found = &l
			break
		}
	}
	if found == nil {
		t.Fatalf("not found among %+v", LiveSessions(nil))
	}
	if found.SessionID != id || found.TTY == "" || found.Transcript != filepath.Join(dir, id+".jsonl") {
		t.Fatalf("%+v", found)
	}
	if got := LiveSessions(map[string]bool{found.TTY: true}); len(got) != 0 {
		for _, l := range got {
			if l.Cwd == work {
				t.Fatal("a skipped tty must not be listed")
			}
		}
	}
}
