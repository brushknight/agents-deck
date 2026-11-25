package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// secret is a made-up key with a known shape, built at run time so no
// key-shaped string sits in the source.
var secret = "gh" + "p_" + strings.Repeat("Ab3Xy9Qz", 5)

const mark = "[redacted:github]"

func TestStoreRedactsAgentText(t *testing.T) {
	s := New("", "test", "0")
	s.Add(&Entry{A: model.Agent{ID: "a1", Title: "a1", LastPrompt: "deploy with " + secret}})
	s.Update("a1", func(e *Entry) bool {
		e.A.LastAnswer = "used " + secret
		e.A.AITitle = "rotate " + secret
		e.A.Activity = &model.Activity{Tool: "Bash", Detail: "export GH=" + secret}
		e.A.Waiting = &model.Prompt{ID: "p1", Kind: "permission", Title: "run " + secret + "?",
			Detail: "curl -H 'Authorization: token " + secret + "'", Context: secret,
			Options: []model.Option{{Key: "1", Label: "yes, " + secret}}}
		e.A.Subagents = []model.Subagent{{ID: "s1", Title: "check " + secret}}
		return true
	})
	s.Each(func(e *Entry) bool { return false })

	e, _ := s.Get("a1")
	b, _ := json.Marshal(e.A)
	if strings.Contains(string(b), secret) {
		t.Fatalf("the secret is still in the agent: %s", b)
	}
	snap, _ := json.Marshal(s.Snapshot())
	if strings.Contains(string(snap), secret) {
		t.Fatalf("the secret is still in the snapshot: %s", snap)
	}
	if e.A.LastPrompt != "deploy with "+mark || e.A.Waiting.Options[0].Key != "1" || e.A.Waiting.Title != "run "+mark+"?" {
		t.Errorf("redacted wrong: %+v / %+v", e.A.LastPrompt, e.A.Waiting)
	}
}

func TestStoreRedactsSavedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	old := []*Entry{{A: model.Agent{ID: "a1", Title: "a1", LastPrompt: "token " + secret}}}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path, "test", "0")
	s.Load()
	if e, _ := s.Get("a1"); e.A.LastPrompt != "token "+mark {
		t.Errorf("state saved before redaction kept the secret: %q", e.A.LastPrompt)
	}
}

func TestStoreKeepsOrdinaryText(t *testing.T) {
	s := New("", "test", "0")
	s.Add(&Entry{A: model.Agent{ID: "a1", Title: "a1", LastPrompt: "fix the failing test in queue_test.go"}})
	if e, _ := s.Get("a1"); e.A.LastPrompt != "fix the failing test in queue_test.go" {
		t.Errorf("ordinary prompt changed: %q", e.A.LastPrompt)
	}
}
