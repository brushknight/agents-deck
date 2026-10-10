package server

import (
	"bufio"
	"context"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type fakeCtl struct{ answered string }

func (f *fakeCtl) Answer(id, p, k string) error {
	if id != "a1" {
		return ErrNotFound
	}
	if p != "p1" {
		return ErrStalePrompt
	}
	f.answered = k
	return nil
}
func (f *fakeCtl) Focus(string) error     { return nil }
func (f *fakeCtl) Interrupt(string) error { return nil }
func (f *fakeCtl) Dismiss(string) error   { return nil }
func (f *fakeCtl) Resume(string) error    { return nil }

func newTestServer() (*Server, *fakeCtl) {
	st := store.New("", "test", "0")
	st.Add(&store.Entry{A: model.Agent{ID: "a1", Title: "one", Status: model.Idle}})
	ctl := &fakeCtl{}
	return &Server{Store: st, Ctl: ctl, Static: fstest.MapFS{"index.html": {Data: []byte("hi")}},
		DeviceToken: func() string { return "devtoken" }, WebSession: "websession", LocalAddr: "127.0.0.1:7340"}, ctl
}

func do(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:7340"
	for k, v := range hdr {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDeviceAuth(t *testing.T) {
	s, ctl := newTestServer()
	h := s.DeviceHandler()
	if c := do(h, "GET", "/v1/state", "", nil).Code; c != 401 {
		t.Errorf("no token: %d", c)
	}
	if c := do(h, "GET", "/v1/state", "", map[string]string{"Authorization": "Bearer nope"}).Code; c != 401 {
		t.Errorf("bad token: %d", c)
	}
	auth := map[string]string{"Authorization": "Bearer devtoken"}
	if c := do(h, "GET", "/v1/state", "", auth).Code; c != 200 {
		t.Errorf("good token: %d", c)
	}
	if c := do(h, "GET", "/index.html", "", auth).Code; c != 404 {
		t.Errorf("device listener must not serve the web UI: %d", c)
	}
	if c := do(h, "POST", "/v1/agents/a1/answer", `{"promptId":"p1","key":"2"}`, auth).Code; c != 204 || ctl.answered != "2" {
		t.Errorf("answer: %d %q", c, ctl.answered)
	}
	if c := do(h, "POST", "/v1/agents/a1/answer", `{"promptId":"old","key":"2"}`, auth).Code; c != 409 {
		t.Errorf("stale: %d", c)
	}
	if c := do(h, "POST", "/v1/agents/zz/answer", `{"promptId":"p1","key":"2"}`, auth).Code; c != 404 {
		t.Errorf("unknown agent: %d", c)
	}
}

func TestLocalGuards(t *testing.T) {
	s, _ := newTestServer()
	h := s.LocalHandler()
	if c := do(h, "GET", "/", "", map[string]string{"Host": "evil.example:7340"}).Code; c != 421 {
		t.Errorf("rebinding host: %d", c)
	}
	if c := do(h, "GET", "/v1/state", "", nil).Code; c != 401 {
		t.Errorf("no cookie: %d", c)
	}
	cookie := map[string]string{"Cookie": sessionCookie + "=websession"}
	if c := do(h, "GET", "/v1/state", "", cookie).Code; c != 200 {
		t.Errorf("cookie: %d", c)
	}
	post := map[string]string{"Cookie": sessionCookie + "=websession", "Origin": "https://evil.example"}
	if c := do(h, "POST", "/v1/agents/a1/focus", "", post).Code; c != 403 {
		t.Errorf("cross-origin post: %d", c)
	}
	post["Origin"] = "http://127.0.0.1:7340"
	if c := do(h, "POST", "/v1/agents/a1/focus", "", post).Code; c != 204 {
		t.Errorf("same-origin post: %d", c)
	}
	w := do(h, "GET", "/", "", nil)
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", w.Header())
	}
}

func TestLoginCodeOneTime(t *testing.T) {
	s, _ := newTestServer()
	h := s.LocalHandler()
	code := s.logins.issue()
	w := do(h, "GET", "/login?code="+code, "", nil)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Set-Cookie"), "HttpOnly") ||
		!strings.Contains(w.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Fatalf("login: %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	if c := do(h, "GET", "/login?code="+code, "", nil).Code; c != 401 {
		t.Errorf("reused code: %d", c)
	}
}

func TestEventsStream(t *testing.T) {
	s, _ := newTestServer()
	srv := httptest.NewServer(s.DeviceHandler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer devtoken")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	readEvent := func() string {
		var data string
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				data = line
			}
			if line == "\n" && data != "" {
				return data
			}
		}
	}
	if first := readEvent(); !strings.Contains(first, `"title":"one"`) {
		t.Fatalf("first event: %s", first)
	}
	s.Store.Update("a1", func(e *store.Entry) bool { e.A.Title = "renamed"; return true })
	if next := readEvent(); !strings.Contains(next, `"title":"renamed"`) {
		t.Fatalf("change event: %s", next)
	}
}

func TestOrder(t *testing.T) {
	s, _ := newTestServer()
	s.Store.Add(&store.Entry{A: model.Agent{ID: "a2", Title: "two"}})
	s.Store.Add(&store.Entry{A: model.Agent{ID: "a3", Title: "three"}})
	h := s.DeviceHandler()
	auth := map[string]string{"Authorization": "Bearer devtoken"}
	if c := do(h, "POST", "/v1/order", `{"ids":["a3","zz","a1"]}`, auth).Code; c != 204 {
		t.Fatalf("order: %d", c)
	}
	got := []string{}
	for _, a := range s.Store.Snapshot().Agents {
		got = append(got, a.ID)
	}
	if strings.Join(got, ",") != "a3,a1,a2" {
		t.Fatalf("order = %v", got)
	}
	if c := do(h, "POST", "/v1/order", `{"ids":[]}`, auth).Code; c != 400 {
		t.Errorf("empty order: %d", c)
	}
}

func TestMoveToFreeSlot(t *testing.T) {
	s, _ := newTestServer()
	s.Store.Add(&store.Entry{A: model.Agent{ID: "a2", Title: "two"}})
	h := s.DeviceHandler()
	auth := map[string]string{"Authorization": "Bearer devtoken"}
	slots := func() map[string]int {
		m := map[string]int{}
		for _, a := range s.Store.Snapshot().Agents {
			m[a.ID] = a.Slot
		}
		return m
	}
	if c := do(h, "POST", "/v1/agents/a1/move", `{"slot":9}`, auth).Code; c != 204 {
		t.Fatalf("move: %d", c)
	}
	if got := slots(); got["a1"] != 9 || got["a2"] != 1 {
		t.Fatalf("free spot: %v", got)
	}
	if c := do(h, "POST", "/v1/agents/a2/move", `{"slot":9}`, auth).Code; c != 204 {
		t.Fatalf("move: %d", c)
	}
	if got := slots(); got["a2"] != 9 || got["a1"] != 1 {
		t.Fatalf("taken spot should swap: %v", got)
	}
	s.Store.Add(&store.Entry{A: model.Agent{ID: "a3", Title: "three"}})
	if got := slots(); got["a3"] != 0 {
		t.Fatalf("new agents take the first free spot: %v", got)
	}
	for _, body := range []string{`{}`, `{"slot":-1}`, `{"slot":256}`} {
		if c := do(h, "POST", "/v1/agents/a1/move", body, auth).Code; c != 400 {
			t.Errorf("%s: %d", body, c)
		}
	}
	if c := do(h, "POST", "/v1/agents/zz/move", `{"slot":3}`, auth).Code; c != 404 {
		t.Errorf("unknown: %d", c)
	}
}

func TestHatEndpoints(t *testing.T) {
	s, _ := newTestServer()
	dir := t.TempDir()
	s.Hats = &hats.Book{Path: filepath.Join(dir, "config.json")}
	s.Store.Hat = s.Hats.For
	s.Store.Update("a1", func(e *store.Entry) bool { e.A.Cwd = "/Users/sam/dev/app"; return true })
	h := s.DeviceHandler()
	auth := map[string]string{"Authorization": "Bearer devtoken"}
	hatOf := func() *model.Hat {
		for _, a := range s.Store.Snapshot().Agents {
			if a.ID == "a1" {
				return a.Hat
			}
		}
		return nil
	}
	if got := hatOf(); got == nil || !got.Auto {
		t.Fatalf("automatic hat in the state: %+v", got)
	}
	if c := do(h, "POST", "/v1/agents/a1/hat", `{"shape":"top hat","color":"purple"}`, auth).Code; c != 204 {
		t.Fatalf("set: %d", c)
	}
	if got := hatOf(); got.Shape != "top hat" || got.Color != "purple" || got.Auto {
		t.Fatalf("chosen: %+v", got)
	}
	if c := do(h, "POST", "/v1/agents/a1/hat", `{"shape":"sombrero"}`, auth).Code; c != 400 {
		t.Fatalf("unknown shape: %d", c)
	}
	if c := do(h, "POST", "/v1/hats", `{"folder":"/Users/sam/dev/app","auto":true}`, auth).Code; c != 204 {
		t.Fatalf("auto by folder: %d", c)
	}
	if got := hatOf(); !got.Auto {
		t.Fatalf("back to auto: %+v", got)
	}
	if c := do(h, "POST", "/v1/agents/zz/hat", `{"auto":true}`, auth).Code; c != 404 {
		t.Fatalf("unknown agent: %d", c)
	}
}
