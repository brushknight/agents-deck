package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/hats"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

// Project is a folder you start agents in, with what they start with: a
// tool, a Claude account and a hat (each optional). Settings, Agents keeps
// the list; Home starts an agent in one.
type Project struct {
	Name    string      `json:"name"`
	Folder  string      `json:"folder"`            // a full path ("~/" allowed, kept as written)
	Tool    string      `json:"tool,omitempty"`    // claude | codex | shell ("" = claude)
	Account string      `json:"account,omitempty"` // a Claude account's name ("" = the default)
	Hat     *ProjectHat `json:"hat,omitempty"`     // nil: the folder's own hat
}

// ProjectHat is a project's hat: a shape and a colour from the deck's set.
type ProjectHat struct {
	Shape string `json:"shape"`
	Color string `json:"color"`
}

// ProjectTools are the tools a project can start.
var ProjectTools = []string{"claude", "codex", "shell"}

// MaxProjects caps the list; a project's name is at most 40 characters.
const (
	MaxProjects    = 200
	maxProjectName = 40
)

// Starter starts agents (the live daemon); nil in demo mode and in tests
// that don't start anything.
type Starter interface {
	server.Starter
	server.Prompter
}

// projectStore guards projects.json in the data folder.
type projectStore struct{ mu sync.Mutex }

func (a *Admin) projectsPath() string { return filepath.Join(a.DataDir, "projects.json") }

// Projects is the stored list, in the order it was made.
func (a *Admin) Projects() []Project {
	a.projects.mu.Lock()
	defer a.projects.mu.Unlock()
	return a.readProjects()
}

func (a *Admin) readProjects() []Project {
	var file struct {
		Projects []Project `json:"projects"`
	}
	b, err := os.ReadFile(a.projectsPath())
	if err != nil {
		return []Project{}
	}
	if err := json.Unmarshal(b, &file); err != nil {
		log.Printf("projects.json: %v", err)
		return []Project{}
	}
	if file.Projects == nil {
		return []Project{}
	}
	return file.Projects
}

func (a *Admin) writeProjects(list []Project) error {
	b, _ := json.MarshalIndent(map[string]any{"projects": list}, "", "  ")
	if err := os.MkdirAll(a.DataDir, 0o700); err != nil {
		return err
	}
	tmp := a.projectsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.projectsPath())
}

// expandHome turns a leading ~/ into the home folder.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// cleanProject checks a project and tidies it: names are trimmed, an
// empty hat is none.
func cleanProject(p Project) (Project, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.Folder = strings.TrimSpace(p.Folder)
	switch {
	case p.Name == "":
		return p, errors.New("give the project a name")
	case utf8.RuneCountInString(p.Name) > maxProjectName:
		return p, fmt.Errorf("a project's name is at most %d characters", maxProjectName)
	case strings.ContainsAny(p.Name, "/\\\n\r\t"):
		return p, errors.New("a project's name can't have a slash or a line break")
	case p.Folder == "":
		return p, errors.New("give the project's folder")
	}
	dir := expandHome(p.Folder)
	if !filepath.IsAbs(dir) {
		return p, fmt.Errorf("give the folder as a full path (or ~/…): %q", p.Folder)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return p, fmt.Errorf("there is no folder %s on this computer", p.Folder)
	}
	if p.Tool != "" && !oneOf(p.Tool, ProjectTools...) {
		return p, fmt.Errorf("tool must be one of %s", strings.Join(ProjectTools, ", "))
	}
	if p.Account != "" {
		if _, ok := accounts.Find(p.Account); !ok {
			return p, fmt.Errorf("no Claude account called %s", p.Account)
		}
		if p.Tool != "" && p.Tool != "claude" {
			return p, errors.New("an account is for Claude Code: pick claude as the tool, or no account")
		}
	}
	if p.Hat != nil && p.Hat.Shape == "" && p.Hat.Color == "" {
		p.Hat = nil
	}
	if p.Hat != nil {
		if err := hats.Validate(p.Hat.Shape, p.Hat.Color); err != nil {
			return p, err
		}
	}
	return p, nil
}

func findProject(list []Project, name string) int {
	for i, p := range list {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

// projectsList is GET /admin/v1/projects.
func (a *Admin) projectsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": a.Projects(), "tools": ProjectTools, "canStart": a.Starter != nil})
}

// projectsAdd is POST /admin/v1/projects {"name", "folder", "tool", "account", "hat"}.
func (a *Admin) projectsAdd(w http.ResponseWriter, r *http.Request) {
	var p Project
	if err := readBody(r, 4096, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := cleanProject(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.projects.mu.Lock()
	defer a.projects.mu.Unlock()
	list := a.readProjects()
	if findProject(list, p.Name) >= 0 {
		writeErr(w, http.StatusConflict, "there is a project called "+p.Name+" already")
		return
	}
	if len(list) >= MaxProjects {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("at most %d projects", MaxProjects))
		return
	}
	list = append(list, p)
	if err := a.writeProjects(list); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// projectAction is POST /admin/v1/projects/{name}/{action}: update (the
// whole project, its name too), remove, or start {"prompt", "tool"}.
func (a *Admin) projectAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch r.PathValue("action") {
	case "update":
		var p Project
		if err := readBody(r, 4096, &p); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		p, err := cleanProject(p)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		a.projects.mu.Lock()
		defer a.projects.mu.Unlock()
		list := a.readProjects()
		i := findProject(list, name)
		if i < 0 {
			writeErr(w, http.StatusNotFound, "no project called "+name)
			return
		}
		if j := findProject(list, p.Name); j >= 0 && j != i {
			writeErr(w, http.StatusConflict, "there is a project called "+p.Name+" already")
			return
		}
		list[i] = p
		if err := a.writeProjects(list); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	case "remove":
		a.projects.mu.Lock()
		defer a.projects.mu.Unlock()
		list := a.readProjects()
		i := findProject(list, name)
		if i < 0 {
			writeErr(w, http.StatusNotFound, "no project called "+name)
			return
		}
		if err := a.writeProjects(append(list[:i], list[i+1:]...)); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "start":
		a.projectStart(w, r, name)
	default:
		writeErr(w, http.StatusNotFound, "unknown action: update, remove or start")
	}
}

// startWait is how long a new agent has to get ready for its first prompt.
var startWait = 60 * time.Second

// projectStart starts an agent in the project's folder, with its tool and
// account (a tool in the body picks another for this agent), and sends the
// first prompt once it is ready.
func (a *Admin) projectStart(w http.ResponseWriter, r *http.Request, name string) {
	var b struct {
		Prompt string `json:"prompt"`
		Tool   string `json:"tool"`
	}
	if err := readBody(r, 16<<10, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	i := -1
	list := a.Projects()
	if i = findProject(list, name); i < 0 {
		writeErr(w, http.StatusNotFound, "no project called "+name)
		return
	}
	p := list[i]
	if a.Starter == nil {
		writeErr(w, http.StatusServiceUnavailable, "this service can't start agents (demo mode)")
		return
	}
	tool := p.Tool
	if b.Tool != "" {
		if !oneOf(b.Tool, ProjectTools...) {
			writeErr(w, http.StatusBadRequest, "tool must be one of "+strings.Join(ProjectTools, ", "))
			return
		}
		tool = b.Tool
	}
	if tool == "" {
		tool = "claude"
	}
	prompt := ""
	if strings.TrimSpace(b.Prompt) != "" {
		var err error
		if prompt, err = server.CleanPrompt(b.Prompt); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	account := p.Account
	if tool != "claude" {
		account = ""
	}
	id, err := a.Starter.Create(server.CreateOpts{Dir: expandHome(p.Folder), Title: p.Name, Tool: tool, Account: account})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if prompt != "" {
		go func(st Starter) {
			if err := st.WaitReady(id, startWait); err != nil {
				log.Printf("project %s: agent %s got no first prompt: %v", p.Name, id, err)
				return
			}
			if err := st.Prompt(id, prompt, false); err != nil {
				log.Printf("project %s: agent %s: first prompt: %v", p.Name, id, err)
			}
		}(a.Starter)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "project": p.Name, "tool": tool, "prompted": prompt != ""})
}
