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
	if _, n := FindSession(all, "1111"); n != 2 {
		t.Errorf("ambiguous prefix: %d", n)
	}
	if s, n := FindSession(all, "11112"); n != 1 || s.AITitle != "Docs" {
		t.Errorf("unique prefix: %d %+v", n, s)
	}
	if _, n := FindSession(all, "111"); n != 0 {
		t.Error("too-short prefix must not match")
	}
	if s, n := FindSession(all, all[0].ID); n != 1 || s.ID != all[0].ID {
		t.Error("full id")
	}
	if !IsSessionID(all[0].ID) || IsSessionID("'; rm -rf ~") {
		t.Error("IsSessionID")
	}
	_ = fmt.Sprint
}

func TestSubagents(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "0b5c2f4e-1111-4222-8333-944455556666.jsonl")
	if subs := Subagents(tr, time.Minute, time.Hour); subs != nil {
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
	old := time.Now().Add(-5 * time.Minute)
	// a3: quiet for 5 minutes, but inside a tool call (a long build): working.
	inTool := write("agent-a3.jsonl", toolCall+"\n")
	// a4: quiet for 5 minutes after a tool result: gone.
	quiet := write("agent-a4.jsonl", toolCall+"\n"+result+"\n")
	for _, f := range []string{inTool, quiet} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	subs := Subagents(tr, time.Minute, time.Hour)
	if len(subs) != 2 || subs[1].ID != "a3" && subs[0].ID != "a3" {
		t.Fatalf("want a1 and a3 working: %+v", subs)
	}
	if got := Subagents(tr, time.Minute, 2*time.Minute); len(got) != 1 {
		t.Fatalf("a tool call past toolWindow is over: %+v", got)
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
