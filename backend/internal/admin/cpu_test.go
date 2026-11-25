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

// The schedule through /admin/v1/settings: a limit picked by hand while it
// is on is an override (limitFrom "override" in /admin/v1/system), null is
// Back to schedule, overlapping blocks are refused with a message naming
// both, and an override that is over leaves config.json.
func TestCPUSchedule(t *testing.T) {
	r := newRig(t)
	g := cpuguard.New(steadyMeter(50))
	r.a.Guard = g
	g.Ended = r.a.DropCPUOverride
	week := `{"cpuGuard": {"schedule": {"on": true, "blocks": [{"days": ["mon","tue","wed","thu","fri","sat","sun"], "from": "00:00", "to": "24:00", "limit": 65}]}}}`
	if code, out := r.do(t, "PATCH", "/admin/v1/settings", week); code != 200 {
		t.Fatalf("schedule: %d %v", code, out)
	}
	_, out := r.do(t, "GET", "/admin/v1/system", "")
	if cpu, _ := out["cpu"].(map[string]any); cpu["limit"] != 65.0 || cpu["limitFrom"] != "schedule" || cpu["scheduled"] != true {
		t.Fatalf("scheduled: %v", cpu)
	}
	if code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"limit": 75}}`); code != 200 {
		t.Fatalf("limit: %d %v", code, out)
	}
	_, out = r.do(t, "GET", "/admin/v1/system", "")
	if cpu, _ := out["cpu"].(map[string]any); cpu["limit"] != 75.0 || cpu["limitFrom"] != "override" || cpu["by"] != "Settings" || cpu["until"] == nil || cpu["next"] != 65.0 {
		t.Fatalf("override: %v", cpu)
	}
	_, got := r.do(t, "GET", "/admin/v1/settings", "")
	if s, _ := got["cpuGuard"].(map[string]any); s["limit"] != 80.0 || s["override"] == nil {
		t.Fatalf("setting %v", got["cpuGuard"])
	}
	code, out := r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"schedule": {"blocks": [
		{"days": ["mon"], "from": "09:00", "to": "18:00", "limit": 60}, {"days": ["mon", "tue"], "from": "17:30", "to": "20:00", "limit": 70}]}}}`)
	if msg, _ := out["error"].(string); code != 400 || msg != "block 2 (Mon, Tue 17:30-20:00) overlaps block 1 (Mon 09:00-18:00)" {
		t.Fatalf("overlap: %d %v", code, out)
	}
	r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"override": null}}`)
	if _, out = r.do(t, "GET", "/admin/v1/system", ""); out["cpu"].(map[string]any)["limitFrom"] != "schedule" {
		t.Fatalf("back to schedule: %v", out["cpu"])
	}

	// An override that is over (here: one written in the past) is dropped.
	r.do(t, "PATCH", "/admin/v1/settings", `{"cpuGuard": {"limit": 90}}`)
	s := g.Setting()
	s.Override.Until = time.Now().Add(-time.Minute)
	if err := r.a.editConfig(func(cfg map[string]any) error { cfg["cpuGuard"] = s; return nil }); err != nil {
		t.Fatal(err)
	}
	g.Set(s)
	if cfg, _ := os.ReadFile(r.a.Config); strings.Contains(string(cfg), "override") || g.Setting().Override != nil {
		t.Errorf("config.json still has it:\n%s", cfg)
	}
}

// bgPrio keeps which processes are in background mode.
type bgPrio map[int]bool

func (p bgPrio) Background(pid int, on bool) error { p[pid] = on; return nil }

// Slow it and Full speed: only while busy, only an agent with a terminal;
// /admin/v1/system names the slowed agents.
func TestCPUSlow(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "POST", "/admin/v1/cpu/slow/a1", ""); code != 404 {
		t.Errorf("no slower: %d", code)
	}
	g := cpuguard.New(steadyMeter(92))
	prio := bgPrio{}
	s := &cpuguard.Slower{Guard: g, Prio: prio, Self: 600, Cores: 10,
		Agents: func() []proc.AgentTerminal {
			return []proc.AgentTerminal{{TTY: "/dev/ttys003", ID: "a1", Title: "fix it"}}
		},
		List: func() ([]proc.PS, error) {
			return []proc.PS{{PID: 700, TTY: "ttys003", CPU: 80, Comm: "claude"}, {PID: 500, TTY: "??", CPU: 90, Comm: "browser"}}, nil
		}}
	g.Slowed, g.SlowedByYou = s.Slowed, s.SlowedByYou
	r.a.Guard, r.a.Slower = g, s
	if code, out := r.do(t, "POST", "/admin/v1/cpu/slow/a1", ""); code != 409 {
		t.Errorf("calm: %d %v", code, out)
	}
	g.Step()
	if code, _ := r.do(t, "POST", "/admin/v1/cpu/full-speed/a1", ""); code != 409 {
		t.Errorf("full speed, not slowed: %d", code)
	}
	if code, _ := r.do(t, "POST", "/admin/v1/cpu/slow/zz", ""); code != 409 {
		t.Errorf("unknown agent: %d", code)
	}
	code, out := r.do(t, "POST", "/admin/v1/cpu/slow/a1", "")
	if l, _ := out["slowed"].([]any); code != 200 || len(l) != 1 || l[0] != "a1" || !prio[700] || prio[500] {
		t.Fatalf("slow it: %d %v %v", code, out, prio)
	}
	if l, _ := out["slowedByYou"].([]any); len(l) != 1 || l[0] != "a1" {
		t.Errorf("slowed by you: %v", out["slowedByYou"])
	}
	_, out = r.do(t, "GET", "/admin/v1/system", "")
	if l, _ := out["cpu"].(map[string]any)["slowed"].([]any); len(l) != 1 {
		t.Errorf("system: %v", out["cpu"])
	}
	code, out = r.do(t, "POST", "/admin/v1/cpu/full-speed/a1", "")
	if l, _ := out["slowed"].([]any); code != 200 || l == nil || len(l) != 0 || prio[700] || out["slowedByYou"] == nil {
		t.Errorf("full speed: %d %v %v", code, out, prio)
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
