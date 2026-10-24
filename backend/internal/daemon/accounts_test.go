package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// An agent out of usage in one account continues its conversation in the
// other: same tile, transcript copied, Claude started with the other folder.
func TestHandoff(t *testing.T) {
	if _, err := exec.LookPath(tmux.Bin); err != nil {
		t.Skip("no tmux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTSTERM_HOME", filepath.Join(home, "data"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "elsewhere")) // the agents' own folder must win
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-h%d", os.Getpid())
	t.Cleanup(func() { exec.Command(tmux.Bin, "-L", tmux.Socket, "kill-server").Run(); tmux.Socket = old })

	// A stand-in claude on the login shell's PATH: records how it was started.
	bin, runs := filepath.Join(home, "bin"), filepath.Join(home, "runs.log")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"dir=${CLAUDE_CONFIG_DIR-unset} args=$*\" >> " + runs + "\nexec sleep 60\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zprofile"), []byte("export PATH="+bin+":$PATH\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Save([]accounts.Account{{Name: "personal", Dir: "~/.claude", Color: "sky"}, {Name: "work", Dir: "~/.claude-work", Color: "yellow"}}, "work"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	const sid = "0b6c4f7e-1111-4222-8333-444455556666"
	src := filepath.Join(home, ".claude-work", "projects", "-work-app", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := store.New(filepath.Join(home, "agents.json"), "test", "0")
	st.Account = accounts.Of
	d := &Daemon{Store: st, Agentctl: "/bin/true"}
	id := store.NewID()
	st.Add(&store.Entry{TmuxName: "a-" + id, SessionID: sid, Transcript: src, Account: "work",
		A: model.Agent{ID: id, Tool: "claude", Status: model.Error, Cwd: dir, Folder: filepath.Base(dir)}})
	if err := tmux.NewSession("a-"+id, dir, nil, []string{"sleep", "60"}); err != nil { // the out-of-usage Claude
		t.Fatal(err)
	}
	if a := st.Snapshot().Agents[0].Account; a == nil || a.Name != "work" || a.Color != "yellow" {
		t.Fatalf("account before: %+v", a)
	}

	if _, err := d.Handoff(id, "work"); err == nil {
		t.Error("handing over to its own account should fail")
	}
	if _, err := d.Handoff(id, "nope"); err == nil {
		t.Error("an unknown account should fail")
	}
	got, err := d.Handoff(id, "personal")
	if err != nil || got != id {
		t.Fatalf("handoff: %q, %v", got, err)
	}
	e, _ := st.Get(id)
	dst := filepath.Join(home, ".claude", "projects", "-work-app", sid+".jsonl")
	if e.Account != "personal" || e.Transcript != dst || e.A.Status != model.Starting {
		t.Fatalf("entry after: account %q transcript %q status %s", e.Account, e.Transcript, e.A.Status)
	}
	if b, _ := os.ReadFile(dst); string(b) != `{"type":"user"}`+"\n" {
		t.Fatalf("transcript not copied: %q", b)
	}
	if a := st.Snapshot().Agents[0].Account; a == nil || a.Name != "personal" {
		t.Fatalf("account after: %+v", a)
	}
	// Claude Code's default folder: CLAUDE_CONFIG_DIR unset, whatever the shell had.
	waitRun(t, runs, "dir=unset args=--resume "+sid+" --settings")

	// And back again: the work folder, by CLAUDE_CONFIG_DIR.
	if _, err := d.Handoff(id, "work"); err != nil {
		t.Fatal(err)
	}
	waitRun(t, runs, "dir="+filepath.Join(home, ".claude-work")+" args=--resume "+sid)
}

func waitRun(t *testing.T, log, want string) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if b, _ := os.ReadFile(log); strings.Contains(string(b), want) {
			return
		}
	}
	b, _ := os.ReadFile(log)
	t.Fatalf("claude never ran with %q; runs:\n%s", want, b)
}
