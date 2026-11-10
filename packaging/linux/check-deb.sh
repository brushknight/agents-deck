#!/bin/bash
# Installs an agents deck .deb on this (clean, disposable) machine and checks
# it: the files are where they belong, agentctl runs, the systemd user unit and
# the udev rule are valid, and removing the package takes it all away. Run as
# root or with sudo available. CI runs it on a fresh Ubuntu runner.
#
#   packaging/linux/check-deb.sh dist/agents-deck_0.2.0_linux_amd64.deb [expected version]
set -euo pipefail

deb="$1"
want="${2:-}"
SUDO=""
[ "$(id -u)" = 0 ] || SUDO="sudo"
fail() { echo "check-deb: $*" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
$SUDO apt-get install -y --no-install-recommends "$(cd "$(dirname "$deb")" && pwd)/$(basename "$deb")"

for f in /opt/agents-deck/agents-deck /opt/agents-deck/agentctl /usr/bin/agentctl /usr/bin/agents-deck \
  /usr/lib/systemd/user/agents-deck.service /usr/lib/udev/rules.d/70-agents-deck.rules \
  /usr/share/applications/dev.agents-deck.menubar.desktop \
  /usr/share/icons/hicolor/256x256/apps/dev.agents-deck.menubar.png /usr/share/doc/agents-deck/copyright; do
  [ -e "$f" ] || fail "missing $f"
done
[ "$(stat -c %U /opt/agents-deck/agentctl)" = root ] || fail "files should belong to root"

got="$(agentctl version)"
echo "agentctl version: $got"
[ -z "$want" ] || [ "$got" = "$want" ] || fail "agentctl version says $got, expected $want"

# The unit is valid for a user manager (systemd-analyze checks ExecStart exists).
if command -v systemd-analyze >/dev/null; then
  # It needs a runtime directory, which a CI shell or container may not have.
  XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-$(mktemp -d)}" systemd-analyze --user verify \
    /usr/lib/systemd/user/agents-deck.service
  echo "systemd user unit: valid"
else
  echo "no systemd-analyze: unit not verified"
fi
# udevadm verify is new in systemd 254.
if udevadm verify --help >/dev/null 2>&1; then
  udevadm verify --resolve-names=never /usr/lib/udev/rules.d/70-agents-deck.rules
  echo "udev rule: valid"
else
  echo "no udevadm verify: rule not checked"
fi
if command -v desktop-file-validate >/dev/null; then
  desktop-file-validate /usr/share/applications/dev.agents-deck.menubar.desktop
  echo "desktop entry: valid"
fi
# No system service: nothing of ours runs as root.
if systemctl list-unit-files 2>/dev/null | grep -q agents-deck; then
  fail "a system unit named agents-deck exists; the service must be per user"
fi

$SUDO apt-get remove -y agents-deck >/dev/null
[ ! -e /usr/bin/agentctl ] && [ ! -e /opt/agents-deck ] || fail "files left after removal"
echo "check-deb: ok"
