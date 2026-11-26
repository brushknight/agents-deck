package main

// agentctl machines: the other machines of a multi-node workspace.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/node"
	"github.com/brushknight/agents-deck/backend/internal/server"
)

const machinesUsage = `usage: agentctl machines                      the machines of this workspace
       agentctl machines invite               a one-time code for a new machine (5 minutes)
       agentctl machines join <address> <code>  join the workspace of the machine at address
       agentctl machines remove <name>        take a machine out (it is told when it can be reached)
       agentctl machines leave                take this machine out of the workspace
       agentctl machines waiting              calls waiting for a machine out of reach, and recent answers
       agentctl machines cancel <key>         take a waiting call back before it goes`

func machinesCmd(args []string) error {
	if len(args) == 0 {
		return machinesList()
	}
	switch args[0] {
	case "invite":
		var r struct {
			Name, Code, Port, Fingerprint string
			Expires                       time.Time
		}
		if err := call("POST", "/local/machines/invite", nil, &r); err != nil {
			return err
		}
		fmt.Printf("join code: %s (one use, until %s)\n", r.Code, r.Expires.Local().Format("15:04"))
		fmt.Printf("on the new machine:\n  agentctl machines join <this machine's address>:%s %s\n", r.Port, r.Code)
		fmt.Printf("this machine: %s · CA %s\n", r.Name, r.Fingerprint)
		return nil
	case "join":
		if len(args) != 3 {
			return errors.New(machinesUsage)
		}
		var r struct{ Name, Address, Scope string }
		if err := callTimeout("POST", "/local/machines/join", map[string]string{"address": args[1], "code": args[2]}, &r, 45*time.Second); err != nil {
			return err
		}
		fmt.Printf("joined %s at %s: each machine may %s on the other\n", r.Name, r.Address, scopeWords(r.Scope))
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return errors.New(machinesUsage)
		}
		var r struct {
			Name string
			Told bool
		}
		if err := callTimeout("POST", "/local/machines/remove", map[string]string{"name": args[1]}, &r, 20*time.Second); err != nil {
			return err
		}
		if r.Told {
			fmt.Printf("removed %s; it forgot this machine too\n", r.Name)
		} else {
			fmt.Printf("removed %s here; it couldn't be reached, so run agentctl machines remove on it too, or leave there\n", r.Name)
		}
		return nil
	case "waiting":
		var r struct{ Outbox []server.OutboxRow }
		if err := call("GET", "/local/outbox", nil, &r); err != nil {
			return err
		}
		any := false
		for _, m := range r.Outbox {
			for i, c := range m.Waiting {
				any = true
				fmt.Printf("%s  waits for %s (%d)  %s %s  asked by %s %s ago\n", c.Key, m.Machine, i+1, c.Method, c.Path, c.By, humanAgo(time.Since(c.At)))
			}
		}
		for _, m := range r.Outbox {
			for _, a := range m.Answers {
				any = true
				fmt.Printf("%s  %s answered %d %s ago%s\n", a.Key, m.Machine, a.Code, humanAgo(time.Since(a.At)), answerError(a.Body))
			}
		}
		if !any {
			fmt.Println("no calls wait for another machine")
		}
		return nil
	case "cancel":
		if len(args) != 2 {
			return errors.New(machinesUsage)
		}
		if err := call("POST", "/local/outbox/cancel", map[string]string{"key": args[1]}, nil); err != nil {
			return err
		}
		fmt.Println("cancelled: it won't be sent")
		return nil
	case "leave":
		var r struct{ Left []string }
		if err := callTimeout("POST", "/local/machines/leave", nil, &r, 60*time.Second); err != nil {
			return err
		}
		if len(r.Left) == 0 {
			fmt.Println("this machine isn't in a workspace")
		} else {
			fmt.Printf("left the workspace: %s\n", strings.Join(r.Left, ", "))
		}
		return nil
	}
	return errors.New(machinesUsage)
}

func machinesList() error {
	var r struct {
		Name     string
		Machines []server.MachineRow
	}
	if err := call("GET", "/local/machines", nil, &r); err != nil {
		return err
	}
	if len(r.Machines) == 0 {
		fmt.Printf("%s is on its own: agentctl machines invite makes a code for another machine\n", r.Name)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tAGENTS\tWAITING\tADDRESS")
	fmt.Fprintf(tw, "%s\tthis machine\t\t\t\n", r.Name)
	for _, m := range r.Machines {
		waiting := ""
		if m.Waiting > 0 {
			waiting = fmt.Sprint(m.Waiting)
		}
		fmt.Fprintf(tw, "%s\t%s\t\t%s\t%s\n", m.Name, machineStatus(m.State, time.Now()), waiting, m.Address)
	}
	return tw.Flush()
}

// machineStatus is the STATUS column: "online · 38 ms", or the away word
// with since when ("out of reach since 10:31", or a day for older ones).
func machineStatus(st node.State, now time.Time) string {
	if st.Reachable {
		return fmt.Sprintf("online · %d ms", st.RTTMs)
	}
	if st.AwaySince.IsZero() {
		return node.AwayWord
	}
	since := st.AwaySince.Local()
	when := since.Format("15:04")
	if now.Sub(since) > 20*time.Hour {
		when = since.Format("Jan 2 15:04")
	}
	s := node.AwayWord + " since " + when
	if st.OneWay {
		s += " (it reaches this machine; this one can't reach it: its port 7341?)"
	}
	return s
}

// answerError is ": <error>" from an answer's body, if it has one.
func answerError(body []byte) string {
	var e struct{ Error string }
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return ": " + e.Error
	}
	return ""
}

// scopeWords says what a scope lets a machine do.
func scopeWords(s string) string {
	switch s {
	case "admin":
		return "start, stop and move agents (admin)"
	case "act":
		return "answer and prompt agents (act)"
	}
	return s
}
