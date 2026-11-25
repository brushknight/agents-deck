package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/queue"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// turnCtl is a fake agent runner: a prompt typed into an idle agent starts
// a turn (running), as Claude Code's hooks would report; finish ends it.
// Nothing is typed anywhere.
type turnCtl struct {
	mcpCtl
	noTurn bool // the prompt doesn't start a turn (status stays idle)
}

func (c *turnCtl) Prompt(id, text string, interrupt bool) error {
	if err := c.mcpCtl.Prompt(id, text, interrupt); err != nil {
		return err
	}
	if !c.noTurn {
		c.st.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Running); return true })
	}
	return nil
}

// finish ends the agent's turn: idle, with a new statusSince.
func finish(st *store.Store, id string) {
	time.Sleep(2 * time.Millisecond) // statusSince must move
	st.Update(id, func(e *store.Entry) bool { e.SetStatus(model.Idle); return true })
}

func newQueueServer(t *testing.T, path string) (*Server, *turnCtl, map[string]string) {
	t.Helper()
	s, _, tok := newMCPServer(t)
	ctl := &turnCtl{mcpCtl: mcpCtl{st: s.Store}}
	s.Ctl = ctl
	s.Queue = queue.Open(path)
	s.Store.Queued = s.Queue.Agents
	s.Audit = &devices.Audit{Path: filepath.Join(t.TempDir(), "audit.log")}
	return s, ctl, tok
}

// queuePrompt sends one prompt with mode queue over the socket API and
// returns the status code and answer.
func queuePrompt(t *testing.T, h http.Handler, id, text, position string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"text": text, "mode": "queue", "position": position})
	w := do(h, "POST", "/v1/agents/"+id+"/prompt", string(b), nil)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func sentTexts(ctl *turnCtl) []string { return append([]string(nil), ctl.prompted...) }

func TestQueueDeliversInOrderAfterEachTurn(t *testing.T) {
	s, ctl, _ := newQueueServer(t, filepath.Join(t.TempDir(), "q.json"))
	h := s.UnixHandler(nil)
	for i, text := range []string{"one", "two"} {
		code, out := queuePrompt(t, h, "busy", text, "")
		if code != http.StatusAccepted || out["status"] != "queued" || out["position"].(float64) != float64(i+1) || !strings.HasPrefix(out["id"].(string), "q") {
			t.Fatalf("queue %s: %d %v", text, code, out)
		}
	}
	if a, _ := s.Store.Get("busy"); s.Store.Snapshot().Agents[agentIndex(s, "busy")].Queued != 2 || a.A.Status != model.Running {
		t.Fatalf("queued count: %+v", s.Store.Snapshot().Agents)
	}
	now := time.Now()
	s.deliver(now)
	if len(ctl.prompted) != 0 {
		t.Fatalf("delivered while running: %v", ctl.prompted)
	}
	finish(s.Store, "busy")
	s.deliver(now)
	s.deliver(now) // the agent runs "one": "two" waits
	if got := sentTexts(ctl); len(got) != 1 || got[0] != `busy "one" false` {
		t.Fatalf("after the first turn: %v", got)
	}
	finish(s.Store, "busy")
	s.deliver(now)
	if got := sentTexts(ctl); len(got) != 2 || got[1] != `busy "two" false` {
		t.Fatalf("after the second turn: %v", got)
	}
	if s.Queue.Count("busy") != 0 {
		t.Fatal("queue not empty")
	}
	b, _ := os.ReadFile(s.Audit.Path)
	for _, want := range []string{`"device":"local","action":"POST /v1/agents/busy/prompt","status":202,"detail":"queued q`,
		`"device":"local","action":"deliver queued prompt","status":204`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("audit lacks %s:\n%s", want, b)
		}
	}
}

func agentIndex(s *Server, id string) int {
	for i, a := range s.Store.Snapshot().Agents {
		if a.ID == id {
			return i
		}
	}
	return -1
}

// A prompt that doesn't start a turn holds the next one only so long.
func TestQueueNextWaitsForTheTurnToStart(t *testing.T) {
	s, ctl, _ := newQueueServer(t, "")
	ctl.noTurn = true
	h := s.UnixHandler(nil)
	queuePrompt(t, h, "busy", "one", "")
	queuePrompt(t, h, "busy", "two", "")
	finish(s.Store, "busy")
	now := time.Now()
	s.deliver(now)
	s.deliver(now.Add(queueTurnWait / 2))
	if len(ctl.prompted) != 1 {
		t.Fatalf("the second went before the first started a turn: %v", ctl.prompted)
	}
	s.deliver(now.Add(queueTurnWait + time.Second))
	if len(ctl.prompted) != 2 {
		t.Fatalf("the second never went: %v", ctl.prompted)
	}
}

func TestQueueFrontGoesFirst(t *testing.T) {
	s, ctl, _ := newQueueServer(t, "")
	h := s.UnixHandler(nil)
	queuePrompt(t, h, "busy", "a", "back")
	queuePrompt(t, h, "busy", "b", "")
	if code, out := queuePrompt(t, h, "busy", "urgent", "front"); code != http.StatusAccepted || out["position"].(float64) != 1 {
		t.Fatalf("front: %d %v", code, out)
	}
	for range 3 {
		finish(s.Store, "busy")
		s.deliver(time.Now())
	}
	want := []string{`busy "urgent" false`, `busy "a" false`, `busy "b" false`}
	if got := sentTexts(ctl); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order: %v, want %v", got, want)
	}
}

// A queued prompt survives a restart: a new queue from the same file, on a
// new service, delivers it.
func TestQueueSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-queue.json")
	s, _, _ := newQueueServer(t, path)
	queuePrompt(t, s.UnixHandler(nil), "busy", "after the restart", "")

	s2, ctl2, _ := newQueueServer(t, path)
	if s2.Queue.Count("busy") != 1 {
		t.Fatalf("lost in the restart: %v", s2.Queue.Agents())
	}
	finish(s2.Store, "busy")
	s2.deliver(time.Now())
	if got := sentTexts(ctl2); len(got) != 1 || got[0] != `busy "after the restart" false` {
		t.Fatalf("after restart: %v", got)
	}
}

// Nothing goes in while the agent waits on a question, and the question is
// never answered for you; once you answer and its turn ends, the queue goes on.
func TestQueueHoldsWhileWaiting(t *testing.T) {
	s, ctl, _ := newQueueServer(t, "")
	h := s.UnixHandler(nil)
	if code, _ := queuePrompt(t, h, "ask", "next task", ""); code != http.StatusAccepted {
		t.Fatalf("queue to a waiting agent: %d", code)
	}
	now := time.Now()
	for i := range 5 {
		s.deliver(now.Add(time.Duration(i) * queueTurnWait))
	}
	if len(ctl.prompted) != 0 || ctl.answered != "" {
		t.Fatalf("delivered or answered while waiting: %v %q", ctl.prompted, ctl.answered)
	}
	if a, _ := s.Store.Get("ask"); a.A.Status != model.Waiting || a.A.Waiting == nil {
		t.Fatal("the question is gone")
	}
	finish(s.Store, "ask")
	s.deliver(now)
	if len(ctl.prompted) != 1 {
		t.Fatalf("not delivered once idle: %v", ctl.prompted)
	}
}

func TestQueueCancel(t *testing.T) {
	s, ctl, _ := newQueueServer(t, "")
	h := s.UnixHandler(nil)
	_, out := queuePrompt(t, h, "busy", "never mind", "")
	qid := out["id"].(string)
	queuePrompt(t, h, "busy", "keep", "")
	w := do(h, "GET", "/v1/agents/busy/queue", "", nil)
	var l struct{ Queue []QueuedPrompt }
	if json.Unmarshal(w.Body.Bytes(), &l); w.Code != 200 || len(l.Queue) != 2 || l.Queue[0].ID != qid || l.Queue[0].By != "local" {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/v1/agents/busy/queue/"+qid+"/cancel", "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/v1/agents/busy/queue/"+qid+"/cancel", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("cancel again: %d", w.Code)
	}
	finish(s.Store, "busy")
	s.deliver(time.Now())
	if got := sentTexts(ctl); len(got) != 1 || got[0] != `busy "keep" false` {
		t.Fatalf("delivered: %v", got)
	}
	if w := do(h, "GET", "/v1/agents/zzz/queue", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: %d", w.Code)
	}
}

func TestQueueModes(t *testing.T) {
	s, ctl, _ := newQueueServer(t, "")
	h := s.UnixHandler(nil)
	// Idle with nothing queued: typed in at once.
	if code, out := queuePrompt(t, h, "a1", "go", ""); code != http.StatusOK || out["status"] != "sent" || len(ctl.prompted) != 1 {
		t.Fatalf("idle: %d %v %v", code, out, ctl.prompted)
	}
	// Idle again, but something waits: it goes behind.
	finish(s.Store, "a1")
	s.Queue.Add("a1", "first", "web", false)
	if code, out := queuePrompt(t, h, "a1", "second", ""); code != http.StatusAccepted || out["position"].(float64) != 2 {
		t.Fatalf("behind: %d %v", code, out)
	}
	for _, c := range []struct {
		id, body string
		code     int
	}{
		{"busy", `{"text": "go", "mode": "later"}`, 400},
		{"busy", `{"text": "go", "mode": "queue", "interrupt": true}`, 400},
		{"busy", `{"text": "go", "mode": "queue", "position": "middle"}`, 400},
		{"busy", `{"text": "go", "mode": "now"}`, 409},
		{"busy", `{"text": "go", "mode": "interrupt"}`, 204},
		{"ext", `{"text": "go", "mode": "queue"}`, 422},
		{"zzz", `{"text": "go", "mode": "queue"}`, 404},
	} {
		if w := do(h, "POST", "/v1/agents/"+c.id+"/prompt", c.body, nil); w.Code != c.code {
			t.Errorf("%s %s: %d, want %d (%s)", c.id, c.body, w.Code, c.code, w.Body)
		}
	}
	// An agent that has exited takes no more.
	s.Store.Update("a1", func(e *store.Entry) bool { e.SetStatus(model.Exited); return true })
	if code, _ := queuePrompt(t, h, "a1", "third", ""); code != http.StatusConflict {
		t.Fatalf("exited: %d", code)
	}
	// Without a queue: not available.
	s.Queue = nil
	if code, _ := queuePrompt(t, h, "busy", "x", ""); code != http.StatusNotImplemented {
		t.Fatalf("no queue: %d", code)
	}
}

// An agent that left the deck has its queue dropped after a while.
func TestQueueDroppedWhenAgentLeaves(t *testing.T) {
	s, _, _ := newQueueServer(t, "")
	s.Queue.Add("gone1", "x", "mcp:boss", false)
	now := time.Now()
	s.deliver(now)
	if s.Queue.Count("gone1") != 1 {
		t.Fatal("dropped at once")
	}
	s.deliver(now.Add(queueGone))
	if s.Queue.Count("gone1") != 0 {
		t.Fatal("not dropped")
	}
	if b, _ := os.ReadFile(s.Audit.Path); !strings.Contains(string(b), `"device":"mcp:boss","action":"drop queued prompt"`) {
		t.Errorf("audit: %s", b)
	}
}

func TestQueueOverMCP(t *testing.T) {
	s, ctl, tok := newQueueServer(t, "")
	h := s.LocalHandler()

	// Scopes: queueing and cancelling need act, listing needs view.
	_, out := rpc(h, "/mcp", tok["looker"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "later", "mode": "queue"}))
	if text, isErr := toolText(t, out); !isErr || !strings.Contains(text, "needs act") || s.Queue.Count("busy") != 0 {
		t.Fatalf("view token queued: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "later", "mode": "queue"}))
	text, isErr := toolText(t, out)
	var r map[string]any
	if json.Unmarshal([]byte(text), &r); isErr || r["status"] != "queued" || r["position"].(float64) != 1 {
		t.Fatalf("queue: %s", text)
	}
	qid := r["queue_id"].(string)
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "first", "mode": "queue", "queue_position": "front"}))
	if text, isErr = toolText(t, out); isErr || !strings.Contains(text, `"position": 1`) {
		t.Fatalf("front: %s", text)
	}
	for _, bad := range []map[string]any{
		{"agent_id": "busy", "text": "x", "mode": "soon"},
		{"agent_id": "busy", "text": "x", "mode": "queue", "queue_position": "middle"},
		{"agent_id": "busy", "text": "x", "mode": "queue", "interrupt": true},
	} {
		_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", bad))
		if _, isErr = toolText(t, out); !isErr {
			t.Errorf("accepted %v", bad)
		}
	}
	// queued: n in list_agents and get_agent.
	_, out = rpc(h, "/mcp", tok["looker"], call("list_agents", nil))
	text, _ = toolText(t, out)
	if !strings.Contains(text, `"queued": 2`) {
		t.Errorf("list_agents: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["looker"], call("get_agent", map[string]any{"agent_id": "busy"}))
	if text, _ = toolText(t, out); !strings.Contains(text, `"queued": 2`) {
		t.Errorf("get_agent: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["looker"], call("list_queue", map[string]any{"agent_id": "busy"}))
	text, isErr = toolText(t, out)
	var l struct {
		Queue []struct {
			QueueID  string `json:"queue_id"`
			Position int
			Text, By string
		}
	}
	if json.Unmarshal([]byte(text), &l); isErr || len(l.Queue) != 2 || l.Queue[0].Text != "first" || l.Queue[1].QueueID != qid || l.Queue[1].By != "mcp:doer" || l.Queue[1].Position != 2 {
		t.Fatalf("list_queue: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["looker"], call("cancel_queued", map[string]any{"agent_id": "busy", "queue_id": qid}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "needs act") {
		t.Fatalf("view token cancelled: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("cancel_queued", map[string]any{"agent_id": "busy", "queue_id": qid}))
	if text, isErr = toolText(t, out); isErr || s.Queue.Count("busy") != 1 {
		t.Fatalf("cancel_queued: %s", text)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("cancel_queued", map[string]any{"agent_id": "busy", "queue_id": qid}))
	if text, isErr = toolText(t, out); !isErr || !strings.Contains(text, "already") {
		t.Fatalf("cancel twice: %s", text)
	}
	// An idle agent gets it at once.
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "a1", "text": "now please", "mode": "queue"}))
	if text, isErr = toolText(t, out); isErr || !strings.HasPrefix(text, "sent:") {
		t.Fatalf("idle: %s", text)
	}
	finish(s.Store, "busy")
	s.deliver(time.Now())
	if got := ctl.prompted; got[len(got)-1] != `busy "first" false` {
		t.Fatalf("delivered: %v", got)
	}
	b, _ := os.ReadFile(s.Audit.Path)
	for _, want := range []string{
		`"device":"mcp:doer","action":"POST /v1/agents/busy/prompt","status":202,"detail":"queued ` + qid + ` at the back"`,
		`"device":"mcp:doer","action":"POST /v1/agents/busy/queue/` + qid + `/cancel","status":204,"detail":"cancelled ` + qid + `"`,
		`"device":"mcp:doer","action":"deliver queued prompt","status":204`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("audit lacks %s:\n%s", want, b)
		}
	}
	if NeedScope("GET", "/v1/agents/busy/queue") != devices.View || NeedScope("POST", "/v1/agents/busy/queue/q1/cancel") != devices.Act {
		t.Error("scopes of the queue endpoints")
	}
}

// Over MCP, send_prompt without a mode queues: a busy agent gets it when its
// turn ends, like typing several prompts one after another in its chat.
// mode now still refuses a busy agent.
func TestMCPQueuesByDefault(t *testing.T) {
	s, _, tok := newQueueServer(t, "")
	h := s.LocalHandler()
	for i, text := range []string{"one", "two"} {
		_, out := rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": text}))
		got, isErr := toolText(t, out)
		var r map[string]any
		if json.Unmarshal([]byte(got), &r); isErr || r["status"] != "queued" || int(r["position"].(float64)) != i+1 {
			t.Fatalf("send_prompt %q: %s", text, got)
		}
	}
	_, out := rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "three", "mode": "now"}))
	if got, isErr := toolText(t, out); !isErr || !strings.Contains(got, "working") || s.Queue.Count("busy") != 2 {
		t.Errorf("mode now to a busy agent: %s", got)
	}
}

// mode type types the prompt in at once, also while the agent works (its
// tool's own queue takes it), and never while it asks a question. The
// deck's queue stays out of it.
func TestTypeAhead(t *testing.T) {
	s, ctl, tok := newQueueServer(t, "")
	h := s.LocalHandler()
	_, out := rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "and add a test", "mode": "type"}))
	if got, isErr := toolText(t, out); isErr || !strings.Contains(got, "typed") || s.Queue.Count("busy") != 0 ||
		len(ctl.prompted) != 1 || ctl.prompted[0] != `busy "and add a test" typed` {
		t.Fatalf("type to a busy agent: %s %v", got, ctl.prompted)
	}
	_, out = rpc(h, "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "ask", "text": "2", "mode": "type"}))
	if got, isErr := toolText(t, out); !isErr || !strings.Contains(got, "question") || len(ctl.prompted) != 1 {
		t.Errorf("type to an agent asking a question: %s", got)
	}
	_, out = rpc(h, "/mcp", tok["looker"], call("send_prompt", map[string]any{"agent_id": "busy", "text": "x", "mode": "type"}))
	if got, isErr := toolText(t, out); !isErr || !strings.Contains(got, "needs act") {
		t.Errorf("a view token typed: %s", got)
	}
}

// fakeHold is the CPU guard: on while the computer is busy.
type fakeHold struct{ on bool }

func (f *fakeHold) Holding() (bool, string) {
	if !f.on {
		return false, ""
	}
	return true, "The Mac is busy (92% CPU for 4 minutes); try again in a few minutes"
}

// While the CPU guard holds, queued prompts wait; when it lets go (calm, or
// Send them now), they go on. What you send to an idle agent goes in at
// once, with mode queue or now. start_agent over MCP is refused.
func TestQueueHeldWhileBusy(t *testing.T) {
	s, ctl, tok := newQueueServer(t, "")
	hold := &fakeHold{on: true}
	s.Guard = hold
	h := s.UnixHandler(nil)
	queuePrompt(t, h, "busy", "one", "")
	finish(s.Store, "busy")
	now := time.Now()
	s.deliver(now)
	s.deliver(now.Add(time.Minute))
	if len(ctl.prompted) != 0 || s.Queue.Count("busy") != 1 {
		t.Fatalf("delivered while held: %v", ctl.prompted)
	}
	// Typed by hand: an idle agent with nothing queued takes it now.
	s.Store.Add(&store.Entry{A: model.Agent{ID: "free", Title: "free", Status: model.Idle, Tool: "claude"}})
	if code, out := queuePrompt(t, h, "free", "by hand", ""); code != http.StatusOK || out["status"] != "sent" {
		t.Fatalf("queue mode to an idle agent: %d %v", code, out)
	}
	finish(s.Store, "free")
	if w := do(h, "POST", "/v1/agents/free/prompt", `{"text": "right now", "mode": "now"}`, nil); w.Code != http.StatusNoContent {
		t.Fatalf("mode now: %d %s", w.Code, w.Body)
	}
	if got := sentTexts(ctl); len(got) != 2 || got[0] != `free "by hand" false` || got[1] != `free "right now" false` {
		t.Fatalf("by hand: %v", got)
	}
	// Starting an agent is never held: async work can always start more.
	_, out := rpc(s.LocalHandler(), "/mcp", tok["chief"], call("start_agent", map[string]any{"folder": "/work/app"}))
	if text, isErr := toolText(t, out); isErr || len(ctl.started) != 1 {
		t.Fatalf("start_agent while busy: %s %v", text, ctl.started)
	}
	// An MCP client's prompt waits, even for an idle agent.
	finish(s.Store, "free")
	_, out = rpc(s.LocalHandler(), "/mcp", tok["doer"], call("send_prompt", map[string]any{"agent_id": "free", "text": "from a script"}))
	if text, isErr := toolText(t, out); isErr || !strings.Contains(text, `"queued"`) || s.Queue.Count("free") != 1 {
		t.Fatalf("MCP prompt while busy: %s", text)
	}
	hold.on = false
	s.deliver(now.Add(2 * time.Minute))
	if got := sentTexts(ctl); len(got) != 4 || !strings.Contains(strings.Join(got[2:], " "), `busy "one" false`) ||
		!strings.Contains(strings.Join(got[2:], " "), `free "from a script" false`) || s.Queue.Count("busy")+s.Queue.Count("free") != 0 {
		t.Fatalf("after the hold: %v", got)
	}
	if _, out = rpc(s.LocalHandler(), "/mcp", tok["chief"], call("start_agent", map[string]any{"folder": "/work/app"})); len(ctl.started) != 2 {
		t.Fatalf("start_agent after the hold: %v", out)
	}
}
