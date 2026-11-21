// agentctl runs and controls the agents deck service.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/daemon"
	"github.com/brushknight/agents-deck/backend/internal/demo"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/service"
	"github.com/brushknight/agents-deck/backend/internal/sim"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
	"github.com/brushknight/agents-deck/backend/internal/web"
)

// version is stamped by release builds: -ldflags "-X main.version=<tag>".
var version = "dev"

const usage = `agentctl: runs and controls agents deck

  agentctl new [dir] [-t title] [-c claude|codex|gemini|shell] [-a account] [-d]
                                 start an agent (dir defaults to .) and attach (-d: don't)
  agentctl hats                  the hats agents wear (one per working folder)
  agentctl accounts              the Claude accounts agents run in (account add|default|rm)
  agentctl handoff <agent> <account>
                                 continue a Claude session in another account (out of usage)
  agentctl set hat <agent|folder> <shape> [colour] | auto
  agentctl live                  Claude sessions started by hand in other terminals
  agentctl add <session>         put one of them on the deck (watched)
  agentctl reopen                iTerm tabs for every agent no terminal shows (after a crash)
  agentctl resume <agent or session>... [-d] [--force]
                                 bring an agent back and open it: lost with the tmux server
                                 (restored), ended (resumed), a Claude session not on the deck
                                 (started), or still running (just opened). -d: don't open it;
                                 with several names, none is opened
  agentctl restore <agent>...    the same as resume
  agentctl restore [--all|-y]    bring back everything lost with the tmux server (crash, reboot);
                                 lists it first, asks before more than 5
  agentctl attach <agent>        show an agent in this terminal (detach: ctrl-q); one that isn't
                                 running is brought back first (it asks)
  agentctl ls [--sort active]    list agents (ACTIVE: its latest message sent or received);
                                 --sort active: most recently active first
  agentctl rm <agent>            stop and forget an agent
  agentctl move <agent> <n>      put an agent at position n (1 = first tile; free spots ok)
  agentctl sessions [words] [-n N]
                                 look up Claude sessions (any, not just agentctl's) by id/title/folder/prompt

  <agent> is an id, an id's start, a name (any case), or a part of a name that only one
  agent has; resume also takes Claude sessions by id or name. When several match, agentctl
  lists them and asks for the id. Tab completes names and ids (see completion zsh).

  agentctl sim start --root DIR --slots N [--speed X] [--patience S]
                                 simulated Claudes working in real projects under DIR (for demos)
  agentctl sim stop              remove all simulated agents
  agentctl web                   open the dashboard in the browser
  agentctl pair <name> [--scope view|act|admin] [--bundle FILE]
                                 pair a device: a one-time code it trades for its own certificate,
                                 or (--bundle) a key and certificate to copy to it over SSH
  agentctl mcp token create <name> [--scope view|act|admin] | token list | token revoke <name>
                                 MCP tokens: let an agent (Claude Code) run the deck's agents
  agentctl mcp [--token T]       the MCP server over stdio, for an MCP client (token from
                                 AGENTSDECK_MCP_TOKEN); see docs/mcp.md
  agentctl devices [revoke|approve|remove <name> | scope <name> <scope>]
                                 paired devices and serial boards; cut one off, approve a board
  agentctl link [ports | add <port> [--baud N] | rm <port>]
                                 Deck Link boards: status (USB or Wi-Fi), find ports, register one
  agentctl link wifi <board> --ssid NAME [--password PW] | --forget
                                 give a board on USB its Wi-Fi network (asks for the password; never
                                 stored on this computer); from then on it connects over Wi-Fi when not on USB
  agentctl link pair <board>     a new Wi-Fi certificate for a board (it pairs again over USB)
  agentctl link update <board> --image FILE --board B --version V
                                 put an app image from this computer on a board on Wi-Fi (it goes back
                                 to its old firmware if the new one doesn't reach the deck)
  agentctl link sim [--id ID] [--fields tile,card,prompt,numbers] [--json]
                                 be a board without hardware (type commands: answer, focus, …)
  agentctl link check <port> [--baud N] [--seconds N]
                                 test a board's firmware against a demo deck (stop the daemon's use: link rm)
  agentctl flash <port> <image.bin> [--offset 0x0] [--probe]
                                 write a firmware image to an ESP32-S3 board over USB
  agentctl flash --board <board> [--version v] [--local] <port>
                                 the service downloads, checks and writes the board's firmware
                                 (--local: the build made on this computer)
  agentctl ports [register|identify|forget <port>]
                                 USB ports and what is on them (listed without opening them)
  agentctl devices rename <name> <new name> | update <name> [--local]
                                 a board's name in Settings; update its firmware (the newest, a
                                 release's or one built on this computer)
  agentctl devices reset [--yes]  start the deck's certificates over (a lost CA key): network
                                 devices pair again, boards get new ones on USB
  agentctl devices uplink <name> auto|wifi|ble
                                 how a board reaches this computer when it can do several
  agentctl bluetooth [scan | ls | add <name>]
                                 boards over Bluetooth: find one nearby, add it with the code on its screen
  agentctl system                what the service is doing (boards, new ports, updates)
  agentctl settings [<key> <value>]
                                 start-at-login, notifications, sound, restore, updates, network
  agentctl diagnostics           a report to send when something's wrong (no names or folders)
  agentctl update [--check]      look for a new release, and install it
  agentctl firmware ls           the boards and firmware in the latest release, and builds made here
  agentctl firmware add <folder or files>
                                 keep firmware built on this computer (the release script's
                                 agents-deck-<board>-<version>.bin, .app.bin, .manifest.json): a board
                                 that runs an older version installs it with one click
  agentctl firmware rm <board> [<version>]
                                 remove builds made on this computer
  agentctl serve [--demo [--fixture FILE]] [--log FILE]
                                 run the service (normally started at login, see install)
  agentctl install               start the service at login (launchd, systemd, Windows startup) and now
  agentctl uninstall             stop the service and no longer start it at login
  agentctl status [--json]       is the service running, does it start at login, where are its files
  agentctl set term iterm|tmux|window
  agentctl set mouse tmux|native
                                 how focus shows an agent (default iterm); applies immediately
  agentctl get [key]             show settings
  agentctl completion zsh        tab completion (add to ~/.zshrc: source <(agentctl completion zsh))
  agentctl hook                  (internal) Claude Code hook entry point
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if err := platformSupports(cmd); err != nil {
		fmt.Fprintln(os.Stderr, "agentctl:", err)
		os.Exit(1)
	}
	var err error
	switch cmd {
	case "hook":
		hook() // never fails, never prints
		return
	case "serve":
		err = serve(args)
	case "new":
		err = newAgent(args)
	case "attach", "a":
		err = attach(args)
	case "reopen":
		err = reopen()
	case "restore":
		err = bringBack("restore", args)
	case "accounts", "account":
		err = accountsCmd(args)
	case "handoff":
		err = handoff(args)
	case "hats":
		err = hatsCmd()
	case "live":
		err = liveCmd()
	case "add":
		err = addCmd(args)
	case "ls", "list":
		err = list(args)
	case "rm":
		err = remove(args)
	case "move", "mv":
		err = move(args)
	case "resume", "continue":
		err = bringBack("resume", args)
	case "sessions", "s":
		err = sessionsCmd(args)
	case "sim":
		err = simCmd(args)
	case "completion":
		err = completionCmd(args)
	case "set":
		err = setCmd(args)
	case "get":
		err = getCmd(args)
	case "__complete":
		completeCmd(args) // silent: used by shell completion
		return
	case "open-link":
		openLink(args) // silent: run by a click in an agent terminal
		return
	case "sim-agent":
		err = simAgent(args)
	case "web":
		err = openWeb(args)
	case "pair":
		err = pair(args)
	case "mcp":
		err = mcpCmd(args)
	case "devices":
		err = devicesCmd(args)
	case "bluetooth", "ble":
		err = bluetoothCmd(args) // bluetooth.go
	case "link":
		err = linkCmd(args)
	case "flash":
		err = flashCmd(args)
	case "install":
		err = install()
	case "uninstall":
		err = uninstall()
	case "status":
		err = status(args)
	case "release-key":
		err = releaseKeyCmd(args)
	case "update":
		err = updateCmd(args)
	case "firmware":
		err = firmwareCmd(args)
	case "system":
		err = systemCmd(args)
	case "settings":
		err = settingsCmd(args)
	case "ports":
		err = portsCmd(args)
	case "diagnostics":
		err = diagnosticsCmd(args)
	case "version", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentctl:", err)
		os.Exit(1)
	}
}

// ---- hook -------------------------------------------------------------------

// hook forwards a Claude Code hook payload to the daemon. It must stay fast,
// print nothing (stdout from some hooks is fed back to Claude) and always exit 0.
func hook() {
	id := os.Getenv("AGENTSTERM_ID")
	if id == "" {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	c := unixClient(2 * time.Second)
	resp, err := c.Post("http://agentd/hook?agent="+id, "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}

// ---- client helpers ------------------------------------------------------------

func unixClient(timeout time.Duration) *http.Client {
	// An explicit AGENTSTERM_HOME wins over the socket inherited from an agent shell.
	sock := os.Getenv("AGENTSTERM_SOCK")
	if sock == "" || os.Getenv("AGENTSTERM_HOME") != "" {
		sock = paths.Socket()
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
}

func call(method, path string, in, out any) error {
	return callTimeout(method, path, in, out, 10*time.Second)
}

// callTimeout is call for requests that may take longer than usual.
func callTimeout(method, path string, in, out any, timeout time.Duration) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://agentd"+path, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := unixClient(timeout).Do(req)
	if err != nil {
		return callErr(err, timeout)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return errors.New(e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// errUnreachable: the service's socket is there but the request failed on
// the way (it closed the connection, e.g. while restarting).
var errUnreachable = errors.New("couldn't reach the service")

// callErr says why a request to the service didn't get an answer: it isn't
// running (no socket, or nobody listening), it took longer than timeout (it
// may still be doing it), or something else broke on the way.
// wsaeConnRefused is WSAECONNREFUSED: on Windows, nothing listens on the
// socket (syscall.ECONNREFUSED there is a made-up number that never matches).
const wsaeConnRefused = 10061

func callErr(err error, timeout time.Duration) error {
	var ne net.Error
	var errno syscall.Errno
	switch {
	case errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) || errors.Is(err, os.ErrNotExist),
		errors.As(err, &errno) && errno == wsaeConnRefused:
		return errNotRunning
	case errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("the service didn't answer within %s; it may still be doing it: check with agentctl ls", timeout.Round(time.Second))
	}
	return fmt.Errorf("%w: %v", errUnreachable, err)
}

func state() (model.State, error) {
	var s model.State
	return s, call("GET", "/v1/state", nil, &s)
}

// ---- commands ------------------------------------------------------------------

func newAgent(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	title := fs.String("t", "", "title (default: folder name)")
	tool := fs.String("c", "claude", "tool: claude, codex, gemini, shell")
	account := fs.String("a", "", "Claude account (default: the default account)")
	detached := fs.Bool("d", false, "don't attach")
	_ = fs.Parse(reorder(args, map[string]bool{"-t": true, "-c": true, "-a": true}))
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var res struct{ ID string }
	if err := call("POST", "/local/agents", map[string]string{"dir": abs, "title": *title, "tool": *tool, "account": *account}, &res); err != nil {
		return err
	}
	if *detached {
		fmt.Println(res.ID)
		return nil
	}
	return attach([]string{res.ID})
}

// reorder lets flags follow the positional dir (`agentctl new . -t x`).
func reorder(args []string, takesValue map[string]bool) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if takesValue[args[i]] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	return append(flags, pos...)
}

func attach(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentctl attach <agent>   (an id, or its name or part of it)")
	}
	a, err := resolve(args[0], "attach")
	if err != nil {
		return err
	}
	if a.Lost || a.Status == model.Exited {
		// Not running: offer to bring it back (it starts a process, so ask).
		t := agentTarget(a)
		if !stdinIsTerminal() {
			return fmt.Errorf("%s is %s: bring it back with agentctl resume %s", listName(a.Title), t.stateLong(), a.ID)
		}
		if !confirm(fmt.Sprintf("%s is %s. Bring it back and open it? [Y/n] ", listName(a.Title), t.stateLong()), true) {
			return nil
		}
		return bringBackTargets([]target{t}, bringOpts{})
	}
	return attachTmux(a)
}

// attachTmux shows a running agent in this terminal.
func attachTmux(a model.Agent) error {
	if a.External {
		app := a.App
		if app == "" {
			app = "the " + a.Tool + " app"
		}
		return fmt.Errorf("%s runs in %s: open it there (agentctl can't attach to it)", listName(a.Title), app)
	}
	argv := tmux.AttachArgv("a-" + a.ID)
	if os.Getenv("TMUX") != "" {
		// Inside tmux already: open in a new window of the outer tmux instead of nesting.
		return exec.Command("tmux", "new-window", "-n", a.Title, strings.Join(argv, " ")).Run()
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

func hatName(s string) string {
	return strings.ToLower(strings.NewReplacer("-", " ", "_", " ").Replace(s))
}

// setHat: agentctl set hat <agent|folder> <shape> [colour] | auto
func setHat(args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return fmt.Errorf("usage: agentctl set hat <agent|folder> <shape> [colour]   or   agentctl set hat <agent|folder> auto\nshapes: %s, none\ncolours: %s",
			strings.Join(hats.Shapes, ", "), strings.Join(hats.Colors, ", "))
	}
	body := map[string]any{}
	if args[1] == "auto" {
		body["auto"] = true
	} else {
		shape, color := hatName(args[1]), ""
		if len(args) == 3 {
			color = strings.ToLower(args[2])
		}
		if err := hats.Validate(shape, color); err != nil {
			return err
		}
		body["shape"], body["color"] = shape, color
	}
	target := args[0]
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		abs, _ := filepath.Abs(target)
		body["folder"] = abs
		if err := call("POST", "/v1/hats", body, nil); err != nil {
			return err
		}
		fmt.Printf("%s → %s\n", tildePath(abs), hatLabel(body))
		return nil
	}
	a, err := resolve(target, "set hat")
	if err != nil {
		return fmt.Errorf("%v (give an agent id or title, or a folder)", err)
	}
	if err := call("POST", "/v1/agents/"+a.ID+"/hat", body, nil); err != nil {
		return err
	}
	fmt.Printf("%s (%s) → %s\n", a.Title, tildePath(a.Cwd), hatLabel(body))
	return nil
}

func hatLabel(body map[string]any) string {
	if body["auto"] == true {
		return "automatic hat"
	}
	l := fmt.Sprint(body["shape"])
	if c, _ := body["color"].(string); c != "" {
		l += " · " + c
	}
	return l
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// hatsCmd lists the hats, the colours, and who wears what.
func hatsCmd() error {
	var res struct {
		Shapes, Colors []string
		Overrides      map[string]hats.Override
	}
	if err := call("GET", "/v1/hats", nil, &res); err != nil {
		return err
	}
	s, err := state()
	if err != nil {
		return err
	}
	fmt.Printf("shapes:  %s, none\ncolours: %s\n\n", strings.Join(res.Shapes, ", "), strings.Join(res.Colors, ", "))
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tHAT\tFOLDER")
	for _, a := range s.Agents {
		h := "none"
		if a.Hat != nil {
			h = a.Hat.Shape + " · " + a.Hat.Color
			if a.Hat.Auto {
				h += " (auto)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.ID, a.Title, h, tildePath(a.Cwd))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(res.Overrides) > 0 {
		fmt.Println("\nchosen by hand:")
		folders := make([]string, 0, len(res.Overrides))
		for f := range res.Overrides {
			folders = append(folders, f)
		}
		sort.Strings(folders)
		for _, f := range folders {
			o := res.Overrides[f]
			fmt.Printf("  %s → %s\n", tildePath(f), strings.TrimSuffix(o.Shape+" · "+o.Color, " · "))
		}
	}
	fmt.Println("\nchange one: agentctl set hat <agent|folder> <shape> [colour]  ·  back to automatic: agentctl set hat <agent|folder> auto")
	return nil
}

// liveCmd lists Claude sessions started by hand in other terminals.
func liveCmd() error {
	var res struct {
		Sessions []struct {
			SessionID, Title, Folder string
			OnBoard, Background      bool
		}
	}
	if err := call("GET", "/v1/live", nil, &res); err != nil {
		return err
	}
	if len(res.Sessions) == 0 {
		fmt.Println("no other Claude sessions running in your terminals")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tTITLE\tFOLDER\tKIND\tDECK")
	for _, s := range res.Sessions {
		deck := "-"
		if s.OnBoard {
			deck = "on the deck"
		}
		kind := "terminal"
		if s.Background {
			kind = "background"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.SessionID[:8], s.Title, s.Folder, kind, deck)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Println("\nput one on the deck: agentctl add <session>")
	return nil
}

func addCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentctl add <session id or prefix>   (see agentctl live)")
	}
	if err := call("POST", "/v1/live/"+url.PathEscape(args[0])+"/add", nil, nil); err != nil {
		return err
	}
	fmt.Println("on the deck · watched: status, context and cost; answer it in its own terminal")
	return nil
}

// reopen opens an iTerm tab for every live agent that no terminal shows, e.g.
// after iTerm quit or crashed (the agents kept running in tmux).
func reopen() error {
	s, err := state()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	var script strings.Builder
	script.WriteString("tell application \"iTerm2\"\n  activate\n  if (count of windows) = 0 then create window with default profile\n  tell current window\n")
	n := 0
	for _, a := range s.Agents {
		if a.Attached || a.External || a.Status == model.Exited {
			continue
		}
		cmd := fmt.Sprintf("'%s' attach %s", strings.ReplaceAll(self, "'", ""), a.ID)
		fmt.Fprintf(&script, "    set t to (create tab with default profile command \"%s\")\n", esc(cmd))
		fmt.Fprintf(&script, "    set name of current session of t to \"%s\"\n", esc(a.Title))
		fmt.Printf("%s  %s\n", a.ID, a.Title)
		n++
	}
	if n == 0 {
		fmt.Println("every agent already has a terminal")
		return nil
	}
	script.WriteString("  end tell\nend tell\n")
	if out, err := exec.Command("osascript", "-e", script.String()).CombinedOutput(); err != nil {
		return fmt.Errorf("iTerm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Printf("opened %d tab(s) in iTerm\n", n)
	return nil
}

func list(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	sortBy := fs.String("sort", "position", "order: position (as on the deck) or active (most recently active first)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("agentctl ls takes no names: %q (try agentctl ls --sort active)", fs.Arg(0))
	}
	s, err := state()
	if err != nil {
		return err
	}
	switch *sortBy {
	case "position", "slot":
	case "active":
		sort.SliceStable(s.Agents, func(i, j int) bool { return s.Agents[i].LastActive.After(s.Agents[j].LastActive) })
	default:
		return fmt.Errorf("--sort %s: use position or active", *sortBy)
	}
	if len(s.Agents) == 0 {
		fmt.Println("no agents — start one with `agentctl new`")
		return nil
	}
	// One row per agent, like `docker ps`: the id, the name and the status
	// first, always in the same columns; what a waiting agent asks goes last
	// so it never pushes the other columns around.
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
	acctCol, waitCol := false, false
	for _, a := range s.Agents {
		acctCol = acctCol || a.Account != nil
		waitCol = waitCol || a.Waiting != nil
	}
	head := []string{"ID", "NAME", "STATUS", "ACTIVE", "CONTEXT", "COST", "FOLDER"}
	if acctCol {
		head = append(head, "ACCOUNT")
	}
	if waitCol {
		head = append(head, "WAITING ON")
	}
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	lost := 0
	for _, a := range s.Agents {
		ctx := "-"
		if a.Context.Window > 0 {
			ctx = fmt.Sprintf("%d%%", a.Context.Used*100/a.Context.Window)
		}
		st := string(a.Status)
		if a.Lost {
			st = "lost"
			lost++
		} else if !a.StatusSince.IsZero() && a.Status != model.Exited {
			st += " " + humanAgo(time.Since(a.StatusSince))
		}
		cost := fmt.Sprintf("$%.2f", a.CostUSD)
		if a.External {
			cost = "—" // Codex reports no prices
		}
		row := []string{a.ID, clip(listName(a.Title), 40), st, activeAgo(a.LastActive), ctx, cost, clip(a.Folder, 24)}
		if acctCol {
			acct := ""
			if a.Account != nil {
				acct = a.Account.Name
			}
			row = append(row, acct)
		}
		if waitCol {
			w := ""
			if a.Waiting != nil {
				w = clip(oneLine(a.Waiting.Title), 60)
			}
			row = append(row, w)
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if lost > 0 {
		fmt.Printf("\n%d agent(s) were lost with the tmux server · `agentctl restore` brings them back\n", lost)
	}
	return nil
}

// liveWindow: a transcript written this recently probably belongs to a Claude
// that is still open somewhere; resuming it would run two Claudes on one session.
const liveWindow = 2 * time.Minute

func sessionsCmd(args []string) error {
	fs := flag.NewFlagSet("sessions", flag.ExitOnError)
	n := fs.Int("n", 20, "how many to show (0 = all)")
	_ = fs.Parse(reorder(args, map[string]bool{"-n": true}))
	all, account, err := allSessions()
	if err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	board := boardSessions()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	acctCol := len(account) > 0
	if acctCol {
		fmt.Fprint(tw, "ACCOUNT\t")
	}
	fmt.Fprintln(tw, "ID\tWHEN\tTITLE\tFOLDER\tBRANCH\tLAST PROMPT")
	shown := 0
	for _, s := range all {
		if query != "" && !s.Match(query) {
			continue
		}
		if *n > 0 && shown == *n {
			break
		}
		when := ago(s.Modified)
		if a, ok := board[s.ID]; ok && a.Status != model.Exited {
			when = "● agent " + a.ID
		} else if time.Since(s.Modified) < liveWindow {
			when = "● open?"
		}
		if acctCol {
			fmt.Fprintf(tw, "%s\t", account[s.ID])
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.ID[:8], when, clip(s.Title(), 32),
			clip(shortHome(s.Cwd), 28), clip(s.Branch, 16), clip(s.LastPrompt, 48))
		shown++
	}
	if shown == 0 {
		if query != "" {
			fmt.Printf("no sessions match %q\n", query)
		} else {
			fmt.Println("no Claude sessions found in", accountDirs())
		}
		return nil
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "\ncontinue one with: agentctl resume <id or name>")
	return nil
}

// boardSessions maps Claude session ids to agents on the board (best effort).
func boardSessions() map[string]model.Agent {
	m := map[string]model.Agent{}
	var res []struct {
		SessionID string      `json:"sessionId"`
		Agent     model.Agent `json:"agent"`
	}
	if call("GET", "/local/agents", nil, &res) == nil {
		for _, r := range res {
			if r.SessionID != "" {
				m[r.SessionID] = r.Agent
			}
		}
	}
	return m
}

func shortHome(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// listName is a title for one table cell: the name as it was set (with
// /rename it can hold any punctuation, quotes too), only kept on one line.
func listName(s string) string {
	s = oneLine(s)
	if s == "" {
		return "-"
	}
	return s
}

// oneLine joins a text's lines and runs of spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func move(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: agentctl move <id|title> <position>   (1 = first tile; free spots are fine)")
	}
	a, err := resolve(args[0], "move")
	if err != nil {
		return err
	}
	var pos int
	if _, err := fmt.Sscan(args[1], &pos); err != nil || pos < 1 || pos > store.MaxSlot+1 {
		return fmt.Errorf("position must be a number from 1 to %d", store.MaxSlot+1)
	}
	// Any position works, free or not; an agent already there swaps places.
	if err := call("POST", "/v1/agents/"+a.ID+"/move", map[string]int{"slot": pos - 1}, nil); err != nil {
		return err
	}
	fmt.Printf("%s → position %d\n", a.Title, pos)
	return nil
}

func remove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agentctl rm <id|title>")
	}
	a, err := resolve(args[0], "rm")
	if err != nil {
		return err
	}
	return call("POST", "/v1/agents/"+a.ID+"/dismiss", nil, nil)
}

func openWeb(args []string) error {
	cfg := loadConfig()
	var res struct{ Code string }
	if err := call("POST", "/local/login-code", nil, &res); err != nil {
		return err
	}
	url := "http://" + cfg.LocalAddr + "/login?code=" + res.Code
	if err := openBrowser(url); err != nil {
		// No browser opener (e.g. headless Linux): print the link instead.
		fmt.Println("open this in your browser:", url)
	}
	return nil
}

// openBrowser opens url in the default browser (see proc.Open). The
// one-time login code expires quickly, so callers should mint it
// immediately before calling this.
func openBrowser(url string) error { return proc.Open(url) }

func pair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	scope := fs.String("scope", "act", "what the device may do: view, act or admin")
	bundle := fs.String("bundle", "", "write a key and certificate for the device to `FILE` (- for stdout) instead of a pairing code")
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return errors.New("usage: agentctl pair <name> [--scope view|act|admin] [--bundle FILE]   e.g. agentctl pair desk-panel --bundle panel.json")
	}
	body := map[string]string{"name": name, "scope": *scope}
	if *bundle != "" {
		var b server.Bundle
		if err := call("POST", "/local/devices/provision", body, &b); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(b, "", "  ")
		if *bundle == "-" {
			fmt.Println(string(out))
			return nil
		}
		if err := os.WriteFile(*bundle, append(out, '\n'), 0o600); err != nil {
			return err
		}
		fmt.Printf("paired %s (%s). Copy %s to the device, then delete it here: it holds the device's private key.\n", name, *scope, *bundle)
		return nil
	}
	var res struct{ Code, URL, CA, Expires string }
	if err := call("POST", "/local/devices/code", body, &res); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(map[string]string{"url": res.URL, "ca": res.CA, "code": res.Code}, "", "  ")
	fmt.Printf("pairing code for %s (%s), valid 2 minutes, single use:\n%s\n", name, *scope, out)
	fmt.Println("the device makes its own key and sends a certificate request with this code to POST /v1/pair")
	return nil
}

func devicesCmd(args []string) error {
	if len(args) > 0 && (args[0] == "rename" || args[0] == "update") {
		return devicesAdminCmd(args) // admin_cli.go
	}
	if len(args) > 0 && args[0] == "reset" {
		return devicesReset(args[1:]) // devices_reset.go
	}
	if len(args) > 0 && args[0] == "uplink" {
		return devicesUplink(args) // bluetooth.go
	}
	if len(args) > 0 {
		action := args[0]
		switch {
		case action == "scope" && len(args) == 3:
			return call("POST", "/local/devices/"+args[1]+"/scope", map[string]string{"scope": args[2]}, nil)
		case action == "auto-approve":
			return devicesAutoApprove(args[1:])
		case (action == "revoke" || action == "approve" || action == "remove") && len(args) == 2:
			if err := call("POST", "/local/devices/"+args[1]+"/"+action, nil, nil); err != nil {
				return err
			}
			fmt.Printf("%s: %s\n", args[1], map[string]string{"revoke": "revoked", "approve": "approved", "remove": "removed"}[action])
			return nil
		}
		return errors.New("usage: agentctl devices [revoke|approve|remove <name> | scope <name> view|act|admin | auto-approve usb on|off [--scope view|act] | reset [--yes]]")
	}
	var res struct {
		Devices     []devices.Device
		CA, URL     string
		AutoApprove devices.AutoApprove
	}
	if err := call("GET", "/local/devices", nil, &res); err != nil {
		return err
	}
	fmt.Printf("listener %s · CA %s\n", res.URL, res.CA)
	fmt.Println(autoApproveLine(res.AutoApprove))
	fmt.Println()
	if len(res.Devices) == 0 {
		fmt.Println("no devices yet: agentctl pair <name>")
		return nil
	}
	// How each board is connected right now (USB, Wi-Fi), from the daemon's Deck Link sessions.
	var ls linkState
	_ = call("GET", "/local/link", nil, &ls)
	via := map[string][]string{}
	defWifi := map[string]string{}
	for _, b := range ls.Boards {
		if b.DefaultWifi != "" {
			defWifi[b.Board] = b.DefaultWifi
		}
		v := map[string]string{link.OverSerial: "usb", link.OverWifi: "wifi", link.OverBLE: "bluetooth"}[b.Transport]
		if b.Transport == link.OverWifi && b.Net != nil && b.Net.IP != "" {
			v += " " + b.Net.IP
			if b.Net.RSSI != 0 {
				v += fmt.Sprintf(" %d dBm", b.Net.RSSI)
			}
		}
		if b.Net != nil && b.Net.BLE != "" && b.Net.BLE != "not supported" {
			v += " (bluetooth " + b.Net.BLE
			if b.Net.Uplink != "" {
				v += ", uplink " + b.Net.Uplink
			}
			v += ")"
		}
		via[b.Board] = append(via[b.Board], v)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tSCOPE\tSTATE\tVIA\tLAST SEEN\tDETAIL")
	for _, d := range res.Devices {
		state := "active"
		switch {
		case d.Revoked:
			state = "revoked"
		case d.Kind == devices.Serial && !d.Approved:
			state = "waiting for approval"
		}
		seen := "never"
		if !d.LastSeen.IsZero() {
			seen = humanAgo(time.Since(d.LastSeen)) + " ago"
		}
		detail := ""
		if d.Kind == devices.Serial {
			detail = strings.TrimSpace(d.Type + " " + d.Firmware + " · " + d.BoardID)
			if d.CertSerial != "" {
				detail += " · Wi-Fi certificate"
			}
			if d.WifiOwn {
				detail += " · its own Wi-Fi network"
			}
			if dw := defWifi[d.Name]; dw != "" {
				detail += " · " + dw
			}
		}
		on := strings.Join(via[d.Name], ", ")
		if on == "" {
			on = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.Kind, d.Scope, state, on, seen, detail)
	}
	return tw.Flush()
}

func autoApproveLine(a devices.AutoApprove) string {
	if !a.USB {
		return "boards plugged in over USB: wait for agentctl devices approve (auto-approve usb off)"
	}
	return "boards plugged in over USB: approved automatically, scope " + string(a.Scope) + " (agentctl devices auto-approve usb off)"
}

// devicesAutoApprove: agentctl devices auto-approve usb on|off [--scope view|act].
func devicesAutoApprove(args []string) error {
	usage := errors.New("usage: agentctl devices auto-approve usb on|off [--scope view|act]")
	if len(args) < 2 || args[0] != "usb" || (args[1] != "on" && args[1] != "off") {
		return usage
	}
	fs := flag.NewFlagSet("devices auto-approve", flag.ExitOnError)
	scope := fs.String("scope", "", "the scope boards approved this way get: view or act (admin is given by hand)")
	_ = fs.Parse(args[2:])
	body := map[string]any{"usb": args[1] == "on"}
	if *scope != "" {
		body["scope"] = *scope
	}
	var res devices.AutoApprove
	if err := call("POST", "/local/devices/auto-approve", body, &res); err != nil {
		return err
	}
	fmt.Println(autoApproveLine(res))
	return nil
}

func humanAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func localHostName() string {
	if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
		if h := strings.TrimSpace(string(out)); h != "" {
			return h + ".local"
		}
	}
	h, _ := os.Hostname()
	return h
}

// ---- config & serve ---------------------------------------------------------------

type fileConfig struct {
	daemon.Config
	LocalAddr  string `json:"localAddr"`
	DeviceAddr string `json:"deviceAddr"` // "" disables the network listener (paired devices)
	Link       struct {
		Ports []link.PortConfig `json:"ports"` // serial ports with Deck Link boards (agentctl link add)
	} `json:"link"`
}

// lanIPs are this computer's non-loopback IPv4 addresses.
func lanIPs() []string {
	var ips []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			ips = append(ips, ipn.IP.String())
		}
	}
	return ips
}

func loadConfig() fileConfig {
	cfg := fileConfig{LocalAddr: "127.0.0.1:7340", DeviceAddr: ":7341"}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			log.Printf("config.json: %v (using defaults)", err)
		}
	}
	return cfg
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	demoMode := fs.Bool("demo", false, "serve a simulated fleet instead of real agents")
	still := fs.Bool("still", false, "with --demo: a fleet that doesn't change (for tests)")
	fixture := fs.String("fixture", "", "with --demo: the fleet from the dataset `FILE` instead of the built-in one")
	logFile := fs.String("log", "", "append the log to `FILE` instead of printing it")
	after := fs.Int(strings.TrimPrefix(service.AfterFlag, "--"), 0, "wait until process `PID` (the service this one replaces) has exited")
	_ = fs.Parse(args)
	if *fixture != "" && !*demoMode {
		return errors.New("--fixture goes with --demo")
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	if err := serviceLog(*logFile); err != nil {
		return err
	}
	if *after > 0 && !service.WaitGone(*after, 30*time.Second, 100*time.Millisecond, proc.Alive) {
		log.Printf("the service this one replaces (process %d) is still running after 30s", *after)
	}
	// A service that answers on the socket is already running. A second one
	// would take its socket away (ServeUnix replaces the file) and then stop
	// on the busy web port, leaving the first one unreachable.
	if c, err := net.DialTimeout("unix", paths.Socket(), time.Second); err == nil {
		c.Close()
		return errors.New("the service is already running (agentctl status)")
	}
	defer servicePID()()
	cfg := loadConfig()
	if cfg.LocalAddr == "" || !strings.HasPrefix(cfg.LocalAddr, "127.0.0.1:") {
		return errors.New("localAddr must be 127.0.0.1:<port> (the web UI is loopback-only)")
	}
	host := localHostName()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)

	var st *store.Store
	var ctl server.Controller
	var lc server.LocalController
	var fleet *demo.Demo
	if *demoMode {
		st = store.New("", strings.TrimSuffix(host, ".local")+" (demo)", version)
		var d *demo.Demo
		var err error
		if *fixture != "" {
			d, err = demo.LoadFile(st, *fixture) // only the dataset's fleet
		} else {
			d, err = demo.Load(st)
		}
		if err != nil {
			return err
		}
		if d.Name != "" {
			st.Name = d.Name // the dataset names the deck
		}
		d.Still = *still
		fleet = d
		go d.Run()
		ctl = d
	} else {
		st = store.New(paths.Agents(), strings.TrimSuffix(host, ".local"), version)
		st.Load()
		d, err := daemon.New(st, cfg.Config, self)
		if err != nil {
			return err
		}
		d.Adopt()
		go d.Run()
		go d.WatchCodex()
		go d.WatchOrca()
		go d.WatchHerdr()
		go d.WatchCmux()
		go d.WatchLive()
		ctl, lc = d, d
	}

	webSession, err := server.LoadOrCreateSecret(paths.WebToken())
	if err != nil {
		return err
	}
	book := &hats.Book{Path: paths.Config()}
	st.Hat = book.For
	if fleet != nil {
		st.Hat = demoHats(fleet, book)
	}
	st.Account = accounts.Of
	srv := &server.Server{Store: st, Ctl: ctl, Static: web.Static(),
		WebSession: webSession, LocalAddr: cfg.LocalAddr, Hats: book,
		Devices: &devices.Registry{Path: paths.Devices()}, Audit: &devices.Audit{Path: paths.Audit()},
		Tokens: &devices.Tokens{Path: paths.MCPTokens()}}
	errc := make(chan error, 4)
	// The default Wi-Fi network for boards plugged in over USB: its password in
	// the OS secret store (the login Keychain on macOS), never in a file here.
	var wifiStore pki.Keystore = pki.DefaultKeystore(paths.Data())
	if *demoMode { // a demo never touches the real secret store
		wifiStore = pki.FileKeystore{Dir: filepath.Join(paths.Data(), "keys")}
	}
	wifiDefault := &link.WifiDefault{Store: wifiStore}
	adm := newAdmin(st, srv, self, *logFile)
	hub := &link.Hub{Wifi: wifiDefault, OnFirmware: adm.FirmwareChanged} // updates over Wi-Fi feed admin jobs
	links := &link.Manager{Deck: link.ServerDeck{S: srv}, Hub: hub, Wifi: wifiDefault,
		Ports: func() []link.PortConfig { return adm.LinkPorts(loadConfig().Link.Ports) }}
	go links.Run(context.Background())
	srv.LinkUpgrade = link.UpgradeHandler(link.ServerDeck{S: srv}, hub)
	srv.LinkStatus = func() any {
		return map[string]any{"ports": links.Status(), "boards": hub.Links(), "wifiDefault": wifiDefault.SSID()}
	}
	srv.LinkWifi = hub.SendWifi
	srv.LinkWifiDefault = func(ssid, password string, forget bool) error {
		if forget {
			return wifiDefault.Clear()
		}
		return wifiDefault.Set(ssid, password)
	}
	srv.LinkWifiUseDefault = hub.SendDefault
	srv.LinkFirmware, srv.LinkFirmwareStatus = hub.UpdateFromFile, func() any { return hub.Firmware() }
	var deviceTLS *tls.Config
	if cfg.DeviceAddr != "" {
		ca, err := pki.LoadOrCreateCA(paths.CACert(), pki.DefaultKeystore(paths.Data()), strings.TrimSuffix(host, ".local"))
		if err != nil {
			return fmt.Errorf("certificate authority: %w", err)
		}
		hosts := append([]string{host, "localhost", "127.0.0.1"}, lanIPs()...)
		cert, err := ca.ServerCert(paths.ServerCert(), paths.ServerKey(), hosts)
		if err != nil {
			return fmt.Errorf("server certificate: %w", err)
		}
		_, port, _ := net.SplitHostPort(cfg.DeviceAddr)
		srv.CA, srv.DeviceURL = ca, "https://"+host+":"+port
		// Where a board on Wi-Fi finds the deck: the .local name, then the addresses.
		srv.DeviceHosts = append([]string{host}, lanIPs()...)
		srv.DevicePort, _ = strconv.Atoi(port)
		srv.DeviceLink = link.DeviceUpgradeHandler(link.ServerDeck{S: srv}, hub)
		srv.DeviceFirmware = link.FirmwareHandler(link.ServerDeck{S: srv}, hub) // images from the release catalog (wireAdmin)
		srv.ServerFingerprint = pki.Fingerprint(cert.Certificate[0])
		deviceTLS = srv.TLSConfig(cert)
		for _, old := range []string{"device-token", "tls-cert.pem", "tls-key.pem"} { // before per-device certificates
			_ = os.Remove(filepath.Join(paths.Data(), old))
		}
	}

	adm.Bluetooth = startBluetooth(srv, hub, *demoMode) // bluetooth.go
	wireAdmin(adm, srv, hub, links, wifiDefault, deviceTLS != nil, *demoMode)

	if lc != nil {
		go func() { errc <- fmt.Errorf("unix socket: %w", server.ServeUnix(paths.Socket(), srv.UnixHandler(lc))) }()
	} else {
		go func() {
			errc <- fmt.Errorf("unix socket: %w", server.ServeUnix(paths.Socket(), srv.UnixHandler(demoLocal{ctl})))
		}()
	}
	go func() {
		errc <- fmt.Errorf("web %s: %w", cfg.LocalAddr, server.ServeLocal(cfg.LocalAddr, srv.LocalHandler()))
	}()
	if deviceTLS != nil {
		log.Printf("device listener %s: TLS 1.3, client certificates from CA %s", cfg.DeviceAddr, srv.CA.Fingerprint())
		go func() {
			errc <- fmt.Errorf("devices %s: %w", cfg.DeviceAddr, server.ServeDevice(cfg.DeviceAddr, deviceTLS, srv.DeviceHandler()))
		}()
	}
	log.Printf("agentctl %s serving web on http://%s", version, cfg.LocalAddr)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case err := <-errc:
		st.Flush()
		return err
	case <-sig:
		st.Flush()
		log.Printf("stopping")
		return nil
	case <-serveStop:
		st.Flush()
		log.Printf("stopping (asked from Settings)")
		return nil
	}
}

// demoHats gives a demo agent its hat: one picked by hand on a screen, else
// the one its dataset names, else the usual one for its folder.
func demoHats(d *demo.Demo, book *hats.Book) func(cwd string) *model.Hat {
	return func(cwd string) *model.Hat {
		if _, byHand := book.Overrides()[hats.Clean(cwd)]; !byHand {
			if h := d.Hat(cwd); h != nil {
				return h
			}
		}
		return book.For(cwd)
	}
}

// demoLocal lets the CLI talk to a demo daemon (no hooks, no create).
type demoLocal struct{ server.Controller }

func (demoLocal) Hook(string, []byte) error { return nil }
func (demoLocal) Create(server.CreateOpts) (string, error) {
	return "", errors.New("demo mode: agents can't be created")
}
func (demoLocal) ResumeSession(string, string, string) (string, error) {
	return "", errors.New("demo mode: no history")
}
func (demoLocal) HistoryJSON() any { return []any{} }

// ---- settings ----------------------------------------------------------------------

func setCmd(args []string) error {
	if len(args) > 0 && args[0] == "hat" {
		return setHat(args[1:])
	}
	if len(args) != 2 {
		return errors.New("usage: agentctl set <key> <value>   e.g. agentctl set term tmux")
	}
	if err := daemon.Set(args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", args[0], args[1])
	if args[0] == "mouse" { // applies to running agents now; reattach for links and scrollback
		tmux.NativeMouse = func() bool { return daemon.CurrentMouse() == daemon.MouseNative }
		tmux.ServerDefaults()
		fmt.Println("applied to running agents (reattach a terminal to pick up links and scrollback)")
	}
	return nil
}

func getCmd(args []string) error {
	vals := map[string]string{"term": daemon.CurrentTerm(), "mouse": daemon.CurrentMouse(), "restore": daemon.CurrentRestore(), "header": daemon.CurrentHeader(), "sessions": daemon.CurrentSessions()}
	keys := []string{"term", "mouse", "restore", "header", "sessions"}
	if len(args) == 1 {
		v, ok := vals[args[0]]
		if !ok {
			return fmt.Errorf("unknown setting %q", args[0])
		}
		fmt.Println(v)
		return nil
	}
	for _, k := range keys {
		fmt.Printf("%-8s %-7s %s\n", k, vals[k], daemon.Settings[k].Help)
	}
	return nil
}

// openLink opens the link a click in an agent terminal landed on: the
// hyperlink under the mouse, else a URL-looking word. Only http, https and
// file links are opened (macOS open), like ⌘-click in iTerm.
func openLink(args []string) {
	var link, word string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--link="); ok {
			link = v
		} else if v, ok := strings.CutPrefix(a, "--word="); ok {
			word = v
		}
	}
	if u := linkTarget(link, word); u != "" {
		_ = proc.Open(u)
	}
}

func linkTarget(link, word string) string {
	cand := strings.TrimSpace(link)
	if cand == "" {
		// A word in prose: drop the punctuation around it.
		cand = strings.TrimRight(strings.TrimLeft(strings.TrimSpace(word), "(<[{'\"`"), ".,;:!?)>]}'\"`")
	}
	u, err := url.Parse(cand)
	if err != nil || strings.ContainsAny(cand, " \t\n") {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
	case "file":
		if u.Path == "" {
			return ""
		}
	default:
		return ""
	}
	return u.String()
}

// ---- simulation --------------------------------------------------------------------

func simCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agentctl sim start --root DIR --slots N [--speed X] [--patience S] | agentctl sim stop")
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("sim start", flag.ExitOnError)
		root := fs.String("root", "", "parent folder whose projects the simulated agents work in")
		slots := fs.Int("slots", 8, "how many simulated agents")
		speed := fs.Float64("speed", 1, "activity speed (2 = twice as fast)")
		patience := fs.Int("patience", 45, "seconds before a simulated user answers an unanswered prompt (-1 = never)")
		_ = fs.Parse(args[1:])
		if *root == "" {
			return errors.New("--root is required (e.g. --root ~/dev)")
		}
		dir, err := filepath.Abs(*root)
		if err != nil {
			return err
		}
		projects, err := sim.Projects(dir)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return fmt.Errorf("no projects found under %s (looking for folders with .git, go.mod, package.json, …)", dir)
		}
		if *slots < 1 || *slots > 64 {
			return errors.New("--slots must be 1..64")
		}
		rand.Shuffle(len(projects), func(i, j int) { projects[i], projects[j] = projects[j], projects[i] })
		for i := 0; i < *slots; i++ {
			p := projects[i%len(projects)]
			var res struct{ ID string }
			if err := call("POST", "/local/agents", server.CreateOpts{Dir: p, Tool: "sim", Speed: *speed, Patience: *patience}, &res); err != nil {
				return err
			}
			fmt.Printf("%s  %s\n", res.ID, filepath.Base(p))
		}
		fmt.Fprintf(os.Stderr, "%d simulated agents started from %d projects in %s — stop with `agentctl sim stop`\n", *slots, min(*slots, len(projects)), dir)
		return nil
	case "stop":
		var rows []struct {
			Sim   bool        `json:"sim"`
			Agent model.Agent `json:"agent"`
		}
		if err := call("GET", "/local/agents", nil, &rows); err != nil {
			return err
		}
		n := 0
		for _, r := range rows {
			if r.Sim {
				if err := call("POST", "/v1/agents/"+r.Agent.ID+"/dismiss", nil, nil); err == nil {
					n++
				}
			}
		}
		fmt.Printf("removed %d simulated agents\n", n)
		return nil
	}
	return fmt.Errorf("unknown sim command %q", args[0])
}

// simAgent is the fake Claude that runs inside an agent's tmux session.
func simAgent(args []string) error {
	fs := flag.NewFlagSet("sim-agent", flag.ExitOnError)
	dir := fs.String("dir", "", "project folder")
	speed := fs.Float64("speed", 1, "")
	patience := fs.Int("patience", 90, "")
	transcript := fs.String("transcript", "", "")
	_ = fs.Parse(args)
	id := os.Getenv("AGENTSTERM_ID")
	if id == "" || *dir == "" || *transcript == "" {
		return errors.New("sim-agent is started by the daemon (agentctl sim start)")
	}
	sock := os.Getenv("AGENTSTERM_SOCK")
	if sock == "" {
		sock = paths.Socket()
	}
	var pat time.Duration
	if *patience > 0 {
		pat = time.Duration(*patience) * time.Second
	}
	return sim.Run(sim.Config{ID: id, Socket: sock, Dir: *dir, Transcript: *transcript,
		Seed: time.Now().UnixNano() ^ int64(len(id))<<20, Speed: *speed, Patience: pat})
}
