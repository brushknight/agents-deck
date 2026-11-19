#!/bin/sh
# agents deck installer: downloads a release from GitHub, checks it against
# the release's SHA256SUMS (and the release signature, when an agentctl already
# installed here can check it), installs agentctl to ~/.local/bin and starts the
# service (launchd on macOS, a systemd user service on Linux). Add --app for the
# menu bar app (~/Applications on macOS; on Linux the system tray app, in
# ~/.local/share/agents-deck). In a clone, --source builds from code.
#
# Linux: release archives for x86_64 and arm64. When a release has none (older
# releases), a clone builds from source instead. --app from source needs
# Flutter and GTK 3 build tools; add --autostart to start the tray app at login.
#
#   curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --app            # also the menu bar app
#   curl -fsSL …/install.sh | sh -s -- --version v0.2.0
#   ./install.sh --source --app                         # from a clone (needs Go; Flutter for --app)
#   ./install.sh --app --autostart                      # Linux: also start the tray app at login
#   --no-daemon: install agentctl but don't (re)start its login item
set -eu

repo="brushknight/agents-deck"
# The release signing key (base64), pinned here once the project has one. With
# it, an agentctl already installed here checks SHA256SUMS.sig with this key;
# without it, with the key built into that agentctl.
release_pub=""
version=""
app=0
autostart=0
daemon=1
source=0
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app=1 ;;
    --autostart) autostart=1 ;;
    --no-daemon) daemon=0 ;;
    --source) source=1 ;;
    --version) version="$2"; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

os="$(uname -s)"; arch="$(uname -m)"
case "$os/$arch" in
  Darwin/arm64)          target="darwin_arm64"; goarch=arm64 ;;
  Darwin/x86_64)         target="darwin_amd64"; goarch=amd64 ;;
  Linux/x86_64)          target="linux_amd64"; goarch=amd64 ;;
  Linux/aarch64|Linux/arm64) target="linux_arm64"; goarch=arm64 ;;
  *) echo "agents deck has no build for $os/$arch (macOS, or Linux x86_64/arm64)" >&2; exit 1 ;;
esac

if [ "$autostart" = 1 ] && [ "$os" = Darwin ]; then
  echo "note: --autostart is for the Linux tray app; on macOS add agents deck in System Settings > General > Login Items"
fi

clone="$(cd "$(dirname "$0")" 2>/dev/null && pwd)"
in_clone() { [ -f "$clone/backend/go.mod" ]; }

# What the deck runs agents with: worth a word, not a stop.
command -v tmux >/dev/null 2>&1 || echo "note: agentctl runs agents in tmux; install it (brew install tmux, or your package manager)"
command -v claude >/dev/null 2>&1 || echo "note: no claude on PATH; install Claude Code to start agents with agentctl new"

finish() {
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) echo "add ~/.local/bin to your PATH, e.g.: echo 'export PATH=\$HOME/.local/bin:\$PATH' >> ~/.zshrc" ;;
  esac
  echo "done · try: agentctl new"
}

# Register the service to run at login. agentctl install does it on every
# platform where it knows how (all of them since it has `agentctl status`);
# an older agentctl on Linux gets the systemd user unit written here.
setup_daemon() {
  bin="$HOME/.local/bin/agentctl"
  if [ "$os" = Darwin ]; then
    "$bin" install
    return
  fi
  if ! "$bin" status 2>&1 | grep -q 'unknown command "status"'; then
    "$bin" install
    return
  fi
  # Linux, older agentctl: systemd user service, best effort.
  if command -v systemctl >/dev/null 2>&1; then
    unit_dir="$HOME/.config/systemd/user"
    mkdir -p "$unit_dir"
    cat > "$unit_dir/agents-deck.service" <<EOF
[Unit]
Description=agents deck daemon
After=default.target

[Service]
ExecStart=$bin serve
Restart=on-failure
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload 2>/dev/null || true
    if systemctl --user enable --now agents-deck.service 2>/dev/null; then
      echo "  daemon → systemd user service 'agents-deck' (running)"
    else
      echo "  systemd user unit written; start it with:"
      echo "    systemctl --user enable --now agents-deck.service"
      echo "  (or just run it in a terminal: agentctl serve)"
    fi
  else
    echo "  no systemd found: start the daemon yourself with agentctl serve"
  fi
}

# The Linux tray app: the same Flutter app as the macOS one, built for GTK, in
# ~/.local/share/agents-deck/app, with a launcher in the app menu, `agents-deck`
# in ~/.local/bin, and with --autostart an XDG autostart entry. $1 is the app's
# folder (a release's, or a fresh build).
dest="$HOME/.local/share/agents-deck/app"
place_linux_app() {
  bundle="$1"
  [ -x "$bundle/agents-deck" ] || { echo "no tray app in $bundle" >&2; exit 1; }
  pkill -x agents-deck 2>/dev/null || true # the old copy, if it's running
  rm -rf "$dest"
  mkdir -p "$(dirname "$dest")" "$HOME/.local/bin" "$HOME/.local/share/applications"
  cp -R "$bundle" "$dest"
  ln -sf "$dest/agents-deck" "$HOME/.local/bin/agents-deck"
  desktop="$HOME/.local/share/applications/dev.agents-deck.menubar.desktop"
  write_desktop_entry > "$desktop"
  echo "  tray app → ~/.local/share/agents-deck/app (agents-deck, and in your app menu)"
  autostart_file="$HOME/.config/autostart/dev.agents-deck.menubar.desktop"
  if [ "$autostart" = 1 ]; then
    mkdir -p "$HOME/.config/autostart"
    # --background: at login the tray may come up after the app; wait for it.
    { write_desktop_entry | sed 's|^Exec=.*|& --background|'; echo "X-GNOME-Autostart-enabled=true"; } > "$autostart_file"
    echo "  starts at login → ~/.config/autostart/dev.agents-deck.menubar.desktop"
  elif [ -f "$autostart_file" ]; then
    echo "  still starts at login: remove ~/.config/autostart/dev.agents-deck.menubar.desktop to stop that"
  fi
  if [ -f "$dest/share/70-agents-deck.rules" ] && [ ! -f /usr/lib/udev/rules.d/70-agents-deck.rules ] &&
    [ ! -f /etc/udev/rules.d/70-agents-deck.rules ]; then
    echo "  boards over USB: to open their ports without joining the dialout group, run once:"
    echo "    sudo cp $dest/share/70-agents-deck.rules /etc/udev/rules.d/ && sudo udevadm control --reload-rules"
  fi
  if [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
    (nohup "$dest/agents-deck" >/dev/null 2>&1 &)
  fi
  case "${XDG_CURRENT_DESKTOP:-}" in
    *GNOME*) echo "note: GNOME shows tray icons only with the AppIndicator extension (gnome-shell-extension-appindicator; Ubuntu has it on)" ;;
  esac
}

build_linux_app() {
  command -v flutter >/dev/null 2>&1 || {
    echo "--app needs Flutter 3.44+ (https://docs.flutter.dev/get-started/install/linux)" >&2
    echo "  and its Linux build tools, e.g.: sudo apt-get install clang cmake ninja-build pkg-config libgtk-3-dev" >&2
    exit 1
  }
  (cd "$root/clients/macos" && flutter pub get >/dev/null && flutter build linux --release) || {
    echo "the tray app didn't build; Flutter's Linux build needs: clang cmake ninja-build pkg-config libgtk-3-dev" >&2
    exit 1
  }
  for bundle in "$root"/clients/macos/build/linux/*/release/bundle; do
    place_linux_app "$bundle"
    return
  done
}

write_desktop_entry() {
  cat <<DESKTOP
[Desktop Entry]
Type=Application
Name=agents deck
Comment=Your coding agents in the system tray
Exec=$dest/agents-deck
Icon=$dest/data/app_icon.png
Terminal=false
Categories=Development;
StartupNotify=false
StartupWMClass=dev.agents-deck.menubar
DESKTOP
}

from_source() {
  root="$clone"
  in_clone || { echo "--source runs from a clone of the repo: ./install.sh --source" >&2; exit 1; }
  command -v go >/dev/null 2>&1 || { echo "--source needs Go 1.24+ (brew install go, or go.dev/dl)" >&2; exit 1; }
  v="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)"
  echo "agents deck from source ($v)"
  mkdir -p "$HOME/.local/bin"
  (cd "$root/backend" && go build -ldflags "-X main.version=$v" -o "$HOME/.local/bin/agentctl" ./cmd/agentctl)
  echo "  agentctl → ~/.local/bin/agentctl"
  if [ "$daemon" = 1 ]; then setup_daemon; fi
  if [ "$app" = 1 ]; then
    if [ "$os" != Darwin ]; then
      build_linux_app
    else
      command -v flutter >/dev/null 2>&1 || { echo "--app needs Flutter 3.44+: brew install --cask flutter" >&2; exit 1; }
      (cd "$root/clients/macos" && flutter pub get >/dev/null && flutter build macos --release)
      mkdir -p "$HOME/Applications"
      rm -rf "$HOME/Applications/agents deck.app"
      ditto "$root/clients/macos/build/macos/Build/Products/Release/agents deck.app" "$HOME/Applications/agents deck.app"
      echo "  menu bar app → ~/Applications/agents deck.app"
      open "$HOME/Applications/agents deck.app"
    fi
  fi
  finish
  exit 0
}

if [ "$source" = 1 ]; then from_source; fi

# Linux releases before v0.2.0 have no archives: a clone builds instead.
no_release() {
  if [ "$os" = Linux ] && in_clone; then
    echo "$1: building from source instead."
    from_source
  fi
  echo "$1. Install from a clone instead:" >&2
  echo "  git clone https://github.com/$repo && cd agents-deck && ./install.sh --source" >&2
  exit 1
}

if [ -z "$version" ] && [ -z "${AGENTS_DECK_BASE_URL:-}" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)" || true
  [ -n "$version" ] || no_release "no release found"
fi
v="${version#v}"
# AGENTS_DECK_BASE_URL points at another copy of the release (testing).
base="${AGENTS_DECK_BASE_URL:-https://github.com/$repo/releases/download/$version}"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fetch() { curl -fsSL -o "$tmp/$1" "$base/$1"; }
# sha256sum (coreutils) on Linux, where shasum (Perl) is often missing; shasum on macOS.
if command -v sha256sum >/dev/null 2>&1; then sha256check="sha256sum -c -"; else sha256check="shasum -a 256 -c -"; fi
verify() { # the file's line from SHA256SUMS must match
  (cd "$tmp" && grep "  $1\$" SHA256SUMS | $sha256check) >/dev/null ||
    { echo "checksum mismatch for $1: not installing" >&2; exit 1; }
}

# The release signature on SHA256SUMS, checked by an agentctl already installed
# here (a fresh download can't vouch for itself). Without one, the checksums
# over HTTPS are the check, as before.
verify_signature() {
  checker=""
  for c in "$HOME/.local/bin/agentctl" "$(command -v agentctl 2>/dev/null || true)"; do
    [ -n "$c" ] && [ -x "$c" ] || continue
    if ! "$c" release-key 2>&1 | grep -q 'unknown command'; then checker="$c"; break; fi
  done
  if [ -z "$checker" ]; then
    echo "  signature: not checked (no installed agentctl that checks signatures yet)"
    return
  fi
  if ! fetch SHA256SUMS.sig 2>/dev/null; then
    if [ -n "$release_pub" ]; then echo "this release isn't signed: not installing" >&2; exit 1; fi
    echo "  signature: this release has none"
    return
  fi
  if [ -n "$release_pub" ]; then
    out="$("$checker" release-key verify "$tmp/SHA256SUMS" --pub "$release_pub" 2>&1)" ||
      { echo "$out" >&2; echo "the release signature doesn't match: not installing" >&2; exit 1; }
  elif ! out="$("$checker" release-key verify "$tmp/SHA256SUMS" 2>&1)"; then
    case "$out" in
      *"has no release key"*) echo "  signature: not checked (the installed agentctl has no release key)"; return ;;
    esac
    echo "$out" >&2; echo "the release signature doesn't match: not installing" >&2; exit 1
  fi
  echo "  signature: ok"
}

echo "agents deck $version"
fetch SHA256SUMS || no_release "no release $version at $base"
verify_signature
cli="agentctl_${v}_${target}.tar.gz"
grep -q "  $cli\$" "$tmp/SHA256SUMS" || no_release "release $version has no agentctl for $target"
fetch "$cli"; verify "$cli"
mkdir -p "$HOME/.local/bin" "$tmp/cli"
tar -xzf "$tmp/$cli" -C "$tmp/cli"
install -m 0755 "$tmp/cli/agentctl" "$HOME/.local/bin/agentctl"
echo "  agentctl → ~/.local/bin/agentctl"
if [ "$daemon" = 1 ]; then setup_daemon; fi

if [ "$app" = 1 ]; then
  if [ "$os" = Darwin ]; then
    zip="agents-deck_${v}_macos_${goarch}.zip"
    fetch "$zip" || { echo "release $version has no menu bar app for $goarch" >&2; exit 1; }
    verify "$zip"
    mkdir -p "$HOME/Applications"
    rm -rf "$HOME/Applications/agents deck.app"
    ditto -x -k "$tmp/$zip" "$HOME/Applications"
    echo "  menu bar app → ~/Applications/agents deck.app"
    open "$HOME/Applications/agents deck.app"
  else
    pkg="agents-deck_${v}_linux_${goarch}.tar.gz"
    if ! grep -q "  $pkg\$" "$tmp/SHA256SUMS"; then
      if in_clone; then root="$clone"; build_linux_app; finish; exit 0; fi
      echo "release $version has no tray app for $goarch: install it from a clone (./install.sh --source --app)" >&2
      exit 1
    fi
    fetch "$pkg"; verify "$pkg"
    mkdir -p "$tmp/app"
    tar -xzf "$tmp/$pkg" -C "$tmp/app"
    place_linux_app "$tmp/app/agents-deck"
  fi
fi

finish
