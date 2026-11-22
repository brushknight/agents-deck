package link

import (
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/queue"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// promptCtl takes prompts for idle agents only (nothing is typed anywhere).
type promptCtl struct {
	fakeCtl
	srv *server.Server
}

func (c *promptCtl) Prompt(id, text string, interrupt bool) error {
	e, ok := c.srv.Store.Get(id)
	switch {
	case !ok:
		return server.ErrNotFound
	case e.A.Status == model.Running && !interrupt:
		return server.ErrBusy
	}
	c.calls = append(c.calls, "prompt "+id+" "+text)
	return nil
}

func (c *promptCtl) Type(id, text string) error {
	c.calls = append(c.calls, "type "+id+" "+text)
	return nil
}

// SEND_TYPE is "mode": "type": typed in now, also while the agent works.
func TestSessionTypeAhead(t *testing.T) {
	srv, _ := testDeck(t)
	ctl := &promptCtl{srv: srv}
	srv.Ctl = ctl
	b := start(t, srv, Binary)
	b.send(hi("board-t"))
	b.next()
	_ = srv.Devices.Approve("oled-tile-boardt")
	_ = srv.Devices.SetScope("oled-tile-boardt", devices.Act)
	b.next()
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_SendPrompt{SendPrompt: &linkpb.SendPrompt{AgentId: "b2", Text: "also this", Mode: linkpb.SendMode_SEND_TYPE}}})
	if r := b.next(); r.Seq != 2 || r.GetResult().GetCode() != 204 || len(ctl.calls) != 1 || ctl.calls[0] != "type b2 also this" {
		t.Fatalf("type ahead: %+v %v", r, ctl.calls)
	}
}

// A board queues a prompt for a busy agent, lists the queue and cancels it.
func TestSessionQueue(t *testing.T) {
	srv, _ := testDeck(t)
	ctl := &promptCtl{srv: srv}
	srv.Ctl = ctl
	srv.Queue = queue.Open("")
	b := start(t, srv, Binary)
	b.send(hi("board-q"))
	b.next() // welcome (pending)
	_ = srv.Devices.Approve("oled-tile-boardq")
	_ = srv.Devices.SetScope("oled-tile-boardq", devices.Act)
	b.next() // welcome (approved)

	queueIt := func(seq uint32, id, text string, front bool) uint32 {
		b.send(&linkpb.Frame{Seq: seq, Body: &linkpb.Frame_SendPrompt{SendPrompt: &linkpb.SendPrompt{AgentId: id, Text: text,
			Mode: linkpb.SendMode_SEND_QUEUE, Front: front}}})
		r := b.next()
		if r.Seq != seq || r.GetResult() == nil {
			t.Fatalf("send prompt: %+v", r)
		}
		return r.GetResult().GetCode()
	}
	if code := queueIt(2, "b2", "first "+strings.Repeat("x", 100), false); code != 202 {
		t.Fatalf("queue for a busy agent: %d", code)
	}
	if code := queueIt(3, "b2", "urgent", true); code != 202 {
		t.Fatalf("queue at the front: %d", code)
	}
	if code := queueIt(4, "c3", "go", false); code != 200 || len(ctl.calls) != 1 {
		t.Fatalf("idle agent: %d %v", code, ctl.calls)
	}
	b.send(&linkpb.Frame{Seq: 5, Body: &linkpb.Frame_Queue{Queue: &linkpb.Queue{AgentId: "b2"}}})
	r := b.next()
	l := r.GetQueueList()
	if r.Seq != 5 || l == nil || l.Total != 2 || len(l.Prompts) != 2 || l.Prompts[0].Text != "urgent" ||
		len(l.Prompts[1].Text) > 63 || l.Prompts[1].By != "oled-tile-boardq" || l.Prompts[1].QueuedAtMs == 0 {
		t.Fatalf("queue list: %+v", r)
	}
	cancel := &linkpb.Frame{Seq: 6, Body: &linkpb.Frame_Queue{Queue: &linkpb.Queue{AgentId: "b2", CancelId: l.Prompts[0].Id}}}
	b.send(cancel)
	if r := b.next(); r.Seq != 6 || r.GetResult().GetCode() != 204 || srv.Queue.Count("b2") != 1 {
		t.Fatalf("cancel: %+v", r)
	}
	cancel.Seq = 7
	b.send(cancel)
	if r := b.next(); r.Seq != 7 || r.GetResult().GetCode() != 404 {
		t.Fatalf("cancel again: %+v", r)
	}
}
