package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memKeys struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (k *memKeys) Load(n string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v, ok := k.m[n]; ok {
		return v, nil
	}
	return nil, errors.New("no key stored")
}
func (k *memKeys) Save(n string, v []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[n] = v
	return nil
}
func (k *memKeys) Delete(n string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, n)
	return nil
}

type svcHarness struct {
	*Service
	keys *memKeys
	mu   sync.Mutex
	got  []delivered
}

func newService(t *testing.T, f *fakeAPI) *svcHarness {
	dir := t.TempDir()
	h := &svcHarness{keys: &memKeys{m: map[string][]byte{}}}
	h.Service = &Service{Keys: h.keys, Path: filepath.Join(dir, "telegram.json"), Dir: filepath.Join(dir, "telegram"),
		Base: f.srv.URL, Logf: t.Logf,
		Deliver: func(agent, text string) (Result, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.got = append(h.got, delivered{agent, text})
			return Result{}, nil
		},
		Agent: func(id string) (AgentInfo, bool) {
			switch id {
			case "q601r3", "k3f9a2":
				return AgentInfo{Title: "Desk " + id, Status: "idle"}, true
			case "ended1":
				return AgentInfo{Status: "exited", Ended: true}, true
			}
			return AgentInfo{}, false
		}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *svcHarness) do(t *testing.T, method, body string) (int, string) {
	t.Helper()
	path := "/local/telegram"
	if strings.HasPrefix(body, "?") { // a GET's query
		path, body = path+body, ""
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func (h *svcHarness) prompts() []delivered {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]delivered(nil), h.got...)
}

func TestServiceSetupLinkStatusOff(t *testing.T) {
	f := newFake(t)
	h := newService(t, f)

	if code, out := h.do(t, "POST", `{"op":"link","agent":"q601r3"}`); code != 400 || !strings.Contains(out, "no Telegram bridge yet") {
		t.Fatalf("link before setup: %d %s", code, out)
	}
	if code, out := h.do(t, "POST", `{"op":"setup","token":"999:wrong"}`); code != 400 || strings.Contains(out, "999:wrong") {
		t.Fatalf("wrong token: %d %s", code, out)
	}
	code, out := h.do(t, "POST", `{"op":"setup","token":"`+testToken+`"}`)
	var st BridgeStatus
	json.Unmarshal([]byte(out), &st)
	if code != 200 || strings.Contains(out, testToken) || st.ID != "default" || !st.On || st.Bot != "@test_deck_bot" || len(st.PairCode) != 8 {
		t.Fatalf("setup: %d %s", code, out)
	}
	if v, _ := h.keys.Load(TokenName("default")); string(v) != testToken {
		t.Fatal("token not in the keystore under the bridge's name")
	}
	for body, want := range map[string]int{
		`{"op":"link","agent":"nosuch"}`: 404,
		`{"op":"link","agent":"ended1"}`: 409,
		`{"op":"link"}`:                  400,
	} {
		if code, _ := h.do(t, "POST", body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	if code, out := h.do(t, "POST", `{"op":"link","agent":"q601r3"}`); code != 200 || !strings.Contains(out, `"agent":"q601r3"`) {
		t.Fatalf("link: %d %s", code, out)
	}
	if code, out := h.do(t, "POST", `{"op":"user","userId":42}`); code != 200 || !strings.Contains(out, `"userId":42`) {
		t.Fatalf("user: %d %s", code, out)
	}
	if code, out := h.do(t, "GET", ""); code != 200 || strings.Contains(out, testToken) || strings.Contains(out, "pairCode") || !strings.Contains(out, `"id":"default"`) {
		t.Fatalf("status: %d %s", code, out)
	}
	b, _ := os.ReadFile(h.Path)
	if strings.Contains(string(b), testToken) || !strings.Contains(string(b), `"agent": "q601r3"`) || !strings.Contains(string(b), `"userId": 42`) {
		t.Fatalf("telegram.json:\n%s", b)
	}
	if fi, _ := os.Stat(h.Path); fi.Mode().Perm() != 0o600 {
		t.Errorf("telegram.json is %v", fi.Mode().Perm())
	}
	if code, out := h.do(t, "POST", `{"op":"off"}`); code != 200 || strings.Contains(out, `"on":true`) {
		t.Fatalf("off: %d %s", code, out)
	}
	if _, err := h.keys.Load(TokenName("default")); err == nil {
		t.Fatal("token still stored after off")
	}
	if code, out := h.do(t, "POST", `{"op":"pair"}`); code != 409 {
		t.Fatalf("pair with no token: %d %s", code, out)
	}
}

// Two bridges, two bots, two agents: separate offsets, inboxes and answers.
// One bot can't serve two bridges; with two, --bridge is needed.
func TestTwoBridges(t *testing.T) {
	old := DefaultDebounce
	DefaultDebounce = 50 * time.Millisecond
	defer func() { DefaultDebounce = old }()
	f := newFake(t)
	h := newService(t, f)

	mustOK := func(body string) string {
		t.Helper()
		code, out := h.do(t, "POST", body)
		if code != 200 {
			t.Fatalf("%s: %d %s", body, code, out)
		}
		return out
	}
	mustOK(`{"op":"setup","bridge":"design","token":"` + testToken + `"}`)
	if code, out := h.do(t, "POST", `{"op":"setup","bridge":"ops","token":"`+testToken+`"}`); code != 409 || !strings.Contains(out, "bridge design already uses") {
		t.Fatalf("same bot twice: %d %s", code, out)
	}
	mustOK(`{"op":"setup","bridge":"ops","token":"` + testToken2 + `"}`)
	if code, out := h.do(t, "POST", `{"op":"link","agent":"q601r3"}`); code != 400 || !strings.Contains(out, "--bridge") {
		t.Fatalf("no bridge named with two: %d %s", code, out)
	}
	if code, _ := h.do(t, "POST", `{"op":"setup","bridge":"Bad Id","token":"x"}`); code != 400 {
		t.Fatal("a bad id was taken")
	}
	mustOK(`{"op":"link","bridge":"design","agent":"q601r3"}`)
	mustOK(`{"op":"link","bridge":"ops","agent":"k3f9a2"}`)
	mustOK(`{"op":"user","bridge":"design","userId":42}`)
	mustOK(`{"op":"user","bridge":"ops","userId":43}`)

	f.pushTo(testToken, Update{ID: 500, Message: msg(1, 42, "made-up for design")})
	f.pushTo(testToken2, Update{ID: 9000, Message: msg(1, 43, "made-up for ops")}, Update{ID: 9001, Message: msg(2, 43, "more made-up for ops")})
	waitFor(t, func() bool { return len(h.prompts()) == 2 })

	byAgent := map[string]string{}
	for _, p := range h.prompts() {
		byAgent[p.agent] = p.text
	}
	if !strings.Contains(byAgent["q601r3"], "made-up for design") || strings.Contains(byAgent["q601r3"], "ops") ||
		!strings.HasPrefix(byAgent["k3f9a2"], "[Telegram] 2 messages") {
		t.Fatalf("prompts %v", byAgent)
	}
	waitFor(t, func() bool {
		var file struct{ Bridges []Config }
		b, _ := os.ReadFile(h.Path)
		json.Unmarshal(b, &file)
		off := map[string]int64{}
		for _, c := range file.Bridges {
			off[c.ID] = c.Offset
		}
		return off["design"] == 501 && off["ops"] == 9002
	})
	for _, id := range []string{"design", "ops"} {
		if m, _ := filepath.Glob(filepath.Join(h.Dir, id, "inbox", "*", "*", "batch.json")); len(m) != 1 {
			t.Errorf("%s inbox: %v", id, m)
		}
	}
	waitFor(t, func() bool { // each bot answered its own chat once
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.sentBy[testToken]) == 1 && len(f.sentBy[testToken2]) == 1
	})
	// Turning one off leaves the other running.
	mustOK(`{"op":"off","bridge":"design"}`)
	code, out := h.do(t, "GET", "")
	if code != 200 || !strings.Contains(out, `"id":"design","on":false`) || !strings.Contains(out, `"id":"ops","on":true`) {
		t.Fatalf("after one off: %s", out)
	}
}

// A linked agent ends while the service is down: the held batch survives a
// restart and goes on the re-link through the service.
func TestServiceRelinkDeliversHeld(t *testing.T) {
	f := newFake(t)
	h := newService(t, f)
	if code, out := h.do(t, "POST", `{"op":"setup","token":"`+testToken+`"}`); code != 200 {
		t.Fatal(out)
	}
	h.do(t, "POST", `{"op":"user","userId":42}`)
	// Linked to an agent that then ended: written as the CLI would have left it.
	if err := h.update("default", func(c *Config) { c.Agent = "ended1" }); err != nil {
		t.Fatal(err)
	}
	r := h.runOf("default")
	must(t, r.bridge.handle(context.Background(), msg(1, 42, "made-up held note")))
	r.bridge.CloseNow()
	waitFor(t, func() bool { return r.bridge.Status().Held == 1 })
	code, out := h.do(t, "POST", `{"op":"link","agent":"q601r3"}`)
	if code != 200 || !strings.Contains(out, `"delivered":1`) {
		t.Fatalf("relink: %d %s", code, out)
	}
	if p := h.prompts(); len(p) != 1 || p[0].agent != "q601r3" || !strings.Contains(p[0].text, "made-up held note") {
		t.Fatalf("delivered %v", p)
	}
}

// Unlink holds what comes next; drop drops it; GET ?recent lists what was
// delivered, with its place in the queue; strangers are only counted.
func TestServiceUnlinkDropRecent(t *testing.T) {
	f := newFake(t)
	h := newService(t, f)
	h.Deliver = func(agent, text string) (Result, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.got = append(h.got, delivered{agent, text})
		return Result{Queued: true, Position: 2}, nil
	}
	if code, out := h.do(t, "POST", `{"op":"setup","token":"`+testToken+`"}`); code != 200 {
		t.Fatal(out)
	}
	h.do(t, "POST", `{"op":"user","userId":42}`)
	h.do(t, "POST", `{"op":"link","agent":"q601r3"}`)
	b := h.runOf("default").bridge
	ctx := context.Background()
	fw := msg(1, 42, "Made-up feedback about the receipt email.")
	fw.Origin = &Origin{Type: "user", Date: 1794990000, SenderUser: &User{ID: 9, FirstName: "Sam", LastName: "Example"}}
	must(t, b.handle(ctx, fw))
	b.CloseNow()
	must(t, b.handle(ctx, msg(2, 77, "a stranger"))) // counted, not kept

	code, out := h.do(t, "GET", "?recent=1")
	var res struct{ Bridges []BridgeStatus }
	json.Unmarshal([]byte(out), &res)
	if code != 200 || len(res.Bridges) != 1 || len(res.Bridges[0].Recent) != 1 {
		t.Fatalf("recent: %d %s", code, out)
	}
	d := res.Bridges[0].Recent[0]
	if d.Agent != "q601r3" || !d.Queued || d.Position != 2 || d.Summary != "1 message" || d.From != "Sam Example" ||
		!strings.HasPrefix(d.Snippet, "Made-up feedback") || res.Bridges[0].Ignored != 1 {
		t.Fatalf("delivery %+v, ignored %d", d, res.Bridges[0].Ignored)
	}

	// Unlinked: the next batch is held, and the bot says so.
	if code, out := h.do(t, "POST", `{"op":"unlink"}`); code != 200 || strings.Contains(out, `"agent"`) {
		t.Fatalf("unlink: %d %s", code, out)
	}
	must(t, b.handle(ctx, msg(3, 42, "made-up while unlinked")))
	must(t, b.handle(ctx, msg(4, 42, "and another")))
	b.CloseNow()
	_, out = h.do(t, "GET", "")
	if !strings.Contains(out, `"held":1`) || !strings.Contains(out, `"heldMessages":2`) || !strings.Contains(out, `"heldSince"`) {
		t.Fatalf("held: %s", out)
	}
	f.mu.Lock()
	last := f.sent[len(f.sent)-1]
	f.mu.Unlock()
	if !strings.HasPrefix(last, "Held: no agent linked") {
		t.Fatalf("bot said %q", last)
	}
	code, out = h.do(t, "POST", `{"op":"drop"}`)
	if code != 200 || !strings.Contains(out, `"dropped":2`) || strings.Contains(out, `"held"`) {
		t.Fatalf("drop: %d %s", code, out)
	}
	// Dropped is gone from this computer: a link later delivers nothing, the
	// held batch's folder (text and files) is deleted, and a tombstone heads
	// the recent deliveries.
	if code, out := h.do(t, "POST", `{"op":"link","agent":"k3f9a2"}`); code != 200 || strings.Contains(out, `"delivered"`) {
		t.Fatalf("link after drop: %d %s", code, out)
	}
	if n := len(h.prompts()); n != 1 {
		t.Fatalf("delivered %d", n)
	}
	all, _ := filepath.Glob(filepath.Join(h.Dir, "default", "inbox", "*", "*", "batch.json"))
	for _, f := range all {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "made-up while unlinked") {
			t.Fatalf("a dropped batch is still on disk: %s", f)
		}
	}
	if len(all) != 2 { // the delivered batch and the tombstone
		t.Fatalf("batch files %v", all)
	}
	_, out = h.do(t, "GET", "?recent=1")
	json.Unmarshal([]byte(out), &res)
	if r := res.Bridges[0].Recent; len(r) != 2 || r[0].Dropped != 2 || r[0].Summary != "dropped 2 messages" || r[0].Agent != "" || r[1].Agent != "q601r3" {
		t.Fatalf("recent after drop: %s", out)
	}
}

// What the app needs: the bot's name, when the pairing code runs out, a try
// now, and removing a bridge (its held messages dropped, token and pairing
// forgotten).
func TestServiceRetryRemove(t *testing.T) {
	f := newFake(t)
	h := newService(t, f)
	code, out := h.do(t, "POST", `{"op":"setup","token":"`+testToken+`"}`)
	var st BridgeStatus
	json.Unmarshal([]byte(out), &st)
	if code != 200 || st.BotName == "" || st.PairUntil.IsZero() || time.Until(st.PairUntil) > PairTTL {
		t.Fatalf("setup: %d %s", code, out)
	}
	if code, out := h.do(t, "POST", `{"op":"user","userId":42}`); code != 200 || strings.Contains(out, "pairUntil") {
		t.Fatalf("user: %d %s", code, out)
	}
	if code, out := h.do(t, "POST", `{"op":"retry"}`); code != 200 || !strings.Contains(out, `"on":true`) {
		t.Fatalf("retry: %d %s", code, out)
	}
	if code, out := h.do(t, "POST", `{"op":"remove"}`); code != 200 || strings.Contains(out, testToken) {
		t.Fatalf("remove: %d %s", code, out)
	}
	if _, err := h.keys.Load(TokenName("default")); err == nil {
		t.Error("the token is still in the keystore")
	}
	if code, out := h.do(t, "GET", ""); code != 200 || strings.Contains(out, `"id":"default"`) {
		t.Fatalf("after remove: %d %s", code, out)
	}
	if code, _ := h.do(t, "POST", `{"op":"retry"}`); code != 400 {
		t.Errorf("retry with no bridge: %d", code)
	}
}

// Offline: the status says since when, and when the next try is.
func TestServiceErrorSinceRetryAt(t *testing.T) {
	f := newFake(t)
	h := newService(t, f)
	if code, out := h.do(t, "POST", `{"op":"setup","token":"`+testToken+`"}`); code != 200 {
		t.Fatalf("setup: %d %s", code, out)
	}
	f.srv.Close() // Telegram can't be reached
	before := time.Now()
	code, out := h.do(t, "POST", `{"op":"retry"}`)
	var st BridgeStatus
	json.Unmarshal([]byte(out), &st)
	if code != 200 || st.Error == "" || st.ErrorSince.Before(before.Add(-time.Second)) || !st.RetryAt.After(st.ErrorSince) {
		t.Fatalf("offline: %d %s", code, out)
	}
	if code, out := h.do(t, "GET", ""); code != 200 || !strings.Contains(out, `"errorSince"`) || !strings.Contains(out, `"retryAt"`) {
		t.Fatalf("GET: %d %s", code, out)
	}
}
