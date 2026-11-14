//go:build linux

package term

import "testing"

func TestScreenOf(t *testing.T) {
	for in, want := range map[string]string{
		"(['e5ad11e1-fd0d-4cd9-ab4c-409584a51cee'],)\n":                                       "e5ad11e1-fd0d-4cd9-ab4c-409584a51cee",
		"(['e5ad11e1-fd0d-4cd9-ab4c-409584a51cee', '00000000-0000-0000-0000-000000000000'],)": "e5ad11e1-fd0d-4cd9-ab4c-409584a51cee",
		"(@as [],)\n": "",
		"":            "",
	} {
		if got := screenOf(in); got != want {
			t.Errorf("screenOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWindowTitled(t *testing.T) {
	list := "0x02000003 -1 Shangri-La plank\n" +
		"0x03c0823c  0 Shangri-La agents-deck-0011223344556677\n" +
		"0x05400003  0 Shangri-La agents-deck-0011223344556677 - Brave\n"
	if got := windowTitled(list, "agents-deck-0011223344556677"); got != "0x03c0823c" {
		t.Errorf("got %q, want the terminal's window", got)
	}
	if got := windowTitled(list, "agents-deck-ffff"); got != "" {
		t.Errorf("got %q for a title nobody has", got)
	}
}
