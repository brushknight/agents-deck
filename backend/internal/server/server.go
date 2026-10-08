// Package server exposes protocol v1 on three listeners:
//   - unix socket: hooks + CLI (trusted by filesystem permissions)
//   - 127.0.0.1:7340: web UI + API, cookie session from a one-time login code
//   - LAN :7341 TLS: the panel, bearer device token, pinned self-signed cert
package server

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
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
	Store       *store.Store
	Ctl         Controller
	Static      fs.FS
	DeviceToken func() string
	WebSession  string
	LocalAddr   string // e.g. 127.0.0.1:7340
	logins      loginCodes
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
	mux.HandleFunc("POST /v1/agents/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		s.act(w, s.Ctl.Resume(r.PathValue("id")))
	})
	return mux
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

// ---- device listener (LAN, TLS, bearer) ---------------------------------

func (s *Server) DeviceHandler() http.Handler {
	api := s.api()
	return secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || !equal(strings.TrimPrefix(auth, "Bearer "), s.DeviceToken()) {
			time.Sleep(300 * time.Millisecond) // slow down guessing
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		api.ServeHTTP(w, r)
	}))
}

func ServeDevice(addr string, cert tls.Certificate, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0), // panel reconnects produce TLS noise
	}
	return srv.ListenAndServeTLS("", "")
}

// ---- local listener (loopback, web UI) ----------------------------------

func (s *Server) LocalHandler() http.Handler {
	api := s.api()
	static := http.FileServerFS(s.Static)
	return secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DNS-rebinding guard: only our own host names.
		if r.Host != s.LocalAddr && r.Host != strings.Replace(s.LocalAddr, "127.0.0.1", "localhost", 1) {
			writeErr(w, http.StatusMisdirectedRequest, "bad host")
			return
		}
		// The session cookie belongs to 127.0.0.1; send localhost visitors there
		// so a bookmark on either name works.
		if strings.HasPrefix(r.Host, "localhost") && r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/v1/") {
			http.Redirect(w, r, "http://"+s.LocalAddr+r.URL.RequestURI(), http.StatusTemporaryRedirect)
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
		if r.Method != http.MethodGet {
			o := r.Header.Get("Origin")
			if o != "http://"+s.LocalAddr && o != "http://"+strings.Replace(s.LocalAddr, "127.0.0.1", "localhost", 1) {
				writeErr(w, http.StatusForbidden, "bad origin")
				return
			}
		}
		api.ServeHTTP(w, r)
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
	mux.Handle("/v1/", api)
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
