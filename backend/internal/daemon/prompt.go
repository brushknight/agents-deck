package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// Timings of sending a prompt, variables so tests can shorten them.
var (
	// afterInterrupt: how long Esc takes to stop a turn before the prompt is typed.
	afterInterrupt = 700 * time.Millisecond
	// beforeEnter: how long a paste takes to land before Enter submits it.
	beforeEnter = 300 * time.Millisecond
)

// Prompt types text into the agent's session and presses Enter, as if you
// typed it there: agents the deck runs in tmux and agents in an app that
// runs them (herdr, cmux, Orca). A session started by hand in another
// terminal can't be typed into. With interrupt, a working agent (or one
// waiting on a question) gets Esc first.
func (d *Daemon) Prompt(id, text string, interrupt bool) error {
	return d.stampActive(id, d.prompt(id, text, interrupt, false))
}

// Type types the prompt in now, also while the agent works: the tool's own
// queue takes it, as when you type ahead in its chat. An agent asking a
// question refuses it (the text would answer the question).
func (d *Daemon) Type(id, text string) error {
	return d.stampActive(id, d.prompt(id, text, false, true))
}

func (d *Daemon) prompt(id, text string, interrupt, ahead bool) error {
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	h, inHarness := d.harnessOf(e)
	if !inHarness && e.External {
		return readOnlyErr(e)
	}
	// Claude started here (hooks) and agents in an app (the app says) have a
	// status to trust; other tools started here show "running" until they exit.
	known := e.A.Tool == "claude" || inHarness
	stop := false
	switch {
	case e.A.Status == model.Exited:
		return server.ErrExited
	case !known:
		stop = interrupt
	case e.A.Status == model.Starting:
		return server.ErrStarting
	case e.A.Status == model.Waiting:
		if !interrupt {
			return server.ErrAsking
		}
		stop = true
	case e.A.Status == model.Running:
		if ahead {
			break
		}
		if !interrupt {
			return server.ErrBusy
		}
		stop = true
	}
	if stop {
		if err := d.interrupt(id); err != nil {
			return err
		}
		time.Sleep(afterInterrupt)
	}
	var err error
	switch {
	case inHarness:
		err = h.prompt(e, text)
	case e.TmuxName == "":
		// Never let tmux pick a session: an empty target is its "current" one.
		return fmt.Errorf("%w: the deck has no terminal for this agent", server.ErrReadOnly)
	default:
		err = tmuxPrompt(e.TmuxName, text)
	}
	if err != nil {
		return err
	}
	log.Printf("prompt %s: %d bytes typed", id, len(text))
	return nil
}

// WaitReady waits until an agent the deck just started can take a prompt:
// its screen has drawn and stayed still for a second, and a Claude agent
// has started its session (idle). It returns server.ErrAsking when the
// agent asks something first (trusting the folder), server.ErrExited when
// it quit.
func (d *Daemon) WaitReady(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last, still := "", time.Now()
	for {
		e, ok := d.Store.Get(id)
		if !ok {
			return server.ErrNotFound
		}
		switch e.A.Status {
		case model.Exited:
			return server.ErrExited
		case model.Waiting:
			return server.ErrAsking
		}
		screen := tmux.Capture(e.TmuxName, 0)
		if screen != last {
			last, still = screen, time.Now()
		}
		ready := strings.TrimSpace(screen) != "" && time.Since(still) >= time.Second
		if e.A.Tool == "claude" {
			ready = ready && e.A.Status == model.Idle
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the agent wasn't ready for a prompt after %s: look at it with get_agent, then use send_prompt", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// tmuxPrompt pastes the text into the session, then presses Enter.
func tmuxPrompt(name, text string) error {
	if err := tmux.Paste(name, text); err != nil {
		return err
	}
	time.Sleep(beforeEnter)
	return tmux.SendKeys(name, "", "Enter")
}

// bracketed wraps multi-line text in a bracketed paste, so an app that takes
// raw text sees its newlines as part of it rather than as Enter.
func bracketed(text string) string {
	if !strings.Contains(text, "\n") {
		return text
	}
	return "\x1b[200~" + text + "\x1b[201~"
}

// readOnlyErr says why the deck can't type into an external session.
func readOnlyErr(e store.Entry) error {
	if e.A.Tool == "claude" {
		return fmt.Errorf("%w: this session runs in another terminal, and nothing can type into another app's terminal; start agents from the deck (agentctl new, or start_agent over MCP) to give them tasks", server.ErrReadOnly)
	}
	return fmt.Errorf("%w: this is a Codex app session; give it tasks in the Codex app", server.ErrReadOnly)
}

// Typing a prompt through each app: the text, then Enter on its own.

func (d *Daemon) herdrPrompt(e store.Entry, text string) error {
	c := d.herdrClient(e.HerdrSocket)
	if err := c.SendText(e.HerdrPane, bracketed(text)); err != nil {
		return err
	}
	time.Sleep(beforeEnter)
	return c.SendKeys(e.HerdrPane, "enter")
}

func (d *Daemon) cmuxPrompt(e store.Entry, text string) error {
	c := d.cmuxClient()
	if err := d.cmuxErr(c.SendText(e.CmuxSurface, bracketed(text))); err != nil {
		return err
	}
	time.Sleep(beforeEnter)
	return d.cmuxErr(c.SendKey(e.CmuxSurface, "enter"))
}

func (d *Daemon) orcaPrompt(e store.Entry, text string) error {
	c := d.orcaClient()
	if err := c.Send(e.OrcaTerminal, bracketed(text), false); err != nil {
		return err
	}
	time.Sleep(beforeEnter)
	return c.Send(e.OrcaTerminal, "\r", false)
}
