#!/bin/bash
# Packages the macOS app for a release: puts agentctl inside the app, signs it
# from the inside out, notarizes and staples it when Apple credentials are set,
# and writes the two files people and the updater download:
#
#   agents-deck_<version>_macos_<arch>.dmg   the app and a link to Applications
#   agents-deck_<version>_macos_<arch>.zip   the app alone (what the updater swaps in)
#
#   packaging/macos/package.sh --app "build/.../agents deck.app" --agentctl dist/agentctl \
#     --version 0.2.0 --out dist [--arch universal] [--identity "Developer ID Application: …"]
#
# Without --identity everything is signed ad hoc: it runs, but macOS asks before
# the first open. With it, every piece is signed with that identity, the hardened
# runtime and a secure timestamp. Notarization runs when --identity is given and
# APPLE_ID, APPLE_TEAM_ID and APPLE_APP_PASSWORD are set in the environment.
#
# The input app is not changed: the script works on a copy. Only Apple's own
# tools are used (codesign, ditto, hdiutil, notarytool, stapler).
set -euo pipefail

app="" agentctl="" version="" out="" arch="universal" identity="" entitlements=""
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
entitlements="$root/clients/macos/macos/Runner/Release.entitlements"

usage() { sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app="$2"; shift ;;
    --agentctl) agentctl="$2"; shift ;;
    --version) version="$2"; shift ;;
    --out) out="$2"; shift ;;
    --arch) arch="$2"; shift ;;
    --identity) identity="$2"; shift ;;
    --entitlements) entitlements="$2"; shift ;;
    -h|--help) usage ;;
    *) echo "unknown option: $1" >&2; usage ;;
  esac
  shift
done
[ -d "$app" ] && [ -f "$agentctl" ] && [ -n "$version" ] && [ -n "$out" ] || usage
[ -f "$entitlements" ] || { echo "no entitlements file at $entitlements" >&2; exit 1; }

name="agents deck"
base="agents-deck_${version}_macos_${arch}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
dest="$work/$name.app"
ditto "$app" "$dest"

if [ -n "$identity" ]; then
  sign_opts=(--force --sign "$identity" --options runtime --timestamp)
  echo "signing with $identity (hardened runtime, timestamp)"
else
  sign_opts=(--force --sign -)
  echo "no identity: ad-hoc signing (macOS will ask before the first open)"
fi
sign() { codesign "${sign_opts[@]}" "$@"; }

# The service, next to the app's own executable, where the app looks for it.
install -m 0755 "$agentctl" "$dest/Contents/MacOS/agentctl"
echo "agentctl: $(lipo -archs "$dest/Contents/MacOS/agentctl" 2>/dev/null || echo '?'), app: $(lipo -archs "$dest/Contents/MacOS/$name" 2>/dev/null || echo '?')"

# Inside out: nested code first, deepest first, then the app itself. Each piece
# gets its own signature, so the app's signature needs no --deep.
sign -i dev.agents-deck.agentctl "$dest/Contents/MacOS/agentctl"
while IFS= read -r -d '' item; do
  sign "$item"
done < <(find "$dest/Contents" -depth \( -name '*.dylib' -o -name '*.framework' -o -name '*.xpc' \
  -o -name '*.appex' -o -name '*.bundle' -o \( -name '*.app' -not -path "$dest" \) \) -print0)
# Any other executable in Contents/MacOS besides the app's own and agentctl.
while IFS= read -r -d '' item; do
  case "$item" in "$dest/Contents/MacOS/$name"|"$dest/Contents/MacOS/agentctl"|*.dylib) continue ;; esac
  sign "$item"
done < <(find "$dest/Contents/MacOS" -type f -perm -u+x -print0)
sign --entitlements "$entitlements" "$dest"
codesign --verify --deep --strict --verbose=2 "$dest"

notarize() { # notarize <file>: submit and wait; fails the build if Apple rejects it
  xcrun notarytool submit "$1" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" \
    --password "$APPLE_APP_PASSWORD" --wait --output-format json > "$work/notary.json" || true
  status="$(/usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("status",""))' "$work/notary.json" 2>/dev/null || true)"
  if [ "$status" != "Accepted" ]; then
    cat "$work/notary.json" >&2 || true
    id="$(/usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("id",""))' "$work/notary.json" 2>/dev/null || true)"
    [ -n "$id" ] && xcrun notarytool log "$id" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" \
      --password "$APPLE_APP_PASSWORD" >&2 || true
    echo "notarization of $(basename "$1") failed: $status" >&2
    exit 1
  fi
}
can_notarize=0
if [ -n "$identity" ] && [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ] && [ -n "${APPLE_APP_PASSWORD:-}" ]; then
  can_notarize=1
fi

if [ "$can_notarize" = 1 ]; then
  echo "notarizing the app"
  ditto -c -k --keepParent "$dest" "$work/notarize.zip"
  notarize "$work/notarize.zip"
  rm "$work/notarize.zip"
  xcrun stapler staple "$dest"
  xcrun stapler validate "$dest"
else
  echo "not notarizing (needs --identity and APPLE_ID, APPLE_TEAM_ID, APPLE_APP_PASSWORD)"
fi

# The zip the updater swaps in: the app, stapled when notarized.
rm -f "$out/$base.zip"
ditto -c -k --keepParent "$dest" "$out/$base.zip"

# The disk image: the app and a link to Applications, compressed.
stage="$work/dmg"
mkdir "$stage"
ditto "$dest" "$stage/$name.app"
ln -s /Applications "$stage/Applications"
rm -f "$out/$base.dmg"
hdiutil create -quiet -volname "$name" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$out/$base.dmg"
if [ -n "$identity" ]; then
  codesign --force --sign "$identity" --timestamp "$out/$base.dmg"
fi
if [ "$can_notarize" = 1 ]; then
  echo "notarizing the disk image"
  notarize "$out/$base.dmg"
  xcrun stapler staple "$out/$base.dmg"
  xcrun stapler validate "$out/$base.dmg"
fi
hdiutil verify -quiet "$out/$base.dmg"

ls -la "$out/$base.zip" "$out/$base.dmg"
