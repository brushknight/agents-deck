# agents deck menu bar and tray app

The deck from the desk panel, in the macOS menu bar or the Linux system tray. Click the critter icon
and the deck opens next to it; the icon turns orange when an agent needs you. It talks to `agentctl`
on the same computer over the daemon's unix socket (`~/.local/share/agents-terminal/agentd.sock`, or
under `$AGENTSTERM_HOME`), so nothing crosses the network.

One Flutter app for both: `lib/main.dart` draws the deck with
[`clients/flutter/agents_ui`](../flutter/agents_ui), and a small native shell per platform owns the
icon, the deck's window and the Settings window. The shells speak the same method channel,
`agents_deck/menubar`:

| call | direction | what |
|---|---|---|
| `attention(n)` | Dart → shell | how many agents need you; 0 is the plain icon, more turns it orange |
| `hide` | Dart → shell | Esc with no card open puts the deck away |
| `settings(url)` | Dart → shell | open the Settings window on `url`, a one-time login link to the dashboard on 127.0.0.1; the shell puts the deck away. It refuses (`bad-url`) anything that isn't `http://127.0.0.1:<port>` or `http://localhost:<port>` |
| `isPinned` | Dart → shell | whether the deck is pinned (remembered across launches); macOS only |
| `pin(bool)` | Dart → shell | the Pin button: pin or unpin the deck; macOS only |
| `drag` | Dart → shell | the pointer went down on the status line of a pinned deck: move the window with it; macOS only |
| `visible(bool)` | shell → Dart | the deck was shown or hidden |
| `pinned(bool)` | shell → Dart | "Keep the Deck on Top" in the icon's menu changed it |
| `openSettings(page)` | shell → Dart | "Settings…" in the icon's menu; Dart answers `true` once it has called `settings(url)`, `false` when the service didn't answer, and the shell then shows the deck (which says the service isn't running) |

## The status line

One row under the deck (`lib/status_line.dart`), 32 points high, so the deck's window is 440 × 472.
It says, in this order: whether the service is running, how many boards are online, and what wants
attention (a busy Mac, an update ready or available, a new board on USB, boards waiting for
approval, firmware updates). Each part opens Settings on its page (Troubleshoot, Boards, Agents,
Updates), the gear opens General. What doesn't fit becomes `+N`.

It comes from `GET /admin/v1/system` on the daemon's socket (`lib/service.dart`), every 10 s while
the deck is open, every minute while it isn't, at once when the deck opens or the event stream comes
up or goes down. A service from before that endpoint answers 404: the line then says only "running"
and the gear.

When nothing answers on the socket, the line says "not running" and the deck makes way for a card
that says so. If an `agentctl` sits next to the app's own executable (the packaged app, see
`packaging/`), the card has **Start**, which runs `agentctl install` and waits for the socket; the
app also does that by itself once when it starts and finds the service down. Without one, the card
says how to start it by hand.

## Settings

The gear, a part of the status line, or "Settings…" in the icon's menu open the Settings window:
the service's own Settings page in a web view. Dart asks the service for a one-time login code
(`POST /local/login-code`, as `agentctl web` does) and hands the shell
`http://127.0.0.1:<port>/login?code=…&next=%2Fsettings#<page>`, with the port from `localAddr` in
the service's `config.json`. A service that doesn't know `next` yet sends the login to its front page;
the window then goes on to `/settings#<page>` itself. The window loads nothing but that one
address: links elsewhere open in the default browser.

## macOS

`macos/Runner/AppDelegate.swift`: a status item, a borderless window pinned under the menu bar, no
Dock icon. Right-click the icon for Settings, the web dashboard and Quit.

**Pin** in the status line (or "Keep the Deck on Top" in the icon's menu) keeps the deck open above
other apps' windows (window level `.floating`, below menus) when you click elsewhere. Drag the status
line to move it; it stays there while it's pinned. Click **Pinned** to make it a drop-down again.
The choice is kept in the app's defaults (`deckPinned`). The Linux shell has no Pin yet: Dart shows
the button only on macOS.

Install it from a release's `.dmg` (drag it to Applications; `agentctl` comes inside the app, in
`Contents/MacOS/`), with `install.sh --app` (to `~/Applications`), or from a clone with
`./install.sh --source --app` (needs Flutter 3.44+). What goes where, updating and removing it:
[docs/install.md](../../docs/install.md#macos).

`macos/Runner/SettingsWindow.swift`: the Settings window, a WKWebView in a normal window (900 × 640
to start; it remembers where you put it). While it is open the app has a Dock icon and a menu bar
(Cmd-W, Cmd-Q, copy and paste, Cmd-R to reload); closing it makes the app a menu bar app again.
`Info.plist` sets `NSAllowsLocalNetworking` so the web view may load plain http from 127.0.0.1.

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
- **The icon's menu** (right-click on most desktops): Show the deck, Settings…, Open dashboard in
  browser (`agentctl web`), Quit agents deck.
- **Settings** (`linux/runner/settings_window.cc`): a window with a WebKitGTK web view when the app
  was built with `libwebkit2gtk-4.1-dev` (it then needs `libwebkit2gtk-4.1-0` to run), Ctrl+W
  closes it, its size is kept in `~/.config/agents-deck/settings-window.ini`. Built without
  WebKitGTK, Settings opens in the default browser.
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

From a release: the `.deb` (Debian and Ubuntu) puts the app in `/opt/agents-deck` with `agentctl`
next to it, a systemd user service, an app menu entry and a udev rule for the boards' USB ports; the
`.tar.gz` is the same app folder for other distributions, which `install.sh --app` sets up in your
home folder. Both are described, with updating and removing them, in
[docs/install.md](../../docs/install.md#linux).

From a clone, with Go 1.24+, Flutter 3.44+ and Flutter's Linux build tools
(`sudo apt-get install clang cmake ninja-build pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev` on
Debian and Ubuntu; leave out the last one and Settings opens in the browser):

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
`docs/fixtures/state.json`, `/admin/v1/system`, a login code and a pretend dashboard) and checks the
icon, its menu, the deck window, the status line's request, Settings (its window, or the browser
when built without WebKitGTK), Esc, a second start and Quit. It needs `xvfb`, `xdotool`, `python3-gi` and `gdbus`. CI runs it on every push.

## Windows

There is no Windows shell yet. Everything in `lib/` is platform-neutral; a Windows runner must:

- implement the channel above: a notification-area icon that toggles a borderless 440 × 472 window
  above the taskbar, `attention`, `hide`, `visible`, "Settings…" calling `openSettings`, and
  `settings(url)` opening a normal window with WebView2 on that URL that loads only that origin
  (others go to the default browser), or the default browser when WebView2 is missing;
- ship `agentctl.exe` next to the app's executable, so **Start** can run `agentctl install`.

`lib/service.dart` and the deck's client reach the service over its unix socket. Dart has no unix
sockets on Windows, so the Windows build also needs another local transport there (a named pipe,
or a loopback port with a token only your user can read) on both sides.

## Tests

```bash
flutter analyze && flutter test
flutter test --update-goldens   # redraws test/goldens/ (the golden tests only run then)
```
