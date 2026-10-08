package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestServerDefaultsKeepSelectionAndPassKeys(t *testing.T) {
	if _, err := exec.LookPath(Bin); err != nil {
		t.Skip("no tmux")
	}
	old := Socket
	Socket = fmt.Sprintf("agentsdeck-test-%d", os.Getpid())
	t.Cleanup(func() { _, _ = run("kill-server"); Socket = old })
	if err := NewSession("t", t.TempDir(), nil, []string{"cat"}); err != nil {
		t.Fatal(err)
	}
	keys, err := run("list-keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"copy-mode MouseDragEnd1Pane send-keys -X copy-pipe-no-clear /usr/bin/pbcopy",
		"copy-mode-vi MouseDragEnd1Pane send-keys -X copy-pipe-no-clear /usr/bin/pbcopy",
		`copy-mode a send-keys -X cancel \; send-keys -H 61`,
		`copy-mode \; send-keys -X cancel \; send-keys -H 3b`,
		`copy-mode-vi Enter send-keys -X cancel \; send-keys Enter`,
		"root C-q detach-client",
	} {
		found := false
		for _, l := range strings.Split(keys, "\n") {
			if strings.Join(strings.Fields(l), " ") == "bind-key -T "+want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing binding: %s", want)
		}
	}
	// Scrolling the history still works in copy mode.
	if !strings.Contains(strings.Join(strings.Fields(keys), " "), "copy-mode WheelUpPane select-pane") {
		t.Error("wheel binding lost")
	}
}
