//go:build !windows

package claude

import (
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// processTTYs asks ps for the terminals of the given processes. ok is false
// when ps didn't answer (it exits with 1, an answer, when none of them runs).
func processTTYs(pids []int) (ttys map[int]string, ok bool) {
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	ttys = map[int]string{}
	out, err := proc.Output(5*time.Second, "/bin/ps", "-o", "pid=,tty=", "-p", strings.Join(list, ","))
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		return ttys, false
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[1] != "??" {
			if pid, err := strconv.Atoi(f[0]); err == nil {
				ttys[pid] = "/dev/" + f[1]
			}
		}
	}
	return ttys, true
}

// LiveSessions lists interactive `claude` processes that have a terminal and
// aren't on one of skipTTYs (agentctl's own tmux panes), each matched to its
// session transcript. Processes whose transcript can't be found are left out.
func LiveSessions(skipTTYs map[string]bool) []Live {
	out, err := proc.Output(5*time.Second, "/bin/ps", "-axww", "-o", "pid=,lstart=,tty=,args=")
	if err != nil {
		return nil
	}
	var procs []Live
	var args = map[int][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 { // pid, 5 lstart fields, tty, argv0
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		tty, argv := f[6], f[7:]
		if tty == "??" || tty == "-" || !isInteractiveClaude(argv) {
			continue
		}
		dev := "/dev/" + tty
		if skipTTYs[dev] {
			continue
		}
		started, _ := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(f[1:6], " "), time.Local)
		procs = append(procs, Live{PID: pid, TTY: dev, Started: started})
		args[pid] = argv
	}
	if len(procs) == 0 {
		return nil
	}
	cwds := processCwds(procs)
	claimed := map[string]bool{}
	// Explicit session ids first, then the oldest processes pick first.
	sort.Slice(procs, func(i, j int) bool { return procs[i].Started.Before(procs[j].Started) })
	var res []Live
	for _, explicitPass := range []bool{true, false} {
		for i := range procs {
			p := &procs[i]
			if p.SessionID != "" {
				continue
			}
			p.Cwd = cwds[p.PID]
			if p.Cwd == "" {
				continue
			}
			id := sessionArg(args[p.PID])
			if explicitPass != (id != "") {
				continue
			}
			if id == "" {
				id = newestTranscript(ProjectDirFor(p.Cwd), p.Started, claimed)
			}
			if id == "" || claimed[id] {
				continue
			}
			claimed[id] = true
			p.SessionID = id
			p.Transcript = filepath.Join(ProjectDirFor(p.Cwd), id+".jsonl")
			res = append(res, *p)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Started.Before(res[j].Started) })
	return res
}

// processCwds asks lsof for the working directories of the given processes.
func processCwds(procs []Live) map[int]string {
	pids := make([]string, len(procs))
	for i, p := range procs {
		pids[i] = strconv.Itoa(p.PID)
	}
	out, _ := proc.Output(10*time.Second, "/usr/sbin/lsof", "-a", "-d", "cwd", "-p", strings.Join(pids, ","), "-Fpn")
	cwds := map[int]string{}
	pid := 0
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			pid, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "n") && pid != 0:
			cwds[pid] = l[1:]
		}
	}
	return cwds
}
