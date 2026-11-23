package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// ProjectBook is the project list as MCP changes it. The admin service
// keeps the list and does every check (admin/projects.go); these tools only
// pass the arguments on. nil: MCP has no project tools.
type ProjectBook interface {
	ListProjects() []ProjectInfo
	CreateProject(p ProjectInfo) (ProjectInfo, error)
	ChangeProject(project string, c ProjectChange) (ProjectInfo, error)
	RemoveProject(project string) error
	// StartInProject starts an agent in one of the project's folders, with
	// its tool and account unless o picks others.
	StartInProject(project string, o ProjectStartOpts) (ProjectStartedAgent, error)
}

// ProjectStartOpts picks what start_agent starts in a project; each is
// optional.
type ProjectStartOpts struct {
	Folder, Tool, Account, Title string
}

// ProjectStartedAgent is the agent StartInProject started.
type ProjectStartedAgent struct {
	ID, Project, Folder, Tool string
}

// ProjectInfo is a project as MCP shows it: its folders in full.
type ProjectInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
	Tool    string   `json:"tool,omitempty"`    // what its agents start with ("" = claude)
	Account string   `json:"account,omitempty"` // the Claude account they start in ("" = the default)
	Agents  int      `json:"agents"`            // agents on the deck working in its folders (list_projects only)
}

// ProjectChange is what update_project changes; nil and empty leave a part
// as it is.
type ProjectChange struct {
	Name, Tool, Account       *string
	AddFolders, RemoveFolders []string
}

var projectProp = prop("string", "The project: its name (any case) or its id, from list_projects.")

func foldersProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

var projectTools = []mcpTool{
	{Name: "list_projects", Title: "List projects",
		Description: "The projects on this deck: id, name, folders (full paths), the tool and Claude account their agents start with, " +
			"and how many agents work in them now. An agent belongs to the project whose folder holds its working folder (the deepest wins).",
		InputSchema: schema(nil, map[string]any{}), Annotations: map[string]any{"readOnlyHint": true}},
	{Name: "create_project", Title: "Create a project",
		Description: "Adds a project: a name and one or more folders that exist on this computer. A folder belongs to one project. Needs scope admin.",
		InputSchema: schema([]string{"name", "folders"}, map[string]any{
			"name":    prop("string", "Its name, at most 40 characters, no slashes."),
			"folders": foldersProp("Full paths (or ~/…), at most 20; agents start in the first by default."),
			"tool":    map[string]any{"type": "string", "enum": []string{"claude", "codex", "shell"}, "description": "What its agents start with (default claude)."},
			"account": prop("string", "claude only: the Claude account its agents start in (default: the default one)."),
		})},
	{Name: "update_project", Title: "Change a project",
		Description: "Renames a project, adds or removes folders, or changes its tool or account. Leave out what stays. " +
			"Its id stays the same. Needs scope admin.",
		InputSchema: schema([]string{"project"}, map[string]any{
			"project":        projectProp,
			"name":           prop("string", "A new name."),
			"add_folders":    foldersProp("Folders to add (full paths or ~/…)."),
			"remove_folders": foldersProp("Folders to take off; a project keeps at least one."),
			"tool":           map[string]any{"type": "string", "enum": []string{"", "claude", "codex", "shell"}, "description": "What its agents start with; \"\" is claude."},
			"account":        prop("string", "The Claude account its agents start in; \"\" is the default one."),
		})},
	{Name: "remove_project", Title: "Remove a project",
		Description: "Takes a project off the list. Its folders and agents stay as they are; the agents just have no project. Needs scope admin.",
		InputSchema: schema([]string{"project"}, map[string]any{"project": projectProp})},
}

// strs reads a list of strings (or one string) from the arguments.
func (a mcpArgs) strs(k string) []string {
	switch v := a[k].(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// optStr is a string argument that was given, "" included (nil: left out).
func (a mcpArgs) optStr(k string) *string {
	if _, ok := a[k].(string); !ok {
		return nil
	}
	v := a.str(k)
	return &v
}

func (s *Server) projectBook() (ProjectBook, error) {
	if s.Projects == nil {
		return nil, errors.New("projects are not available on this service")
	}
	return s.Projects, nil
}

// projectChanged audits a project change made over MCP.
func (s *Server) projectChanged(who devices.Device, action, detail string, err error) {
	code := http.StatusOK
	if err != nil {
		code = http.StatusBadRequest
	}
	if s.Audit != nil {
		s.Audit.Log(devices.AuditEntry{Device: who.Name, Action: action, Status: code, Detail: detail})
	}
	if s.Devices != nil {
		s.Devices.Seen(who.Name)
	}
	if err == nil {
		log.Printf("mcp: %s: %s %s", who.Name, action, detail)
	}
}

func needAdmin(who devices.Device, tool string) error {
	if who.Scope.Allows(devices.Admin) {
		return nil
	}
	return fmt.Errorf("%s may %s, %s needs admin: create a token with scope admin in Settings, MCP", who.Name, who.Scope, tool)
}

func toolListProjects(s *Server, who devices.Device, _ mcpArgs) (string, error) {
	book, err := s.projectBook()
	if err != nil {
		return "", err
	}
	list := book.ListProjects()
	if agents, err := s.agents(who); err == nil {
		count := map[string]int{}
		for _, a := range agents {
			if a.Project != nil {
				count[a.Project.ID]++
			}
		}
		for i := range list {
			list[i].Agents = count[list[i].ID]
		}
	}
	return asJSON(map[string]any{"projects": list}), nil
}

func toolCreateProject(s *Server, who devices.Device, a mcpArgs) (string, error) {
	if err := needAdmin(who, "create_project"); err != nil {
		return "", err
	}
	book, err := s.projectBook()
	if err != nil {
		return "", err
	}
	name, err := a.need("name")
	if err != nil {
		return "", err
	}
	p, err := book.CreateProject(ProjectInfo{Name: name, Folders: a.strs("folders"), Tool: a.str("tool"), Account: a.str("account")})
	s.projectChanged(who, "create project", name, err)
	if err != nil {
		return "", err
	}
	return asJSON(p), nil
}

func toolUpdateProject(s *Server, who devices.Device, a mcpArgs) (string, error) {
	if err := needAdmin(who, "update_project"); err != nil {
		return "", err
	}
	book, err := s.projectBook()
	if err != nil {
		return "", err
	}
	project, err := a.need("project")
	if err != nil {
		return "", err
	}
	c := ProjectChange{Name: a.optStr("name"), Tool: a.optStr("tool"), Account: a.optStr("account"),
		AddFolders: a.strs("add_folders"), RemoveFolders: a.strs("remove_folders")}
	if c.Name != nil && *c.Name == "" {
		return "", errors.New("name can't be empty: leave it out to keep the name")
	}
	p, err := book.ChangeProject(project, c)
	s.projectChanged(who, "change project", project, err)
	if err != nil {
		return "", err
	}
	return asJSON(p), nil
}

func toolRemoveProject(s *Server, who devices.Device, a mcpArgs) (string, error) {
	if err := needAdmin(who, "remove_project"); err != nil {
		return "", err
	}
	book, err := s.projectBook()
	if err != nil {
		return "", err
	}
	project, err := a.need("project")
	if err != nil {
		return "", err
	}
	err = book.RemoveProject(project)
	s.projectChanged(who, "remove project", project, err)
	if err != nil {
		return "", err
	}
	return asJSON(map[string]any{"removed": project}), nil
}
