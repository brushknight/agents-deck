package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Captured from Claude Code 2.1.281 (paths shortened).
const permissionPane = `⏺ Bash(date > now.txt)
  ⎿  Waiting…
────────────────────────────────────────────────────────────
 Bash command
   date > now.txt
   Write current date to now.txt
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to /tmp/at-proj from this project
   3. No
 Esc to cancel · Tab to amend`

const questionPane = `────────────────────────────────────────────────────────────
 ☐ Color
Which color do you prefer?
❯ 1. Red
     A warm, vibrant color
  2. Green
     A natural, calming color
  3. Blue
     A cool, peaceful color
  4. Type something.
────────────────────────────────────────────────────────────
  5. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel`

const trustPane = `────────────────────────────────────────────────────────────
 Accessing workspace:
 /tmp/at-proj
 Quick safety check: Is this a project you created or one you trust?
 Claude Code'll be able to read, edit, and execute files here.
 Security guide
 ❯ No, exit
   Yes, I trust this folder
 Enter to confirm · Esc to cancel`

func TestParseOptionsPermission(t *testing.T) {
	got := ParseOptions(permissionPane)
	want := []string{"yes", "yes, and always allow access to /tmp/at-proj from this project", "no"}
	if len(got) != len(want) {
		t.Fatalf("got %d options: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Label != w || got[i].Key != string(rune('1'+i)) {
			t.Errorf("option %d = %+v, want %q", i, got[i], w)
		}
	}
	if !got[0].Primary {
		t.Error("first option should be primary")
	}
}

func TestParseOptionsIgnoresEarlierLists(t *testing.T) {
	pane := "Plan:\n 1. read files\n 2. edit\n 3. test\n" + permissionPane
	if got := ParseOptions(pane); len(got) != 3 || got[2].Label != "no" {
		t.Fatalf("want the dialog's options, got %+v", got)
	}
}

func TestParseOptionsNone(t *testing.T) {
	if got := ParseOptions("just some output\n❯ \n"); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestRefineKeepsQuestionLabels(t *testing.T) {
	p := &model.Prompt{Kind: "question", Options: []model.Option{{Key: "1", Label: "red — warm"}, {Key: "2", Label: "green"}, {Key: "3", Label: "blue"}}}
	if RefineOptions(p, questionPane) || len(p.Options) != 3 || p.Options[0].Label != "red — warm" {
		t.Fatalf("question options must not change: %+v", p.Options)
	}
}

func TestRefinePermission(t *testing.T) {
	p := &model.Prompt{Kind: "permission", Options: DefaultPermissionOptions()}
	if !RefineOptions(p, permissionPane) {
		t.Fatal("expected refinement")
	}
	if !strings.Contains(p.Options[1].Label, "always allow") {
		t.Fatalf("got %+v", p.Options)
	}
	if RefineOptions(p, permissionPane) {
		t.Fatal("second refinement with the same pane should report no change")
	}
}

func TestParseCursorMenuTrust(t *testing.T) {
	title, opts, ok := ParseCursorMenu(trustPane)
	if !ok {
		t.Fatal("trust menu not found")
	}
	if title != "trust this folder?" {
		t.Errorf("title = %q", title)
	}
	if len(opts) != 2 || opts[0].Label != "no, exit" || opts[1].Label != "yes, I trust this folder" {
		t.Fatalf("opts = %+v", opts)
	}
	if opts[0].Primary || !opts[1].Primary {
		t.Error("the yes option should be primary")
	}
}

func TestParseCursorMenuRejectsNumbered(t *testing.T) {
	if _, _, ok := ParseCursorMenu(permissionPane); ok {
		t.Fatal("numbered dialog is not a cursor menu")
	}
	if _, _, ok := ParseCursorMenu("❯ \n"); ok {
		t.Fatal("prompt line is not a menu")
	}
}

func ev(name string, kv ...string) Event {
	e := Event{Name: name}
	for i := 0; i+1 < len(kv); i += 2 {
		switch kv[i] {
		case "prompt":
			e.Prompt = kv[i+1]
		case "tool":
			e.ToolName = kv[i+1]
		case "input":
			e.ToolInput = json.RawMessage(kv[i+1])
		case "type":
			e.NotificationType = kv[i+1]
		case "message":
			e.Message = kv[i+1]
		}
	}
	return e
}

func TestApplyLifecycle(t *testing.T) {
	e := &store.Entry{}
	e.A.Status = model.Starting
	e.A.Cwd = "/work/proj"

	steps := []struct {
		ev   Event
		want model.Status
	}{
		{ev("SessionStart"), model.Idle},
		{ev("UserPromptSubmit", "prompt", "fix   the\nbug"), model.Running},
		{ev("PreToolUse", "tool", "Edit", "input", `{"file_path":"/work/proj/src/a.go"}`), model.Running},
		{ev("PermissionRequest", "tool", "Bash", "input", `{"command":"rm -rf build\nmake"}`), model.Waiting},
		{ev("PostToolUse", "tool", "Bash"), model.Running},
		{ev("Stop"), model.Idle},
		{ev("SessionEnd"), model.Exited},
	}
	for i, s := range steps {
		Apply(e, s.ev)
		if e.A.Status != s.want {
			t.Fatalf("step %d (%s): status %s, want %s", i, s.ev.Name, e.A.Status, s.want)
		}
		switch i {
		case 1:
			if e.A.LastPrompt != "fix the bug" || e.A.Turns != 1 {
				t.Errorf("prompt %q turns %d", e.A.LastPrompt, e.A.Turns)
			}
		case 2:
			if e.A.Activity == nil || e.A.Activity.Detail != "src/a.go" {
				t.Errorf("activity %+v", e.A.Activity)
			}
		case 3:
			p := e.A.Waiting
			if p == nil || p.Kind != "permission" || p.Title != "run this command?" || p.Detail != "rm -rf build\nmake" {
				t.Fatalf("prompt %+v", p)
			}
			if e.A.Activity != nil {
				t.Error("activity should clear while waiting")
			}
		case 4:
			if e.A.Waiting != nil {
				t.Error("waiting should clear once the tool ran")
			}
		}
	}
}

func TestApplyQuestionBeatsPermissionHook(t *testing.T) {
	e := &store.Entry{}
	in := `{"questions":[{"question":"Which color?","options":[{"label":"Red","description":"warm"},{"label":"Green"}]}]}`
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", in))
	first := e.A.Waiting
	if first == nil || first.Kind != "question" || len(first.Options) != 2 || first.Options[0].Label != "Red — warm" {
		t.Fatalf("question %+v", first)
	}
	Apply(e, ev("PermissionRequest", "tool", "AskUserQuestion", "input", in))
	if e.A.Waiting != first {
		t.Fatalf("permission hook replaced the question: %+v", e.A.Waiting)
	}
}

func TestApplyMultiQuestionIsInput(t *testing.T) {
	e := &store.Entry{}
	in := `{"questions":[{"question":"A?","options":[{"label":"x"},{"label":"y"}]},{"question":"B?","options":[{"label":"z"},{"label":"w"}]}]}`
	Apply(e, ev("PreToolUse", "tool", "AskUserQuestion", "input", in))
	if e.A.Waiting == nil || e.A.Waiting.Kind != "input" || len(e.A.Waiting.Options) != 0 {
		t.Fatalf("multi-question form should be input-only: %+v", e.A.Waiting)
	}
}

func TestApplyPromptIDsChange(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("PermissionRequest", "tool", "Bash", "input", `{"command":"a"}`))
	id1 := e.A.Waiting.ID
	Apply(e, ev("PostToolUse"))
	Apply(e, ev("PermissionRequest", "tool", "Bash", "input", `{"command":"b"}`))
	if e.A.Waiting.ID == id1 {
		t.Fatal("a new prompt must get a new id")
	}
}

func TestToolDetail(t *testing.T) {
	cases := []struct{ tool, in, want string }{
		{"Bash", `{"command":"go   test\n./..."}`, "go test ./..."},
		{"Read", `{"file_path":"/work/proj/main.go"}`, "main.go"},
		{"Grep", `{"pattern":"TODO"}`, "TODO"},
		{"TodoWrite", `{"todos":[{},{},{}]}`, "planning 3 steps"},
		{"Unknown", `{}`, ""},
	}
	for _, c := range cases {
		if got := ToolDetail(c.tool, json.RawMessage(c.in), "/work/proj"); got != c.want {
			t.Errorf("%s: %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestHooksSettingsShape(t *testing.T) {
	b, err := HooksSettings("/usr/local/bin/agentctl")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	for _, name := range Events {
		g := s.Hooks[name]
		if len(g) != 1 || len(g[0].Hooks) != 1 || g[0].Hooks[0].Command != `"/usr/local/bin/agentctl" hook` {
			t.Errorf("%s: %+v", name, g)
		}
	}
	if s.Hooks["PreToolUse"][0].Matcher != "*" {
		t.Error("tool hooks need a matcher")
	}
}

func TestTailTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","gitBranch":"main"}`,
		// one API message split across two content-block entries: counted once
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}`,
		`{"type":"ai-title","aiTitle":"Fix the bug"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &store.Entry{Transcript: path}
	if !Tail(e) {
		t.Fatal("expected change")
	}
	if e.A.Tokens.Output != 100 || e.A.Tokens.CacheRead != 1000 {
		t.Fatalf("tokens %+v (duplicate message counted twice?)", e.A.Tokens)
	}
	if e.A.Context.Used != 1510 || e.A.Context.Window != 1_000_000 {
		t.Errorf("context %+v", e.A.Context)
	}
	if e.A.ModelLabel != "opus 5.5" || e.A.Branch != "main" || e.A.AITitle != "Fix the bug" {
		t.Errorf("model %q branch %q title %q", e.A.ModelLabel, e.A.Branch, e.A.AITitle)
	}
	if e.A.CostUSD <= 0 {
		t.Error("cost should be positive")
	}
	if Tail(e) {
		t.Error("no new bytes: no change")
	}

	// A partial line is held until its newline arrives.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","isApiErrorMessage":true,"message":{"id":"m2","model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 Overloaded"}]`)
	f.Close()
	if Tail(e) {
		t.Error("partial line must not be parsed")
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("}}\n")
	f.Close()
	Tail(e)
	if e.A.Status != model.Error || e.A.Error == nil || !strings.Contains(e.A.Error.Message, "529") {
		t.Fatalf("api error not surfaced: %s %+v", e.A.Status, e.A.Error)
	}
}

func TestDisplayTitle(t *testing.T) {
	cases := []struct {
		e    store.Entry
		want string
	}{
		{store.Entry{A: model.Agent{Folder: "api"}}, "api"},
		{store.Entry{A: model.Agent{Folder: "api", AITitle: "Fix login race"}}, "Fix login race"},
		{store.Entry{A: model.Agent{Folder: "api", AITitle: "Fix login race"}, CustomTitle: "api"}, "Fix login race"}, // old -n folder launches
		{store.Entry{A: model.Agent{Folder: "api", AITitle: "Fix login race"}, CustomTitle: "auth rewrite"}, "auth rewrite"},
		{store.Entry{A: model.Agent{Folder: "api", Title: "mine", AITitle: "x"}, TitleSet: true, CustomTitle: "y"}, "mine"},
	}
	for i, c := range cases {
		if got := c.e.DisplayTitle(); got != c.want {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}

func TestTailRenamesLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"custom-title","customTitle":"api"}`+"\n"+`{"type":"ai-title","aiTitle":"Fix login race"}`+"\n"), 0o600)
	e := &store.Entry{Transcript: path, A: model.Agent{Folder: "api", Title: "api"}}
	Tail(e)
	if e.A.Title != "Fix login race" {
		t.Fatalf("title %q", e.A.Title)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"custom-title","customTitle":"auth rewrite"}` + "\n")
	f.Close()
	Tail(e)
	if e.A.Title != "auth rewrite" {
		t.Fatalf("rename not picked up: %q", e.A.Title)
	}
}

func TestApplyHungry(t *testing.T) {
	e := &store.Entry{}
	Apply(e, ev("UserPromptSubmit", "prompt", "go"))
	if e.A.Unseen {
		t.Fatal("working agent can't be hungry")
	}
	Apply(e, ev("Stop"))
	if !e.A.Unseen || e.A.Status != model.Idle {
		t.Fatalf("finished turn should be unseen: %+v", e.A)
	}
	Apply(e, ev("UserPromptSubmit", "prompt", "next"))
	if e.A.Unseen {
		t.Fatal("feeding it a prompt clears hungry")
	}
}

func TestTailCatchesUpInOneCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	msg := func(id string, ctx int) string {
		return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"model":"claude-opus-5-5","usage":{"input_tokens":%d,"output_tokens":10}}}`, id, ctx) + "\n"
	}
	var b strings.Builder
	b.WriteString(msg("early", 900_000)) // before a compaction
	pad := `{"type":"user","message":{"content":"` + strings.Repeat("x", 4000) + `"}}` + "\n"
	for b.Len() < 3*maxRead {
		b.WriteString(pad)
	}
	b.WriteString(msg("late", 50_000))
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &store.Entry{Transcript: path}
	if !Tail(e) {
		t.Fatal("no change")
	}
	if e.A.Context.Used != 50_000 || e.Offset != int64(b.Len()) {
		t.Fatalf("one call should reach the end: ctx %d, offset %d of %d", e.A.Context.Used, e.Offset, b.Len())
	}
	if e.A.Tokens.Input != 950_000 {
		t.Fatalf("totals over the whole file: %+v", e.A.Tokens)
	}
}

func TestCompaction(t *testing.T) {
	e := &store.Entry{}
	e.SetStatus(model.Idle)
	// A /compact you typed: compacting, then back to idle (not hungry).
	Apply(e, Event{Name: "PreCompact", Trigger: "manual"})
	if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != CompactTool || e.A.Activity.Detail != "compacting conversation" {
		t.Fatalf("manual start: %+v %+v", e.A, e.A.Activity)
	}
	Apply(e, Event{Name: "SessionStart", Source: "compact"})
	if e.A.Status != model.Idle || e.A.Activity != nil || e.A.Unseen {
		t.Fatalf("manual end: %+v %+v", e.A, e.A.Activity)
	}
	// Auto-compact mid-turn: the turn goes on, and the transcript's boundary
	// line ends it if the hook didn't.
	e.SetStatus(model.Running)
	Apply(e, Event{Name: "PreCompact", Trigger: "auto"})
	if e.A.Activity == nil || e.A.Activity.Detail != "auto-compacting · context full" {
		t.Fatalf("auto start: %+v", e.A.Activity)
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Transcript = path
	Tail(e)
	if e.A.Status != model.Running || e.A.Activity != nil {
		t.Fatalf("auto end: %+v %+v", e.A, e.A.Activity)
	}
	// A stray compact SessionStart changes nothing.
	if changed, _ := Apply(e, Event{Name: "SessionStart", Source: "compact"}); changed {
		t.Fatal("no compaction running")
	}
}
