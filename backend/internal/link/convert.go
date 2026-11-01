package link

import (
	"time"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

var tools = map[string]linkpb.Tool{
	"claude": linkpb.Tool_TOOL_CLAUDE, "codex": linkpb.Tool_TOOL_CODEX, "gemini": linkpb.Tool_TOOL_GEMINI,
	"shell": linkpb.Tool_TOOL_SHELL, "opencode": linkpb.Tool_TOOL_OPENCODE, "sim": linkpb.Tool_TOOL_SIM,
}

var statuses = map[model.Status]linkpb.Status{
	model.Starting: linkpb.Status_STATUS_STARTING, model.Running: linkpb.Status_STATUS_RUNNING,
	model.Waiting: linkpb.Status_STATUS_WAITING, model.Idle: linkpb.Status_STATUS_IDLE,
	model.Error: linkpb.Status_STATUS_ERROR, model.Exited: linkpb.Status_STATUS_EXITED,
}

var promptKinds = map[string]linkpb.PromptKind{
	"permission": linkpb.PromptKind_PROMPT_PERMISSION, "question": linkpb.PromptKind_PROMPT_QUESTION,
	"input": linkpb.PromptKind_PROMPT_INPUT,
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// toPB converts an agent, with only the field groups in fields (id and slot
// always), clipped to the link limits.
func toPB(a model.Agent, fields uint32) *linkpb.Agent {
	if fields == 0 {
		fields = uint32(linkpb.FieldGroup_FIELDS_TILE)
	}
	has := func(g linkpb.FieldGroup) bool { return fields&uint32(g) != 0 }
	p := &linkpb.Agent{Id: a.ID, Slot: uint32(a.Slot)}
	if has(linkpb.FieldGroup_FIELDS_TILE) {
		p.Title, p.Tool, p.Status = a.Title, tools[a.Tool], statuses[a.Status]
		p.StatusSinceMs, p.Folder = ms(a.StatusSince), a.Folder
		p.Context = &linkpb.Context{Used: a.Context.Used, Window: a.Context.Window}
		p.Unseen, p.Lost, p.Focused, p.External, p.App = a.Unseen, a.Lost, a.Focused, a.External, a.App
		if a.Hat != nil {
			p.Hat = &linkpb.Hat{Shape: a.Hat.Shape, Color: a.Hat.Color, Automatic: a.Hat.Auto}
		}
		if a.Account != nil {
			p.Account = &linkpb.Account{Name: a.Account.Name, Color: a.Account.Color}
		}
		if a.Activity != nil {
			p.Activity = &linkpb.Activity{Tool: a.Activity.Tool}
		}
		p.SubagentCount = uint32(len(a.Subagents))
	}
	if has(linkpb.FieldGroup_FIELDS_CARD) {
		p.AiTitle, p.Cwd, p.Branch = a.AITitle, a.Cwd, a.Branch
		p.Model, p.ModelLabel, p.Effort = a.Model, a.ModelLabel, a.Effort
		if a.Activity != nil {
			if p.Activity == nil {
				p.Activity = &linkpb.Activity{}
			}
			p.Activity.Detail = a.Activity.Detail
		}
		p.LastPrompt = a.LastPrompt
		if a.Error != nil {
			p.Error = a.Error.Message
		}
		p.Resumable, p.Attached = a.Resumable, a.Attached
		for _, s := range a.Subagents {
			p.Subagents = append(p.Subagents, &linkpb.Subagent{Id: s.ID, Title: s.Title, Type: s.Type, Tool: s.Tool})
		}
		p.StartedAtMs, p.UpdatedAtMs = ms(a.StartedAt), ms(a.UpdatedAt)
	}
	if has(linkpb.FieldGroup_FIELDS_PROMPT) && a.Waiting != nil {
		w := &linkpb.Prompt{Id: a.Waiting.ID, Kind: promptKinds[a.Waiting.Kind], Title: a.Waiting.Title,
			Detail: a.Waiting.Detail, Context: a.Waiting.Context}
		for _, o := range a.Waiting.Options {
			w.Options = append(w.Options, &linkpb.Option{Key: o.Key, Label: o.Label, Primary: o.Primary, Selected: o.Selected})
		}
		p.Waiting = w
	}
	if has(linkpb.FieldGroup_FIELDS_NUMBERS) {
		p.Tokens = &linkpb.Tokens{Input: a.Tokens.Input, Output: a.Tokens.Output, CacheRead: a.Tokens.CacheRead, CacheWrite: a.Tokens.CacheWrite}
		p.CostUsd, p.Turns = a.CostUSD, uint32(a.Turns)
	}
	clipMessage(p.ProtoReflect())
	return p
}
