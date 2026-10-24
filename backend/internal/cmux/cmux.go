// Package cmux reads cmux (cmux.com), a macOS terminal built for coding
// agents. The Claude sessions in its terminals, with their state, come from
// the file cmux's Claude hooks keep (no access needed). The terminal actions
// the deck needs (read the screen, type, press keys, focus) go through its
// control socket.
//
// cmux lets only programs it started use the socket, unless its Socket
// Control Mode is Automation mode (Settings › Automation, or
// "socketControlMode": "automation" in ~/.config/cmux/cmux.json). The deck
// runs outside cmux, so it needs that setting for the actions.
package cmux

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
	"strings"
	"syscall"
	"time"
)

// StateDir is where cmux keeps its state files.
func StateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cmuxterm")
}

// Session is a Claude session running in a cmux terminal, as cmux's Claude
// hooks last reported it.
type Session struct {
	ID         string  `json:"sessionId"`
	Surface    string  `json:"surfaceId"` // the terminal it runs in
	PID        int     `json:"pid"`
	Cwd        string  `json:"cwd"`
	Transcript string  `json:"transcriptPath"`
	Lifecycle  string  `json:"agentLifecycle"` // idle | running | needsInput
	Body       string  `json:"lastBody"`       // its last notification ("Do you prefer red or blue?\n[Red] [Blue]")
	Updated    float64 `json:"updatedAt"`      // seconds; changes with every hook event
}

// Sessions are the Claude sessions running in cmux terminals now: the ones
// each terminal last started, whose process is still alive.
func Sessions() ([]Session, error) {
	b, err := os.ReadFile(filepath.Join(StateDir(), "claude-hook-sessions.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sessions(b, alive)
}

func sessions(b []byte, alive func(pid int) bool) ([]Session, error) {
	var f struct {
		BySurface map[string]struct {
			SessionID string `json:"sessionId"`
		} `json:"activeSessionsBySurface"`
		Sessions map[string]Session `json:"sessions"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("cmux sessions: %v", err)
	}
	var out []Session
	for surface, a := range f.BySurface {
		s, ok := f.Sessions[a.SessionID]
		if !ok || s.ID == "" || s.PID <= 0 || !alive(s.PID) {
			continue
		}
		s.Surface = surface
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Surface < out[j].Surface })
	return out, nil
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Socket is the control socket of the cmux that's running (it may not be).
func Socket() string {
	if p := os.Getenv("CMUX_SOCKET_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "state", "cmux")
	if b, err := os.ReadFile(filepath.Join(dir, "last-socket-path")); err == nil {
		if p := strings.TrimSpace(string(b)); filepath.IsAbs(p) {
			return p
		}
	}
	return filepath.Join(dir, "cmux.sock")
}

// ErrDenied: cmux refused the deck (its Socket Control Mode isn't Automation mode).
var ErrDenied = errors.New("cmux lets only its own terminals use its socket: in cmux, set Settings › Automation › Socket Control Mode to Automation mode")

// Client talks to cmux over its socket.
type Client struct {
	Socket  string
	Timeout time.Duration // per call; default 3 s
}

type reply struct {
	OK     bool            `json:"ok"`
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
	line, err := bufio.NewReaderSize(conn, 1<<20).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("cmux %s: %v", method, err)
	}
	return decode(line, out, method)
}

func decode(line []byte, out any, method string) error {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "{") { // a refusal comes as plain text
		if strings.Contains(strings.ToLower(text), "access denied") {
			return ErrDenied
		}
		return fmt.Errorf("cmux %s: %s", method, text)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return fmt.Errorf("cmux %s: %v", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("cmux %s: %s", method, r.Error.Message)
	}
	if !r.OK || r.Result == nil {
		return errors.New("cmux " + method + ": no result")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}

// Screen is the terminal's visible screen as text.
func (c Client) Screen(surface string) (string, error) {
	var r struct {
		Text string `json:"text"`
	}
	err := c.call(&r, "surface.read_text", map[string]any{"surface_id": surface})
	return r.Text, err
}

// SendText types text into a terminal.
func (c Client) SendText(surface, text string) error {
	return c.call(nil, "surface.send_text", map[string]any{"surface_id": surface, "text": text})
}

// SendKey presses a key in a terminal ("escape", "enter", "down" …).
func (c Client) SendKey(surface, key string) error {
	return c.call(nil, "surface.send_key", map[string]any{"surface_id": surface, "key": key})
}

// Focus shows the terminal in its cmux window and brings cmux to the front.
func (c Client) Focus(surface string) error {
	if err := c.call(nil, "surface.focus", map[string]any{"surface_id": surface}); err != nil {
		return err
	}
	var id struct {
		App string `json:"app_bundle_path"`
	}
	if c.call(&id, "system.identify", map[string]any{}) != nil || !strings.HasSuffix(id.App, ".app") {
		id.App = "/Applications/cmux.app"
	}
	return exec.Command("/usr/bin/open", "-a", id.App).Run()
}
