// Package claude turns Claude Code hook events and transcripts into agent state.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Events the per-agent settings file subscribes to.
var Events = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PermissionRequest", "Notification", "Stop", "SessionEnd",
}

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
}

// Apply folds one hook event into the agent. It returns true when the agent
// changed and whether the pane should be scanned for prompt options.
func Apply(e *store.Entry, ev Event) (changed, scanPane bool) {
	e.Background = false // hooks are the real status again
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
	switch ev.Name {
	case "SessionStart":
		if e.A.Status == model.Starting || e.A.Status == model.Exited || e.A.Status == model.Error {
			e.SetStatus(model.Idle)
		}
	case "UserPromptSubmit":
		e.A.Unseen = false // it's being fed
		e.SetStatus(model.Running)
		e.A.LastPrompt = clip(oneLine(ev.Prompt), 280)
		e.A.Turns++
	case "PreToolUse":
		if ev.ToolName == "AskUserQuestion" {
			if p := questionPrompt(e, ev.ToolInput); p != nil {
				e.SetStatus(model.Waiting)
				e.A.Waiting = p
				return true, true
			}
		}
		e.SetStatus(model.Running)
		e.A.Activity = &model.Activity{Tool: ev.ToolName, Detail: ToolDetail(ev.ToolName, ev.ToolInput, e.A.Cwd)}
	case "PermissionRequest":
		if ev.ToolName == "AskUserQuestion" {
			// Questions also pass through the permission hook; the question prompt
			// set by PreToolUse is the one the user actually sees.
			if e.A.Waiting != nil && e.A.Waiting.Kind != "permission" {
				return false, false
			}
			if p := questionPrompt(e, ev.ToolInput); p != nil {
				e.SetStatus(model.Waiting)
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
			if e.A.Status == model.Running {
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
		e.A.Activity = nil
	case "Stop":
		e.SetStatus(model.Idle)
		e.A.Unseen = true // finished: hungry until you look at it (the daemon clears it if you are)
	case "SessionEnd":
		e.SetStatus(model.Exited)
		e.A.Unseen = false
	default:
		return false, false
	}
	return true, false
}

// DefaultPermissionOptions mirror Claude Code's dialog when the pane can't be read.
func DefaultPermissionOptions() []model.Option {
	return []model.Option{
		{Key: "1", Label: "yes", Primary: true},
		{Key: "2", Label: "yes, and don't ask again"},
		{Key: "3", Label: "no"},
	}
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
		p.Kind = "input" // multi-question forms are answered at the terminal
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
