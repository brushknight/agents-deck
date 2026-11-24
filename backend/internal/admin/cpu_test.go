package admin

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/proc"
)

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
