# Notes for coding agents

agents deck shows coding agents (Claude Code, Codex) running on a Mac on a 4×4 deck: in the browser, in
the menu bar, on a desk panel, and on small boards over USB serial.

## Layout

| path | what |
|---|---|
| `backend/` | `agentctl`, the Go daemon and CLI. Go standard library plus `google.golang.org/protobuf`; keep it that way. |
| `backend/internal/server` | the `/v1` API on three listeners; device certificates and scopes (`devices.go`) |
| `backend/internal/link` | Deck Link, the serial protocol for boards |
| `proto/agentsdeck/link/v1` | `link.proto` and `link.options`: the Deck Link contract |
| `clients/flutter/agents_ui` | the deck UI as a Flutter package (desk panel, menu bar app) |
| `clients/macos` | the menu bar app: `macos/` (Swift shell), and `linux/` (the Linux tray app, a GTK shell) |
| `firmware/deck-link` | the C firmware kit for Deck Link boards, with examples |
| `firmware/devices` | firmware for specific devices |
| `docs/` | `protocol.md` (the API and security), `link/` (Deck Link), `features.md`, `harnesses.md` (adding a harness) |

## Before you open a PR

- `cd backend && go vet ./... && go test ./...`
- `cd clients/flutter/agents_ui && flutter analyze && flutter test` if you touched Flutter
- `clients/macos/linux/test/smoke.sh` (on Linux, after `flutter build linux`) if you touched `clients/macos/linux`
- `make -C firmware/deck-link/test/host test` and the PlatformIO builds if you touched firmware
- Keep `docs/` in step with the code: the API table in `docs/protocol.md` is checked against Deck Link
  by a test, and so are `link.options` and the Go limits.

## Rules

- No secrets, host names, IP addresses, user names or personal paths in code, tests, fixtures or docs.
  Tests use `192.0.2.x` (documentation range) and made-up names.
- The web API and Deck Link stay at parity: a new endpoint or Agent field needs its `link.proto`
  counterpart in the same PR (`go test ./internal/link` fails otherwise). Changing `link.proto` means
  regenerating both sides: `protoc` for Go (see `backend/internal/link/generate.go`) and
  `firmware/deck-link/tools/generate.sh` for nanopb.
- Never weaken the security model in `docs/protocol.md`: nothing on the network without TLS 1.3 and a
  device certificate; the web dashboard stays on `127.0.0.1`.
- Write plainly in docs and UI text: say what something does, from the user's side.

## Adding a harness

Follow [docs/harnesses.md](docs/harnesses.md): a client package under `backend/internal/<name>`, a
watcher in `backend/internal/daemon`, and a case in `harnessOf` (`harness.go`).

## Adding a Deck Link device

Follow [docs/link/new-device.md](docs/link/new-device.md). Device PRs add a folder under
`firmware/devices/` and don't touch `proto/`.
