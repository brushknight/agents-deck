package accounts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTSTERM_HOME", filepath.Join(home, "data"))
	return home
}

func TestNoAccountsIsTheDefaultFolder(t *testing.T) {
	home := setup(t)
	if InUse() {
		t.Fatal("in use without config")
	}
	if d := Default(); d.Path() != filepath.Join(home, ".claude") || !d.IsHome() || d.ConfigDir() != "" {
		t.Errorf("default: %+v", d)
	}
	if Of("", filepath.Join(home, ".claude", "projects", "p", "s.jsonl")) != nil {
		t.Error("one account: agents carry no account")
	}
}

func TestAccountsFromConfig(t *testing.T) {
	home := setup(t)
	if err := Save([]Account{{Name: "personal", Dir: "~/.claude", Color: "sky"}, {Name: "work", Dir: "~/.claude-work", Color: "yellow"}}, ""); err != nil {
		t.Fatal(err)
	}
	if !InUse() {
		t.Fatal("two accounts: in use")
	}
	if d := Default(); d.Name != "personal" { // the default folder wins without "account"
		t.Errorf("default %q", d.Name)
	}
	work, _ := Find("work")
	if work.ConfigDir() != filepath.Join(home, ".claude-work") || work.IsHome() {
		t.Errorf("work: %+v", work)
	}
	// ~/.claude-work/… is work, not personal (~/.claude is a prefix of the name, not the folder).
	if a := Of("", filepath.Join(home, ".claude-work", "projects", "p", "s.jsonl")); a == nil || a.Name != "work" || a.Color != "yellow" {
		t.Errorf("work transcript: %+v", a)
	}
	if a := Of("", filepath.Join(home, ".claude", "projects", "p", "s.jsonl")); a == nil || a.Name != "personal" {
		t.Errorf("personal transcript: %+v", a)
	}
	if a := Of("work", filepath.Join(home, ".claude", "projects", "p", "s.jsonl")); a == nil || a.Name != "work" {
		t.Errorf("started in work: %+v", a)
	}
	if Of("", "/elsewhere/s.jsonl") != nil {
		t.Error("a transcript outside every account has none")
	}

	time.Sleep(10 * time.Millisecond) // a new mtime for the cache
	if err := Save(List(), "work"); err != nil {
		t.Fatal(err)
	}
	if d := Default(); d.Name != "work" {
		t.Errorf("default after save %q", d.Name)
	}
	if err := Save([]Account{{Name: "Bad Name", Dir: "~/x", Color: "sky"}}, ""); err == nil {
		t.Error("bad name accepted")
	}
	if err := Save([]Account{{Name: "x", Dir: "~/x", Color: "orange"}}, ""); err == nil {
		t.Error("the status orange accepted as a colour")
	}
}

func TestCopySession(t *testing.T) {
	home := setup(t)
	from := Account{Name: "work", Dir: "~/.claude-work", Color: "yellow"}
	to := Account{Name: "personal", Dir: "~/.claude", Color: "sky"}
	proj := filepath.Join(from.ProjectsDir(), "-Users-sam-app")
	src := filepath.Join(proj, "0b6c4f7e-1111-4222-8333-444455556666.jsonl")
	sub := filepath.Join(proj, "0b6c4f7e-1111-4222-8333-444455556666", "subagents", "agent-a.jsonl")
	for p, body := range map[string]string{src: "line 1\nline 2\n", sub: "sub\n"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dst, err := CopySession(src, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude", "projects", "-Users-sam-app", filepath.Base(src)); dst != want {
		t.Errorf("copied to %s, want %s", dst, want)
	}
	if b, _ := os.ReadFile(dst); string(b) != "line 1\nline 2\n" {
		t.Errorf("copy: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(to.ProjectsDir(), "-Users-sam-app", "0b6c4f7e-1111-4222-8333-444455556666", "subagents", "agent-a.jsonl")); string(b) != "sub\n" {
		t.Errorf("subagent transcript not copied: %q", b)
	}
	// Handed back later: the newer copy replaces the old one.
	_ = os.WriteFile(src, []byte("line 1\nline 2\nline 3\n"), 0o600)
	if _, err := CopySession(src, from, to); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "line 1\nline 2\nline 3\n" {
		t.Errorf("second copy: %q", b)
	}
	if b, _ := os.ReadFile(src); string(b) != "line 1\nline 2\nline 3\n" {
		t.Errorf("the original changed: %q", b)
	}
	if _, err := CopySession("/elsewhere/x.jsonl", from, to); err == nil {
		t.Error("a transcript outside the account copied")
	}
}
