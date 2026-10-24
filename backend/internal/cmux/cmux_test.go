package cmux

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

const hookFile = `{
  "activeSessionsBySurface": {
    "S1": {"sessionId": "11111111-1111-4111-8111-111111111111"},
    "S2": {"sessionId": "22222222-2222-4222-8222-222222222222"},
    "S3": {"sessionId": "33333333-3333-4333-8333-333333333333"}
  },
  "sessions": {
    "11111111-1111-4111-8111-111111111111": {"sessionId": "11111111-1111-4111-8111-111111111111", "surfaceId": "S1",
      "pid": 101, "cwd": "/work/api", "transcriptPath": "/t/1.jsonl", "agentLifecycle": "needsInput",
      "lastBody": "Do you prefer red or blue?\n[Red] [Blue]", "updatedAt": 1700000000.5},
    "22222222-2222-4222-8222-222222222222": {"sessionId": "22222222-2222-4222-8222-222222222222", "surfaceId": "S2",
      "pid": 102, "cwd": "/work/web", "agentLifecycle": "idle", "updatedAt": 1700000001},
    "44444444-4444-4444-8444-444444444444": {"sessionId": "44444444-4444-4444-8444-444444444444", "surfaceId": "S1",
      "pid": 104, "cwd": "/work/old", "agentLifecycle": "idle", "updatedAt": 1600000000}
  }
}`

func TestSessions(t *testing.T) {
	// S2's process has exited, S3's session isn't in the file, and the
	// older session in S1 isn't the one S1 runs now.
	got, err := sessions([]byte(hookFile), func(pid int) bool { return pid != 102 })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("sessions: %+v", got)
	}
	s := got[0]
	if s.Surface != "S1" || s.PID != 101 || s.Cwd != "/work/api" || s.Transcript != "/t/1.jsonl" ||
		s.Lifecycle != "needsInput" || s.Body != "Do you prefer red or blue?\n[Red] [Blue]" || s.Updated != 1700000000.5 {
		t.Errorf("session: %+v", s)
	}
	if _, err := sessions([]byte("{"), alive); err == nil {
		t.Error("broken file: no error")
	}
}

func TestDecode(t *testing.T) {
	var out struct {
		Text string `json:"text"`
	}
	if err := decode([]byte(`{"ok":true,"id":"x","result":{"text":"hi"}}`+"\n"), &out, "m"); err != nil || out.Text != "hi" {
		t.Errorf("result: %v %q", err, out.Text)
	}
	if err := decode([]byte(`{"ok":false,"error":{"code":"invalid_params","message":"Missing text"}}`), nil, "m"); err == nil ||
		err.Error() != "cmux m: Missing text" {
		t.Errorf("error: %v", err)
	}
	if err := decode([]byte("ERROR: Access denied - only processes started inside cmux can connect\n"), nil, "m"); !errors.Is(err, ErrDenied) {
		t.Errorf("refusal: %v", err)
	}
}

func TestClient(t *testing.T) {
	dir, err := os.MkdirTemp("", "cm") // short: socket paths have a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "cmux.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var got []string
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			var req struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.Unmarshal(line, &req)
			p, _ := json.Marshal(req.Params)
			got = append(got, req.Method+" "+string(p))
			reply := `{"ok":true,"id":"agentsdeck","result":{}}`
			if req.Method == "surface.read_text" {
				reply = `{"ok":true,"id":"agentsdeck","result":{"text":"❯ 1. Red"}}`
			}
			c.Write([]byte(reply + "\n"))
			c.Close()
		}
	}()
	c := Client{Socket: path}
	if s, err := c.Screen("S1"); err != nil || s != "❯ 1. Red" {
		t.Fatalf("screen: %v %q", err, s)
	}
	if err := c.SendText("S1", "2"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendKey("S1", "escape"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`surface.read_text {"surface_id":"S1"}`,
		`surface.send_text {"surface_id":"S1","text":"2"}`,
		`surface.send_key {"key":"escape","surface_id":"S1"}`,
	}
	if len(got) != len(want) {
		t.Fatalf("requests: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: %s, want %s", i, got[i], want[i])
		}
	}
}
