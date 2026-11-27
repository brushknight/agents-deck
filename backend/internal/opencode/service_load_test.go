package opencode

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeService(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Dir(dir))
}

func TestLoadServiceOnlyLoopback(t *testing.T) {
	writeService(t, `{"url":"http://0.0.0.0:49374","password":"pw"}`)
	svc, err := LoadService()
	if err != nil || svc.URL != "http://127.0.0.1:49374" {
		t.Fatalf("wildcard: %+v %v", svc, err)
	}
	writeService(t, `{"url":"http://192.0.2.10:49374","password":"pw"}`)
	if _, err := LoadService(); err == nil {
		t.Fatal("accepted a non-loopback service URL")
	}
	writeService(t, `{"url":"http://127.0.0.1:49374/path","password":"pw"}`)
	if _, err := LoadService(); err == nil {
		t.Fatal("accepted a service URL with a path")
	}
}

func TestServiceDoesNotFollowRedirects(t *testing.T) {
	hit := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/session/active", http.StatusFound)
	}))
	defer srv.Close()
	defer target.Close()
	svc := &Service{URL: srv.URL, Password: "pw", client: serviceClient()}
	if _, err := svc.Active(); err == nil {
		t.Fatal("followed a redirect")
	}
	if hit != 0 {
		t.Fatalf("redirect target hit %d times", hit)
	}
}
