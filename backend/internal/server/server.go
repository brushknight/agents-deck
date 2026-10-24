// Package server exposes protocol v1 on three listeners:
//   - unix socket: hooks, CLI and the menu bar app (trusted by filesystem permissions)
//   - 127.0.0.1:7340: web UI + API, cookie session from a one-time login code
//   - network :7341: TLS 1.3 with a client certificate per paired device
//     (devices.go), signed by the deck's own CA
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Controller performs agent actions; the live daemon and demo mode implement it.
type Controller interface {
	Answer(id, promptID, key string) error
	Focus(id string) error
	Interrupt(id string) error
	Dismiss(id string) error
	Resume(id string) error
}

// Restorer brings back agents lost with the tmux server (POST /v1/restore).
type Restorer interface {
	RestoreJSON() any
}

// Handoffer continues a Claude session in another account (POST
// /v1/agents/{id}/handoff), for when one account runs out of usage.
type Handoffer interface {
	Handoff(id, account string) (string, error)
}

// Watcher lists Claude sessions started by hand in other terminals and puts
// the ones you pick on the deck (GET /v1/live, POST /v1/live/{session}/add).
type Watcher interface {
	LiveJSON() any
	AddLive(sessionID string, slot int) error
}

// Local-only extras (unix socket).
type LocalController interface {
	Controller
	Hook(agentID string, body []byte) error
	Create(o CreateOpts) (string, error)
	ResumeSession(sessionID, cwd, title string) (string, error)
	HistoryJSON() any
}

// CreateOpts is the body of POST /local/agents.
type CreateOpts struct {
	Dir      string  `json:"dir"`
	Title    string  `json:"title"`
	Tool     string  `json:"tool"`               // claude | codex | gemini | opencode | shell | sim
	Account  string  `json:"account,omitempty"`  // claude: the Claude account to run in ("" = the default)
	Speed    float64 `json:"speed,omitempty"`    // sim only
	Patience int     `json:"patience,omitempty"` // sim only: seconds before auto-answering prompts
}

// Typed errors map to HTTP statuses.
var (
	ErrNotFound    = errors.New("no such agent")
	ErrStalePrompt = errors.New("prompt changed")
	ErrBadKey      = errors.New("not an option of the current prompt")
)

type Server struct {
	Store      *store.Store
	Ctl        Controller
	Static     fs.FS
	WebSession string
	LocalAddr  string // e.g. 127.0.0.1:7340
	// Hats holds the per-folder hat overrides (nil: hats can't be changed).
	Hats *hats.Book
	// Devices and CA: paired devices and the authority that signs their
	// certificates (nil: no network listener). Audit records their actions.
	Devices *devices.Registry
	CA      *pki.CA
	Audit   *devices.Audit
	// DeviceURL is how devices reach the network listener (https://host:port).
	DeviceURL string
	// ServerFingerprint is the network listener's certificate SHA-256.
	ServerFingerprint string
	// DeviceHosts and DevicePort are where boards reach the network listener
	// over Wi-Fi: the Mac's .local name first, then its LAN addresses.
	DeviceHosts []string
	DevicePort  int
	// DeviceLink serves GET /v1/link on the network listener: a paired
	// board's Deck Link session over TLS (link.DeviceUpgradeHandler).
	DeviceLink func(w http.ResponseWriter, r *http.Request, d devices.Device)
	// LinkWifi sends Wi-Fi credentials to a board connected over USB serial
	// (POST /local/link/wifi); it returns the board's answer.
	LinkWifi func(device, ssid, password string, forget bool) (int, string)
	// LinkUpgrade turns POST /local/link into a Deck Link stream (a board
	// without hardware: agentctl link sim); LinkStatus is GET /local/link:
	// {"ports": serial ports, "boards": connected boards on either transport}.
	LinkUpgrade http.HandlerFunc
	LinkStatus  func() any
	logins      loginCodes
	apiOnce     sync.Once
	apiMux      *http.ServeMux
}

// sharedAPI is one API mux for in-process callers (Dispatch).
func (s *Server) sharedAPI() *http.ServeMux {
	s.apiOnce.Do(func() { s.apiMux = s.api() })
	return s.apiMux
}

const sessionCookie = "agentsterm_session"

// ---- shared API --------------------------------------------------------

func (s *Server) api() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Store.Snapshot())
	})
	mux.HandleFunc("GET /v1/events", s.events)
	mux.HandleFunc("POST /v1/agents/{id}/answer", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PromptID string `json:"promptId"`
			Key      string `json:"key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad json")
			return
		}
		s.act(w, s.Ctl.Answer(r.PathValue("id"), body.PromptID, body.Key))
	})
	mux.HandleFunc("POST /v1/agents/{id}/focus", func(w http.ResponseWriter, r *http.Request) {
		s.act(w, s.Ctl.Focus(r.PathValue("id")))
	})
	mux.HandleFunc("POST /v1/agents/{id}/interrupt", func(w http.ResponseWriter, r *http.Request) {
		s.act(w, s.Ctl.Interrupt(r.PathValue("id")))
	})
	mux.HandleFunc("POST /v1/agents/{id}/dismiss", func(w http.ResponseWriter, r *http.Request) {
		s.act(w, s.Ctl.Dismiss(r.PathValue("id")))
	})
	mux.HandleFunc("POST /v1/order", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil || len(body.IDs) == 0 {
			writeErr(w, http.StatusBadRequest, "body must be {\"ids\": [...]}")
			return
		}
		s.Store.Reorder(body.IDs)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/agents/{id}/move", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slot *int `json:"slot"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || body.Slot == nil ||
			*body.Slot < 0 || *body.Slot > store.MaxSlot {
			writeErr(w, http.StatusBadRequest, "body must be {\"slot\": 0..255}")
			return
		}
		if !s.Store.Place(r.PathValue("id"), *body.Slot) {
			writeErr(w, http.StatusNotFound, "no such agent")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/hats", func(w http.ResponseWriter, r *http.Request) {
		over := map[string]hats.Override{}
		if s.Hats != nil {
			over = s.Hats.Overrides()
		}
		writeJSON(w, http.StatusOK, map[string]any{"shapes": hats.Shapes, "colors": hats.Colors, "overrides": over})
	})
	mux.HandleFunc("POST /v1/hats", func(w http.ResponseWriter, r *http.Request) {
		var body hatBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.Folder == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"folder", "shape", "color"} or {"folder", "auto": true}`)
			return
		}
		s.setHat(w, body.Folder, body)
	})
	mux.HandleFunc("POST /v1/agents/{id}/hat", func(w http.ResponseWriter, r *http.Request) {
		var body hatBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, `body must be {"shape", "color"} or {"auto": true}`)
			return
		}
		e, ok := s.Store.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, http.StatusNotFound, "no such agent")
			return
		}
		if e.A.Cwd == "" {
			writeErr(w, http.StatusBadRequest, "this agent has no working folder")
			return
		}
		s.setHat(w, e.A.Cwd, body)
	})
	mux.HandleFunc("GET /v1/live", func(w http.ResponseWriter, r *http.Request) {
		lw, ok := s.Ctl.(Watcher)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": lw.LiveJSON()})
	})
	mux.HandleFunc("POST /v1/live/{session}/add", func(w http.ResponseWriter, r *http.Request) {
		lw, ok := s.Ctl.(Watcher)
		if !ok {
			writeErr(w, http.StatusNotImplemented, "not available")
			return
		}
		body := struct {
			Slot *int `json:"slot"`
		}{}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) // optional
		slot := -1
		if body.Slot != nil && *body.Slot >= 0 && *body.Slot <= store.MaxSlot {
			slot = *body.Slot
		}
		s.act(w, lw.AddLive(r.PathValue("session"), slot))
	})
	mux.HandleFunc("POST /v1/restore", func(w http.ResponseWriter, r *http.Request) {
		rs, ok := s.Ctl.(Restorer)
		if !ok {
			writeErr(w, http.StatusNotImplemented, "restore is not available")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": rs.RestoreJSON()})
	})
	mux.HandleFunc("POST /v1/agents/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		s.act(w, s.Ctl.Resume(r.PathValue("id")))
	})
	mux.HandleFunc("GET /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"accounts": accountList()})
	})
	mux.HandleFunc("POST /v1/agents/{id}/handoff", func(w http.ResponseWriter, r *http.Request) {
		ho, ok := s.Ctl.(Handoffer)
		if !ok {
			writeErr(w, http.StatusNotImplemented, "handing over is not available")
			return
		}
		var body struct {
			Account string `json:"account"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || body.Account == "" {
			writeErr(w, http.StatusBadRequest, `body must be {"account": "<name>"}`)
			return
		}
		id, err := ho.Handoff(r.PathValue("id"), body.Account)
		if err != nil {
			s.act(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	})
	return mux
}

// AccountInfo is one row of GET /v1/accounts.
type AccountInfo struct {
	Name    string `json:"name"`
	Color   string `json:"color"`
	Default bool   `json:"default"` // new agents start in it
}

// accountList is the configured Claude accounts; empty while there is only
// one (nothing to choose between).
func accountList() []AccountInfo {
	out := []AccountInfo{}
	if !accounts.InUse() {
		return out
	}
	def := accounts.Default().Name
	for _, a := range accounts.List() {
		out = append(out, AccountInfo{Name: a.Name, Color: a.Color, Default: a.Name == def})
	}
	return out
}

// hatBody picks a hat ({"shape", "color"}; "color" optional) or goes back to
// the automatic one ({"auto": true}).
type hatBody struct {
	Folder string `json:"folder"`
	Shape  string `json:"shape"`
	Color  string `json:"color"`
	Auto   bool   `json:"auto"`
}

// setHat stores a folder's hat and tells every client.
func (s *Server) setHat(w http.ResponseWriter, folder string, b hatBody) {
	if s.Hats == nil {
		writeErr(w, http.StatusNotImplemented, "hats can't be changed here")
		return
	}
	if !b.Auto && b.Shape == "" {
		writeErr(w, http.StatusBadRequest, `give a "shape" (or "auto": true)`)
		return
	}
	if err := s.Hats.Set(folder, b.Shape, b.Color, b.Auto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Store.Touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) act(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrStalePrompt):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrBadKey):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// events streams full snapshots: one on connect, one per change (debounced), pings every 15s.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch, unsub := s.Store.Subscribe()
	defer unsub()
	rc := http.NewResponseController(w)
	send := func() bool {
		b, _ := json.Marshal(s.Store.Snapshot())
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send() {
		return
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			time.Sleep(150 * time.Millisecond) // coalesce bursts of hook events
			select {
			case <-ch:
			default:
			}
			if !send() {
				return
			}
		case <-ping.C:
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// ---- local listener (loopback, web UI) ----------------------------------

func (s *Server) LocalHandler() http.Handler {
	alt := strings.Replace(s.LocalAddr, "127.0.0.1", "localhost", 1)
	return s.webHandler(func(r *http.Request) bool {
		return r.Host == s.LocalAddr || r.Host == alt
	}, func(w http.ResponseWriter, r *http.Request) bool {
		// The session cookie belongs to 127.0.0.1; send localhost visitors there
		// so a bookmark on either name works.
		if strings.HasPrefix(r.Host, "localhost") && r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/v1/") {
			http.Redirect(w, r, "http://"+s.LocalAddr+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return true
		}
		return false
	})
}

// webHandler is the web UI + cookie-authenticated API behind a host check
// (DNS-rebinding guard) and a same-origin check for writes.
func (s *Server) webHandler(hostOK func(*http.Request) bool, pre func(http.ResponseWriter, *http.Request) bool) http.Handler {
	api := s.api()
	static := http.FileServerFS(s.Static)
	return secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostOK(r) {
			writeErr(w, http.StatusMisdirectedRequest, "bad host")
			return
		}
		if pre != nil && pre(w, r) {
			return
		}
		if r.URL.Path == "/login" {
			if s.logins.redeem(r.URL.Query().Get("code")) {
				http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.WebSession, Path: "/",
					HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			writeErr(w, http.StatusUnauthorized, "login link expired — run `agentctl web` again")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			if r.URL.Path == "/fixture.json" {
				http.NotFound(w, r) // dev-only file never served by the daemon
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || !equal(c.Value, s.WebSession) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("Origin") != "http://"+r.Host {
			writeErr(w, http.StatusForbidden, "bad origin")
			return
		}
		s.audited("web", api).ServeHTTP(w, r)
	}))
}

func ServeLocal(addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// ---- unix socket (hooks, CLI) --------------------------------------------

func (s *Server) UnixHandler(lc LocalController) http.Handler {
	api := s.api()
	mux := http.NewServeMux()
	mux.Handle("/v1/", s.audited("local", api))
	s.localDevices(mux)
	if s.LinkUpgrade != nil {
		mux.HandleFunc("POST /local/link", s.LinkUpgrade)
	}
	mux.HandleFunc("POST /local/link/wifi", s.linkWifi)
	mux.HandleFunc("GET /local/link", func(w http.ResponseWriter, r *http.Request) {
		if s.LinkStatus == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ports": []any{}, "boards": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, s.LinkStatus())
	})
	mux.HandleFunc("POST /hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err := lc.Hook(r.URL.Query().Get("agent"), body); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /local/agents", func(w http.ResponseWriter, r *http.Request) {
		var body CreateOpts
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad json")
			return
		}
		id, err := lc.Create(body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	})
	mux.HandleFunc("GET /local/agents", func(w http.ResponseWriter, r *http.Request) {
		type row struct {
			SessionID string      `json:"sessionId"`
			Sim       bool        `json:"sim"`
			Agent     model.Agent `json:"agent"`
		}
		var out []row
		for _, a := range s.Store.Snapshot().Agents {
			if e, ok := s.Store.Get(a.ID); ok {
				out = append(out, row{e.SessionID, e.Sim, a})
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /local/history", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, lc.HistoryJSON())
	})
	mux.HandleFunc("POST /local/resume", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"sessionId"`
			Cwd       string `json:"cwd"`
			Title     string `json:"title"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad json")
			return
		}
		id, err := lc.ResumeSession(body.SessionID, body.Cwd, body.Title)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	})
	mux.HandleFunc("POST /local/login-code", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"code": s.logins.issue()})
	})
	return mux
}

func ServeUnix(path string, h http.Handler) error {
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	return srv.Serve(l)
}

// ---- helpers --------------------------------------------------------------

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
