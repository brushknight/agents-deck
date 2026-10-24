// Package tmux drives the private tmux server that hosts every agent
// (`tmux -L agentsterm`), so agents never mix with the user's own tmux.
package tmux

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var Socket = "agentsterm" // a variable only so tests can use their own server

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
		{"set-option", "-t", name, "history-limit", "50000"},
	} {
		_, _ = run(o...)
	}
	ServerDefaults()
	return nil
}

// pbcopy puts tmux selections on the macOS clipboard.
const pbcopy = "/usr/bin/pbcopy"

// NativeMouse reports whether the terminal, not tmux, should own the mouse
// (agentctl set mouse); the daemon wires it to config.json.
var NativeMouse = func() bool { return false }

// TitleFormat is the terminal tab title of an agent pane. (No ":" or "/"
// in the patterns: tmux would split the format there.)
const TitleFormat = "#{?#{m/r:[A-Za-z0-9],#{pane_title}},#{s/^[^A-Za-z0-9]+ //:pane_title},#{b:pane_current_path}}"

// ServerDefaults sets the server-wide options and key bindings of our private
// server (a no-op while it isn't running; NewSession applies them again).
//
// Mouse, native (default): tmux mouse mode is off, so iTerm itself selects,
// copies, ⌘-clicks links and scrolls. Hyperlinks (OSC 8, which Claude Code
// prints) are passed through, and tmux skips the alternate screen so what
// scrolls off lands in iTerm's own scrollback. Terminal features and
// overrides are read when a client attaches: reattach to pick them up.
//
// Mouse, tmux (default): tmux mouse mode is on (the wheel scrolls tmux
// history). A drag selection stays highlighted and goes to the clipboard; a
// click clears it, and typing leaves copy mode with the key passed on to the
// agent. iTerm never sees the clicks, so tmux opens links itself: a plain
// click on a hyperlink or a URL runs `agentctl open-link`.
func ServerDefaults() {
	native := NativeMouse()
	mouse, screen := "on", []string{"set-option", "-su", "terminal-overrides[90]"}
	// Hyperlinks (OSC 8) reach iTerm only when iTerm owns the mouse (⌘-click).
	// tmux re-sends them on every redraw, and iTerm keeps a mark for each
	// one. On tmux's alternate screen nothing scrolls away, so the marks
	// never go and iTerm walks all of them on every frame: after a day or
	// two it stops responding. Leaving "hyperlinks" out of terminal-features
	// is not enough: tmux turns it on by itself when iTerm answers its
	// version query, so the link capability (Hls) is removed instead; tmux
	// applies overrides after what it detects. With tmux's mouse, clicks
	// open links from tmux's own record of them.
	links := [][]string{
		{"set-option", "-su", "terminal-features[90]"},
		{"set-option", "-s", "terminal-overrides[91]", "*:Hls@"},
	}
	if native {
		mouse, screen = "off", []string{"set-option", "-s", "terminal-overrides[90]", "*:smcup@:rmcup@"}
		links = [][]string{
			{"set-option", "-s", "terminal-features[90]", "*:hyperlinks"},
			{"set-option", "-su", "terminal-overrides[91]"},
		}
	}
	self, _ := os.Executable()
	cmds := [][]string{
		{"set-option", "-g", "mouse", mouse},
		// Words end at spaces, quotes and brackets: a URL or a path is one
		// word (double-click selects it whole, a click on it opens it).
		{"set-option", "-g", "word-separators", ` "'()<>[]{}|` + "`"},
		{"bind-key", "-n", "MouseUp1Pane", `if-shell -F "#{mouse_any_flag}" { send-keys -M } { run-shell -b "'` + self + `' open-link --link=#{q:mouse_hyperlink} --word=#{q:mouse_word}" }`},
		screen,
		// The terminal tab shows the agent's name as Claude Code sets it,
		// not just "tmux", without its leading status glyph ("✳ CMM server"
		// is "CMM server": a narrow tab showed only the ✳), and the folder
		// while the pane has no name yet.
		{"set-option", "-g", "set-titles", "on"},
		{"set-option", "-g", "set-titles-string", TitleFormat},
		{"set-option", "-g", "escape-time", "10"},
		{"set-option", "-g", "focus-events", "on"}, // lets us see which tab has focus
		{"bind-key", "-n", "C-q", "detach-client"},
		{"bind-key", "-n", "DoubleClick1Pane", `select-pane -t = ; if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -H ; send-keys -X select-word ; send-keys -X copy-pipe-no-clear ` + pbcopy + ` }`},
		{"bind-key", "-n", "TripleClick1Pane", `select-pane -t = ; if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -H ; send-keys -X select-line ; send-keys -X copy-pipe-no-clear ` + pbcopy + ` }`},
	}
	cmds = append(cmds, links...)
	for _, table := range []string{"copy-mode", "copy-mode-vi"} {
		cmds = append(cmds,
			[]string{"bind-key", "-T", table, "MouseDragEnd1Pane", "send-keys -X copy-pipe-no-clear " + pbcopy},
			[]string{"bind-key", "-T", table, "DoubleClick1Pane", "select-pane ; send-keys -X select-word ; send-keys -X copy-pipe-no-clear " + pbcopy},
			[]string{"bind-key", "-T", table, "TripleClick1Pane", "select-pane ; send-keys -X select-line ; send-keys -X copy-pipe-no-clear " + pbcopy},
		)
		for _, k := range passThroughKeys() {
			cmds = append(cmds, []string{"bind-key", "-T", table, k.name, "send-keys -X cancel ; send-keys " + k.send})
		}
	}
	// Sessions made by older versions set mouse per session: follow the global.
	for name := range Sessions() {
		cmds = append(cmds, []string{"set-option", "-u", "-t", name, "mouse"})
	}
	if err := Batch(cmds); err != nil {
		log.Printf("tmux defaults: %v", err)
	}
}

// Batch runs tmux commands in a few calls, joined with ";" (one call has a
// size limit). It returns the last error.
func Batch(cmds [][]string) error {
	var last error
	for len(cmds) > 0 {
		n := min(len(cmds), 40)
		var args []string
		for i, c := range cmds[:n] {
			if i > 0 {
				args = append(args, ";")
			}
			args = append(args, c...)
		}
		if _, err := run(args...); err != nil {
			last = describe(err)
		}
		cmds = cmds[n:]
	}
	return last
}

type passKey struct{ name, send string }

// passThroughKeys are the typing keys that, in copy mode, leave it and reach
// the agent: printable ASCII (sent as hex, so nothing needs quoting) plus
// Space, Enter, Tab and Backspace. Arrows, Page Up/Down and the wheel keep
// scrolling the history; Escape and C-c still just leave copy mode.
func passThroughKeys() []passKey {
	keys := []passKey{{"Space", "Space"}, {"Enter", "Enter"}, {"Tab", "Tab"}, {"BSpace", "BSpace"}}
	for c := byte('!'); c <= '~'; c++ {
		name := string(c)
		if c == ';' {
			name = `\;` // a bare ";" separates commands
		}
		keys = append(keys, passKey{name, fmt.Sprintf("-H %02x", c)})
	}
	return keys
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

// PaneTTYs are the terminals of every pane on our server (agentctl's agents).
func PaneTTYs() map[string]bool {
	out, _ := run("list-panes", "-a", "-F", "#{pane_tty}")
	ttys := map[string]bool{}
	for _, l := range strings.Fields(out) {
		ttys[l] = true
	}
	return ttys
}

// ServerRunning reports whether our tmux server is up. (It exits by itself
// once its last session ends.)
func ServerRunning() bool {
	_, err := run("list-sessions", "-F", "#{session_name}")
	return err == nil
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

// PanePID is the process running in the session's pane (0 when unknown).
func PanePID(name string) int {
	out, err := run("list-panes", "-t", name, "-F", "#{pane_pid}")
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]))
	return pid
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
