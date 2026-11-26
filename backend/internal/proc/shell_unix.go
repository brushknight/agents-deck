//go:build !windows

package proc

import (
	"bufio"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

// LoginShell is the shell agents start in, as a login shell (`<shell> -lc
// "exec …"`), so they get the user's own PATH and environment. It is the
// user's shell: $SHELL, else the account's login shell (the passwd entry,
// which is what a systemd user service has, with no $SHELL), else /bin/sh,
// else whatever "sh" is on PATH. NixOS has neither zsh nor /bin/bash, so no
// particular shell is assumed.
func LoginShell() string {
	shellOnce.Do(func() { shell = findShell(os.Getenv("SHELL"), passwdShell(), exec.LookPath) })
	return shell
}

var (
	shellOnce sync.Once
	shell     string
)

// fallbackShells are tried when neither $SHELL nor the passwd entry names a
// shell that exists.
var fallbackShells = []string{"/bin/sh"}

func findShell(env, passwd string, look func(string) (string, error)) string {
	for _, s := range append([]string{env, passwd}, fallbackShells...) {
		if usable(s) {
			return s
		}
	}
	if p, err := look("sh"); err == nil {
		return p
	}
	return "/bin/sh"
}

// usable: an absolute path to an executable file. "nologin" and "false"
// shells (service accounts) don't count.
func usable(s string) bool {
	if s == "" || !filepath.IsAbs(s) {
		return false
	}
	switch filepath.Base(s) {
	case "nologin", "false":
		return false
	}
	fi, err := os.Stat(s)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// passwdShell is the user's login shell from /etc/passwd ("" when it isn't
// there: macOS keeps it in Directory Services, where $SHELL is always set).
func passwdShell() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	return shellFromPasswd(bufio.NewScanner(f), u.Username)
}

func shellFromPasswd(sc *bufio.Scanner, name string) string {
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) >= 7 && f[0] == name {
			return f[6]
		}
	}
	return ""
}
