package admin

import (
	"net/http"
	"path/filepath"
	"slices"

	"github.com/brushknight/agents-deck/backend/internal/server"
)

// MCPProjects is the project list for MCP's project tools: every change
// goes through AddProject, EditProject and RemoveProject, with their checks.
func (a *Admin) MCPProjects() server.ProjectBook { return mcpProjects{a} }

type mcpProjects struct{ a *Admin }

func projectInfo(p Project) server.ProjectInfo {
	return server.ProjectInfo{ID: p.ID, Name: p.Name, Folders: p.Paths, Tool: p.Tool, Account: p.Account}
}

func (m mcpProjects) ListProjects() []server.ProjectInfo {
	list := m.a.Projects()
	out := make([]server.ProjectInfo, len(list))
	for i, p := range list {
		out[i] = projectInfo(p)
	}
	return out
}

func (m mcpProjects) CreateProject(p server.ProjectInfo) (server.ProjectInfo, error) {
	added, err := m.a.AddProject(Project{Name: p.Name, Folders: p.Folders, Tool: p.Tool, Account: p.Account})
	if err != nil {
		return server.ProjectInfo{}, err
	}
	return projectInfo(added), nil
}

func (m mcpProjects) ChangeProject(project string, c server.ProjectChange) (server.ProjectInfo, error) {
	changed, err := m.a.EditProject(project, func(p *Project) error {
		if c.Name != nil {
			p.Name = *c.Name
		}
		if c.Tool != nil {
			p.Tool = *c.Tool
		}
		if c.Account != nil {
			p.Account = *c.Account
		}
		for _, gone := range c.RemoveFolders {
			n := len(p.Folders)
			want := filepath.Clean(expandHome(gone))
			p.Folders = slices.DeleteFunc(p.Folders, func(f string) bool { return filepath.Clean(expandHome(f)) == want })
			if len(p.Folders) == n {
				return projectErr(http.StatusBadRequest, gone+" isn't one of "+p.Name+"'s folders")
			}
		}
		p.Folders = append(p.Folders, c.AddFolders...)
		if len(p.Folders) == 0 {
			return projectErr(http.StatusBadRequest, "a project keeps at least one folder: remove the project instead")
		}
		return nil
	})
	if err != nil {
		return server.ProjectInfo{}, err
	}
	return projectInfo(changed), nil
}

func (m mcpProjects) RemoveProject(project string) error { return m.a.RemoveProject(project) }

func (m mcpProjects) StartInProject(project string, o server.ProjectStartOpts) (server.ProjectStartedAgent, error) {
	s, err := m.a.StartInProject(project, ProjectStart{Folder: o.Folder, Tool: o.Tool, Account: o.Account, Title: o.Title})
	if err != nil {
		return server.ProjectStartedAgent{}, err
	}
	return server.ProjectStartedAgent{ID: s.ID, Project: s.Project, Folder: s.Folder, Tool: s.Tool}, nil
}
