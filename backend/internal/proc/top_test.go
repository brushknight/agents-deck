package proc

import (
	"reflect"
	"testing"
)

// A made-up ps: two agents on their terminals (with a build one of them
// started), the service and a helper it runs, a hook on an agent's terminal,
// an app with helpers, and idle programs.
const psFixture = `    1     0 ??         0.0 /sbin/launchd
  500     1 ??        40.0 /Applications/Example Browser.app/Contents/MacOS/Example Browser
  501   500 ??        20.0 /Applications/Example Browser.app/Contents/Frameworks/Example Helper (Renderer).app/Contents/MacOS/Example Helper (Renderer)
  600     1 ??         2.0 /usr/local/bin/agentctl
  601   600 ??         1.0 /usr/sbin/iostat
  700   690 ttys003   30.0 claude
  701   700 ttys003   50.0 /usr/local/go/bin/go
  703   701 ttys003   12.0 /usr/local/go/pkg/tool/compile
  704   701 ttys003    8.0 /usr/local/go/pkg/tool/compile
  705   700 ttys003    0.0 /bin/sleep
  702   700 ttys003    0.7 /usr/local/bin/agentctl
  710   690 ttys004    8.0 claude
  720   719 ttys009    5.0 -zsh
  800     1 ??         0.0 /usr/libexec/idled
  bad line
`

func TestParsePS(t *testing.T) {
	rows := parsePS(psFixture)
	if len(rows) != 14 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	if r := rows[2]; r.pid != 501 || r.ppid != 500 || r.tty != "??" || r.cpu != 20 ||
		r.comm != "/Applications/Example Browser.app/Contents/Frameworks/Example Helper (Renderer).app/Contents/MacOS/Example Helper (Renderer)" {
		t.Errorf("row %+v", r)
	}
	if r := parsePS("  42     1 pts/3   12,5 kworker/0:1\n"); len(r) != 1 || r[0].cpu != 12.5 || r[0].tty != "pts/3" || r[0].comm != "kworker/0:1" {
		t.Errorf("linux row %+v", r)
	}
}

func TestProgramName(t *testing.T) {
	for comm, want := range map[string]string{
		"/Applications/Example Browser.app/Contents/Frameworks/Example Helper (Renderer).app/Contents/MacOS/Example Helper (Renderer)": "Example Browser",
		"/usr/sbin/iostat": "iostat",
		"claude":           "claude",
		"-zsh":             "zsh",
	} {
		if got := programName(comm); got != want {
			t.Errorf("%q: %q, want %q", comm, got, want)
		}
	}
}

// Each agent gets every process on its terminal, with its biggest programs
// (the compiler it runs, more than the agent itself); agentctl anywhere and the
// service's children are the deck; the rest goes by program (an app with
// its helpers); idle programs are left out. Percent is of the whole
// machine (10 cores here).
func TestGroupCPU(t *testing.T) {
	agents := []AgentTerminal{
		{TTY: "/dev/ttys003", ID: "a1", Title: "fix the login page"},
		{TTY: "/dev/ttys004", ID: "a2", Title: "write the docs"},
		{TTY: "/dev/ttys011", ID: "a3", Title: "gone quiet"},
	}
	got := groupCPU(parsePS(psFixture), agents, 600, 10, 10)
	want := []CPUUser{
		{Kind: "agent", Name: "fix the login page", Agent: "a1", Percent: 10, Processes: 5,
			Commands: []Command{{"go", 5}, {"claude", 3}, {"compile", 2}}},
		{Kind: "program", Name: "Example Browser", Percent: 6, Processes: 2},
		{Kind: "agent", Name: "write the docs", Agent: "a2", Percent: 0.8, Processes: 1, Commands: []Command{{"claude", 0.8}}},
		{Kind: "program", Name: "zsh", Percent: 0.5, Processes: 1},
		{Kind: "deck", Name: "agents deck", Percent: 0.4, Processes: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	// Cut to n, the deck kept in the last place.
	got = groupCPU(parsePS(psFixture), agents, 600, 10, 2)
	if len(got) != 2 || got[0].Agent != "a1" || got[1].Kind != "deck" {
		t.Fatalf("top 2: %+v", got)
	}
	if got = groupCPU(nil, nil, 600, 10, 3); len(got) != 1 || got[0].Kind != "deck" || got[0].Processes != 0 {
		t.Fatalf("no rows: %+v", got)
	}
}

// ps with lstart: five words of start time before the program, which may
// hold spaces.
func TestParseProcesses(t *testing.T) {
	got := parseProcesses(`  700   690 ttys003   30.0 Mon Nov 23 10:04:05 2026     claude
  501   500 ??        20.0 Sun Nov 22 09:00:00 2026     /Applications/Example App.app/Contents/MacOS/Example App
  bad line
`)
	want := []PS{
		{PID: 700, PPID: 690, TTY: "ttys003", CPU: 30, Start: "Mon Nov 23 10:04:05 2026", Comm: "claude"},
		{PID: 501, PPID: 500, TTY: "??", CPU: 20, Start: "Sun Nov 22 09:00:00 2026", Comm: "/Applications/Example App.app/Contents/MacOS/Example App"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if !IsDeck(5, 1, "/usr/local/bin/agentctl", 9) || !IsDeck(9, 1, "x", 9) || !IsDeck(5, 9, "iostat", 9) || IsDeck(5, 1, "claude", 9) {
		t.Error("IsDeck")
	}
}
