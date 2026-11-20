# Drives agentctl's zsh completion in a real interactive zsh, without a
# terminal: completion_test.go runs it with a built agentctl first on PATH
# (talking to a demo service).
#
#   zsh -f complete.zsh <out file> <command line>...
#
# For each command line it types the line, presses Tab once, and writes to
# <out file>: "BUF <the line after Tab>", then "CAND <match>" for every
# match zsh listed (after its own matching against what was typed), in the
# order offered, "HIDDEN <match>" for those it takes but doesn't list, then
# "END".
zmodload zsh/zpty || exit 1
out=$1; shift
rc=${out}.rc
: > $out
cat > $rc <<RC
PS1='> '
unsetopt beep
bindkey -e
autoload -Uz compinit
compinit -u -d ${out}.compdump
source <(agentctl completion zsh)
typeset -ga __cands
# Every match zsh would add, after matching: compadd -O stores them.
compadd() {
  local -a m
  # Calls that only filter or collect (-O, -A, -D) add nothing: pass them on.
  if (( \${@[(I)-[OAD]]} )); then builtin compadd "\$@"; return; fi
  builtin compadd -O m "\$@"
  [[ -n \$AGENTCTL_COMPLETE_DEBUG ]] && print -r -- "ARGS \${(q)@}" >> $out
  # Hidden matches (-n) can be typed but aren't listed.
  if (( \${@[(I)-n]} )); then __cands+=("\${m[@]/#/HIDDEN }"); else __cands+=("\${m[@]/#/CAND }"); fi
  builtin compadd "\$@"
}
dumpbuf() {
  print -r -- "BUF \$BUFFER" >> $out
  local c
  for c in "\${__cands[@]}"; do print -r -- "\$c" >> $out; done
  print -r -- END >> $out
  __cands=()
}
zle -N dumpbuf
bindkey '^X^D' dumpbuf
# Tab, saying when the completion is over (insertion included).
tab() {
  zle complete-word
  print -r -- TABDONE >> $out
}
zle -N tab
bindkey '^I' tab
print -r -- READY >> $out
RC

zpty z zsh -f -i || exit 1
drain() { local c; while zpty -r -t z c 2>/dev/null; do :; done }
# wait waits until the out file has n lines saying $1.
wait_for() {
  local i
  for i in {1..100}; do
    (( $(grep -c "^$1" $out) >= $2 )) && return 0
    sleep 0.1; drain
  done
  print -u2 "complete.zsh: timed out waiting for $1"
  return 1
}
zpty -w z "source $rc"
wait_for READY 1 || exit 1
n=0
for line in "$@"; do
  (( n++ ))
  zpty -w -n z "$line"; sleep 0.2; drain
  zpty -w -n z $'\t'
  wait_for TABDONE $n || exit 1
  zpty -w -n z $'\x18\x04'
  wait_for END $n || exit 1
  zpty -w -n z $'\x15'; sleep 0.2; drain
done
zpty -d z
