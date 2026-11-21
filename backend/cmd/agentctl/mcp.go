package main

// MCP from the command line (docs/mcp.md):
//
//	agentctl mcp [--token T]                the stdio transport for an MCP client (token from AGENTSDECK_MCP_TOKEN)
//	agentctl mcp token create <name> [--scope view|act|admin]
//	agentctl mcp token list
//	agentctl mcp token revoke <name>

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// mcpTokenEnv holds the token for `agentctl mcp`.
const mcpTokenEnv = "AGENTSDECK_MCP_TOKEN"

const mcpUsage = "usage: agentctl mcp [--token T] | agentctl mcp token create <name> [--scope view|act|admin] | token list | token revoke <name>"

func mcpCmd(args []string) error {
	if len(args) > 0 && args[0] == "token" {
		return mcpTokenCmd(args[1:])
	}
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	token := fs.String("token", "", "the MCP token (default: $"+mcpTokenEnv+")")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		return errors.New(mcpUsage)
	}
	if *token == "" {
		*token = os.Getenv(mcpTokenEnv)
	}
	return mcpStdio(os.Stdin, os.Stdout, strings.TrimSpace(*token), func(body []byte, token string) (int, []byte, error) {
		req, _ := http.NewRequest(http.MethodPost, "http://agentd/local/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		// start_agent waits up to a minute for a new agent.
		resp, err := unixClient(90 * time.Second).Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return resp.StatusCode, b, err
	})
}

// mcpStdio is the stdio transport: one JSON-RPC message per line in, each
// passed to the service as is (with the token), its answer one line out.
// The service checks the token exactly as on POST /mcp.
func mcpStdio(in io.Reader, out io.Writer, token string, post func(body []byte, token string) (int, []byte, error)) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	w := bufio.NewWriter(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		code, body, err := post(line, token)
		switch {
		case err != nil:
			body = rpcErrorLine(line, -32603, "can't reach the agents deck service ("+err.Error()+"): is it running? agentctl status")
		case code == http.StatusAccepted || len(bytes.TrimSpace(body)) == 0 || isNotification(line):
			continue // a notification gets no answer, not even an error
		case code >= 300 && !bytes.Contains(body, []byte(`"jsonrpc"`)):
			body = rpcErrorLine(line, -32603, "the service answered "+http.StatusText(code))
		}
		w.Write(bytes.TrimSpace(body))
		w.WriteByte('\n')
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return sc.Err()
}

// isNotification: a single JSON-RPC message without an id.
func isNotification(line []byte) bool {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	return line[0] == '{' && json.Unmarshal(line, &req) == nil && req.Method != "" && len(req.ID) == 0
}

// rpcErrorLine is a JSON-RPC error for the request in line.
func rpcErrorLine(line []byte, code int, msg string) []byte {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(line, &req)
	if len(req.ID) == 0 {
		req.ID = json.RawMessage("null")
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
	return b
}

func mcpTokenCmd(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "create", "new":
		fs := flag.NewFlagSet("mcp token create", flag.ExitOnError)
		scope := fs.String("scope", "act", "what it may do: view, act (prompts, answers, interrupt) or admin (also start agents)")
		if len(args) < 2 {
			return errors.New(mcpUsage)
		}
		_ = fs.Parse(args[2:])
		var r struct {
			Token    string            `json:"token"`
			Name     string            `json:"name"`
			Scope    string            `json:"scope"`
			Commands map[string]string `json:"commands"`
		}
		if err := adminCall("POST", "/admin/v1/mcp/tokens", map[string]string{"name": args[1], "scope": *scope}, &r); err != nil {
			return err
		}
		fmt.Printf("token %s (scope %s): %s\n\nIt is shown only now. Add it to Claude Code:\n\n  %s\n\nor over stdio:\n\n  %s\n",
			r.Name, r.Scope, r.Token, r.Commands["http"], r.Commands["stdio"])
		return nil
	case "list", "ls":
		var r struct {
			Tokens []devices.Token `json:"tokens"`
			URL    string          `json:"url"`
		}
		if err := adminCall("GET", "/admin/v1/mcp", nil, &r); err != nil {
			return err
		}
		if len(r.Tokens) == 0 {
			fmt.Println("no MCP tokens: agentctl mcp token create <name>")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSCOPE\tCREATED\tLAST USED\tSTATE")
		for _, t := range r.Tokens {
			used, state := "never", "active"
			if !t.LastUsed.IsZero() {
				used = t.LastUsed.Local().Format("2 Jan 15:04")
			}
			if t.Revoked {
				state = "revoked"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.Scope, t.Created.Local().Format("2 Jan 15:04"), used, state)
		}
		tw.Flush()
		fmt.Println("\nendpoint:", r.URL)
		return nil
	case "revoke", "remove", "rm":
		if len(args) != 2 {
			return errors.New(mcpUsage)
		}
		action := args[0]
		if action == "rm" {
			action = "remove"
		}
		if err := adminCall("POST", "/admin/v1/mcp/tokens/"+args[1]+"/"+action, nil, nil); err != nil {
			return err
		}
		fmt.Printf("%s: %sd\n", args[1], action)
		return nil
	}
	return errors.New(mcpUsage)
}
