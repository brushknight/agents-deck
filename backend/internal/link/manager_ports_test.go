package link

import (
	"errors"
	"os"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/serial"
)

func TestPortPresent(t *testing.T) {
	missing := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	there := func(string) (os.FileInfo, error) { return nil, nil }
	listed := func() ([]serial.Info, error) { return []serial.Info{{Path: "COM3"}, {Path: "COM12"}}, nil }
	broken := func() ([]serial.Info, error) { return nil, errors.New("registry") }
	cases := []struct {
		goos, path string
		stat       func(string) (os.FileInfo, error)
		list       func() ([]serial.Info, error)
		want       bool
	}{
		{"darwin", "/dev/cu.usbmodem101", there, nil, true},
		{"linux", "/dev/ttyACM0", missing, nil, false},
		// Windows: os.Stat is never asked, the listed ports decide.
		{"windows", "COM12", missing, listed, true},
		{"windows", "com3", missing, listed, true},
		{"windows", `\\.\COM12`, missing, listed, true},
		{"windows", "COM5", there, listed, false},
		{"windows", "COM5", missing, broken, true},
	}
	for _, c := range cases {
		if got := portPresent(c.goos, c.path, c.stat, c.list); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.goos, c.path, got, c.want)
		}
	}
}

func TestLsofPath(t *testing.T) {
	noPath := func(string) (string, error) { return "", errors.New("not found") }
	onPath := func(string) (string, error) { return "/opt/tools/lsof", nil }
	only := func(want string) func(string) (os.FileInfo, error) {
		return func(p string) (os.FileInfo, error) {
			if p == want {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
	}
	none := only("")
	cases := []struct {
		goos string
		look func(string) (string, error)
		stat func(string) (os.FileInfo, error)
		want string
	}{
		{"windows", onPath, only("/usr/bin/lsof"), ""},
		{"linux", onPath, none, "/opt/tools/lsof"},
		{"darwin", noPath, only("/usr/sbin/lsof"), "/usr/sbin/lsof"},
		{"linux", noPath, only("/usr/bin/lsof"), "/usr/bin/lsof"},
		{"linux", noPath, none, ""},
	}
	for _, c := range cases {
		if got := lsofPath(c.goos, c.look, c.stat); got != c.want {
			t.Errorf("%s: %q, want %q", c.goos, got, c.want)
		}
	}
}
