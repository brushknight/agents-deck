package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

func TestClaudeArgv(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Users\sam\.local\bin\claude.exe" --resume x`:                                                      "claude --resume x",
		`C:\nodejs\node.exe C:\Users\sam\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\cli.js -c`: "claude -c",
		`C:\nodejs\node.exe server.js`:                                                                         "",
		`notepad.exe`:                                                                                          "",
	} {
		got := claudeArgv(proc.SplitCommandLine(in))
		if s := join(got); s != want {
			t.Errorf("%s: got %q, want %q", in, s, want)
		}
	}
}

func join(a []string) string {
	s := ""
	for i, x := range a {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s
}

func TestLiveFromProcesses(t *testing.T) {
	projects := t.TempDir()
	start := time.Now().Add(-10 * time.Minute)
	write := func(project, id, cwd string, mod time.Time) {
		dir := filepath.Join(projects, project)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, id+".jsonl")
		line := `{"type":"user","cwd":` + quote(cwd) + `,"sessionId":"` + id + `"}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	explicit := "0b5c2f4e-1111-4222-8333-944455556666"
	fresh := "1b5c2f4e-1111-4222-8333-944455556666"
	old := "2b5c2f4e-1111-4222-8333-944455556666"
	write("C--work-api", explicit, `C:\work\api`, start.Add(-time.Hour)) // resumed by id: age doesn't matter
	write("C--work-web", fresh, `C:\work\web`, time.Now())
	write("C--work-old", old, `C:\work\old`, start.Add(-time.Hour)) // written before anything started
	ps := []proc.Process{
		{PID: 10, CommandLine: `C:\x\claude.exe`, Started: start},
		{PID: 11, CommandLine: `C:\x\claude.exe --resume ` + explicit, Started: start.Add(time.Minute)},
		{PID: 12, CommandLine: `C:\x\claude.exe -p "one shot"`, Started: start},
		{PID: 13, CommandLine: `C:\x\claude.exe`, Started: start.Add(2 * time.Minute)}, // nothing left for it
	}
	got := liveFromProcesses(ps, []string{projects})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	byPID := map[int]Live{}
	for _, l := range got {
		byPID[l.PID] = l
	}
	if l := byPID[11]; l.SessionID != explicit || l.Cwd != `C:\work\api` || l.TTY != "" {
		t.Errorf("explicit: %+v", l)
	}
	if l := byPID[10]; l.SessionID != fresh || l.Cwd != `C:\work\web` || filepath.Base(l.Transcript) != fresh+".jsonl" {
		t.Errorf("newest: %+v", l)
	}
}

func quote(s string) string {
	out := `"`
	for _, r := range s {
		if r == '\\' || r == '"' {
			out += `\`
		}
		out += string(r)
	}
	return out + `"`
}
