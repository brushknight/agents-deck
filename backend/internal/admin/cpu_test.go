package admin

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/cpuguard"
	"github.com/brushknight/agents-deck/backend/internal/proc"
)

// steadyMeter reads the same percent forever.
type steadyMeter float64

func (m steadyMeter) Report() proc.MachineCPU {
	return proc.MachineCPU{Available: true, Now: float64(m)}
}
func (m steadyMeter) Average(time.Duration) (float64, bool) { return float64(m), true }

// GET /admin/v1/system has the guard as "cpu" (absent while it is off),
// POST /admin/v1/cpu/release is Send them now, and the setting is read and
// changed through /admin/v1/settings, into config.json and the guard.
func TestCPUGuard(t *testing.T) {
	r := newRig(t)
	g := cpuguard.New(steadyMeter(92))
	g.Queued = func() int { return 2 }
	r.a.Guard = g
	g.Step()
	_, out := r.do(t, "GET", "/admin/v1/system", "")
	cpu, _ := out["cpu"].(map[string]any)
	if cpu == nil || cpu["available"] != true || cpu["percent"] != 92.0 || cpu["busy"] != true || cpu["since"] == nil ||
		cpu["limit"] != 80.0 || cpu["hold"] != true || cpu["held"] != 2.0 || cpu["released"] != false {
		t.Fatalf("cpu %v", cpu)
	}
	if code, out := r.do(t, "POST", "/admin/v1/cpu/release", ""); code != 200 || out["hold"] != false || out["held"] != 0.0 || out["released"] != true {
		t.Fatalf("release: %d %v", code, out)
	}

	_, got := r.do(t, "GET", "/admin/v1/settings", "")
	if s, _ := got["cpuGuard"].(map[string]any); s["on"] != true || s["limit"] != 80.0 || s["for"] != "2m" || s["hold"] != true || s["slow"] != true {
		t.Fatalf("default setting %v", got["cpuGuard"])
	}
	code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"limit": 90, "for": "5m"}}`)
	if s, _ := out["settings"].(map[string]any)["cpuGuard"].(map[string]any); code != 200 || s["limit"] != 90.0 || s["for"] != "5m" || s["hold"] != true {
		t.Fatalf("patch: %d %v", code, out)
	}
	if s := g.Setting(); s.Limit != 90 || s.For != "5m" || !s.Hold || !s.Slow {
		t.Errorf("the guard has %+v", s)
	}
	cfg, _ := os.ReadFile(r.a.Config)
	if !strings.Contains(string(cfg), `"limit": 90`) {
		t.Errorf("config.json:\n%s", cfg)
	}
	for _, bad := range []string{`{"cpuGuard": {"limit": 99}}`, `{"cpuGuard": {"for": "3m"}}`, `{"cpuGuard": {"hold": "yes"}}`} {
		if code, _ := r.do(t, "PATCH", "/admin/v1/settings", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"on": false}}`)
	if _, out = r.do(t, "GET", "/admin/v1/system", ""); out["cpu"] != nil {
		t.Errorf("cpu while off: %v", out["cpu"])
	}
}

// GET /admin/v1/cpu: the machine always, who uses it only with ?top=N (at
// most 50), a bad N refused.
func TestCPUTop(t *testing.T) {
	asked := 0
	a := &Admin{
		CPU: func() proc.CPUReport {
			return proc.CPUReport{Budget: 2, Machine: &proc.MachineCPU{Available: true, Now: 42}}
		},
		CPUTop: func(n int) proc.CPUTop {
			asked = n
			return proc.CPUTop{Available: true, Users: []proc.CPUUser{{Kind: "deck", Name: "agents deck"}}}
		},
	}
	get := func(path string) (int, proc.CPUReport) {
		w := httptest.NewRecorder()
		a.cpu(w, httptest.NewRequest("GET", path, nil))
		var r proc.CPUReport
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return w.Code, r
	}
	if code, r := get("/admin/v1/cpu"); code != 200 || r.Machine == nil || r.Machine.Now != 42 || r.Top != nil || asked != 0 {
		t.Fatalf("plain: %d %+v (ps asked for %d)", code, r, asked)
	}
	if code, r := get("/admin/v1/cpu?top=500"); code != 200 || r.Top == nil || len(r.Top.Users) != 1 || asked != 50 {
		t.Fatalf("top: %d %+v (asked for %d)", code, r.Top, asked)
	}
	for _, bad := range []string{"0", "-1", "yes"} {
		if code, _ := get("/admin/v1/cpu?top=" + bad); code != 400 {
			t.Errorf("top=%s: %d", bad, code)
		}
	}
	a.CPUTop = nil
	if _, r := get("/admin/v1/cpu?top=3"); r.Top == nil || r.Top.Available || r.Top.Why == "" {
		t.Errorf("no ps: %+v", r.Top)
	}
}
