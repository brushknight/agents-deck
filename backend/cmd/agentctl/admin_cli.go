package main

// The CLI side of the management API (/admin/v1): everything Settings does
// can be done from a terminal too.
//
//	agentctl system [--json]
//	agentctl settings [<key> <value>]
//	agentctl ports [register|identify|forget <port>]
//	agentctl flash --board <board> [--version v] [--local] <port>
//	agentctl devices rename <id> <name> | update <id> [--version v] [--local]
//	agentctl diagnostics
//	agentctl update (through the service when it runs)

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/admin"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/update"
)

// errOld: the service runs but has no management API (an older version).
var errOld = errors.New("the running service is too old for this: update it (agentctl install restarts it with this agentctl)")

// adminCall is call() for /admin/v1, with a clearer word for a 404 from an
// older service.
func adminCall(method, path string, in, out any) error {
	err := call(method, path, in, out)
	if err != nil && err.Error() == "" { // a 404 without our JSON error: no /admin/v1 there
		return errOld
	}
	return err
}

func systemCmd(args []string) error {
	fs := flag.NewFlagSet("system", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print it as JSON")
	_ = fs.Parse(args)
	var s admin.System
	if err := adminCall("GET", "/admin/v1/system", nil, &s); err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("agents deck %s, %s, installed as %s, running since %s\n", s.Version, s.Platform, s.Install, s.Started.Local().Format("2 Jan 15:04"))
	fmt.Printf("boards:   %d online of %d", s.Boards.Online, s.Boards.Total)
	if s.Boards.Pending > 0 {
		fmt.Printf(", %d waiting for approval (agentctl devices approve <name>)", s.Boards.Pending)
	}
	fmt.Println()
	if s.Ports.New > 0 {
		fmt.Printf("usb:      %d new board(s) plugged in: agentctl ports\n", s.Ports.New)
	}
	upd := map[string]string{"none": "up to date", "available": s.Update.Version + " is out", "ready": s.Update.Version + " is ready to install: agentctl update",
		"installing": "installing " + s.Update.Version, "disabled": "this copy doesn't update itself"}[s.Update.State]
	fmt.Printf("updates:  %s\n", upd)
	if s.FirmwareUpdates > 0 {
		fmt.Printf("firmware: %d board(s) can be updated: agentctl devices update <name>\n", s.FirmwareUpdates)
	}
	return nil
}

// settingsCmd shows every setting, or changes one.
func settingsCmd(args []string) error {
	const usage = "usage: agentctl settings [start-at-login on|off | notifications off|attention|finished | sound on|off |\n" +
		"                         restore ask|auto | sessions pick|auto | updates notify|auto|off | network on|off]"
	if len(args) == 0 {
		var s admin.Settings
		if err := adminCall("GET", "/admin/v1/settings", nil, &s); err != nil {
			return err
		}
		login := "not available here"
		if s.StartAtLogin != nil {
			login = onOff(*s.StartAtLogin)
			if s.LoginNote != "" {
				login += " (" + s.LoginNote + ")"
			}
		}
		net := onOff(s.Network)
		if s.Network != s.NetworkRunning {
			net += " (applies when the service restarts)"
		}
		wifi := s.Wifi.SSID
		if wifi == "" {
			wifi = "none"
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintf(tw, "start-at-login\t%s\n", login)
		fmt.Fprintf(tw, "notifications\t%s\n", s.Notifications)
		fmt.Fprintf(tw, "sound\t%s\n", onOff(s.Sound))
		fmt.Fprintf(tw, "restore\t%s\n", s.Restore)
		fmt.Fprintf(tw, "sessions\t%s\n", s.Sessions)
		fmt.Fprintf(tw, "updates\t%s\n", s.Updates)
		fmt.Fprintf(tw, "network\t%s\n", net)
		fmt.Fprintf(tw, "board wi-fi\t%s\t(agentctl link wifi --default)\n", wifi)
		return tw.Flush()
	}
	if len(args) != 2 {
		return errors.New(usage)
	}
	on := map[string]bool{"on": true, "yes": true, "true": true, "off": false, "no": false, "false": false}
	body := map[string]any{}
	switch args[0] {
	case "start-at-login", "network", "sound":
		v, ok := on[args[1]]
		if !ok {
			return errors.New(usage)
		}
		body[map[string]string{"start-at-login": "startAtLogin", "network": "network", "sound": "sound"}[args[0]]] = v
	case "notifications", "restore", "sessions", "updates":
		body[args[0]] = args[1]
	default:
		return errors.New(usage)
	}
	var res struct {
		Message       string
		RestartNeeded bool
	}
	if err := adminCall("PATCH", "/admin/v1/settings", body, &res); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", args[0], args[1])
	if res.Message != "" {
		fmt.Println(res.Message)
	}
	if res.RestartNeeded {
		fmt.Println("it applies when the service restarts (agentctl install restarts it)")
	}
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// portRef turns a port's path or id into its id.
func portRef(p string) string { return url.PathEscape(filepath.Base(strings.ReplaceAll(p, `\`, "/"))) }

func portsCmd(args []string) error {
	const usage = "usage: agentctl ports [register <port> | identify <port> | forget <port>]"
	if len(args) == 0 {
		var res struct{ Ports []admin.PortRow }
		if err := adminCall("GET", "/admin/v1/ports", nil, &res); err != nil {
			return err
		}
		if len(res.Ports) == 0 {
			fmt.Println("no USB serial ports: plug the board in (with a cable that carries data)")
			return nil
		}
		words := map[string]string{
			admin.PortNew: "new: a likely board, not opened", admin.PortRegistered: "registered, waiting for the board",
			admin.PortConnected: "connected", admin.PortBusy: "busy", admin.PortFlashing: "the service is writing to it",
			admin.PortUnplugged: "registered, not plugged in", admin.PortIgnored: "not a board (you said)", admin.PortOther: "doesn't look like a board",
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "PORT\tUSB ID\tSTATE\tBOARD")
		for _, p := range res.Ports {
			id := "-"
			if p.VID != "" {
				id = p.VID + ":" + p.PID
				if p.LikelyName != "" {
					id += " " + p.LikelyName
				}
			}
			st := words[p.State]
			if p.State == admin.PortBusy && p.Detail != "" {
				st = p.Detail
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Path, id, st, p.Name)
		}
		_ = tw.Flush()
		fmt.Println("\nnothing opens a port until you register it: agentctl ports register <port>")
		return nil
	}
	if len(args) != 2 {
		return errors.New(usage)
	}
	port := portRef(args[1])
	switch args[0] {
	case "register", "add":
		var p admin.PortRow
		if err := adminCall("POST", "/admin/v1/ports/"+port+"/register", nil, &p); err != nil {
			return err
		}
		fmt.Printf("%s registered: the deck opens it within 2 seconds. Next: agentctl ports identify %s\n", p.Path, p.ID)
		return nil
	case "forget", "rm":
		if err := adminCall("POST", "/admin/v1/ports/"+port+"/forget", nil, nil); err != nil {
			return err
		}
		fmt.Printf("%s: forgotten; the deck won't open it or ask about it again\n", args[1])
		return nil
	case "identify":
		fmt.Println("asking the board… (if nothing happens: hold BOOT, press and release RST, let go of BOOT)")
		var id admin.Identity
		if err := call("POST", "/admin/v1/ports/"+port+"/identify", nil, &id); err != nil {
			return err
		}
		printIdentity(id, args[1])
		return nil
	}
	return errors.New(usage)
}

func printIdentity(id admin.Identity, port string) {
	if id.Source == "session" && id.Device != nil {
		d := id.Device
		fmt.Printf("%s runs agents deck firmware %s (%s, %s)\n", d.Name, d.Firmware, d.Type, d.ID)
		if id.Board != nil && id.Latest != "" {
			fmt.Printf("the latest firmware for %s is %s: agentctl flash --board %s %s\n", id.Board.Name, id.Latest, id.Board.Board, port)
		}
		return
	}
	fl := "unknown flash size"
	if id.FlashSize > 0 {
		fl = fmt.Sprintf("%d MB flash", id.FlashSize>>20)
	}
	fmt.Printf("%s, %s, no agents deck firmware on it yet\n", id.Chip, fl)
	if id.Note != "" {
		fmt.Println(id.Note)
	}
	if len(id.Local) > 0 {
		fmt.Println("built on this computer for this chip:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		for _, b := range id.Local {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", b.Board, b.Name, b.Version)
		}
		_ = tw.Flush()
		fmt.Printf("install with: agentctl flash --local --board <board> %s\n", port)
	}
	if len(id.Candidates) > 0 && id.ReleaseKey {
		fmt.Println("boards it could be:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		for _, b := range id.Candidates {
			wifi := "USB only"
			if b.Wifi {
				wifi = "Wi-Fi"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", b.Board, b.Name, wifi)
		}
		_ = tw.Flush()
		fmt.Printf("install with: agentctl flash --board <board> %s\n", port)
	}
	if !id.ReleaseKey && len(id.Local) == 0 {
		fmt.Println("this copy of agents deck was built from source, so it can't check firmware downloaded from a release and won't install it.\n" +
			"firmware built on this computer installs in one click: agentctl firmware add <folder>, then agentctl flash --local --board <board> " + port + "\n" +
			"or write an image by hand: agentctl flash " + port + " <image.bin>")
	}
}

// flashBoardCmd: agentctl flash --board <board> [--version v] <port>, done by
// the service (it downloads and checks the image, borrows the port, writes
// it and hands the port back so the board pairs).
func flashBoardCmd(args []string) error {
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	board := fs.String("board", "", "the board, as agentctl ports identify or agentctl firmware ls names it")
	ver := fs.String("version", "", "the firmware version (default: the latest release)")
	local := fs.Bool("local", false, "install the build made on this computer (agentctl firmware add), not a release's")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	_ = fs.Parse(args)
	pos = append(pos, fs.Args()...)
	if *board == "" || len(pos) != 1 {
		return errors.New("usage: agentctl flash --board <board> [--version v] [--local] <port>   (the port must be registered: agentctl ports)")
	}
	body := localBody(*local, *ver)
	body["board"] = *board
	var job admin.Job
	if err := adminCall("POST", "/admin/v1/ports/"+portRef(pos[0])+"/flash", body, &job); err != nil {
		return err
	}
	return followJob(job)
}

// followJob prints a job's progress until it ends; Ctrl-C stops it.
func followJob(job admin.Job) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	last := ""
	tty := isTerminal()
	for {
		line := job.Message
		if job.Total > 0 && (job.Stage == "write" || job.Stage == "verify") {
			line = fmt.Sprintf("%-28s %3d%%  %.1f / %.1f MB", job.Message, job.Percent, float64(job.Done)/(1<<20), float64(job.Total)/(1<<20))
		}
		if line != last {
			if tty {
				fmt.Printf("\r\033[K%s", line)
			} else {
				fmt.Println(line)
			}
			last = line
		}
		if job.State != admin.JobRunning {
			break
		}
		select {
		case <-ctx.Done():
			_ = call("POST", "/admin/v1/jobs/"+job.ID+"/cancel", nil, nil)
			stop()
			ctx = context.Background()
		case <-time.After(300 * time.Millisecond):
		}
		if err := call("GET", "/admin/v1/jobs/"+job.ID, nil, &job); err != nil {
			if tty {
				fmt.Println()
			}
			return err
		}
	}
	if tty {
		fmt.Println()
	}
	switch job.State {
	case admin.JobDone:
		for _, s := range job.Steps {
			fmt.Println("  " + s)
		}
		return nil
	case admin.JobCancelled:
		return errors.New("stopped")
	}
	if job.BootHelp {
		return fmt.Errorf("%s\n(hold BOOT, press and release RST, let go of BOOT, then run it again)", job.Error)
	}
	return errors.New(job.Error)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// devicesAdminCmd: agentctl devices rename <id> <name> | update <id> [--version v].
func devicesAdminCmd(args []string) error {
	switch {
	case args[0] == "rename" && len(args) >= 3:
		name := strings.Join(args[2:], " ")
		if err := adminCall("POST", "/admin/v1/devices/"+url.PathEscape(args[1])+"/rename", map[string]string{"name": name}, nil); err != nil {
			return err
		}
		fmt.Printf("%s is now called %q (its id stays %s)\n", args[1], name, args[1])
		return nil
	case args[0] == "update" && len(args) >= 2:
		fs := flag.NewFlagSet("devices update", flag.ExitOnError)
		ver := fs.String("version", "", "the firmware version (default: the newest, a release's or one built on this computer)")
		local := fs.Bool("local", false, "install the build made on this computer (agentctl firmware add), not a release's")
		_ = fs.Parse(args[2:])
		var job admin.Job
		if err := adminCall("POST", "/admin/v1/devices/"+url.PathEscape(args[1])+"/update", localBody(*local, *ver), &job); err != nil {
			return err
		}
		return followJob(job)
	}
	return errors.New("usage: agentctl devices rename <name> <new name> | update <name> [--version v] [--local]")
}

func diagnosticsCmd(args []string) error {
	var d admin.Diagnostics
	if err := adminCall("GET", "/admin/v1/diagnostics", nil, &d); err != nil {
		return err
	}
	fmt.Print(d.Report)
	return nil
}

// cpuCmd: agentctl cpu [--json], how much CPU the service and its helpers
// use (the budget: under 2% of one core, idle).
func cpuCmd(args []string) error {
	fs := flag.NewFlagSet("cpu", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print it as JSON")
	_ = fs.Parse(args)
	var r proc.CPUReport
	if err := adminCall("GET", "/admin/v1/cpu", nil, &r); err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("budget: %.0f%% of one core, idle\n", r.Budget)
	for _, s := range []struct {
		name string
		span *proc.CPUSpan
	}{{"last minute", r.Last1}, {"last 10 min", r.Last10}, {"last hour", r.Last60}, {"since start", r.Since}} {
		if s.span == nil {
			continue
		}
		fmt.Printf("%-12s %5.1f%%  (service %.1f%%, helpers %.1f%%)\n", s.name, s.span.Percent, s.span.Service, s.span.Children)
	}
	top := r.Last10
	if top == nil {
		top = r.Since
	}
	if top != nil && len(top.Helpers) > 0 {
		fmt.Printf("\nhelpers over the %.0f min:\n", top.Minutes)
		for _, h := range top.Helpers {
			fmt.Printf("  %-14s %5.1f%%  %d runs\n", h.Program, h.Percent, h.Runs)
		}
	}
	return nil
}

// updateViaService runs agentctl update through the running service, so the
// CLI, the tray app and Settings share one download and one state. It
// reports false when no service answers (then the CLI updates by itself).
func updateViaService(check, yes bool) (bool, error) {
	var st admin.UpdateView
	resp, err := unixClient(2 * time.Second).Get("http://agentd/admin/v1/update")
	if err != nil {
		return false, nil
	}
	resp.Body.Close()
	if resp.StatusCode == 404 {
		return false, nil // an older service: do it here
	}
	fmt.Println("checking through the service…")
	if err := call("POST", "/admin/v1/update/check", nil, &st); err != nil {
		return true, err
	}
	printUpdate(st.State)
	if check || (st.State.State != update.StateAvailable && st.State.State != update.StateReady) || st.Install == update.KindDeb || st.Message != "" {
		return true, nil
	}
	if !yes {
		fmt.Printf("Install agents deck %s now? The service restarts; agents keep running. [y/N] ", st.Version)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Println("not installed")
			return true, nil
		}
	}
	var job admin.Job
	if err := call("POST", "/admin/v1/update/apply", nil, &job); err != nil {
		return true, err
	}
	err = followJob(job)
	if errors.Is(err, errNotRunning) || errors.Is(err, errUnreachable) {
		fmt.Println("the service is restarting with the new version")
		return true, nil
	}
	return true, err
}
