package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

func linkCmd(args []string) error {
	if len(args) == 0 {
		return linkStatus()
	}
	switch args[0] {
	case "status":
		return linkStatus()
	case "ports":
		return linkPorts()
	case "add":
		return linkAdd(args[1:])
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New("usage: agentctl link rm <port>")
		}
		return editLinkPorts(func(ps []link.PortConfig) ([]link.PortConfig, error) {
			out := ps[:0]
			found := false
			for _, p := range ps {
				if p.Path == args[1] {
					found = true
					continue
				}
				out = append(out, p)
			}
			if !found {
				return nil, fmt.Errorf("%s isn't registered", args[1])
			}
			fmt.Printf("%s removed: the daemon lets go of it within 2 seconds\n", args[1])
			return out, nil
		})
	case "sim":
		return linkSim(args[1:])
	case "check":
		return linkCheck(args[1:])
	case "wifi":
		return linkWifi(args[1:])
	case "pair":
		if len(args) != 2 {
			return errors.New("usage: agentctl link pair <board>")
		}
		if err := call("POST", "/local/devices/"+args[1]+"/repair", nil, nil); err != nil {
			return err
		}
		fmt.Printf("%s: its certificate no longer works. On USB and approved, it pairs again by itself (a new key and certificate).\n", args[1])
		return nil
	}
	return errors.New("usage: agentctl link [status | ports | add <port> [--baud N] | rm <port> | sim | check <port> | wifi <board> | pair <board>]")
}

type linkState struct {
	Ports       []link.PortStatus
	Boards      []link.LinkInfo
	WifiDefault string
}

func linkStatus() error {
	var res linkState
	if err := call("GET", "/local/link", nil, &res); err != nil {
		return err
	}
	if len(res.Boards) > 0 {
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "BOARD\tVIA\tFROM\tSINCE\tWI-FI")
		for _, b := range res.Boards {
			via := map[string]string{link.OverSerial: "usb", link.OverWifi: "wifi"}[b.Transport]
			wifi := netLine(b.Net)
			if b.DefaultWifi != "" {
				wifi += " · " + b.DefaultWifi
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", b.Board, via, b.Where, link.Ago(b.Since), wifi)
		}
		_ = tw.Flush()
		fmt.Println()
	}
	if res.WifiDefault != "" {
		fmt.Printf("default Wi-Fi network: %s (sent to approved boards plugged in over USB)\n\n", res.WifiDefault)
	} else {
		fmt.Println("no default Wi-Fi network (agentctl link wifi --default --ssid NAME)")
		fmt.Println()
	}
	if len(res.Ports) == 0 {
		fmt.Println("no serial ports registered: plug a board in, then `agentctl link ports` and `agentctl link add <port>`")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PORT\tBAUD\tSTATE\tBOARD\tSINCE")
	for _, p := range res.Ports {
		state := p.State
		if p.Problem != "" {
			state += ": " + p.Problem
		}
		since := ""
		if !p.Since.IsZero() {
			since = link.Ago(p.Since)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", p.Path, p.Baud, state, p.Board, since)
	}
	return tw.Flush()
}

func linkPorts() error {
	registered := map[string]bool{}
	for _, p := range loadConfig().Link.Ports {
		registered[p.Path] = true
	}
	ports := link.CandidatePorts()
	if len(ports) == 0 {
		fmt.Println("no USB serial ports found: plug the board in (and use a data cable)")
		return nil
	}
	for _, p := range ports {
		mark := "  "
		if registered[p] {
			mark = "✓ "
		}
		fmt.Println(mark + p)
	}
	fmt.Println("\nregister one with: agentctl link add <port>   (only registered ports are ever opened)")
	return nil
}

func linkAdd(args []string) error {
	fs := flag.NewFlagSet("link add", flag.ExitOnError)
	baud := fs.Int("baud", 115200, "baud rate (boards with native USB ignore it)")
	var port string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		port, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if port == "" {
		return errors.New("usage: agentctl link add <port> [--baud N]   (see agentctl link ports)")
	}
	if _, err := os.Stat(port); err != nil {
		fmt.Fprintf(os.Stderr, "note: %s isn't there right now; it's used whenever it appears\n", port)
	}
	return editLinkPorts(func(ps []link.PortConfig) ([]link.PortConfig, error) {
		for i, p := range ps {
			if p.Path == port {
				ps[i].Baud = *baud
				fmt.Printf("%s: baud %d\n", port, *baud)
				return ps, nil
			}
		}
		fmt.Printf("%s registered: the daemon opens it within 2 seconds. A new board then waits for `agentctl devices approve <name>`.\n", port)
		return append(ps, link.PortConfig{Path: port, Baud: *baud}), nil
	})
}

// editLinkPorts rewrites config.json "link.ports", keeping every other key.
func editLinkPorts(fn func([]link.PortConfig) ([]link.PortConfig, error)) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(paths.Config()); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON: %v", err)
		}
	}
	ports, err := fn(loadConfig().Link.Ports)
	if err != nil {
		return err
	}
	l, _ := cfg["link"].(map[string]any)
	if l == nil {
		l = map[string]any{}
	}
	l["ports"] = ports
	cfg["link"] = l
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := paths.Config() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.Config())
}

func parseFields(s string) (uint32, error) {
	var f uint32
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(part) {
		case "tile":
			f |= uint32(linkpb.FieldGroup_FIELDS_TILE)
		case "card":
			f |= uint32(linkpb.FieldGroup_FIELDS_CARD)
		case "prompt":
			f |= uint32(linkpb.FieldGroup_FIELDS_PROMPT)
		case "numbers":
			f |= uint32(linkpb.FieldGroup_FIELDS_NUMBERS)
		case "":
		default:
			return 0, fmt.Errorf("unknown field group %q (tile, card, prompt, numbers)", part)
		}
	}
	return f, nil
}

func linkSim(args []string) error {
	fs := flag.NewFlagSet("link sim", flag.ExitOnError)
	id := fs.String("id", "sim-board", "board id (a new id is a new board to approve)")
	fields := fs.String("fields", "tile,prompt", "field groups: tile, card, prompt, numbers")
	asJSON := fs.Bool("json", false, "speak the legacy JSON-lines format")
	_ = fs.Parse(args)
	f, err := parseFields(*fields)
	if err != nil {
		return err
	}
	conn, err := link.DialSim(paths.Socket())
	if err != nil {
		return fmt.Errorf("is the daemon running? %w", err)
	}
	defer conn.Close()
	b := link.NewBoard(conn, &linkpb.Hello{Protocol: 1, BoardId: *id, Type: "sim", Firmware: version}, f)
	if *asJSON {
		b.Mode = link.JSON
	}
	b.Event = func(f *linkpb.Frame) {
		if line := link.Describe(f); line != "" {
			fmt.Println(line)
		}
	}
	// A simulated board has no radio: it keeps the network and pretends it
	// joined (a documentation address), so `agentctl link wifi` can be tried.
	b.Wifi = func(w *linkpb.WifiConfig) (uint32, string) {
		st := &linkpb.NetStatus{Transport: linkpb.Transport_TRANSPORT_SERIAL, Wifi: linkpb.WifiState_WIFI_NOT_SET}
		if !w.Forget {
			st.Wifi, st.Ssid, st.Ip, st.Rssi = linkpb.WifiState_WIFI_IDLE, w.Ssid, "192.0.2.50", -50
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			b.Send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: st}})
		}()
		return 204, ""
	}
	done := make(chan error, 1)
	go func() { done <- b.Run() }()
	fmt.Fprintln(os.Stderr, "a simulated board on the deck. commands: answer <agent> <key>, focus|interrupt|dismiss|resume <agent>, move <agent> <slot>, hats, live, state, quit")
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			w := strings.Fields(sc.Text())
			if len(w) == 0 {
				continue
			}
			if err := simCommand(b, w); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		done <- nil
	}()
	return <-done
}

func simCommand(b *link.Board, w []string) error {
	agent := func() (*linkpb.Agent, error) {
		if len(w) < 2 {
			return nil, fmt.Errorf("usage: %s <agent id or title>", w[0])
		}
		if a := b.Agent(w[1]); a != nil {
			return a, nil
		}
		return nil, fmt.Errorf("no agent %q on this board", w[1])
	}
	switch w[0] {
	case "quit", "exit":
		os.Exit(0)
	case "state":
		b.Send(&linkpb.Frame{Body: &linkpb.Frame_GetState{GetState: &linkpb.GetState{}}})
	case "hats":
		b.Send(&linkpb.Frame{Body: &linkpb.Frame_ListHats{ListHats: &linkpb.ListHats{}}})
	case "live":
		b.Send(&linkpb.Frame{Body: &linkpb.Frame_ListLive{ListLive: &linkpb.ListLive{}}})
	case "answer":
		a, err := agent()
		if err != nil {
			return err
		}
		if a.Waiting == nil || len(w) < 3 {
			return errors.New("usage: answer <agent> <key> (the agent must be waiting; subscribe with --fields tile,prompt)")
		}
		b.Send(&linkpb.Frame{Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: a.Id, PromptId: a.Waiting.Id, Key: w[2]}}})
	case "focus", "interrupt", "dismiss", "resume":
		a, err := agent()
		if err != nil {
			return err
		}
		frames := map[string]*linkpb.Frame{
			"focus":     {Body: &linkpb.Frame_Focus{Focus: &linkpb.Focus{AgentId: a.Id}}},
			"interrupt": {Body: &linkpb.Frame_Interrupt{Interrupt: &linkpb.Interrupt{AgentId: a.Id}}},
			"dismiss":   {Body: &linkpb.Frame_Dismiss{Dismiss: &linkpb.Dismiss{AgentId: a.Id}}},
			"resume":    {Body: &linkpb.Frame_Resume{Resume: &linkpb.Resume{AgentId: a.Id}}},
		}
		b.Send(frames[w[0]])
	case "move":
		a, err := agent()
		if err != nil {
			return err
		}
		if len(w) < 3 {
			return errors.New("usage: move <agent> <slot>")
		}
		n, err := strconv.Atoi(w[2])
		if err != nil {
			return err
		}
		b.Send(&linkpb.Frame{Body: &linkpb.Frame_Move{Move: &linkpb.Move{AgentId: a.Id, Slot: uint32(n)}}})
	default:
		return fmt.Errorf("unknown command %q", w[0])
	}
	return nil
}

func linkCheck(args []string) error {
	fs := flag.NewFlagSet("link check", flag.ExitOnError)
	baud := fs.Int("baud", 115200, "baud rate")
	secs := fs.Int("seconds", 20, "how long to watch the board")
	var port string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		port, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if port == "" {
		return errors.New("usage: agentctl link check <port> [--baud N] [--seconds N]")
	}
	for _, p := range loadConfig().Link.Ports {
		if p.Path == port {
			return fmt.Errorf("the daemon uses %s: `agentctl link rm %s` first (add it back after)", port, port)
		}
	}
	f, err := link.OpenPort(port, *baud)
	if err != nil {
		return err
	}
	defer f.Close()
	fails, err := link.Check(f, os.Stdout, time.Duration(*secs)*time.Second)
	if err != nil {
		return err
	}
	if fails > 0 {
		return fmt.Errorf("%d checks failed", fails)
	}
	fmt.Println("all checks passed")
	return nil
}

// netLine is a board's Wi-Fi in a few words: "connected · 192.0.2.23 · -58 dBm".
func netLine(n *link.NetInfo) string {
	if n == nil {
		return "-"
	}
	parts := []string{n.Wifi}
	if n.IP != "" {
		parts = append(parts, n.IP)
	}
	if n.RSSI != 0 {
		parts = append(parts, fmt.Sprintf("%d dBm", n.RSSI))
	}
	if !n.HasCert && n.Wifi != "not supported" {
		parts = append(parts, "no certificate yet")
	}
	return strings.Join(parts, " · ")
}

// linkWifi sends Wi-Fi credentials to a board on USB serial. The password is
// asked for without echo (so it stays out of the shell's history), goes to
// the daemon over the unix socket and on to the board; nothing keeps it.
func linkWifi(args []string) error {
	fs := flag.NewFlagSet("link wifi", flag.ExitOnError)
	ssid := fs.String("ssid", "", "the network's name")
	password := fs.String("password", "", "the password (leave out to be asked; \"\" with --open)")
	open := fs.Bool("open", false, "an open network, no password")
	forget := fs.Bool("forget", false, "make the board forget its Wi-Fi network")
	def := fs.Bool("default", false, "set the default network: boards plugged in over USB get it by themselves")
	forgetDef := fs.Bool("forget-default", false, "clear the default network (boards keep what they have)")
	useDef := fs.Bool("use-default", false, "the board follows the default network again (undoes its own --ssid)")
	var board string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		board, args = args[0], args[1:]
	}
	_ = fs.Parse(args)
	if board == "" && fs.NArg() > 0 {
		board = fs.Arg(0)
	}
	if *forgetDef {
		if err := call("POST", "/local/link/wifi-default", map[string]any{"forget": true}, nil); err != nil {
			return err
		}
		fmt.Println("default network cleared: boards keep the network they have, new ones get none.")
		return nil
	}
	if *useDef {
		if board == "" {
			return errors.New("usage: agentctl link wifi <board> --use-default")
		}
		var res struct {
			Sent    bool
			Message string
		}
		if err := call("POST", "/local/link/wifi", map[string]any{"device": board, "useDefault": true}, &res); err != nil {
			return err
		}
		fmt.Printf("%s follows the default network again: %s.\n", board, res.Message)
		return nil
	}
	if *def && board != "" {
		return errors.New("--default sets the deck's default network, for every board: leave out the board")
	}
	if (!*def && board == "") || (*ssid == "" && !*forget) || (*def && *forget) {
		return errors.New("usage: agentctl link wifi <board> --ssid NAME [--password PW | --open]   or   agentctl link wifi <board> --forget\n" +
			"       agentctl link wifi --default --ssid NAME [--open]   or   agentctl link wifi --forget-default   or   agentctl link wifi <board> --use-default")
	}
	pw := *password
	passwordGiven := false
	fs.Visit(func(f *flag.Flag) { passwordGiven = passwordGiven || f.Name == "password" })
	if !*forget && !*open && !passwordGiven {
		var err error
		if pw, err = askPassword("Wi-Fi password for " + *ssid + ": "); err != nil {
			return err
		}
		if pw == "" {
			return errors.New("no password: for an open network use --open")
		}
	}
	if *open {
		pw = ""
	}
	if !*forget {
		if err := server.CheckWifi(*ssid, pw); err != nil {
			return err
		}
	}
	if *def {
		if err := call("POST", "/local/link/wifi-default", map[string]any{"ssid": *ssid, "password": pw}, nil); err != nil {
			return err
		}
		fmt.Printf("default network %s set (password in the secret store). Approved boards plugged in over USB and paired\n"+
			"for Wi-Fi get it by themselves, unless one has its own (agentctl link wifi <board> --ssid); `agentctl link` shows how it went.\n", *ssid)
		return nil
	}
	body := map[string]any{"device": board, "ssid": *ssid, "password": pw, "forget": *forget}
	if err := call("POST", "/local/link/wifi", body, nil); err != nil {
		return err
	}
	if *forget {
		fmt.Printf("%s forgot its Wi-Fi network: it connects over USB only.\n", board)
		return nil
	}
	fmt.Printf("%s stored the network. Testing it", board)
	// The board tries the network once and reports (NetStatus): wait for it.
	deadline := time.Now().Add(30 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		fmt.Print(".")
		var res linkState
		if call("GET", "/local/link", nil, &res) != nil {
			continue
		}
		for _, b := range res.Boards {
			if b.Board != board || b.Transport != link.OverSerial || b.Net == nil {
				continue
			}
			last = b.Net.Wifi
			switch b.Net.Wifi {
			case "connected", "set, off while on USB":
				if b.Net.IP == "" { // not tested yet
					continue
				}
				fmt.Printf("\nit works: %s.\n", netLine(b.Net))
				fmt.Println("Unplugged from the Mac (on another USB power supply) it now connects over Wi-Fi.")
				return nil
			case "wrong password", "network not found", "failed":
				fmt.Println()
				return fmt.Errorf("the board couldn't join %s: %s. Run agentctl link wifi again", *ssid, b.Net.Wifi)
			}
		}
	}
	fmt.Println()
	if last == "" {
		fmt.Println("the board didn't report back; `agentctl link` shows its Wi-Fi state once it does.")
	} else {
		fmt.Printf("still %s; `agentctl link` shows the outcome.\n", last)
	}
	return nil
}

// askPassword reads a line from the terminal without echoing it (or from
// stdin when it isn't a terminal).
func askPassword(prompt string) (string, error) {
	fi, _ := os.Stdin.Stat()
	tty := fi != nil && fi.Mode()&os.ModeCharDevice != 0
	if tty {
		fmt.Fprint(os.Stderr, prompt)
		off := exec.Command("stty", "-echo")
		off.Stdin = os.Stdin
		if err := off.Run(); err != nil {
			return "", fmt.Errorf("can't hide the password as you type (%v): pass --password", err)
		}
		defer func() {
			on := exec.Command("stty", "echo")
			on.Stdin = os.Stdin
			_ = on.Run()
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
