package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
)

// zshCompletion is printed by `agentctl completion zsh`; load it with
// `source <(agentctl completion zsh)` after compinit.
const zshCompletion = `#compdef agentctl
# zsh completion for agentctl — dynamic values come from "agentctl __complete".

_agentctl_values() {
  local -a vals
  vals=("${(@f)$(agentctl __complete "$1" 2>/dev/null)}")
  (( ${#vals} )) && [[ -n ${vals[1]} ]] && _describe -t "$1" "$2" vals
}
_agentctl_agents()    { _agentctl_values agents 'agents' }
_agentctl_resumable() { _agentctl_values resumable 'sessions to resume' }

_agentctl() {
  local -a commands
  commands=(
    'new:start an agent in a folder and attach'
    'attach:show an agent in this terminal (ctrl-q detaches)'
    'ls:list agents'
    'rm:stop and forget an agent'
    'move:move an agent to another position'
    'sessions:look up Claude sessions by words'
    'resume:continue a Claude session or ended agent'
    'sim:simulated agents for demos'
    'web:open the dashboard in the browser'
    'pair:print the values the panel needs'
    'serve:run the daemon'
    'install:install and start the launchd agent'
    'restore:bring back agents lost with the tmux server'
    'reopen:iTerm tabs for every agent no terminal shows'
    'set:change a setting (term iterm|tmux|window, mouse tmux|native)'
    'get:show settings'
    'completion:print the shell completion script'
    'version:print the version'
    'help:show usage'
  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'agentctl command' commands
    return
  fi
  local cmd=${words[2]}
  case $cmd in
    new)
      _arguments -s \
        '-t[title]:title:' \
        '-c[tool]:tool:(claude codex gemini shell)' \
        '-d[start detached]' \
        '1:folder:_files -/' ;;
    attach|a|rm)
      (( CURRENT == 3 )) && _agentctl_agents ;;
    move|mv)
      (( CURRENT == 3 )) && _agentctl_agents ;;
    resume|continue)
      _arguments -s \
        '--force[resume even if it looks open elsewhere]' \
        '-d[start detached]' \
        '1:session:_agentctl_resumable' ;;
    sessions|s)
      _arguments -s '-n[how many to show (0 = all)]:count:(10 20 50 0)' '*:search words:' ;;
    sim)
      if (( CURRENT == 3 )); then
        local -a sub; sub=('start:start simulated agents' 'stop:remove all simulated agents')
        _describe -t sim 'sim command' sub
      elif [[ ${words[3]} == start ]]; then
        _arguments -s \
          '--root[parent folder with projects]:folder:_files -/' \
          '--slots[how many agents]:slots:(4 8 12 16)' \
          '--speed[activity speed]:speed:(0.5 1 1.5 2 3)' \
          '--patience[seconds before prompts self-answer, -1 never]:seconds:(30 45 90 -1)'
      fi ;;
    pair)       _arguments '--rotate[issue a new token]' ;;
    serve)      _arguments '--demo[simulated fleet from the fixture]' ;;
    completion) (( CURRENT == 3 )) && _values 'shell' zsh ;;
    set)
      if (( CURRENT == 3 )); then _values 'setting' 'term[how focus shows an agent]' 'mouse[who owns the mouse in agent terminals]' 'restore[what happens to agents lost with tmux]'
      elif [[ ${words[3]} == term ]]; then
        local -a modes; modes=('iterm:select the agent'"'"'s iTerm tab and pane' 'tmux:switch your last-used tab to the agent' 'window:just raise the agent'"'"'s window')
        _describe -t modes 'focus mode' modes
      elif [[ ${words[3]} == restore ]]; then
        local -a modes; modes=('ask:keep them for agentctl restore' 'auto:bring them back right away')
        _describe -t modes 'restore mode' modes
      elif [[ ${words[3]} == mouse ]]; then
        local -a modes; modes=('tmux:wheel scrolls tmux history, drag copies, click opens links' 'native:your terminal handles the mouse and its own scrollback')
        _describe -t modes 'mouse mode' modes
      fi ;;
    get) (( CURRENT == 3 )) && _values 'setting' term mouse restore ;;
  esac
}

if (( $+functions[compdef] )); then
  compdef _agentctl agentctl
fi
`

func completionCmd(args []string) error {
	if len(args) != 1 || args[0] != "zsh" {
		return errors.New("usage: agentctl completion zsh   (add  source <(agentctl completion zsh)  to ~/.zshrc after compinit)")
	}
	fmt.Print(zshCompletion)
	return nil
}

// completeCmd prints "value:description" lines for the zsh completion. It
// must be fast and silent: errors just mean no suggestions.
func completeCmd(args []string) {
	if len(args) != 1 {
		return
	}
	esc := func(s string) string { return strings.ReplaceAll(s, ":", `\:`) }
	switch args[0] {
	case "agents":
		s, err := state()
		if err != nil {
			return
		}
		for _, a := range s.Agents {
			desc := a.Title + " · " + string(a.Status)
			fmt.Printf("%s:%s\n", a.ID, esc(desc))
			if a.Title != "" && a.Status != model.Exited {
				fmt.Printf("%s:%s\n", esc(a.Title), esc(a.ID+" · "+string(a.Status)))
			}
		}
	case "resumable":
		if s, err := state(); err == nil {
			for _, a := range s.Agents {
				if a.Resumable {
					fmt.Printf("%s:%s\n", a.ID, esc("ended agent · "+a.Title))
				}
			}
		}
		all, err := claude.ScanSessions(claude.ProjectsDir())
		if err != nil {
			return
		}
		for i, ss := range all {
			if i == 40 {
				break
			}
			fmt.Printf("%s:%s\n", ss.ID[:8], esc(ago(ss.Modified)+" · "+clip(ss.Title(), 50)+" · "+shortHome(ss.Cwd)))
		}
	}
}
