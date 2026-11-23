package admin

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// An agent belongs to the project whose folder holds its working folder: the
// folder itself or anything inside it (a worktree, a package). When folders
// of several projects hold it, the deepest wins, so a project for
// ~/dev/app/ios beats one for ~/dev/app. Two projects listing the very same
// folder (a projects.json from before one owner per folder) is settled by
// the order of the list: the first wins.
//
// The service answers from memory: the index is built when projects.json is
// read or written, and each working folder's answer is kept until then, so a
// snapshot of the deck reads nothing from disk.

// projectIndex is every project folder, deepest first.
type projectIndex struct {
	mu      sync.Mutex
	loaded  bool
	folders []indexedFolder
	cache   map[string]folderMatch
}

// indexedFolder is one project folder in full, cleaned.
type indexedFolder struct {
	dir string
	ref model.Project
}

// maxCachedFolders bounds the answers kept: past it they start again.
const maxCachedFolders = 1000

type folderMatch struct {
	ref *model.Project
	dir string
}

// indexFolders lists the projects' folders, deepest first; folders of the
// same depth keep the order of the list, so the first project wins a tie.
func indexFolders(list []Project) []indexedFolder {
	var out []indexedFolder
	for _, p := range list {
		for _, f := range p.Folders {
			if dir := cleanDir(f); dir != "" {
				out = append(out, indexedFolder{dir: dir, ref: model.Project{ID: p.ID, Name: p.Name}})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b indexedFolder) int { return len(b.dir) - len(a.dir) })
	return out
}

// cleanDir is a folder in full (~/ expanded), cleaned; "" when it isn't a
// full path.
func cleanDir(f string) string {
	f = strings.TrimSpace(f)
	if f == "" {
		return ""
	}
	dir := filepath.Clean(expandHome(f))
	if !filepath.IsAbs(dir) {
		return ""
	}
	return dir
}

// inside reports whether dir is folder or inside it, by whole path segments
// (~/dev/app2 isn't inside ~/dev/app). Both are cleaned.
func inside(dir, folder string) bool {
	if dir == folder {
		return true
	}
	if !strings.HasSuffix(folder, string(filepath.Separator)) { // the root ends in one already
		folder += string(filepath.Separator)
	}
	return strings.HasPrefix(dir, folder)
}

// matchFolder finds the project for a working folder in an index.
func matchFolder(folders []indexedFolder, cwd string) folderMatch {
	dir := cleanDir(cwd)
	if dir == "" {
		return folderMatch{}
	}
	for _, f := range folders { // deepest first: the first that holds it wins
		if inside(dir, f.dir) {
			ref := f.ref
			return folderMatch{ref: &ref, dir: f.dir}
		}
	}
	return folderMatch{}
}

// ProjectOf is the project an agent working in cwd belongs to, and the
// project folder it matched (nil and "" for none). It is Store.Project.
func (a *Admin) ProjectOf(cwd string) (*model.Project, string) {
	if cwd == "" {
		return nil, ""
	}
	a.index.mu.Lock()
	loaded := a.index.loaded
	a.index.mu.Unlock()
	if !loaded {
		a.Projects() // reads projects.json once, which builds the index
	}
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	m, ok := a.index.cache[cwd]
	if !ok {
		m = matchFolder(a.index.folders, cwd)
		if a.index.cache == nil || len(a.index.cache) >= maxCachedFolders {
			a.index.cache = map[string]folderMatch{}
		}
		a.index.cache[cwd] = m
	}
	if m.ref == nil {
		return nil, ""
	}
	ref := *m.ref
	return &ref, m.dir
}

// reindex builds the index from the list just read or written. When it
// changes, the screens get the deck again with each agent's new project.
func (a *Admin) reindex(list []Project) {
	folders := indexFolders(list)
	a.index.mu.Lock()
	changed := a.index.loaded && !slices.Equal(folders, a.index.folders)
	if changed || !a.index.loaded {
		a.index.folders, a.index.cache, a.index.loaded = folders, nil, true
	}
	a.index.mu.Unlock()
	if changed && a.Store != nil {
		a.Store.Touch()
	}
}

// folderOwner is the project other than self (by id) that lists one of
// folders exactly, and that folder as written there; -1 for none.
func folderOwner(list []Project, self string, folders []string) (int, string) {
	for _, f := range folders {
		dir := cleanDir(f)
		for i, p := range list {
			if p.ID == self {
				continue
			}
			for _, g := range p.Folders {
				if cleanDir(g) == dir {
					return i, g
				}
			}
		}
	}
	return -1, ""
}
