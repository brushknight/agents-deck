//go:build darwin || linux

package link

import (
	"errors"
	"syscall"
	"testing"
)

// The port must not leak into programs the daemon starts: a tmux server
// started while a board is connected would hold it open for good.
func TestOpenPortClosesOnExec(t *testing.T) {
	ptm, name, err := openPTY()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptm.Close()
	f, err := OpenPort(name, 115200)
	if errors.Is(err, syscall.ENOTTY) {
		t.Skip("this system's ptys don't take a custom baud rate (macOS); Linux runs it")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFD, 0)
	if e != 0 {
		t.Fatal(e)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Error("the port is inherited by child processes (no FD_CLOEXEC)")
	}
}

// Holders of the port as lsof -F pcR lists them: the daemon (pid 100) and,
// for a moment between fork and exec, its children don't count.
func TestOtherUser(t *testing.T) {
	cases := []struct{ lsof, want string }{
		{"p100\nR1\ncagentctl\nf5\n", ""},
		{"p100\nR1\ncagentctl\nf5\np230\nR100\ncagentctl\nf5\n", ""},
		{"p100\nR1\ncagentctl\nf5\np300\nR77\ncscreen\nf3\n", "screen"},
		{"p300\nR1\ncesptool.py\nf3\n", "esptool.py"},
	}
	for _, c := range cases {
		if got := otherUser(c.lsof, 100); got != c.want {
			t.Errorf("%q: %q, want %q", c.lsof, got, c.want)
		}
	}
}
