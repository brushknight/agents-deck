#!/bin/bash
# Packages the Linux tray app and agentctl for a release:
#
#   agents-deck_<version>_linux_<arch>.deb      for Debian and Ubuntu (dpkg-deb)
#   agents-deck_<version>_linux_<arch>.tar.gz   the app folder, for other distributions
#
#   packaging/linux/package.sh --bundle clients/macos/build/linux/x64/release/bundle \
#     --agentctl dist/agentctl --version 0.2.0 --arch amd64 --out dist
#
# Both hold:
#   /opt/agents-deck/                      the app (Flutter bundle) with agentctl next to it
#   /usr/bin/agentctl, /usr/bin/agents-deck
#   /usr/lib/systemd/user/agents-deck.service   the service, per user (never started as root)
#   /usr/lib/udev/rules.d/70-agents-deck.rules  the logged-in user may open the boards' serial ports
#   /usr/share/applications/dev.agents-deck.menubar.desktop, and its icon
#
# The archive is one folder, agents-deck/: the app with agentctl next to it, and
# in agents-deck/share/ the service, udev rule and desktop entry. install.sh
# puts that folder in ~/.local/share/agents-deck/app, and the updater swaps it
# in there (it expects exactly this: the app's folder, nothing around it).
set -euo pipefail

bundle="" agentctl="" version="" arch="" out=""
here="$(cd "$(dirname "$0")" && pwd)"

usage() { sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
while [ $# -gt 0 ]; do
  case "$1" in
    --bundle) bundle="$2"; shift ;;
    --agentctl) agentctl="$2"; shift ;;
    --version) version="$2"; shift ;;
    --arch) arch="$2"; shift ;;
    --out) out="$2"; shift ;;
    -h|--help) usage ;;
    *) echo "unknown option: $1" >&2; usage ;;
  esac
  shift
done
[ -d "$bundle" ] && [ -f "$agentctl" ] && [ -n "$version" ] && [ -n "$out" ] || usage
case "$arch" in amd64|arm64) ;; *) echo "--arch is amd64 or arm64" >&2; exit 2 ;; esac
[ -x "$bundle/agents-deck" ] || { echo "no agents-deck executable in $bundle" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "needs dpkg-deb (apt-get install dpkg, or brew install dpkg)" >&2; exit 1; }

# Debian versions: a pre-release (0.2.0-rc.1) sorts before its release with "~".
deb_version="${version//-/\~}"
base="agents-deck_${version}_linux_${arch}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
root="$work/root"

# Files, owned by root, readable by everyone.
umask 022
mkdir -p "$root/opt" "$root/usr/bin" "$root/usr/lib/systemd/user" "$root/usr/lib/udev/rules.d" \
  "$root/usr/share/applications" "$root/usr/share/icons/hicolor/256x256/apps" \
  "$root/usr/share/doc/agents-deck" "$root/usr/share/lintian/overrides"
cp -R "$bundle" "$root/opt/agents-deck"
install -m 0755 "$agentctl" "$root/opt/agents-deck/agentctl"
ln -s /opt/agents-deck/agentctl "$root/usr/bin/agentctl"
ln -s /opt/agents-deck/agents-deck "$root/usr/bin/agents-deck"
install -m 0644 "$here/agents-deck.service" "$root/usr/lib/systemd/user/agents-deck.service"
install -m 0644 "$here/70-agents-deck.rules" "$root/usr/lib/udev/rules.d/70-agents-deck.rules"
install -m 0644 "$here/dev.agents-deck.menubar.desktop" "$root/usr/share/applications/dev.agents-deck.menubar.desktop"
icon="$bundle/data/app_icon.png"
[ -f "$icon" ] || icon="$here/../../clients/macos/linux/runner/app_icon.png"
install -m 0644 "$icon" "$root/usr/share/icons/hicolor/256x256/apps/dev.agents-deck.menubar.png"
install -m 0644 "$here/copyright" "$root/usr/share/doc/agents-deck/copyright"
printf 'agents-deck (%s) unstable; urgency=medium\n\n  * Release %s: https://github.com/brushknight/agents-deck/releases\n\n -- agents deck <agents-deck@users.noreply.github.com>  %s\n' \
  "$deb_version" "$version" "$(LC_ALL=C date -u -R)" | gzip -9n > "$root/usr/share/doc/agents-deck/changelog.gz"
install -m 0644 "$here/lintian-overrides" "$root/usr/share/lintian/overrides/agents-deck"
chmod -R u+rwX,go+rX,go-w "$root"

# Dependencies: from the app's own libraries when dpkg-shlibdeps is at hand (on
# the Ubuntu build machine), else the GTK 3 the Flutter shell links.
depends="libgtk-3-0t64 | libgtk-3-0, libglib2.0-0t64 | libglib2.0-0, libc6"
if command -v dpkg-shlibdeps >/dev/null; then
  mapfile -t elfs < <(find "$root/opt/agents-deck" -type f \( -name '*.so*' -o -name agents-deck \) \
    -exec sh -c '[ "$(head -c 4 "$1" | tail -c 3)" = ELF ] && echo "$1"' _ {} \;)
  if [ "${#elfs[@]}" -gt 0 ]; then
    mkdir -p "$work/shlib/debian"
    printf 'Source: agents-deck\n\nPackage: agents-deck\nArchitecture: any\n' > "$work/shlib/debian/control"
    if got="$(cd "$work/shlib" && dpkg-shlibdeps -O --ignore-missing-info -l"$root/opt/agents-deck/lib" "${elfs[@]}" 2>"$work/shlibdeps.err")"; then
      depends="${got#shlibs:Depends=}"
    else
      echo "dpkg-shlibdeps failed, using the default Depends:" >&2
      cat "$work/shlibdeps.err" >&2
    fi
  fi
fi
recommends="tmux, libwebkit2gtk-4.1-0, gnome-shell-extension-appindicator"
case "$depends" in *libwebkit2gtk-4.1*) recommends="tmux, gnome-shell-extension-appindicator" ;; esac

installed_size="$(du -sk "$root" | cut -f1)"
mkdir -p "$root/DEBIAN"
cat > "$root/DEBIAN/control" <<EOF
Package: agents-deck
Version: $deb_version
Architecture: $arch
Maintainer: agents deck <agents-deck@users.noreply.github.com>
Installed-Size: $installed_size
Depends: $depends
Recommends: $recommends
Section: devel
Priority: optional
Homepage: https://github.com/brushknight/agents-deck
Description: your coding agents on a deck: tray app, service and boards
 agents deck shows the coding agents (Claude Code, Codex) running on this
 computer on a 4x4 deck: in the system tray, in the browser and on small
 boards over USB.
 .
 This package holds the tray app, agentctl (the service and its command
 line), a systemd user service and a udev rule that lets the logged-in
 user open the boards' USB serial ports.
EOF
install -m 0755 "$here/postinst" "$root/DEBIAN/postinst"
install -m 0755 "$here/postrm" "$root/DEBIAN/postrm"

rm -f "$out/$base.deb" "$out/$base.tar.gz"
dpkg-deb --root-owner-group -Zxz --build "$root" "$out/$base.deb" >/dev/null
# The archive: the app folder, with the system files in share/ for whoever
# installs it by hand on another distribution.
arc="$work/archive/agents-deck"
mkdir -p "$work/archive"
cp -R "$root/opt/agents-deck" "$arc"
mkdir -p "$arc/share"
cp "$here/agents-deck.service" "$here/70-agents-deck.rules" "$here/dev.agents-deck.menubar.desktop" "$arc/share/"
tar --owner=0 --group=0 --numeric-owner -C "$work/archive" -czf "$out/$base.tar.gz" agents-deck 2>/dev/null ||
  tar -C "$work/archive" -czf "$out/$base.tar.gz" agents-deck # bsdtar (macOS) has no --owner
dpkg-deb --info "$out/$base.deb" | sed -n '/Package:/,$p'
ls -la "$out/$base.deb" "$out/$base.tar.gz"
