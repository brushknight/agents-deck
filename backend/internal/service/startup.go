package service

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// RunKey is where Windows keeps the user's startup entries, and RunValue the
// name of ours (Task Manager's Startup apps lists it).
const (
	RunKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	RunValue = "agents deck"
)

// Registry is the user's Run key: read, write and remove one value.
type Registry interface {
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
	Delete(name string) error
}

// Startup starts the service at sign-in on Windows with a startup entry in
// the user's Run key. agentctl is a console program, so the entry runs it
// through conhost.exe --headless: the service starts without a console
// window. Install also starts it right away, Uninstall stops it.
type Startup struct {
	Exe, Log   string
	SystemRoot string // %SystemRoot%, e.g. C:\Windows
	Reg        Registry
	// Start runs argv in the background, with no window, detached from this
	// console; Stop ends the running service (false when none was running).
	Start func(argv []string) error
	Stop  func() (bool, error)
}

// ServeArgv is the service's command: agentctl serve, logging to Log.
func (s *Startup) ServeArgv() []string { return []string{s.Exe, "serve", "--log", s.Log} }

// Command is the startup entry: the command line Windows runs at sign-in.
func (s *Startup) Command() string {
	conhost := s.SystemRoot + `\System32\conhost.exe`
	return winJoin(append([]string{conhost, "--headless"}, s.ServeArgv()...))
}

// winJoin joins arguments into a Windows command line (the reverse of
// proc.SplitCommandLine): quotes around those with spaces or quotes,
// backslashes doubled only before a quote.
func winJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = winQuote(a)
	}
	return strings.Join(out, " ")
}

func winQuote(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\"") {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(a); i++ {
		switch c := a[i]; c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteByte(a[i])
	}
	b.WriteString(strings.Repeat(`\`, slashes)) // before the closing quote
	b.WriteByte('"')
	return b.String()
}

// Install writes the startup entry, stops a service that is already running
// (an older agentctl, say) and starts this one.
func (s *Startup) Install(out io.Writer) error {
	if s.SystemRoot == "" {
		return errors.New("SystemRoot is not set: can't find conhost.exe")
	}
	if err := s.Reg.Set(RunValue, s.Command()); err != nil {
		return fmt.Errorf("startup entry: %w", err)
	}
	restarted, err := s.Stop()
	if err != nil {
		return fmt.Errorf("stopping the running service: %w", err)
	}
	if err := s.Start(s.ServeArgv()); err != nil {
		return fmt.Errorf("starting the service: %w", err)
	}
	if restarted {
		fmt.Fprintln(out, "restarted the service; it starts when you sign in:", RunKey+`\`+RunValue)
	} else {
		fmt.Fprintln(out, "installed and started the service; it starts when you sign in:", RunKey+`\`+RunValue)
	}
	return nil
}

// Uninstall stops the service and removes the startup entry.
func (s *Startup) Uninstall(out io.Writer) error {
	stopped, err := s.Stop()
	if err != nil {
		return fmt.Errorf("stopping the service: %w", err)
	}
	_, registered, err := s.Reg.Get(RunValue)
	if err != nil {
		return fmt.Errorf("startup entry: %w", err)
	}
	if registered {
		if err := s.Reg.Delete(RunValue); err != nil {
			return fmt.Errorf("startup entry: %w", err)
		}
	}
	switch {
	case registered:
		fmt.Fprintln(out, "stopped and removed the startup entry", RunKey+`\`+RunValue)
	case stopped:
		fmt.Fprintln(out, "stopped the service (it had no startup entry)")
	default:
		fmt.Fprintln(out, "not installed: nothing to remove")
	}
	return nil
}

// Registration: the startup entry, and whether it runs this agentctl.
func (s *Startup) Registration() Registration {
	r := Registration{Kind: "startup entry", Where: RunKey + `\` + RunValue}
	v, ok, err := s.Reg.Get(RunValue)
	if err != nil {
		r.Note = err.Error()
		return r
	}
	r.Registered = ok
	if ok && !strings.EqualFold(v, s.Command()) {
		r.Note = "the entry runs another agentctl: " + v + " (agentctl install updates it)"
	}
	return r
}
