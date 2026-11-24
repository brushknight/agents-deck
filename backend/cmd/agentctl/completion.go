package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// zshCompletion is printed by `agentctl completion zsh`; load it with
// `source <(agentctl completion zsh)` after compinit.
const zshCompletion = `#compdef agentctl
# zsh completion for agentctl: agents and sessions come from "agentctl __complete".

# Agents and sessions by name or id: any part of a name, in any case, finds
# them. "agentctl __complete <list>" prints one "value<TAB>shown<TAB>hidden"
# line each; hidden values (ids) can be typed but aren't listed. The list
# keeps agentctl's order (most recently active first).
_agentctl_values() {
  local -a lines vals disp hid expl
  local l v rest
  lines=("${(@f)$(agentctl __complete "$1" 2>/dev/null)}")
  for l in "${lines[@]}"; do
    [[ -z $l ]] && continue
    v=${l%%$'\t'*}; rest=${l#*$'\t'}
    if [[ ${rest##*$'\t'} == 1 ]]; then
      hid+=("$v")
    else
      vals+=("$v"); disp+=("${rest%$'\t'*}")
    fi
  done
  (( ${#vals} + ${#hid} )) || return 1
  local m='m:{[:lower:][:upper:]}={[:upper:][:lower:]} l:|=* r:|=*'
  _description -V "$1" expl "$2"
  compadd "${expl[@]}" -M "$m" -l -d disp -a vals
  (( ${#hid} )) && compadd "${expl[@]}" -M "$m" -n -a hid
  return 0
}
# _arguments for a command's own options and arguments: the words after the
# command, as if it were the command line.
_agentctl_args() {
  shift words
  (( CURRENT-- ))
  _arguments "$@"
}
_agentctl_agents()    { _agentctl_values agents 'agents' }
_agentctl_bring()     { _agentctl_values bring 'agents and sessions to bring back' }
_agentctl_accounts()  { _agentctl_values accounts 'Claude accounts' }
_agentctl_devices()   { _agentctl_values devices 'devices' }

_agentctl() {
  local -a commands
  commands=(
    'new:start an agent in a folder and open it'
    'resume:bring an agent or Claude session back and open it'
    'restore:the same as resume; alone, brings back everything lost with tmux'
    'attach:open an agent in this terminal (ctrl-q detaches)'
    'ls:list agents'
    'rm:stop and forget an agent'
    'move:move an agent to another position'
    'sessions:look up Claude sessions by words'
    'reopen:iTerm tabs for every agent no terminal shows'
    'live:Claude sessions started by hand in other terminals'
    'add:put a hand-started Claude session on the deck'
    'hats:the hats agents wear (one per working folder)'
    'accounts:the Claude accounts agents run in'
    'handoff:continue a Claude session in another account'
    'sim:simulated agents for demos'
    'web:open the dashboard in the browser'
    'pair:pair a device'
    'devices:paired devices and boards'
    'link:Deck Link boards (USB or Wi-Fi)'
    'ports:USB ports and what is on them'
    'bluetooth:boards over Bluetooth'
    'flash:write firmware to a board over USB'
    'firmware:the firmware in the latest release and builds made here'
    'update:look for a new release, and install it'
    'system:what the service is doing'
    'settings:show or change the service settings'
    'diagnostics:a report to send when something is wrong'
    'cpu:how much CPU the service and the whole machine use'
    'status:is the service running and where its files are'
    'serve:run the service'
    'install:start the service at login, and now'
    'uninstall:stop the service and no longer start it at login'
    'set:change a setting (term, mouse, restore, hat)'
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
      _agentctl_args -s \
        '-t[title]:title:' \
        '-c[tool]:tool:(claude codex gemini shell)' \
        '-a[Claude account]:account:_agentctl_accounts' \
        '-d[start detached]' \
        '1:folder:_files -/' ;;
    resume|continue|restore)
      _agentctl_args -s \
        '-d[bring it back without opening it]' \
        '--force[resume a session even if it looks open elsewhere]' \
        '--all[restore every lost agent without asking]' \
        '-y[do not ask]' \
        '*:agent or session:_agentctl_bring' ;;
    attach|a|rm)
      (( CURRENT == 3 )) && _agentctl_agents ;;
    move|mv)
      (( CURRENT == 3 )) && _agentctl_agents ;;
    handoff)
      if (( CURRENT == 3 )); then _agentctl_agents
      elif (( CURRENT == 4 )); then _agentctl_accounts
      fi ;;
    ls|list)
      _agentctl_args '--sort[order]:order:((position\:"as on the deck" active\:"most recently active first"))' ;;
    accounts|account)
      if (( CURRENT == 3 )); then _values 'action' add default rm
      elif (( CURRENT == 4 )) && [[ ${words[3]} == (default|rm) ]]; then _agentctl_accounts
      elif (( CURRENT == 5 )) && [[ ${words[3]} == add ]]; then _files -/
      elif (( CURRENT == 6 )) && [[ ${words[3]} == add ]]; then _values 'colour' teal blue yellow green purple pink sky ink
      fi ;;
    sessions|s)
      _agentctl_args -s '-n[how many to show (0 = all)]:count:(10 20 50 0)' '*:search words:' ;;
    sim)
      if (( CURRENT == 3 )); then
        local -a sub; sub=('start:start simulated agents' 'stop:remove all simulated agents')
        _describe -t sim 'sim command' sub
      elif [[ ${words[3]} == start ]]; then
        _agentctl_args -s \
          '--root[parent folder with projects]:folder:_files -/' \
          '--slots[how many agents]:slots:(4 8 12 16)' \
          '--speed[activity speed]:speed:(0.5 1 1.5 2 3)' \
          '--patience[seconds before prompts self-answer, -1 never]:seconds:(30 45 90 -1)'
      fi ;;
    pair)
      _agentctl_args '--scope[what it may do]:scope:(view act admin)' '--bundle[write a key and certificate]:file:_files' '--rotate[issue a new token]' ;;
    devices)
      if (( CURRENT == 3 )); then
        local -a sub; sub=('revoke:cut a device off' 'approve:approve a board' 'remove:forget a device' 'scope:change what a device may do'
          'rename:rename a board' 'update:update a board'"'"'s firmware' 'uplink:how a board reaches this computer'
          'reset:start the certificates over' 'auto-approve:approve new boards on USB by themselves')
        _describe -t devices 'devices command' sub
      elif (( CURRENT == 4 )) && [[ ${words[3]} == (revoke|approve|remove|scope|rename|update|uplink) ]]; then _agentctl_devices
      elif (( CURRENT == 5 )) && [[ ${words[3]} == scope ]]; then _values 'scope' view act admin
      elif (( CURRENT == 5 )) && [[ ${words[3]} == uplink ]]; then _values 'uplink' auto wifi ble
      elif [[ ${words[3]} == reset ]]; then _agentctl_args '--yes[do not ask]'
      fi ;;
    link)
      if (( CURRENT == 3 )); then
        local -a sub; sub=('status:boards and ports' 'ports:find ports' 'add:register a port' 'rm:forget a port'
          'wifi:give a board its Wi-Fi network' 'pair:a new Wi-Fi certificate for a board' 'update:put an app image on a board'
          'sim:be a board without hardware' 'check:test a board'"'"'s firmware against a demo deck')
        _describe -t link 'link command' sub
      elif (( CURRENT == 4 )) && [[ ${words[3]} == (add|rm|check) ]]; then _files -W /dev -g 'cu.*'
      fi ;;
    ports)
      if (( CURRENT == 3 )); then _values 'action' register identify forget
      elif (( CURRENT == 4 )); then _files -W /dev -g 'cu.*'
      fi ;;
    bluetooth|ble)
      if (( CURRENT == 3 )); then
        local -a sub; sub=('scan:find a board nearby' 'ls:boards over Bluetooth' 'add:add a board with the code on its screen')
        _describe -t bluetooth 'bluetooth command' sub
      fi ;;
    flash)
      _agentctl_args '--board[the board type]:board:' '--version[firmware version]:version:' '--offset[where to write]:offset:(0x0 0x10000)' '--probe[only check the board]' '--local[the build made on this computer]' '*:port or image:_files' ;;
    firmware)   if (( CURRENT == 3 )); then _values 'action' ls add rm; elif [[ $words[3] == add ]]; then _files; fi ;;
    update)     _agentctl_args '--check[only look]' '--yes[do not ask]' ;;
    settings)
      if (( CURRENT == 3 )); then _values 'setting' start-at-login notifications sound restore updates network
      elif (( CURRENT == 4 )); then
        case ${words[3]} in
          start-at-login|sound|network) _values 'value' on off ;;
          notifications) _values 'value' off attention finished ;;
          restore) _values 'value' ask auto ;;
          updates) _values 'value' notify auto off ;;
        esac
      fi ;;
    status)     _agentctl_args '--json[print it as JSON]' ;;
    cpu)
      if (( CURRENT == 3 )); then _values 'cpu' 'limit[the CPU guard'"'"'s limit for now]' 'schedule[the week'"'"'s schedule]' '--top[also list who uses the CPU]' '--json[print it as JSON]'
      elif [[ ${words[3]} == limit ]]; then
        if (( CURRENT == 4 )); then _values 'limit' 60 65 70 75 80 85 90 95 'schedule[back to the schedule]'; else _values 'option' '--for[how long, such as 3h]'; fi
      fi ;;
    serve)      _agentctl_args '--demo[simulated fleet from the fixture]' '--fixture[with --demo, the fleet from a dataset file]:file:_files' '--log[append the log to a file]:file:_files' ;;
    completion) (( CURRENT == 3 )) && _values 'shell' zsh ;;
    set)
      if (( CURRENT == 3 )); then _values 'setting' 'term[how focus shows an agent]' 'mouse[who owns the mouse in agent terminals]' 'restore[what happens to agents lost with tmux]' 'hat[the hat of an agent'"'"'s working folder]'
      elif [[ ${words[3]} == hat ]]; then
        if (( CURRENT == 4 )); then _alternative 'agents:agent:_agentctl_agents' 'folders:folder:_files -/'
        elif (( CURRENT == 5 )); then _values 'hat' auto none hard-hat cap beanie helmet wizard beret chef crown headphones propeller top-hat cowboy bandana
        elif (( CURRENT == 6 )); then _values 'colour' teal blue yellow green purple pink sky ink
        fi
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

// completeCmd prints the lines the zsh completion offers (see
// _agentctl_values). It must be fast and silent: errors just mean no
// suggestions.
func completeCmd(args []string) {
	if len(args) != 1 {
		return
	}
	switch args[0] {
	case "accounts":
		for _, a := range accounts.Configured() {
			compLine(os.Stdout, a.Name, a.Name+"  -- "+shortHome(a.Path()), false)
		}
	case "devices":
		var res struct{ Devices []devices.Device }
		if call("GET", "/local/devices", nil, &res) == nil {
			for _, d := range res.Devices {
				compLine(os.Stdout, d.Name, d.Name+"  -- "+string(d.Kind)+" · "+string(d.Scope), false)
			}
		}
	case "agents":
		s, err := state()
		if err != nil {
			return
		}
		ts := boardTargets(s)
		byActive(ts)
		writeCompletions(os.Stdout, ts)
	case "bring", "resumable":
		ts, err := bringCandidates(true)
		if err != nil {
			return
		}
		writeCompletions(os.Stdout, clipSessions(ts, 40))
	}
}

// clipSessions keeps every agent and the first n sessions.
func clipSessions(ts []target, n int) []target {
	out := ts[:0:0]
	for _, t := range ts {
		if t.Session != nil {
			if n == 0 {
				continue
			}
			n--
		}
		out = append(out, t)
	}
	return out
}

// compLine writes one completion line: the value zsh inserts, what the list
// shows for it, and whether it is hidden (typed, not listed).
func compLine(w io.Writer, value, shown string, hidden bool) {
	h := "0"
	if hidden {
		h = "1"
	}
	flat := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	fmt.Fprintf(w, "%s\t%s\t%s\n", oneLine(value), flat.Replace(shown), h)
}

// writeCompletions lists each target once, as its name (zsh quotes it when
// it inserts one: spaces, quotes), with its id and a hint that tells agents
// apart: state, last active, folder. Its id is offered too, hidden. A name
// that two targets share is offered as the id instead, so what zsh inserts
// names one target.
func writeCompletions(w io.Writer, ts []target) {
	count := map[string]int{}
	width := 0
	for _, t := range ts {
		count[strings.ToLower(t.Name)]++
		width = max(width, len([]rune(clip(t.Name, 40))))
	}
	for _, t := range ts {
		value := t.Name
		if value == "" || count[strings.ToLower(value)] > 1 {
			value = t.shortID()
		}
		name := clip(t.Name, 40)
		shown := name + strings.Repeat(" ", width-len([]rune(name))) + "  " + fmt.Sprintf("%-8s", t.shortID()) + "  -- " + completionHint(t)
		compLine(w, value, shown, false)
		if value != t.shortID() {
			compLine(w, t.shortID(), "", true)
		}
	}
}

// completionHint tells agents apart in the completion list.
func completionHint(t target) string {
	active := activeAgo(t.Active)
	if active == "-" {
		active = "active: not known"
	} else {
		active = "active " + active
	}
	return t.state() + " · " + active + " · " + t.Folder
}
