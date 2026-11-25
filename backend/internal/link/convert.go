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
		if a.Project != nil {
			p.Project = &linkpb.Project{Id: a.Project.ID, Name: a.Project.Name}
		}
		if a.Activity != nil {
			p.Activity = &linkpb.Activity{Tool: a.Activity.Tool}
		}
		p.SubagentCount = uint32(len(a.Subagents))
		p.AttentionSlot = uint32(a.AttentionSlot)
		p.Queued = uint32(a.Queued)
		p.Slowed, p.SlowedByYou = a.Slowed, a.SlowedByYou
		if a.Pet != nil {
			p.Pet = &linkpb.Pet{Name: a.Pet.Name, Stage: a.Pet.Stage, Level: uint32(a.Pet.Level), Ate: a.Pet.Ate,
				NextAt: a.Pet.NextAt, Hearts: uint32(a.Pet.Hearts), Mood: a.Pet.Mood, HatchedAtMs: ms(a.Pet.HatchedAt),
				Ready: a.Pet.Ready, LevelFrom: a.Pet.LevelFrom}
			if a.Pet.Hat != nil {
				p.Pet.Hat = &linkpb.Hat{Shape: a.Pet.Hat.Shape, Color: a.Pet.Hat.Color}
			}
		}
		p.OutUntilMs = ms(a.OutUntil)
	}
	if has(linkpb.FieldGroup_FIELDS_CARD) {
		p.AiTitle, p.Cwd, p.Branch, p.ProjectFolder = a.AITitle, a.Cwd, a.Branch, a.ProjectFolder
		p.Model, p.ModelLabel, p.Effort = a.Model, a.ModelLabel, a.Effort
		if a.Activity != nil {
			if p.Activity == nil {
				p.Activity = &linkpb.Activity{}
			}
			p.Activity.Detail = a.Activity.Detail
		}
		p.LastPrompt = a.LastPrompt
		p.LastAnswer, p.LastAnswerAtMs = a.LastAnswer, ms(a.LastAnswerAt)
		if a.Error != nil {
			p.Error = a.Error.Message
		}
		p.Resumable, p.Attached = a.Resumable, a.Attached
		for _, s := range a.Subagents {
			p.Subagents = append(p.Subagents, &linkpb.Subagent{Id: s.ID, Title: s.Title, Type: s.Type, Tool: s.Tool})
		}
		p.StartedAtMs, p.UpdatedAtMs, p.LastActiveMs = ms(a.StartedAt), ms(a.UpdatedAt), ms(a.LastActive)
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
		p.CostUsd, p.Turns, p.Cpu = a.CostUSD, uint32(a.Turns), float32(a.CPU)
	}
	clipMessage(p.ProtoReflect())
	return p
}

var limitLevels = map[string]linkpb.LimitLevel{"near": linkpb.LimitLevel_LIMIT_NEAR, "low": linkpb.LimitLevel_LIMIT_LOW, "out": linkpb.LimitLevel_LIMIT_OUT}

// limitsPB converts the accounts' usage limits, without the windows that
// aren't shown (five_hour), clipped to the link limits.
func limitsPB(ls []model.Limits) *linkpb.Limits {
	out := &linkpb.Limits{}
	for _, l := range ls {
		set := &linkpb.LimitSet{Tool: tools[l.Tool], Account: l.Account, Level: limitLevels[l.Level], AsOfMs: ms(l.AsOf)}
		for _, w := range l.Windows {
			if w.Hidden {
				continue
			}
			set.Windows = append(set.Windows, &linkpb.LimitWindow{Label: w.Label, Used: uint32(max(w.Used, 0)),
				ResetsAtMs: ms(w.ResetsAt), Level: limitLevels[w.Level], Hit: w.Hit, Stale: w.Reset})
		}
		if len(set.Windows) > 0 {
			out.Sets = append(out.Sets, set)
		}
	}
	clipMessage(out.ProtoReflect())
	return out
}
