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
func (u *Updater) applyApp(ctx context.Context, zip string) error {
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
		return err
	}
	return u.Sys.Detach("/bin/sh", "-c", appRestart, "sh", filepath.Join(bundle, "Contents", "MacOS", filepath.Base(u.Exe)), name, bundle)
}

// swapDir puts fresh where cur is. cur moves into trash (a folder on the same
// volume, removed by the caller); if fresh can't move in, cur goes back.
func swapDir(cur, fresh, trash string) error {
	old := filepath.Join(trash, "old")
	if err := os.Rename(cur, old); err != nil {
		return fmt.Errorf("moving %s aside: %w", cur, err)
	}
	if err := os.Rename(fresh, cur); err != nil {
		if rerr := os.Rename(old, cur); rerr != nil {
			return fmt.Errorf("putting the new version in place: %v; and putting the old one back: %v (it is in %s)", err, rerr, old)
		}
		return fmt.Errorf("putting the new version in place: %w", err)
	}
	return nil
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
	// one keeps running until the restart.
	if err := os.Rename(fresh, u.Exe); err != nil {
		return fmt.Errorf("replacing %s: %w", u.Exe, err)
	}
	if newApp != "" {
		if err := u.swapAppDir(newApp); err != nil {
			return fmt.Errorf("agentctl is updated, the tray app isn't: %w", err)
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
	if err := os.Rename(newApp, near); err != nil {
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
