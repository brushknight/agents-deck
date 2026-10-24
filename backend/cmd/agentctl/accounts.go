package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/hats"
)

var accountUsage = `usage:
  agentctl accounts                              the Claude accounts agents can run in
  agentctl account add <name> <folder> <colour>  a Claude config folder (~/.claude is the default one)
  agentctl account default <name>                the account new agents start in
  agentctl account rm <name>
colours: ` + strings.Join(hats.Colors, ", ")

// accountsCmd: agentctl accounts | agentctl account add|default|rm …
func accountsCmd(args []string) error {
	if len(args) == 0 {
		return listAccounts()
	}
	list := accounts.Configured()
	def := accounts.Default().Name
	switch args[0] {
	case "add":
		if len(args) != 4 {
			return errors.New(accountUsage)
		}
		a := accounts.Account{Name: args[1], Dir: args[2], Color: args[3]}
		if err := accounts.Validate(a); err != nil {
			return err
		}
		if st, err := os.Stat(a.Path()); err != nil || !st.IsDir() {
			return fmt.Errorf("no such folder: %s (log in once with CLAUDE_CONFIG_DIR=%s claude)", a.Path(), a.Dir)
		}
		replaced := false
		for i := range list {
			if list[i].Name == a.Name {
				list[i], replaced = a, true
			} else if list[i].Path() == a.Path() {
				return fmt.Errorf("%s already uses that folder", list[i].Name)
			}
		}
		if !replaced {
			list = append(list, a)
		}
		if len(list) == 1 {
			def = a.Name
		}
	case "default":
		if len(args) != 2 {
			return errors.New(accountUsage)
		}
		if _, ok := find(list, args[1]); !ok {
			return fmt.Errorf("no account named %q", args[1])
		}
		def = args[1]
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New(accountUsage)
		}
		i, ok := find(list, args[1])
		if !ok {
			return fmt.Errorf("no account named %q", args[1])
		}
		list = append(list[:i], list[i+1:]...)
		if def == args[1] {
			def = ""
		}
	default:
		return errors.New(accountUsage)
	}
	if err := accounts.Save(list, def); err != nil {
		return err
	}
	return listAccounts()
}

func find(list []accounts.Account, name string) (int, bool) {
	for i, a := range list {
		if a.Name == name {
			return i, true
		}
	}
	return 0, false
}

func listAccounts() error {
	list := accounts.Configured()
	if len(list) == 0 {
		fmt.Printf("one account: %s (Claude Code's default folder)\n\n", shortHome(accounts.Home()))
		fmt.Println("add accounts to show them on the deck and hand sessions between them:")
		fmt.Println("  agentctl account add personal ~/.claude sky")
		fmt.Println("  agentctl account add work ~/.claude-work yellow")
		return nil
	}
	def := accounts.Default().Name
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tFOLDER\tCOLOUR\t")
	for _, a := range list {
		mark := ""
		if a.Name == def {
			mark = "← new agents"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, shortHome(a.Path()), a.Color, mark)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(list) == 1 {
		fmt.Println("\none account: the deck shows accounts once there are two")
	}
	return nil
}

// handoff: agentctl handoff <agent> <account>
func handoff(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: agentctl handoff <id|title> <account>   (continue the session in another Claude account)")
	}
	a, err := resolve(args[0])
	if err != nil {
		return err
	}
	var res struct{ ID string }
	if err := call("POST", "/v1/agents/"+a.ID+"/handoff", map[string]string{"account": args[1]}, &res); err != nil {
		return err
	}
	if res.ID != a.ID {
		fmt.Printf("%s continues in %s as agent %s (the original keeps its terminal)\n", a.Title, args[1], res.ID)
	} else {
		fmt.Printf("%s continues in %s\n", a.Title, args[1])
	}
	return nil
}

// allSessions lists the Claude sessions of every account, newest first.
// account maps a session id to its account's name (only with accounts set up).
func allSessions() (all []claude.SessionInfo, account map[string]string, err error) {
	account = map[string]string{}
	seen := map[string]bool{}
	var firstErr error
	for _, a := range accounts.List() {
		got, err := claude.ScanSessions(a.ProjectsDir())
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, s := range got {
			if !seen[s.ID] { // handed over: the same session in two accounts, newest first
				seen[s.ID] = true
				all = append(all, s)
				if accounts.InUse() {
					account[s.ID] = a.Name
				}
			} else {
				for i := range all {
					if all[i].ID == s.ID && s.Modified.After(all[i].Modified) {
						all[i] = s
						if accounts.InUse() {
							account[s.ID] = a.Name
						}
					}
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Modified.After(all[j].Modified) })
	if len(all) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return all, account, nil
}

// accountDirs names where sessions are looked up, for "nothing found".
func accountDirs() string {
	var out []string
	for _, a := range accounts.List() {
		out = append(out, shortHome(a.ProjectsDir()))
	}
	return strings.Join(out, ", ")
}
