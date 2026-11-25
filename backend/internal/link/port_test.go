//go:build darwin || linux

package link

import (
	"errors"
	"maps"
	"os"
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
	flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.(*os.File).Fd(), syscall.F_GETFD, 0)
	if e != 0 {
		t.Fatal(e)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Error("the port is inherited by child processes (no FD_CLOEXEC)")
	}
}

// Holders of the ports as lsof -F pcRn lists them: the daemon (pid 100) and,
// for a moment between fork and exec, its children don't count.
func TestOtherUsersIn(t *testing.T) {
	const a, b = "/dev/cu.usbmodem101", "/dev/cu.usbmodem202"
	cases := []struct {
		lsof string
		want map[string]string
	}{
		{"p100\nR1\ncagentctl\nf5\nn" + a + "\n", map[string]string{}},
		{"p100\nR1\ncagentctl\nf5\nn" + a + "\np230\nR100\ncagentctl\nf5\nn" + a + "\n", map[string]string{}},
		{"p100\nR1\ncagentctl\nf5\nn" + a + "\nf6\nn" + b + "\np300\nR77\ncscreen\nf3\nn" + b + "\n", map[string]string{b: "screen"}},
		{"p300\nR1\ncesptool.py\nf3\nn" + a + "\np301\nR1\ncminicom\nf4\nn" + a + "\nf5\nn" + b + "\n",
			map[string]string{a: "esptool.py", b: "minicom"}},
		{"", map[string]string{}},
	}
	for _, c := range cases {
		if got := otherUsersIn(c.lsof, 100); !maps.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.lsof, got, c.want)
		}
	}
}
