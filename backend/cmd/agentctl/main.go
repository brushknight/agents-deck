// agentctl runs and controls the agents-terminal daemon.
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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/daemon"
	"github.com/brushknight/agents-deck/backend/internal/demo"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/sim"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
	"github.com/brushknight/agents-deck/backend/internal/web"
)

// version is stamped by release builds: -ldflags "-X main.version=<tag>".
var version = "dev"

const usage = `agentctl — agents-terminal

  agentctl new [dir] [-t title] [-c claude|codex|gemini|shell] [-a account] [-d]
                                 start an agent (dir defaults to .) and attach (-d: don't)
  agentctl restore               bring back agents lost with the tmux server (crash, reboot)
  agentctl hats                  the hats agents wear (one per working folder)
  agentctl accounts              the Claude accounts agents run in (account add|default|rm)
  agentctl handoff <id|title> <account>
                                 continue a Claude session in another account (out of usage)
  agentctl set hat <agent|folder> <shape> [colour] | auto
  agentctl live                  Claude sessions started by hand in other terminals
  agentctl add <session>         put one of them on the deck (watched)
  agentctl reopen                iTerm tabs for every agent no terminal shows (after a crash)
  agentctl attach <id|title>     show an agent in this terminal (detach: ctrl-q)
  agentctl ls                    list agents
  agentctl rm <id|title>         stop and forget an agent
  agentctl move <id|title> <n>   put an agent at position n (1 = first tile; free spots ok)
  agentctl sessions [words] [-n N]
                                 look up Claude sessions (any, not just agentctl's) by id/title/folder/prompt
  agentctl resume <id> [-d] [--force]
                                 continue a Claude session by id or id prefix (or an ended agent's id/title)
  agentctl sim start --root DIR --slots N [--speed X] [--patience S]
                                 simulated Claudes working in real projects under DIR (for demos)
  agentctl sim stop              remove all simulated agents
  agentctl web                   open the dashboard in the browser
  agentctl pair <name> [--scope view|act|admin] [--bundle FILE]
                                 pair a device: a one-time code it trades for its own certificate,
                                 or (--bundle) a key and certificate to copy to it over SSH
  agentctl devices [revoke|approve|remove <name> | scope <name> <scope>]
                                 paired devices and serial boards; cut one off, approve a board
  agentctl link [ports | add <port> [--baud N] | rm <port>]
                                 Deck Link boards: status (USB or Wi-Fi), find ports, register one
  agentctl link wifi <board> --ssid NAME [--password PW] | --forget
                                 give a board on USB its Wi-Fi network (asks for the password; never
                                 stored on the Mac); from then on it connects over Wi-Fi when not on USB
  agentctl link pair <board>     a new Wi-Fi certificate for a board (it pairs again over USB)
  agentctl link sim [--id ID] [--fields tile,card,prompt,numbers] [--json]
                                 be a board without hardware (type commands: answer, focus, …)
  agentctl link check <port> [--baud N] [--seconds N]
                                 test a board's firmware against a demo deck (stop the daemon's use: link rm)
  agentctl serve [--demo]        run the daemon (normally via launchd)
  agentctl install               install + start the launchd agent
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
		err = restore()
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
		err = list()
	case "rm":
		err = remove(args)
	case "move", "mv":
		err = move(args)
	case "resume", "continue":
		err = resume(args)
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
	case "devices":
		err = devicesCmd(args)
	case "link":
		err = linkCmd(args)
	case "install":
		err = install()
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
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://agentd"+path, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := unixClient(10 * time.Second).Do(req)
	if err != nil {
		return errors.New("daemon not running — start it with `agentctl install` (or `agentctl serve`)")
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

func state() (model.State, error) {
	var s model.State
	return s, call("GET", "/v1/state", nil, &s)
}

func resolve(ref string) (model.Agent, error) {
	s, err := state()
	if err != nil {
		return model.Agent{}, err
	}
	for _, a := range s.Agents {
		if a.ID == ref {
			return a, nil
		}
	}
	for _, a := range s.Agents {
		if a.Title == ref && a.Status != model.Exited {
			return a, nil
		}
	}
	return model.Agent{}, fmt.Errorf("no agent %q", ref)
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
		return errors.New("usage: agentctl attach <id|title>")
	}
	a, err := resolve(args[0])
	if err != nil {
		return err
	}
	if a.Status == model.Exited {
		return fmt.Errorf("%s has exited", a.Title)
	}
	argv := tmux.AttachArgv("a-" + a.ID)
	if os.Getenv("TMUX") != "" {
		// Inside tmux already: open in a new window of the outer tmux instead of nesting.
		return exec.Command("tmux", "new-window", "-n", a.Title, strings.Join(argv, " ")).Run()
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

// hatName accepts "hard hat", hard-hat or hard_hat.
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
	a, err := resolve(target)
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

// restore brings back every agent lost with the tmux server (crash, reboot).
func restore() error {
	var res struct {
		Results []struct {
			ID, Title, Action, Error string
		}
	}
	if err := call("POST", "/v1/restore", nil, &res); err != nil {
		return err
	}
	if len(res.Results) == 0 {
		fmt.Println("nothing to restore: no agents were lost with the tmux server")
		return nil
	}
	failed := 0
	for _, r := range res.Results {
		line := fmt.Sprintf("%s  %-10s %s", r.ID, r.Action, r.Title)
		if r.Error != "" {
			line += "  (" + r.Error + ")"
			failed++
		}
		fmt.Println(line)
	}
	if n := len(res.Results) - failed; n > 0 {
		fmt.Printf("%d agent(s) back · `agentctl reopen` opens their terminals\n", n)
	}
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

func list() error {
	s, err := state()
	if err != nil {
		return err
	}
	if len(s.Agents) == 0 {
		fmt.Println("no agents — start one with `agentctl new`")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	acctCol := false
	for _, a := range s.Agents {
		acctCol = acctCol || a.Account != nil
	}
	if acctCol {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tCONTEXT\tCOST\tFOLDER\tACCOUNT")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tCONTEXT\tCOST\tFOLDER")
	}
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
		}
		if a.Waiting != nil {
			st += ": " + a.Waiting.Title
		}
		cost := fmt.Sprintf("$%.2f", a.CostUSD)
		if a.External {
			cost = "—" // Codex reports no prices
		}
		if acctCol {
			acct := ""
			if a.Account != nil {
				acct = a.Account.Name
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Title, st, ctx, cost, a.Folder, acct)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Title, st, ctx, cost, a.Folder)
		}
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
	fmt.Fprintln(os.Stderr, "\ncontinue one with: agentctl resume <id>")
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

// resume continues an ended session and attaches to it: an exited agent on the
// board (id or title), or any Claude session by id / id prefix (see `agentctl sessions`).
func resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	force := fs.Bool("force", false, "resume even if the session looks open elsewhere")
	detached := fs.Bool("d", false, "don't attach")
	_ = fs.Parse(reorder(args, nil))
	if fs.NArg() == 0 {
		return sessionsCmd([]string{"-n", "15"})
	}
	ref := fs.Arg(0)
	done := func(id string) error {
		if *detached {
			fmt.Println(id)
			return nil
		}
		return attach([]string{id})
	}
	s, err := state()
	if err != nil {
		return err
	}
	for _, a := range s.Agents {
		if a.Resumable && (a.ID == ref || a.Title == ref) {
			if err := call("POST", "/v1/agents/"+a.ID+"/resume", nil, nil); err != nil {
				return err
			}
			return done(a.ID)
		}
	}
	all, _, err := allSessions()
	if err != nil {
		return err
	}
	sess, hits := claude.FindSession(all, ref)
	switch {
	case hits == 0:
		return fmt.Errorf("no session matches %q — look it up with `agentctl sessions <words>`", ref)
	case hits > 1:
		return fmt.Errorf("%q matches %d sessions — use more of the id", ref, hits)
	}
	if a, ok := boardSessions()[sess.ID]; ok && a.Status != model.Exited {
		fmt.Fprintf(os.Stderr, "already running as agent %s\n", a.ID)
		return done(a.ID)
	}
	if time.Since(sess.Modified) < liveWindow && !*force {
		return fmt.Errorf("session %s was written %s ago — it may still be open in another terminal (use --force to resume anyway)", sess.ID[:8], ago(sess.Modified))
	}
	if _, err := os.Stat(sess.Cwd); err != nil {
		return fmt.Errorf("the session's folder is gone: %s", sess.Cwd)
	}
	var res struct{ ID string }
	if err := call("POST", "/local/resume", map[string]string{"sessionId": sess.ID, "cwd": sess.Cwd}, &res); err != nil {
		return err
	}
	return done(res.ID)
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
	a, err := resolve(args[0])
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
	a, err := resolve(args[0])
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

// openBrowser opens url in the default browser: `open` on macOS, `xdg-open`
// elsewhere (Linux). The one-time login code expires quickly, so callers
// should mint it immediately before calling this.
func openBrowser(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Run()
}

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
		return errors.New("usage: agentctl devices [revoke|approve|remove <name> | scope <name> view|act|admin | auto-approve usb on|off [--scope view|act]]")
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
		v := map[string]string{link.OverSerial: "usb", link.OverWifi: "wifi"}[b.Transport]
		if b.Transport == link.OverWifi && b.Net != nil && b.Net.IP != "" {
			v += " " + b.Net.IP
			if b.Net.RSSI != 0 {
				v += fmt.Sprintf(" %d dBm", b.Net.RSSI)
			}
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

// lanIPs are this Mac's non-loopback IPv4 addresses.
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
	_ = fs.Parse(args)
	if err := paths.Ensure(); err != nil {
		return err
	}
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
	if *demoMode {
		st = store.New("", strings.TrimSuffix(host, ".local")+" (demo)", version)
		d, err := demo.Load(st)
		if err != nil {
			return err
		}
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
	st.Account = accounts.Of
	srv := &server.Server{Store: st, Ctl: ctl, Static: web.Static(),
		WebSession: webSession, LocalAddr: cfg.LocalAddr, Hats: book,
		Devices: &devices.Registry{Path: paths.Devices()}, Audit: &devices.Audit{Path: paths.Audit()}}
	errc := make(chan error, 4)
	// The default Wi-Fi network for boards plugged in over USB: its password in
	// the OS secret store (the login Keychain on macOS), never in a file here.
	wifiDefault := &link.WifiDefault{Store: pki.DefaultKeystore(paths.Data())}
	hub := &link.Hub{Wifi: wifiDefault}
	links := &link.Manager{Deck: link.ServerDeck{S: srv}, Hub: hub, Wifi: wifiDefault,
		Ports: func() []link.PortConfig { return loadConfig().Link.Ports }}
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
		srv.ServerFingerprint = pki.Fingerprint(cert.Certificate[0])
		deviceTLS = srv.TLSConfig(cert)
		for _, old := range []string{"device-token", "tls-cert.pem", "tls-key.pem"} { // before per-device certificates
			_ = os.Remove(filepath.Join(paths.Data(), old))
		}
	}

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
	vals := map[string]string{"term": daemon.CurrentTerm(), "mouse": daemon.CurrentMouse(), "restore": daemon.CurrentRestore(), "header": daemon.CurrentHeader()}
	keys := []string{"term", "mouse", "restore", "header"}
	if len(args) == 1 {
		v, ok := vals[args[0]]
		if !ok {
			return fmt.Errorf("unknown setting %q", args[0])
		}
		fmt.Println(v)
		return nil
	}
	for _, k := range keys {
		fmt.Printf("%-7s %-7s %s\n", k, vals[k], daemon.Settings[k].Help)
	}
	return nil
}

// openLink opens the link a click in an agent terminal landed on: the
// hyperlink under the mouse, else a URL-looking word. Only http, https and
// file links go to macOS open, like ⌘-click in iTerm.
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
		_ = exec.Command("/usr/bin/open", u).Run()
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

// ---- install ---------------------------------------------------------------------

const plistLabel = "dev.agents-deck.agentctl"

func install() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if err := paths.Ensure(); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "linux" {
		return installSystemd(self, home)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>serve</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>%s/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, plistLabel, self, home, paths.Log(), paths.Log())
	uid := fmt.Sprint(os.Getuid())
	job := "gui/" + uid + "/" + plistLabel
	// Same login item already registered: just restart the daemon (it picks
	// up a rebuilt binary). Re-registering makes macOS announce a new
	// background item every time.
	if cur, err := os.ReadFile(plist); err == nil && string(cur) == content &&
		exec.Command("launchctl", "print", job).Run() == nil {
		if out, err := exec.Command("launchctl", "kickstart", "-k", job).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl kickstart: %v: %s", err, out)
		}
		fmt.Println("restarted", plistLabel)
		return nil
	}
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", job).Run()
	time.Sleep(500 * time.Millisecond)
	if out, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	fmt.Println("installed", plist)
	return nil
}

// installSystemd is install on Linux: a systemd user service that starts the
// daemon at login and keeps it running, like the launchd agent on macOS.
func installSystemd(self, home string) error {
	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	unit := filepath.Join(dir, plistLabel+".service")
	content := fmt.Sprintf(`[Unit]
Description=agents deck daemon (agentctl serve)

[Service]
ExecStart=%s serve
Environment=PATH=%s/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
Restart=always
RestartSec=2
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, self, home, paths.Log(), paths.Log())
	systemctl := func(args ...string) error {
		if out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	// Same service already running: just restart the daemon (it picks up a
	// rebuilt binary).
	if cur, err := os.ReadFile(unit); err == nil && string(cur) == content &&
		exec.Command("systemctl", "--user", "is-active", "--quiet", plistLabel).Run() == nil {
		if err := systemctl("restart", plistLabel); err != nil {
			return err
		}
		fmt.Println("restarted", plistLabel)
		return nil
	}
	if err := os.WriteFile(unit, []byte(content), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", plistLabel); err != nil {
		return err
	}
	if err := systemctl("restart", plistLabel); err != nil {
		return err
	}
	fmt.Println("installed", unit)
	return nil
}
