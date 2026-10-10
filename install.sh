#!/bin/sh
# agents deck installer: downloads a release from GitHub, checks it against
# the release's SHA256SUMS, installs agentctl to ~/.local/bin and starts the
# daemon (`agentctl install`). Add --app for the menu bar app (~/Applications).
#
#   curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --app            # also the menu bar app
#   curl -fsSL …/install.sh | sh -s -- --version v0.2.0
#   --no-daemon: install agentctl but don't (re)start its login item
set -eu

repo="brushknight/agents-deck"
version=""
app=0
daemon=1
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app=1 ;;
    --no-daemon) daemon=0 ;;
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

if [ -z "$version" ] && [ -z "${AGENTS_DECK_BASE_URL:-}" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$version" ] || { echo "no release found" >&2; exit 1; }
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

case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) echo "add ~/.local/bin to your PATH, e.g.: echo 'export PATH=\$HOME/.local/bin:\$PATH' >> ~/.zshrc" ;;
esac
echo "done · try: agentctl new"
