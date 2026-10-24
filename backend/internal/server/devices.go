package server

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
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
		if r.URL.Path == "/v1/link" && s.DeviceLink != nil {
			// A board's Deck Link session over Wi-Fi: a transport, not an API
			// endpoint. Its commands go through Dispatch (scope, audit).
			s.Devices.Seen(d.Name)
			s.DeviceLink(w, r, d)
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
		case "repair": // a board pairs again over USB (a new key and certificate)
			err = s.Devices.ClearBoardCert(name)
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

// ---- Deck Link boards: Wi-Fi certificates and credentials --------------------

// PairBoard issues an approved serial board its client certificate from a
// CSR (DER or PEM) for its own key, and records it on the board's entry: the
// same name, scope, revocation and audit log over Wi-Fi as over USB. It
// returns the certificate (DER) and its serial, or an HTTP status and error.
func (s *Server) PairBoard(d devices.Device, csr []byte) (cert []byte, serial string, code int, err error) {
	if s.CA == nil || s.Devices == nil {
		return nil, "", http.StatusServiceUnavailable, errors.New("the network listener is off (deviceAddr in config.json)")
	}
	if d.Kind != devices.Serial || !d.Active() {
		return nil, "", http.StatusForbidden, errors.New(d.Name + " isn't approved: agentctl devices approve " + d.Name)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(csr), []byte("-----BEGIN")) {
		csr = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})
	}
	c, _, err := s.CA.SignCSR(d.Name, csr)
	if err != nil {
		return nil, "", http.StatusBadRequest, err
	}
	serial = pki.SerialHex(c)
	if err := s.Devices.SetBoardCert(d.Name, serial, pki.Fingerprint(c.Raw)); err != nil {
		return nil, "", http.StatusForbidden, err
	}
	s.Audit.Log(devices.AuditEntry{Device: d.Name, Action: "pair (deck link)", Status: http.StatusOK, Detail: "certificate " + serial})
	log.Printf("link: %s paired for Wi-Fi (certificate %s)", d.Name, serial)
	s.Store.Touch() // the devices list on the web dashboard
	return c.Raw, serial, http.StatusOK, nil
}

// linkWifi: POST /local/link/wifi {"device", "ssid", "password", "forget"}.
// Unix socket only: the password goes straight to the board over USB serial
// and is never stored, logged or audited on the Mac. There is deliberately no
// /v1 counterpart: /v1 is reachable from the network, and a Wi-Fi password
// shouldn't travel further than this Mac and the board's USB cable.
func (s *Server) linkWifi(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Device   string `json:"device"`
		SSID     string `json:"ssid"`
		Password string `json:"password"`
		Forget   bool   `json:"forget"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Device == "" {
		writeErr(w, http.StatusBadRequest, `body must be {"device", "ssid", "password"} or {"device", "forget": true}`)
		return
	}
	if !b.Forget {
		if err := CheckWifi(b.SSID, b.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.LinkWifi == nil {
		writeErr(w, http.StatusServiceUnavailable, "Deck Link is off")
		return
	}
	code, msg := s.LinkWifi(b.Device, b.SSID, b.Password, b.Forget)
	what := "set"
	if b.Forget {
		what = "forget"
	}
	if s.Audit != nil {
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "wifi " + b.Device, Status: code, Detail: what})
	}
	if code >= 300 {
		writeErr(w, code, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CheckWifi: an SSID of 1 to 32 bytes; a WPA passphrase of 8 to 63
// characters, 64 hex digits, or "" for an open network.
func CheckWifi(ssid, password string) error {
	if ssid == "" || len(ssid) > 32 {
		return errors.New("the network name (SSID) must be 1 to 32 bytes")
	}
	switch n := len(password); {
	case n == 0, n >= 8 && n <= 63:
	case n == 64 && isHex(password):
	default:
		return errors.New("a Wi-Fi password has 8 to 63 characters (or 64 hex digits); leave it empty for an open network")
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
