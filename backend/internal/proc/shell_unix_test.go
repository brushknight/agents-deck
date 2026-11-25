//go:build !windows

package proc

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindShell(t *testing.T) {
	dir := t.TempDir()
	exe := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fish, bash, sh := exe("fish"), exe("bash"), exe("sh")
	nologin := exe("nologin")
	noLook := func(string) (string, error) { return "", errors.New("none") }

	old := fallbackShells
	defer func() { fallbackShells = old }()
	fallbackShells = []string{sh}

	cases := []struct{ name, env, passwd, want string }{
		{"$SHELL first", fish, bash, fish},
		{"passwd when no $SHELL (a systemd service)", "", fish, fish},
		{"$SHELL that doesn't exist", filepath.Join(dir, "gone"), fish, fish},
		{"nologin isn't a shell", "", nologin, sh},
		{"relative isn't trusted", "zsh", "", sh},
		{"passwd before /bin/sh", "", bash, bash},
		{"/bin/sh last", "", "", sh},
	}
	for _, c := range cases {
		if got := findShell(c.env, c.passwd, noLook); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	// Nothing at a fixed place (NixOS): whatever sh is on PATH.
	fallbackShells = nil
	if got := findShell("", "", func(string) (string, error) { return "/run/current-system/sw/bin/sh", nil }); got != "/run/current-system/sw/bin/sh" {
		t.Errorf("PATH sh: %s", got)
	}
}

func TestShellFromPasswd(t *testing.T) {
	passwd := "root:x:0:0::/root:/run/current-system/sw/bin/bash\n" +
		"demo:x:1000:100:Demo User:/home/demo:/run/current-system/sw/bin/bash\n" +
		"other:x:1001:100::/home/other:/bin/zsh\n"
	if got := shellFromPasswd(bufio.NewScanner(strings.NewReader(passwd)), "demo"); got != "/run/current-system/sw/bin/bash" {
		t.Fatalf("got %q", got)
	}
	if got := shellFromPasswd(bufio.NewScanner(strings.NewReader(passwd)), "nobody"); got != "" {
		t.Fatalf("unknown user: %q", got)
	}
}
