package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deckCtl = "/Applications/agents deck.app/Contents/MacOS/agentctl"

// The deck's status line goes in, everything else stays in its order, the
// user's status line is kept to chain to, and off puts the file back.
func TestDeckStatusLineAddAndRemove(t *testing.T) {
	acct, save := t.TempDir(), t.TempDir()
	file := filepath.Join(acct, "settings.json")
	orig := `{
  "model": "opus",
  "statusLine": {
    "type": "command",
    "command": "~/bin/my-line.sh",
    "padding": 0
  },
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ]
  },
  "env": {}
}
`
	if err := os.WriteFile(file, []byte(orig), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := AddDeckStatusLine(acct, deckCtl, save); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if !strings.Contains(s, `agentctl\" statusline`) {
		t.Fatalf("no deck status line:\n%s", s)
	}
	if i, j, k := strings.Index(s, `"model"`), strings.Index(s, `"statusLine"`), strings.Index(s, `"permissions"`); !(i < j && j < k) || !strings.Contains(s, `"Bash(ls:*)"`) || !strings.Contains(s, `"env": {}`) {
		t.Errorf("other keys moved or changed:\n%s", s)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want 0640", fi.Mode().Perm())
	}
	if bak, _ := os.ReadFile(file + ".agents-deck.bak"); string(bak) != orig {
		t.Errorf("backup is not the original:\n%s", bak)
	}
	if c := SavedStatusLine(save, acct); c != "~/bin/my-line.sh" {
		t.Errorf("saved status line %q", c)
	}
	// Again: nothing changes, and the saved one is still the user's.
	if err := AddDeckStatusLine(acct, deckCtl, save); err != nil {
		t.Fatal(err)
	}
	if c := SavedStatusLine(save, acct); c != "~/bin/my-line.sh" {
		t.Errorf("after a second add, saved %q", c)
	}
	// The app moved: the deck's command follows, the user's stays saved.
	if err := AddDeckStatusLine(acct, "/other/agentctl", save); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), `/other/agentctl\" statusline`) {
		t.Errorf("command not updated:\n%s", b)
	}
	if c := SavedStatusLine(save, acct); c != "~/bin/my-line.sh" {
		t.Errorf("after the app moved, saved %q", c)
	}

	if err := RemoveDeckStatusLine(acct, save); err != nil {
		t.Fatal(err)
	}
	back, _ := os.ReadFile(file)
	if string(back) != orig {
		t.Errorf("not put back as it was:\n%s\nwant:\n%s", back, orig)
	}
	if c := SavedStatusLine(save, acct); c != "" {
		t.Errorf("saved status line left: %q", c)
	}
}

// No status line before: none after. No settings.json: one is made. No
// config folder: nothing happens. A file that isn't JSON is left alone.
func TestDeckStatusLineEdges(t *testing.T) {
	save := t.TempDir()
	acct := t.TempDir()
	if err := AddDeckStatusLine(acct, deckCtl, save); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(acct, "settings.json")); !strings.Contains(string(b), "statusline") {
		t.Errorf("no settings.json made:\n%s", b)
	}
	if err := RemoveDeckStatusLine(acct, save); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(acct, "settings.json")); string(b) != "{}\n" {
		t.Errorf("after off: %q", b)
	}

	missing := filepath.Join(t.TempDir(), "nope")
	if err := AddDeckStatusLine(missing, deckCtl, save); err != nil {
		t.Errorf("missing folder: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("a missing config folder was made")
	}

	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "settings.json"), []byte("{oops"), 0o600)
	if err := AddDeckStatusLine(bad, deckCtl, save); err == nil {
		t.Error("a broken settings.json was written over")
	}
	if b, _ := os.ReadFile(filepath.Join(bad, "settings.json")); string(b) != "{oops" {
		t.Errorf("broken file changed: %q", b)
	}

	// The user put their own status line in while it was on: off leaves it.
	mine := t.TempDir()
	if err := AddDeckStatusLine(mine, deckCtl, save); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mine, "settings.json"), []byte(`{"statusLine":{"type":"command","command":"echo mine"}}`), 0o600)
	if err := RemoveDeckStatusLine(mine, save); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(mine, "settings.json")); !strings.Contains(string(b), "echo mine") {
		t.Errorf("the user's new status line was removed: %s", b)
	}
}
