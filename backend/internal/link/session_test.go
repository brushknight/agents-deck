package link

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type fakeCtl struct{ calls []string }

func (f *fakeCtl) Answer(id, p, k string) error {
	f.calls = append(f.calls, "answer "+id+" "+p+" "+k)
	if p != "p1" {
		return server.ErrStalePrompt
	}
	return nil
}
func (f *fakeCtl) Focus(id string) error     { f.calls = append(f.calls, "focus "+id); return nil }
func (f *fakeCtl) Interrupt(id string) error { f.calls = append(f.calls, "interrupt "+id); return nil }
func (f *fakeCtl) Dismiss(id string) error   { f.calls = append(f.calls, "dismiss "+id); return nil }
func (f *fakeCtl) Resume(id string) error    { f.calls = append(f.calls, "resume "+id); return nil }

func testDeck(t *testing.T) (*server.Server, *fakeCtl) {
	t.Helper()
	dir := t.TempDir()
	st := store.New("", "test-mac", "9.9")
	st.Add(&store.Entry{A: model.Agent{ID: "a1", Slot: 0, Title: "checkout-api", Tool: "claude", Status: model.Waiting,
		Waiting: &model.Prompt{ID: "p1", Kind: "permission", Title: "run this command?", Detail: "make test",
			Options: []model.Option{{Key: "1", Label: "yes", Primary: true}, {Key: "3", Label: "no"}}}}})
	st.Add(&store.Entry{A: model.Agent{ID: "b2", Slot: 1, Title: strings.Repeat("long title ", 20), Tool: "codex", Status: model.Running}})
	st.Add(&store.Entry{A: model.Agent{ID: "c3", Title: "far away", Status: model.Idle}})
	st.Place("c3", 20)
	ctl := &fakeCtl{}
	return &server.Server{Store: st, Ctl: ctl, Static: fstest.MapFS{},
		Devices: &devices.Registry{Path: filepath.Join(dir, "devices.json")},
		Audit:   &devices.Audit{Path: filepath.Join(dir, "audit.log")}}, ctl
}

// board is the test's side of the serial line.
type board struct {
	t    *testing.T
	conn net.Conn
	dec  Decoder
	q    []Decoded
	mode Mode
}

func (b *board) send(f *linkpb.Frame) {
	b.t.Helper()
	enc := Encode
	if b.mode == JSON {
		enc = EncodeJSON
	}
	w, err := enc(f)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := b.conn.Write(w); err != nil {
		b.t.Fatal(err)
	}
}

// next returns the next frame the deck sent that isn't a Ping.
func (b *board) next() *linkpb.Frame {
	b.t.Helper()
	buf := make([]byte, 4096)
	for {
		for len(b.q) > 0 {
			f := b.q[0]
			b.q = b.q[1:]
			if f.Mode != b.mode {
				b.t.Fatalf("deck answered in mode %d, board speaks %d", f.Mode, b.mode)
			}
			if f.Frame.GetPing() == nil {
				return f.Frame
			}
		}
		_ = b.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := b.conn.Read(buf)
		if err != nil {
			b.t.Fatalf("waiting for a frame: %v", err)
		}
		b.q = append(b.q, b.dec.Feed(buf[:n])...)
	}
}

func start(t *testing.T, srv *server.Server, mode Mode) *board {
	t.Helper()
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); deckEnd.Close(); boardEnd.Close() })
	s := NewSession(ServerDeck{srv}, deckEnd, "test")
	s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond
	go func() { _ = s.Run(ctx) }()
	return &board{t: t, conn: boardEnd, mode: mode}
}

func hi(id string) *linkpb.Frame {
	return &linkpb.Frame{Seq: 1, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: id, Type: "oled-tile", Firmware: "1.0"}}}
}

func TestSessionApprovalSnapshotAndUpdates(t *testing.T) {
	for _, mode := range []Mode{Binary, JSON} {
		srv, ctl := testDeck(t)
		b := start(t, srv, mode)
		b.send(hi("AA:BB:CC:00:11:22"))
		w := b.next().GetWelcome()
		if w == nil || w.Approved || w.DeviceName != "oled-tile-001122" || w.ServerName != "test-mac" {
			t.Fatalf("mode %d: first welcome %+v", mode, w)
		}
		// Before approval: subscriptions wait, commands are refused.
		b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{SlotCount: 16, Fields: 1 | 4}}})
		b.send(&linkpb.Frame{Seq: 3, Body: &linkpb.Frame_Focus{Focus: &linkpb.Focus{AgentId: "a1"}}})
		if r := b.next().GetResult(); r == nil || r.Code != 403 {
			t.Fatalf("command before approval: %+v", r)
		}
		// Approve: a new Welcome, then the snapshot for slots 0..15.
		if err := srv.Devices.Approve("oled-tile-001122"); err != nil {
			t.Fatal(err)
		}
		if w := b.next().GetWelcome(); w == nil || !w.Approved || w.Scope != linkpb.Scope_SCOPE_VIEW {
			t.Fatalf("welcome after approval: %+v", w)
		}
		if sb := b.next().GetSnapshotBegin(); sb == nil || sb.AgentCount != 3 {
			t.Fatalf("snapshot begin: %+v", sb)
		}
		a := b.next().GetAgentChanged().GetAgent()
		if a.GetId() != "a1" || a.Status != linkpb.Status_STATUS_WAITING || a.Waiting.GetId() != "p1" || len(a.Waiting.Options) != 2 || !a.Waiting.Options[0].Primary {
			t.Fatalf("agent a1: %+v", a)
		}
		long := b.next().GetAgentChanged().GetAgent()
		if long.GetId() != "b2" || len(long.Title) > 63 || !strings.HasSuffix(long.Title, "…") || long.Tool != linkpb.Tool_TOOL_CODEX {
			t.Fatalf("agent b2 (clipped title): %q %v", long.GetTitle(), long.GetTool())
		}
		if f := b.next(); f.GetSnapshotEnd() == nil {
			t.Fatalf("no snapshot end (c3 is outside the subscribed slots): %v", f)
		}
		// View scope: acting is refused with 403 and the reason.
		b.send(&linkpb.Frame{Seq: 4, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a1", PromptId: "p1", Key: "1"}}})
		if r := b.next().GetResult(); r.GetCode() != 403 || !strings.Contains(r.Error, "view") {
			t.Fatalf("answer with view scope: %+v", r)
		}
		// Raise the scope: Welcome again, and the answer goes through the web API.
		_ = srv.Devices.SetScope("oled-tile-001122", devices.Act)
		if w := b.next().GetWelcome(); w.GetScope() != linkpb.Scope_SCOPE_ACT {
			t.Fatalf("welcome after scope change: %+v", w)
		}
		b.send(&linkpb.Frame{Seq: 5, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a1", PromptId: "p1", Key: "1"}}})
		if r := b.next(); r.Seq != 5 || r.GetResult().GetCode() != 204 {
			t.Fatalf("answer: %+v", r)
		}
		b.send(&linkpb.Frame{Seq: 6, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a1", PromptId: "old", Key: "1"}}})
		if r := b.next().GetResult(); r.GetCode() != 409 {
			t.Fatalf("stale answer: %+v", r)
		}
		b.send(&linkpb.Frame{Seq: 7, Body: &linkpb.Frame_Dismiss{Dismiss: &linkpb.Dismiss{AgentId: "a1"}}})
		if r := b.next().GetResult(); r.GetCode() != 403 {
			t.Fatalf("dismiss needs admin: %+v", r)
		}
		if len(ctl.calls) != 2 || ctl.calls[0] != "answer a1 p1 1" {
			t.Fatalf("controller calls: %v", ctl.calls)
		}
		// A change reaches the board as one AgentChanged; leaving the slots as AgentRemoved.
		srv.Store.Update("a1", func(e *store.Entry) bool { e.SetStatus(model.Running); e.A.Waiting = nil; return true })
		if a := b.next().GetAgentChanged().GetAgent(); a.GetId() != "a1" || a.Status != linkpb.Status_STATUS_RUNNING || a.Waiting != nil {
			t.Fatalf("update: %+v", a)
		}
		srv.Store.Place("b2", 30)
		if r := b.next().GetAgentRemoved(); r.GetId() != "b2" {
			t.Fatalf("moved out of the slots: %+v", r)
		}
		// Ping is answered with the same seq and time.
		b.send(&linkpb.Frame{Seq: 99, Body: &linkpb.Frame_Ping{Ping: &linkpb.Ping{TimeMs: 1234}}})
		if p := b.next(); p.Seq != 99 || p.GetPong().GetTimeMs() != 1234 {
			t.Fatalf("pong: %+v", p)
		}
		// Every action is in the audit log under the board's name.
		log, _ := os.ReadFile(srv.Audit.Path)
		if !strings.Contains(string(log), `"device":"oled-tile-001122","action":"POST /v1/agents/a1/answer","status":204`) {
			t.Fatalf("audit: %s", log)
		}
		// Revoked: a Welcome with approved false, and nothing works anymore.
		_ = srv.Devices.Revoke("oled-tile-001122")
		if w := b.next().GetWelcome(); w == nil || w.Approved {
			t.Fatalf("welcome after revoke: %+v", w)
		}
		b.send(&linkpb.Frame{Seq: 8, Body: &linkpb.Frame_Focus{Focus: &linkpb.Focus{AgentId: "a1"}}})
		if r := b.next().GetResult(); r.GetCode() != 403 {
			t.Fatalf("command after revoke: %+v", r)
		}
	}
}

// The "Needs you" spot comes with TILE, and MoveAttention moves it like the
// web API does, with the same scope as Move.
func TestSessionAttentionSlot(t *testing.T) {
	srv, _ := testDeck(t)
	b := start(t, srv, Binary)
	b.send(hi("board-3"))
	b.next() // welcome (pending)
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{}}})
	_ = srv.Devices.Approve("oled-tile-board3")
	b.next() // welcome (approved, view)
	b.next() // snapshot begin
	got := map[string]uint32{}
	for {
		f := b.next()
		if f.GetSnapshotEnd() != nil {
			break
		}
		a := f.GetAgentChanged().GetAgent()
		got[a.GetId()] = a.GetAttentionSlot()
	}
	if got["a1"] != 1 || got["b2"] != 2 || got["c3"] != 0 {
		t.Fatalf("attention slots in the snapshot: %v", got)
	}
	move := &linkpb.Frame{Seq: 3, Body: &linkpb.Frame_MoveAttention{MoveAttention: &linkpb.MoveAttention{AgentId: "a1", Slot: 7}}}
	b.send(move)
	if r := b.next().GetResult(); r.GetCode() != 403 {
		t.Fatalf("move with view scope: %+v", r)
	}
	_ = srv.Devices.SetScope("oled-tile-board3", devices.Act)
	b.next() // welcome (act)
	b.send(move)
	var moved, done bool
	for !moved || !done {
		f := b.next()
		if r := f.GetResult(); r != nil {
			if f.Seq != 3 || r.Code != 204 {
				t.Fatalf("move: %+v", f)
			}
			done = true
		}
		if a := f.GetAgentChanged().GetAgent(); a != nil && a.Id == "a1" && a.AttentionSlot == 7 {
			moved = true
		}
	}
	b.send(&linkpb.Frame{Seq: 4, Body: &linkpb.Frame_MoveAttention{MoveAttention: &linkpb.MoveAttention{AgentId: "c3", Slot: 2}}})
	if r := b.next().GetResult(); r.GetCode() != 409 {
		t.Fatalf("move of an agent not on the grid: %+v", r)
	}
}

// A board that subscribes with FIELDS_LIMITS gets the usage limits after
// the snapshot and when they change; agents out of usage carry until when.
func TestSessionLimits(t *testing.T) {
	srv, _ := testDeck(t)
	var mu sync.Mutex
	used := 86
	srv.Store.Limits = func() []model.Limits {
		mu.Lock()
		defer mu.Unlock()
		lv := map[bool]string{false: "near", true: "out"}[used >= 100]
		return []model.Limits{{Tool: "codex", Level: lv, Windows: []model.LimitWindow{
			{ID: "seven_day", Label: "week", Used: used, Level: lv, ResetsAt: time.Now().Add(time.Hour)}}}}
	}
	b := start(t, srv, Binary)
	b.send(hi("board-4"))
	b.next() // welcome (pending)
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{Fields: uint32(linkpb.FieldGroup_FIELDS_TILE | linkpb.FieldGroup_FIELDS_LIMITS)}}})
	_ = srv.Devices.Approve("oled-tile-board4")
	b.next() // welcome (approved)
	b.next() // snapshot begin
	for {
		f := b.next()
		if f.GetSnapshotEnd() != nil {
			break
		}
		if a := f.GetAgentChanged().GetAgent(); a.GetId() == "b2" && a.OutUntilMs != 0 {
			t.Fatalf("b2 isn't out: %+v", a)
		}
	}
	l := b.next().GetLimits()
	if len(l.GetSets()) != 1 || l.Sets[0].Level != linkpb.LimitLevel_LIMIT_NEAR || l.Sets[0].Windows[0].Used != 86 {
		t.Fatalf("limits after the snapshot: %+v", l)
	}
	mu.Lock()
	used = 100
	mu.Unlock()
	srv.Store.Touch()
	var out, sent bool
	for !out || !sent {
		f := b.next()
		if a := f.GetAgentChanged().GetAgent(); a.GetId() == "b2" && a.OutUntilMs > 0 {
			out = true
		}
		if l := f.GetLimits(); l != nil && l.Sets[0].Level == linkpb.LimitLevel_LIMIT_OUT {
			sent = true
		}
	}
}

func TestSessionTypedReplies(t *testing.T) {
	srv, _ := testDeck(t)
	b := start(t, srv, Binary)
	b.send(hi("board-2"))
	b.next() // welcome (pending)
	_ = srv.Devices.Approve("oled-tile-board2")
	_ = srv.Devices.SetScope("oled-tile-board2", devices.Admin)
	b.next() // welcome (approved)
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_ListHats{ListHats: &linkpb.ListHats{}}})
	if r := b.next(); r.Seq != 2 || r.GetResult().GetCode() != 501 {
		// The test server has no hat book: the web API's 501 comes through.
		if r.GetHatList() == nil {
			t.Fatalf("list hats: %+v", r)
		}
	}
	b.send(&linkpb.Frame{Seq: 3, Body: &linkpb.Frame_ListLive{ListLive: &linkpb.ListLive{}}})
	if r := b.next(); r.Seq != 3 || r.GetLiveSessions() == nil {
		t.Fatalf("list live: %+v", r)
	}
	b.send(&linkpb.Frame{Seq: 4, Body: &linkpb.Frame_Restore{Restore: &linkpb.Restore{}}})
	if r := b.next(); r.Seq != 4 || r.GetResult().GetCode() != 501 {
		t.Fatalf("restore without a restorer: %+v", r)
	}
	b.send(&linkpb.Frame{Seq: 5, Body: &linkpb.Frame_GetState{GetState: &linkpb.GetState{}}})
	if r := b.next(); r.Seq != 5 || r.GetResult().GetCode() != 204 {
		t.Fatalf("get state without a subscription: %+v", r)
	}
}

func TestSmallFramesAndNoise(t *testing.T) {
	srv, _ := testDeck(t)
	srv.Store.Update("a1", func(e *store.Entry) bool { e.A.Waiting.Detail = strings.Repeat("x", 2000); return true })
	b := start(t, srv, Binary)
	// Boot log noise first, then a Hello asking for small frames.
	_, _ = io.WriteString(b.conn, "ets Jun  8 2016\r\nrst:0x1 (POWERON_RESET)\r\n")
	h := hi("tiny")
	h.GetHello().MaxFrame = 512
	b.send(h)
	b.next()
	_ = srv.Devices.Approve("oled-tile-tiny")
	b.next()
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{Fields: 1 | 2 | 4 | 8}}})
	b.next() // begin
	a := b.next().GetAgentChanged().GetAgent()
	if a.GetId() != "a1" || a.Waiting == nil || len(a.Waiting.Detail) > 128 {
		t.Fatalf("shrunk agent: detail %d bytes", len(a.GetWaiting().GetDetail()))
	}
}

type restoringCtl struct {
	fakeCtl
	asked []string
}

func (r *restoringCtl) RestoreJSON(ids []string) any {
	r.asked = ids
	out := []map[string]string{}
	for _, id := range ids {
		out = append(out, map[string]string{"id": id, "title": "t", "action": "resumed"})
	}
	return out
}

// Restore with agent ids restores only those.
func TestSessionRestoreSome(t *testing.T) {
	srv, _ := testDeck(t)
	ctl := &restoringCtl{}
	srv.Ctl = ctl
	b := start(t, srv, Binary)
	b.send(hi("board-3"))
	b.next()
	_ = srv.Devices.Approve("oled-tile-board3")
	_ = srv.Devices.SetScope("oled-tile-board3", devices.Admin)
	b.next()
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Restore{Restore: &linkpb.Restore{AgentIds: []string{"a1"}}}})
	r := b.next()
	if rr := r.GetRestoreResult(); r.Seq != 2 || len(rr.GetResults()) != 1 || rr.Results[0].Id != "a1" {
		t.Fatalf("restore a1: %+v", r)
	}
	if len(ctl.asked) != 1 || ctl.asked[0] != "a1" {
		t.Fatalf("asked: %q", ctl.asked)
	}
}
