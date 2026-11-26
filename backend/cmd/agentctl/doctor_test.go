package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/node"
	"github.com/brushknight/agents-deck/backend/internal/service"
)

// A NixOS-like server: bash from the system profile, no zsh anywhere.
func serverEnv() doctorEnv {
	tools := map[string]string{
		"tmux":   "/run/current-system/sw/bin/tmux",
		"claude": "/home/demo/.npm-global/bin/claude",
		"git":    "/run/current-system/sw/bin/git",
	}
	return doctorEnv{
		goos: "linux", shell: "/run/current-system/sw/bin/bash",
		lookPath:  func(n string) string { return tools[n] },
		run:       func(argv ...string) string { return "tmux 3.5a" },
		writable:  func(string) error { return nil },
		running:   func() (string, bool) { return "0.5.0", true },
		login:     service.Registration{Kind: "systemd", Registered: true, Where: "/home/demo/.config/systemd/user/agents-deck.service"},
		linger:    func() (bool, bool) { return false, true },
		lingerFix: "loginctl enable-linger demo",
		portFree:  func(string) bool { return true },
		cfg:       fileConfig{LocalAddr: "127.0.0.1:7340", DeviceAddr: ":7341"},
	}
}

func TestDoctorServer(t *testing.T) {
	var out bytes.Buffer
	failed := printChecks(&out, doctorChecks(serverEnv()))
	s := out.String()
	if failed != 0 {
		t.Fatalf("nothing should fail:\n%s", s)
	}
	for _, want := range []string{
		"ok    shell     /run/current-system/sw/bin/bash",
		"ok    tmux      tmux 3.5a · server agentsterm",
		"warn  codex",         // not installed: a warning, not a failure
		"warn  linger    off", // a server needs it
		"fix: loginctl enable-linger demo",
		"ok    network   listening on :7341",
		"ok    port      :7341 (devices): the service's",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
}

func TestDoctorFailures(t *testing.T) {
	e := serverEnv()
	e.lookPath = func(string) string { return "" } // no tmux, no tools
	e.running = func() (string, bool) { return "", false }
	e.portFree = func(a string) bool { return a != ":7341" } // something else holds the device port
	e.linger = func() (bool, bool) { return true, true }
	e.goos = "darwin"
	var out bytes.Buffer
	failed := printChecks(&out, doctorChecks(e))
	s := out.String()
	if failed != 3 || !strings.Contains(s, "FAIL  tmux") || !strings.Contains(s, "FAIL  service") || !strings.Contains(s, "FAIL  port      :7341 (devices): another program") {
		t.Fatalf("want tmux, service and the device port failed, got %d:\n%s", failed, s)
	}
	if !strings.Contains(s, "ok    port      127.0.0.1:7340 (dashboard): free") {
		t.Errorf("free port:\n%s", s)
	}
	if strings.Contains(s, "linger") {
		t.Errorf("linger is Linux only:\n%s", s)
	}
}

// A test instance (AGENTSTERM_HOME of its own) never gets told to run
// agentctl install: the login item is the main deck's.
func TestDoctorSeparateInstance(t *testing.T) {
	e := serverEnv()
	e.separate = true
	e.running = func() (string, bool) { return "", false }
	var out bytes.Buffer
	printChecks(&out, doctorChecks(e))
	if s := out.String(); strings.Contains(s, "agentctl install") || !strings.Contains(s, "agentctl serve (with this AGENTSTERM_HOME)") {
		t.Fatalf("a test instance was told to install:\n%s", s)
	}
}

// A one-way link is named, with the machine whose port to open.
func TestDoctorOneWayMachine(t *testing.T) {
	e := serverEnv()
	e.machines = func() []node.State {
		return []node.State{{Name: "attic", Reachable: true, RTTMs: 38}, {Name: "garage", OneWay: true}}
	}
	var out bytes.Buffer
	printChecks(&out, doctorChecks(e))
	s := out.String()
	if !strings.Contains(s, "ok    machine   attic: online · 38 ms") || !strings.Contains(s, "warn  machine   garage reaches this machine, this machine can't reach it") || !strings.Contains(s, "open port 7341 on garage") {
		t.Fatalf("%s", s)
	}
}
