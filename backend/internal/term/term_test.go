package term

import "testing"

func TestStealFocusRejectsNonTerminals(t *testing.T) {
	for _, p := range []string{"", "/etc/passwd", "/dev/ttys", "/dev/ttys1", "/dev/ttys012/../../etc/hosts", "/dev/null", "relative/ttys012", "/dev/ttys99999",
		"/dev/pts/", "/dev/pts/x", "/dev/pts/1/../../etc/hosts", "/dev/pts/123456"} {
		if err := StealFocus(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}
