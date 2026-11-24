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
  702   700 ttys003    0.7 /usr/local/bin/agentctl
  710   690 ttys004    8.0 claude
  720   719 ttys009    5.0 -zsh
  800     1 ??         0.0 /usr/libexec/idled
  bad line
`

func TestParsePS(t *testing.T) {
	rows := parsePS(psFixture)
	if len(rows) != 11 {
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

// Each agent gets every process on its terminal; agentctl anywhere and the
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
		{Kind: "agent", Name: "fix the login page", Agent: "a1", Percent: 8, Processes: 2},
		{Kind: "program", Name: "Example Browser", Percent: 6, Processes: 2},
		{Kind: "agent", Name: "write the docs", Agent: "a2", Percent: 0.8, Processes: 1},
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
