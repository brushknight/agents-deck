package main

// When this agentctl and the service it talks to are different versions
// (a copy in ~/.local/bin left behind by an update of the app, or the other
// way round), the first answer from the service says so: the service puts
// its version on every answer over the socket (Agents-Deck-Version), and
// the command prints one note on stderr saying which side to update.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/brushknight/agents-deck/backend/internal/update"
)

// quietVersion: commands that other programs run (hooks, the status line,
// the MCP bridge, completion) and the ones that show both versions anyway.
var quietVersion = map[string]bool{
	"hook": true, "statusline": true, "mcp": true, "__complete": true, "completion": true,
	"open-link": true, "sim-agent": true, "serve": true, "status": true, "version": true, "--version": true,
}

var (
	versionNoted   sync.Once
	versionQuiet   bool      // set by main for quietVersion commands
	versionNoteOut io.Writer = os.Stderr
)

// noteVersion is called with the service's Agents-Deck-Version header.
func noteVersion(service string) {
	if versionQuiet || service == "" || service == version {
		return
	}
	versionNoted.Do(func() {
		exe, _ := os.Executable()
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		fmt.Fprintln(versionNoteOut, versionNote(version, service, exe, macApp()))
	})
}

// macApp: the agents deck app on this Mac, "" elsewhere or when there is none.
var macApp = func() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	return update.InstalledMacApp()
}

// versionNote says what to do when this agentctl (cli, at exe) and the
// service run different versions. app is the agents deck app on this Mac, if any.
func versionNote(cli, service, exe, app string) string {
	msg := fmt.Sprintf("note: this agentctl is %s, the service runs %s.", cli, service)
	inApp := strings.Contains(exe, ".app/Contents/MacOS/")
	switch {
	case update.IsDevBuild(cli) || update.IsDevBuild(service):
		return msg + " One of them is a build from source."
	case update.Newer(service, cli) && app != "" && !inApp:
		return msg + " The app at " + app + " is newer than this copy (" + exe + "): delete it and turn on " +
			"Settings › General › The agentctl command, so the command is always the app's."
	case update.Newer(service, cli):
		return msg + " Update this agentctl: run install.sh again (or install it the way you did before)."
	default:
		return msg + " agentctl install restarts the service with this version."
	}
}
