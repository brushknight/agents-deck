package main

// agentctl install, uninstall and status: the service that runs at login
// (launchd on macOS, systemd on Linux, a startup entry on Windows). The
// platform code is in internal/service.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/proc"
	"github.com/brushknight/agents-deck/backend/internal/service"
)

func serviceManager() (service.Manager, string, error) {
	exe, err := service.Executable()
	if err != nil {
		return nil, "", err
	}
	home, _ := os.UserHomeDir()
	return service.New(service.Config{Exe: exe, Home: home, Data: paths.Data(), Log: paths.Log(), PIDFile: paths.PID(), Exit: stopServe}), exe, nil
}

// serveStop is closed to shut serve down cleanly: the Windows service does
// it to restart or stop itself from Settings (see service.Startup).
var (
	serveStop     = make(chan struct{})
	serveStopOnce sync.Once
)

func stopServe() { serveStopOnce.Do(func() { close(serveStop) }) }

// install registers the service to start at login and starts it now.
func install() error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	m, _, err := serviceManager()
	if err != nil {
		return err
	}
	noteInServiceLog("agentctl install: (re)starting the service")
	return m.Install(os.Stdout)
}

// noteInServiceLog adds a line to the service's log saying what this
// command is about to do to the service and which program ran it, so a
// restart nobody remembers can be traced.
func noteInServiceLog(what string) {
	f, err := os.OpenFile(paths.Log(), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	by := fmt.Sprintf("pid %d", os.Getppid())
	if runtime.GOOS != "windows" {
		if out, err := proc.Output(2*time.Second, "/bin/ps", "-o", "comm=", "-p", strconv.Itoa(os.Getppid())); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				by = filepath.Base(name) + ", " + by
			}
		}
	}
	fmt.Fprintf(f, "%s %s (run by %s)\n", time.Now().Format("2006/01/02 15:04:05"), what, by)
}

// uninstall stops the service and removes it from login. The data folder
// (devices, certificates, settings) stays.
func uninstall() error {
	m, _, err := serviceManager()
	if err != nil {
		return err
	}
	noteInServiceLog("agentctl uninstall: stopping the service")
	if err := m.Uninstall(os.Stdout); err != nil {
		return err
	}
	fmt.Println("your settings, devices and certificates stay in", paths.Data())
	return nil
}

// serviceStatus is `agentctl status --json`.
type serviceStatus struct {
	Version   string               `json:"version"`  // this agentctl
	Platform  string               `json:"platform"` // e.g. windows/amd64
	Agentctl  string               `json:"agentctl"`
	Running   bool                 `json:"running"`
	Service   string               `json:"service,omitempty"` // the running service's version
	Login     service.Registration `json:"login"`
	Data      string               `json:"data"`
	Socket    string               `json:"socket"`
	Log       string               `json:"log"`
	Config    string               `json:"config"`
	Dashboard string               `json:"dashboard"`
}

var errNotRunning = errors.New("the service is not running: start it with agentctl install (or agentctl serve)")

// status says whether the service is registered and running, which version,
// and where its files are. It exits 0 only when the service answers.
func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print it as JSON")
	_ = fs.Parse(args)
	m, exe, err := serviceManager()
	if err != nil {
		return err
	}
	st := serviceStatus{Version: version, Platform: runtime.GOOS + "/" + runtime.GOARCH, Agentctl: exe,
		Login: m.Registration(), Data: paths.Data(), Socket: paths.Socket(), Log: paths.Log(),
		Config: paths.Config(), Dashboard: "http://" + loadConfig().LocalAddr}
	if s, err := quickState(); err == nil {
		st.Running, st.Service = true, s.Server.Version
	}
	if *asJSON {
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
	} else {
		printStatus(st)
	}
	if !st.Running {
		if *asJSON {
			os.Exit(1) // the JSON says why; no message after it
		}
		return errNotRunning
	}
	return nil
}

func printStatus(st serviceStatus) {
	yes := map[bool]string{true: "yes", false: "no"}
	running := yes[st.Running]
	if st.Running {
		running += ", version " + st.Service
		if st.Service != st.Version {
			running += " (this agentctl is " + st.Version + ": agentctl install restarts the service with it)"
		}
	}
	login := yes[st.Login.Registered] + ", " + st.Login.Kind
	if st.Login.Where != "" {
		login += " (" + st.Login.Where + ")"
	}
	fmt.Printf("running:     %s\n", running)
	fmt.Printf("at login:    %s\n", login)
	if st.Login.Note != "" {
		fmt.Printf("             %s\n", st.Login.Note)
	}
	fmt.Printf("agentctl:    %s (%s, %s)\n", st.Agentctl, st.Version, st.Platform)
	fmt.Printf("dashboard:   %s (agentctl web logs you in)\n", st.Dashboard)
	fmt.Printf("data folder: %s\n", st.Data)
	fmt.Printf("socket:      %s\n", st.Socket)
	fmt.Printf("log:         %s\n", st.Log)
	fmt.Printf("config:      %s\n", st.Config)
}

// serviceLog sends the service's log to file (serve --log FILE): on
// Windows nothing else keeps a windowless program's output.
func serviceLog(file string) error {
	if file == "" {
		return nil
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	log.SetOutput(f)
	return nil
}

// servicePID records the running service's process id (paths.PID) for
// agentctl uninstall and install on Windows; the function it returns
// removes it on a clean stop.
func servicePID() func() {
	remove, err := service.WritePID(paths.PID())
	if err != nil {
		log.Printf("pid file: %v", err)
	}
	return remove
}

// quickState asks the service for its state, giving up after two seconds.
func quickState() (model.State, error) {
	var s model.State
	resp, err := unixClient(2 * time.Second).Get("http://agentd/v1/state")
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return s, errors.New(resp.Status)
	}
	return s, json.NewDecoder(resp.Body).Decode(&s)
}
