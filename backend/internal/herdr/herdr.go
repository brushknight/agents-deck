// Package herdr reads the herdr agent multiplexer (herdr.dev) through its
// socket API: the agents in its panes, with the status herdr detects on their
// screens, and the pane actions the deck needs (read, send, focus). Each
// herdr session (the default one and every named one) has its own socket.
package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dir is herdr's config folder, which holds the sockets.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr")
}

// Sockets are the sockets of the herdr sessions that may be running: the
// default session's and each named session's. A socket file can outlive its
// server; calls to it fail.
func Sockets() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(os.Getenv("HERDR_SOCKET_PATH"))
	if p := filepath.Join(Dir(), "herdr.sock"); exists(p) {
		add(p)
	}
	named, _ := filepath.Glob(filepath.Join(Dir(), "sessions", "*", "herdr.sock"))
	sort.Strings(named)
	for _, p := range named {
		add(p)
	}
	return out
}

// SessionName is the herdr session a socket belongs to ("" for the default).
func SessionName(socket string) string {
	if filepath.Base(filepath.Dir(filepath.Dir(socket))) == "sessions" {
		return filepath.Base(filepath.Dir(socket))
	}
	return ""
}

// ClientTTYs are the terminals attached to a herdr session (`herdr`,
// `herdr --session <name>`, `herdr session attach <name>`), newest first.
func ClientTTYs(session string) []string {
	out, err := exec.Command("/bin/ps", "-axww", "-o", "pid=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	return clientTTYs(string(out), session)
}

func clientTTYs(ps, session string) []string {
	type row struct {
		pid int
		tty string
	}
	var rows []row
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || filepath.Base(f[2]) != "herdr" || f[1] == "??" || f[1] == "-" {
			continue
		}
		args := f[3:]
		name, ok := "", true
		switch {
		case len(args) == 0:
		case len(args) >= 2 && args[0] == "--session":
			name = args[1]
		case len(args) >= 3 && args[0] == "session" && args[1] == "attach":
			name = args[2]
		default:
			ok = false // the server, or a CLI command
		}
		if !ok || name != session {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		rows = append(rows, row{pid, "/dev/" + f[1]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].pid > rows[j].pid })
	var ttys []string
	for _, r := range rows {
		ttys = append(ttys, r.tty)
	}
	return ttys
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Agent is one agent record of session.snapshot.
type Agent struct {
	TerminalID    string `json:"terminal_id"` // stable for the terminal's life
	PaneID        string `json:"pane_id"`     // "w1:p2"; changes when panes move
	WorkspaceID   string `json:"workspace_id"`
	Agent         string `json:"agent"`        // claude, codex, …
	Name          string `json:"name"`         // the name it was given in herdr
	Status        string `json:"agent_status"` // idle | working | blocked | done | unknown
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
	Title         string `json:"terminal_title_stripped"`
	Session       *struct {
		Kind  string `json:"kind"`
		Value string `json:"value"` // with herdr's Claude hook installed: the session id
	} `json:"agent_session"`
	StateChangeSeq int64 `json:"state_change_seq"`
}

// Dir is the folder the agent works in.
func (a Agent) Dir() string {
	if a.ForegroundCwd != "" {
		return a.ForegroundCwd
	}
	return a.Cwd
}

// Workspace is one workspace of session.snapshot.
type Workspace struct {
	ID    string `json:"workspace_id"`
	Label string `json:"label"`
}

// Snapshot is a session's state, from one session.snapshot call.
type Snapshot struct {
	Agents     []Agent     `json:"agents"`
	Workspaces []Workspace `json:"workspaces"`
}

// Client talks to one herdr session over its socket.
type Client struct {
	Socket  string
	Timeout time.Duration // per call; default 3 s
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call sends one request (newline-delimited JSON) and decodes its result.
func (c Client) call(out any, method string, params any) error {
	t := c.Timeout
	if t == 0 {
		t = 3 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.Socket, t)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(t))
	req, _ := json.Marshal(map[string]any{"id": "agentsdeck", "method": method, "params": params})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReaderSize(conn, 1<<16).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("herdr %s: %v", method, err)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return fmt.Errorf("herdr %s: %v", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("herdr %s: %s", method, r.Error.Message)
	}
	if r.Result == nil {
		return errors.New("herdr " + method + ": no result")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}

// Read takes a snapshot of the session.
func (c Client) Read() (Snapshot, error) {
	var r struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	err := c.call(&r, "session.snapshot", map[string]any{})
	return r.Snapshot, err
}

// PID is the agent's process in a pane (its foreground process), 0 if none.
func (c Client) PID(pane string) int {
	var r struct {
		Info struct {
			Foreground []struct {
				PID int `json:"pid"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if c.call(&r, "pane.process_info", map[string]any{"pane_id": pane}) != nil || len(r.Info.Foreground) == 0 {
		return 0
	}
	return r.Info.Foreground[0].PID
}

// Screen is the pane's visible screen as text.
func (c Client) Screen(pane string) (string, error) {
	var r struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err := c.call(&r, "pane.read", map[string]any{"pane_id": pane, "source": "visible"})
	return r.Read.Text, err
}

// SendText types text into a pane.
func (c Client) SendText(pane, text string) error {
	return c.call(nil, "pane.send_text", map[string]any{"pane_id": pane, "text": text})
}

// SendKeys presses keys in a pane ("esc", "enter", "ctrl+c" …).
func (c Client) SendKeys(pane string, keys ...string) error {
	return c.call(nil, "pane.send_keys", map[string]any{"pane_id": pane, "keys": keys})
}

// Focus shows the agent's pane in herdr (which marks a finished turn seen).
func (c Client) Focus(pane string) error {
	return c.call(nil, "agent.focus", map[string]any{"target": pane})
}
