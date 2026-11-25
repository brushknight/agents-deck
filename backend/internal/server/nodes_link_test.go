package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/node"
)

// relay forwards TCP to a listener; the test cuts it, holds it silent, or
// brings it back on the same address.
type relay struct {
	addr, to string
	mu       sync.Mutex
	ln       net.Listener
	conns    []net.Conn
	silent   bool
}

func newRelay(t *testing.T, to string) *relay {
	r := &relay{to: to}
	r.up(t, "127.0.0.1:0")
	t.Cleanup(func() { r.down() })
	return r
}

func (r *relay) up(t *testing.T, addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.ln, r.addr = ln, ln.Addr().String()
	r.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			silent := r.silent
			r.conns = append(r.conns, c)
			r.mu.Unlock()
			if silent {
				continue // accept, never answer
			}
			b, err := net.Dial("tcp", r.to)
			if err != nil {
				c.Close()
				continue
			}
			r.mu.Lock()
			r.conns = append(r.conns, b)
			r.mu.Unlock()
			go func() { _, _ = io.Copy(b, c); b.Close() }()
			go func() { _, _ = io.Copy(c, b); c.Close() }()
		}
	}()
}

// down closes the listener and every connection through it.
func (r *relay) down() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln != nil {
		r.ln.Close()
		r.ln = nil
	}
	for _, c := range r.conns {
		c.Close()
	}
	r.conns = nil
}

// fast shortens the link's timings for a test.
func fast(t *testing.T) {
	old := []any{node.HeartbeatEvery, node.AwayAfter, node.BackoffMin, node.BackoffMax, node.WatchEvery, node.AskAfter}
	node.HeartbeatEvery, node.AwayAfter, node.BackoffMin, node.BackoffMax, node.WatchEvery, node.AskAfter =
		100*time.Millisecond, 700*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond, 50*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() {
		node.HeartbeatEvery, node.AwayAfter, node.BackoffMin, node.BackoffMax, node.WatchEvery, node.AskAfter =
			old[0].(time.Duration), old[1].(time.Duration), old[2].(time.Duration), old[3].(time.Duration), old[4].(time.Duration), old[5].(time.Duration)
	})
}

// pair joins b to a, and routes each machine's calls to the other through a
// relay of its own. Each deck records, in order, the calls it ran.
func pair(t *testing.T) (a, b *deck, ra, rb *relay, ran map[*deck]*[]string) {
	t.Helper()
	a, b = newDeck(t, "studio"), newDeck(t, "attic")
	code, _, _ := a.s.Node.Invite()
	if _, err := b.s.Node.Join(context.Background(), a.addr, code); err != nil {
		t.Fatal(err)
	}
	ra, rb = newRelay(t, a.addr), newRelay(t, b.addr) // to a, to b
	_ = b.s.Node.SetAddress("studio", ra.addr)
	_ = a.s.Node.SetAddress("attic", rb.addr)
	ran = map[*deck]*[]string{}
	for _, d := range []*deck{a, b} {
		mu := &ranMu
		list := &[]string{}
		ran[d] = list
		d.s.Node.Dir = d.dir
		d.s.Node.Exec = func(from devices.Device, c node.Call) (int, []byte) {
			mu.Lock()
			*list = append(*list, string(c.Body))
			mu.Unlock()
			return http.StatusOK, []byte(`{"ok":true}`)
		}
	}
	return
}

// ranMu guards the lists of calls each deck ran (pair); ranList reads one.
var ranMu sync.Mutex

func ranList(l *[]string) []string {
	ranMu.Lock()
	defer ranMu.Unlock()
	return append([]string(nil), *l...)
}

func run(t *testing.T, d *deck) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.s.Node.Run(ctx)
}

func waitFor(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(within)
	for time.Now().Before(end) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("not within %s: %s", within, what)
}

func reachable(d *deck, name string) bool {
	for _, s := range d.s.Node.States() {
		if s.Name == name {
			return s.Reachable
		}
	}
	return false
}

func TestLinkOnlineAwayBack(t *testing.T) {
	fast(t)
	a, b, ra, rb, _ := pair(t)
	run(t, a)
	run(t, b)
	waitFor(t, "both online", 2*time.Second, func() bool { return reachable(a, "attic") && reachable(b, "studio") })
	for _, s := range a.s.Node.States() {
		if s.Version != "0.6.0" {
			t.Errorf("state: %+v", s)
		}
	}
	// The relays cut: each sees the other out of reach within AwayAfter (and a bit).
	ra.down()
	rb.down()
	waitFor(t, "both out of reach", node.AwayAfter+time.Second, func() bool { return !reachable(a, "attic") && !reachable(b, "studio") })
	for _, s := range a.s.Node.States() {
		if s.AwaySince.IsZero() {
			t.Errorf("no awaySince: %+v", s)
		}
	}
	// Back on the same addresses: online again within the backoff.
	ra.up(t, ra.addr)
	rb.up(t, rb.addr)
	waitFor(t, "both online again", node.BackoffMax+time.Second, func() bool { return reachable(a, "attic") && reachable(b, "studio") })
}

func TestOutboxDeliversInOrderOnceAfterReconnect(t *testing.T) {
	fast(t)
	a, b, ra, rb, ran := pair(t)
	ra.down()
	rb.down()
	// 100 calls while apart, half before the sender restarts.
	for i := range 100 {
		if i == 50 { // the sender restarts: a new manager over the same files
			old := a.s.Node
			a.s.Node = &node.Manager{Name: old.Name, Version: old.Version, CA: old.CA, Devices: old.Devices, Audit: old.Audit, Path: old.Path, DevicePort: old.DevicePort}
			a.s.Node.Dir, a.s.Node.Exec = old.Dir, old.Exec
		}
		if _, err := a.s.Node.Enqueue("attic", "local", "POST", "/v1/test", []byte(`"`+string(rune('a'+i%26))+`-`+strconv.Itoa(i)+`"`)); err != nil {
			t.Fatal(err)
		}
	}
	cancelled, _ := a.s.Node.Enqueue("attic", "local", "POST", "/v1/test", []byte(`"cancelled"`))
	if !a.s.Node.Cancel("attic", cancelled.Key) {
		t.Fatal("cancel before it goes")
	}
	if n := a.s.Node.Pending("attic"); n != 100 {
		t.Fatalf("waiting: %d", n)
	}
	run(t, a)
	run(t, b)
	ra.up(t, ra.addr)
	rb.up(t, rb.addr)
	waitFor(t, "all delivered", 5*time.Second, func() bool { return a.s.Node.Pending("attic") == 0 && len(ranList(ran[b])) == 100 })
	for i, body := range ranList(ran[b]) {
		if want := `"` + string(rune('a'+i%26)) + `-` + strconv.Itoa(i) + `"`; body != want {
			t.Fatalf("call %d: %s, want %s (order)", i, body, want)
		}
		if body == `"cancelled"` {
			t.Fatal("a cancelled call went out")
		}
	}
	if st := a.s.Node.States(); st[0].Waiting != 0 {
		t.Errorf("waiting after delivery: %+v", st)
	}
}

// A call sent twice (the answer was lost) runs once; the second time it gets
// the first answer.
func TestOutboxSameKeyRunsOnce(t *testing.T) {
	a, b, _, _, ran := pair(t)
	_ = a
	studio, _ := b.s.Devices.Get("studio")
	body, _ := json.Marshal(map[string]any{"calls": []node.Call{{Key: "k1", Seq: 1, By: "local", Method: "POST", Path: "/v1/test", Body: []byte(`"once"`)}}})
	for range 2 {
		w := httptest.NewRecorder()
		b.s.Node.ServeOutbox(w, httptest.NewRequest("POST", "/node/v1/outbox", bytes.NewReader(body)), studio)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"key":"k1","code":200`) {
			t.Fatalf("answer: %d %s", w.Code, w.Body)
		}
	}
	if len(ranList(ran[b])) != 1 {
		t.Fatalf("ran %d times", len(ranList(ran[b])))
	}
}

// machine.ask goes out once per absence with "Ask me", never with "Do nothing".
func TestMachineAsk(t *testing.T) {
	fast(t)
	for _, onAway := range []string{node.OnAwayAsk, node.OnAwayNothing} {
		a, _, ra, rb, _ := pair(t)
		ra.down()
		rb.down()
		a.s.Node.OnAway = func() string { return onAway }
		events, cancel := a.s.Node.Subscribe()
		run(t, a)
		asks := 0
		deadline := time.After(node.AskAfter + node.AwayAfter + time.Second)
	loop:
		for {
			select {
			case e := <-events:
				if e.Kind == "machine.ask" {
					asks++
					if e.Machine != "attic" || e.Since == "" {
						t.Errorf("event: %+v", e)
					}
				}
			case <-deadline:
				break loop
			}
		}
		cancel()
		if want := map[string]int{node.OnAwayAsk: 1, node.OnAwayNothing: 0}[onAway]; asks != want {
			t.Errorf("onAway %s: %d asks, want %d", onAway, asks, want)
		}
	}
}

// Nothing waits on the link: with the other machine silent (connections
// accepted, never answered), this machine's API answers at once.
func TestLinkNeverBlocks(t *testing.T) {
	fast(t)
	a, _, _, rb, _ := pair(t)
	rb.down()
	rb.silent = true
	rb.up(t, rb.addr)
	run(t, a)
	_, _ = a.s.Node.Enqueue("attic", "local", "POST", "/v1/test", []byte(`"x"`))
	time.Sleep(300 * time.Millisecond) // the link is now stuck on the silent relay
	h := a.s.UnixHandler(nil)
	for _, path := range []string{"/v1/state", "/v1/machines", "/local/machines"} {
		start := time.Now()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK || time.Since(start) > 200*time.Millisecond {
			t.Errorf("%s: %d after %s", path, w.Code, time.Since(start))
		}
	}
}

// Every word for "can't be reached" comes from node.AwayWord.
func TestAwayWordOnlyFromTheConstant(t *testing.T) {
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(strings.TrimSpace(line), "//") // comments may say it
			if strings.Contains(code, `"out of reach"`) && !strings.Contains(code, "AwayWord = ") {
				t.Errorf("%s: %s", p, strings.TrimSpace(line))
			}
		}
		return nil
	})
}

// Only one direction open (attic can't dial studio, studio can dial attic):
// attic shows studio out of reach, one way, and its outbox waits; studio,
// whose own dial works, shows attic online. Being called isn't reachable.
func TestLinkOneWay(t *testing.T) {
	fast(t)
	a, b, ra, _, ran := pair(t) // ra: attic → studio
	ra.down()
	run(t, a)
	run(t, b)
	_, _ = b.s.Node.Enqueue("studio", "local", "POST", "/v1/test", []byte(`"waits"`))
	waitFor(t, "studio sees attic online", 2*time.Second, func() bool { return reachable(a, "attic") })
	waitFor(t, "attic sees studio one way", node.AwayAfter+time.Second, func() bool {
		for _, s := range b.s.Node.States() {
			if s.Name == "studio" {
				return !s.Reachable && s.OneWay && s.Waiting == 1
			}
		}
		return false
	})
	if len(ranList(ran[a])) != 0 {
		t.Fatalf("attic's call reached studio over a closed direction: %v", ranList(ran[a]))
	}
	// The direction opens: the call goes.
	ra.up(t, ra.addr)
	waitFor(t, "attic's call delivered", node.BackoffMax+2*time.Second, func() bool { return len(ranList(ran[a])) == 1 })
}
