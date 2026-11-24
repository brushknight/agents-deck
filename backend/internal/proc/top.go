package proc

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AgentTerminal is a deck agent and the terminal it runs on (ttys012,
// pts/3): every process on that terminal counts as the agent's.
type AgentTerminal struct {
	TTY   string
	ID    string
	Title string
}

// CPUUser is one user of the machine's CPU: a deck agent, the deck itself,
// or any other program.
type CPUUser struct {
	Kind      string  `json:"kind"`            // "agent", "deck" or "program"
	Name      string  `json:"name"`            // the agent's title, "agents deck", or the program
	Agent     string  `json:"agent,omitempty"` // the agent's id
	Percent   float64 `json:"percent"`         // of the whole machine, as MachineCPU
	Processes int     `json:"processes"`
}

// CPUTop is who uses the machine's CPU, biggest first.
type CPUTop struct {
	Available bool      `json:"available"`
	Why       string    `json:"why,omitempty"` // when not available
	Users     []CPUUser `json:"users"`
}

// TopUsers runs ps once and returns the n biggest users of the CPU. ps's
// %cpu is a decaying average of the last minute or so on macOS, and the
// average over each process's life on Linux. Not on Windows.
func TopUsers(agents []AgentTerminal, n int) CPUTop {
	if runtime.GOOS == "windows" {
		return CPUTop{Why: "not measured on Windows", Users: []CPUUser{}}
	}
	all := "-ax"
	if runtime.GOOS == "linux" {
		all = "-e"
	}
	out, err := Output(10*time.Second, "/bin/ps", all, "-o", "pid=,ppid=,tty=,%cpu=,comm=")
	if err != nil {
		return CPUTop{Why: "ps: " + err.Error(), Users: []CPUUser{}}
	}
	return CPUTop{Available: true, Users: groupCPU(parsePS(string(out)), agents, os.Getpid(), runtime.NumCPU(), n)}
}

type psRow struct {
	pid, ppid int
	tty       string
	cpu       float64 // percent of one core
	comm      string
}

// parsePS reads `ps -o pid=,ppid=,tty=,%cpu=,comm=`; the program's path is
// last and may hold spaces.
func parsePS(out string) []psRow {
	var rows []psRow
	for _, l := range strings.Split(out, "\n") {
		var f [4]string
		rest := strings.TrimSpace(l)
		ok := true
		for i := range f {
			var found bool
			f[i], rest, found = strings.Cut(rest, " ")
			rest = strings.TrimLeft(rest, " ")
			ok = ok && found
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		cpu, e3 := strconv.ParseFloat(strings.Replace(f[3], ",", ".", 1), 64)
		if !ok || rest == "" || e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		rows = append(rows, psRow{pid, ppid, f[2], cpu, rest})
	}
	return rows
}

// programName is the name to group a process under: the app it belongs to
// (the first "Name.app" in its path, so an app's helpers count as the app),
// else the program's file name.
func programName(comm string) string {
	if i := strings.Index(comm, ".app/"); i >= 0 {
		comm = comm[:i]
	}
	return strings.TrimPrefix(filepath.Base(comm), "-") // -zsh: a login shell
}

// groupCPU adds up ps rows into the deck (this service, its helpers right
// now, any other agentctl), each agent (every process on its terminal), and
// every other program by name, in percent of the whole machine, and keeps
// the n biggest. The deck is always listed; others only when they use some
// CPU.
func groupCPU(rows []psRow, agents []AgentTerminal, self, cores, n int) []CPUUser {
	n = max(n, 1)
	byTTY := map[string]AgentTerminal{}
	for _, a := range agents {
		byTTY[strings.TrimPrefix(a.TTY, "/dev/")] = a
	}
	deck := &CPUUser{Kind: "deck", Name: "agents deck"}
	groups := map[string]*CPUUser{"deck": deck}
	for _, r := range rows {
		name := programName(r.comm)
		key, u := "deck", CPUUser{}
		a, onAgent := byTTY[r.tty]
		switch {
		case name == "agentctl" || r.pid == self || r.ppid == self: // a hook on an agent's terminal too
		case onAgent:
			key, u = "agent "+a.ID, CPUUser{Kind: "agent", Name: a.Title, Agent: a.ID}
		default:
			key, u = "program "+name, CPUUser{Kind: "program", Name: name}
		}
		g := groups[key]
		if g == nil {
			g = &u
			groups[key] = g
		}
		g.Percent += r.cpu / float64(max(cores, 1))
		g.Processes++
	}
	var out []CPUUser
	for _, g := range groups {
		g.Percent = math.Round(g.Percent*10) / 10
		if g.Percent > 0 || g == deck {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Name < out[j].Name
	})
	for i := range out {
		if i >= n && out[i].Kind == "deck" {
			out[n-1] = out[i]
		}
	}
	return out[:min(n, len(out))]
}
