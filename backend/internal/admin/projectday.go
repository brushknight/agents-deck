package admin

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// A project's day: for each of its folders, the agents working in it now and
// how long its agents worked today (the time they spent running since
// midnight, store/worked.go). The Settings page shows it on the project;
// the Day summary's section per project is meant to read the same.

// ProjectDay is GET /admin/v1/projects/{name}/today.
type ProjectDay struct {
	Project model.Project `json:"project"`
	Since   time.Time     `json:"since"` // midnight, this computer's clock
	Folders []DayFolder   `json:"folders"`
}

// DayFolder is one of the project's folders, in the project's order.
type DayFolder struct {
	Folder    string     `json:"folder"` // as written in the project ("~/" allowed)
	Path      string     `json:"path"`   // in full
	Agents    []DayAgent `json:"agents"` // working in it now, in the deck's order
	ActiveMin int        `json:"activeMin"`
}

// DayAgent is an agent working in a folder now.
type DayAgent struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status model.Status `json:"status"`
	// Worktree is the git worktree inside the folder it works in, relative
	// to the folder ("" = the folder's own checkout).
	Worktree string `json:"worktree,omitempty"`
}

// dayOf works out p's day from the agents on the deck now, the time each
// worked today (by id), that of agents removed today (by working folder),
// and which project folder a working folder belongs to (Admin.ProjectOf).
func dayOf(p Project, agents []model.Agent, worked, gone map[string]time.Duration,
	match func(cwd string) (*model.Project, string), since time.Time) ProjectDay {
	out := ProjectDay{Project: model.Project{ID: p.ID, Name: p.Name}, Since: since, Folders: make([]DayFolder, 0, len(p.Folders))}
	at := map[string]int{} // a folder in full, cleaned: its place in out.Folders
	for _, f := range p.Folders {
		dir := cleanDir(f)
		if _, ok := at[dir]; ok || dir == "" {
			continue
		}
		at[dir] = len(out.Folders)
		out.Folders = append(out.Folders, DayFolder{Folder: f, Path: expandHome(f), Agents: []DayAgent{}})
	}
	sums := make([]time.Duration, len(out.Folders))
	folderOf := func(cwd string) (int, string) {
		ref, dir := match(cwd)
		if ref == nil || ref.ID != p.ID {
			return -1, ""
		}
		i, ok := at[dir]
		if !ok {
			return -1, ""
		}
		return i, dir
	}
	for _, a := range agents {
		i, dir := folderOf(a.Cwd)
		if i < 0 {
			continue
		}
		name := a.Title
		if name == "" {
			name = a.Folder
		}
		out.Folders[i].Agents = append(out.Folders[i].Agents, DayAgent{ID: a.ID, Name: name, Status: a.Status, Worktree: worktreeOf(a.Cwd, dir)})
		sums[i] += worked[a.ID]
	}
	for cwd, d := range gone {
		if i, _ := folderOf(cwd); i >= 0 {
			sums[i] += d
		}
	}
	for i, d := range sums {
		out.Folders[i].ActiveMin = int((d + 30*time.Second) / time.Minute)
	}
	return out
}

// worktreeOf is the git worktree inside folder that cwd is in (a folder
// with a .git file pointing into a repository's worktrees, as `git worktree
// add` makes), relative to folder with forward slashes; "" for none. It
// looks at the few folders between cwd and folder only.
func worktreeOf(cwd, folder string) string {
	dir := cleanDir(cwd)
	for dir != "" && dir != folder && inside(dir, folder) {
		if b, err := readSmall(filepath.Join(dir, ".git")); err == nil && bytes.HasPrefix(b, []byte("gitdir:")) &&
			bytes.Contains(bytes.ReplaceAll(b, []byte(`\`), []byte("/")), []byte("/worktrees/")) {
			rel, _ := filepath.Rel(folder, dir)
			return filepath.ToSlash(rel)
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return ""
}

// readSmall reads the start of a regular file (a .git file is one line).
func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	b := make([]byte, 1024)
	n, _ := f.Read(b)
	return b[:n], nil
}

// projectToday is GET /admin/v1/projects/{name}/today: {name} is the
// project's name or id.
func (a *Admin) projectToday(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	list := a.Projects()
	i := findProject(list, name)
	if i < 0 {
		writeErr(w, http.StatusNotFound, "no project called "+name)
		return
	}
	now := time.Now()
	var agents []model.Agent
	var worked, gone map[string]time.Duration
	if a.Store != nil {
		agents = a.Store.Snapshot().Agents
		worked, gone = a.Store.Today(now)
	}
	y, m, d := now.Date()
	writeJSON(w, http.StatusOK, dayOf(list[i], agents, worked, gone, a.ProjectOf, time.Date(y, m, d, 0, 0, 0, 0, now.Location())))
}
