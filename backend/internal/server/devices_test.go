package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// tlsServer serves DeviceHandler over real TLS 1.3 with the deck's
// certificates; the client presents device's certificate ("" for none).
func tlsServer(t *testing.T, s *Server, device string) (*httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cert, err := s.CA.ServerCert(filepath.Join(dir, "srv.pem"), filepath.Join(dir, "srv.key"), []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(s.DeviceHandler())
	srv.TLS = s.TLSConfig(cert)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, clientFor(s, device)
}

func clientFor(s *Server, device string) *http.Client {
	cfg := &tls.Config{RootCAs: s.CA.Pool(), MinVersion: tls.VersionTLS13}
	if device != "" {
		cfg.Certificates = []tls.Certificate{testCerts[device]}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func TestDeviceTLS(t *testing.T) {
	s, _ := newTestServer()
	srv, panel := tlsServer(t, s, "panel")
	if r, err := panel.Get(srv.URL + "/v1/state"); err != nil || r.StatusCode != 200 {
		t.Fatalf("paired device: %v %v", r, err)
	}
	// No certificate: the handshake works, the API doesn't.
	if r, err := clientFor(s, "").Get(srv.URL + "/v1/state"); err != nil || r.StatusCode != 401 {
		t.Fatalf("no certificate: %v %v", r, err)
	}
	// A revoked device fails the handshake itself.
	if _, err := clientFor(s, "gone").Get(srv.URL + "/v1/state"); err == nil {
		t.Fatal("revoked device connected")
	}
	// TLS 1.2 is refused.
	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: s.CA.Pool(), MaxVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{testCerts["panel"]}}}}
	if _, err := old.Get(srv.URL + "/v1/state"); err == nil {
		t.Fatal("TLS 1.2 accepted")
	}
	// Revoking cuts off new connections at once.
	_ = s.Devices.Revoke("panel")
	fresh := clientFor(s, "panel")
	if _, err := fresh.Get(srv.URL + "/v1/state"); err == nil {
		t.Fatal("device still connects after revoke")
	}
}

func TestPairWithCode(t *testing.T) {
	s, _ := newTestServer()
	srv, _ := tlsServer(t, s, "")
	anon := clientFor(s, "")
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	csr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	post := func(code string) *http.Response {
		b, _ := json.Marshal(map[string]string{"code": code, "csr": csr})
		r, err := anon.Post(srv.URL+"/v1/pair", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := post("wrong"); r.StatusCode != 401 {
		t.Fatalf("wrong code: %d", r.StatusCode)
	}
	_ = s.Devices.NewPairingCode("phone", "view", "abc123")
	r := post("abc123")
	if r.StatusCode != 200 {
		t.Fatalf("pair: %d", r.StatusCode)
	}
	var out struct{ Name, Cert, CA string }
	_ = json.NewDecoder(r.Body).Decode(&out)
	if out.Name != "phone" || !strings.Contains(out.CA, "BEGIN CERTIFICATE") {
		t.Fatalf("pair result: %+v", out)
	}
	if r := post("abc123"); r.StatusCode != 401 {
		t.Fatalf("code reused: %d", r.StatusCode)
	}
	// The new certificate with the device's own key works, with view scope.
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	pair, err := tls.X509KeyPair([]byte(out.Cert), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}))
	if err != nil {
		t.Fatal(err)
	}
	testCerts["phone"] = pair
	phone := clientFor(s, "phone")
	if r, err := phone.Get(srv.URL + "/v1/state"); err != nil || r.StatusCode != 200 {
		t.Fatalf("paired phone: %v %v", r, err)
	}
	if r, _ := phone.Post(srv.URL+"/v1/agents/a1/focus", "", nil); r.StatusCode != 403 {
		t.Fatalf("view scope acted: %d", r.StatusCode)
	}
}

func TestNeedScope(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         string
	}{
		{"GET", "/v1/state", "view"}, {"GET", "/v1/events", "view"}, {"GET", "/v1/hats", "view"}, {"GET", "/v1/live", "admin"},
		{"POST", "/v1/agents/x/answer", "act"}, {"POST", "/v1/agents/x/focus", "act"}, {"POST", "/v1/agents/x/move", "act"},
		{"POST", "/v1/order", "act"}, {"POST", "/v1/hats", "act"}, {"POST", "/v1/agents/x/hat", "act"},
		{"POST", "/v1/agents/x/dismiss", "admin"}, {"POST", "/v1/agents/x/resume", "admin"}, {"POST", "/v1/restore", "admin"},
		{"POST", "/v1/live/s/add", "admin"}, {"POST", "/v1/agents/x/handoff", "admin"}, {"GET", "/v1/accounts", "view"},
	} {
		if got := NeedScope(c.method, c.path); string(got) != c.want {
			t.Errorf("%s %s: %s, want %s", c.method, c.path, got, c.want)
		}
	}
}
