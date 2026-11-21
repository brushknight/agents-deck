<p align="center"><img src="docs/images/hero-v3.png" alt="agents deck: the board of Claude Code and Codex agents in the macOS menu bar, on a 4-inch wall panel and on a 2.4-inch LilyGo T4-S3, finding agents in Orca, herdr, cmux, Codex and tmux" width="100%"></p>

<p align="center">
  <a href="https://github.com/brushknight/agents-deck/actions/workflows/ci.yml"><img src="https://github.com/brushknight/agents-deck/actions/workflows/ci.yml/badge.svg" alt="ci"></a>
  <a href="https://github.com/brushknight/agents-deck/releases/latest"><img src="https://img.shields.io/github/v/release/brushknight/agents-deck?label=release&color=FF4D00&labelColor=1b1b18" alt="latest release"></a>
  <img src="https://img.shields.io/badge/macOS-Apple%20Silicon%20%7C%20Intel-7A776F?labelColor=1b1b18" alt="macOS on Apple Silicon and Intel">
  <img src="https://img.shields.io/badge/Linux-x64%20%7C%20arm64-7A776F?labelColor=1b1b18" alt="Linux on x64 and arm64">
  <img src="https://img.shields.io/badge/Windows-preview-7A776F?labelColor=1b1b18" alt="Windows, preview">
</p>

# agents deck

**Mission control for your Claude Code and Codex agents**: in your menu bar, in the browser, on your
desk, and on a little screen you flash yourself.

A small program on your computer keeps track of every coding agent you run: who's working, who needs
you, what each one is doing, its context and cost. You see them all as a deck of tiles, each agent a
pixel critter, and you can answer an agent's question or permission prompt, stop it, or jump to its
terminal from any screen. Everything stays on your computer. The boards are optional and cheap: plug
one in with a USB cable and it shows the same deck; give it your Wi-Fi, or add it over Bluetooth,
and it works without the cable.

- [What you need](#what-you-need) · [Install](#install) · [What you see](#what-you-see)
- [Pick a display](#pick-a-display) · [Flash a board](#flash-a-board) · [Wi-Fi](#wi-fi) · [Bluetooth](#bluetooth)
- [Updates](#updates) · [What works where](#what-works-where) · [Harnesses](#works-with-your-harness)
- [For the terminal](#for-the-terminal) · [How it works](#how-it-works) · [Security](#security--privacy) · [Docs](#docs)

## What you need

- **A Mac, a Linux PC or a Windows PC.** macOS 13 or later (Apple Silicon or Intel); Linux on x64 or
  arm64 with a desktop that shows tray icons; Windows 10 or 11, 64-bit, as a
  [preview](#what-works-where) that hasn't been tried on a real Windows PC yet.
- **Claude Code or Codex**, however you like to run them.
- **Optional: a board.** One of the [displays below](#pick-a-display) and a USB-C cable that carries
  data. No soldering, no wiring: the screen, touch and USB are all on the board.

## Install

**1. Download the app for your computer** from the
[latest release](https://github.com/brushknight/agents-deck/releases/latest) (under **Assets**):

| your computer | the file | then |
|---|---|---|
| **Mac** | `agents-deck_<version>_macos_universal.dmg` | open it, drag **agents deck** to Applications, open it from there |
| **Windows** (preview) | `agents-deck_<version>_windows_amd64_setup.exe` | run it; no administrator password needed |
| **Linux**, Ubuntu 24.04+ or Debian 13+ | `agents-deck_<version>_linux_amd64.deb` (`arm64` for ARM) | open it with your software installer, then **agents deck** from the app menu |
| **Linux**, anything else | `agents-deck_<version>_linux_amd64.tar.gz` (`arm64` for ARM) | see [install.md](docs/install.md#other-distributions-the-targz) |

The app brings everything with it: the service (`agentctl`, which watches your agents and boards even
when the app is closed) and the app on top of it, at the same version. It starts the service when you
log in. A critter appears in the menu bar (the system tray on Linux); click it and the deck opens.
Agents you already run in Claude Code, Codex, Orca, herdr or cmux show up on it right away. The gear
under the deck opens **Settings**.

On Windows, for now, the installer brings only the service and `agentctl`: **agents deck** in the
Start menu opens the deck in your browser, and there is no tray app yet. The Windows build hasn't been
tried on a real Windows PC.

**2. Plug in a board** (optional). Open Settings, **Boards**: a new board on USB shows up there.
Say it's yours, pick which board it is, and click **Install firmware**. About a minute later it shows
your agents. [Flash a board](#flash-a-board) has the details.

What each download puts where, how to update and how to remove it: [docs/install.md](docs/install.md).
**Coming from 0.1.1?** Install 0.2.0 over it: your boards, panels and phones stay paired
([upgrading from 0.1.1](docs/install.md#upgrading-from-011)).

**Warnings you may see the first time:**

- **Windows** says **Windows protected your PC**, because the preview installer isn't signed yet.
  Click **More info**, then **Run anyway**.
- **macOS**, for a build that isn't notarized by Apple, says it could not verify the app. Click
  **Done**, then System Settings, Privacy & Security, **Open Anyway**. Notarized releases open without
  it. Either way, macOS then tells you a background item was added: that's the agents deck service.
  Leave it on.

## What you see

### The deck

<table>
<tr>
<td width="62%"><img src="docs/images/web-board-v4.png" alt="the deck in the browser: one tile per agent"></td>
<td><img src="docs/images/menubar-deck-v3.png" alt="the same deck dropped down from the menu bar"></td>
</tr>
</table>

One tile per agent, 16 to a page, the same in the menu bar, the browser, on the desk panel and on the
4″ board. The critter shows what its agent is doing: thinking, reading, editing, running a
command, compacting. Orange means working; a **solid orange** tile needs you (a permission prompt or a
question); **stripes** and a critter chomping a cookie mean it finished and you haven't looked at the
result yet. The border fills with the agent's context. A small critter in the corner means subagents
are at work too, and an agent whose background subagents are still running stays working until the
last one is done. Each agent wears a pixel hat picked by its folder, so a project's agents look alike.

### Needs you

<table>
<tr>
<td width="62%"><img src="docs/images/web-needs-you-v4.png" alt="Home in the browser: the agents that need you, listed beside the deck"></td>
<td><img src="docs/images/devices/lcd4-needs-you.png" alt="the same Needs you page on the 4-inch board"></td>
</tr>
</table>

One page to the left of the first page of the deck, with only the agents that need you: the ones
**working**, the ones **asking** you something, and the ones that **finished and you haven't looked
at yet**. Those stay until you check them; the others leave 5 seconds after they stop needing you.
It looks like any page of the deck.

The service decides who sits in which spot, so the browser, the menu bar, the desk panel and the
boards show the same agents **in the same places**, and an agent keeps its spot while others come and
go. In the browser, Home lists the agents asking you something beside the deck (here). In the menu
bar app and on the desk panel: press ←, swipe or scroll left, or click the mark at the left of the
page bars (it shows how many agents are there; it turns solid orange while you are on it). On the boards: swipe
right on the first page (or press BOOT) on the 4″, the 1.8″ AMOLED and the T4-S3, and hold BOOT on
the 1.47″. The round boards don't have it yet. [Features](docs/features.md#the-board) has every rule.

### The agent card

<table>
<tr>
<td width="62%"><img src="docs/images/web-permission-v4.png" alt="answering a permission prompt from the browser"></td>
<td><img src="docs/images/panel-detail-v3.png" alt="an agent's card on the desk panel, with its subagents"></td>
</tr>
</table>

Click or tap a tile for its card: what it's doing now and its subagents, context, tokens, estimated
cost, turns and the last prompt, with **focus terminal**, **interrupt** and **stop**. A permission
prompt shows the exact command with Claude's own options, a question shows its choices, and your
answer is typed into the agent's terminal. In the browser you can also type your own answer instead
of picking one; elsewhere, text you'd have to type yourself sends you to the terminal.

### Settings

<table>
<tr>
<td width="50%"><img src="docs/images/settings-boards-v4.png" alt="Settings, Boards: each board with its connection, firmware and uplink"></td>
<td><img src="docs/images/settings-updates-v4.png" alt="Settings, Updates: the app and each board with newer firmware"></td>
</tr>
<tr>
<td><b>Boards.</b> Every board and device: how it's connected (USB, Wi-Fi or Bluetooth), which firmware it runs and one line about updates with its button, what it may do, rename, remove. A new board on USB appears at the top with <b>Set it up</b>.</td>
<td><b>Updates.</b> The version you have and whether to tell you, install automatically or never check, and every board that has newer firmware. Boards never update by themselves.</td>
</tr>
<tr>
<td><img src="docs/images/settings-network-v4.png" alt="Settings, Network: the Wi-Fi boards get, each board's network, Bluetooth and pairing a phone"></td>
<td><img src="docs/images/menubar-needs-you-v3.png" alt="the Needs you page in the menu bar"></td>
</tr>
<tr>
<td><b>Network.</b> Whether paired devices may connect over the network, the Wi-Fi network boards get, each board's network, whether Bluetooth is on, and pairing a phone.</td>
<td>The menu bar app shows the same deck and Needs you page (here). Settings opens in its own window from the gear; in the browser it's in the sidebar.</td>
</tr>
</table>

Also in Settings: **General** (start at login, notifications), **Agents** (the tools found, Claude
sessions outside the deck, accounts) and **Troubleshoot** (restart the service, a report to copy, the
log). Every page is also a command ([features](docs/features.md#the-app-in-the-browser)).

## Pick a display

All of these are small ESP32-S3 boards: a microcontroller (a tiny computer that runs one program,
the "firmware") with a screen on it. You put our firmware on it once, over USB ("flashing"), and from
then on it's a little deck. Pick by size and shape; the [devices page](docs/devices.md) has every
screen of every board.

<table>
<tr>
<td align="center" width="180"><img src="docs/images/devices/round-28-home.png" width="160" alt="the 2.8-inch round board"></td>
<td><b>Waveshare ESP32-S3-Touch-LCD-2.8C</b><br>2.8″ round · touch · Wi-Fi · Bluetooth<br>Agents on a ring you turn with a finger, 9 to a turn.<br>Buy: <a href="https://www.waveshare.com/esp32-s3-touch-lcd-2.8c.htm">Waveshare</a>, about $40 · firmware: <a href="firmware/devices/waveshare-touch-lcd-28c"><code>waveshare-touch-lcd-28c</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/devices/round-21-done.png" width="140" alt="the 2.1-inch round board"></td>
<td><b>Waveshare ESP32-S3-Touch-LCD-2.1</b><br>2.1″ round · touch · Wi-Fi · Bluetooth<br>The same ring, 7 to a turn.<br>Buy: <a href="https://www.waveshare.com/esp32-s3-touch-lcd-2.1.htm">Waveshare</a>, about $33 · firmware: <a href="firmware/devices/waveshare-touch-lcd-21"><code>waveshare-touch-lcd-21</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/devices/lcd4-home.png" width="160" alt="the 4-inch square board"></td>
<td><b>Waveshare ESP32-S3-Touch-LCD-4</b><br>4″ square · touch · Wi-Fi · Bluetooth · Needs you<br>The full 4×4 deck, like the desk panel.<br>Buy: <a href="https://www.waveshare.com/esp32-s3-touch-lcd-4.htm">Waveshare</a>, about $33 · firmware: <a href="firmware/devices/waveshare-touch-lcd-4"><code>waveshare-touch-lcd-4</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/devices/amoled18-card.png" width="120" alt="the 1.8-inch AMOLED board"></td>
<td><b>Waveshare ESP32-S3-Touch-AMOLED-1.8</b><br>1.8″ AMOLED · touch · Wi-Fi · Bluetooth · Needs you<br>2×2 tiles you scroll, and a card for each agent.<br>Buy: <a href="https://www.waveshare.com/esp32-s3-touch-amoled-1.8.htm">Waveshare</a>, about $28 · firmware: <a href="firmware/devices/waveshare-touch-amoled-18"><code>waveshare-touch-amoled-18</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/devices/t4s3-home.png" width="120" alt="the LilyGO T4-S3"></td>
<td><b>LilyGO T4-S3</b><br>2.41″ AMOLED, tall · touch · Wi-Fi · Bluetooth · Needs you<br>2×3 tiles you scroll, and a card for each agent.<br>Buy: <a href="https://lilygo.cc/products/t4-s3">LilyGO</a>, about $59 · firmware: <a href="firmware/devices/lilygo-t4-s3"><code>lilygo-t4-s3</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/devices/lcd147-running.png" width="160" alt="the 1.47-inch strip"></td>
<td><b>Waveshare ESP32-S3-LCD-1.47B</b><br>1.47″ strip · no touch · USB only · Needs you (hold BOOT)<br>One agent and a mini-board; look only.<br>Buy: <a href="https://www.waveshare.com/esp32-s3-lcd-1.47b.htm">Waveshare</a>, about $13 · firmware: <a href="firmware/devices/waveshare-lcd-147"><code>waveshare-lcd-147</code></a></td>
</tr>
<tr>
<td align="center" width="180"><img src="docs/images/panel-board-v3.png" width="160" alt="the desk panel"></td>
<td><b>Wall Panel (Raspberry Pi CM4)</b><br>4″ 720×720 · touch · Wi-Fi · Needs you<br>The full deck app.<br>Buy: <a href="https://github.com/dotstartech/wall-panel">project page</a> · firmware: <a href="clients/flutter/agents_ui"><code>clients/flutter</code></a></td>
</tr>
</table>

Prices are from the maker's own shop (Waveshare, LilyGO) in October 2026, for the version to buy:
**with touch** where there's a choice, without a battery for the 1.8″ AMOLED, the standard "B" for
the 1.47″, and the LilyGO with its shell. They change, and shipping and tax are extra. Not sure? The
**2.8″ round** is the nicest to use; the **1.8″ AMOLED** is the smallest touch board with a
rectangular screen and has the Needs you screen.

**Want another board?** Ask for it in a [hardware request](https://github.com/brushknight/agents-deck/issues/new?title=Hardware+requests%3A+which+display+should+agents+deck+support+next%3F&body=agents+deck+runs+on+these+boards+today+%28see+%5Bdocs%2Fdevices.md%5D%28..%2Fblob%2Fmain%2Fdocs%2Fdevices.md%29%29%3A%0A%0A-+Waveshare+ESP32-S3-Touch-LCD-2.1+%28round%29%0A-+Waveshare+ESP32-S3-Touch-LCD-2.8C+%28round%29%0A-+Waveshare+ESP32-S3-Touch-LCD-4+%28square%2C+desk-panel+layout%29%0A-+Waveshare+ESP32-S3-Touch-AMOLED-1.8+%28small+square%29%0A-+LilyGO+T4-S3+2.41%E2%80%B3+AMOLED%0A-+Waveshare+ESP32-S3-LCD-1.47+%28strip%29%0A%0A%2A%2AWant+another+one%3F%2A%2A+Comment+here+with%3A%0A%0A1.+%2A%2ABoard%2A%2A+%E2%80%94+exact+name+and+a+link+to+the+maker%27s+product+page%0A2.+%2A%2AScreen%2A%2A+%E2%80%94+size%2C+resolution%2C+shape+%28round+%2F+square+%2F+bar%29%2C+touch+or+buttons%0A3.+%2A%2AChip%2A%2A+%E2%80%94+ESP32-S3+boards+are+easiest+%28USB%2C+PSRAM%2C+Wi-Fi%29%3B+others+are+welcome+but+take+longer%0A4.+%2A%2AHow+you%27d+use+it%2A%2A+%E2%80%94+on+the+desk%2C+on+the+wall%2C+in+a+keyboard%2C+as+a+status+light%E2%80%A6%0A%0A%F0%9F%91%8D+the+requests+you%27d+like+too%3A+the+most-wanted+boards+get+built+first.%0A%0AWriting+firmware+yourself%3F+Follow+%5Bdocs%2Flink%2Fnew-device.md%5D%28..%2Fblob%2Fmain%2Fdocs%2Flink%2Fnew-device.md%29+%E2%80%94+a+coding+agent+can+follow+it+too.&labels=hardware): say which board, its

## Flash a board

<p align="center"><img src="docs/images/settings-new-board-v4.png" alt="Settings, Set up a new board: pick which board it is from the pictures" width="80%"></p>

You do this once per board. Plug it in with a USB-C cable that carries data (many cables that come
with chargers only carry power). Then pick one way:

- **From the app.** Settings, **Boards** shows the new board. Click **Set it up**, then **Yes, it's
  mine**, pick the board from the pictures, and click **Install firmware**. The app downloads the
  image from the latest release, checks its signature, writes it and reads it back. When it's done
  the board restarts, pairs and shows your agents.
- **From the [web flasher](https://brushknight.com/agents-deck)**, if you haven't installed anything: open it in Chrome or
  Edge, pick your board, click **Connect**, choose the board's USB port, and click **Install**. (It
  uses Web Serial, which Safari and Firefox don't have.)
- **From a terminal**, with `agentctl flash`, or by building the firmware yourself with PlatformIO
  to run the newest code.

All of them, step by step, with the BOOT and RST rescue for a board that doesn't answer:
[docs/flashing.md](docs/flashing.md).

A board you confirm in Settings is approved at once and can answer prompts, focus a terminal and
interrupt an agent (scope `act`): plugging it into your computer counts as your consent. Giving it
**admin** (it adds stop), approving boards by hand, renaming or removing one: all in Settings,
Boards. Something not working? See [troubleshooting](docs/flashing.md#troubleshooting).

## One agent runs the others (MCP)

agents deck is an MCP server too. Make a token in Settings, **Agents**, MCP, add it to Claude Code
(`claude mcp add --transport http agents-deck http://127.0.0.1:7340/mcp --header "Authorization:
Bearer …"`), and that agent can see every agent on your deck, give them tasks, answer their questions,
interrupt them and start new ones in a project folder. Local only, scoped and audited:
[docs/mcp.md](docs/mcp.md).

## Wi-Fi

Every touch board except the 4″ (for now) can leave your computer and run from any USB charger. Set
your Wi-Fi network once in Settings, **Network**; every board that's plugged in gets it, and so does
every board you plug in later. Then unplug it, power it anywhere on the same network, and it finds
the deck. Plug it back in and it's on USB again at once. Boards use 2.4 GHz Wi-Fi only.

## Bluetooth

<table>
<tr>
<td width="68%"><img src="docs/images/settings-bluetooth-v4.png" alt="Settings, Boards, Add a board: Look for Bluetooth boards finds boards nearby, each with an Add button"></td>
<td><img src="docs/images/devices/amoled18-bluetooth.png" alt="the board shows a 6-digit code to type on the computer"></td>
</tr>
</table>

New in 0.2.0, Bluetooth is a third way for a board to reach the deck, next to USB and Wi-Fi. Use it
for a board next to a laptop with no Wi-Fi and no cable, or to set up a board without plugging it in:
over Bluetooth it shows your agents and can get your Wi-Fi network.

**Add a board.** In Settings, **Boards**, **Add a board**, turn on **Look for Bluetooth boards**. Boards nearby appear with
their name (`deck-` and six characters, shown on the board's screen). Click **Add** next to yours; the
board shows a 6-digit code, and you type it into the window macOS opens. Nothing is added on sight:
only a board whose code you typed. Each board then has an **uplink**: Auto (USB, else Wi-Fi, else
Bluetooth), Wi-Fi, or Bluetooth.

**Where it works today:** the touch boards on their `idf5` firmware, which is what the releases
install on the 2.8″ and 2.1″ round boards, the 4″, the 1.8″ AMOLED and the T4-S3 (the 1.47″ has
none). On the computer side, only the macOS app has
the Bluetooth helper (`deck-ble`); Linux and Windows don't have Bluetooth yet
(help wanted: a helper like `deck-ble` for BlueZ, see [the roadmap](docs/roadmap.md)). Firmware updates don't
go over Bluetooth: a board updates over Wi-Fi or the cable. More in
[features](docs/features.md#serial-boards-deck-link) and
[Deck Link, Bluetooth](docs/link/protocol.md#bluetooth).

## Updates

- **The app updates itself.** It checks for a new release two minutes after it starts and then daily.
  Settings, **Updates** shows what's new with **Install and restart**; your agents keep running. You
  can choose "tell me", "automatic" or "never check". The Linux `.deb` tells you and your package
  manager installs it. `agentctl update` does the same from a terminal.
- **Boards update from Settings**, never by themselves: an **Update** button on each board that has
  newer firmware. A board on USB is flashed again; a board on Wi-Fi updates over the air and goes back
  to its old firmware if the new one can't reach the deck. It keeps its name, pairing and Wi-Fi.
- **Only signed releases.** agents deck checks every download against the release's `SHA256SUMS`
  and our signature on it, and installs nothing, app or firmware, that doesn't match the key built
  into it. A copy without that key (built from source, or before the first signed release) still says
  when a new version is out, but installs nothing it downloaded.
- **Firmware you built** installs anyway: `agentctl firmware add <folder>` keeps a build, and every
  board of that kind running an older version gets an **Install** button. **Install a firmware
  file...** on a board installs a single image you pick. Both come only from this computer
  ([flashing.md](docs/flashing.md#firmware-built-on-this-computer)).

## What works where

| | macOS | Linux | Windows (preview) |
|---|---|---|---|
| see Claude Code and Codex sessions, answer prompts | yes | yes | yes, where the tool has hooks |
| the deck and Needs you next to an icon (menu bar or tray) | yes | yes ([desktops](clients/macos/README.md#where-it-works)) | not yet: the deck and Settings open in your browser |
| Settings, web dashboard | yes | yes | yes, in the browser |
| boards on USB and Wi-Fi, flashing, board updates | yes | yes | yes |
| boards over Bluetooth | yes (the app) | not yet | not yet |
| start agents with `agentctl new`, restore after a restart | yes (needs tmux) | yes (needs tmux) | not yet: needs tmux |
| jump to the agent's terminal | iTerm2, Orca, cmux, Codex app | where the harness supports it | not yet |
| updates itself | yes | yes (the `.deb` tells you; your package manager installs) | yes, when installed with the installer (the `.zip` tells you) |
| signed by the platform | notarized by Apple, once the project's certificate is set up | no platform signing | not yet: SmartScreen warns |

Windows has not been tried on a real Windows PC yet; please
[report what you find](https://github.com/brushknight/agents-deck/issues).

## Where else it shows up

| | |
|---|---|
| <img src="docs/images/panel-question-v3.png" alt="a question on the desk panel"> | <img src="docs/images/cli.png" alt="agentctl in the terminal"> |
| **Desk panel.** A 4″ touch panel on Wi-Fi ([Wall Panel](https://github.com/dotstartech/wall-panel)), paired with `agentctl pair`. | **Terminal.** `agentctl` starts, lists, resumes and restores agents. |

The browser works on your phone too ([docs/phone.md](docs/phone.md)). The full tour of every screen
is in [docs/features.md](docs/features.md).

## Works with your harness

agents deck is a status board, not a harness. If you already run your agents in one of these, there's
nothing to set up: the deck finds them and lets you answer, interrupt and jump to them from any screen.

| Runs your agents | What the deck shows |
|---|---|
| [Orca](https://github.com/stablyai/orca) | agents in Orca's worktrees, while Orca is open |
| [herdr](https://herdr.dev) | agents in herdr's panes, in every herdr session that's running |
| [cmux](https://cmux.com) | Claude agents in cmux's terminals, while cmux is open (answering needs one cmux setting, see [features](docs/features.md#cmux)) |
| Codex app and Codex CLI | threads from the last 6 hours, read-only |
| agentctl (built in) | agents it starts with `agentctl new`, each in a tmux session with a status header |
| Claude Code on its own | sessions you started by hand in any terminal, once you add them (`agentctl live`, `agentctl add`) |

**Your harness missing?** Open a pull request ([docs/harnesses.md](docs/harnesses.md) walks through
adding one), or name it in the [harness tracking issue](https://github.com/brushknight/agents-deck/issues/7).

## For the terminal

Everything in the app is also a command, and you don't need the app at all if you'd rather not.

**Install from a terminal** (macOS or Linux): the service and `agentctl`, with `--app` the menu bar or
tray app as well:

```bash
curl -fsSL https://raw.githubusercontent.com/brushknight/agents-deck/main/install.sh | sh -s -- --app
```

That downloads the latest release, checks it against the release's `SHA256SUMS`, installs `agentctl`
to `~/.local/bin`, and starts the service at login. On Linux, `--autostart` starts the tray app at
login too.

**From a clone** (Go 1.24+; Flutter 3.44+ for `--app`), for the newest code before it's in a
release. A build from source never updates itself:

```bash
git clone https://github.com/brushknight/agents-deck && cd agents-deck
./install.sh --source --app
```

On Linux the tray app needs Flutter's Linux build tools:
`sudo apt-get install clang cmake ninja-build pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev`.
More in [clients/macos/README.md](clients/macos/README.md#linux).

**Start agents of your own** (macOS and Linux, needs tmux):

```bash
cd ~/dev/my-project
agentctl new         # a Claude agent for this folder, in tmux (ctrl-q detaches)
agentctl web         # the dashboard in your browser (a one-time login link)
```

Want a full deck without spending tokens? `agentctl sim start --root ~/dev --slots 12` fills it with
pretend agents; `agentctl sim stop` clears them.

**Boards from the terminal:**

```bash
agentctl link ports                              # USB serial ports, with their USB ids
agentctl flash /dev/cu.usbmodem1101 agents-deck-<board>-<version>.bin   # write a release image
agentctl link add /dev/cu.usbmodem1101           # let the deck open this port (once per port)
agentctl devices scope <board-name> admin        # admin adds stop
agentctl link wifi --default --ssid "my network" # the Wi-Fi every board gets (asks for the password)
agentctl bluetooth scan                          # boards nearby over Bluetooth (macOS)
agentctl bluetooth add deck-1a2b3c               # add one: type the code it shows
agentctl firmware ls                             # each board's firmware, the release's, and builds made here
agentctl devices update <board-name>             # update a board (--local: a build made here)
```

The deck only ever opens USB ports you register (that's what **Yes, it's mine** in Settings does).
Prefer to approve each board by hand? `agentctl devices auto-approve usb off`; a new board then shows
"waiting for approval" with the exact `agentctl devices approve …` command on its screen.

### Commands

```text
agentctl new [dir] [-t title] [-c claude|codex|gemini|shell] [-a account] [-d]   start an agent and attach (-d: stay detached)
agentctl attach <agent>             show an agent in this terminal (ctrl-q detaches)
agentctl ls [--sort active]         the fleet at a glance, with when each agent was last active
agentctl web                        open the dashboard
agentctl live / add <session>       Claude sessions started by hand; put one on the deck
agentctl sessions [words]           look up any Claude session by id, title, folder, branch or last prompt
agentctl resume <agent or session>  bring it back and open it, whatever happened to it (see below)
agentctl restore                    bring back everything lost with the tmux server (crash, reboot)
agentctl rm <agent>                 stop and forget an agent
agentctl link [ports | add <port>]  boards: how each is connected, find ports, register one
agentctl link wifi --default --ssid NAME   the Wi-Fi network every board gets (asks for the password)
agentctl bluetooth [scan | ls | add <name>]   boards over Bluetooth (macOS)
agentctl flash <port> <image.bin>   write a board's firmware over USB (--probe: only read the chip)
agentctl flash --board <board> [--local] <port>   install the release's firmware (or a build made here) on a registered port
agentctl firmware ls | add <folder> | rm <board>   firmware: the release's, and builds made on this computer
agentctl devices [approve | scope | revoke <name>]   boards and paired devices
agentctl devices update <name> [--local]   update a board's firmware
agentctl devices uplink <name> auto|wifi|ble   which way a board reaches the deck off USB
agentctl devices reset              start the deck's certificates over (every device pairs again)
agentctl pair <name> [--bundle F]   pair the desk panel: its own certificate, scope view|act|admin
agentctl install | uninstall        start the service at login, or stop that
agentctl status                     is the service running, where are its files
agentctl update [--check]           check for a new version and install it
agentctl settings | ports | diagnostics   what Settings shows, from the terminal
agentctl sim start|stop             pretend agents for demos
```

**Getting agents back.** `resume` and `restore` are one command: bring an agent back and open it.
Name one and agentctl does what it takes: restores it if it was lost with the tmux server, resumes it
if it ended, starts a Claude session that isn't on the deck as an agent, or, if it is still running,
just opens it. `restore` with no name brings back everything lost in a crash (it lists them first and
asks before more than 5; `--all` skips the question).

You can name an agent by its id, the start of its id, its name, or any part of its name that only one
agent has, in any case: `agentctl resume notes` finds "Release Notes v2". When several match,
agentctl lists them (id, name, last active, folder) and asks for the id. With
`source <(agentctl completion zsh)` in `~/.zshrc`, Tab offers the matching agents and sessions by
name, the most recently active first: lost and ended agents, then running ones, then sessions.

**Last active** is when an agent last sent or got a message: from the transcript for Claude and
Codex, from the app for Orca and cmux, and logged by the service for the rest (shells, herdr). It
survives a crash and a restart, so after one you can still tell which agents you were using.

`agentctl help` lists the rest (accounts, hats, handoff, terminal and mouse settings). Building and
flashing boards with a coding agent's help: [docs/flashing.md](docs/flashing.md#let-a-coding-agent-do-it).

## How it works

```mermaid
flowchart LR
  subgraph run["where your agents run"]
    direction TB
    A["agentctl new<br/>(Claude, Codex… in tmux)"]:::a
    O["Orca"]
    H["herdr"]
    M["cmux"]
    L["Claude Code<br/>in any terminal"]
    X["Codex app and CLI"]
  end
  D["agentctl serve<br/>(the service, on your computer)"]:::a
  subgraph see["where you see them"]
    direction TB
    W["web dashboard"]
    MB["menu bar app"]
    P["desk panel"]
    B["boards (ESP32)"]
  end
  A <-->|"hooks, transcripts · send-keys"| D
  O <-->|"orca CLI"| D
  H <-->|"herdr socket"| D
  M <-->|"cmux socket, hook file"| D
  L -->|"transcripts (agentctl add)"| D
  X -->|"~/.codex, read-only"| D
  D <-->|"/v1 + SSE · 127.0.0.1"| W
  D <-->|"/v1 · unix socket"| MB
  D <-->|"/v1 + SSE · TLS 1.3, device certificate"| P
  D <-->|"Deck Link · USB serial, TLS 1.3 on Wi-Fi, or Bluetooth LE"| B
  D -->|"iTerm API"| I["iTerm tab<br/>(jump to an agent)"]
  classDef a fill:#FF4D00,color:#0A0A0A,stroke:#FF4D00
```

On the left, wherever your agents run; on the right, every screen that shows them; in the middle, one
service. Agents started with `agentctl new` run in their own tmux server (`tmux -L agentsterm`), each
with its own hook settings, so your global `~/.claude/settings.json` is never touched. Hooks report
prompts, tool calls, permission requests, compaction and stops; Claude's transcripts
(`~/.claude/projects/*.jsonl`) give tokens, context, model and title. Answers are typed into the
agent's tmux session, or sent through the harness that runs it (Orca, herdr, cmux); Codex is only
read. The web UI, the menu bar app and the panel speak one small JSON API
([docs/protocol.md](docs/protocol.md)); boards speak [Deck Link](docs/link/protocol.md), a compact
protobuf version of it.

## Security & privacy

- **Nothing about you leaves your computer:** no telemetry. The only thing agents deck fetches is this
  project's GitHub releases, to check for updates and download firmware, and it installs nothing
  whose signature doesn't match the key built into it. The only Go dependency is Google's protobuf
  module.
- **The web dashboard and Settings are loopback-only** (`127.0.0.1`), behind a one-time login link, a
  strict CSP and host and origin checks. Settings and its management API are never reachable from the
  network, by a board or by anyone else.
- **The menu bar app** (and the Linux tray app) uses the service's local socket: nothing crosses the
  network.
- **The desk panel and boards on Wi-Fi** connect over TLS 1.3 with their own client certificate,
  signed by the deck's local CA (its key is in the macOS Keychain, or the data folder elsewhere). Each
  device has a scope (view, act or admin), shows up in Settings, Boards and `agentctl devices`, can be
  revoked on its own, and every action it takes lands in an audit log. Updates over Wi-Fi travel on
  the same connection, and a board refuses firmware made for another board.
- **Boards on USB** are only heard on ports you confirmed (Settings, Boards, or `agentctl link add`);
  a new port is listed from its USB id alone, without opening it. A new board on a confirmed port is
  approved at once with scope `act`: physical USB on your computer counts as your consent
  (`agentctl devices auto-approve usb off` makes them wait). Never over Wi-Fi, never a revoked board;
  `admin` is always given by hand. A board gets its certificate only over USB or Bluetooth, never
  over Wi-Fi.
- **Boards over Bluetooth** are never added on sight: nothing goes over Bluetooth without an encrypted
  link from LE Secure Connections pairing with the 6-digit code on the board's own screen. A board
  added that way is a device like any other, with a scope and an audit log, and can be revoked.
  Firmware never goes over Bluetooth.
- **Codex files are only read.** App control on a Mac is a single Apple event (asking iTerm for its API cookie): no synthetic keystrokes, no screen reading.
- **Secrets** live in your data folder (`~/.local/share/agents-terminal/` on macOS and Linux,
  `%LOCALAPPDATA%\agents-deck` on Windows; `0700` / `0600`), and on a Mac the deck's key and the
  default Wi-Fi password in the Keychain.

The whole model, with its known limits: [docs/protocol.md](docs/protocol.md#devices-and-security) and
[docs/link/protocol.md](docs/link/protocol.md#pairing-and-wi-fi).

## Docs

[Install](docs/install.md) · [Upgrading from 0.1.1](docs/install.md#upgrading-from-011) · [Devices](docs/devices.md) · [Flashing](docs/flashing.md) · [Features](docs/features.md) · [Protocol](docs/protocol.md) · [Deck Link](docs/link/protocol.md) · [New device](docs/link/new-device.md) · [Harnesses](docs/harnesses.md) · [Releasing](docs/releasing.md) · [Roadmap](docs/roadmap.md)

| path | |
|---|---|
| `backend/` | Go daemon and CLI (`agentctl`), with the web UI embedded. `go test -race ./...` |
| `clients/flutter/agents_ui` | The deck UI as a Flutter package, shared by the desk panel and the menu bar app. |
| `clients/macos` | The menu bar app (Flutter + a small Swift shell), and the Linux tray app (the same Flutter app + a small GTK shell). |
| `packaging/`, `install.sh` | The installers: the macOS `.dmg`, the Linux `.deb` and archive, the Windows setup, and the install script ([releasing](docs/releasing.md)). |
| `proto/` | `link.proto`: the Deck Link contract for boards. |
| `firmware/deck-link` | The C firmware kit for Deck Link boards (ESP32), with examples. |
| `firmware/devices` | Firmware for each board, one folder each ([devices](docs/devices.md)). |
| `firmware/panel-core` | The boards' shared core: Deck Link, Wi-Fi, critters, fonts, and the round, square and big layouts. |
| `docs/fixtures/state.json` | The sample fleet behind demo mode, the web fixture view, the tests and the screenshots. |

## Credits

The per-project pixel hats were inspired by [“9 NEW Claude Mods that can truly change how you work”](https://www.youtube.com/watch?v=lDrAZ1wAyVs) by Jay E ([RoboNuggets](https://www.youtube.com/@RoboNuggets)).
The 1.47″ board's original drawing, mascots and font are by **belidor**.
