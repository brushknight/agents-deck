package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// accountOf is the Claude account an agent runs in: the one the deck started
// it in, else the folder its transcript is in, else Claude Code's default
// folder (where every agent ran before accounts).
func accountOf(e store.Entry) accounts.Account {
	if a, ok := accounts.Find(e.Account); ok {
		return a
	}
	if a, ok := accounts.ForTranscript(e.Transcript); ok {
		return a
	}
	return homeAccount()
}

func homeAccount() accounts.Account {
	for _, a := range accounts.List() {
		if a.IsHome() {
			return a
		}
	}
	return accounts.Account{Dir: accounts.Home()}
}

// projectsDirs is every account's transcript folder, first's first.
func projectsDirs(first accounts.Account) []string {
	out := []string{first.ProjectsDir()}
	for _, a := range accounts.List() {
		if a.Path() != first.Path() {
			out = append(out, a.ProjectsDir())
		}
	}
	return out
}

// accountForSession finds the account holding a session's transcript
// (sessions picked from history or from disk); the default account when no
// account has it.
func accountForSession(sessionID, cwd string) accounts.Account {
	for _, a := range accounts.List() {
		if _, ok := claude.FindTranscript([]string{a.ProjectsDir()}, cwd, sessionID); ok {
			return a
		}
	}
	return accounts.Default()
}

// activeSessions asks every account's Claude Code for its sessions.
func activeSessions(bin string, skip map[string]bool) ([]claude.Live, bool) {
	var all []claude.Live
	seen := map[string]bool{}
	ok := false
	for _, a := range accounts.List() {
		got, answered := claude.ActiveSessions(bin, a.ConfigDir(), projectsDirs(a), skip)
		if !answered {
			continue
		}
		ok = true
		for _, l := range got {
			if !seen[l.SessionID] {
				seen[l.SessionID] = true
				all = append(all, l)
			}
		}
	}
	return all, ok
}

// accountsKey changes whenever the account setup does, so the deck can
// redraw the agents' account dots.
func accountsKey() string {
	return fmt.Sprint(accounts.InUse(), accounts.List(), accounts.Default().Name)
}

// Handoff continues a Claude session in another account, for when one runs
// out of usage: the transcript is copied into the other account's folder and
// Claude resumes it there. An agent the deck started stops and comes back in
// the same tile; a session started by hand keeps running in its own terminal
// and the copy starts as a new agent. It returns the agent now running the
// conversation.
func (d *Daemon) Handoff(id, name string) (string, error) {
	to, ok := accounts.Find(name)
	if !ok || !accounts.InUse() {
		return "", fmt.Errorf("no account named %q", name)
	}
	e, ok := d.Store.Get(id)
	if !ok {
		return "", server.ErrNotFound
	}
	if e.A.Tool != "claude" || e.Sim || e.SessionID == "" {
		return "", errors.New("only Claude sessions can move to another account")
	}
	from := accountOf(e)
	if from.Path() == to.Path() {
		return "", fmt.Errorf("it already runs in %s", to.Name)
	}
	if e.Transcript == "" {
		return "", errors.New("this session has no transcript yet: send it a prompt first")
	}
	if _, err := os.Stat(e.A.Cwd); err != nil {
		return "", errors.New("folder no longer exists: " + e.A.Cwd)
	}
	if e.External {
		path, err := accounts.CopySession(e.Transcript, from, to)
		if err != nil {
			return "", err
		}
		newID, err := d.startResumed(e.SessionID, e.A.Cwd, e.A.Title, e.A.AITitle, e.A.LastPrompt, to, path)
		if err == nil {
			log.Printf("handoff %s (watched) to %s: new agent %s", id, to.Name, newID)
		}
		return newID, err
	}

	// Stop it first, and wait for Claude to finish writing: its last lines
	// belong in the copy, and a late hook from it mustn't touch the new run.
	if pid := tmux.PanePID(e.TmuxName); pid > 0 {
		_ = tmux.Kill(e.TmuxName)
		waitGone(pid, 10*time.Second)
	} else {
		_ = tmux.Kill(e.TmuxName)
	}
	path, err := accounts.CopySession(e.Transcript, from, to)
	if err != nil {
		return "", err
	}
	d.Store.Update(id, func(x *store.Entry) bool {
		x.Account = to.Name
		x.Transcript, x.Offset, x.Usage, x.Pending = path, 0, nil, ""
		x.SetStatus(model.Starting)
		x.Menu = false
		x.A.Lost = false
		x.A.StartedAt = time.Now().UTC() // the "still creating" grace period
		return true
	})
	if err := tmux.NewSession(e.TmuxName, e.A.Cwd, agentEnv(id), d.claudeArgv(explicitTitle(e), to, []string{"--resume", e.SessionID})); err != nil {
		return "", err
	}
	log.Printf("handoff %s from %s to %s", id, from.Name, to.Name)
	return id, nil
}

// waitGone waits until process pid has exited (or the time is up).
func waitGone(pid int, limit time.Duration) {
	for end := time.Now().Add(limit); time.Now().Before(end); {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startResumed puts a Claude session on the board as a new agent, resumed in
// account acc (transcript: where its transcript is in that account).
func (d *Daemon) startResumed(sessionID, cwd, title, aiTitle, lastPrompt string, acc accounts.Account, transcript string) (string, error) {
	if title == "" {
		title = filepath.Base(cwd)
	}
	id := store.NewID()
	e := &store.Entry{TmuxName: "a-" + id, SessionID: sessionID, Account: acc.Name, Transcript: transcript}
	e.A = model.Agent{ID: id, Title: d.uniqueTitle(title), AITitle: aiTitle, Tool: "claude", Status: model.Starting,
		Cwd: cwd, Folder: filepath.Base(cwd), LastPrompt: lastPrompt}
	d.Store.Add(e)
	if err := tmux.NewSession(e.TmuxName, cwd, agentEnv(id), d.claudeArgv("", acc, []string{"--resume", sessionID})); err != nil {
		d.Store.Remove(id)
		return "", err
	}
	return id, nil
}
