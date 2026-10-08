// Package iterm2 is a minimal client for iTerm2's local Python API (a
// WebSocket on ~/Library/Application Support/iTerm2/private/socket speaking
// protobuf, see iTerm2's proto/api.proto). Only what focus needs is
// implemented: list sessions, read a session's tty, activate a session.
//
// It needs "Enable Python API" in iTerm's settings. Connections are
// authenticated with a cookie that iTerm issues over AppleScript (the only
// Apple event this program sends); with the API server's "allow all apps"
// setting no cookie is needed.
package iterm2

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SocketPath is iTerm2's API socket.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "iTerm2", "private", "socket")
}

// Available reports whether the API socket exists (iTerm running, API enabled).
func Available() bool {
	st, err := os.Stat(SocketPath())
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// RequestCookie asks iTerm for a one-time API cookie+key via AppleScript.
func RequestCookie(appName string) (cookie, key string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e",
		`tell application "iTerm2" to request cookie and key for app named "`+strings.ReplaceAll(appName, `"`, ``)+`"`).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", "", fmt.Errorf("iTerm refused the API cookie request: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", "", err
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return "", "", errors.New("unexpected cookie reply from iTerm")
	}
	return f[0], f[1], nil
}

// Conn is one authenticated API connection. Requests are serialised.
type Conn struct {
	mu   sync.Mutex
	c    net.Conn
	br   *bufio.Reader
	next int64
}

// Dial connects and authenticates with cookie/key (both may be empty when
// iTerm is set to allow all apps).
func Dial(cookie, key, appName string) (*Conn, error) {
	c, err := net.DialTimeout("unix", SocketPath(), 3*time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	wsKey := base64.StdEncoding.EncodeToString(nonce)
	req := "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + wsKey + "\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: api.iterm2.com\r\nOrigin: ws://localhost/\r\n" +
		"x-iterm2-library-version: python 3.0\r\nx-iterm2-advisory-name: " + appName + "\r\n"
	if cookie != "" {
		req += "x-iterm2-cookie: " + cookie + "\r\nx-iterm2-key: " + key + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		c.Close()
		return nil, fmt.Errorf("iTerm API refused the connection (%s)", resp.Status)
	}
	sum := sha1.Sum([]byte(wsKey + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, errors.New("iTerm API: bad websocket handshake")
	}
	_ = c.SetDeadline(time.Time{})
	return &Conn{c: c, br: br}, nil
}

func (k *Conn) Close() error { return k.c.Close() }

// ---- websocket framing (RFC 6455, client side) --------------------------------

func (k *Conn) writeFrame(payload []byte) error {
	var hdr []byte
	hdr = append(hdr, 0x82) // FIN + binary
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 1<<16:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	hdr = append(hdr, mask...)
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	_, err := k.c.Write(append(hdr, masked...))
	return err
}

func (k *Conn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(k.br, h); err != nil {
			return nil, err
		}
		op, fin := h[0]&0x0f, h[0]&0x80 != 0
		n := int64(h[1] & 0x7f)
		switch n {
		case 126:
			b := make([]byte, 2)
			if _, err := io.ReadFull(k.br, b); err != nil {
				return nil, err
			}
			n = int64(binary.BigEndian.Uint16(b))
		case 127:
			b := make([]byte, 8)
			if _, err := io.ReadFull(k.br, b); err != nil {
				return nil, err
			}
			n = int64(binary.BigEndian.Uint64(b))
		}
		if n > 64<<20 {
			return nil, errors.New("iTerm API: frame too large")
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(k.br, mask); err != nil {
				return nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(k.br, p); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8:
			return nil, errors.New("iTerm API closed the connection")
		case 0x9: // ping
			continue
		case 0xA:
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}

// ---- protobuf (just enough) -----------------------------------------------------

func varint(b []byte, v uint64) []byte { return binary.AppendUvarint(b, v) }

func field(b []byte, num int, wire int) []byte { return varint(b, uint64(num<<3|wire)) }

func pbBytes(b []byte, num int, data []byte) []byte {
	b = field(b, num, 2)
	b = varint(b, uint64(len(data)))
	return append(b, data...)
}

func pbString(b []byte, num int, s string) []byte { return pbBytes(b, num, []byte(s)) }

func pbBool(b []byte, num int, v bool) []byte {
	b = field(b, num, 0)
	if v {
		return varint(b, 1)
	}
	return varint(b, 0)
}

// pbFields decodes one message level: field number -> values (raw bytes for
// length-delimited, the varint for varints).
type pbVal struct {
	raw []byte
	num uint64
}

func pbFields(b []byte) (map[int][]pbVal, error) {
	out := map[int][]pbVal{}
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errors.New("bad protobuf tag")
		}
		b = b[n:]
		num, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errors.New("bad varint")
			}
			b = b[n:]
			out[num] = append(out[num], pbVal{num: v})
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return nil, errors.New("bad length")
			}
			b = b[n:]
			out[num] = append(out[num], pbVal{raw: b[:l]})
			b = b[l:]
		case 1:
			if len(b) < 8 {
				return nil, errors.New("bad fixed64")
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return nil, errors.New("bad fixed32")
			}
			b = b[4:]
		default:
			return nil, fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return out, nil
}

// Field numbers from proto/api.proto.
const (
	fID           = 1
	fError        = 2
	fListSessions = 106
	fActivate     = 114
	fVariable     = 115
)

// call sends a request in oneof field fnum and returns the matching response body.
func (k *Conn) call(fnum int, body []byte) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.next++
	id := k.next
	msg := field(nil, fID, 0)
	msg = varint(msg, uint64(id))
	msg = pbBytes(msg, fnum, body)
	_ = k.c.SetDeadline(time.Now().Add(5 * time.Second))
	defer k.c.SetDeadline(time.Time{})
	if err := k.writeFrame(msg); err != nil {
		return nil, err
	}
	for {
		raw, err := k.readMessage()
		if err != nil {
			return nil, err
		}
		f, err := pbFields(raw)
		if err != nil {
			return nil, err
		}
		if len(f[fID]) == 0 || int64(f[fID][0].num) != id {
			continue // a notification or another response
		}
		if e := f[fError]; len(e) > 0 {
			return nil, errors.New("iTerm API: " + string(e[0].raw))
		}
		if r := f[fnum]; len(r) > 0 {
			return r[0].raw, nil
		}
		return nil, errors.New("iTerm API: empty response")
	}
}

// Session is one terminal session (a pane) in iTerm.
type Session struct {
	ID       string // unique_identifier
	TabID    string
	WindowID string
	Selected bool // its tab is the window's selected tab and it is the tab's active pane
}

// Sessions lists every session in every window (hotkey windows included).
func (k *Conn) Sessions() ([]Session, error) {
	raw, err := k.call(fListSessions, nil)
	if err != nil {
		return nil, err
	}
	top, err := pbFields(raw)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, w := range top[1] { // windows
		wf, err := pbFields(w.raw)
		if err != nil {
			continue
		}
		wid, selTab := str(wf, 2), str(wf, 5)
		for _, t := range wf[1] { // tabs
			tf, err := pbFields(t.raw)
			if err != nil {
				continue
			}
			tid, active := str(tf, 2), str(tf, 7)
			for _, root := range tf[3] {
				walkTree(root.raw, func(id string) {
					out = append(out, Session{ID: id, TabID: tid, WindowID: wid, Selected: tid == selTab && id == active})
				})
			}
		}
	}
	return out, nil
}

func walkTree(node []byte, visit func(string)) {
	nf, err := pbFields(node)
	if err != nil {
		return
	}
	for _, link := range nf[2] {
		lf, err := pbFields(link.raw)
		if err != nil {
			continue
		}
		for _, s := range lf[1] { // SessionSummary
			sf, err := pbFields(s.raw)
			if err == nil {
				if id := str(sf, 1); id != "" {
					visit(id)
				}
			}
		}
		for _, child := range lf[2] {
			walkTree(child.raw, visit)
		}
	}
}

func str(f map[int][]pbVal, n int) string {
	if v := f[n]; len(v) > 0 {
		return string(v[0].raw)
	}
	return ""
}

// Variable reads one session variable (e.g. "tty"); "" when unset.
func (k *Conn) Variable(sessionID, name string) (string, error) {
	body := pbString(nil, 1, sessionID)
	body = pbString(body, 3, name)
	raw, err := k.call(fVariable, body)
	if err != nil {
		return "", err
	}
	f, err := pbFields(raw)
	if err != nil {
		return "", err
	}
	if s := f[1]; len(s) > 0 && s[0].num != 0 {
		return "", fmt.Errorf("variable %s: status %d", name, s[0].num)
	}
	if v := f[2]; len(v) > 0 {
		var s string
		if json.Unmarshal(v[0].raw, &s) == nil {
			return s, nil
		}
	}
	return "", nil
}

// Activate selects the session's tab and pane and brings its window and the
// app to the front.
func (k *Conn) Activate(sessionID string) error {
	app := pbBool(nil, 1, false)
	app = pbBool(app, 2, true)
	body := pbString(nil, 3, sessionID)
	body = pbBool(body, 4, true)
	body = pbBool(body, 5, true)
	body = pbBool(body, 6, true)
	body = pbBytes(body, 7, app)
	raw, err := k.call(fActivate, body)
	if err != nil {
		return err
	}
	f, err := pbFields(raw)
	if err != nil {
		return err
	}
	if s := f[1]; len(s) > 0 && s[0].num != 0 {
		return fmt.Errorf("activate: status %d", s[0].num)
	}
	return nil
}
