package server

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// ---- network listener: TLS 1.3, one client certificate per device ----------

// NeedScope is the scope an API request needs: reading is view; acting on an
// agent is act; stopping, restarting or adding agents is admin.
func NeedScope(method, path string) devices.Scope {
	if method == http.MethodGet {
		if path == "/v1/live" {
			return devices.Admin // lists sessions outside the deck, to add them
		}
		return devices.View
	}
	switch {
	case strings.HasSuffix(path, "/dismiss"), strings.HasSuffix(path, "/resume"), strings.HasSuffix(path, "/handoff"),
		path == "/v1/restore", strings.HasPrefix(path, "/v1/live/"):
		return devices.Admin
	}
	return devices.Act
}

type deviceKey struct{}

// DeviceHandler serves the API to paired devices. The TLS layer has already
// checked the certificate against the CA and the registry (TLSConfig); here
// the device's scope gates each request. A connection without a certificate
// may only pair (POST /v1/pair with a one-time code).
func (s *Server) DeviceHandler() http.Handler {
	api := s.api()
	return secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.deviceOf(r)
		if !ok {
			if r.Method == http.MethodPost && r.URL.Path == "/v1/pair" {
				s.pair(w, r)
				return
			}
			time.Sleep(300 * time.Millisecond) // slow down probing
			writeErr(w, http.StatusUnauthorized, "this device isn't paired: run `agentctl pair` on the Mac")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if need := NeedScope(r.Method, r.URL.Path); !d.Scope.Allows(need) {
			writeErr(w, http.StatusForbidden, d.Name+" may "+string(d.Scope)+", this needs "+string(need)+": agentctl devices scope "+d.Name+" "+string(need))
			return
		}
		s.Devices.Seen(d.Name)
		s.audited(d.Name, api).ServeHTTP(w, r)
	}))
}

// deviceOf is the active device whose verified certificate made this request.
func (s *Server) deviceOf(r *http.Request) (devices.Device, bool) {
	if s.Devices == nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return devices.Device{}, false
	}
	return s.Devices.ByCertSerial(pki.SerialHex(r.TLS.VerifiedChains[0][0]))
}

// TLSConfig: TLS 1.3 only; a client certificate is optional at the handshake
// (so a new device can pair) but must be signed by the deck's CA and belong
// to an active device when given; revoked devices fail the handshake.
func (s *Server) TLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    s.CA.Pool(),
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.VerifiedChains) == 0 {
				return nil // no certificate: pairing only (DeviceHandler)
			}
			if _, ok := s.Devices.ByCertSerial(pki.SerialHex(cs.VerifiedChains[0][0])); !ok {
				return errors.New("device revoked or unknown")
			}
			return nil
		},
	}
}

// ServeDevice runs the network listener.
func ServeDevice(addr string, cfg *tls.Config, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         cfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0), // device reconnects produce TLS noise
	}
	return srv.ListenAndServeTLS("", "")
}

// pair: POST /v1/pair {"code", "csr"} → {"name", "cert", "ca"}. The code
// (from `agentctl pair <name>`, 2 minutes, single use) names the device and
// its scope; the request's key becomes the device's.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		CSR  string `json:"csr"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	name, scope, ok := s.Devices.RedeemPairingCode(body.Code, equal)
	if !ok {
		time.Sleep(300 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "pairing code expired or wrong: run `agentctl pair` again")
		return
	}
	cert, certPEM, err := s.CA.SignCSR(name, []byte(body.CSR))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Devices.AddNetwork(name, scope, pki.SerialHex(cert), pki.Fingerprint(cert.Raw)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Log(devices.AuditEntry{Device: name, Action: "pair", Status: http.StatusOK, Detail: "scope " + string(scope)})
	log.Printf("paired %s (%s)", name, scope)
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "cert": string(certPEM), "ca": string(s.CA.PEM)})
}

// Dispatch runs one API request for a device that isn't on the network
// listener (a Deck Link board): the same scope check, handlers and audit log.
// It returns the status code and the response body.
func (s *Server) Dispatch(d devices.Device, method, path string, body []byte) (int, []byte) {
	if need := NeedScope(method, path); !d.Scope.Allows(need) {
		b, _ := json.Marshal(map[string]string{"error": d.Name + " may " + string(d.Scope) + ", this needs " + string(need)})
		return http.StatusForbidden, b
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.audited(d.Name, s.sharedAPI()).ServeHTTP(w, r)
	if s.Devices != nil {
		s.Devices.Seen(d.Name)
	}
	return w.Code, w.Body.Bytes()
}

// ---- audit -------------------------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// audited logs every request that changes something, under who.
func (s *Server) audited(who string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || s.Audit == nil {
			h.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(sw, r)
		s.Audit.Log(devices.AuditEntry{Device: who, Action: r.Method + " " + r.URL.Path, Status: sw.code})
	})
}

// ---- device management on the unix socket (agentctl devices / pair) ----------

// Bundle is what a provisioned device needs to connect: where, whom to trust,
// and its own certificate and key.
type Bundle struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	CA          string `json:"ca"`          // PEM: the deck's CA
	Fingerprint string `json:"fingerprint"` // the listener's certificate, for hosts its names don't cover
	Cert        string `json:"cert"`        // PEM: the device's certificate
	Key         string `json:"key"`         // PEM: the device's private key
}

func (s *Server) localDevices(mux *http.ServeMux) {
	ready := func(w http.ResponseWriter) bool {
		if s.Devices == nil || s.CA == nil {
			writeErr(w, http.StatusServiceUnavailable, "the network listener is off (deviceAddr in config.json)")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /local/devices", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": s.Devices.List(), "ca": s.CA.Fingerprint(), "url": s.DeviceURL})
	})
	type nameScope struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	read := func(w http.ResponseWriter, r *http.Request) (string, devices.Scope, bool) {
		var b nameScope
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil {
			writeErr(w, http.StatusBadRequest, "bad json")
			return "", "", false
		}
		if b.Scope == "" {
			b.Scope = string(devices.Act)
		}
		sc, err := devices.ParseScope(b.Scope)
		if err == nil {
			err = devices.ValidName(b.Name)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return "", "", false
		}
		return b.Name, sc, true
	}
	// A one-time code for a device that makes its own key (POST /v1/pair).
	mux.HandleFunc("POST /local/devices/code", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name, sc, ok := read(w, r)
		if !ok {
			return
		}
		code := RandomToken()[:20]
		if err := s.Devices.NewPairingCode(name, sc, code); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"code": code, "url": s.DeviceURL, "ca": s.CA.Fingerprint(),
			"expires": time.Now().Add(devices.PairingCodeTTL).UTC().Format(time.RFC3339)})
	})
	// A key and certificate made here, for a device you set up over SSH.
	mux.HandleFunc("POST /local/devices/provision", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name, sc, ok := read(w, r)
		if !ok {
			return
		}
		cert, certPEM, keyPEM, err := s.CA.NewDeviceKey(name)
		if err == nil {
			err = s.Devices.AddNetwork(name, sc, pki.SerialHex(cert), pki.Fingerprint(cert.Raw))
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "provision " + name, Status: http.StatusOK, Detail: "scope " + string(sc)})
		writeJSON(w, http.StatusOK, Bundle{Name: name, URL: s.DeviceURL, CA: string(s.CA.PEM), Fingerprint: s.ServerFingerprint,
			Cert: string(certPEM), Key: string(keyPEM)})
	})
	mux.HandleFunc("POST /local/devices/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		name := r.PathValue("name")
		var err error
		switch r.PathValue("action") {
		case "revoke":
			err = s.Devices.Revoke(name)
		case "approve":
			err = s.Devices.Approve(name)
		case "remove":
			err = s.Devices.Remove(name)
		case "scope":
			var b nameScope
			_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&b)
			var sc devices.Scope
			if sc, err = devices.ParseScope(b.Scope); err == nil {
				err = s.Devices.SetScope(name, sc)
			}
		default:
			writeErr(w, http.StatusNotFound, "unknown action")
			return
		}
		switch {
		case errors.Is(err, devices.ErrUnknown):
			writeErr(w, http.StatusNotFound, err.Error())
		case err != nil:
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			s.Audit.Log(devices.AuditEntry{Device: "local", Action: r.PathValue("action") + " " + name, Status: http.StatusNoContent})
			s.Store.Touch() // the devices list on the web dashboard
			w.WriteHeader(http.StatusNoContent)
		}
	})
}
