#!/bin/sh
# agents deck installer: downloads a release from GitHub, checks it against
# the release's SHA256SUMS, installs agentctl to ~/.local/bin and starts the
# daemon (`agentctl install`). Add --app for the menu bar app (~/Applications).
# In a clone of the repo, --source builds both from the code instead.
#
#   curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --app            # also the menu bar app
#   curl -fsSL …/install.sh | sh -s -- --version v0.2.0
#   ./install.sh --source --app                         # from a clone (needs Go; Flutter for --app)
#   --no-daemon: install agentctl but don't (re)start its login item
set -eu

repo="brushknight/agents-deck"
version=""
app=0
daemon=1
source=0
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app=1 ;;
    --no-daemon) daemon=0 ;;
    --source) source=1 ;;
    --version) version="$2"; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

os="$(uname -s)"; arch="$(uname -m)"
case "$os/$arch" in
  Darwin/arm64) target="darwin_arm64" ;;
  *) echo "agents deck has no release build for $os/$arch yet (macOS on Apple Silicon only, for now)" >&2; exit 1 ;;
esac

# What the deck runs agents with: worth a word, not a stop.
command -v tmux >/dev/null 2>&1 || echo "note: agentctl runs agents in tmux; install it with: brew install tmux"
command -v claude >/dev/null 2>&1 || echo "note: no claude on PATH; install Claude Code to start agents with agentctl new"

finish() {
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) echo "add ~/.local/bin to your PATH, e.g.: echo 'export PATH=\$HOME/.local/bin:\$PATH' >> ~/.zshrc" ;;
  esac
  echo "done · try: agentctl new"
}

if [ "$source" = 1 ]; then
  root="$(cd "$(dirname "$0")" && pwd)"
  [ -f "$root/backend/go.mod" ] || { echo "--source runs from a clone of the repo: ./install.sh --source" >&2; exit 1; }
  command -v go >/dev/null 2>&1 || { echo "--source needs Go 1.24+: brew install go" >&2; exit 1; }
  v="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)"
  echo "agents deck from source ($v)"
  mkdir -p "$HOME/.local/bin"
  (cd "$root/backend" && go build -ldflags "-X main.version=$v" -o "$HOME/.local/bin/agentctl" ./cmd/agentctl)
  echo "  agentctl → ~/.local/bin/agentctl"
  if [ "$daemon" = 1 ]; then "$HOME/.local/bin/agentctl" install; fi
  if [ "$app" = 1 ]; then
    command -v flutter >/dev/null 2>&1 || { echo "--app needs Flutter 3.44+: brew install --cask flutter" >&2; exit 1; }
    (cd "$root/clients/macos" && flutter pub get >/dev/null && flutter build macos --release)
    mkdir -p "$HOME/Applications"
    rm -rf "$HOME/Applications/agents deck.app"
    ditto "$root/clients/macos/build/macos/Build/Products/Release/agents deck.app" "$HOME/Applications/agents deck.app"
    echo "  menu bar app → ~/Applications/agents deck.app"
    open "$HOME/Applications/agents deck.app"
  fi
  finish
  exit 0
fi

if [ -z "$version" ] && [ -z "${AGENTS_DECK_BASE_URL:-}" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$version" ] || { echo "no release found: install from a clone instead (git clone https://github.com/$repo && cd agents-deck && ./install.sh --source)" >&2; exit 1; }
fi
v="${version#v}"
# AGENTS_DECK_BASE_URL points at another copy of the release (testing).
base="${AGENTS_DECK_BASE_URL:-https://github.com/$repo/releases/download/$version}"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fetch() { curl -fsSL -o "$tmp/$1" "$base/$1"; }
verify() { # the file's line from SHA256SUMS must match
  (cd "$tmp" && grep "  $1\$" SHA256SUMS | shasum -a 256 -c -) >/dev/null ||
    { echo "checksum mismatch for $1: not installing" >&2; exit 1; }
}

echo "agents deck $version"
fetch SHA256SUMS
cli="agentctl_${v}_${target}.tar.gz"
fetch "$cli"; verify "$cli"
mkdir -p "$HOME/.local/bin"
tar -xzf "$tmp/$cli" -C "$tmp"
install -m 0755 "$tmp/agentctl" "$HOME/.local/bin/agentctl"
echo "  agentctl → ~/.local/bin/agentctl"
if [ "$daemon" = 1 ]; then "$HOME/.local/bin/agentctl" install; fi

if [ "$app" = 1 ]; then
  zip="agents-deck_${v}_macos_arm64.zip"
  fetch "$zip"; verify "$zip"
  mkdir -p "$HOME/Applications"
  rm -rf "$HOME/Applications/agents deck.app"
  ditto -x -k "$tmp/$zip" "$HOME/Applications"
  echo "  menu bar app → ~/Applications/agents deck.app"
  open "$HOME/Applications/agents deck.app"
fi

finish
