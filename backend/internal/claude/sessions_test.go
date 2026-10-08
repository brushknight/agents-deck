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

func TestSubagentsBusy(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "0b5c2f4e-1111-4222-8333-944455556666.jsonl")
	if SubagentsBusy(tr, time.Minute) {
		t.Fatal("no subagents folder: not busy")
	}
	sub := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "agent-a1.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SubagentsBusy(tr, time.Minute) {
		t.Fatal("fresh subagent transcript: busy")
	}
	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	if SubagentsBusy(tr, time.Minute) {
		t.Fatal("quiet subagent transcript: not busy")
	}
}
