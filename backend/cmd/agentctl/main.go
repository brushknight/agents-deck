// agentctl runs and controls the agents-terminal daemon.
package main

import (
	"bytes"
	"context"
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
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/daemon"
	"github.com/brushknight/agents-deck/backend/internal/demo"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/sim"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
	"github.com/brushknight/agents-deck/backend/internal/web"
)

const version = "0.1.0"

const usage = `agentctl — agents-terminal

  agentctl new [dir] [-t title] [-c claude|codex|gemini|shell] [-d]
                                 start an agent (dir defaults to .) and attach (-d: don't)
  agentctl restore               bring back agents lost with the tmux server (crash, reboot)
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
  agentctl pair [--rotate]       print the values the panel needs
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
	detached := fs.Bool("d", false, "don't attach")
	_ = fs.Parse(reorder(args, map[string]bool{"-t": true, "-c": true}))
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var res struct{ ID string }
	if err := call("POST", "/local/agents", map[string]string{"dir": abs, "title": *title, "tool": *tool}, &res); err != nil {
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
	fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tCONTEXT\tCOST\tFOLDER")
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Title, st, ctx, cost, a.Folder)
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
	all, err := claude.ScanSessions(claude.ProjectsDir())
	if err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	board := boardSessions()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.ID[:8], when, clip(s.Title(), 32),
			clip(shortHome(s.Cwd), 28), clip(s.Branch, 16), clip(s.LastPrompt, 48))
		shown++
	}
	if shown == 0 {
		if query != "" {
			fmt.Printf("no sessions match %q\n", query)
		} else {
			fmt.Println("no Claude sessions found in", claude.ProjectsDir())
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
	all, err := claude.ScanSessions(claude.ProjectsDir())
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
	if len(args) > 0 && args[0] == "--phone" {
		if cfg.Web != "lan" {
			return errors.New("the dashboard is on this Mac only: run `agentctl set web lan` first")
		}
		var res struct{ Code string }
		if err := call("POST", "/local/login-code?for=phone", nil, &res); err != nil {
			return err
		}
		ips := lanIPs()
		hosts := append([]string{}, ips...)
		hosts = append(hosts, localHostName())
		fmt.Println("open on your phone (one-time link, valid 5 minutes):")
		for _, h := range hosts {
			fmt.Printf("  http://%s:%s/login?code=%s\n", h, lanWebPort, res.Code)
		}
		if len(hosts) > 0 {
			link := fmt.Sprintf("http://%s:%s/login?code=%s", hosts[0], lanWebPort, res.Code)
			c := exec.Command("/usr/bin/pbcopy")
			c.Stdin = strings.NewReader(link)
			if c.Run() == nil {
				fmt.Println("the first link is on the clipboard (paste it on an iPhone via Universal Clipboard)")
			}
		}
		return nil
	}
	var res struct{ Code string }
	if err := call("POST", "/local/login-code", nil, &res); err != nil {
		return err
	}
	return exec.Command("open", "http://"+cfg.LocalAddr+"/login?code="+res.Code).Run()
}

func pair(args []string) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	rotate := len(args) > 0 && args[0] == "--rotate"
	var tok string
	var err error
	if rotate {
		tok, err = server.RotateSecret(paths.DeviceToken())
	} else {
		tok, err = server.LoadOrCreateSecret(paths.DeviceToken())
	}
	if err != nil {
		return err
	}
	host := localHostName()
	cert, err := server.LoadOrCreateCert(paths.TLSCert(), paths.TLSKey(), host)
	if err != nil {
		return err
	}
	cfg := loadConfig()
	_, port, _ := net.SplitHostPort(cfg.DeviceAddr)
	out, _ := json.MarshalIndent(map[string]string{
		"url":         "https://" + host + ":" + port,
		"token":       tok,
		"fingerprint": server.Fingerprint(cert),
	}, "", "  ")
	fmt.Println(string(out))
	if rotate {
		fmt.Fprintln(os.Stderr, "token rotated — the daemon picks it up within a few seconds; re-pair the panel")
	}
	return nil
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
	DeviceAddr string `json:"deviceAddr"` // "" disables the LAN listener
	Web        string `json:"web"`        // "lan" also serves the web UI on the LAN (lanWebPort)
}

const lanWebPort = "7342"

// lanHosts are the host:port names the LAN web listener answers to.
func lanHosts() []string {
	hosts := []string{localHostName() + ":" + lanWebPort}
	for _, ip := range lanIPs() {
		hosts = append(hosts, ip+":"+lanWebPort)
	}
	return hosts
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
		ctl, lc = d, d
	}

	webSession, err := server.LoadOrCreateSecret(paths.WebToken())
	if err != nil {
		return err
	}
	tokenCache := newSecretCache(paths.DeviceToken())
	srv := &server.Server{Store: st, Ctl: ctl, Static: web.Static(), DeviceToken: tokenCache.get,
		WebSession: webSession, LocalAddr: cfg.LocalAddr}
	errc := make(chan error, 4)
	if cfg.Web == "lan" {
		srv.LANHosts = lanHosts
		log.Printf("web also on the local network, port %s", lanWebPort)
		go func() {
			errc <- fmt.Errorf("web lan :%s: %w", lanWebPort, server.ServeLocal(":"+lanWebPort, srv.LANHandler()))
		}()
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
	if cfg.DeviceAddr != "" {
		if _, err := server.LoadOrCreateSecret(paths.DeviceToken()); err != nil {
			return err
		}
		cert, err := server.LoadOrCreateCert(paths.TLSCert(), paths.TLSKey(), host)
		if err != nil {
			return err
		}
		log.Printf("panel listener %s (fingerprint %s)", cfg.DeviceAddr, server.Fingerprint(cert))
		go func() {
			errc <- fmt.Errorf("panel %s: %w", cfg.DeviceAddr, server.ServeDevice(cfg.DeviceAddr, cert, srv.DeviceHandler()))
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

// secretCache re-reads the device token file at most every few seconds so
// `agentctl pair --rotate` takes effect without a restart.
type secretCache struct {
	mu   sync.Mutex
	path string
	val  string
	at   time.Time
}

func newSecretCache(path string) *secretCache { return &secretCache{path: path} }

func (c *secretCache) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) > 3*time.Second {
		if b, err := os.ReadFile(c.path); err == nil {
			c.val = strings.TrimSpace(string(b))
		}
		c.at = time.Now()
	}
	return c.val
}

// ---- settings ----------------------------------------------------------------------

func setCmd(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: agentctl set <key> <value>   e.g. agentctl set term tmux")
	}
	if err := daemon.Set(args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", args[0], args[1])
	if args[0] == "web" { // the daemon reads it at start
		uid := fmt.Sprint(os.Getuid())
		if exec.Command("launchctl", "kickstart", "-k", "gui/"+uid+"/"+plistLabel).Run() == nil {
			fmt.Println("daemon restarted")
		}
	}
	if args[0] == "mouse" { // applies to running agents now; reattach for links and scrollback
		tmux.NativeMouse = func() bool { return daemon.CurrentMouse() == daemon.MouseNative }
		tmux.ServerDefaults()
		fmt.Println("applied to running agents (reattach a terminal to pick up links and scrollback)")
	}
	return nil
}

func getCmd(args []string) error {
	vals := map[string]string{"term": daemon.CurrentTerm(), "mouse": daemon.CurrentMouse(), "restore": daemon.CurrentRestore()}
	keys := []string{"term", "mouse", "restore"}
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
