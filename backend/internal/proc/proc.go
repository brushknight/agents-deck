// Package proc answers small questions about other processes on this
// computer and starts the few it needs (a browser for a link), the same way
// on every platform the service runs on.
package proc

import (
	"encoding/json"
	"strings"
	"time"
)

// Process is one running program, as Windows lists it (see List).
type Process struct {
	PID         int       `json:"pid"`
	Name        string    `json:"name"`
	CommandLine string    `json:"cmd"`
	Started     time.Time `json:"started"`
}

// Argv is the process's command line split into arguments.
func (p Process) Argv() []string { return SplitCommandLine(p.CommandLine) }

// parseList reads the JSON array the PowerShell query in List prints
// (empty output: no processes).
func parseList(out []byte) ([]Process, error) {
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}
	var ps []Process
	if err := json.Unmarshal(out, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// SplitCommandLine splits a Windows command line the way programs built with
// the Microsoft C runtime do: spaces separate arguments, double quotes group,
// 2n backslashes before a quote are n backslashes, 2n+1 are n and a literal
// quote, and backslashes elsewhere are literal (C:\dir\file stays as is).
func SplitCommandLine(s string) []string {
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					quoted = !quoted
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			inArg = true
		case c == '"':
			if quoted && i+1 < len(s) && s[i+1] == '"' { // "" inside quotes: a literal quote
				cur.WriteByte('"')
				i++
			} else {
				quoted = !quoted
			}
			inArg = true
		case (c == ' ' || c == '\t') && !quoted:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}
