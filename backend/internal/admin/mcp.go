package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// mcpInfo is GET /admin/v1/mcp: the tokens (never their secrets) and where
// MCP clients connect.
func (a *Admin) mcpInfo(w http.ResponseWriter, r *http.Request) {
	if a.Tokens == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP is not available on this service")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": a.Tokens.List(), "url": a.MCPURL, "agentctl": a.agentctl()})
}

// agentctl is how the stdio transport is started: this agentctl's full
// path, since Claude Code may not have ~/.local/bin on its PATH.
func (a *Admin) agentctl() string {
	if a.Exe == "" {
		return "agentctl"
	}
	return a.Exe
}

// mcpCreate is POST /admin/v1/mcp/tokens {"name", "scope"}: a new token,
// shown this once, with the commands that add it to Claude Code.
func (a *Admin) mcpCreate(w http.ResponseWriter, r *http.Request) {
	if a.Tokens == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP is not available on this service")
		return
	}
	var b struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := readBody(r, 4096, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.Scope == "" {
		b.Scope = string(devices.Act)
	}
	sc, err := devices.ParseScope(b.Scope)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	secret, t, err := a.Tokens.Create(strings.TrimSpace(b.Name), sc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": secret, "name": t.Name, "scope": t.Scope, "url": a.MCPURL,
		"commands": MCPCommands(a.MCPURL, a.agentctl(), secret)})
}

// MCPCommands are the `claude mcp add` lines for a token: over HTTP with
// the Authorization header, and over stdio (agentctl mcp) with the token in
// AGENTSDECK_MCP_TOKEN.
func MCPCommands(url, agentctl, token string) map[string]string {
	if strings.ContainsAny(agentctl, " '\"") {
		agentctl = "'" + strings.ReplaceAll(agentctl, "'", `'\''`) + "'"
	}
	return map[string]string{
		"http":  "claude mcp add --transport http agents-deck " + url + " --header \"Authorization: Bearer " + token + "\"",
		"stdio": "claude mcp add agents-deck -e AGENTSDECK_MCP_TOKEN=" + token + " -- " + agentctl + " mcp",
	}
}

// mcpAction is POST /admin/v1/mcp/tokens/{name}/{action}: revoke (it stops
// working at once and stays listed) or remove (forgotten).
func (a *Admin) mcpAction(w http.ResponseWriter, r *http.Request) {
	if a.Tokens == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP is not available on this service")
		return
	}
	var err error
	switch name := r.PathValue("name"); r.PathValue("action") {
	case "revoke":
		err = a.Tokens.Revoke(name)
	case "remove":
		err = a.Tokens.Remove(name)
	default:
		writeErr(w, http.StatusNotFound, "unknown action: revoke or remove")
		return
	}
	switch {
	case errors.Is(err, devices.ErrUnknown):
		writeErr(w, http.StatusNotFound, "no such token")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
