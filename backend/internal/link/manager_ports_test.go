package link

import (
	"errors"
	"os"
	"slices"
	"testing"
	"time"

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

// A port the deck holds is looked at for another program only when its board
// goes quiet (backing off while it stays quiet) and on a slow backstop: lsof
// is expensive, and it ran every few seconds on every port.
func TestPortLookSchedule(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	s := &Session{PingEvery: 5 * time.Second}
	s.heard(t0)
	r := &portRun{session: s, done: make(chan struct{}), nextLook: t0.Add(lookEvery)}
	m := &Manager{running: map[string]*portRun{"/dev/x": r}}
	p := PortConfig{Path: "/dev/x"}
	due := func(at time.Duration) bool { return m.due(p, nil, t0.Add(at)) }

	// The board talks: nothing until the backstop.
	for at := time.Second; at < lookEvery; at += 30 * time.Second {
		s.heard(t0.Add(at))
		if due(at) {
			t.Fatalf("looked at %s while the board talks", at)
		}
	}
	s.heard(t0.Add(lookEvery - time.Second))
	if !due(lookEvery) {
		t.Fatal("no backstop look")
	}
	r.looked(t0.Add(lookEvery))
	if r.quietWait != 0 || !r.nextLook.Equal(t0.Add(2*lookEvery)) {
		t.Fatalf("after a look while talking: wait %s, next %s", r.quietWait, r.nextLook.Sub(t0))
	}

	// It goes quiet: a look right away, then backing off to quietMax.
	last := lookEvery - time.Second
	at := last + quietAfter + time.Second
	if !due(at) {
		t.Fatal("no look when the board went quiet")
	}
	var waits []time.Duration
	for range 7 {
		r.looked(t0.Add(at))
		waits = append(waits, r.quietWait)
		if due(at + r.quietWait - time.Second) {
			t.Fatalf("looked again before the backoff (%s)", r.quietWait)
		}
		at += r.quietWait
		if !due(at) {
			t.Fatalf("no look after the backoff (%s)", r.quietWait)
		}
	}
	want := []time.Duration{quietFirst, 2 * quietFirst, 4 * quietFirst, 8 * quietFirst, quietMax, quietMax, quietMax}
	if !slices.Equal(waits, want) {
		t.Fatalf("backoff %v, want %v", waits, want)
	}

	// It talks again: back to the backstop.
	s.heard(t0.Add(at))
	r.looked(t0.Add(at))
	s.heard(t0.Add(at + time.Minute - time.Second))
	if r.quietWait != 0 || due(at+time.Minute) {
		t.Fatal("still looking often after the board came back")
	}

	// A session that ended needs no look.
	close(r.done)
	if due(at + lookEvery) {
		t.Fatal("looked at a port whose session ended")
	}
}
