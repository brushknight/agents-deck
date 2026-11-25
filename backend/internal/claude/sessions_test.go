package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSession(t *testing.T, dir, project, id string, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, project, id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanSessions(t *testing.T) {
	dir := t.TempDir()
	a := "11111111-2222-4333-8444-555555555555"
	b := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	// A big transcript: metadata at the start and the end, megabytes between.
	filler := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 4000) + `"}}`
	big := []string{`{"type":"user","cwd":"/work/api","gitBranch":"main"}`}
	for i := 0; i < 600; i++ {
		big = append(big, filler)
	}
	big = append(big,
		`{"type":"ai-title","aiTitle":"Fix refresh token race"}`,
		`{"type":"last-prompt","lastPrompt":"add a   regression\ntest"}`,
		`{"type":"user","cwd":"/work/api","gitBranch":"fix/race"}`)
	pa := writeSession(t, dir, "-work-api", a, big...)
	pb := writeSession(t, dir, "-work-docs", b, `{"type":"user","cwd":"/work/docs"}`)
	writeSession(t, dir, "-work-docs", "not-a-session", `{"cwd":"/x"}`)
	writeSession(t, dir, "-work-docs/subagents", "cccccccc-bbbb-4ccc-8ddd-eeeeeeeeeeee", `{"cwd":"/x"}`)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(pb, old, old)
	_ = pa

	got, err := ScanSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d: %+v", len(got), got)
	}
	s := got[0] // newest first
	if s.ID != a || s.Cwd != "/work/api" || s.Branch != "fix/race" || s.AITitle != "Fix refresh token race" || s.LastPrompt != "add a regression test" {
		t.Fatalf("big session parsed wrong: %+v", s)
	}
	if got[1].ID != b || got[1].Title() != "docs" {
		t.Fatalf("second: %+v", got[1])
	}
}

func TestMatchAndFind(t *testing.T) {
	all := []SessionInfo{
		{ID: "11111111-2222-4333-8444-555555555555", AITitle: "Fix refresh token race", Cwd: "/work/api", Branch: "main"},
		{ID: "11112222-2222-4333-8444-555555555555", AITitle: "Docs", Cwd: "/work/docs", LastPrompt: "rewrite install guide"},
	}
	if !all[0].Match("TOKEN api") || all[0].Match("token docs") || !all[1].Match("install") {
		t.Error("match")
	}
	if !IsSessionID(all[0].ID) || IsSessionID("'; rm -rf ~") {
		t.Error("IsSessionID")
	}
	_ = fmt.Sprint
}

// The fallback: a session whose main transcript says nothing about its
// subagents (see subagents_test.go for the main transcript's records).
func TestSubagentsFallback(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "0b5c2f4e-1111-4222-8333-944455556666.jsonl")
	if subs := Subagents(tr, time.Time{}, time.Now()); subs != nil {
		t.Fatalf("no subagents folder: %v", subs)
	}
	sub := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) string {
		p := filepath.Join(sub, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	toolCall := `{"type":"assistant","message":{"stop_reason":null,"content":[{"type":"tool_use","name":"Grep"}]}}`
	result := `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`
	final := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`
	write("agent-a1.jsonl", toolCall+"\n"+result+"\n"+`{"type":"attachment","attachment":{}}`+"\n")
	write("agent-a1.meta.json", `{"agentType":"Explore","description":"find  the\nauth code"}`)
	write("agent-a2.jsonl", toolCall+"\n"+result+"\n"+final+"\n") // finished
	// a3: quiet for 20 minutes after a tool result (a long reply): working.
	// a4: quiet for 31 minutes: gone. ("If they did something in 30 minutes,
	// keep the main thread active.")
	inTool := write("agent-a3.jsonl", toolCall+"\n"+result+"\n")
	quiet := write("agent-a4.jsonl", toolCall+"\n"+result+"\n")
	for f, ago := range map[string]time.Duration{inTool: 20 * time.Minute, quiet: 31 * time.Minute} {
		old := time.Now().Add(-ago)
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	subs := Subagents(tr, time.Time{}, time.Now())
	if len(subs) != 2 || subs[1].ID != "a3" && subs[0].ID != "a3" {
		t.Fatalf("want a1 and a3 working: %+v", subs)
	}
	if got := Subagents(tr, time.Time{}, time.Now().Add(15*time.Minute)); len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("a3 quiet past half an hour is over: %+v", got)
	}
	for _, x := range subs {
		if x.ID == "a1" {
			subs[0] = x
		}
	}
	if s := subs[0]; s.ID != "a1" || s.Title != "find the auth code" || s.Type != "Explore" || s.Tool != "Grep" {
		t.Fatalf("a1: %+v", s)
	}
}
