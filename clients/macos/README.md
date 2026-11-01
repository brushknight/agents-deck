# agents deck menu bar and tray app

The deck from the desk panel, in the macOS menu bar or the Linux system tray. Click the critter icon
and the deck opens next to it; the icon turns orange when an agent needs you. It talks to `agentctl`
on the same computer over the daemon's unix socket (`~/.local/share/agents-terminal/agentd.sock`, or
under `$AGENTSTERM_HOME`), so nothing crosses the network.

One Flutter app for both: `lib/main.dart` draws the deck with
[`clients/flutter/agents_ui`](../flutter/agents_ui), and a small native shell per platform owns the
icon and the window. The two shells speak the same method channel, `agents_deck/menubar`:

| call | direction | what |
|---|---|---|
| `attention(n)` | Dart → shell | how many agents need you; 0 is the plain icon, more turns it orange |
| `hide` | Dart → shell | Esc with no card open puts the deck away |
| `visible(bool)` | shell → Dart | the deck was shown or hidden |

## macOS

`macos/Runner/AppDelegate.swift`: a status item, a borderless window pinned under the menu bar, no
Dock icon. Install with `./install.sh --app` (from a release) or `./install.sh --source --app` (from a
clone, needs Flutter 3.44+). Right-click the icon for the web dashboard and Quit.

```bash
flutter build macos --release   # build/macos/Build/Products/Release/agents deck.app
```

## Linux

`linux/runner/`: a GTK shell (`my_application.cc`) and the tray icon (`tray.cc`), a
[StatusNotifierItem](https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/) with its
menu over D-Bus. It uses no tray plugin: the AppIndicator libraries behind the Flutter tray plugins
keep left clicks for the menu, and here a click opens the deck.

- **Click** the icon: the deck opens next to it, under a top panel or above a bottom one. Click again,
  press Esc or click anywhere else to put it away.
- **The icon's menu** (right-click on most desktops): Show the deck, Open dashboard in browser
  (`agentctl web`), Quit agents deck.
- **Run `agents-deck` again** (or pick it in your app menu) while it runs: the deck opens. That's also
  the way in on a desktop without a tray, where the deck opens by itself at start.

### Where it works

| desktop | tray icon |
|---|---|
| KDE Plasma, XFCE, Cinnamon, MATE | shown as is |
| GNOME | needs the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/) (`gnome-shell-extension-appindicator`; Ubuntu has it on) |
| others | needs a panel or bar with StatusNotifierItem support (waybar's `tray`, for one) |

The app runs on X11 even in a Wayland session (through XWayland), because Wayland doesn't let an app
put its own window next to the tray icon. Set `GDK_BACKEND=wayland` to run it natively; the
compositor then decides where the deck appears. A panel that only ever opens the menu, never sends
the click, still has **Show the deck** in it.

### Install

From a clone, with Go 1.24+, Flutter 3.44+ and Flutter's Linux build tools
(`sudo apt-get install clang cmake ninja-build pkg-config libgtk-3-dev` on Debian and Ubuntu):

```bash
./install.sh --source --app               # agentctl, its systemd user service, and the tray app
./install.sh --source --app --autostart   # and start the tray app at login
```

That puts the app in `~/.local/share/agents-deck/app`, links `agents-deck` into `~/.local/bin`, adds
`~/.local/share/applications/dev.agents-deck.menubar.desktop` to your app menu and starts it. With
`--autostart` the same entry goes to `~/.config/autostart/`, with `--background` (wait quietly for
the tray, which may come up after the app at login); delete that file to stop it starting at login.

### Build and test

```bash
flutter build linux --release   # build/linux/<arch>/release/bundle/agents-deck
xvfb-run -a dbus-run-session -- linux/test/smoke.sh
```

The smoke test runs the app headless against a pretend tray and a pretend daemon (the fixture fleet in
`docs/fixtures/state.json`) and checks the icon, its menu, the deck window, Esc, a second start and
Quit. It needs `xvfb`, `xdotool`, `python3-gi` and `gdbus`. CI runs it on every push.

## Tests

```bash
flutter analyze && flutter test
flutter test --update-goldens   # redraws test/goldens/ (the golden tests only run then)
```
