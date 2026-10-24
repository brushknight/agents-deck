package herdr

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// serve answers each request on a fake herdr socket with reply(method,
// params) and records the requests.
func serve(t *testing.T, reply func(method string, params map[string]any) string) (string, *[]string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hd") // short: socket paths have a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "herdr.sock")
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
			c.Write([]byte(reply(req.Method, req.Params) + "\n"))
			c.Close()
		}
	}()
	return path, &got
}

func TestClient(t *testing.T) {
	path, got := serve(t, func(method string, _ map[string]any) string {
		switch method {
		case "session.snapshot":
			return `{"id":"agentsdeck","result":{"type":"session_snapshot","snapshot":{"agents":[{"terminal_id":"term_1","pane_id":"w1:p1",` +
				`"workspace_id":"w1","agent":"claude","name":"api","agent_status":"blocked","cwd":"/work/api","state_change_seq":3,` +
				`"agent_session":{"source":"herdr:claude","agent":"claude","kind":"id","value":"0b6c4f7e"}}],"workspaces":[{"workspace_id":"w1","label":"api"}]}}}`
		case "pane.read":
			return `{"id":"agentsdeck","result":{"type":"pane_read","read":{"pane_id":"w1:p1","text":"❯ 1. Yes"}}}`
		case "pane.process_info":
			return `{"id":"agentsdeck","result":{"type":"pane_process_info","process_info":{"foreground_processes":[{"pid":4242}]}}}`
		case "agent.focus":
			return `{"id":"agentsdeck","error":{"code":"not_found","message":"agent not found"}}`
		}
		return `{"id":"agentsdeck","result":{"type":"ok"}}`
	})
	c := Client{Socket: path}
	snap, err := c.Read()
	if err != nil || len(snap.Agents) != 1 || snap.Agents[0].Status != "blocked" || snap.Agents[0].Session.Value != "0b6c4f7e" ||
		snap.Agents[0].Dir() != "/work/api" || snap.Workspaces[0].Label != "api" {
		t.Fatalf("snapshot: %+v, %v", snap, err)
	}
	if s, err := c.Screen("w1:p1"); s != "❯ 1. Yes" || err != nil {
		t.Errorf("screen: %q, %v", s, err)
	}
	if pid := c.PID("w1:p1"); pid != 4242 {
		t.Errorf("pid %d", pid)
	}
	if err := c.SendText("w1:p1", "2"); err != nil {
		t.Error(err)
	}
	if err := c.SendKeys("w1:p1", "esc"); err != nil {
		t.Error(err)
	}
	if err := c.Focus("w1:p9"); err == nil || err.Error() != "herdr agent.focus: agent not found" {
		t.Errorf("focus error: %v", err)
	}
	want := []string{
		`session.snapshot {}`,
		`pane.read {"pane_id":"w1:p1","source":"visible"}`,
		`pane.process_info {"pane_id":"w1:p1"}`,
		`pane.send_text {"pane_id":"w1:p1","text":"2"}`,
		`pane.send_keys {"keys":["esc"],"pane_id":"w1:p1"}`,
		`agent.focus {"target":"w1:p9"}`,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("requests:\n%q\nwant\n%q", *got, want)
	}
}

func TestSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_SOCKET_PATH", "")
	for _, p := range []string{"herdr.sock", "sessions/work/herdr.sock"} {
		p = filepath.Join(home, ".config", "herdr", p)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, nil, 0o600)
	}
	got := Sockets()
	if len(got) != 2 || SessionName(got[0]) != "" || SessionName(got[1]) != "work" {
		t.Errorf("sockets %q", got)
	}
}

func TestClientTTYs(t *testing.T) {
	ps := `  101 ??       /opt/homebrew/bin/herdr server
  102 ttys003  herdr
  103 ttys004  herdr --session work
  104 ttys005  /opt/homebrew/bin/herdr session attach work
  105 ttys006  herdr agent list
  106 ttys007  vim herdr
`
	if got := clientTTYs(ps, ""); !reflect.DeepEqual(got, []string{"/dev/ttys003"}) {
		t.Errorf("default session: %q", got)
	}
	if got := clientTTYs(ps, "work"); !reflect.DeepEqual(got, []string{"/dev/ttys005", "/dev/ttys004"}) {
		t.Errorf("work session (newest first): %q", got)
	}
}
