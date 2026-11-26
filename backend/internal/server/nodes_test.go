package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/node"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// deck is one machine of a test workspace: its own data folder, CA, devices
// and a real TLS device listener on 127.0.0.1.
type deck struct {
	s    *Server
	ts   *httptest.Server
	addr string
	dir  string
}

func newDeck(t *testing.T, name string) *deck {
	t.Helper()
	dir := t.TempDir()
	ca, err := pki.LoadOrCreateCA(filepath.Join(dir, "ca.pem"), pki.FileKeystore{Dir: filepath.Join(dir, "keys")}, name)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := ca.ServerCert(filepath.Join(dir, "server-cert.pem"), filepath.Join(dir, "server-key.pem"), []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store.New("", name, "0.6.0"), CA: ca,
		Devices: &devices.Registry{Path: filepath.Join(dir, "devices.json")},
		Audit:   &devices.Audit{Path: filepath.Join(dir, "audit.log")}}
	ts := httptest.NewUnstartedServer(s.DeviceHandler())
	ts.TLS = s.TLSConfig(cert)
	ts.StartTLS()
	t.Cleanup(ts.Close)
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	s.Node = &node.Manager{Name: name, Version: "0.6.0", CA: ca, Devices: s.Devices, Audit: s.Audit,
		Path: filepath.Join(dir, "machines.json"), DevicePort: port}
	return &deck{s: s, ts: ts, addr: ts.Listener.Addr().String(), dir: dir}
}

// call asks d's listener with the certificate the caller holds there
// (from its machines.json), or with a given one.
func (d *deck) hello(t *testing.T, from *deck, cert *tls.Certificate) int {
	t.Helper()
	var client *http.Client
	caCert, _ := x509.ParseCertificate(d.s.CA.Cert.Raw)
	if cert != nil {
		client = node.Pinned(caCert, []tls.Certificate{*cert})
	} else {
		x, ok := from.s.Node.Get(d.s.Node.Name)
		if !ok {
			return -1
		}
		c, err := from.s.Node.Client(x)
		if err != nil {
			t.Fatal(err)
		}
		client = c
	}
	resp, err := client.Get("https://" + d.addr + "/node/v1/hello")
	if err != nil {
		return 0 // the TLS handshake refused it
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestMachinesJoinBothWays(t *testing.T) {
	a, b := newDeck(t, "studio"), newDeck(t, "attic")
	code, _, err := a.s.Node.Invite()
	if err != nil {
		t.Fatal(err)
	}
	x, err := b.s.Node.Join(context.Background(), a.addr, code)
	if err != nil {
		t.Fatal(err)
	}
	if x.Name != "studio" {
		t.Fatalf("joined %q", x.Name)
	}
	// Each lists the other, as a node with the join scope, with a certificate to call it.
	for _, p := range []struct{ me, other *deck }{{a, b}, {b, a}} {
		m, ok := p.me.s.Node.Get(p.other.s.Node.Name)
		if !ok || m.Cert == "" || m.Key == "" || m.CA == "" {
			t.Fatalf("%s doesn't know %s fully: %+v", p.me.s.Node.Name, p.other.s.Node.Name, m)
		}
		d, ok := p.me.s.Devices.Get(p.other.s.Node.Name)
		if !ok || d.Kind != devices.Node || d.Scope != devices.NodeJoinScope {
			t.Fatalf("%s's device for %s: %+v", p.me.s.Node.Name, p.other.s.Node.Name, d)
		}
		if c := p.other.hello(t, p.me, nil); c != http.StatusOK {
			t.Fatalf("%s calling %s: %d", p.me.s.Node.Name, p.other.s.Node.Name, c)
		}
	}
	// The code was used: it doesn't work again.
	if _, err := newDeck(t, "garage").s.Node.Join(context.Background(), a.addr, code); err == nil || !strings.Contains(err.Error(), "expired, was used") {
		t.Fatalf("reused code: %v", err)
	}
	// The audit log shows the join on both sides.
	for _, d := range []*deck{a, b} {
		if log, _ := os.ReadFile(d.s.Audit.Path); !strings.Contains(string(log), `"action":"join","status":200`) {
			t.Errorf("%s audit: %s", d.s.Node.Name, log)
		}
	}

	// Removed on one side: the other is told, and neither can call the other.
	held, _ := b.s.Node.Get("studio") // attic's certificate for studio, before it forgets it
	bCert, err := tls.X509KeyPair([]byte(held.Cert), []byte(held.Key))
	if err != nil {
		t.Fatal(err)
	}
	told, err := a.s.Node.Remove(context.Background(), "attic")
	if err != nil || !told {
		t.Fatalf("remove: %v told=%v", err, told)
	}
	if _, ok := b.s.Node.Get("studio"); ok {
		t.Error("attic still knows studio")
	}
	if c := a.hello(t, nil, &bCert); c == http.StatusOK {
		t.Errorf("removed machine still answered: %d", c)
	}
	if log, _ := os.ReadFile(a.s.Audit.Path); !strings.Contains(string(log), `"action":"remove"`) {
		t.Errorf("audit lacks the removal: %s", log)
	}
}

func TestMachinesJoinRefused(t *testing.T) {
	a, b := newDeck(t, "studio"), newDeck(t, "attic")
	ctx := context.Background()

	// A code with another machine's fingerprint: refused before the secret is sent.
	code, _, _ := a.s.Node.Invite()
	other, _, _ := newDeck(t, "garage").s.Node.Invite()
	forged := code[:strings.LastIndexByte(code, '-')] + other[strings.LastIndexByte(other, '-'):]
	if _, err := b.s.Node.Join(ctx, a.addr, forged); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("wrong fingerprint: %v", err)
	}
	// The real code still works (the secret never left).
	if _, err := b.s.Node.Join(ctx, a.addr, code); err != nil {
		t.Fatalf("after a refused forgery: %v", err)
	}

	// Expired: past five minutes.
	c := newDeck(t, "garage")
	code, _, _ = a.s.Node.Invite()
	a.s.Node.SetClock(func() time.Time { return time.Now().Add(node.JoinCodeTTL + time.Second) })
	if _, err := c.s.Node.Join(ctx, a.addr, code); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired code: %v", err)
	}
	a.s.Node.SetClock(nil)

	// Not a code at all.
	if _, err := c.s.Node.Join(ctx, a.addr, "hello"); err == nil || !strings.Contains(err.Error(), "isn't a join code") {
		t.Fatalf("garbage: %v", err)
	}
}

// /node/v1 answers machines only: a board's or a phone's certificate gets
// 403, and nothing of it is on the loopback dashboard or the unix socket.
func TestNodeRoutesForMachinesOnly(t *testing.T) {
	a, b := newDeck(t, "studio"), newDeck(t, "attic")
	code, _, _ := a.s.Node.Invite()
	if _, err := b.s.Node.Join(context.Background(), a.addr, code); err != nil {
		t.Fatal(err)
	}
	cert, certPEM, keyPEM, err := a.s.CA.NewDeviceKey("phone")
	if err != nil {
		t.Fatal(err)
	}
	_ = a.s.Devices.AddNetwork("phone", devices.Admin, pki.SerialHex(cert), pki.Fingerprint(cert.Raw))
	phone, _ := tls.X509KeyPair(certPEM, keyPEM)
	if c := a.hello(t, nil, &phone); c != http.StatusForbidden {
		t.Fatalf("a phone on /node/v1: %d", c)
	}
	if c := a.hello(t, b, nil); c != http.StatusOK {
		t.Fatalf("the node: %d", c)
	}
	for name, h := range map[string]http.Handler{"loopback": a.s.LocalHandler(), "unix socket": a.s.UnixHandler(nil)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/node/v1/hello", nil))
		if w.Code == http.StatusOK {
			t.Errorf("/node/v1 on the %s: %d", name, w.Code)
		}
	}
}

// The scope a machine gets comes from one setting: flip it, and nothing else.
func TestMachinesJoinScopeSetting(t *testing.T) {
	old := devices.NodeJoinScope
	devices.NodeJoinScope = devices.Act
	defer func() { devices.NodeJoinScope = old }()
	a, b := newDeck(t, "studio"), newDeck(t, "attic")
	code, _, _ := a.s.Node.Invite()
	if _, err := b.s.Node.Join(context.Background(), a.addr, code); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ me, other *deck }{{a, b}, {b, a}} {
		if d, _ := p.me.s.Devices.Get(p.other.s.Node.Name); d.Scope != devices.Act {
			t.Errorf("%s: scope %s", p.me.s.Node.Name, d.Scope)
		}
	}
}
