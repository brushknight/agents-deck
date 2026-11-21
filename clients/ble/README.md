# Bluetooth helpers

agents deck's service (`agentctl`) is Go with no cgo, so it has no Bluetooth of its own. On each
platform a small native helper does the radio work and talks to the service with JSON lines on its
stdin and stdout. The service starts it (`deck-ble` next to `agentctl`, or the program named in
`AGENTS_DECK_BLE_HELPER`), starts it again with a back-off when it stops, and runs a Deck Link session
on each connected board as it would on a serial port (`backend/internal/ble`).

| platform | helper | state |
|---|---|---|
| macOS 12 and later | `macos/`: Swift package, executable `deck-ble`, CoreBluetooth | built, unit-tested, and tried with a real board and Mac |
| Linux | BlueZ over D-Bus | not written: Bluetooth says "Bluetooth isn't available on this computer yet" |
| Windows | WinRT `Windows.Devices.Bluetooth` | not written: same |

This file is the one description of the line protocol. A new helper speaks exactly this.

## The board's side (fixed)

| | UUID | |
|---|---|---|
| service | `7e3a0001-5d2c-4f1b-9a6e-a9e17d5c0de0` | advertised, with the local name `deck-<last 6 hex of the board id>` |
| RX | `7e3a0002-5d2c-4f1b-9a6e-a9e17d5c0de0` | computer to board: write and write without response; needs an encrypted, authenticated link |
| TX | `7e3a0003-5d2c-4f1b-9a6e-a9e17d5c0de0` | board to computer: notify; needs an encrypted, authenticated link |
| Info | `7e3a0004-5d2c-4f1b-9a6e-a9e17d5c0de0` | read without pairing: UTF-8 `board=<board>;id=<board id>;fw=<version>;paired=<0\|1>` |

Deck Link frames (already COBS-delimited) travel as a plain byte stream: the helper cuts what the
service writes to the link's largest write, and passes notifications on as they come. No framing of
its own. The board shows a 6-digit passkey (IO capability: display only) and the person types it on the
computer; pairing is LE Secure Connections with MITM protection, and bonds.

## The line protocol

One JSON object per line, UTF-8, `\n` at the end, at most 64 KiB a line, both ways. Binary data is
standard base64. `id` is the helper's identifier for a peripheral: a UUID on macOS (stable for a
bonded board on this computer, different on another computer), the device address elsewhere. The
service treats it as an opaque string.

The helper exits when its stdin closes. Its stderr goes to the service's log: plain words, never
data or keys.

### Commands (service to helper)

| line | what the helper does |
|---|---|
| `{"op":"scan","on":true}` | look for boards advertising the service; `"on":false` stops. Answers `scanning`; while on, `found` for each board, and again once its Info is read |
| `{"op":"connect","id":"…"}` | connect to that board (seen in a scan, or known from before), find the service, read Info, subscribe to TX, then read Info again (over the link, encrypted now). A board out of range is waited for, with no time limit, until `disconnect`. Answers `connecting`, maybe `passkey`, then `connected` or `disconnected` |
| `{"op":"write","id":"…","data":"<base64>"}` | queue bytes for RX; they go out in order, cut to the largest write, as fast as the link takes them. `sent` reports them gone |
| `{"op":"disconnect","id":"…"}` | hang up (or stop waiting for the board). Answers `disconnected` with code `cancelled` |
| `{"op":"forget","id":"…"}` | hang up and drop every reference to the board. Answers `forgotten`. See "Bonds" for what it can't do |
| `{"op":"state"}` | answer `state` and `scanning` again |

### Events (helper to service)

| line | meaning |
|---|---|
| `{"ev":"ready","protocol":1,"helper":"deck-ble/macos","window":4096}` | the first line. `window`: the most bytes the service may have written and not yet seen in `sent` |
| `{"ev":"state","state":"on","message":"…"}` | the adapter: `on`, `off`, `unauthorized` (the system's privacy setting says no), `unsupported`, `resetting`, `unknown` (starting). Not `on` ends every link with `disconnected` code `bluetooth-off` |
| `{"ev":"scanning","on":true}` | scanning started or stopped |
| `{"ev":"found","id":"…","name":"deck-3fa9c1","rssi":-58}` | a board seen. A second `found` adds its Info: `"board"`, `"boardId"`, `"firmware"`, `"paired"` (false: in setup). Info is read on a short connection without pairing, once per board per scan; a bonded board may refuse others, and then has no Info |
| `{"ev":"connecting","id":"…"}` | `connect` started |
| `{"ev":"passkey","id":"…","message":"Type the 6-digit code shown on the board into the window macOS opens."}` | connected, Info read: the board is in setup (`paired=0`), and the helper is subscribing to TX now, which makes the computer pair: the system's window opens and the board shows the code. The helper can't see that window or the code; this is the moment they appear. Not sent for a board the computer is paired with (`paired=1`): it encrypts with the bond, silently |
| `{"ev":"connected","id":"…","mtu":182,"board":"…","boardId":"…","firmware":"…","paired":true}` | TX notifications are on, so the link is paired, encrypted and ready; the Info fields are from a read made after that, over the encrypted link (the service binds the board by them). `mtu`: the largest single write |
| `{"ev":"data","id":"…","data":"<base64>"}` | bytes from TX |
| `{"ev":"sent","id":"…","n":120}` | that many queued bytes went out |
| `{"ev":"disconnected","id":"…","code":"…","reason":"…"}` | the link ended or never came up; `reason` is plain words for people. Codes: `pairing-failed` (wrong code, or cancelled), `pairing-removed` (the board forgot this computer), `pairing-timeout`, `timeout` (out of range, switched off), `gone` (the board hung up), `cancelled` (the service asked), `unknown-peripheral` (never seen, never paired: scan first), `not-a-deck` (no Deck Link service), `bluetooth-off`, `error` |
| `{"ev":"forgotten","id":"…"}` | after `forget` |
| `{"ev":"error","message":"…"}` | a line it couldn't use (not JSON, unknown `op`, a `write` to a board that isn't connected: then with `id`) |

Flow control: the service keeps at most `window` bytes unacknowledged and waits for `sent`; the helper
writes without response while the link says it can (`canSendWriteWithoutResponse` and
`peripheralIsReady(toSendWriteWithoutResponse:)` on macOS), so a slow link slows the service down
instead of growing a queue. A board whose RX only takes writes with response gets one write at a time;
one it refuses with "insufficient resources" (ATT 0x11: its buffer is full and it took none of it) is
sent again shortly, in order, and its bytes are reported in `sent` once.

The board says `Hello` as soon as TX is subscribed. Notifications that arrive before the subscription
is confirmed are kept and passed on right after `connected`, so the `Hello` isn't lost. The service
writes nothing before the board's `Hello` (its session answers it with `Welcome`).

## macOS: what CoreBluetooth does and doesn't let the helper do

**Pairing.** A central can't start pairing or supply a passkey through CoreBluetooth. Pairing starts
when the board answers a request with "insufficient authentication" (here: subscribing to TX). macOS
then pairs, shows its own window for the passkey the board displays, and finishes the request only
after pairing succeeds. If the person cancels or types the wrong code, the request fails with an ATT
error (insufficient authentication or encryption) or the link drops; a timeout shows as
"encryption is insufficient" or "encryption timed out". The helper turns these into the codes above.

**What the helper can observe.** Only outcomes: TX subscribed (the board accepted the link), or an
error. It can't see the pairing window either, so `passkey` goes out when the window is about to
open: after Info says `paired=0`, as it subscribes. The board itself never asks for encryption while
it is in setup, so pairing (and the window) only follows that subscription, not a scan's short
connection that reads Info. CoreBluetooth doesn't say whether a link is encrypted, whether pairing used a passkey or "just
works", whether a bond exists, or which key size was used. So the service can't verify the security
rule itself: it relies on the board refusing RX and TX on any link that isn't encrypted and
authenticated (MITM), and on the board advertising Info's `paired` honestly. A board that offered
"just works" would break the rule, and only the firmware can prevent that.

**Adding a board that is on USB or Wi-Fi.** Such a board doesn't say `Hello` over Bluetooth (its
other link carries the session); it keeps the new link for 20 s and reports it on that other link. The
service binds the board by the Info read over the encrypted link plus that report
(`backend/internal/ble`, `Uplinks.Add`), then disconnects. Nothing changes in the line protocol.

**Bonds.** macOS keeps the bond in its own Bluetooth settings, for the whole computer, not in the
helper or the deck's data folder. The helper can't list, check or remove bonds: `forget` only drops
its references. To remove a bond, the person forgets the board in System Settings, Bluetooth (if it is
listed there) and puts the board back in setup from its screen. A bond both sides still hold (an add
that failed after the code was typed) needs nothing: adding the board again connects with it, no code. When the board drops its side, macOS
reports "peer removed pairing information" and the helper says `pairing-removed`. The identifier of
a board stays the same on this computer while the bond lasts, which is how known boards are connected
again (`retrievePeripherals(withIdentifiers:)`), without a scan.

**Permission.** Bluetooth needs the person's consent. macOS asks on behalf of the "responsible
process" and reads that process's Info.plist; without `NSBluetoothAlwaysUsageDescription` there it
refuses (or ends the process) without asking. Started by `agentctl`, a launchd agent with no Info.plist,
the helper would be attributed to `agentctl`. So:

- `deck-ble` carries its own Info.plist in a `__TEXT,__info_plist` section (linker `-sectcreate`;
  `macos/Support/Info.plist`): bundle identifier `dev.agents-deck.deck-ble`, name
  "agents deck Bluetooth", and the usage text. macOS reads it only when the helper runs on its own.
- In the app the helper is `agents deck.app/Contents/MacOS/deck-ble`, inside the app's bundle, so macOS
  reads the **app's** Info.plist for it and ignores the built-in one. The app's Info.plist
  (`clients/macos/macos/Runner/Info.plist`) carries the same usage text for that reason. Without it,
  macOS ends the helper (SIGABRT, "attempted to access privacy-sensitive data without a usage
  description") as soon as it starts, and the service restarts it every 30 s. `package.sh` refuses to
  add the helper to an app whose Info.plist doesn't have the key.
- At start it runs itself again as its own responsible process
  (`responsibility_spawnattrs_setdisclaim`, looked up at run time; the way Xcode's debugger and
  Qt Creator start programs), passing stdin, stdout and stderr through and waiting for it. If the call
  isn't there, it carries on as it is. Which name the prompt and the switch in System Settings,
  Privacy & Security, Bluetooth show ("agents deck Bluetooth" or the app's "agents deck") is not yet
  seen on a real Mac.
- The packaging signs it with the identifier `dev.agents-deck.deck-ble`, which matches its Info.plist,
  so the permission survives updates signed by the same team.
- `deck-ble --authorization` prints the current answer without asking.

Why not a separate `.app` or the tray app: an `.app` started through Launch Services can't hand its
stdin and stdout to the service, and the tray app may not be running when the service is (it starts
at login on its own). The disclaim keeps the helper a plain child of the service. If the pairing window
turns out not to appear for a helper with no window of its own, the fallback is to have the tray app
start the helper and relay these same lines over the service's unix socket.

## Building and testing (macOS)

```sh
cd clients/ble/macos
swift build            # debug
swift test             # the line protocol, Info, the write queue and the link logic, with a fake radio
swift build -c release --arch arm64 --arch x86_64 --show-bin-path   # universal, for packaging
```

`packaging/macos/package.sh --deck-ble <that dir>/deck-ble` puts it next to `agentctl` in the app and
signs it. The Go side is tested with a fake helper that speaks these lines
(`backend/internal/ble/fakehelper_test.go`): no test touches a real radio.
