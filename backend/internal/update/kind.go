package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BuildVersion is this binary's version (agentctl sets it from main.version).
// "dev" and `git describe` output mean a build from source.
var BuildVersion = "dev"

// Install kinds: how this copy of agentctl was installed, which decides how it
// updates (design section 07).
const (
	KindApp     = "app"     // macOS: inside agents deck.app (Contents/MacOS/agentctl)
	KindWindows = "windows" // Windows: put there by the installer
	KindDeb     = "deb"     // Linux: owned by the system's package manager
	KindArchive = "archive" // a release archive (install.sh): ~/.local/bin/agentctl
	KindZip     = "zip"     // Windows: agentctl.exe unzipped by hand from the release's .zip
	KindSource  = "source"  // built from source: never updated
)

// InstallKind tells how this copy of agentctl was installed.
func InstallKind() string {
	exe, err := os.Executable()
	if err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
	}
	return detectKind(runtime.GOOS, exe, BuildVersion)
}

func detectKind(goos, exe, version string) string {
	if IsDevBuild(version) {
		return KindSource
	}
	switch goos {
	case "windows":
		// The installer (Inno Setup) leaves its uninstaller, unins000.exe,
		// next to agentctl.exe; a copy unzipped by hand has none.
		if hasUninstaller(exe[:strings.LastIndexAny(exe, `\/`)+1]) {
			return KindWindows
		}
		return KindZip
	case "darwin":
		if appBundleOf(exe) != "" {
			return KindApp
		}
	case "linux":
		// A .deb puts agentctl in /opt/agents-deck with a link in /usr/bin (the
		// path here has its links resolved); install.sh and the archives go
		// under the user's home (or /usr/local, which no package owns).
		if strings.HasPrefix(exe, "/opt/agents-deck/") ||
			(strings.HasPrefix(exe, "/usr/") && !strings.HasPrefix(exe, "/usr/local/")) {
			return KindDeb
		}
	}
	return KindArchive
}

// hasUninstaller: dir (ending in a separator) holds an Inno Setup uninstaller (unins000.exe, or
// unins001.exe and up when several were installed there).
var hasUninstaller = func(dir string) bool {
	m, _ := filepath.Glob(dir + "unins[0-9][0-9][0-9].exe")
	return len(m) > 0
}

// NotifyOnly: installs that never update themselves; a new release is only
// announced, with where to download it.
func NotifyOnly(kind string) bool { return kind == KindDeb || kind == KindZip }

// appBundleOf is the .app folder holding exe at Contents/MacOS/<exe>, or "".
func appBundleOf(exe string) string {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	app := filepath.Dir(contents)
	if filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(app, ".app") {
		return app
	}
	return ""
}

// Asset names in a release (docs/releasing.md). Architectures are Go's names.
func cliArchive(version, goos, goarch string) string {
	return "agentctl_" + version + "_" + goos + "_" + goarch + ".tar.gz"
}

func macApp(version, goarch string) string {
	return "agents-deck_" + version + "_macos_" + goarch + ".zip"
}

func linuxApp(version, goarch string) string {
	return "agents-deck_" + version + "_linux_" + goarch + ".tar.gz"
}

func windowsInstaller(version, goarch string) string {
	return "agents-deck_" + version + "_windows_" + goarch + "_setup.exe"
}

// NotesFile, when a release has it, holds what's new in plain text or Markdown.
const NotesFile = "release-notes.md"
