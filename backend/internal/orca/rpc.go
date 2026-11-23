package orca

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// The Orca app serves its CLI over a local socket named in its runtime file,
// with a token (newline-delimited JSON: one request, one answer, keepalive
// frames in between). The deck polls through that socket itself: the CLI is
// the Orca app (Electron) starting up, and running it twice every 2 seconds
// cost several percent of a core. The CLI stays for everything else, and as
// the fallback when the socket can't be used (Windows' named pipe, an Orca
// that changed its protocol).

// errTransport: the socket couldn't be used (use the CLI instead).
var errTransport = errors.New("orca: no usable runtime socket")

type runtimeMeta struct {
	RuntimeID  string `json:"runtimeId"`
	AuthToken  string `json:"authToken"`
	Transports []struct {
		Kind     string `json:"kind"`
		Endpoint string `json:"endpoint"`
	} `json:"transports"`
}

type rpcAnswer struct {
	envelope
	ID        string `json:"id"`
	Keepalive bool   `json:"_keepalive"`
	Meta      struct {
		RuntimeID string `json:"runtimeId"`
	} `json:"_meta"`
}

// rpc calls method on the running Orca app and decodes its result into out.
// Errors wrap errTransport when the socket itself failed.
func (c Client) rpc(out any, method string, params map[string]any) error {
	b, err := os.ReadFile(RuntimeFile())
	if err != nil {
		return fmt.Errorf("%w: %v", errTransport, err)
	}
	var m runtimeMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("%w: runtime file: %v", errTransport, err)
	}
	endpoint := ""
	for _, t := range m.Transports {
		if t.Kind == "unix" {
			endpoint = t.Endpoint
		}
	}
	if endpoint == "" || m.AuthToken == "" {
		return errTransport
	}
	t := c.Timeout
	if t == 0 {
		t = 5 * time.Second
	}
	conn, err := net.DialTimeout("unix", endpoint, t)
	if err != nil {
		return fmt.Errorf("%w: %v", errTransport, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(t))
	id := newID()
	if params == nil {
		params = map[string]any{}
	}
	req, _ := json.Marshal(map[string]any{"id": id, "authToken": m.AuthToken, "method": method, "params": params})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("%w: %v", errTransport, err)
	}
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("%w: %v", errTransport, err)
		}
		var a rpcAnswer
		if json.Unmarshal(line, &a) != nil {
			if len(line) > 1 {
				return fmt.Errorf("%w: unreadable answer", errTransport)
			}
			continue // a blank line
		}
		if a.Keepalive {
			continue
		}
		if a.ID != id || (a.Meta.RuntimeID != "" && m.RuntimeID != "" && a.Meta.RuntimeID != m.RuntimeID) {
			return fmt.Errorf("%w: answer to another request", errTransport)
		}
		if !a.OK {
			if a.Error != nil && (a.Error.Code == "method_not_found" || a.Error.Code == "unauthorized") {
				// An Orca whose methods changed, or a runtime file out of
				// step with the app: its own CLI still knows the way.
				return fmt.Errorf("%w: %s: %s", errTransport, a.Error.Code, a.Error.Message)
			}
			if a.Error != nil {
				return fmt.Errorf("orca %s: %s", method, a.Error.Message)
			}
			return fmt.Errorf("orca %s: request failed", method)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(a.Result, out)
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// poll asks the socket, else the CLI.
func (c Client) poll(out any, method string, params map[string]any, args ...string) error {
	err := c.rpc(out, method, params)
	if errors.Is(err, errTransport) && c.Bin != "" {
		return c.call(out, args...)
	}
	return err
}
