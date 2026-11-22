// Package claude turns Claude Code hook events and transcripts into agent state.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Events the per-agent settings file subscribes to.
var Events = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PermissionRequest", "Notification", "Stop", "SessionEnd", "PreCompact",
}

// CompactTool is the activity shown while Claude compacts the conversation.
const CompactTool = "Compact"

// HooksSettings is the JSON passed to `claude --settings`: every event runs
// `<agentctl> hook`, which forwards the payload to the daemon and prints nothing.
func HooksSettings(agentctl string) ([]byte, error) {
	cmd := fmt.Sprintf("%q hook", agentctl)
	hooks := map[string]any{}
	for _, ev := range Events {
		group := map[string]any{"hooks": []map[string]any{{"type": "command", "command": cmd, "timeout": 5}}}
		if ev == "PreToolUse" || ev == "PostToolUse" || ev == "PermissionRequest" {
			group["matcher"] = "*"
		}
		hooks[ev] = []any{group}
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
}

// activeEvents are the hooks that mean a message went to the agent or came
// from it: a prompt submitted, a tool call, a question, a turn ending.
// Starting and ending a session are not activity.
var activeEvents = map[string]bool{
	"UserPromptSubmit": true, "PreToolUse": true, "PostToolUse": true,
	"PermissionRequest": true, "Notification": true, "Stop": true, "PreCompact": true,
}

// Now is the clock hooks stamp activity with (tests set it).
var Now = func() time.Time { return time.Now().UTC() }

// Event is the subset of the hook payload we use.
type Event struct {
	Name             string          `json:"hook_event_name"`
	SessionID        string          `json:"session_id"`
	TranscriptPath   string          `json:"transcript_path"`
	Cwd              string          `json:"cwd"`
	Prompt           string          `json:"prompt"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	Source           string          `json:"source"`
	Reason           string          `json:"reason"`
	Trigger          string          `json:"trigger"`  // PreCompact: manual | auto
	AgentID          string          `json:"agent_id"` // set when a subagent fired it (same session id as the main thread)
}

// fromSubagent reports whether a subagent of the session fired the event,
// not its main thread.
func (ev Event) fromSubagent() bool {
	return ev.AgentID != "" || strings.Contains(ev.TranscriptPath, "/subagents/")
}

// asksYou reports whether the event is a prompt that waits for you: those
// show whoever asks, the main thread or a subagent.
func (ev Event) asksYou() bool {
	switch ev.Name {
	case "PermissionRequest":
		return true
	case "PreToolUse":
		return ev.ToolName == "AskUserQuestion"
	case "Notification":
		return ev.NotificationType == "permission_prompt" || ev.NotificationType == "elicitation_dialog" ||
			ev.NotificationType == "" && strings.Contains(ev.Message, "permission")
	}
	return false
}

// Apply folds one hook event into the agent. It returns true when the agent
// changed and whether the pane should be scanned for prompt options.
func Apply(e *store.Entry, ev Event) (changed, scanPane bool) {
	working := e.A.Status == model.Running && !e.Background // in a turn, by its hooks
	sub := ev.fromSubagent()
	if sub && !ev.asksYou() {
		return subagentEvent(e, ev)
	}
	if ev.asksYou() {
		// Whose prompt it is: a subagent's, asked while the main thread was
		// between turns, ends back on the subagents' work (subagentEvent).
		between := sub && !working && e.A.Status != model.Waiting
		defer func() {
			if e.A.Status == model.Waiting {
				e.WaitAgent, e.WaitBackground = "", between
				if sub {
					e.WaitAgent = ev.AgentID
				}
			}
		}()
	}
	// Only the main transcript counts; a subagent's own file would swap the
	// context ring to the subagent's and force a full re-read on the way back.
	if ev.TranscriptPath != "" && e.Transcript != ev.TranscriptPath && !strings.Contains(ev.TranscriptPath, "/subagents/") {
		e.Transcript, e.Offset, e.Usage, e.Pending = ev.TranscriptPath, 0, nil, ""
	}
	if ev.SessionID != "" {
		e.SessionID = ev.SessionID
	}
	if ev.Cwd != "" && e.A.Cwd == "" {
		e.A.Cwd = ev.Cwd
	}
	if activeEvents[ev.Name] {
		// The service logs the time itself, so agents whose transcript it
		// can't read still have one (the transcript, when read, agrees).
		e.MarkActive(Now())
	}
	switch ev.Name {
	case "PreCompact":
		// Compacting between turns (a /compact you typed, or Claude Code
		// compacting an idle session on its own) ends idle again; an
		// auto-compact in the middle of a turn goes on with the turn. Both
		// report the trigger "auto" when it isn't a /compact.
		e.CompactToIdle = ev.Trigger == "manual" || !working
		e.SetStatus(model.Running)
		e.Background = false
		detail := "compacting conversation"
		if ev.Trigger == "auto" {
			detail = "auto-compacting · context full"
		}
		e.A.Activity = &model.Activity{Tool: CompactTool, Detail: detail}
	case "SessionStart":
		if ev.Source == "compact" {
			if !EndCompact(e) {
				return false, false
			}
			break
		}
		e.RunStart = Now() // a new run: subagents of earlier ones are gone
		if e.A.Status == model.Starting || e.A.Status == model.Exited || e.A.Status == model.Error {
			e.SetStatus(model.Idle)
		}
	case "UserPromptSubmit":
		e.A.Unseen = false // it's being fed
		e.SetStatus(model.Running)
		e.Background = false
		e.A.LastPrompt = clip(oneLine(ev.Prompt), model.LastPromptMax)
		e.A.Turns++
	case "PreToolUse":
		if ev.ToolName == "AskUserQuestion" {
			if p := askPrompt(e, ev.ToolInput); p != nil {
				e.A.Waiting = p
				return true, true
			}
		}
		if sub {
			return subagentEvent(e, ev) // a question we could not read: the subagent's own call
		}
		e.SetStatus(model.Running)
		e.Background = false
		e.A.Activity = &model.Activity{Tool: ev.ToolName, Detail: ToolDetail(ev.ToolName, ev.ToolInput, e.A.Cwd)}
	case "PermissionRequest":
		if ev.ToolName == "AskUserQuestion" {
			// Questions also pass through the permission hook; the question prompt
			// set by PreToolUse is the one the user actually sees.
			if e.A.Waiting != nil && e.A.Waiting.Kind != "permission" {
				return false, false
			}
			if p := askPrompt(e, ev.ToolInput); p != nil {
				e.A.Waiting = p
				return true, true
			}
		}
		e.SetStatus(model.Waiting)
		e.A.Waiting = permissionPrompt(e, ev.ToolName, ev.ToolInput)
		return true, true
	case "Notification":
		switch {
		case ev.NotificationType == "permission_prompt" || strings.Contains(ev.Message, "permission"):
			if e.A.Status != model.Waiting {
				e.SetStatus(model.Waiting)
				e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "permission",
					Title: lower(ev.Message), Context: ShortPath(e.A.Cwd), Options: DefaultPermissionOptions()}
			}
			return true, true
		case ev.NotificationType == "idle_prompt":
			if e.A.Status == model.Running && !e.Background { // (subagents at work keep it running)
				e.SetStatus(model.Idle)
			}
		case ev.NotificationType == "elicitation_dialog":
			e.SetStatus(model.Waiting)
			e.A.Waiting = &model.Prompt{ID: e.NextPromptID(), Kind: "input", Title: lower(ev.Message)}
			return true, true
		default:
			return false, false
		}
	case "PostToolUse":
		e.SetStatus(model.Running)
		e.Background = false
		e.A.Activity = nil
	case "Stop":
		e.A.Unseen = true // finished: hungry until you look at it (the daemon clears it if you are)
		// Its background subagents may still work: then it goes on running
		// for them, without showing idle in between.
		e.A.Subagents = Subagents(e.Transcript, e.RunSince(), Now())
		if len(e.A.Subagents) > 0 {
			ShowBackground(e)
		} else {
			e.SetStatus(model.Idle)
		}
	case "SessionEnd":
		e.SetStatus(model.Exited)
		e.A.Unseen = false
	default:
		return false, false
	}
	return true, false
}

// subagentEvent folds in a hook event a subagent fired that asks nothing of
// you (its tool calls). Those are the subagent's, not the main thread's: they
// change nothing, except that the subagent that asked you something going on
// ends the wait (you answered).
func subagentEvent(e *store.Entry, ev Event) (changed, scanPane bool) {
	if e.A.Status != model.Waiting || e.WaitAgent == "" || ev.AgentID != e.WaitAgent || ev.Name != "PreToolUse" && ev.Name != "PostToolUse" {
		return false, false
	}
	if e.WaitBackground {
		ShowBackground(e) // asked between turns: back to the subagents' work
	} else {
		e.SetStatus(model.Running) // asked while the main thread waits on the subagent
		e.A.Activity = &model.Activity{Tool: "Task"}
	}
	return true, false
}

// EndCompact clears a running compaction: one that started between turns
// leaves the agent idle (it was waiting for you before), one in the middle of
// a turn goes on working.
func EndCompact(e *store.Entry) bool {
	if e.A.Activity == nil || e.A.Activity.Tool != CompactTool {
		return false
	}
	if e.CompactToIdle && len(e.A.Subagents) > 0 {
		ShowBackground(e) // its subagents work on
	} else if e.CompactToIdle {
		e.SetStatus(model.Idle)
	} else {
		e.A.Activity = nil
	}
	e.CompactToIdle = false
	return true
}

// SettleAfter is how long an agent whose transcript shows its turn over can
// still show as working: time for the Stop hook to arrive, and for a prompt
// the hooks just reported to reach the transcript.
const SettleAfter = 30 * time.Second

// compactLimit is how long a compaction is left to report its own end.
const compactLimit = 10 * time.Minute

// Settle ends a turn the hooks never reported as ended, from the transcript:
// a turn interrupted with Esc (no hook runs for that), a Stop hook lost while
// the service restarted, a command that ran no turn, a status replayed from
// old transcript lines. It goes by where the turn stands, not by how long it
// has been quiet: an agent in a long tool call or a long think has an open
// turn and stays working. A question or permission prompt ends the same way
// once the turn is over after it came up (refused or interrupted in the
// terminal). It reports whether the agent changed.
func Settle(e *store.Entry, now time.Time) bool {
	if e.Transcript == "" || e.Background {
		return false
	}
	switch e.A.Status {
	case model.Running:
		since := now.Sub(e.A.StatusSince)
		compacting := e.A.Activity != nil && e.A.Activity.Tool == CompactTool
		if since < SettleAfter || compacting && since < compactLimit {
			return false
		}
	case model.Waiting:
	default:
		return false
	}
	t, ok := ReadTurn(e.Transcript)
	if !ok || !t.Over {
		return false
	}
	if e.A.Status == model.Running && now.Sub(t.At) < SettleAfter {
		return false // the Stop hook may be on its way
	}
	if e.A.Status == model.Waiting && !t.At.After(e.A.StatusSince) {
		return false // the turn that ended is an older one
	}
	e.SetStatus(model.Idle)
	e.CompactToIdle = false
	return true
}

// DefaultPermissionOptions mirror Claude Code's dialog when the pane can't be read.
func DefaultPermissionOptions() []model.Option {
	return []model.Option{
		{Key: "1", Label: "yes", Primary: true},
		{Key: "2", Label: "yes, and don't ask again"},
		{Key: "3", Label: "no"},
	}
}

// PromptFor is the prompt for a tool call that waits for you: a question
// (AskUserQuestion) or a permission request, as the hooks build it.
func PromptFor(e *store.Entry, tool string, input json.RawMessage) *model.Prompt {
	if tool == "AskUserQuestion" {
		if p := questionPrompt(e, input); p != nil {
			return p
		}
	}
	return permissionPrompt(e, tool, input)
}

func permissionPrompt(e *store.Entry, tool string, input json.RawMessage) *model.Prompt {
	title, ctx := "allow "+strings.ToLower(tool)+"?", strings.ToLower(tool)
	detail := ToolDetail(tool, input, e.A.Cwd)
	switch tool {
	case "Bash":
		title = "run this command?"
		detail = field(input, "command") // full command, not the one-line summary
	case "Edit", "MultiEdit", "NotebookEdit":
		title = "edit this file?"
	case "Write":
		title = "create this file?"
	case "Read":
		title = "read this file?"
	case "WebFetch":
		title = "fetch this url?"
	case "WebSearch":
		title = "search the web?"
	}
	if strings.HasPrefix(tool, "mcp__") {
		parts := strings.Split(tool, "__")
		title, ctx = "use "+parts[len(parts)-1]+"?", "mcp · "+parts[1]
	}
	if e.A.Cwd != "" {
		ctx += " · " + ShortPath(e.A.Cwd)
	}
	return &model.Prompt{ID: e.NextPromptID(), Kind: "permission", Title: title, Detail: clip(detail, 2000),
		Context: ctx, Options: DefaultPermissionOptions()}
}

// askPrompt is the prompt for an AskUserQuestion the agent's hooks report,
// with the agent set waiting: the first step of a form when it takes more
// than one key (see form.go), else the question.
func askPrompt(e *store.Entry, input json.RawMessage) *model.Prompt {
	if f := newForm(input); f != nil {
		e.SetStatus(model.Waiting)
		e.Form = f
		return formPrompt(e)
	}
	p := questionPrompt(e, input)
	if p != nil {
		e.SetStatus(model.Waiting)
	}
	return p
}

// questionPrompt is a single question with its options. A form (several
// questions) is answered at the terminal here; agents whose hooks report to
// the daemon get askPrompt instead, which walks it.
func questionPrompt(e *store.Entry, input json.RawMessage) *model.Prompt {
	var in struct {
		Questions []struct {
			Question string `json:"question"`
			Header   string `json:"header"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(input, &in) != nil || len(in.Questions) == 0 {
		return nil
	}
	q := in.Questions[0]
	p := &model.Prompt{ID: e.NextPromptID(), Kind: "question", Title: lower(q.Question),
		Context: fmt.Sprintf("question · 1 of %d", len(in.Questions))}
	for i, o := range q.Options {
		label := o.Label
		if o.Description != "" {
			label += " — " + o.Description
		}
		p.Options = append(p.Options, model.Option{Key: fmt.Sprint(i + 1), Label: label, Primary: i == 0})
	}
	if len(in.Questions) > 1 || len(p.Options) == 0 {
		p.Kind = "input" // forms are answered at the terminal (or walked, see askPrompt)
		p.Options = nil
	}
	return p
}

// ToolDetail is a one-line summary of what a tool call touches.
func ToolDetail(tool string, input json.RawMessage, cwd string) string {
	rel := func(p string) string {
		if cwd != "" {
			if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
				return r
			}
		}
		return ShortPath(p)
	}
	switch tool {
	case "Bash":
		return clip(oneLine(field(input, "command")), 160)
	case "Edit", "MultiEdit", "Write", "Read", "NotebookEdit":
		if p := field(input, "file_path"); p != "" {
			return rel(p)
		}
		return rel(field(input, "notebook_path"))
	case "Grep", "Glob":
		return clip(field(input, "pattern"), 120)
	case "WebFetch":
		return clip(field(input, "url"), 160)
	case "WebSearch":
		return clip(field(input, "query"), 120)
	case "Task", "Agent":
		return clip(field(input, "description"), 120)
	case "TodoWrite":
		var in struct {
			Todos []json.RawMessage `json:"todos"`
		}
		_ = json.Unmarshal(input, &in)
		return fmt.Sprintf("planning %d steps", len(in.Todos))
	}
	return ""
}

func field(raw json.RawMessage, key string) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// ShortPath renders a path with ~ for the home directory.
func ShortPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func lower(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'a' && r[1] <= 'z' { // keep acronyms like "API"
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}
