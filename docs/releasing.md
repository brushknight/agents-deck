# Releasing

## Cutting a release

```bash
git tag v0.2.0 && git push origin v0.2.0
```

The `release` workflow builds everything for that tag, creates the GitHub release (with generated notes) and attaches:

| file | what |
|---|---|
| `agentctl_<version>_darwin_arm64.tar.gz` | the daemon and CLI |
| `agents-deck_<version>_macos_arm64.zip` | the menu bar app (`agents deck.app`) |
| `SHA256SUMS` | checksums of both, checked by `install.sh` |

Publishing a release from the GitHub UI works too: the workflow fills that release. To rebuild an existing tag, run the workflow by hand (Actions → release → Run workflow).

Every push to `main` and every pull request runs `ci`: Go vet and tests, cross-compiling for the planned platforms, and the Flutter tests of the deck UI and the menu bar app.

## Signing and notarization (macOS)

Without signing secrets, builds are ad-hoc signed: they run, but macOS warns the first time and treats every build as a new app (permission prompts come back after each update). With a Developer ID certificate, both `agentctl` and the app are signed with it, and the app is notarized so macOS opens it without a warning.

Repository secrets (Settings → Secrets and variables → Actions):

| secret | value |
|---|---|
| `MACOS_CERT_P12` | your **Developer ID Application** certificate with its private key, exported from Keychain Access as .p12, base64-encoded: `base64 -i cert.p12 \| pbcopy` |
| `MACOS_CERT_PASSWORD` | the password you gave the .p12 export |
| `MACOS_SIGNING_IDENTITY` | the identity name, e.g. `Developer ID Application: Your Name (TEAMID)` (`security find-identity -v -p codesigning`) |
| `APPLE_ID` | the Apple ID of the developer account (for notarization) |
| `APPLE_TEAM_ID` | the 10-character team ID |
| `APPLE_APP_PASSWORD` | an app-specific password for that Apple ID (appleid.apple.com → Sign-In and Security) |

The first three turn on signing; the last three add notarization. Notarization needs a paid Apple Developer membership; a certificate of another kind can sign, but macOS will still warn on first launch.

## Platforms

| target | status |
|---|---|
| macOS arm64 | released: `agentctl` and the menu bar app |
| macOS amd64 | `agentctl` compiles; the app needs a universal Flutter build (blocked locally by Xcode 27's `lipo`, fine on CI's Xcode) |
| Linux amd64 / arm64 | `agentctl` compiles (CI checks it); still needs a systemd user unit instead of the LaunchAgent, and terminal focus without iTerm |
| Windows | not yet: agents run in tmux, which Windows lacks (WSL would be the path), and a few Unix-only calls need ports |

Adding a target means a job beside `macos-arm64` in `.github/workflows/release.yml` that uploads its archives as an artifact; `publish` collects every artifact into the release and `SHA256SUMS`.
