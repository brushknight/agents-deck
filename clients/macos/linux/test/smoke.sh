#!/bin/bash
# Smoke test for the Linux tray app, headless: a fake tray (StatusNotifierWatcher)
# and a fake daemon (the fixture fleet on a unix socket), then the icon, its
# menu and the deck window are checked over D-Bus and X11.
#
# Needs a built bundle (flutter build linux --release in clients/macos), and
# xvfb, xdotool, python3-gi and gdbus. Run from anywhere:
#   xvfb-run -a dbus-run-session -- clients/macos/linux/test/smoke.sh
set -u
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../../../.." && pwd)"
app="$(ls -d "$here"/../../build/linux/*/release/bundle/agents-deck 2>/dev/null | head -1)"
[ -x "$app" ] || { echo "no bundle: run flutter build linux --release in clients/macos" >&2; exit 1; }

tmp="$(mktemp -d)"
export HOME="$tmp/home" AGENTSTERM_HOME="$tmp/data"
mkdir -p "$HOME" "$AGENTSTERM_HOME"
pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null; pkill -x agents-deck 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

fail=0
check() {
  if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi
}
visible() { xdotool search --onlyvisible --name '^agents deck$' >/dev/null 2>&1; }
# Waits up to $1 seconds for the condition in $2.
wait_for() {
  local i
  for ((i = 0; i < $1 * 10; i++)); do eval "$2" && return 0; sleep 0.1; done
  return 1
}

echo "-- without a tray, the deck opens by itself"
"$app" >"$tmp/app1.log" 2>&1 &
p=$!
sleep 1.5
check "hidden at first" "! visible"
check "shown when no tray turns up" "wait_for 6 visible"
check "says why" "grep -q 'no system tray' '$tmp/app1.log'"
kill $p; wait $p 2>/dev/null

echo "-- started at login (--background), it waits for the tray"
"$app" --background >"$tmp/app0.log" 2>&1 &
p=$!
sleep 4.5
check "still hidden" "! visible"
kill $p; wait $p 2>/dev/null

echo "-- with a tray and a daemon"
python3 "$here/fake_daemon.py" "$AGENTSTERM_HOME/agentd.sock" "$repo/docs/fixtures/state.json" >"$tmp/daemon.log" 2>&1 &
pids+=($!)
python3 "$here/fake_watcher.py" "$tmp/registered" >"$tmp/watcher.log" 2>&1 &
pids+=($!)
wait_for 5 "[ -S '$AGENTSTERM_HOME/agentd.sock' ]"
"$app" >"$tmp/app2.log" 2>&1 &
p=$!
check "the icon registers with the tray" "wait_for 10 \"grep -qs '^org.kde.StatusNotifierItem-' '$tmp/registered'\""
svc="$(head -1 "$tmp/registered" 2>/dev/null)"
check "the deck stays hidden" "! visible"
check "the app reads the daemon" "wait_for 10 \"grep -q 'sent state' '$tmp/daemon.log'\""
prop() {
  gdbus call --session -d "$svc" -o /StatusNotifierItem \
    -m org.freedesktop.DBus.Properties.Get org.kde.StatusNotifierItem "$1"
}
layout() {
  gdbus call --session -d "$svc" -o /MenuBar -m com.canonical.dbusmenu.GetLayout -- 0 -1 '[]'
}
# The fixture has agents waiting for an answer.
check "the icon asks for attention" "wait_for 5 \"prop Status | grep -q NeedsAttention\""
check "the tooltip counts them" "prop ToolTip | grep -q 'need'"
check "pixmaps at panel sizes" "prop IconPixmap | grep -q '(22, 22,'"
check "the menu has Quit" "layout | grep -q 'Quit agents deck'"
check "the menu has the dashboard" "layout | grep -q 'Open dashboard in browser'"

gdbus call --session -d "$svc" -o /StatusNotifierItem -m org.kde.StatusNotifierItem.Activate 100 0 >/dev/null
check "a click opens the deck" "wait_for 3 visible"
wid="$(xdotool search --onlyvisible --name '^agents deck$' | head -1)"
check "the deck is 440×440" "xdotool getwindowgeometry '$wid' | grep -q 'Geometry: 440x440'"
xdotool windowfocus --sync "$wid" key Escape
check "Esc puts it away" "wait_for 3 '! visible'"
gdbus call --session -d "$svc" -o /StatusNotifierItem -m org.kde.StatusNotifierItem.Activate 100 0 >/dev/null
check "a click opens it again" "wait_for 3 visible"
gdbus call --session -d "$svc" -o /StatusNotifierItem -m org.kde.StatusNotifierItem.Activate 100 0 >/dev/null
check "another click puts it away" "wait_for 3 '! visible'"

"$app" >"$tmp/app3.log" 2>&1
check "starting it again opens the running deck" "wait_for 3 visible"
check "and leaves one copy running" "[ \"\$(pgrep -c -x agents-deck)\" = 1 ]"

gdbus call --session -d "$svc" -o /MenuBar -m com.canonical.dbusmenu.Event 4 clicked '<0>' 0 >/dev/null
check "Quit in the menu ends the app" "wait_for 3 '! kill -0 $p 2>/dev/null'"
wait $p
check "cleanly" "[ $? = 0 ]"

if [ "$fail" != 0 ]; then
  echo "-- app log"
  cat "$tmp/app2.log"
fi
exit $fail
