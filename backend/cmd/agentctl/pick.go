package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// target is something a command can name: an agent on the deck, or a Claude
// session on disk that is not on the deck.
type target struct {
	ID      string // agent id, or the full session id
	Name    string
	Folder  string // as shown (~ for home)
	Active  time.Time
	Agent   *model.Agent        // nil for a session
	Session *claude.SessionInfo // nil for an agent
}

// shortID is the id as lists show it (a session's first 8 characters).
func (t target) shortID() string {
	if t.Session != nil && len(t.ID) > 8 {
		return t.ID[:8]
	}
	return t.ID
}

// state says in a word or two what a target is: lost, ended, a status, or
// a Claude session.
func (t target) state() string {
	switch {
	case t.Session != nil:
		return "session"
	case t.Agent.Lost:
		return "lost"
	case t.Agent.Status == model.Exited:
		return "ended"
	}
	return string(t.Agent.Status)
}

// stateLong is state in a few words, for a sentence.
func (t target) stateLong() string {
	switch t.state() {
	case "lost":
		return "lost with the tmux server"
	case "ended":
		return "ended"
	case "session":
		return "a Claude session not on the deck"
	}
	return string(t.Agent.Status)
}

func agentTarget(a model.Agent) target {
	return target{ID: a.ID, Name: oneLine(a.Title), Folder: a.Folder, Active: a.LastActive, Agent: &a}
}

func sessionTarget(s claude.SessionInfo) target {
	return target{ID: s.ID, Name: oneLine(s.Title()), Folder: shortHome(s.Cwd), Active: s.Modified, Session: &s}
}

// Shortest id prefixes that count: agent ids are 6 characters, session ids
// are uuids (a short hex prefix would hit hundreds).
const (
	minAgentPrefix   = 2
	minSessionPrefix = 4
)

var errNoMatch = errors.New("no match")

// ambiguous is the error for a name that fits several targets.
type ambiguous struct {
	ref   string
	cands []target
}

func (e *ambiguous) Error() string { return fmt.Sprintf("%q matches %d", e.ref, len(e.cands)) }

// match finds the one target ref names, trying in order: an exact id; an id
// prefix; an exact name (any case); a part of a name (any case). It never
// guesses: several targets at the first step that finds any are an
// *ambiguous error, and so is an id prefix when a name matches another
// target too (agent ids look like words: "inf" may begin an id and a name).
// No match at all is errNoMatch.
func match(cands []target, ref string) (target, error) {
	ref = oneLine(ref)
	if ref == "" {
		return target{}, errNoMatch
	}
	low := strings.ToLower(ref)
	var exact, prefix, named, part []target
	for _, c := range cands {
		id := strings.ToLower(c.ID)
		name := strings.ToLower(c.Name)
		min := minAgentPrefix
		if c.Session != nil {
			min = minSessionPrefix
		}
		switch {
		case id == low:
			exact = append(exact, c)
		case len(low) >= min && strings.HasPrefix(id, low):
			prefix = append(prefix, c)
		}
		switch {
		case name == "":
		case name == low:
			named = append(named, c)
		case strings.Contains(name, low):
			part = append(part, c)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 { // an agent id equal to a session id can't happen, but don't guess
		return target{}, &ambiguous{ref, exact}
	}
	if len(prefix) > 0 {
		all := union(prefix, named, part)
		if len(all) == 1 {
			return all[0], nil
		}
		return target{}, &ambiguous{ref, all}
	}
	for _, step := range [][]target{named, part} {
		switch len(step) {
		case 0:
			continue
		case 1:
			return step[0], nil
		}
		return target{}, &ambiguous{ref, step}
	}
	return target{}, errNoMatch
}

// union joins target lists, each target once, in order.
func union(lists ...[]target) []target {
	seen := map[string]bool{}
	var out []target
	for _, l := range lists {
		for _, t := range l {
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// byActive sorts targets most recently active first (unknown last); ties
// keep their order.
func byActive(ts []target) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Active.After(ts[j].Active) })
}

// pickErr turns a match error into what to tell the user: the candidates as
// a short table and how to pick one, or where to look.
func pickErr(err error, ref, cmd, what string) error {
	var amb *ambiguous
	if errors.As(err, &amb) {
		var b strings.Builder
		plural := what + "s"
		if what == "agent or session" {
			plural = "agents or sessions"
		}
		fmt.Fprintf(&b, "%q matches %d %s:\n\n", amb.ref, len(amb.cands), plural)
		byActive(amb.cands)
		writeTargets(&b, amb.cands)
		fmt.Fprintf(&b, "\nname one by its id: agentctl %s %s", cmd, amb.cands[0].shortID())
		return errors.New(b.String())
	}
	if errors.Is(err, errNoMatch) {
		return fmt.Errorf("no %s matches %q: see `agentctl ls`, or look it up with `agentctl sessions <words>`", what, oneLine(ref))
	}
	return err
}

// writeTargets prints targets as a table: id, name, state, last active, folder.
func writeTargets(w interface{ Write([]byte) (int, error) }, ts []target) {
	tw := tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
	fmt.Fprintln(tw, "  ID\tNAME\tSTATE\tACTIVE\tFOLDER")
	for _, t := range ts {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", t.shortID(), clip(listName(t.Name), 40), t.state(), activeAgo(t.Active), clip(t.Folder, 32))
	}
	_ = tw.Flush()
}

// activeAgo says how long ago something was last active: "just now",
// "12m ago", "3h ago", "2d ago", or "-" when not known.
func activeAgo(t time.Time) string {
	return activeAgoAt(t, time.Now())
}

func activeAgoAt(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// boardTargets are the agents on the deck, as targets.
func boardTargets(s model.State) []target {
	out := make([]target, 0, len(s.Agents))
	for _, a := range s.Agents {
		out = append(out, agentTarget(a))
	}
	return out
}

// resolve finds the agent on the deck that ref names (see match).
func resolve(ref, cmd string) (model.Agent, error) {
	s, err := state()
	if err != nil {
		return model.Agent{}, err
	}
	t, err := match(boardTargets(s), ref)
	if err != nil {
		return model.Agent{}, pickErr(err, ref, cmd, "agent")
	}
	return *t.Agent, nil
}

// stdinIsTerminal reports whether someone can answer a question.
func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull) // a character device too, but nobody types there
	return err != nil || !os.SameFile(st, null)
}
