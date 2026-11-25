package server

import (
	"encoding/json"
	"slices"
	"testing"
)

type restoringCtl struct {
	fakeCtl
	asked [][]string
}

func (r *restoringCtl) RestoreJSON(ids []string) any {
	r.asked = append(r.asked, ids)
	return []map[string]string{}
}

// POST /v1/restore takes an optional list of agent ids.
func TestRestoreIDs(t *testing.T) {
	s, _ := newTestServer()
	ctl := &restoringCtl{}
	s.Ctl = ctl
	h := s.DeviceHandler()
	boss := map[string]string{"Device": "boss"}
	if w := do(h, "POST", "/v1/restore", "", boss); w.Code != 200 {
		t.Fatalf("restore all: %d %s", w.Code, w.Body)
	}
	w := do(h, "POST", "/v1/restore", `{"ids":["abc123","def456"]}`, boss)
	var res struct{ Results []any }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Results == nil {
		t.Fatalf("restore some: %d %s", w.Code, w.Body)
	}
	if len(ctl.asked) != 2 || len(ctl.asked[0]) != 0 || !slices.Equal(ctl.asked[1], []string{"abc123", "def456"}) {
		t.Fatalf("asked: %q", ctl.asked)
	}
}
