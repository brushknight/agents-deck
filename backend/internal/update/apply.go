package update

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// appRestart runs detached after the bundle swap: the new bundle's agentctl
// registers and restarts the service, then the app, if it was running, is
// quit and opened again from the new bundle.
// $1 bundled agentctl, $2 the app's process name, $3 the bundle.
const appRestart = `sleep 1
"$1" install
if pkill -x "$2"; then sleep 2; open "$3"; fi
`

// linuxRestart: the systemd user service restarts only if it is running
// (someone running "agentctl serve" by hand restarts it themselves).
const linuxRestart = `systemctl --user try-restart --no-block agents-deck.service`

// applyApp replaces the macOS app bundle this agentctl runs from with the one
// in the release's zip, after checking the new one's signature: it must
// verify, and when the running app is signed with a Developer ID the new one
// must be signed by the same team.
func (u *Updater) applyApp(ctx context.Context, rel *Release, zip string) error {
	bundle := appBundleOf(u.Exe)
	if bundle == "" {
		return fmt.Errorf("%s isn't inside an app bundle", u.Exe)
	}
	stage, err := os.MkdirTemp(filepath.Dir(bundle), ".agents-deck-update-")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", bundle, err)
	}
	defer os.RemoveAll(stage)
	unpacked := filepath.Join(stage, "new")
	if err := os.Mkdir(unpacked, 0o755); err != nil {
		return err
	}
	if err := u.Sys.Unzip(ctx, zip, unpacked); err != nil {
		return fmt.Errorf("unpacking the new app: %w", err)
	}
	newApp, err := only(unpacked, func(e fs.DirEntry) bool { return e.IsDir() && strings.HasSuffix(e.Name(), ".app") })
	if err != nil {
		return fmt.Errorf("the release's app zip: %w", err)
	}
	bundled := filepath.Join(newApp, "Contents", "MacOS", filepath.Base(u.Exe))
	if fi, err := os.Stat(bundled); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("the new app has no %s inside", filepath.Base(u.Exe))
	}
	if err := u.Sys.CodesignVerify(ctx, newApp); err != nil {
		return err
	}
	team, devID, err := u.Sys.SigningTeam(ctx, bundle)
	if err != nil {
		return fmt.Errorf("reading the running app's signature: %w", err)
	}
	if devID {
		newTeam, newDevID, err := u.Sys.SigningTeam(ctx, newApp)
		if err != nil {
			return fmt.Errorf("reading the new app's signature: %w", err)
		}
		if !newDevID || newTeam != team {
			return fmt.Errorf("the new app is signed by team %q, not %q like this one: not installing it", newTeam, team)
		}
	}
	name := appExecutable(bundle)
	if err := swapDir(bundle, newApp, stage); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return &AppManagementError{App: bundle, Page: u.Releases.PageURL(rel.Version), Err: err}
		}
		return err
	}
	return u.Sys.Detach("/bin/sh", "-c", appRestart, "sh", filepath.Join(bundle, "Contents", "MacOS", filepath.Base(u.Exe)), name, bundle)
}

// swapDir puts fresh where cur is. cur moves into trash (a folder on the same
// volume, removed by the caller); if fresh can't move in, cur goes back.
func swapDir(cur, fresh, trash string) error {
	old := filepath.Join(trash, "old")
	if err := rename(cur, old); err != nil {
		return fmt.Errorf("moving %s aside: %w", cur, err)
	}
	if err := rename(fresh, cur); err != nil {
		if rerr := rename(old, cur); rerr != nil {
			return fmt.Errorf("putting the new version in place: %v; and putting the old one back: %v (it is in %s)", err, rerr, old)
		}
		return fmt.Errorf("putting the new version in place: %w", err)
	}
	return nil
}

// rename is os.Rename; tests make it fail the way macOS does.
var rename = os.Rename

// AppManagementError: macOS didn't let agents deck replace its own app.
// Since macOS 13 (App Management, under Privacy & Security), a program may
// change another app's bundle only with that permission, or when the same
// developer signed both. The old app is still in place and works.
type AppManagementError struct {
	App  string // the bundle it tried to replace
	Page string // where to download the release by hand
	Err  error
}

func (e *AppManagementError) Error() string {
	msg := "macOS didn't let agents deck replace " + e.App + ", so nothing changed and this version keeps running. " +
		"To update from here, turn agents deck on in System Settings › Privacy & Security › App Management and try again"
	if e.Page != "" {
		msg += "; or download the new version from " + e.Page + " and drag it into Applications"
	}
	return msg + "."
}

func (e *AppManagementError) Unwrap() error { return e.Err }

// InstalledMacApp: the agents deck app in /Applications or ~/Applications, or "".
func InstalledMacApp() string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, d := range dirs {
		if app := filepath.Join(d, "agents deck.app"); isDir(filepath.Join(app, "Contents", "MacOS")) {
			return app
		}
	}
	return ""
}

// besideAppMessage: a separate agentctl on a Mac that also has the app. Only
// the app updates: replacing this copy alone would leave the command and the
// app on different versions, and the service would move to this copy.
func (u *Updater) besideAppMessage() string {
	return "This agentctl is a separate copy (" + u.Exe + "); the agents deck app at " + u.MacApp +
		" has its own and updates itself. Update the app in its Settings › Updates (or run agentctl update while the service runs). " +
		"To keep the command on the app's version, delete this copy and turn on Settings › General › The agentctl command."
}

var bundleExeRe = regexp.MustCompile(`<key>CFBundleExecutable</key>\s*<string>([^<]+)</string>`)

// appExecutable: the app's own executable (its process name), from Info.plist.
func appExecutable(bundle string) string {
	if b, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist")); err == nil {
		if m := bundleExeRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return strings.TrimSuffix(filepath.Base(bundle), ".app")
}

// applyArchive replaces agentctl in place (and on Linux the tray app's
// folder, when there is one and the release has it), then restarts the service.
func (u *Updater) applyArchive(ctx context.Context, rel *Release, files map[string]string) error {
	stage, err := os.MkdirTemp(filepath.Dir(u.Exe), ".agentctl-update-")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", u.Exe, err)
	}
	defer os.RemoveAll(stage)

	cli := filepath.Join(stage, "cli")
	if err := untarGz(files[cliArchive(rel.Version, u.GOOS, u.GOARCH)], cli, MaxFile); err != nil {
		return err
	}
	exeName := "agentctl"
	if u.GOOS == "windows" {
		exeName += ".exe"
	}
	fresh := filepath.Join(cli, exeName)
	if fi, err := os.Lstat(fresh); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("the release's archive has no %s", exeName)
	}
	if err := os.Chmod(fresh, 0o755); err != nil {
		return err
	}

	appPkg := files[linuxApp(rel.Version, u.GOARCH)]
	var newApp string
	if appPkg != "" && u.AppDir != "" {
		dir := filepath.Join(stage, "app")
		if err := untarGz(appPkg, dir, MaxFile); err != nil {
			return err
		}
		// The archive holds the app's folder, or its content directly.
		newApp = dir
		if d, err := only(dir, func(e fs.DirEntry) bool { return true }); err == nil && isDir(d) {
			newApp = d
		}
	}

	// The binary first: a rename over the running file is atomic, and the old
	// one keeps running until the restart. A hard link keeps the old one, so
	// a tray app that can't be swapped puts agentctl back as it was: the two
	// never end up on different versions.
	old := filepath.Join(stage, "old")
	kept := newApp != "" && os.Link(u.Exe, old) == nil
	if err := rename(fresh, u.Exe); err != nil {
		return fmt.Errorf("replacing %s: %w", u.Exe, err)
	}
	if newApp != "" {
		if err := u.swapAppDir(newApp); err != nil {
			if !kept {
				return fmt.Errorf("agentctl is updated, the tray app isn't: %w", err)
			}
			if rerr := rename(old, u.Exe); rerr != nil {
				return fmt.Errorf("the tray app couldn't be updated (%v), and the old agentctl couldn't be put back: %w", err, rerr)
			}
			return fmt.Errorf("the tray app couldn't be updated, so nothing was changed: %w", err)
		}
	}
	if u.GOOS == "darwin" {
		return u.Sys.Detach(u.Exe, "install")
	}
	return u.Sys.Detach("/bin/sh", "-c", linuxRestart)
}

// swapAppDir: the app folder may sit on another volume than agentctl, so the
// new one is first moved (or copied) next to it.
func (u *Updater) swapAppDir(newApp string) error {
	trash, err := os.MkdirTemp(filepath.Dir(u.AppDir), ".agents-deck-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(trash)
	near := filepath.Join(trash, "new")
	if err := rename(newApp, near); err != nil {
		var le *os.LinkError
		if !errors.As(err, &le) {
			return err
		}
		if err := os.CopyFS(near, os.DirFS(newApp)); err != nil {
			return err
		}
	}
	return swapDir(u.AppDir, near, trash)
}

// Inno Setup's switches for an unattended per-user install that closes and
// reopens the running app.
var windowsInstallerArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/RESTARTAPPLICATIONS"}

// applyWindows runs the release's installer silently. It closes the app and
// the service, replaces the files and starts them again.
func (u *Updater) applyWindows(installer string) error {
	return u.Sys.Detach(installer, windowsInstallerArgs...)
}
