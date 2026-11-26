package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/node"
	"github.com/brushknight/agents-deck/backend/internal/pki"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// actCtl runs agents for these tests: one agent waits on question p1; an
// answer to anything else is stale (409), as a real one would be.
type actCtl struct {
	mcpCtl
	mu         sync.Mutex
	question   string
	answers    []string
	interrupts int
}

func (c *actCtl) Answer(id, prompt, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.st.Get(id); !ok {
		return ErrNotFound
	}
	if prompt != c.question {
		return ErrStalePrompt
	}
	c.answers = append(c.answers, key)
	c.question = "" // it moved on
	return nil
}

func (c *actCtl) Interrupt(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.st.Get(id); !ok {
		return ErrNotFound
	}
	c.interrupts++
	return nil
}

func (c *actCtl) seen() (answers []string, prompts []string, interrupts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.answers...), append([]string(nil), c.prompted...), c.interrupts
}

// actPair: studio (a) acts on attic's (b) agent k3f9a2, waiting on p1.
func actPair(t *testing.T) (a, b *deck, ra, rb *relay, ctl *actCtl) {
	t.Helper()
	fast(t)
	a, b, ra, rb, _ = pair(t)
	a.s.Node.Exec, b.s.Node.Exec = nil, nil // the real one (RunNode)
	ctl = &actCtl{mcpCtl: mcpCtl{st: b.s.Store}, question: "p1"}
	b.s.Ctl = ctl
	b.s.Store.Add(&store.Entry{A: model.Agent{ID: "k3f9a2", Title: "shop api", Tool: "claude", Status: model.Idle}})
	runNode(t, a)
	runNode(t, b)
	waitFor(t, "both online", 2*time.Second, func() bool { return reachable(a, "attic") && reachable(b, "studio") })
	return
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return w
}

func TestActForwardedLive(t *testing.T) {
	a, b, _, _, ctl := actPair(t)
	h := a.s.UnixHandler(nil)
	if w := post(t, h, "/v1/agents/k3f9a2@a/answer", `{"promptId":"p1","key":"2"}`); w.Code != http.StatusNoContent {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if w := post(t, h, "/v1/agents/k3f9a2@a/prompt", `{"text":"run the tests","mode":"now"}`); w.Code >= 300 {
		t.Fatalf("prompt: %d %s", w.Code, w.Body)
	}
	if w := post(t, h, "/v1/agents/k3f9a2@a/interrupt", ``); w.Code != http.StatusNoContent {
		t.Fatalf("interrupt: %d %s", w.Code, w.Body)
	}
	answers, prompts, interrupts := ctl.seen()
	if len(answers) != 1 || answers[0] != "2" || len(prompts) != 1 || !strings.Contains(prompts[0], "run the tests") || interrupts != 1 {
		t.Fatalf("on attic: answers %v prompts %v interrupts %d", answers, prompts, interrupts)
	}
	// The same answer again is stale on attic, and studio's caller sees the 409.
	if w := post(t, h, "/v1/agents/k3f9a2@a/answer", `{"promptId":"p1","key":"2"}`); w.Code != http.StatusConflict {
		t.Fatalf("stale answer: %d %s", w.Code, w.Body)
	}
	// An unknown agent on attic: its 404, as if local.
	if w := post(t, h, "/v1/agents/zzzzzz@a/interrupt", ``); w.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d %s", w.Code, w.Body)
	}
	// Both audit logs say who asked.
	if log, _ := os.ReadFile(a.s.Audit.Path); !strings.Contains(string(log), `"action":"forward POST /v1/agents/k3f9a2@a/answer"`) {
		t.Errorf("studio audit: %s", log)
	}
	if log, _ := os.ReadFile(b.s.Audit.Path); !strings.Contains(string(log), "asked by local@studio") {
		t.Errorf("attic audit: %s", log)
	}
	// MCP's tools act through Dispatch, the same path.
	code, out := a.s.Dispatch(devices.Device{Name: "mcp:planner", Kind: devices.Network, Scope: devices.Act}, "POST", "/v1/agents/k3f9a2@a/prompt", []byte(`{"text":"from mcp","mode":"now"}`))
	if code >= 300 {
		t.Fatalf("mcp prompt: %d %s", code, out)
	}
	if _, prompts, _ := ctl.seen(); len(prompts) != 2 {
		t.Fatalf("mcp prompt didn't land: %v", prompts)
	}
}

func TestActWaitsWhileApart(t *testing.T) {
	a, b, ra, rb, ctl := actPair(t)
	h := a.s.UnixHandler(nil)
	ra.down()
	rb.down()
	waitFor(t, "attic out of reach", node.AwayAfter+time.Second, func() bool { return !reachable(a, "attic") })

	var keys []string
	for i, c := range []struct{ path, body string }{
		{"/v1/agents/k3f9a2@a/answer", `{"promptId":"p1","key":"1"}`},
		{"/v1/agents/k3f9a2@a/prompt", `{"text":"cancel me","mode":"now"}`},
		{"/v1/agents/k3f9a2@a/interrupt", ``},
	} {
		w := post(t, h, c.path, c.body)
		var r struct {
			Waiting  string
			Position int
			Key      string
		}
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		if w.Code != http.StatusAccepted || r.Waiting != "attic" || r.Position != i+1 || r.Key == "" {
			t.Fatalf("%s while apart: %d %s", c.path, w.Code, w.Body)
		}
		keys = append(keys, r.Key)
	}
	// The question is answered on attic itself meanwhile: the waiting answer will be stale.
	_ = ctl.Answer("k3f9a2", "p1", "local")
	// The prompt is taken back.
	if w := post(t, h, "/local/outbox/cancel", `{"key":"`+keys[1]+`"}`); w.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	ra.up(t, ra.addr)
	rb.up(t, rb.addr)
	waitFor(t, "delivered", node.BackoffMax+3*time.Second, func() bool { return a.s.Node.Pending("attic") == 0 && len(a.s.Node.Answers("attic")) == 2 })

	answers, prompts, interrupts := ctl.seen()
	if len(answers) != 1 || answers[0] != "local" || len(prompts) != 0 || interrupts != 1 {
		t.Fatalf("on attic: answers %v prompts %v interrupts %d", answers, prompts, interrupts)
	}
	// The stale answer's 409 is visible on studio, under its key.
	got := map[string]int{}
	for _, x := range a.s.Node.Answers("attic") {
		got[x.Key] = x.Code
	}
	if got[keys[0]] != http.StatusConflict || got[keys[2]] != http.StatusNoContent {
		t.Fatalf("answers on studio: %v", got)
	}
	_ = b
}

// A device that may only view can't act on another machine's agent either:
// studio refuses it before anything is forwarded.
func TestActViewScopeRefused(t *testing.T) {
	a, _, _, _, ctl := actPair(t)
	cert, certPEM, keyPEM, err := a.s.CA.NewDeviceKey("wall")
	if err != nil {
		t.Fatal(err)
	}
	_ = a.s.Devices.AddNetwork("wall", devices.View, pki.SerialHex(cert), pki.Fingerprint(cert.Raw))
	pair, _ := tls.X509KeyPair(certPEM, keyPEM)
	ca, _ := x509.ParseCertificate(a.s.CA.Cert.Raw)
	c := node.Pinned(ca, []tls.Certificate{pair})
	resp, err := c.Post("https://"+a.addr+"/v1/agents/k3f9a2@a/interrupt", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("view device: %d", resp.StatusCode)
	}
	if _, _, n := ctl.seen(); n != 0 {
		t.Fatal("forwarded anyway")
	}
	_ = context.Background
}
