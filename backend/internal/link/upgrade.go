package link

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// UpgradeHandler serves POST /local/link on the daemon's unix socket: the
// connection becomes a Deck Link stream, so `agentctl link sim` (and tests)
// can be a board without hardware. It counts as USB serial.
func UpgradeHandler(deck Deck, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "deck-link") {
			http.Error(w, "send Upgrade: deck-link", http.StatusBadRequest)
			return
		}
		conn, stream, err := upgrade(w)
		if err != nil {
			return
		}
		defer conn.Close()
		s := NewSession(deck, stream, "sim")
		s.Hub, s.Where = hub, "sim"
		_ = s.Run(context.Background())
	}
}

// NetworkIdle ends a Wi-Fi session after this long without a frame (the
// board answers a Ping every 5 s).
var NetworkIdle = 30 * time.Second

// DeviceUpgradeHandler serves GET /v1/link on the network listener (TLS 1.3,
// a client certificate): a paired board's Deck Link session over Wi-Fi. The
// certificate is the board's identity: its Hello must carry the board id of
// the device the certificate belongs to, and the session ends when that
// device is revoked or pairs again. Everything else is the same session as
// over USB: scope, commands, audit log.
func DeviceUpgradeHandler(deck Deck, hub *Hub) func(http.ResponseWriter, *http.Request, devices.Device) {
	return func(w http.ResponseWriter, r *http.Request, d devices.Device) {
		if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "deck-link") {
			writeErr(w, http.StatusBadRequest, "GET /v1/link with Upgrade: deck-link")
			return
		}
		if d.Kind != devices.Serial || d.BoardID == "" {
			writeErr(w, http.StatusForbidden, "only Deck Link boards use /v1/link")
			return
		}
		conn, stream, err := upgrade(w)
		if err != nil {
			return
		}
		defer conn.Close()
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		s := NewSession(deck, stream, d.Name+"@wifi")
		s.Hub, s.Transport, s.Where, s.Peer, s.IdleTimeout = hub, OverWifi, host, &d, NetworkIdle
		log.Printf("link: %s connected over Wi-Fi from %s", d.Name, host)
		err = s.Run(context.Background())
		log.Printf("link: %s left Wi-Fi (%v)", d.Name, err)
	}
}

// upgrade answers 101 and hands back the raw connection as a stream (with
// whatever the client sent after its request).
func upgrade(w http.ResponseWriter) (net.Conn, io.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Time{}) // the listener's header timeouts no longer apply
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: deck-link\r\nConnection: Upgrade\r\n\r\n")
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, struct {
		io.Reader
		io.Writer
	}{rw.Reader, conn}, nil
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// DialSim connects to the daemon's unix socket as a Deck Link board.
func DialSim(sock string) (io.ReadWriteCloser, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	fmt.Fprint(conn, "POST /local/link HTTP/1.1\r\nHost: agentctl\r\nConnection: Upgrade\r\nUpgrade: deck-link\r\nContent-Length: 0\r\n\r\n")
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("daemon refused the link: %s", strings.TrimSpace(status))
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if l == "\r\n" {
			break
		}
	}
	return struct {
		io.Reader
		io.Writer
		io.Closer
	}{br, conn, conn}, nil
}
