package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextStatusLine(t *testing.T) {
	acct, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", acct)
	write := func(path, cmd string) {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":`+cmd+`}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := []byte(`{"cwd":"` + proj + `","workspace":{"project_dir":"` + proj + `"}}`)
	if got := nextStatusLine(in); got != "" {
		t.Errorf("no status line anywhere: got %q", got)
	}
	write(filepath.Join(acct, "settings.json"), `"~/bin/my-line.sh"`)
	if got := nextStatusLine(in); got != "~/bin/my-line.sh" {
		t.Errorf("the account's: got %q", got)
	}
	write(filepath.Join(proj, ".claude", "settings.json"), `"echo project"`)
	if got := nextStatusLine(in); got != "echo project" {
		t.Errorf("the project's wins over the account's: got %q", got)
	}
	write(filepath.Join(proj, ".claude", "settings.local.json"), `"\"/Applications/x/agentctl\" statusline"`)
	if got := nextStatusLine(in); got != "echo project" {
		t.Errorf("the deck's own is skipped: got %q", got)
	}
}
