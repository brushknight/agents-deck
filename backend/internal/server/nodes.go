package server

// Multi-node (internal/node): the routes other machines of the workspace
// call, on the device listener only (never on 127.0.0.1:7340 or the unix
// socket), and the local ones `agentctl machines` calls on the unix socket.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/node"
)

// serveNode answers /node/v1/ on the device listener. Only the CA and the
// join itself are served without a certificate (like POST /v1/pair, the
// code is the key); the rest is for machines that joined: kind node only,
// every other device gets 403.
func (s *Server) serveNode(w http.ResponseWriter, r *http.Request, d devices.Device, paired bool) {
	if s.Node == nil {
		writeErr(w, http.StatusNotFound, "this machine isn't set up for a workspace")
		return
	}
	route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/node/v1")
	switch route {
	case "GET /ca":
		s.Node.ServeCA(w, r)
		return
	case "POST /join":
		s.Node.ServeJoin(w, r)
		return
	}
	if !paired || d.Kind != devices.Node {
		time.Sleep(300 * time.Millisecond)
		writeErr(w, http.StatusForbidden, "only machines of this workspace may call /node/v1")
		return
	}
	s.Devices.Seen(d.Name)
	s.Node.Heard(d.Name)
	switch route {
	case "POST /outbox":
		s.Node.ServeOutbox(w, r, d)
	case "GET /hello":
		s.Node.ServeHello(w, r, d)
	case "POST /join/finish":
		s.Node.ServeJoinFinish(w, r, d)
	case "POST /leave":
		s.Node.ServeLeave(w, r, d)
	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

// MachineRow is one line of `agentctl machines` (GET /local/machines).
type MachineRow struct {
	node.State
	Address  string    `json:"address"`
	Joined   time.Time `json:"joined"`
	LastSeen time.Time `json:"lastSeen,omitzero"` // its last request here
	Scope    string    `json:"scope"`             // what it may do here
}

// localMachines adds the unix-socket routes behind `agentctl machines`.
func (s *Server) localMachines(mux *http.ServeMux) {
	need := func(w http.ResponseWriter) bool {
		if s.Node == nil {
			writeErr(w, http.StatusNotImplemented, "this machine isn't set up for a workspace (no network listener: deviceAddr is empty)")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /local/machines", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		rows := []MachineRow{}
		states := map[string]node.State{}
		for _, st := range s.Node.States() {
			states[st.Name] = st
		}
		for _, x := range s.Node.List() {
			row := MachineRow{State: states[x.Name], Address: x.Address, Joined: x.Joined, LastSeen: s.Node.LastSeen(x.Name)}
			row.Name = x.Name
			if d, ok := s.Devices.Get(x.Name); ok {
				row.Scope = string(d.Scope)
			}
			rows = append(rows, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": s.Node.Name, "machines": rows})
	})
	mux.HandleFunc("POST /local/machines/invite", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		code, exp, err := s.Node.Invite()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": s.Node.Name, "code": code, "expires": exp,
			"port": s.Node.DevicePort, "fingerprint": s.CA.Fingerprint()})
	})
	mux.HandleFunc("POST /local/machines/join", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var b struct{ Address, Code string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Address == "" || b.Code == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"address": "host[:port]", "code": "…"}`)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		x, err := s.Node.Join(ctx, b.Address, b.Code)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": x.Name, "address": x.Address, "scope": devices.NodeJoinScope})
	})
	mux.HandleFunc("POST /local/machines/remove", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var b struct{ Name string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Name == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"name": "…"}`)
			return
		}
		told, err := s.Node.Remove(r.Context(), b.Name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": b.Name, "told": told})
	})
	mux.HandleFunc("POST /local/machines/leave", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		names, err := s.Node.Leave(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"left": names})
	})
}
