package proc

import (
	"errors"
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
	// Commands: for an agent, what on its terminal uses the most, by
	// program (the build or test it runs, more often than the agent
	// itself), at most TopCommands. Omitted for the others.
	Commands []Command `json:"commands,omitempty"`
}

// Command is one program on an agent's terminal and its share of the CPU.
type Command struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"` // of the whole machine
}

// TopCommands is how many of an agent's programs CPUUser.Commands lists.
const TopCommands = 3

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
	start     string // ps lstart ("Mon Nov 23 10:04:05 2026"), when asked for
}

// parsePS reads `ps -o pid=,ppid=,tty=,%cpu=,comm=`; the program's path is
// last and may hold spaces.
func parsePS(out string) []psRow { return parsePSWith(out, 0) }

// parsePSWith reads ps rows with n more words between %cpu and comm (the
// five of lstart) into start.
func parsePSWith(out string, n int) []psRow {
	var rows []psRow
	for _, l := range strings.Split(out, "\n") {
		f := make([]string, 4+n)
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
		rows = append(rows, psRow{pid, ppid, f[2], cpu, rest, strings.Join(f[4:], " ")})
	}
	return rows
}

// PS is one process, for background mode (cpuguard).
type PS struct {
	PID, PPID int
	TTY       string  // "ttys012", "pts/3"; "??" or "?" for none
	CPU       float64 // percent of one core, as ps has it
	Start     string  // when it started, to tell a reused pid apart
	Comm      string
}

// Processes runs ps once for every process, with its start time. Not on
// Windows.
func Processes() ([]PS, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("not on Windows")
	}
	all := "-ax"
	if runtime.GOOS == "linux" {
		all = "-e"
	}
	out, err := Output(10*time.Second, "/bin/ps", all, "-o", "pid=,ppid=,tty=,%cpu=,lstart=,comm=")
	if err != nil {
		return nil, err
	}
	return parseProcesses(string(out)), nil
}

func parseProcesses(out string) []PS {
	var list []PS
	for _, r := range parsePSWith(out, 5) {
		list = append(list, PS{PID: r.pid, PPID: r.ppid, TTY: r.tty, CPU: r.cpu, Start: r.start, Comm: r.comm})
	}
	return list
}

// IsDeck says whether a process is the deck's own: this service (self), a
// helper it runs, or any agentctl (a hook on an agent's terminal too).
func IsDeck(pid, ppid int, comm string, self int) bool {
	return pid == self || ppid == self || programName(comm) == "agentctl"
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
	commands := map[string]map[string]float64{} // by group: each program's share
	for _, r := range rows {
		name := programName(r.comm)
		key, u := "deck", CPUUser{}
		a, onAgent := byTTY[r.tty]
		switch {
		case IsDeck(r.pid, r.ppid, r.comm, self):
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
		share := r.cpu / float64(max(cores, 1))
		g.Percent += share
		g.Processes++
		if g.Kind == "agent" {
			if commands[key] == nil {
				commands[key] = map[string]float64{}
			}
			commands[key][name] += share
		}
	}
	var out []CPUUser
	for key, g := range groups {
		g.Percent = math.Round(g.Percent*10) / 10
		g.Commands = topCommands(commands[key])
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

// topCommands is the TopCommands biggest programs, in percent of the whole
// machine; nil when none uses any.
func topCommands(by map[string]float64) []Command {
	var out []Command
	for name, p := range by {
		if p = math.Round(p*10) / 10; p > 0 {
			out = append(out, Command{name, p})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Name < out[j].Name
	})
	return out[:min(TopCommands, len(out))]
}
