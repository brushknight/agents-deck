package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// demoDeck builds agentctl, starts `agentctl serve --demo` in a temp home
// --still (its fixture fleet, standing still: running agents, one lost, one
// ended) and gives a fake
// HOME holding two made-up Claude sessions. It returns the environment for
// running agentctl against it, with the built binary first on PATH.
func demoDeck(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets and zsh")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool to build agentctl")
	}
	bin := t.TempDir()
	build := exec.Command(gobin, "build", "-o", filepath.Join(bin, "agentctl"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	data, home := t.TempDir(), t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	cfg := fmt.Sprintf(`{"deviceAddr": "", "localAddr": %q}`, addr)
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	// Two Claude sessions not on the deck, written 3 and 5 hours ago.
	proj := filepath.Join(home, ".claude", "projects", "-work-notes")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, s := range []struct{ id, line string }{
		{"0b5c2f4e-1111-4222-8333-944455556666", `{"type":"ai-title","aiTitle":"Tidy Meeting Notes"}`},
		{"7d1e0a33-2222-4333-8444-a55566667777", `{"type":"custom-title","customTitle":"Fix the user's login"}`},
	} {
		p := filepath.Join(proj, s.id+".jsonl")
		body := `{"type":"user","cwd":"` + home + `","sessionId":"` + s.id + `","message":{"role":"user","content":"hi"}}` + "\n" + s.line + "\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-time.Duration(3+2*i) * time.Hour)
		_ = os.Chtimes(p, when, when)
	}
	env := []string{"AGENTSTERM_HOME=" + data, "HOME=" + home, "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "AGENTSTERM_HOME", "HOME", "PATH", "AGENTSTERM_SOCK", "CLAUDE_CONFIG_DIR", "TMUX", "ZDOTDIR":
			continue
		}
		env = append(env, kv)
	}
	// --still: the order by last activity is what the fixture says, however
	// long the test takes (a moving fleet marks every running agent active
	// every few seconds).
	srv := exec.Command(filepath.Join(bin, "agentctl"), "serve", "--demo", "--still", "--log", filepath.Join(data, "agentd.log"))
	srv.Env = env
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })
	for i := 0; ; i++ {
		ls := exec.Command(filepath.Join(bin, "agentctl"), "ls")
		ls.Env = env
		if out, err := ls.CombinedOutput(); err == nil && strings.Contains(string(out), "r3l34s") {
			break
		} else if i == 100 {
			log, _ := os.ReadFile(filepath.Join(data, "agentd.log"))
			t.Fatalf("demo service didn't come up: %v %s\n%s", err, out, log)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return env
}

func run(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	// The agentctl built for this test, by its full path: exec.Command("agentctl")
	// looks it up in the test's own PATH and fails where none is installed.
	bin := "agentctl"
	for _, kv := range env {
		if p, ok := strings.CutPrefix(kv, "PATH="); ok {
			bin = filepath.Join(strings.Split(p, string(os.PathListSeparator))[0], "agentctl")
		}
	}
	c := exec.Command(bin, args...)
	c.Env = env
	out, err := c.CombinedOutput()
	return string(out), err
}

type completion struct {
	buf   string   // the command line after Tab
	cands []string // what zsh offered, in order
}

// complete types each line into a real zsh and presses Tab (testdata/complete.zsh).
func complete(t *testing.T, env []string, lines ...string) []completion {
	t.Helper()
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	if exec.Command(zsh, "-fc", "zmodload zsh/zpty").Run() != nil {
		t.Skip("zsh has no zpty")
	}
	out := filepath.Join(t.TempDir(), "out")
	script, _ := filepath.Abs("testdata/complete.zsh")
	c := exec.Command(zsh, append([]string{"-f", script, out}, lines...)...)
	c.Env = env
	if b, err := c.CombinedOutput(); err != nil {
		res, _ := os.ReadFile(out)
		t.Fatalf("complete.zsh: %v\n%s\n%s", err, b, res)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var res []completion
	var cur *completion
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "BUF "):
			res = append(res, completion{buf: strings.TrimPrefix(line, "BUF ")})
			cur = &res[len(res)-1]
		case strings.HasPrefix(line, "CAND ") && cur != nil:
			cur.cands = append(cur.cands, strings.TrimPrefix(line, "CAND "))
		}
	}
	if len(res) != len(lines) {
		t.Fatalf("got %d completions for %d lines: %+v", len(res), len(lines), res)
	}
	return res
}

// The zsh completion and resume/restore by name, against a demo deck.
func TestCompletionAndBringBack(t *testing.T) {
	env := demoDeck(t)

	t.Run("zsh", func(t *testing.T) {
		got := complete(t, env,
			"agentctl resume inf",     // 0: part of a name, several
			"agentctl resume RELEASE", // 1: any case, a name with spaces
			"agentctl attach NET",     // 2: the middle of a name
			"agentctl resume user",    // 3: a session whose name has a quote
			"agentctl resume ",        // 4: everything, in order
			"agentctl restore cle",    // 5: restore offers the same
			"agentctl rm r3l",         // 6: an id
			"agentctl ls --sort ",     // 7
		)
		if c := got[0]; c.buf != "agentctl resume infra" || !slices.Equal(c.cands, []string{"Infra Cleanup", "infra-network", "infra-tf"}) {
			t.Errorf("inf: %+v (want the ended agent first, then most recently active)", c)
		}
		if c := got[1]; c.buf != `agentctl resume Release\ Notes\ v2 ` {
			t.Errorf("RELEASE: %q", c.buf)
		}
		if c := got[2]; c.buf != "agentctl attach infra-network " {
			t.Errorf("NET: %q", c.buf)
		}
		if c := got[3]; c.buf != `agentctl resume Fix\ the\ user\'s\ login ` {
			t.Errorf("user: %q", c.buf)
		}
		c := got[4]
		if len(c.cands) < 6 || c.cands[0] != "Release Notes v2" || c.cands[1] != "Infra Cleanup" || c.cands[2] != "checkout-api" {
			t.Errorf("all: lost, then ended, then the most recently active: %q", c.cands)
		}
		if i, j := slices.Index(c.cands, "Tidy Meeting Notes"), slices.Index(c.cands, "Fix the user's login"); i < 0 || j != i+1 || i < slices.Index(c.cands, "search-index") {
			t.Errorf("all: sessions last, newest first: %q", c.cands)
		}
		if slices.Contains(c.cands, "r3l34s") {
			t.Errorf("all: ids are hidden from the list: %q", c.cands)
		}
		if c := got[5]; c.buf != `agentctl restore Infra\ Cleanup ` {
			t.Errorf("restore cle: %q", c.buf)
		}
		if c := got[6]; c.buf != "agentctl rm r3l34s " {
			t.Errorf("rm r3l: %q", c.buf)
		}
		if c := got[7]; !slices.Equal(c.cands, []string{"position", "active"}) {
			t.Errorf("ls --sort: %q", c.cands)
		}
	})

	t.Run("ls", func(t *testing.T) {
		out, err := run(t, env, "ls", "--sort", "active")
		if err != nil {
			t.Fatal(err, out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if !strings.Contains(lines[0], "ACTIVE") || !strings.Contains(lines[1], "just now") {
			t.Errorf("ls --sort active: the most recently active first:\n%s", out)
		}
		if i, j, k := strings.Index(out, "Release Notes v2"), strings.Index(out, "Infra Cleanup"), strings.Index(out, "scr4tc"); i < 0 || j < i || k < j {
			t.Errorf("5h ago, then 2d ago, then not known:\n%s", out)
		}
		if _, err := run(t, env, "ls", "--sort", "name"); err == nil {
			t.Error("an unknown order must fail")
		}
	})

	t.Run("bring back", func(t *testing.T) {
		out, err := run(t, env, "resume", "-d", "net")
		if err != nil || !strings.Contains(out, "running") || !strings.Contains(out, "h0m3l4") {
			t.Errorf("a running agent: %v %s", err, out)
		}
		out, err = run(t, env, "resume", "infra")
		if err == nil || !strings.Contains(out, `"infra" matches 3 agents or sessions`) || !strings.Contains(out, "1nfr4t") || !strings.Contains(out, "cl34nx") {
			t.Errorf("several: %v %s", err, out)
		}
		out, err = run(t, env, "resume", "nothing-called-this")
		if err == nil || !strings.Contains(out, "agentctl sessions") {
			t.Errorf("none: %v %s", err, out)
		}
		// attach on an agent that isn't running says what it is and how to
		// bring it back (with no terminal to ask on).
		out, err = run(t, env, "attach", "release")
		if err == nil || !strings.Contains(out, "lost with the tmux server") || !strings.Contains(out, "agentctl resume r3l34s") {
			t.Errorf("attach a lost agent: %v %s", err, out)
		}
		out, err = run(t, env, "restore", "bvifx2")
		if err == nil || !strings.Contains(out, `no agent or session matches "bvifx2"`) {
			t.Errorf("restore with an unknown name must fail, not restore everything: %v %s", err, out)
		}
		if out, _ := run(t, env, "ls"); !strings.Contains(out, "lost") {
			t.Errorf("nothing may have been restored:\n%s", out)
		}
		out, err = run(t, env, "restore", "-d", "release")
		if err != nil || !strings.Contains(out, "restored") || !strings.Contains(out, "r3l34s") {
			t.Errorf("lost: %v %s", err, out)
		}
		out, err = run(t, env, "resume", "-d", "CLEANUP")
		if err != nil || !strings.Contains(out, "resumed") || !strings.Contains(out, "cl34nx") {
			t.Errorf("ended: %v %s", err, out)
		}
		out, err = run(t, env, "restore")
		if err != nil || !strings.Contains(out, "nothing to restore") {
			t.Errorf("restore with nothing lost: %v %s", err, out)
		}
		out, err = run(t, env, "rm", "infra")
		if err == nil || !strings.Contains(out, "agentctl rm ") {
			t.Errorf("rm must not guess: %v %s", err, out)
		}
		out, err = run(t, env, "attach", "nothing-called-this")
		if err == nil || !strings.Contains(out, "no agent matches") {
			t.Errorf("attach none: %v %s", err, out)
		}
	})
}
