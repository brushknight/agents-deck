package daemon

import (
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/orca"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

type fakeOrca struct {
	fronted  int // Front calls: the real one would open the Orca app
	screen   string
	sent     []string
	switched []string
}

func (f *fakeOrca) Read() (orca.Snapshot, error)  { return orca.Snapshot{}, nil }
func (f *fakeOrca) Screen(string) (string, error) { return f.screen, nil }
func (f *fakeOrca) Send(h, text string, _ bool) error {
	f.sent = append(f.sent, h+" "+text)
	return nil
}
func (f *fakeOrca) Switch(h string) error { f.switched = append(f.switched, h); return nil }
func (f *fakeOrca) Front() error          { f.fronted++; return nil }

const permissionScreen = `
 Bash command

   rm -rf build

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /work/app
   3. No, and tell Claude what to do differently (esc)
`

func orcaSnap(state, tool, input string, started int64) orca.Snapshot {
	return orca.Snapshot{
		Worktrees: []orca.Worktree{{WorktreeID: "w1", Path: "/work/app", Branch: "refs/heads/fix-login", DisplayName: "fix-login",
			Agents: []orca.Agent{{PaneKey: "tab1:leaf1", State: state, AgentType: "claude", Prompt: "fix the login bug",
				ToolName: tool, ToolInput: input, StateStartedAt: started, UpdatedAt: started}}}},
		Terminals: []orca.Terminal{{Handle: "term_1", TabID: "tab1", LeafID: "leaf1", WorktreePath: "/work/app", Connected: true}},
	}
}

func TestOrcaAgentsOnTheDeck(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	f := &fakeOrca{screen: permissionScreen}
	d.orca.client = f
	id := orcaAgentID("tab1:leaf1")

	// Working: a running tile with the tool it uses.
	d.syncOrca(orcaSnap("working", "Bash", "go test ./...", 100))
	e, ok := d.Store.Get(id)
	if !ok || e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != "Bash" || e.A.Branch != "fix-login" ||
		e.A.Title != "fix-login" || e.A.Tool != "claude" || e.A.LastPrompt != "fix the login bug" || !e.A.External {
		t.Fatalf("working agent: %+v", e.A)
	}

	// Blocked on a permission: a prompt with the options from its screen.
	d.syncOrca(orcaSnap("blocked", "Bash", "rm -rf build", 200))
	e, _ = d.Store.Get(id)
	p := e.A.Waiting
	if e.A.Status != model.Waiting || p == nil || p.Kind != "permission" || p.Detail != "rm -rf build" || len(p.Options) != 3 ||
		!strings.HasPrefix(p.Options[1].Label, "yes, and don't ask again") {
		t.Fatalf("blocked agent: %+v / %+v", e.A, p)
	}
	// The same turn again keeps the prompt (and its id).
	d.syncOrca(orcaSnap("blocked", "Bash", "rm -rf build", 200))
	if e2, _ := d.Store.Get(id); e2.A.Waiting.ID != p.ID {
		t.Fatal("prompt rebuilt for the same turn")
	}

	// Answering types the key into the Orca terminal; a stale prompt is refused.
	if err := d.Answer(id, "old", "1"); err != server.ErrStalePrompt {
		t.Fatalf("stale answer: %v", err)
	}
	if err := d.Answer(id, p.ID, "9"); err != server.ErrBadKey {
		t.Fatalf("bad key: %v", err)
	}
	if err := d.Answer(id, p.ID, "1"); err != nil || len(f.sent) != 1 || f.sent[0] != "term_1 1" {
		t.Fatalf("answer: %v %v", err, f.sent)
	}

	// Done after working: idle and hungry until you look; focus switches the tab.
	d.syncOrca(orcaSnap("working", "Edit", "/work/app/login.go", 300))
	d.syncOrca(orcaSnap("done", "", "", 400))
	e, _ = d.Store.Get(id)
	if e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("done: %+v", e.A)
	}
	if err := d.Focus(id); err != nil || len(f.switched) != 1 || f.switched[0] != "term_1" || f.fronted != 1 {
		t.Fatalf("focus: %v %v", err, f.switched)
	}
	if e, _ = d.Store.Get(id); e.A.Unseen {
		t.Fatal("still hungry after focus")
	}
	if err := d.Interrupt(id); err != nil || f.sent[len(f.sent)-1] != "term_1 \x1b" {
		t.Fatalf("interrupt: %v %q", err, f.sent)
	}

	// Dismiss hides it until it's active again; a closed terminal removes it.
	if err := d.Dismiss(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("dismissed agent still on the deck")
	}
	d.syncOrca(orcaSnap("done", "", "", 400))
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("hidden agent came back without activity")
	}
	d.orca.hidden = nil
	d.syncOrca(orcaSnap("working", "Read", "/x", 9e15))
	if _, ok := d.Store.Get(id); !ok {
		t.Fatal("agent didn't come back when active")
	}
	gone := orcaSnap("working", "Read", "/x", 9e15)
	gone.Terminals = nil
	d.syncOrca(gone)
	if _, ok := d.Store.Get(id); ok {
		t.Fatal("agent without a terminal still on the deck")
	}
}

// A question without a tool (Claude waiting for you): options from the screen.
func TestOrcaWaitingQuestion(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	d.orca.client = &fakeOrca{screen: " Which database?\n ❯ 1. Postgres\n   2. SQLite\n"}
	d.syncOrca(orcaSnap("waiting", "", "", 10))
	e, _ := d.Store.Get(orcaAgentID("tab1:leaf1"))
	if p := e.A.Waiting; p == nil || p.Kind != "question" || len(p.Options) != 2 || p.Options[1].Label != "SQLite" {
		t.Fatalf("question: %+v", e.A.Waiting)
	}
	d.orca.client = &fakeOrca{screen: "nothing to pick here"}
	d.syncOrca(orcaSnap("waiting", "", "", 20))
	e, _ = d.Store.Get(orcaAgentID("tab1:leaf1"))
	if p := e.A.Waiting; p == nil || p.Kind != "input" || len(p.Options) != 0 {
		t.Fatalf("free-text wait: %+v", e.A.Waiting)
	}
}
