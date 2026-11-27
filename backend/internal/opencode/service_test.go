package opencode

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceActions(t *testing.T) {
	var sawPrompt, sawReply, sawInterrupt bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "opencode" || p != "pw" {
			t.Errorf("auth: %v %q %q", ok, u, p)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/session/active":
			io.WriteString(w, `{"data":{"ses_1":{"type":"running"},"ses_2":{"type":"idle"}}}`)
		case "GET /api/session/ses_1/permission":
			io.WriteString(w, `{"location":{},"data":[{"id":"p1","sessionID":"ses_1","action":"bash","resources":["go test ./..."],"save":[]}]}`)
		case "POST /api/session/ses_1/permission/p1/reply":
			sawReply = true
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["decision"] != "once" {
				t.Errorf("decision: %v", b)
			}
			w.WriteHeader(204)
		case "POST /api/session/ses_1/interrupt":
			sawInterrupt = true
			w.WriteHeader(200)
			io.WriteString(w, `{"type":"interrupted"}`)
		case "POST /api/session/ses_1/prompt":
			sawPrompt = true
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["text"] != "hello" {
				t.Errorf("prompt: %v", b)
			}
			w.WriteHeader(200)
			io.WriteString(w, `{"data":{}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := &Service{URL: srv.URL, Password: "pw"}
	active, err := svc.Active()
	if err != nil || !active["ses_1"] || active["ses_2"] {
		t.Fatalf("active: %v %v", active, err)
	}
	perms, err := svc.PermissionsFor("ses_1")
	if err != nil || len(perms) != 1 || perms[0].Action != "bash" {
		t.Fatalf("perms: %v %v", perms, err)
	}
	if err := svc.ReplyPermission("ses_1", "p1", "once"); err != nil || !sawReply {
		t.Fatalf("reply: %v %v", err, sawReply)
	}
	if err := svc.Interrupt("ses_1"); err != nil || !sawInterrupt {
		t.Fatalf("interrupt: %v %v", err, sawInterrupt)
	}
	if err := svc.Prompt("ses_1", "hello"); err != nil || !sawPrompt {
		t.Fatalf("prompt: %v %v", err, sawPrompt)
	}
	if !strings.HasPrefix(srv.URL, "http://") {
		t.Fatal("server")
	}
}

func TestAPIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	svc := &Service{URL: srv.URL, Password: "pw"}
	err := svc.Interrupt("ses_1")
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusNotFound {
		t.Fatalf("err: %v", err)
	}
}
