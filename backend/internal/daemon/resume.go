package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/paths"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// Session is a finished Claude session that can be resumed later.
type Session struct {
	SessionID  string    `json:"sessionId"`
	Title      string    `json:"title"`
	AITitle    string    `json:"aiTitle"`
	Cwd        string    `json:"cwd"`
	LastPrompt string    `json:"lastPrompt"`
	CostUSD    float64   `json:"costUsd"`
	EndedAt    time.Time `json:"endedAt"`
}

const historyMax = 30

var historyMu sync.Mutex

func historyPath() string { return filepath.Join(paths.Data(), "history.json") }

// HistoryJSON is History for the unix-socket API.
func (d *Daemon) HistoryJSON() any { return d.History() }

// History returns recent resumable sessions, newest first.
func (d *Daemon) History() []Session {
	historyMu.Lock()
	defer historyMu.Unlock()
	return readHistory()
}

func readHistory() []Session {
	var h []Session
	if b, err := os.ReadFile(historyPath()); err == nil {
		_ = json.Unmarshal(b, &h)
	}
	return h
}

// remember records a Claude agent's session when it leaves the board.
func remember(e store.Entry) {
	if e.Sim || e.A.Tool != "claude" || e.SessionID == "" || e.A.Turns == 0 && e.A.LastPrompt == "" {
		return // nothing worth continuing
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	h := readHistory()
	out := []Session{{SessionID: e.SessionID, Title: e.A.Title, AITitle: e.A.AITitle, Cwd: e.A.Cwd,
		LastPrompt: e.A.LastPrompt, CostUSD: e.A.CostUSD, EndedAt: time.Now().UTC()}}
	for _, s := range h {
		if s.SessionID != e.SessionID {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].EndedAt.After(out[j].EndedAt) })
	if len(out) > historyMax {
		out = out[:historyMax]
	}
	if b, err := json.MarshalIndent(out, "", " "); err == nil {
		tmp := historyPath() + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, historyPath())
		}
	}
}

func forget(sessionID string) {
	historyMu.Lock()
	defer historyMu.Unlock()
	h := readHistory()
	out := h[:0]
	for _, s := range h {
		if s.SessionID != sessionID {
			out = append(out, s)
		}
	}
	if b, err := json.MarshalIndent(out, "", " "); err == nil {
		_ = os.WriteFile(historyPath(), b, 0o600)
	}
}

func explicitTitle(e store.Entry) string {
	if e.TitleSet {
		return e.A.Title
	}
	return ""
}

// claudeArgv is the login-shell command that runs Claude with our hooks.
// title is passed as Claude's session name only when the user chose it (-t);
// otherwise Claude's own title shows through.
func (d *Daemon) claudeArgv(title string, session []string) []string {
	inner := append(append([]string{"claude"}, session...), "--settings", paths.HooksFile())
	if title != "" {
		inner = append(inner, "-n", title)
	}
	inner = append(inner, d.Cfg.ClaudeArgs...)
	return []string{"/bin/zsh", "-lc", "exec " + shellJoin(inner)}
}

func agentEnv(id string) []string {
	return []string{"AGENTSTERM_ID=" + id, "AGENTSTERM_SOCK=" + paths.Socket()}
}

// Resume restarts an exited Claude agent on its previous conversation, in the
// same slot.
func (d *Daemon) Resume(id string) error {
	e, ok := d.Store.Get(id)
	if !ok {
		return server.ErrNotFound
	}
	if e.External {
		return externalErr(e)
	}
	if e.A.Status != model.Exited {
		return errors.New("agent is still running")
	}
	if e.A.Tool != "claude" || e.SessionID == "" || e.Sim {
		return errors.New("only Claude sessions can be resumed")
	}
	if _, err := os.Stat(e.A.Cwd); err != nil {
		return errors.New("folder no longer exists: " + e.A.Cwd)
	}
	if err := tmux.NewSession(e.TmuxName, e.A.Cwd, agentEnv(id), d.claudeArgv(explicitTitle(e), []string{"--resume", e.SessionID})); err != nil {
		return err
	}
	d.Store.Update(id, func(e *store.Entry) bool {
		e.SetStatus(model.Starting)
		e.Menu = false
		e.A.Lost = false
		e.A.StartedAt = time.Now().UTC() // restarts the "still creating" grace period
		return true
	})
	return nil
}

// ResumeSession brings a Claude session back onto the board as a new agent:
// one from our history, or any session on disk when cwd is given.
func (d *Daemon) ResumeSession(sessionID, cwd, title string) (string, error) {
	if !claude.IsSessionID(sessionID) {
		return "", errors.New("not a Claude session id")
	}
	if id := d.Store.FindBySession(sessionID); id != "" {
		if e, _ := d.Store.Get(id); e.A.Status == model.Exited {
			return id, d.Resume(id)
		}
		return id, nil // already on the board and alive
	}
	var s *Session
	for _, h := range d.History() {
		if h.SessionID == sessionID {
			s = &h
			break
		}
	}
	if s == nil {
		if cwd == "" {
			return "", errors.New("unknown session")
		}
		if title == "" {
			title = filepath.Base(cwd)
		}
		s = &Session{SessionID: sessionID, Title: title, Cwd: cwd}
	}
	if _, err := os.Stat(s.Cwd); err != nil {
		return "", errors.New("folder no longer exists: " + s.Cwd)
	}
	id := store.NewID()
	e := &store.Entry{TmuxName: "a-" + id, SessionID: s.SessionID}
	e.A = model.Agent{ID: id, Title: d.uniqueTitle(s.Title), AITitle: s.AITitle, Tool: "claude", Status: model.Starting,
		Cwd: s.Cwd, Folder: filepath.Base(s.Cwd), LastPrompt: s.LastPrompt}
	d.Store.Add(e)
	if err := tmux.NewSession(e.TmuxName, s.Cwd, agentEnv(id), d.claudeArgv("", []string{"--resume", s.SessionID})); err != nil {
		d.Store.Remove(id)
		return "", err
	}
	forget(sessionID)
	return id, nil
}
