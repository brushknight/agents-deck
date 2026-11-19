package server

import (
	"net/http"
	"strings"
	"testing"
)

// adminProbe stands in for the management API and records every call that
// reaches it.
type adminProbe struct{ calls []string }

func (p *adminProbe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.calls = append(p.calls, r.Method+" "+r.URL.Path)
	w.WriteHeader(http.StatusOK)
}

// The management API is never served on the network listener, whatever the
// device's scope: not to a paired panel, not to an admin device, not to a
// revoked one, not without a certificate.
func TestAdminNeverOnNetwork(t *testing.T) {
	s, _ := newTestServer()
	probe := &adminProbe{}
	s.Admin = probe
	h := s.DeviceHandler()
	paths := []string{"/admin/v1/system", "/admin/v1/settings", "/admin/v1/ports/cu.x/flash", "/admin/v1/devices/panel/scope",
		"/admin/v1/update/apply", "/admin/v1/events", "/admin/v1/diagnostics", "/v1/../admin/v1/system", "/v1/admin/v1/system",
		"/admin/v1/bluetooth", "/admin/v1/bluetooth/scan", "/admin/v1/bluetooth/boards", "/admin/v1/bluetooth/boards/6B1F2C3D-0000-4000-8000-0000000000A1/add",
		"/admin/v1/devices/panel/uplink", "/admin/v1/devices/panel/firmware-file", "/admin/v1/devices/panel/install-file"}
	for _, dev := range []string{"", "viewer", "panel", "boss", "gone"} {
		hdr := map[string]string{}
		ps := paths
		if dev != "" {
			hdr["Device"] = dev
		}
		if dev == "" || dev == "gone" {
			ps = paths[:2] // refused before routing (and slowed down): two are enough
		}
		for _, p := range ps {
			for _, m := range []string{"GET", "POST", "PATCH"} {
				w := do(h, m, p, `{"board":"x"}`, hdr)
				if loc := w.Header().Get("Location"); w.Code >= 300 && w.Code < 400 {
					w = do(h, m, loc, `{"board":"x"}`, hdr) // a cleaned path: follow it once
				}
				if w.Code < 400 {
					t.Errorf("%s %s as %q: %d", m, p, dev, w.Code)
				}
			}
		}
	}
	if len(probe.calls) != 0 {
		t.Fatalf("the management API answered on the network listener: %v", probe.calls)
	}
}

// A firmware file sent to the network listener never reaches the management
// API, not even from a device with the admin scope: installing a file is for
// this computer only (the unix socket, the loopback dashboard).
func TestFirmwareFileNotOnNetwork(t *testing.T) {
	s, _ := newTestServer()
	probe := &adminProbe{}
	s.Admin = probe
	img := "\xe9\x01" + strings.Repeat("\x00", 4094)
	for _, dev := range []string{"panel", "boss"} {
		for _, p := range []string{"/admin/v1/devices/panel/firmware-file", "/admin/v1/devices/panel/install-file"} {
			w := do(s.DeviceHandler(), "POST", p, img, map[string]string{"Device": dev, "Content-Type": "application/octet-stream", "X-File-Name": "firmware.bin"})
			if w.Code < 400 {
				t.Errorf("POST %s as %s: %d", p, dev, w.Code)
			}
		}
	}
	if len(probe.calls) != 0 {
		t.Fatalf("a firmware file reached the management API on the network listener: %v", probe.calls)
	}
	// On this computer it does, behind the dashboard's session and Origin.
	web := s.LocalHandler()
	hdr := map[string]string{"Cookie": sessionCookie + "=websession", "Content-Type": "application/octet-stream"}
	if c := do(web, "POST", "/admin/v1/devices/panel/firmware-file", img, hdr).Code; c != 403 {
		t.Errorf("without the dashboard's Origin: %d", c)
	}
	hdr["Origin"] = "http://127.0.0.1:7340"
	if c := do(web, "POST", "/admin/v1/devices/panel/firmware-file", img, hdr).Code; c != 200 {
		t.Errorf("from the dashboard: %d", c)
	}
	if c := do(web, "POST", "/admin/v1/devices/panel/firmware-file", img, map[string]string{"Origin": "http://127.0.0.1:7340"}).Code; c != 401 {
		t.Errorf("without the session: %d", c)
	}
	if len(probe.calls) != 1 {
		t.Errorf("calls: %v", probe.calls)
	}
}

func TestAdminOnSocketAndDashboard(t *testing.T) {
	s, _ := newTestServer()
	probe := &adminProbe{}
	s.Admin = probe
	if c := do(s.UnixHandler(nil), "GET", "/admin/v1/system", "", nil).Code; c != 200 {
		t.Errorf("unix socket: %d", c)
	}
	web := s.LocalHandler()
	if c := do(web, "GET", "/admin/v1/system", "", nil).Code; c != 401 {
		t.Errorf("dashboard without a session: %d", c)
	}
	cookie := map[string]string{"Cookie": sessionCookie + "=websession"}
	if c := do(web, "GET", "/admin/v1/system", "", cookie).Code; c != 200 {
		t.Errorf("dashboard with the session: %d", c)
	}
	if c := do(web, "PATCH", "/admin/v1/settings", "{}", cookie).Code; c != 403 {
		t.Errorf("a write without the dashboard's Origin: %d", c)
	}
	cookie["Origin"] = "http://127.0.0.1:7340"
	if c := do(web, "PATCH", "/admin/v1/settings", "{}", cookie).Code; c != 200 {
		t.Errorf("a write from the dashboard: %d", c)
	}
	if c := do(web, "GET", "/admin/v1/system", "", map[string]string{"Host": "attacker.example", "Cookie": sessionCookie + "=websession"}).Code; c != http.StatusMisdirectedRequest {
		t.Errorf("another host name (DNS rebinding): %d", c)
	}
	if len(probe.calls) != 3 {
		t.Errorf("calls: %v", probe.calls)
	}
	// Without a management API: a plain 404 behind the session.
	s.Admin = nil
	if c := do(s.LocalHandler(), "GET", "/admin/v1/system", "", map[string]string{"Cookie": sessionCookie + "=websession"}).Code; c != 404 {
		t.Errorf("no admin: %d", c)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "/",
		"/":                            "/",
		"/settings":                    "/settings",
		"/settings#boards":             "/settings#boards",
		"/settings?x=1":                "/settings?x=1",
		"settings":                     "/",
		"//evil.example":               "/",
		"//evil.example/x":             "/",
		"/\\evil.example":              "/",
		"\\\\evil.example":             "/",
		"https://evil.example/":        "/",
		"http:/evil.example":           "/",
		"javascript:alert(1)":          "/",
		"/%0d%0aSet-Cookie:x=1":        "/%0d%0aSet-Cookie:x=1", // stays encoded: a path on this dashboard
		"/a\r\nSet-Cookie: x=1":        "/",
		"/\tsettings":                  "/",
		"/" + strings.Repeat("a", 600): "/",
	} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoginNext(t *testing.T) {
	s, _ := newTestServer()
	web := s.LocalHandler()
	for next, want := range map[string]string{
		"%2Fsettings":           "/settings",
		"%2F%2Fevil.example":    "/",
		"https%3A%2F%2Fevil.ex": "/",
		"%2F%5Cevil.example":    "/",
		"":                      "/",
	} {
		code := s.logins.issue()
		w := do(web, "GET", "/login?code="+code+"&next="+next, "", nil)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
			t.Errorf("next=%s: %d %q, want %q", next, w.Code, w.Header().Get("Location"), want)
		}
		if !strings.Contains(w.Header().Get("Set-Cookie"), sessionCookie) {
			t.Errorf("next=%s: no session cookie", next)
		}
	}
	if c := do(web, "GET", "/login?code=used&next=%2Fsettings", "", nil).Code; c != 401 {
		t.Errorf("a wrong code: %d", c)
	}
}
