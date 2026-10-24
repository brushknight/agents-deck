package link

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// withCA gives the test deck a CA and a network listener's identity, as
// `agentctl serve` does: hosts, port and the listener certificate.
func withCA(t *testing.T, srv *server.Server) tls.Certificate {
	t.Helper()
	dir := t.TempDir()
	ca, err := pki.LoadOrCreateCA(filepath.Join(dir, "ca.pem"), pki.FileKeystore{Dir: filepath.Join(dir, "keys")}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := ca.ServerCert(filepath.Join(dir, "srv.pem"), filepath.Join(dir, "srv.key"), []string{"deck.local", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	srv.CA = ca
	srv.DeviceHosts = []string{"deck.local", "192.0.2.10", "192.0.2.11", "192.0.2.12", "192.0.2.13"}
	srv.DevicePort = 7341
	srv.ServerFingerprint = pki.Fingerprint(cert.Certificate[0])
	return cert
}

// csrFor makes a board's key and its certificate request (DER).
func csrFor(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "board"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return key, der
}

func certReq(seq uint32, csr []byte) *linkpb.Frame {
	return &linkpb.Frame{Seq: seq, Body: &linkpb.Frame_CertRequest{CertRequest: &linkpb.CertRequest{Csr: csr}}}
}

// Pairing over USB serial: only an approved board gets a certificate, it is
// the deck CA's, it lands on the board's own registry entry, and pairing
// again replaces it.
func TestPairOverSerial(t *testing.T) {
	srv, _ := testDeck(t)
	srvCert := withCA(t, srv)
	b := start(t, srv, Binary)
	b.send(hi("AA:BB:CC:00:00:01"))
	name := b.next().GetWelcome().GetDeviceName()
	_, csr := csrFor(t)

	b.send(certReq(2, csr))
	if r := b.next(); r.Seq != 2 || r.GetResult().GetCode() != 403 {
		t.Fatalf("an unapproved board asked for a certificate: %v", r)
	}
	if d, _ := srv.Devices.Get(name); d.CertSerial != "" {
		t.Fatal("an unapproved board got a certificate")
	}

	_ = srv.Devices.Approve(name)
	if w := b.next().GetWelcome(); !w.GetApproved() || w.CertSerial != "" {
		t.Fatalf("welcome after approval: %v", w)
	}
	b.send(certReq(3, []byte("not a csr")))
	if r := b.next(); r.Seq != 3 || r.GetResult().GetCode() != 400 {
		t.Fatalf("garbage: %v", r)
	}

	b.send(certReq(4, csr))
	r := b.next()
	ci := r.GetCertIssued()
	if r.Seq != 4 || ci == nil {
		t.Fatalf("no certificate: %v", r)
	}
	cert, err := x509.ParseCertificate(ci.Cert)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: srv.CA.Pool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("the certificate isn't the deck CA's: %v", err)
	}
	fp := sha256.Sum256(srvCert.Certificate[0])
	if cert.Subject.CommonName != name || ci.DeviceName != name || ci.Port != 7341 || len(ci.Hosts) != 4 ||
		ci.Hosts[0] != "deck.local" || string(ci.ServerFingerprint) != string(fp[:]) || string(ci.Ca) != string(srv.CA.Cert.Raw) {
		t.Fatalf("certificate issued: %+v", ci)
	}
	d, _ := srv.Devices.Get(name)
	if d.Kind != devices.Serial || d.CertSerial != pki.SerialHex(cert) || d.CertSerial != ci.CertSerial || d.CertFingerprint != pki.Fingerprint(cert.Raw) {
		t.Fatalf("registry entry: %+v", d)
	}
	if got, ok := srv.Devices.ByCertSerial(ci.CertSerial); !ok || got.Name != name {
		t.Fatal("the certificate doesn't identify the board")
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"device":"`+name+`","action":"pair (deck link)"`) {
		t.Fatalf("audit: %s", audit)
	}

	// Pairing again (PEM this time) replaces the certificate.
	_, csr2 := csrFor(t)
	b.send(certReq(5, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr2})))
	ci2 := b.next().GetCertIssued()
	if ci2 == nil || ci2.CertSerial == ci.CertSerial {
		t.Fatalf("second pairing: %v", ci2)
	}
	if _, ok := srv.Devices.ByCertSerial(ci.CertSerial); ok {
		t.Error("the old certificate still works")
	}
	// The next Welcome carries the current serial, so a board knows its certificate is current.
	b.send(hi("AA:BB:CC:00:00:01"))
	if w := b.next().GetWelcome(); w.GetCertSerial() != ci2.CertSerial {
		t.Fatalf("welcome's cert_serial %q, want %q", w.GetCertSerial(), ci2.CertSerial)
	}
	// The network listener off: no certificates.
	srv.CA = nil
	b.send(certReq(6, csr))
	if r := b.next().GetResult(); r.GetCode() != 503 {
		t.Fatalf("listener off: %v", r)
	}
}

// listener serves the network listener over real TLS 1.3, /v1/link included.
func listener(t *testing.T, srv *server.Server, cert tls.Certificate, hub *Hub) string {
	t.Helper()
	srv.DeviceLink = DeviceUpgradeHandler(ServerDeck{srv}, hub)
	ts := httptest.NewUnstartedServer(srv.DeviceHandler())
	ts.TLS = srv.TLSConfig(cert)
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts.Listener.Addr().String()
}

// pairedBoard registers an approved board and pairs it: its TLS certificate.
func pairedBoard(t *testing.T, srv *server.Server, boardID string, scope devices.Scope) (string, tls.Certificate) {
	t.Helper()
	d, err := srv.Devices.Board(boardID, "round", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	_ = srv.Devices.Approve(d.Name)
	_ = srv.Devices.SetScope(d.Name, scope)
	d, _ = srv.Devices.Get(d.Name)
	key, csr := csrFor(t)
	der, _, code, err := srv.PairBoard(d, csr)
	if err != nil || code != 200 {
		t.Fatalf("pair: %d %v", code, err)
	}
	return d.Name, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// dialLink is a board on Wi-Fi: TLS 1.3 with its certificate, verifying the
// deck against its CA, then GET /v1/link with Upgrade: deck-link.
func dialLink(t *testing.T, addr string, srv *server.Server, cert *tls.Certificate) (net.Conn, *bufio.Reader, error) {
	t.Helper()
	cfg := &tls.Config{RootCAs: srv.CA.Pool(), ServerName: "deck.local", MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprint(conn, "GET /v1/link HTTP/1.1\r\nHost: deck.local\r\nUpgrade: deck-link\r\nConnection: Upgrade\r\n\r\n")
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		if l == "\r\n" {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	if !strings.Contains(status, " 101 ") {
		conn.Close()
		return nil, nil, fmt.Errorf("%s", strings.TrimSpace(status))
	}
	return conn, br, nil
}

// tlsBoard is the test's board over a TLS stream.
type tlsBoard struct {
	*board
	r io.Reader
}

func (b *tlsBoard) next() *linkpb.Frame {
	b.t.Helper()
	buf := make([]byte, 4096)
	for {
		for len(b.q) > 0 {
			f := b.q[0]
			b.q = b.q[1:]
			if f.Frame.GetPing() == nil {
				return f.Frame
			}
		}
		_ = b.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := b.r.Read(buf)
		if err != nil {
			b.t.Fatalf("waiting for a frame: %v", err)
		}
		b.q = append(b.q, b.dec.Feed(buf[:n])...)
	}
}

// A paired board runs a normal session over TLS: same name, scope and audit
// log as over USB. Revoking it ends the session and fails the next handshake.
func TestLinkOverTLS(t *testing.T) {
	srv, ctl := testDeck(t)
	srvCert := withCA(t, srv)
	hub := &Hub{}
	addr := listener(t, srv, srvCert, hub)
	name, cert := pairedBoard(t, srv, "AA:BB:CC:00:00:02", devices.Act)

	conn, br, err := dialLink(t, addr, srv, &cert)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := &tlsBoard{&board{t: t, conn: conn}, br}
	b.send(hi("AA:BB:CC:00:00:02"))
	w := b.next().GetWelcome()
	if !w.GetApproved() || w.DeviceName != name || w.Scope != linkpb.Scope_SCOPE_ACT || w.CertSerial == "" {
		t.Fatalf("welcome over Wi-Fi: %v", w)
	}
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{Fields: 1 | 4}}})
	if sb := b.next().GetSnapshotBegin(); sb == nil || sb.AgentCount != 3 {
		t.Fatalf("snapshot: %v", sb)
	}
	for f := b.next(); f.GetSnapshotEnd() == nil; f = b.next() {
	}
	b.send(&linkpb.Frame{Seq: 3, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a1", PromptId: "p1", Key: "1"}}})
	if r := b.next(); r.Seq != 3 || r.GetResult().GetCode() != 204 {
		t.Fatalf("answer over Wi-Fi: %v", r)
	}
	b.send(&linkpb.Frame{Seq: 4, Body: &linkpb.Frame_Dismiss{Dismiss: &linkpb.Dismiss{AgentId: "a1"}}})
	if r := b.next(); r.GetResult().GetCode() != 403 {
		t.Fatalf("scope act must refuse dismiss: %v", r)
	}
	if len(ctl.calls) != 1 || ctl.calls[0] != "answer a1 p1 1" {
		t.Fatalf("calls: %v", ctl.calls)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"device":"`+name+`","action":"POST /v1/agents/a1/answer","status":204`) {
		t.Fatalf("audit: %s", audit)
	}
	// It reports its network; `agentctl link` sees it on Wi-Fi.
	b.send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: &linkpb.NetStatus{Transport: linkpb.Transport_TRANSPORT_WIFI,
		Wifi: linkpb.WifiState_WIFI_CONNECTED, Ip: "192.0.2.23", Rssi: -58, HasCert: true}}})
	b.send(&linkpb.Frame{Seq: 5, Body: &linkpb.Frame_Ping{Ping: &linkpb.Ping{TimeMs: 1}}})
	b.next() // the pong: the NetStatus before it has been handled
	links := hub.Links()
	if len(links) != 1 || links[0].Board != name || links[0].Transport != OverWifi || links[0].Net == nil ||
		links[0].Net.IP != "192.0.2.23" || links[0].Net.RSSI != -58 || links[0].Net.Wifi != "connected" {
		t.Fatalf("links: %+v", links)
	}
	// No pairing and no Wi-Fi credentials over Wi-Fi.
	_, csr := csrFor(t)
	b.send(certReq(6, csr))
	if r := b.next(); r.GetResult().GetCode() != 403 {
		t.Fatalf("pairing over Wi-Fi: %v", r)
	}
	if code, msg := hub.SendWifi(name, "lab", "password1", false); code != 409 || !strings.Contains(msg, "USB") {
		t.Fatalf("wifi over wifi: %d %s", code, msg)
	}

	// Revoked: the session ends (within a recheck) and the handshake fails after.
	_ = srv.Devices.Revoke(name)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	for {
		if _, err := br.Read(buf); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("the session outlived the revocation")
			}
			break
		}
	}
	if c, _, err := dialLink(t, addr, srv, &cert); err == nil {
		c.Close()
		t.Fatal("a revoked board connected over TLS")
	}
}

// A certificate is one board's: it can't say Hello as another, it can't be a
// network device's, and without one there is no link.
func TestLinkOverTLSIdentity(t *testing.T) {
	srv, _ := testDeck(t)
	srvCert := withCA(t, srv)
	addr := listener(t, srv, srvCert, &Hub{})
	nameA, certA := pairedBoard(t, srv, "AA:BB:CC:00:00:0A", devices.Act)
	nameB, _ := pairedBoard(t, srv, "AA:BB:CC:00:00:0B", devices.Admin)

	conn, br, err := dialLink(t, addr, srv, &certA)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := &tlsBoard{&board{t: t, conn: conn}, br}
	b.send(hi("AA:BB:CC:00:00:0B")) // B's board id with A's certificate
	r := b.next()
	if r.GetResult().GetCode() != 403 || !strings.Contains(r.GetResult().GetError(), nameA) {
		t.Fatalf("A claiming B: %v", r)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := br.Read(make([]byte, 64)); err == nil {
		t.Fatal("the connection stayed open")
	}
	if d, _ := srv.Devices.Get(nameB); d.LastSeen.After(time.Now().Add(-time.Minute)) && !d.LastSeen.IsZero() {
		t.Errorf("%s was seen through A's certificate", nameB)
	}

	// A network device's certificate isn't a board's.
	pcert, pcertPEM, pkeyPEM, _ := srv.CA.NewDeviceKey("panel")
	_ = srv.Devices.AddNetwork("panel", devices.Act, pki.SerialHex(pcert), pki.Fingerprint(pcert.Raw))
	panel, _ := tls.X509KeyPair(pcertPEM, pkeyPEM)
	if _, _, err := dialLink(t, addr, srv, &panel); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("a network device on /v1/link: %v", err)
	}
	// No certificate: 401, as for the API.
	if _, _, err := dialLink(t, addr, srv, nil); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("no certificate: %v", err)
	}
	// An unapproved board's certificate (revoked, then approved: it must pair again).
	_ = srv.Devices.Revoke(nameA)
	_ = srv.Devices.Approve(nameA)
	if c, _, err := dialLink(t, addr, srv, &certA); err == nil {
		c.Close()
		t.Fatal("a certificate revoked once works again after approval")
	}
}

// `agentctl link wifi` reaches the board only over USB serial; the password
// goes to the board and nowhere else.
func TestLinkWifiOverSerialOnly(t *testing.T) {
	srv, _ := testDeck(t)
	hub := &Hub{}
	srv.LinkWifi = hub.SendWifi
	unix := srv.UnixHandler(fakeLocal{&fakeCtl{}})
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/local/link/wifi", strings.NewReader(body))
		w := httptest.NewRecorder()
		unix.ServeHTTP(w, r)
		return w
	}
	// Not connected at all.
	if w := post(`{"device":"round-000003","ssid":"lab","password":"s3cret-pass"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "USB serial") {
		t.Fatalf("not connected: %d %s", w.Code, w.Body)
	}
	if w := post(`{"device":"round-000003","ssid":"lab","password":"short"}`); w.Code != 400 {
		t.Fatalf("bad password accepted: %d", w.Code)
	}

	// A board on serial (the unix-socket link, as `agentctl link sim`).
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); deckEnd.Close(); boardEnd.Close() }()
	s := NewSession(ServerDeck{srv}, deckEnd, "test")
	s.Hub, s.RecheckEvery = hub, 50*time.Millisecond
	go func() { _ = s.Run(ctx) }()
	got := make(chan *linkpb.WifiConfig, 1)
	gb := NewBoard(boardEnd, &linkpb.Hello{Protocol: 1, BoardId: "AA:BB:CC:00:00:03", Type: "round", Firmware: "1"}, 1)
	gb.Wifi = func(w *linkpb.WifiConfig) (uint32, string) { got <- w; return 204, "" }
	go func() { _ = gb.Run() }()
	waitFor(t, func() bool { return len(hub.Links()) == 1 })
	name := hub.Links()[0].Board
	if w := post(`{"device":"` + name + `","ssid":"lab","password":"s3cret-pass"}`); w.Code != 403 {
		t.Fatalf("unapproved board: %d %s", w.Code, w.Body)
	}
	_ = srv.Devices.Approve(name)
	waitFor(t, func() bool { return hub.serial(name) != nil })
	if w := post(`{"device":"` + name + `","ssid":"lab","password":"s3cret-pass"}`); w.Code != 204 {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	if w := <-got; w.Ssid != "lab" || w.Password != "s3cret-pass" || w.Forget {
		t.Fatalf("the board got %v", w)
	}
	if w := post(`{"device":"` + name + `","forget":true}`); w.Code != 204 {
		t.Fatalf("forget: %d %s", w.Code, w.Body)
	}
	if w := <-got; !w.Forget {
		t.Fatalf("forget: %v", w)
	}
	audit, _ := os.ReadFile(srv.Audit.Path)
	if !strings.Contains(string(audit), `"action":"wifi `+name+`","status":204,"detail":"set"`) ||
		strings.Contains(string(audit), "s3cret") || strings.Contains(string(audit), "lab") {
		t.Fatalf("audit: %s", audit)
	}
	// A board without Wi-Fi says so.
	gb.Wifi = nil
	if w := post(`{"device":"` + name + `","ssid":"lab","password":"s3cret-pass"}`); w.Code != 501 {
		t.Fatalf("no Wi-Fi: %d %s", w.Code, w.Body)
	}
}

type fakeLocal struct{ *fakeCtl }

func (fakeLocal) Hook(string, []byte) error                            { return nil }
func (fakeLocal) Create(server.CreateOpts) (string, error)             { return "", nil }
func (fakeLocal) ResumeSession(string, string, string) (string, error) { return "", nil }
func (fakeLocal) HistoryJSON() any                                     { return nil }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

var _ = http.StatusOK
