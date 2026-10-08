package term

import "testing"

func TestStealFocusRejectsNonTerminals(t *testing.T) {
	for _, p := range []string{"", "/etc/passwd", "/dev/ttys", "/dev/ttys1", "/dev/ttys012/../../etc/hosts", "/dev/null", "relative/ttys012", "/dev/ttys99999"} {
		if err := StealFocus(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}
