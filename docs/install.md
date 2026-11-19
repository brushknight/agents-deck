# Installing agents deck

One page per question: what to download for your computer, what it puts where, how it starts when you
log in, how to update it, how to remove it completely, and what to do when something doesn't work.

Everything is on the [latest release](https://github.com/brushknight/agents-deck/releases/latest)
page, under **Assets**. `<version>` in a file name is the release's number, for example `0.2.0`.

- [macOS](#macos)
- [Windows (preview)](#windows-preview)
- [Linux](#linux)
- [Only the command line](#only-the-command-line)
- [Troubleshooting](#troubleshooting)

The app always comes with the service inside it. The service (`agentctl`) is the part that watches
your agents, talks to the boards and checks for updates; the app is the icon and the deck on top of
it. Installing the app installs both, at the same version.

## macOS

**You need** macOS 13 (Ventura) or later, on Apple Silicon or Intel.

**Download** `agents-deck_<version>_macos_universal.dmg`. One file works on both kinds of Mac.

**Install.** Open the `.dmg` and drag **agents deck** onto the **Applications** folder next to it.
Then open agents deck from Applications. A critter appears in the menu bar (there's no Dock icon);
click it for the deck.

On its first start the app registers the service to start at login. macOS then shows a notification,
**Background Items Added**, naming `agentctl`: that's this service, and it should stay allowed.

**What goes where**

| what | where |
|---|---|
| the app, with `agentctl` inside it | `/Applications/agents deck.app` (`Contents/MacOS/agentctl`) |
| the service's login item | `~/Library/LaunchAgents/dev.agents-deck.agentctl.plist` |
| your data: settings, paired boards, keys, the log, downloaded firmware | `~/.local/share/agents-terminal/` |
| the deck's private key and the Wi-Fi password for boards | your login Keychain, under `dev.agents-deck` |

**At login.** The service starts by itself; you'll see it in System Settings, General, Login Items
& Extensions, under **Allow in the Background**. The menu bar icon doesn't start by itself unless you
want it to: add **agents deck** to **Open at Login** on the same page.

**Updating.** The app tells you when a new version is out: the line under the deck says **update
ready**, and Settings, Updates shows what's new with an **Install** button. Installing swaps the app
for the new one and restarts it; your agents keep running. From a terminal, `agentctl update` does
the same.

**Removing it completely**

1. Right-click the menu bar icon and choose **Quit**.
2. Stop the service and remove its login item. In Terminal:

   ```bash
   "/Applications/agents deck.app/Contents/MacOS/agentctl" uninstall
   ```

3. Drag **agents deck** from Applications to the Trash.
4. Delete your data: in Finder, Go, Go to Folder, type `~/.local/share/agents-terminal` and move that
   folder to the Trash. This forgets every paired board; they pair again over USB if you reinstall.
5. Optional: in Keychain Access, search for `dev.agents-deck` and delete what it finds.

If you dragged the app to the Trash first, step 2 still works with any `agentctl` you have, or turn
`agentctl` off under Allow in the Background in Login Items & Extensions.

**If macOS says it can't check the app.** Releases are signed and notarized by Apple when the project
has its Apple certificate set up. For a build that isn't notarized, macOS says it could not verify
that "agents deck" is free of malware. To open it anyway, once: click **Done**, then open System
Settings, Privacy & Security, scroll down to the message about agents deck, and click **Open
Anyway**. The release notes say whether a release is notarized.

## Windows (preview)

**You need** Windows 10 or 11, 64-bit (x64).

**This is a preview, not yet tried on a real Windows PC.** The Windows build is made and checked
on other systems only, so expect rough edges, and please
[report what you find](https://github.com/brushknight/agents-deck/issues). On Windows the service
runs, sees your Claude Code and Codex sessions, and drives boards on USB and Wi-Fi, including
flashing them.

**What the installer puts on your PC:** `agentctl.exe` only, which is the service and the command
line in one file, a Start menu entry that opens the deck in your browser, and (if you leave it
ticked) a startup entry that starts the service when you log in. It has no tray or desktop app.

What's missing:

- **No tray icon yet.** The deck and Settings open in your browser, from the Start menu. The Windows
  version of the menu bar app comes later.
- **No starting agents.** `agentctl new` and restoring agents after a restart run agents in tmux,
  which Windows doesn't have. Agents you start yourself, in any terminal, show up on the deck.
- **No jump to the agent's terminal** from the deck.
- **No boards over Bluetooth.** Boards on USB and Wi-Fi work.
- **Answering a prompt** from the deck works where the tool has hooks for it.

**Download** `agents-deck_<version>_windows_amd64_setup.exe`.

**The warning you'll see.** The Windows installer isn't signed yet, so your browser may first say the
file isn't commonly downloaded: choose **Keep** (in Edge, under the **...** next to the download).
When you open it, Windows shows a blue box, **Windows protected your PC**. Click **More info**, check
that the app is `agents-deck_<version>_windows_amd64_setup.exe`, then click **Run anyway**. This
happens until the project has a code-signing certificate.

**Install.** The installer needs no administrator password: it installs for your user only. Leave
**Start agents deck when I log in** ticked unless you want to start it by hand.

**What goes where**

| what | where |
|---|---|
| `agentctl.exe` (and the app, once there is one) | `%LOCALAPPDATA%\Programs\agents deck` |
| Start menu | **agents deck** (opens the deck in your browser) and **Uninstall agents deck** |
| start at login | a startup entry named `agents deck` for your user (Settings, Apps, Startup lists it) |
| your data: settings, paired boards, keys, the log, downloaded firmware | `%LOCALAPPDATA%\agents-deck` |

**At login.** The service starts hidden, with no window. Turn it off in Settings, Apps, Startup, or
from a terminal with `agentctl uninstall` (which also stops it).

**Updating.** Settings, Updates in the browser shows a new version with an **Install** button, and
`agentctl update` does the same from a terminal. It runs the new installer quietly and starts the
service again.

**Removing it completely**

1. Windows Settings, Apps, Installed apps, **agents deck**, Uninstall. This stops the service and
   removes its startup entry and its files.
2. Delete your data: type `%LOCALAPPDATA%\agents-deck` in the File Explorer address bar and delete
   that folder.

## Linux

**You need** a 64-bit PC or ARM computer (x64 or arm64) with a desktop that shows tray icons (see
[the tray on GNOME](#linux-the-tray-icon-doesnt-show)). The `.deb` is for Ubuntu 24.04 and Debian 13
or later; the `.tar.gz` is for everything else. The tray app needs GLib 2.80 or later (Ubuntu 24.04,
Debian 13, Fedora 40 and newer): on older systems such as Ubuntu 22.04 and Debian 12 it doesn't
start, and only `agentctl` and the dashboard in the browser work
([only the command line](#only-the-command-line)).

### Debian and Ubuntu: the .deb

**Download** `agents-deck_<version>_linux_amd64.deb` (most PCs) or `..._linux_arm64.deb` (ARM, for
example a Raspberry Pi 5).

**Install.** Open the file with your software installer (on Ubuntu, App Center), or in a terminal:

```bash
sudo apt install ./agents-deck_<version>_linux_amd64.deb
```

Then open **agents deck** from your app menu. The first time, it registers the service for your user
and starts it, and its icon appears in the tray.

**What goes where**

| what | where |
|---|---|
| the app, with `agentctl` next to it | `/opt/agents-deck/`, with `agentctl` and `agents-deck` in `/usr/bin` |
| app menu entry | `/usr/share/applications/dev.agents-deck.menubar.desktop` |
| the service, for your user | `/usr/lib/systemd/user/agents-deck.service`, and the copy the app writes to `~/.config/systemd/user/` |
| access to the boards' USB ports | `/usr/lib/udev/rules.d/70-agents-deck.rules` (no need to join `dialout`) |
| your data: settings, paired boards, keys, the log, downloaded firmware | `~/.local/share/agents-terminal/` |
| the size and place of the Settings window | `~/.config/agents-deck/` |

**At login.** The service starts at login as a systemd user service. The tray icon doesn't start by
itself: add **agents deck** to your desktop's startup applications (on GNOME, the Tweaks app, Startup
Applications), or copy the app menu entry to `~/.config/autostart/`.

**Updating.** The package manager owns these files, so the app doesn't replace them. It tells you when
a new version is out; download the new `.deb` from the releases page and install it the same way.

**Removing it completely**

```bash
agentctl uninstall                 # stop the service and remove it from login
sudo apt remove agents-deck        # the app, agentctl, the service file and the udev rule
rm -rf ~/.local/share/agents-terminal ~/.config/agents-deck   # your data
```

### Other distributions: the .tar.gz

**Download** `agents-deck_<version>_linux_amd64.tar.gz` (or `_arm64`). It's one folder,
`agents-deck/`: the app with `agentctl` next to it, and in `share/` the service, the udev rule and the
app menu entry.

The install script sets it up for you, and checks the download against the release's checksums:

```bash
curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh -s -- --app --autostart
```

That puts the app in `~/.local/share/agents-deck/app`, `agentctl` in `~/.local/bin`, an entry in
your app menu, starts the service at login (a systemd user service) and, with `--autostart`, the tray
icon too (`~/.config/autostart/dev.agents-deck.menubar.desktop`). It prints the one `sudo` command
that gives you access to the boards' USB ports (copying `share/70-agents-deck.rules` to
`/etc/udev/rules.d/`).

The app needs GTK 3 and WebKitGTK 4.1 (for the Settings window): on Fedora `webkit2gtk4.1`, on Arch
`webkit2gtk-4.1`, on openSUSE `libwebkit2gtk-4_1-0`.

**Updating.** Settings, Updates (or `agentctl update`) puts the new version in place and restarts the
service.

**Removing it completely**

```bash
agentctl uninstall
pkill -x agents-deck
rm -rf ~/.local/share/agents-deck ~/.local/share/agents-terminal ~/.config/agents-deck
rm -f ~/.local/bin/agentctl ~/.local/bin/agents-deck \
  ~/.local/share/applications/dev.agents-deck.menubar.desktop \
  ~/.config/autostart/dev.agents-deck.menubar.desktop
sudo rm -f /etc/udev/rules.d/70-agents-deck.rules    # if you copied it
```

## Only the command line

No app, just `agentctl` and the service, on macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh
```

It downloads the latest release, checks it against the release's checksums (and its signature, once
an `agentctl` that can check it is installed), puts `agentctl` in `~/.local/bin`, and starts the
service at login. Add `--app` for the app as well, `--version v0.2.0` for a given release. On Windows,
`agentctl_<version>_windows_amd64.zip` holds `agentctl.exe` on its own. Copied out by hand, it
doesn't update itself: Settings, Updates says when a new version is out, and you download it again
(or use the installer, which updates itself).

From a clone, `./install.sh --source` builds the same from the code (Go 1.24+; Flutter 3.44+ for
`--app`). A build from source never updates itself.

To see what's installed and whether it runs:

```bash
agentctl status        # running, starts at login, where its files are
agentctl version
agentctl update --check
```

## Troubleshooting

### macOS: Background Items Added

The notification after the first start is about `agentctl`, the service. If you turned it off, the
service won't start at login and the app shows **not running** with a **Start** button. Turn it back
on in System Settings, General, Login Items & Extensions, under Allow in the Background, then click
Start (or log out and back in).

### macOS: "the LaunchAgent is for another agentctl"

You installed agents deck before with `install.sh` and now have the app as well, or the other way
round. `agentctl status` says which one starts at login. Click **Start** in the app (or run its
`agentctl install`) so the app's copy takes over; remove `~/Applications/agents deck.app` if an older
copy sits there.

### Linux: the tray icon doesn't show

KDE Plasma, XFCE, Cinnamon and MATE show it as it is. GNOME needs the AppIndicator extension
(`gnome-shell-extension-appindicator`; Ubuntu has it on, and the `.deb` recommends it). Install it,
log out and back in. On a desktop with no tray at all, open **agents deck** from the app menu: the
deck opens in a window. More in [clients/macos/README.md](../clients/macos/README.md#where-it-works).

### Linux: Settings opens in the browser instead of a window

The app shows Settings in its own window when WebKitGTK 4.1 is installed (`libwebkit2gtk-4.1-0` on
Debian and Ubuntu, which the `.deb` brings along). Without it, or in an app you built without it,
Settings opens in your browser instead, and works the same there. If the app won't start at all
and complains about `libwebkit2gtk-4.1.so.0`, install that package.

### Linux: the board's port can't be opened

The udev rule gives you, the logged-in user, the boards' USB ports. The `.deb` installs it; with the
archive, copy it once:

```bash
sudo cp ~/.local/share/agents-deck/app/share/70-agents-deck.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

Then unplug the board and plug it in again. The rule covers Espressif's own USB and the CH340,
CP210x and FTDI adapters boards use. Without it, joining the `dialout` group (`sudo usermod -aG
dialout $USER`, then log out and in) works too.

### Windows: "Windows protected your PC"

That's SmartScreen, because the installer isn't signed yet: More info, then Run anyway. See
[above](#windows-preview).

### macOS: Bluetooth says it may not be used

The first time agents deck looks for boards over Bluetooth, macOS asks whether **agents deck
Bluetooth** may use Bluetooth (that is the helper inside the app, `deck-ble`). If you said no, or the
prompt never came, Settings, Network says "Bluetooth needs your permission": turn **agents deck
Bluetooth** on in System Settings, Privacy & Security, Bluetooth, then restart the service (Settings,
Troubleshoot, Restart). If Bluetooth is off in Control Centre, it says that instead.

A board that keeps saying "the board no longer knows this computer" was put back in setup on its own
screen: remove it in System Settings, Bluetooth (if it is listed there), then add it again in
Settings, Boards.

### The service isn't running

`agentctl status` says whether it runs, whether it starts at login, and where its log is. The app's
**Start** button (or `agentctl install`) registers it and starts it. The log is `agentd.log` in your
data folder.

### Boards

A board that doesn't show up, doesn't flash or stays on "connecting": see
[flashing.md](flashing.md#troubleshooting).
