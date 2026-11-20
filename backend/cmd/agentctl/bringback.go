package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// resume and restore are one command: bring an agent back and open it.
// Named agents or sessions come back whatever happened to them: lost with
// the tmux server (restored), ended (resumed), a Claude session not on the
// deck (started as an agent), or still running (just opened). Without a
// name, restore brings back everything lost with the tmux server and resume
// lists recent sessions.

// handful is how many lost agents restore brings back without asking.
const handful = 5

// Timeouts for requests that start agents: a restore runs a tool in tmux
// (and waits a little between agents), a resume starts Claude.
const (
	restoreTimeout = time.Minute
	resumeTimeout  = 30 * time.Second
)

type bringOpts struct {
	detached bool // don't open it
	force    bool // resume a session even if it looks open elsewhere
}

// bringBack is `agentctl resume|restore [<agent or session>...]`.
func bringBack(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	detached := fs.Bool("d", false, "don't open it")
	force := fs.Bool("force", false, "resume a session even if it looks open elsewhere")
	all := fs.Bool("all", false, "restore every lost agent without asking")
	yes := fs.Bool("y", false, "don't ask")
	if err := fs.Parse(reorder(args, nil)); err != nil {
		return err
	}
	opts := bringOpts{detached: *detached, force: *force}
	if fs.NArg() == 0 {
		if cmd == "restore" {
			return restoreLost(*all || *yes)
		}
		return sessionsCmd([]string{"-n", "15"})
	}
	cands, err := bringCandidates(true)
	if err != nil {
		return err
	}
	var picked []target
	for _, ref := range fs.Args() {
		t, err := match(cands, ref)
		if err != nil {
			return pickErr(err, ref, cmd, "agent or session")
		}
		picked = union(picked, []target{t})
	}
	return bringBackTargets(picked, opts)
}

// bringCandidates is what resume and restore can name: lost and ended
// agents first, then running ones, then Claude sessions not on the deck
// (withSessions), each group most recently active first.
func bringCandidates(withSessions bool) ([]target, error) {
	s, err := state()
	if err != nil {
		return nil, err
	}
	var gone, live []target
	for _, a := range s.Agents {
		if a.Lost || a.Status == model.Exited {
			gone = append(gone, agentTarget(a))
		} else {
			live = append(live, agentTarget(a))
		}
	}
	byActive(gone)
	byActive(live)
	out := append(gone, live...)
	if !withSessions {
		return out, nil
	}
	all, _, err := allSessions()
	if err != nil {
		return out, nil // no sessions to offer is not an error here
	}
	board := boardSessions()
	for _, ss := range all { // newest first already
		if _, ok := board[ss.ID]; !ok {
			out = append(out, sessionTarget(ss))
		}
	}
	return out, nil
}

// action is what bringing a target back takes.
type action int

const (
	actRestore   action = iota // lost with the tmux server
	actResume                  // an ended agent
	actStart                   // a Claude session not on the deck
	actOpen                    // already running
	actElsewhere               // running in another app: nothing to do here
	actCant                    // ended, and only Claude sessions come back
)

func actionFor(t target) action {
	switch {
	case t.Session != nil:
		return actStart
	case t.Agent.Lost:
		return actRestore
	case t.Agent.Status == model.Exited && t.Agent.Resumable:
		return actResume
	case t.Agent.Status == model.Exited:
		return actCant
	case t.Agent.External:
		return actElsewhere
	}
	return actOpen
}

// bringBackTargets brings each target back and opens it when it is the
// only one (and not detached); with several it prints their ids.
func bringBackTargets(ts []target, opts bringOpts) error {
	var opened []string
	var firstErr error
	for i, t := range ts {
		if i > 0 && actionFor(t) == actRestore {
			time.Sleep(300 * time.Millisecond) // don't start a dozen agents in the same instant
		}
		id, msg, err := bringOne(t, opts.force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", listName(t.Name), err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if actionFor(t) == actOpen && len(ts) == 1 && !opts.detached {
			msg = "already running: attaching"
		}
		fmt.Printf("%-10s %s  %s\n", msg, id, listName(t.Name))
		if actionFor(t) != actElsewhere {
			opened = append(opened, id)
		}
	}
	if firstErr != nil && len(opened) == 0 {
		return errors.New("nothing was brought back")
	}
	if len(ts) != 1 || opts.detached || len(opened) != 1 {
		return firstErr
	}
	a, err := resolve(opened[0], "attach")
	if err != nil {
		return err
	}
	return attachTmux(a)
}

// bringOne brings one target back; it returns the agent's id and what was
// done, in a word.
func bringOne(t target, force bool) (string, string, error) {
	switch actionFor(t) {
	case actRestore:
		if err := restoreIDs([]string{t.ID}, nil); err != nil {
			return "", "", err
		}
		return t.ID, "restored", nil
	case actResume:
		if err := callTimeout("POST", "/v1/agents/"+t.ID+"/resume", nil, nil, resumeTimeout); err != nil {
			return "", "", err
		}
		return t.ID, "resumed", nil
	case actStart:
		id, err := startSession(t, force)
		return id, "resumed", err
	case actElsewhere:
		app := t.Agent.App
		if app == "" {
			app = "its own app"
		}
		return t.ID, "running in " + app, nil
	case actCant:
		return "", "", errors.New("it has ended and only Claude sessions can be resumed: start it again with agentctl new")
	}
	return t.ID, "running", nil
}

// startSession puts a Claude session that is not on the deck back on it.
func startSession(t target, force bool) (string, error) {
	sess := t.Session
	if time.Since(sess.Modified) < liveWindow && !force {
		return "", fmt.Errorf("session %s was written %s ago: it may still be open in another terminal (use --force to resume anyway)", sess.ID[:8], ago(sess.Modified))
	}
	if _, err := os.Stat(sess.Cwd); err != nil {
		return "", fmt.Errorf("the session's folder is gone: %s", sess.Cwd)
	}
	var res struct{ ID string }
	if err := callTimeout("POST", "/local/resume", map[string]string{"sessionId": sess.ID, "cwd": sess.Cwd}, &res, resumeTimeout); err != nil {
		return "", err
	}
	return res.ID, nil
}

type restoreResult struct {
	ID, Title, Action, Error string
}

// restoreIDs restores the listed lost agents, one request each so a long
// list reports as it goes and no single request runs long; each result is
// passed to report (nil: none).
func restoreIDs(ids []string, report func(restoreResult)) error {
	var firstErr error
	for _, id := range ids {
		var res struct{ Results []restoreResult }
		err := callTimeout("POST", "/v1/restore", map[string][]string{"ids": {id}}, &res, restoreTimeout)
		if err == nil && len(res.Results) == 0 {
			err = errors.New("the service restored nothing")
		}
		if err == nil && res.Results[0].Error != "" {
			err = errors.New(res.Results[0].Error)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if report != nil {
				report(restoreResult{ID: id, Action: "failed", Error: err.Error()})
			}
			continue
		}
		if report != nil {
			report(res.Results[0])
		}
	}
	return firstErr
}

// restoreLost is `agentctl restore` with no names: list what was lost with
// the tmux server, and bring it all back (asking first when it is more than
// a handful, unless yes).
func restoreLost(yes bool) error {
	s, err := state()
	if err != nil {
		return err
	}
	var lost []target
	for _, a := range s.Agents {
		if a.Lost {
			lost = append(lost, agentTarget(a))
		}
	}
	if len(lost) == 0 {
		fmt.Println("nothing to restore: no agents were lost with the tmux server")
		return nil
	}
	byActive(lost)
	fmt.Printf("%d agent(s) were lost with the tmux server:\n\n", len(lost))
	writeTargets(os.Stdout, lost)
	fmt.Println()
	if len(lost) > handful && !yes {
		if !stdinIsTerminal() {
			return fmt.Errorf("restore all %d? pass --all, or name the ones to bring back: agentctl restore <agent>...", len(lost))
		}
		if !confirm(fmt.Sprintf("Restore all %d? [y/N] ", len(lost)), false) {
			fmt.Println("nothing restored · name the ones you want: agentctl restore <agent>...")
			return nil
		}
	}
	ids := make([]string, len(lost))
	names := map[string]string{}
	for i, t := range lost {
		ids[i], names[t.ID] = t.ID, t.Name
	}
	back := 0
	_ = restoreIDs(ids, func(r restoreResult) {
		line := fmt.Sprintf("%-10s %s  %s", r.Action, r.ID, listName(names[r.ID]))
		if r.Error != "" {
			line += "  (" + r.Error + ")"
		} else {
			back++
		}
		fmt.Println(line)
	})
	if back > 0 {
		fmt.Printf("%d agent(s) back · `agentctl reopen` opens their terminals\n", back)
	}
	return nil
}

// confirm asks a yes/no question on the terminal; an empty answer is def.
func confirm(q string, def bool) bool {
	fmt.Print(q)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}
