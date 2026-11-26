package tmux

import "testing"

func TestSetSocket(t *testing.T) {
	defer func() { Socket = DefaultSocket }()
	for _, ok := range []string{"agentsterm", "deck-test_2", "a.b"} {
		if err := SetSocket(ok); err != nil || Socket != ok {
			t.Errorf("%q: %v (%s)", ok, err, Socket)
		}
	}
	if err := SetSocket(""); err != nil || Socket != DefaultSocket {
		t.Errorf("empty: %v (%s)", err, Socket)
	}
	for _, bad := range []string{"../x", "a b", "x/y", ".hidden", string(make([]byte, 65))} {
		Socket = "keep"
		if err := SetSocket(bad); err == nil || Socket != "keep" {
			t.Errorf("%q accepted (%s)", bad, Socket)
		}
	}
}
