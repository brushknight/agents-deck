package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
)

func agentT(id, title string, st model.Status) target {
	return agentTarget(model.Agent{ID: id, Title: title, Status: st, Folder: "f"})
}

func sessT(id, title string) target {
	return sessionTarget(claude.SessionInfo{ID: id, AITitle: title, Cwd: "/work/x"})
}

func TestMatch(t *testing.T) {
	cands := []target{
		agentT("k3f9a2", "checkout-api", model.Running),
		agentT("h0m3l4", "infra-network", model.Idle),
		agentT("1nfr4t", "infra-tf", model.Exited),
		agentT("r3l34s", "Release Notes v2", model.Exited),
		agentT("inf0x1", "web", model.Idle), // an id that begins like a name
		sessT("0b5c2f4e-1111-4222-8333-944455556666", "Tidy Meeting Notes"),
		sessT("0b5c9999-1111-4222-8333-944455556666", "Draft the roadmap"),
	}
	cases := []struct {
		ref, want string // want: the id, or "?" ambiguous, or "" no match
	}{
		{"k3f9a2", "k3f9a2"},           // exact id
		{"k3f", "k3f9a2"},              // id prefix
		{"CHECKOUT-API", "k3f9a2"},     // exact name, any case
		{"release notes v2", "r3l34s"}, // exact name with spaces
		{"notes v", "r3l34s"},          // part of a name
		{"NETWORK", "h0m3l4"},          // middle of a name, any case
		{"infra", "?"},                 // two names
		{"inf", "?"},                   // an id prefix and names: never guess
		{"meeting", "0b5c2f4e-1111-4222-8333-944455556666"},
		{"0b5c2f4e", "0b5c2f4e-1111-4222-8333-944455556666"}, // session id prefix
		{"0b5c", "?"}, // two sessions
		{"0b5", ""},   // too short for a session id prefix, and no name holds it
		{"nothing like it", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		got, err := match(cands, c.ref)
		var amb *ambiguous
		switch {
		case c.want == "?":
			if !errors.As(err, &amb) {
				t.Errorf("%q: want ambiguous, got %v %v", c.ref, got.ID, err)
			}
		case c.want == "":
			if !errors.Is(err, errNoMatch) {
				t.Errorf("%q: want no match, got %v %v", c.ref, got.ID, err)
			}
		default:
			if err != nil || got.ID != c.want {
				t.Errorf("%q: got %q %v, want %q", c.ref, got.ID, err, c.want)
			}
		}
	}
	// Several matches: a table with each candidate and how to pick one.
	_, err := match(cands, "infra")
	msg := pickErr(err, "infra", "resume", "agent").Error()
	for _, want := range []string{"h0m3l4", "1nfr4t", "infra-network", "ACTIVE", "agentctl resume "} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguous message lacks %q:\n%s", want, msg)
		}
	}
	_, err = match(cands, "zzz")
	if msg := pickErr(err, "zzz", "rm", "agent").Error(); !strings.Contains(msg, "agentctl sessions") {
		t.Errorf("no-match message: %s", msg)
	}
}

func TestActiveAgo(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5m ago", 3 * time.Hour: "3h ago",
		47 * time.Hour: "47h ago", 72 * time.Hour: "3d ago",
	} {
		if got := activeAgoAt(now.Add(-d), now); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
	if got := activeAgoAt(time.Time{}, now); got != "-" {
		t.Errorf("unknown: %q", got)
	}
}

// What resume and restore do for each state.
func TestActionFor(t *testing.T) {
	lost := agentT("a", "x", model.Exited)
	lost.Agent.Lost = true
	ended := agentT("b", "x", model.Exited)
	ended.Agent.Resumable = true
	shell := agentT("c", "x", model.Exited)
	ext := agentT("d", "x", model.Idle)
	ext.Agent.External = true
	for _, c := range []struct {
		t    target
		want action
	}{
		{lost, actRestore}, {ended, actResume}, {shell, actCant}, {ext, actElsewhere},
		{agentT("e", "x", model.Running), actOpen}, {agentT("f", "x", model.Waiting), actOpen},
		{sessT("0b5c2f4e-1111-4222-8333-944455556666", "s"), actStart},
	} {
		if got := actionFor(c.t); got != c.want {
			t.Errorf("%s (%s): %d, want %d", c.t.ID, c.t.state(), got, c.want)
		}
	}
}

func TestWriteCompletions(t *testing.T) {
	var b strings.Builder
	a := agentT("k3f9a2", "Fix the user's login", model.Idle)
	a.Active = time.Now().Add(-3 * time.Hour)
	writeCompletions(&b, []target{a, agentT("aaaaa1", "twin", model.Idle), agentT("aaaaa2", "twin", model.Idle)})
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines:\n%s", b.String())
	}
	f := strings.Split(lines[0], "\t")
	if f[0] != "Fix the user's login" || !strings.Contains(f[1], "k3f9a2") || !strings.Contains(f[1], "idle · active 3h ago · f") || f[2] != "0" {
		t.Errorf("named agent: %q", f)
	}
	if lines[1] != "k3f9a2\t\t1" {
		t.Errorf("hidden id: %q", lines[1])
	}
	// Two agents share a name: each is offered by its id.
	if !strings.HasPrefix(lines[2], "aaaaa1\t") || !strings.HasPrefix(lines[3], "aaaaa2\t") {
		t.Errorf("shared name: %q %q", lines[2], lines[3])
	}
}

// A request that gets no answer says why: not running, or too slow.
func TestCallErrors(t *testing.T) {
	home, err := os.MkdirTemp("", "ad")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("AGENTSTERM_HOME", home)
	if err := call("GET", "/v1/state", nil, nil); !errors.Is(err, errNotRunning) {
		t.Fatalf("no socket: %v", err)
	}
	// A socket file nobody listens on.
	l, err := net.Listen("unix", paths.Socket())
	if err != nil {
		t.Skip("unix sockets:", err)
	}
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	l.Close()
	if err := call("GET", "/v1/state", nil, nil); !errors.Is(err, errNotRunning) {
		t.Fatalf("nobody listening: %v", err)
	}
	os.Remove(paths.Socket())
	// A service that takes too long.
	l, err = net.Listen("unix", paths.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(2 * time.Second) }))
	err = callTimeout("POST", "/v1/restore", nil, nil, 200*time.Millisecond)
	if err == nil || errors.Is(err, errNotRunning) || !strings.Contains(err.Error(), "didn't answer within") {
		t.Fatalf("slow service: %v", err)
	}
}

// Windows refuses a connection to a socket nobody listens on with
// WSAECONNREFUSED, which syscall.ECONNREFUSED doesn't match there.
func TestCallErrWindowsRefused(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "unix", Err: os.NewSyscallError("connectex", syscall.Errno(wsaeConnRefused))}
	if got := callErr(err, time.Second); !errors.Is(got, errNotRunning) {
		t.Fatalf("got %v", got)
	}
}
