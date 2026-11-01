package daemon

import (
	"errors"
	"log"
	"regexp"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A harness is an app that runs coding agents itself (Orca, herdr, cmux).
// Its watcher mirrors its agents onto the board with Agent.App set to the
// app's name; what you do on the deck goes back through the app. Adding one:
// docs/harnesses.md.
type harness struct {
	send      func(e store.Entry, key string) error // types an answer's key
	interrupt func(e store.Entry) error             // stops the current turn
	focus     func(e store.Entry) error             // shows the agent on the Mac
	hide      func(e store.Entry)                   // off the deck until it's active again
}

// harnessOf is the app an agent runs in, if it runs in one.
func (d *Daemon) harnessOf(e store.Entry) (harness, bool) {
	switch e.A.App {
	case "orca":
		return harness{d.orcaSend, d.orcaInterrupt, d.orcaFocus, d.orcaHide}, true
	case "herdr":
		return harness{d.herdrSend, d.herdrInterrupt, d.herdrFocus, d.herdrHide}, true
	case "cmux":
		return harness{d.cmuxSend, d.cmuxInterrupt, d.cmuxFocus, d.cmuxHide}, true
	}
	return harness{}, false
}

// inApp: an agent mirrored from an app that runs it.
func inApp(e store.Entry) bool { return e.A.App != "" }

// answerInApp checks the answer against the agent's open prompt and types it
// through the app the agent runs in.
func (d *Daemon) answerInApp(h harness, id, promptID, key string) error {
	var e store.Entry
	text := key
	err := errors.New("")
	d.Store.Update(id, func(x *store.Entry) bool {
		p := x.A.Waiting
		if x.A.Status != model.Waiting || p == nil || p.ID != promptID {
			err = server.ErrStalePrompt
			return false
		}
		for _, o := range p.Options {
			if o.Key == key {
				err, e = nil, *x
				text = claude.ScreenFormKey(x, key) // "next" on a pick-any tab is Tab
			}
		}
		if err != nil && !errors.Is(err, server.ErrStalePrompt) {
			err = server.ErrBadKey
		}
		return false
	})
	if err != nil {
		return err
	}
	if err := h.send(e, text); err != nil {
		return err
	}
	d.Store.Update(id, func(x *store.Entry) bool {
		x.SetStatus(model.Running) // until the app reports the next state
		x.A.Waiting = nil
		return true
	})
	log.Printf("answer %s (%s): %s", id, e.A.App, key)
	return nil
}

// screenPrompt builds what an agent in app (herdr, cmux) is asking from its
// screen: the dialog's question and options, and the tool call it waits on
// when the transcript already has it.
func screenPrompt(e *store.Entry, tool, screen, app string) *model.Prompt {
	if p := claude.ScreenFormPrompt(e, screen); p != nil {
		p.Context += " · in " + app // a question form: its tab on screen
		return p
	}
	title, detail := screenQuestion(screen)
	p := &model.Prompt{ID: e.NextPromptID(), Kind: "input", Title: "needs you", Context: "answer it in " + app}
	if tool != "" {
		p = claude.PromptFor(e, tool, orcaToolInput(tool, ""))
		p.Context = strings.TrimSuffix(p.Context, " · Orca") + " · in " + app
	}
	if title != "" {
		p.Title, p.Detail = strings.ToLower(clipText(title, 200)), detail
	}
	if opts := claude.ParseOptions(screen); len(opts) >= 2 {
		p.Options = opts
		if p.Kind == "input" {
			p.Kind = "question"
		}
	} else if p.Kind == "question" && len(p.Options) == 0 {
		p.Kind = "input"
	}
	return p
}

var (
	firstOptionRe = regexp.MustCompile(`^[\s│|]*(?:[❯›>]\s*)?1[.)]\s+\S`)
	ruleRe        = regexp.MustCompile(`^\s*[─━-]{8,}\s*$`)
)

// screenQuestion reads a Claude Code dialog off the screen: the lines
// between the rule it starts with and its first option. title is the last of
// them (the question), detail the ones above it.
func screenQuestion(screen string) (title, detail string) {
	lines := strings.Split(screen, "\n")
	end := -1
	for i, l := range lines {
		if firstOptionRe.MatchString(l) {
			end = i
		}
	}
	if end < 0 {
		return "", ""
	}
	start := end - 8
	for i := end - 1; i >= 0 && i >= end-12; i-- {
		if ruleRe.MatchString(lines[i]) {
			start = i + 1
			break
		}
	}
	var keep []string
	for _, l := range lines[max(start, 0):end] {
		if t := strings.TrimSpace(strings.Trim(l, " │|╭╮╰╯☐☒✔")); t != "" {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		return "", ""
	}
	return keep[len(keep)-1], clipText(strings.Join(keep[:len(keep)-1], "\n"), 2000)
}
