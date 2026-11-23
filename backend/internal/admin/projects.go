package admin

import (
	"crypto/rand"
	"encoding/hex"
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

// Project is a piece of work you start agents in: one or more folders (an
// app, its web front end and its server, say), with what agents start with:
// a tool, a Claude account and a hat (each optional). Settings, Agents
// keeps the list; Home starts an agent in one of its folders.
type Project struct {
	// ID stays the same when the project is renamed or its folders change,
	// for links to it from elsewhere.
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Folders []string    `json:"folders"`           // full paths ("~/" allowed, kept as written); agents start in the first by default
	Folder  string      `json:"folder"`            // the first of Folders (what older versions read and write)
	Tool    string      `json:"tool,omitempty"`    // claude | codex | shell ("" = claude)
	Account string      `json:"account,omitempty"` // a Claude account's name ("" = the default)
	Hat     *ProjectHat `json:"hat,omitempty"`     // nil: the folder's own hat
	// Path and Paths are Folder and Folders in full (~/ expanded), in
	// answers only; never stored.
	Path  string   `json:"path,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// ProjectHat is a project's hat: a shape and a colour from the deck's set.
type ProjectHat struct {
	Shape string `json:"shape"`
	Color string `json:"color"`
}

// ProjectTools are the tools a project can start.
var ProjectTools = []string{"claude", "codex", "shell"}

// MaxProjects caps the list; a project's name is at most 40 characters and
// it has at most 20 folders.
const (
	MaxProjects       = 200
	maxProjectName    = 40
	maxProjectFolders = 20
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
	return withPaths(a.readProjects())
}

// withPaths fills in each project's full paths, for answers.
func withPaths(list []Project) []Project {
	for i := range list {
		list[i].Path = expandHome(list[i].Folder)
		list[i].Paths = make([]string, len(list[i].Folders))
		for j, f := range list[i].Folders {
			list[i].Paths[j] = expandHome(f)
		}
	}
	return list
}

// newProjectID is "p" and 8 random hex digits.
func newProjectID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "p" + hex.EncodeToString(b)
}

// readProjects reads projects.json and builds the index agents are matched
// against from it (projectmatch.go).
func (a *Admin) readProjects() []Project {
	list := a.loadProjects()
	a.reindex(list)
	return list
}

func (a *Admin) loadProjects() []Project {
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
	// Projects kept by an older version have one folder and no id: given
	// them here, and kept so the id stays the same.
	changed := false
	for i := range file.Projects {
		p := &file.Projects[i]
		if len(p.Folders) == 0 && p.Folder != "" {
			p.Folders, changed = []string{p.Folder}, true
		}
		if p.ID == "" {
			p.ID, changed = newProjectID(), true
		}
	}
	if changed {
		if err := a.writeProjects(file.Projects); err != nil {
			log.Printf("projects.json: %v", err)
		}
	}
	return file.Projects
}

func (a *Admin) writeProjects(list []Project) error {
	for i := range list {
		list[i].Path, list[i].Paths = "", nil
	}
	b, _ := json.MarshalIndent(map[string]any{"projects": list}, "", "  ")
	if err := os.MkdirAll(a.DataDir, 0o700); err != nil {
		return err
	}
	tmp := a.projectsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, a.projectsPath()); err != nil {
		return err
	}
	a.reindex(list)
	return nil
}

// folderTaken refuses a folder another project lists already: a folder
// belongs to one project. A folder inside another project's folder is fine
// (the deepest wins).
func folderTaken(list []Project, self string, p Project) error {
	i, f := folderOwner(list, self, p.Folders)
	if i < 0 {
		return nil
	}
	return projectErr(http.StatusConflict, f+" is a folder of "+list[i].Name+" already: a folder belongs to one project")
}

// ProjectError is a project change refused, with the HTTP status it answers
// with (400 bad input, 404 no such project, 409 a clash, 503 can't start).
type ProjectError struct {
	Code int
	Msg  string
}

func (e *ProjectError) Error() string { return e.Msg }

func projectErr(code int, msg string) error { return &ProjectError{Code: code, Msg: msg} }

// writeProjectErr answers err with its status (500 for anything else).
func writeProjectErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var pe *ProjectError
	if errors.As(err, &pe) {
		code = pe.Code
	}
	writeErr(w, code, err.Error())
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

// cleanProject checks a project and tidies it: names and folders are
// trimmed, empty and repeated folders dropped, an empty hat is none. A body
// with only "folder" (an older client) is a project with that one folder.
func cleanProject(p Project) (Project, error) {
	p.Name = strings.TrimSpace(p.Name)
	if len(p.Folders) == 0 && strings.TrimSpace(p.Folder) != "" {
		p.Folders = []string{p.Folder}
	}
	var folders []string
	seen := map[string]bool{}
	for _, f := range p.Folders {
		f = strings.TrimSpace(f)
		dir := filepath.Clean(expandHome(f))
		if f == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		folders = append(folders, f)
	}
	p.Folders = folders
	switch {
	case p.Name == "":
		return p, errors.New("give the project a name")
	case utf8.RuneCountInString(p.Name) > maxProjectName:
		return p, fmt.Errorf("a project's name is at most %d characters", maxProjectName)
	case strings.ContainsAny(p.Name, "/\\\n\r\t"):
		return p, errors.New("a project's name can't have a slash or a line break")
	case len(p.Folders) == 0:
		return p, errors.New("give the project's folder")
	case len(p.Folders) > maxProjectFolders:
		return p, fmt.Errorf("a project has at most %d folders", maxProjectFolders)
	}
	for _, f := range p.Folders {
		dir := expandHome(f)
		if !filepath.IsAbs(dir) {
			return p, fmt.Errorf("give the folder as a full path (or ~/…): %q", f)
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return p, fmt.Errorf("there is no folder %s on this computer", f)
		}
	}
	p.Folder = p.Folders[0]
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

// findProject finds a project by its name (without case) or its id.
func findProject(list []Project, name string) int {
	for i, p := range list {
		if strings.EqualFold(p.Name, name) || (p.ID != "" && p.ID == name) {
			return i
		}
	}
	return -1
}

// AddProject checks p, gives it an id and adds it to the list.
func (a *Admin) AddProject(p Project) (Project, error) {
	p, err := cleanProject(p)
	if err != nil {
		return p, projectErr(http.StatusBadRequest, err.Error())
	}
	a.projects.mu.Lock()
	defer a.projects.mu.Unlock()
	list := a.readProjects()
	if findProject(list, p.Name) >= 0 {
		return p, projectErr(http.StatusConflict, "there is a project called "+p.Name+" already")
	}
	if err := folderTaken(list, "", p); err != nil {
		return p, err
	}
	if len(list) >= MaxProjects {
		return p, projectErr(http.StatusBadRequest, fmt.Sprintf("at most %d projects", MaxProjects))
	}
	p.ID = newProjectID()
	if err := a.writeProjects(append(list, p)); err != nil {
		return p, err
	}
	return withPaths([]Project{p})[0], nil
}

// EditProject changes the project called name (or with that id): edit gets
// a copy to change, and the result is checked as a new project would be,
// its name and folders against the others. The id stays.
func (a *Admin) EditProject(name string, edit func(p *Project) error) (Project, error) {
	a.projects.mu.Lock()
	defer a.projects.mu.Unlock()
	list := a.readProjects()
	i := findProject(list, name)
	if i < 0 {
		return Project{}, projectErr(http.StatusNotFound, "no project called "+name)
	}
	p := list[i]
	p.Folders = append([]string(nil), p.Folders...)
	if err := edit(&p); err != nil {
		return p, err
	}
	p, err := cleanProject(p)
	if err != nil {
		return p, projectErr(http.StatusBadRequest, err.Error())
	}
	if j := findProject(list, p.Name); j >= 0 && j != i {
		return p, projectErr(http.StatusConflict, "there is a project called "+p.Name+" already")
	}
	if err := folderTaken(list, list[i].ID, p); err != nil {
		return p, err
	}
	p.ID = list[i].ID
	list[i] = p
	if err := a.writeProjects(list); err != nil {
		return p, err
	}
	return withPaths([]Project{p})[0], nil
}

// RemoveProject takes the project off the list. Its folders and agents
// stay as they are.
func (a *Admin) RemoveProject(name string) error {
	a.projects.mu.Lock()
	defer a.projects.mu.Unlock()
	list := a.readProjects()
	i := findProject(list, name)
	if i < 0 {
		return projectErr(http.StatusNotFound, "no project called "+name)
	}
	return a.writeProjects(append(list[:i], list[i+1:]...))
}

// projectsList is GET /admin/v1/projects.
func (a *Admin) projectsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": a.Projects(), "tools": ProjectTools, "canStart": a.Starter != nil})
}

// projectsAdd is POST /admin/v1/projects {"name", "folders", "tool", "account", "hat"}.
func (a *Admin) projectsAdd(w http.ResponseWriter, r *http.Request) {
	var p Project
	if err := readBody(r, 4096, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.AddProject(p)
	if err != nil {
		writeProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// projectAction is POST /admin/v1/projects/{name}/{action}: update (the
// whole project, its name too), remove, or start {"prompt", "tool",
// "account", "folder", "title"}. {name} is the project's name or its id.
func (a *Admin) projectAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch r.PathValue("action") {
	case "update":
		var body Project
		if err := readBody(r, 4096, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		p, err := a.EditProject(name, func(p *Project) error { *p = body; return nil })
		if err != nil {
			writeProjectErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	case "remove":
		if err := a.RemoveProject(name); err != nil {
			writeProjectErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "start":
		var b ProjectStart
		if err := readBody(r, 16<<10, &b); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		started, err := a.StartInProject(name, b)
		if err != nil {
			writeProjectErr(w, err)
			return
		}
		if started.Prompt != "" {
			go a.firstPrompt(started)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"id": started.ID, "project": started.Project,
			"folder": started.Folder, "tool": started.Tool, "prompted": started.Prompt != ""})
	default:
		writeErr(w, http.StatusNotFound, "unknown action: update, remove or start")
	}
}

// startWait is how long a new agent has to get ready for its first prompt.
var startWait = 60 * time.Second

// ProjectStart is what to start in a project: each field is optional.
type ProjectStart struct {
	Prompt  string `json:"prompt"`  // its first task
	Tool    string `json:"tool"`    // instead of the project's
	Account string `json:"account"` // instead of the project's
	Folder  string `json:"folder"`  // one of its folders (default: the first)
	Title   string `json:"title"`   // its name on the deck (default: the project's)
}

// ProjectStarted is an agent StartInProject started. Prompt is the first
// task, checked, not sent yet: the caller sends it once the agent is ready.
type ProjectStarted struct {
	ID, Project, Folder, Tool, Prompt string
}

// StartInProject starts an agent in one of the project's folders (the
// first, unless o picks another), with its tool and account unless o picks
// others.
func (a *Admin) StartInProject(name string, o ProjectStart) (ProjectStarted, error) {
	list := a.Projects()
	i := findProject(list, name)
	if i < 0 {
		return ProjectStarted{}, projectErr(http.StatusNotFound, "no project called "+name)
	}
	p := list[i]
	if a.Starter == nil {
		return ProjectStarted{}, projectErr(http.StatusServiceUnavailable, "this service can't start agents (demo mode)")
	}
	tool := p.Tool
	if o.Tool != "" {
		if !oneOf(o.Tool, ProjectTools...) {
			return ProjectStarted{}, projectErr(http.StatusBadRequest, "tool must be one of "+strings.Join(ProjectTools, ", "))
		}
		tool = o.Tool
	}
	if tool == "" {
		tool = "claude"
	}
	prompt := ""
	if strings.TrimSpace(o.Prompt) != "" {
		var err error
		if prompt, err = server.CleanPrompt(o.Prompt); err != nil {
			return ProjectStarted{}, projectErr(http.StatusBadRequest, err.Error())
		}
	}
	account := p.Account
	if o.Account != "" {
		if _, ok := accounts.Find(o.Account); !ok {
			return ProjectStarted{}, projectErr(http.StatusBadRequest, "no Claude account called "+o.Account)
		}
		account = o.Account
	}
	if tool != "claude" {
		account = ""
	}
	dir := p.Path
	if o.Folder != "" {
		want, ok := filepath.Clean(expandHome(strings.TrimSpace(o.Folder))), false
		for _, f := range p.Paths {
			if filepath.Clean(f) == want {
				dir, ok = f, true
				break
			}
		}
		if !ok {
			return ProjectStarted{}, projectErr(http.StatusBadRequest, o.Folder+" isn't one of "+p.Name+"'s folders")
		}
	}
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = p.Name
	}
	id, err := a.Starter.Create(server.CreateOpts{Dir: dir, Title: title, Tool: tool, Account: account})
	if err != nil {
		return ProjectStarted{}, projectErr(http.StatusBadRequest, err.Error())
	}
	return ProjectStarted{ID: id, Project: p.Name, Folder: dir, Tool: tool, Prompt: prompt}, nil
}

// firstPrompt sends a started agent its first task once it is ready.
func (a *Admin) firstPrompt(s ProjectStarted) {
	if err := a.Starter.WaitReady(s.ID, startWait); err != nil {
		log.Printf("project %s: agent %s got no first prompt: %v", s.Project, s.ID, err)
		return
	}
	if err := a.Starter.Prompt(s.ID, s.Prompt, false); err != nil {
		log.Printf("project %s: agent %s: first prompt: %v", s.Project, s.ID, err)
	}
}
