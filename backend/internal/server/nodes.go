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
	"github.com/brushknight/agents-deck/backend/internal/model"
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
	case "POST /index":
		s.Node.ServeIndex(w, r, d)
	case "POST /act":
		s.Node.ServeAct(w, r, d)
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
	mux.HandleFunc("GET /local/outbox", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		rows := []OutboxRow{}
		for _, x := range s.Node.List() {
			rows = append(rows, OutboxRow{Machine: x.Name, Waiting: s.Node.Waiting(x.Name), Answers: s.Node.Answers(x.Name)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"outbox": rows})
	})
	mux.HandleFunc("POST /local/outbox/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var b struct{ Key string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Key == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"key": "…"}`)
			return
		}
		for _, x := range s.Node.List() {
			if s.Node.Cancel(x.Name, b.Key) {
				s.Audit.Log(devices.AuditEntry{Device: "local", Action: "cancel waiting call", Status: http.StatusNoContent, Detail: "for " + x.Name + ", key " + b.Key})
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		writeErr(w, http.StatusNotFound, "no waiting call with that key (it went already, or was cancelled)")
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

// RunNode wires the workspace to this machine's agents and keeps the links
// up until ctx ends: this machine's own agents go out to the others
// (re-indexed on every change), theirs come into the snapshot every screen
// reads, and a machine's state changing redraws the screens.
func (s *Server) RunNode(ctx context.Context) {
	n := s.Node
	n.LocalAgents = func() []model.Agent { return s.Store.LocalSnapshot().Agents }
	n.Changed = s.Store.Touch
	if n.Exec == nil {
		// A call from another machine runs as its /v1 API would for that
		// machine's device: the same scope check and audit, plus who asked.
		n.Exec = func(from devices.Device, c node.Call) (int, []byte) {
			s.Audit.Log(devices.AuditEntry{Device: from.Name, Action: c.Method + " " + c.Path, Detail: "from " + from.Name + ", asked by " + c.By + ", key " + c.Key})
			return s.Dispatch(from, c.Method, c.Path, c.Body)
		}
	}
	s.Store.Machine = func() string {
		if len(n.List()) == 0 {
			return ""
		}
		return n.Name
	}
	s.Store.Remote = n.RemoteAgents
	changes, stop := s.Store.Subscribe()
	events, unsub := n.Subscribe()
	go func() {
		defer stop()
		defer unsub()
		n.ReindexLocal()
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				time.Sleep(100 * time.Millisecond) // coalesce bursts of hook events
				select {
				case <-changes:
				default:
				}
				n.ReindexLocal()
			case e := <-events:
				if e.Kind == "machines" {
					s.Store.Touch() // their agents' reachable changed
				}
			}
		}
	}()
	go n.Run(ctx)
}

// forward sends a call for another machine's agent (<id>@<tag>) to that
// machine as the same call on its own id: now when it can be reached (the
// answer comes back as if the agent were here), else into its outbox: 202
// {"waiting": machine, "position": n, "key"}. Both machines' audit logs say
// who asked.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, id string) {
	machine, localID, ok := s.Node.RemoteOwner(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no machine of this workspace has the tag in "+id)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	path := "/v1/agents/" + localID + strings.TrimPrefix(r.URL.Path, "/v1/agents/"+id)
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	by := callerOf(r) + "@" + s.Node.Name
	a, queued, pos, err := s.Node.Forward(r.Context(), machine, by, r.Method, path, body)
	entry := devices.AuditEntry{Device: callerOf(r), Action: "forward " + r.Method + " " + r.URL.Path}
	switch {
	case err != nil:
		entry.Status, entry.Detail = http.StatusServiceUnavailable, "to "+machine+": "+err.Error()
		s.Audit.Log(entry)
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case queued != nil:
		entry.Status, entry.Detail = http.StatusAccepted, "waits for "+machine+", key "+queued.Key
		s.Audit.Log(entry)
		writeJSON(w, http.StatusAccepted, map[string]any{"waiting": machine, "position": pos, "key": queued.Key})
	default:
		entry.Status, entry.Detail = a.Code, "ran on "+machine
		s.Audit.Log(entry)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.Code)
		if len(a.Body) > 0 && a.Code != http.StatusNoContent {
			_, _ = w.Write(a.Body)
		}
	}
}

// OutboxRow is one machine's part of GET /local/outbox: its waiting calls,
// oldest first, and its recent answers, newest first.
type OutboxRow struct {
	Machine string        `json:"machine"`
	Waiting []node.Call   `json:"waiting"`
	Answers []node.Answer `json:"answers"`
}
