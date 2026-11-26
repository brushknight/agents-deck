package store

import (
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/redact"
)

// scrub takes secrets out of the agent text the deck shows and sends: the
// last prompt and answer, the session's title, what it is doing, the
// question it waits on and its subagents' tasks. The store calls it on every
// change, so /v1, the dashboard, MCP, Deck Link and the saved state only ever
// hold the redacted text. redact.String leaves clean text as it is, so
// scrubbing twice changes nothing.
//
// Watchers that compare what they read with what the store holds compare
// against redact.String of what they read (CleanPrompt for a prompt).
func scrub(e *Entry) {
	a := &e.A
	a.LastPrompt = redact.String(a.LastPrompt)
	a.LastAnswer = redact.String(a.LastAnswer)
	a.AITitle = redact.String(a.AITitle)
	if a.Activity != nil {
		a.Activity.Detail = redact.String(a.Activity.Detail)
	}
	CleanPrompt(a.Waiting)
	for i := range a.Subagents {
		a.Subagents[i].Title = redact.String(a.Subagents[i].Title)
	}
}

// CleanPrompt redacts a prompt's text in place, as the store keeps it.
func CleanPrompt(p *model.Prompt) {
	if p == nil {
		return
	}
	p.Title = redact.String(p.Title)
	p.Detail = redact.String(p.Detail)
	p.Context = redact.String(p.Context)
	for i := range p.Options {
		p.Options[i].Label = redact.String(p.Options[i].Label)
	}
}
