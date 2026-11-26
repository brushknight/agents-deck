package main

// agentctl doctor: is this computer ready to run agents? Most useful on a
// server (a Linux machine with no desktop session, such as a second machine
// for multi-node): it checks what the deck needs there and says how to fix
// what's missing, one line each.

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/daemon"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/service"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// check is one line of the report. A failed check stops agents from
// running; a warning only means something is missing that some agents need.
type check struct {
	name, detail, fix string
	level             int // checkOK, checkWarn, checkFail
}

const (
	checkOK = iota
	checkWarn
	checkFail
)

// doctorEnv is what the checks look at; tests fill it with made-up answers.
type doctorEnv struct {
	goos      string
	shell     string                  // proc.LoginShell
	lookPath  func(string) string     // daemon.LookPathLogin
	run       func(...string) string  // a command's output, "" on failure
	writable  func(dir string) error  // can the data folder be written
	running   func() (string, bool)   // the service's version, and whether it answers
	portFree  func(addr string) bool  // nothing listens on addr now
	login     service.Registration    // what starts the service at login
	linger    func() (on, known bool) // Linux: systemd keeps the user's services after logout
	lingerFix string                  // how to turn linger on
	cfg       fileConfig
	// separate: AGENTSTERM_HOME points at a data folder of its own (a test
	// instance). The login item is the main deck's, so it is left alone.
	separate bool
}

func doctor(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: agentctl doctor")
	}
	m, _, err := serviceManager()
	if err != nil {
		return err
	}
	env := doctorEnv{
		goos: runtime.GOOS, shell: proc.LoginShell(), lookPath: daemon.LookPathLogin,
		run: func(argv ...string) string {
			out, err := proc.Output(5*time.Second, argv[0], argv[1:]...)
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
		writable: writableDir,
		running: func() (string, bool) {
			s, err := quickState()
			if err != nil {
				return "", false
			}
			return s.Server.Version, true
		},
		portFree: func(addr string) bool {
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return false
			}
			l.Close()
			return true
		},
		login: m.Registration(), cfg: loadConfig(), separate: os.Getenv("AGENTSTERM_HOME") != "",
	}
	if sd, ok := m.(*service.Systemd); ok {
		env.linger, env.lingerFix = sd.Linger, sd.LingerHint()
	}
	checks := doctorChecks(env)
	if failed := printChecks(os.Stdout, checks); failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

func doctorChecks(e doctorEnv) []check {
	var out []check
	add := func(c check) { out = append(out, c) }

	// The shell agents start in.
	if e.shell != "" && e.shell != "sh" {
		add(check{name: "shell", detail: e.shell + " (agents start in it as a login shell)"})
	} else {
		add(check{name: "shell", level: checkFail, detail: "no login shell found",
			fix: "set your account's login shell (chsh), or $SHELL"})
	}

	// tmux runs every agent.
	if p := e.lookPath("tmux"); p != "" {
		v := e.run(p, "-V")
		add(check{name: "tmux", detail: strings.TrimSpace(v+" · server "+tmux.Socket) + " (" + p + ")"})
	} else {
		add(check{name: "tmux", level: checkFail, detail: "not found on your login PATH",
			fix: "install tmux (brew install tmux, apt install tmux, or environment.systemPackages on NixOS)"})
	}

	// The tools agents run, and git for moving work between machines.
	for _, t := range []struct{ name, what, install string }{
		{"claude", "Claude Code", "npm install -g @anthropic-ai/claude-code, then sign in: claude"},
		{"codex", "Codex", "npm install -g @openai/codex, then sign in: codex"},
		{"git", "git", "install git"},
	} {
		if p := e.lookPath(t.name); p != "" {
			add(check{name: t.name, detail: p})
		} else {
			add(check{name: t.name, level: checkWarn, detail: t.what + " isn't on your login PATH", fix: t.install})
		}
	}

	// The data folder.
	if err := e.writable(paths.Data()); err != nil {
		add(check{name: "data", level: checkFail, detail: paths.Data() + ": " + err.Error(),
			fix: "make it writable for your user, or set AGENTSTERM_HOME to a folder that is"})
	} else {
		add(check{name: "data", detail: paths.Data()})
	}

	// The service, now and at login.
	if v, ok := e.running(); ok {
		add(check{name: "service", detail: "running, version " + v})
	} else if e.separate {
		add(check{name: "service", level: checkFail, detail: "not running", fix: "agentctl serve (with this AGENTSTERM_HOME)"})
	} else {
		add(check{name: "service", level: checkFail, detail: "not running", fix: "agentctl install (or agentctl serve in a terminal)"})
	}
	switch {
	case e.separate:
		add(check{name: "at login", detail: "not checked: AGENTSTERM_HOME is a data folder of its own, and the login item belongs to the main deck"})
	case !e.login.Registered:
		add(check{name: "at login", level: checkWarn, detail: "not set up to start at login", fix: "agentctl install"})
	case e.login.Note != "":
		add(check{name: "at login", level: checkWarn, detail: e.login.Kind + ": " + e.login.Note, fix: "agentctl install"})
	default:
		add(check{name: "at login", detail: e.login.Kind + " (" + e.login.Where + ")"})
	}

	// Linux: without linger the service and its agents stop at logout.
	if e.goos == "linux" && e.linger != nil {
		switch on, known := e.linger(); {
		case !known:
			add(check{name: "linger", level: checkWarn, detail: "couldn't ask loginctl", fix: e.lingerFix})
		case on:
			add(check{name: "linger", detail: "on: the service keeps running after you log out"})
		default:
			add(check{name: "linger", level: checkWarn, detail: "off", fix: e.lingerFix})
		}
	}

	// The ports: the service's own while it runs, else they must be free.
	_, up := e.running()
	for _, p := range []struct{ name, addr string }{{"dashboard", e.cfg.LocalAddr}, {"devices", e.cfg.DeviceAddr}} {
		switch {
		case p.addr == "":
		case up:
			add(check{name: "port", detail: p.addr + " (" + p.name + "): the service's"})
		case e.portFree(p.addr):
			add(check{name: "port", detail: p.addr + " (" + p.name + "): free"})
		default:
			add(check{name: "port", level: checkFail, detail: p.addr + " (" + p.name + "): another program listens on it",
				fix: "stop that program, or give this deck other ports (localAddr, deviceAddr in config.json)"})
		}
	}

	// The network listener boards, phones and other machines connect to.
	if e.cfg.DeviceAddr == "" {
		add(check{name: "network", detail: "off (deviceAddr is empty): only this computer can reach the deck"})
	} else {
		add(check{name: "network", detail: "listening on " + e.cfg.DeviceAddr + " for paired devices (TLS 1.3); other machines and boards need it open in the firewall"})
	}
	return out
}

// printChecks writes one line per check, a fix under each that has one, and
// returns how many failed.
func printChecks(w io.Writer, checks []check) (failed int) {
	mark := map[int]string{checkOK: "ok  ", checkWarn: "warn", checkFail: "FAIL"}
	for _, c := range checks {
		fmt.Fprintf(w, "%s  %-9s %s\n", mark[c.level], c.name, c.detail)
		if c.fix != "" && c.level != checkOK {
			fmt.Fprintf(w, "                fix: %s\n", c.fix)
		}
		if c.level == checkFail {
			failed++
		}
	}
	return failed
}

// writableDir: dir exists (or can be made) and a file can be written in it.
func writableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".doctor-")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(filepath.Clean(name))
}
