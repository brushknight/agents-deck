package iterm2

import (
	"os"
	"testing"
	"time"
)

func selected(t *testing.T, c *Conn) string {
	ss, err := c.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, s := range ss {
		if s.Selected {
			tty, _ := c.Variable(s.ID, "tty")
			out += " tab" + s.TabID + ":" + tty
		}
	}
	return out
}

// ITERM_LIVE=1 [ITERM_ACTIVATE=/dev/ttysNNN] go test -run TestLive ./internal/iterm2 -v
func TestLive(t *testing.T) {
	if os.Getenv("ITERM_LIVE") == "" {
		t.Skip("set ITERM_LIVE=1 to talk to the running iTerm")
	}
	cookie, key, err := RequestCookie("agentctl")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(cookie, key, "agentctl")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	t.Logf("selected before:%s", selected(t, c))
	if want := os.Getenv("ITERM_ACTIVATE"); want != "" {
		ss, _ := c.Sessions()
		for _, s := range ss {
			if tty, _ := c.Variable(s.ID, "tty"); tty == want {
				t.Logf("activate tab%s %s: %v", s.TabID, tty, c.Activate(s.ID))
			}
		}
		time.Sleep(500 * time.Millisecond)
		t.Logf("selected after:%s", selected(t, c))
	}
}
