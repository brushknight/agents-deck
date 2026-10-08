// Package tmux drives the private tmux server that hosts every agent
// (`tmux -L agentsterm`), so agents never mix with the user's own tmux.
package tmux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Socket = "agentsterm"

// Bin is the tmux binary; launchd's PATH is minimal, so look in the usual places.
var Bin = func() string {
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "tmux"
}()

func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, Bin, append([]string{"-L", Socket}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

// NewSession starts a detached session running argv in dir with extra env.
func NewSession(name, dir string, env []string, argv []string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, argv...)
	if _, err := run(args...); err != nil {
		return describe(err)
	}
	// Quiet, agent-friendly defaults on our private server only.
	for _, o := range [][]string{
		{"set-option", "-t", name, "status", "off"},
		{"set-option", "-t", name, "mouse", "on"},
		{"set-option", "-t", name, "history-limit", "50000"},
		{"set-option", "-g", "escape-time", "10"},
		{"set-option", "-g", "focus-events", "on"}, // lets us see which tab has focus
		{"bind-key", "-n", "C-q", "detach-client"},
	} {
		_, _ = run(o...)
	}
	return nil
}

func describe(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return errors.New(strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}

// Sessions returns the names of live sessions (empty when the server is down).
func Sessions() map[string]bool {
	out, err := run("list-sessions", "-F", "#{session_name}")
	live := map[string]bool{}
	if err != nil {
		return live
	}
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			live[l] = true
		}
	}
	return live
}

// Client is one terminal attached to a session.
type Client struct {
	Session  string
	TTY      string // e.g. /dev/ttys012
	Activity int64  // unix time of the last input/output
	Focused  bool   // the terminal reported focus (needs focus-events on)
}

// ClientList returns every attached client, most recently active first.
func ClientList() []Client {
	// Space-separated: without a UTF-8 locale (launchd) tmux turns tabs into "_".
	// None of these fields can contain a space.
	out, err := run("list-clients", "-F", "#{session_name} #{client_tty} #{client_activity} #{client_flags}")
	if err != nil {
		return nil
	}
	var cs []Client
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 4 {
			continue
		}
		act, _ := strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, Client{Session: f[0], TTY: f[1], Activity: act, Focused: strings.Contains(","+f[3]+",", ",focused,")})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Activity > cs[j].Activity })
	return cs
}

// SetGlobal sets a server-wide option on our tmux server.
func SetGlobal(option, value string) error {
	_, err := run("set-option", "-g", option, value)
	return err
}

// SwitchClient makes the terminal on tty show another session.
func SwitchClient(tty, session string) error {
	if _, err := run("switch-client", "-c", tty, "-t", session); err != nil {
		return describe(err)
	}
	return nil
}

// SendKeys types keys into the session; literal text is sent with -l.
func SendKeys(name string, literal string, keys ...string) error {
	if literal != "" {
		if _, err := run("send-keys", "-t", name, "-l", literal); err != nil {
			return describe(err)
		}
	}
	if len(keys) > 0 {
		if _, err := run(append([]string{"send-keys", "-t", name}, keys...)...); err != nil {
			return describe(err)
		}
	}
	return nil
}

// Capture returns the visible pane text plus `back` lines of history.
func Capture(name string, back int) string {
	out, _ := run("capture-pane", "-p", "-J", "-t", name, "-S", "-"+itoa(back))
	return out
}

func Kill(name string) error { _, err := run("kill-session", "-t", name); return err }

// AttachArgv is the command a terminal runs to show the session.
func AttachArgv(name string) []string {
	return []string{Bin, "-L", Socket, "attach-session", "-t", name}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
