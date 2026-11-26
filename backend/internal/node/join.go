package node

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/pki"
)

// Joining, step by step (A is a member, B joins):
//
//  1. B: GET  A/node/v1/ca            (no certificate) A's CA; B checks it against the code.
//  2. B: POST A/node/v1/join          (no certificate, pinned to A's CA) the code's secret,
//     B's name, a request for a certificate from A's CA, B's CA and port. A signs it, adds
//     B as a node, and answers with B's certificate and a request of its own.
//  3. B signs A's request with its CA, adds A as a node, keeps A in machines.json.
//  4. B: POST A/node/v1/join/finish   (B's new certificate) A's certificate from B's CA.
//     A keeps B in machines.json.

// DefaultPort is the device listener's port when an address has none.
const DefaultPort = "7341"

// caAnswer is GET /node/v1/ca.
type caAnswer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	CA      string `json:"ca"`
}

type joinRequest struct {
	Code string `json:"code"` // the code's secret
	Name string `json:"name"` // the joining machine
	CSR  string `json:"csr"`  // for a certificate from this machine's CA
	CA   string `json:"ca"`   // the joining machine's CA
	Port string `json:"port"` // its device listener's port
}

type joinAnswer struct {
	Name string `json:"name"` // this machine
	Cert string `json:"cert"` // the joining machine's certificate here
	CSR  string `json:"csr"`  // for this machine's certificate there
}

// Hello is GET /node/v1/hello: who answers.
type Hello struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"` // its CA
}

// ---- the member's side (served on the device listener only) -----------------------------------

// ServeCA: GET /node/v1/ca, without a certificate. The CA is public; the
// joining machine checks it against the code before it sends the code.
func (m *Manager) ServeCA(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, caAnswer{Name: m.Name, Version: m.Version, CA: string(m.CA.PEM)})
}

// ServeJoin: POST /node/v1/join, without a certificate (like POST /v1/pair).
func (m *Manager) ServeJoin(w http.ResponseWriter, r *http.Request) {
	var req joinRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	if !m.redeem(req.Code) {
		time.Sleep(300 * time.Millisecond) // slow down guessing
		m.audit(req.Name, "join", http.StatusUnauthorized, "code expired, used or wrong")
		writeErr(w, http.StatusUnauthorized, "the join code expired, was used already or is wrong: run `agentctl machines invite` again")
		return
	}
	if err := devices.ValidName(req.Name); err != nil || req.Name == m.Name {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("machine name %q: it must differ from %q and be lowercase letters, digits and dashes", req.Name, m.Name))
		return
	}
	theirCA, err := parseCert([]byte(req.CA))
	if err != nil || !theirCA.IsCA {
		writeErr(w, http.StatusBadRequest, "the joining machine's CA isn't a CA certificate")
		return
	}
	cert, certPEM, err := m.CA.SignCSR(req.Name, []byte(req.CSR))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := m.Devices.AddNode(req.Name, devices.NodeJoinScope, pki.SerialHex(cert), pki.Fingerprint(cert.Raw)); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	keyPEM, csrPEM, err := newKeyAndCSR(m.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	port := req.Port
	if port == "" {
		port = DefaultPort
	}
	m.mu.Lock()
	if m.pending == nil {
		m.pending = map[string]pendingKey{}
	}
	m.pending[req.Name] = pendingKey{keyPEM: keyPEM, expires: m.clock().Add(JoinCodeTTL)}
	m.mu.Unlock()
	if err := m.put(Machine{Name: req.Name, Address: net.JoinHostPort(host, port), CA: req.CA,
		CAFingerprint: pki.Fingerprint(theirCA.Raw), Key: string(keyPEM), Joined: m.clock().UTC()}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.audit(req.Name, "join", http.StatusOK, "scope "+string(devices.NodeJoinScope))
	log.Printf("node: %s joined (scope %s)", req.Name, devices.NodeJoinScope)
	writeJSON(w, http.StatusOK, joinAnswer{Name: m.Name, Cert: string(certPEM), CSR: string(csrPEM)})
}

// ServeJoinFinish: POST /node/v1/join/finish from a node that just joined:
// this machine's certificate there, for the request ServeJoin sent.
func (m *Manager) ServeJoinFinish(w http.ResponseWriter, r *http.Request, d devices.Device) {
	var b struct {
		Cert string `json:"cert"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	m.mu.Lock()
	p, ok := m.pending[d.Name]
	delete(m.pending, d.Name)
	m.mu.Unlock()
	x, known := m.Get(d.Name)
	if !ok || !known || m.clock().After(p.expires) {
		writeErr(w, http.StatusConflict, "no join in progress for "+d.Name)
		return
	}
	cert, err := parseCert([]byte(b.Cert))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	theirCA, _ := parseCert([]byte(x.CA))
	if theirCA == nil || cert.CheckSignatureFrom(theirCA) != nil || !keyMatches(p.keyPEM, cert) {
		writeErr(w, http.StatusBadRequest, "that certificate isn't for the key this machine asked about, or isn't from "+d.Name+"'s CA")
		return
	}
	x.Cert = b.Cert
	if err := m.put(x); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Hello{Name: m.Name, Version: m.Version, Fingerprint: m.CA.Fingerprint()})
}

// ServeHello: GET /node/v1/hello, for nodes.
func (m *Manager) ServeHello(w http.ResponseWriter, r *http.Request, d devices.Device) {
	writeJSON(w, http.StatusOK, Hello{Name: m.Name, Version: m.Version, Fingerprint: m.CA.Fingerprint()})
}

// ServeLeave: POST /node/v1/leave: the calling machine removed this one, or
// left; it is forgotten here too.
func (m *Manager) ServeLeave(w http.ResponseWriter, r *http.Request, d devices.Device) {
	m.forget(d.Name, "left the workspace (told by "+d.Name+")")
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) forget(name, why string) {
	_ = m.Devices.Remove(name)
	_ = m.drop(name)
	m.forgetRemote(name)
	m.audit(name, "remove", http.StatusOK, why)
	log.Printf("node: %s %s", name, why)
}

// ---- the joining machine's side -----------------------------------------------------------------

// Join joins the workspace of the machine at address with a code it made.
func (m *Manager) Join(ctx context.Context, address, code string) (Machine, error) {
	secret, fp, err := parseCode(code)
	if err != nil {
		return Machine{}, err
	}
	addr := address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), DefaultPort)
	}
	base := "https://" + addr

	// 1. Its CA, checked against the code; its listener must be signed by it.
	var ca caAnswer
	probe := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, // checked below against the code
	}}}
	resp, err := get(ctx, probe, base+"/node/v1/ca", &ca)
	if err != nil {
		return Machine{}, fmt.Errorf("can't reach %s: %w", addr, err)
	}
	theirCA, err := parseCert([]byte(ca.CA))
	if err != nil {
		return Machine{}, fmt.Errorf("%s sent no CA: %w", addr, err)
	}
	if caShort(theirCA.Raw) != fp {
		return Machine{}, errorf("%s isn't the machine that made this code (its CA fingerprint differs): check the address", addr)
	}
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 || resp.TLS.PeerCertificates[0].CheckSignatureFrom(theirCA) != nil {
		return Machine{}, errorf("%s answers with a certificate its CA didn't sign: not joining", addr)
	}
	if ca.Name == m.Name {
		return Machine{}, errorf("%s has this machine's name, %q: give one of them another name (machineName in config.json)", addr, m.Name)
	}

	// 2. Join with the code.
	keyPEM, csrPEM, err := newKeyAndCSR(m.Name)
	if err != nil {
		return Machine{}, err
	}
	var ans joinAnswer
	if _, err := post(ctx, Pinned(theirCA, nil), base+"/node/v1/join", joinRequest{
		Code: secret, Name: m.Name, CSR: string(csrPEM), CA: string(m.CA.PEM), Port: m.DevicePort,
	}, &ans); err != nil {
		return Machine{}, err
	}
	if ans.Name != ca.Name {
		return Machine{}, errorf("%s answered as %q, then as %q: not joining", addr, ca.Name, ans.Name)
	}

	// 3. Its certificate here, and how to reach it.
	cert, certPEM, err := m.CA.SignCSR(ans.Name, []byte(ans.CSR))
	if err != nil {
		return Machine{}, fmt.Errorf("%s's certificate request: %w", ans.Name, err)
	}
	if err := m.Devices.AddNode(ans.Name, devices.NodeJoinScope, pki.SerialHex(cert), pki.Fingerprint(cert.Raw)); err != nil {
		return Machine{}, err
	}
	x := Machine{Name: ans.Name, Address: addr, CA: ca.CA, CAFingerprint: pki.Fingerprint(theirCA.Raw),
		Cert: ans.Cert, Key: string(keyPEM), Joined: m.clock().UTC()}
	if err := m.put(x); err != nil {
		return Machine{}, err
	}

	// 4. Hand it its certificate, with this machine's new one.
	client, err := m.Client(x)
	if err != nil {
		return Machine{}, err
	}
	if _, err := post(ctx, client, base+"/node/v1/join/finish", map[string]string{"cert": string(certPEM)}, nil); err != nil {
		m.forget(x.Name, "join didn't finish: "+err.Error())
		return Machine{}, fmt.Errorf("joining %s didn't finish: %w", x.Name, err)
	}
	m.audit(x.Name, "join", http.StatusOK, "joined its workspace, scope "+string(devices.NodeJoinScope))
	log.Printf("node: joined %s at %s", x.Name, addr)
	return x, nil
}

// Remove takes a machine out of the workspace here and tells it, when it can
// be reached, so it forgets this one too.
func (m *Manager) Remove(ctx context.Context, name string) (told bool, err error) {
	x, ok := m.Get(name)
	if !ok {
		return false, fmt.Errorf("no machine called %q: agentctl machines lists them", name)
	}
	if c, err := m.Client(x); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = post(cctx, c, "https://"+x.Address+"/node/v1/leave", struct{}{}, nil)
		cancel()
		told = err == nil
	}
	m.forget(name, "removed here")
	return told, nil
}

// Leave takes this machine out of the workspace: every machine is told,
// when it can be reached, and forgotten here.
func (m *Manager) Leave(ctx context.Context) (names []string, err error) {
	for _, x := range m.List() {
		if _, err := m.Remove(ctx, x.Name); err != nil {
			return names, err
		}
		names = append(names, x.Name)
	}
	return names, nil
}

// Client calls a machine: its listener is checked against its CA (pinned,
// not its host name, which may differ from the address used), and this
// machine shows the certificate it holds there.
func (m *Manager) Client(x Machine) (*http.Client, error) {
	theirCA, err := parseCert([]byte(x.CA))
	if err != nil {
		return nil, err
	}
	var certs []tls.Certificate
	if x.Cert != "" {
		c, err := tls.X509KeyPair([]byte(x.Cert), []byte(x.Key))
		if err != nil {
			return nil, fmt.Errorf("the certificate for %s: %w", x.Name, err)
		}
		certs = append(certs, c)
	}
	return Pinned(theirCA, certs), nil
}

// Pinned is an HTTPS client that trusts only servers signed by ca.
func Pinned(ca *x509.Certificate, certs []tls.Certificate) *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: certs,
		InsecureSkipVerify: true, // replaced by the check against ca, below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no server certificate")
			}
			leaf, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			if err := leaf.CheckSignatureFrom(ca); err != nil {
				return errors.New("the server's certificate isn't from the machine's CA")
			}
			if now := time.Now(); now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
				return errors.New("the server's certificate has expired")
			}
			return nil
		},
	}}}
}

// ---- helpers ------------------------------------------------------------------------------------

func newKeyAndCSR(name string) (keyPEM, csrPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		return nil, nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func parseCert(p []byte) (*x509.Certificate, error) {
	b, _ := pem.Decode(p)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(b.Bytes)
}

// keyMatches: cert is for the key in keyPEM.
func keyMatches(keyPEM []byte, cert *x509.Certificate) bool {
	b, _ := pem.Decode(keyPEM)
	if b == nil {
		return false
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return false
	}
	pub, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return false
	}
	cpub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	return ok && pub.PublicKey.Equal(cpub)
}

func get(ctx context.Context, c *http.Client, url string, out any) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	return do(c, req, out)
}

func post(ctx context.Context, c *http.Client, url string, in, out any) (*http.Response, error) {
	b, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return do(c, req, out)
}

func do(c *http.Client, req *http.Request, out any) (*http.Response, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return resp, errors.New(e.Error)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
