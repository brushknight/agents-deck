package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestParseState(t *testing.T) {
	out := "s a-one 1800000000\ns a two 0\nc a-one /dev/ttys001 1700000001 attached\n" +
		"c a-one /dev/ttys002 1700000009 attached,focused\np /dev/ttys101\np /dev/ttys102\nnoise\n"
	st := parseState(out, true)
	if !st.Running || len(st.Activity) != 2 || st.Activity["a-one"].Unix() != 1800000000 || !st.Activity["a two"].IsZero() {
		t.Fatalf("sessions: %+v", st.Activity)
	}
	if len(st.Clients) != 2 || st.Clients[0].TTY != "/dev/ttys002" || !st.Clients[0].Focused {
		t.Fatalf("clients, most recent first: %+v", st.Clients)
	}
	if len(st.PaneTTYs) != 2 || !st.PaneTTYs["/dev/ttys101"] {
		t.Fatalf("panes: %v", st.PaneTTYs)
	}
	if st := parseState("", false); st.Running || st.Activity == nil || st.PaneTTYs == nil {
		t.Fatalf("no server: %+v", st)
	}
}

// Look gives what the three separate lists give, in one call.
func TestLookMatchesTheLists(t *testing.T) {
	if _, err := exec.LookPath(Bin); err != nil {
		t.Skip("no tmux")
	}
	old := Socket
	Socket = fmt.Sprintf("agentsdeck-test-l%d", os.Getpid())
	t.Cleanup(func() { _, _ = run("kill-server"); Socket = old })
	if st := Look(); st.Running || len(st.Activity) != 0 {
		t.Fatalf("no server yet: %+v", st)
	}
	for _, name := range []string{"one", "two"} {
		if err := NewSession(name, t.TempDir(), nil, []string{"cat"}); err != nil {
			t.Fatal(err)
		}
	}
	st := Look()
	act, panes := SessionActivity(), PaneTTYs()
	if !st.Running || len(st.Activity) != 2 || !st.Activity["one"].Equal(act["one"]) || len(panes) != 2 {
		t.Fatalf("look %+v, lists %v %v", st, act, panes)
	}
	for p := range panes {
		if !st.PaneTTYs[p] {
			t.Fatalf("pane %s missing: %v", p, st.PaneTTYs)
		}
	}
	if len(st.Clients) != 0 || len(ClientList()) != 0 {
		t.Fatalf("no clients: %+v", st.Clients)
	}
}
