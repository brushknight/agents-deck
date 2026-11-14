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
