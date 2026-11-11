package web_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/web"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:7340"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Every page and every file it loads is served by the dashboard listener.
func TestPagesAndFilesServed(t *testing.T) {
	s := &server.Server{Store: store.New("", "test", "0"), Static: web.Static(), WebSession: "x", LocalAddr: "127.0.0.1:7340"}
	h := s.LocalHandler()
	for _, page := range []string{"/", "/settings", "/settings/"} {
		w := get(t, h, page)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %s", page, w.Code, w.Header().Get("Content-Type"))
		}
		// Every local file the page refers to.
		for _, m := range regexp.MustCompile(`(?:href|src)="\./([^"]+)"`).FindAllStringSubmatch(w.Body.String(), -1) {
			if c := get(t, h, "/"+m[1]).Code; c != 200 {
				t.Errorf("%s loads %s: %d", page, m[1], c)
			}
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Errorf("%s: no CSP", page)
		}
	}
	if !strings.Contains(get(t, h, "/settings").Body.String(), `href="/"`) {
		t.Error("settings links back to the deck")
	}
	if !strings.Contains(get(t, h, "/").Body.String(), `href="/settings"`) {
		t.Error("the deck links to settings")
	}
	// Every static file, and the fonts the stylesheets name.
	_ = fs.WalkDir(web.Static(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "fixture.json" || p == "index.html" { // index.html is served as /
			return err
		}
		if c := get(t, h, "/"+p).Code; c != 200 {
			t.Errorf("/%s: %d", p, c)
		}
		return nil
	})
	css, _ := fs.ReadFile(web.Static(), "settings.css")
	for _, m := range regexp.MustCompile(`url\("\./([^"]+)"\)`).FindAllStringSubmatch(string(css), -1) {
		if _, err := fs.Stat(web.Static(), m[1]); err != nil {
			t.Errorf("settings.css names %s: %v", m[1], err)
		}
	}
	if c := get(t, h, "/fixture.json").Code; c != 404 {
		t.Errorf("fixture.json: %d", c)
	}
}

// The page has no inline script or style: the dashboard's CSP allows neither.
func TestNoInlineCode(t *testing.T) {
	b, _ := fs.ReadFile(web.Static(), "settings.html")
	html := string(b)
	if regexp.MustCompile(`<script(?:\s[^>]*)?>[^<]`).MatchString(html) || strings.Contains(html, "<style") || strings.Contains(html, " style=") {
		t.Error("settings.html has inline script or style")
	}
	js, _ := fs.ReadFile(web.Static(), "settings.js")
	if strings.Contains(string(js), ".innerHTML") || strings.Contains(string(js), "setAttribute('style'") {
		t.Error("settings.js builds HTML from strings or sets style attributes")
	}
}

// The scripts parse (node --check), when node is installed.
func TestScriptsParse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	for _, name := range []string{"settings.js", "app.js"} {
		b, err := fs.ReadFile(web.Static(), name)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, strings.TrimSuffix(name, ".js")+".mjs") // both are ES modules
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "--check", p).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}

// In the app's own window the page drops its top bar (the window's title bar
// takes its place); in a browser it keeps it. The dashboard names Settings in
// words, not only with an icon.
func TestInAppAndSettingsEntry(t *testing.T) {
	css, _ := fs.ReadFile(web.Static(), "settings.css")
	if !strings.Contains(string(css), ".in-app .tb { display: none; }") {
		t.Error("settings.css keeps the top bar in the app")
	}
	js, _ := fs.ReadFile(web.Static(), "settings.js")
	if !strings.Contains(string(js), "window.agentsDeckApp") || !strings.Contains(string(js), "AgentsDeckApp") {
		t.Error("settings.js doesn't know when it runs in the app")
	}
	html, _ := fs.ReadFile(web.Static(), "index.html")
	if !regexp.MustCompile(`id="settings-link"[^>]*>.*<span>Settings</span></a>`).Match(html) {
		t.Error("the deck's header has no Settings entry in words")
	}
}
