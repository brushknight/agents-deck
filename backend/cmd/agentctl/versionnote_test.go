package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestVersionNote(t *testing.T) {
	const app = "/Applications/agents deck.app"
	cases := []struct {
		name, cli, service, exe, app string
		want                         []string
		not                          string
	}{
		{"older copy beside the app", "0.4.1", "0.5.0", "/home/demo/.local/bin/agentctl", app,
			[]string{"this agentctl is 0.4.1, the service runs 0.5.0", "The app at " + app, "The agentctl command"}, "agentctl install"},
		{"older copy, no app", "0.4.1", "0.5.0", "/home/demo/.local/bin/agentctl", "",
			[]string{"install.sh again"}, "agentctl install"},
		{"newer cli", "0.5.1", "0.5.0", "/home/demo/.local/bin/agentctl", "",
			[]string{"agentctl install restarts the service with this version"}, ""},
		{"inside the app", "0.4.1", "0.5.0", app + "/Contents/MacOS/agentctl", app,
			[]string{"install.sh again"}, "The app at"},
		{"from source", "dev", "0.5.0", "/home/demo/.local/bin/agentctl", "",
			[]string{"build from source"}, "agentctl install"},
	}
	for _, c := range cases {
		got := versionNote(c.cli, c.service, c.exe, c.app)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", c.name, got, w)
			}
		}
		if c.not != "" && strings.Contains(got, c.not) {
			t.Errorf("%s: %q has %q", c.name, got, c.not)
		}
	}
}

func TestNoteVersionOnceAndQuiet(t *testing.T) {
	var buf bytes.Buffer
	oldVersion, oldOut, oldApp := version, versionNoteOut, macApp
	defer func() {
		version, versionNoteOut, macApp, versionQuiet, versionNoted = oldVersion, oldOut, oldApp, false, sync.Once{}
	}()
	version, versionNoteOut, macApp = "0.4.1", &buf, func() string { return "" }

	versionNoted, versionQuiet = sync.Once{}, false
	noteVersion("0.4.1") // same: nothing
	noteVersion("")      // an older service sends no header: nothing
	noteVersion("0.5.0")
	noteVersion("0.5.0")
	if n := strings.Count(buf.String(), "note:"); n != 1 {
		t.Fatalf("want one note, got %d: %q", n, buf.String())
	}
	buf.Reset()
	versionNoted, versionQuiet = sync.Once{}, true // hook, statusline, mcp…
	noteVersion("0.5.0")
	if buf.Len() != 0 {
		t.Fatalf("a quiet command printed: %q", buf.String())
	}
}
