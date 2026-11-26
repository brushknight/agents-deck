package paths

import "testing"

func TestSeparate(t *testing.T) {
	t.Setenv("AGENTSTERM_HOME", "")
	if Separate() {
		t.Error("the default data folder counted as a test deck")
	}
	t.Setenv("AGENTSTERM_HOME", defaultData())
	if Separate() {
		t.Error("AGENTSTERM_HOME set to the default folder counted as a test deck")
	}
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	if !Separate() {
		t.Error("a data folder of its own isn't a test deck")
	}
}
