package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// States of an update.
const (
	StateNone       = "none"       // up to date (or nothing known yet)
	StateAvailable  = "available"  // a newer release exists
	StateReady      = "ready"      // downloaded and checked, waiting to be installed
	StateInstalling = "installing" // Apply started; the service and app restart
	StateDisabled   = "disabled"   // this build never updates itself (no release key, or built from source)
)

// State is what the updater knows. Message says, from the user's side, what
// to do when the update can't happen here (no release key, a .deb, a build
// from source).
type State struct {
	State   string `json:"state"`
	Current string `json:"current"`
	Version string `json:"version,omitempty"` // the latest release
	Notes   string `json:"notes,omitempty"`   // what's new, when the release says
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Install string `json:"install"`        // InstallKind
	Page    string `json:"page,omitempty"` // where to download it by hand
}

// ErrNotSelfUpdating: this install is updated some other way (State.Message says how).
var ErrNotSelfUpdating = errors.New("this copy of agents deck doesn't update itself")

const maxNotes = 256 << 10

// Updater checks for, downloads and installs new releases of agents deck.
// It's safe for concurrent use.
type Updater struct {
	Current string // this build's version
	Kind    string // InstallKind
	GOOS    string
	GOARCH  string
	Exe     string // the running agentctl, symlinks resolved
	AppDir  string // Linux archive installs: the tray app's folder, "" when there is none
	// MacApp, for a separate agentctl on a Mac (install.sh): the agents deck
	// app installed there, "" when there is none. The app carries its own
	// agentctl and updates itself; this copy then doesn't (see Check).
	MacApp   string
	Releases *Releases
	Sys      System

	mu    sync.Mutex
	state State
	rel   *Release
}

// New is an Updater for the running binary, reading GitHub releases (or
// AGENTS_DECK_BASE_URL) with the built-in release key.
func New(current string) *Updater {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	u := &Updater{
		Current: current, Kind: detectKind(runtime.GOOS, exe, current),
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Exe: exe,
		Releases: NewReleases(), Sys: HostSystem{},
	}
	if runtime.GOOS == "darwin" && u.Kind == KindArchive {
		u.MacApp = installedMacApp()
	}
	if runtime.GOOS == "linux" {
		if home, err := os.UserHomeDir(); err == nil {
			// Where install.sh puts the tray app.
			if d := filepath.Join(home, ".local", "share", "agents-deck", "app"); isDir(d) {
				u.AppDir = d
			}
		}
	}
	return u
}

// State is the last known state, without asking the network.
func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.base(u.state)
}

func (u *Updater) base(s State) State {
	s.Current, s.Install = u.Current, u.Kind
	if s.State == "" {
		s.State = StateNone
	}
	return s
}

// Check reads the latest release's checksum file and compares versions. When
// this release's files are already downloaded and checked, the state is ready.
func (u *Updater) Check(ctx context.Context) (State, error) {
	rel, err := u.Releases.Latest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		s := u.base(u.state)
		if s.State == StateInstalling {
			s.State = StateNone
		}
		s.Error = err.Error()
		u.state = s
		return s, err
	}
	u.rel = rel
	s := u.base(State{Version: rel.Version, Page: u.Releases.PageURL(rel.Version)})
	newer := Newer(rel.Version, u.Current)
	if newer {
		if b, err := u.Releases.Read(ctx, rel, NotesFile, maxNotes); err == nil {
			s.Notes = string(b)
		}
	}
	switch {
	case u.Kind == KindSource:
		s.State = StateDisabled
		s.Message = "This is a development build, built from source. It never updates itself: pull the code and build it again."
	case !rel.Signed:
		s.State = StateDisabled
		s.Message = "This build has no release key, so it can't check that a download is genuine and never installs one."
		if newer {
			s.Message += " Version " + rel.Version + " is out: download it from " + s.Page + "."
		}
	case !newer:
		s.State = StateNone
	case u.Kind == KindDeb:
		s.State = StateAvailable
		s.Message = "Installed with the system's package manager, so it doesn't update itself. " +
			"Download the new .deb from " + s.Page + " and install it (sudo apt install ./<file>.deb)."
	case u.Kind == KindZip:
		s.State = StateAvailable
		s.Message = "This agentctl.exe was unzipped by hand, so it doesn't update itself. " +
			"Download the new installer or .zip from " + s.Page + "."
	case u.MacApp != "":
		s.State = StateAvailable
		s.Message = u.besideAppMessage()
	default:
		s.State = StateAvailable
		if names, err := u.assets(rel); err != nil {
			s.Message = err.Error() + " Download it from " + s.Page + "."
		} else if u.allCached(rel, names) {
			s.State = StateReady
		}
	}
	u.state = s
	return s, nil
}

// Download fetches this platform's package of the latest release into the
// data folder and checks it. It checks for a release first when needed.
func (u *Updater) Download(ctx context.Context) (State, error) {
	u.mu.Lock()
	rel := u.rel
	u.mu.Unlock()
	if rel == nil {
		if _, err := u.Check(ctx); err != nil {
			return u.State(), err
		}
		u.mu.Lock()
		rel = u.rel
		u.mu.Unlock()
	}
	s := u.State()
	switch {
	case s.State == StateDisabled && u.Kind == KindSource:
		return s, fmt.Errorf("%w: %s", ErrNotSelfUpdating, s.Message)
	case s.State == StateDisabled:
		return s, ErrNoReleaseKey
	case NotifyOnly(u.Kind):
		return s, fmt.Errorf("%w: %s", ErrNotSelfUpdating, s.Message)
	case s.State == StateNone, s.State == StateReady, s.State == StateInstalling:
		return s, nil
	}
	names, err := u.assets(rel)
	if err == nil {
		for _, n := range names {
			if _, err = u.Releases.File(ctx, rel, n, MaxFile); err != nil {
				break
			}
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.rel != rel { // checked again meanwhile
		return u.base(u.state), err
	}
	if err != nil {
		u.state.Error = err.Error()
		return u.base(u.state), err
	}
	u.state.State, u.state.Error = StateReady, ""
	return u.base(u.state), nil
}

// Apply installs the downloaded release, the way this copy was installed
// (design section 07), and restarts the service (and the app). Nothing is
// installed unless the release's signature and every file's checksum verified.
func (u *Updater) Apply(ctx context.Context) error {
	u.mu.Lock()
	rel, s := u.rel, u.base(u.state)
	if s.State != StateReady || rel == nil {
		u.mu.Unlock()
		switch {
		case s.State == StateDisabled && u.Kind != KindSource:
			return ErrNoReleaseKey
		case s.State == StateDisabled, NotifyOnly(u.Kind):
			return fmt.Errorf("%w: %s", ErrNotSelfUpdating, s.Message)
		case s.State == StateInstalling:
			return errors.New("already installing")
		}
		return errors.New("no update downloaded yet")
	}
	u.state.State = StateInstalling
	u.mu.Unlock()

	err := u.apply(ctx, rel)

	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.state.State, u.state.Error = StateReady, err.Error()
	}
	return err
}

func (u *Updater) apply(ctx context.Context, rel *Release) error {
	if !rel.Signed || u.Releases.Key == nil {
		return ErrNoReleaseKey
	}
	names, err := u.assets(rel)
	if err != nil {
		return err
	}
	files := map[string]string{}
	for _, n := range names {
		// File re-checks the checksum of what's in the cache before handing it out.
		p, err := u.Releases.File(ctx, rel, n, MaxFile)
		if err != nil {
			return err
		}
		files[n] = p
	}
	if u.MacApp != "" {
		return fmt.Errorf("%w: %s", ErrNotSelfUpdating, u.besideAppMessage())
	}
	switch u.Kind {
	case KindApp:
		return u.applyApp(ctx, rel, files[macApp(rel.Version, u.GOARCH)])
	case KindArchive:
		return u.applyArchive(ctx, rel, files)
	case KindWindows:
		return u.applyWindows(files[windowsInstaller(rel.Version, u.GOARCH)])
	}
	return fmt.Errorf("%w (installed as %q)", ErrNotSelfUpdating, u.Kind)
}

// assets: the release files this install needs.
func (u *Updater) assets(rel *Release) ([]string, error) {
	var names []string
	switch u.Kind {
	case KindApp:
		names = []string{macApp(rel.Version, u.GOARCH)}
	case KindWindows:
		names = []string{windowsInstaller(rel.Version, u.GOARCH)}
	case KindArchive:
		names = []string{cliArchive(rel.Version, u.GOOS, u.GOARCH)}
		if app := linuxApp(rel.Version, u.GOARCH); u.GOOS == "linux" && u.AppDir != "" {
			if _, ok := rel.Sums[app]; ok {
				names = append(names, app)
			}
		}
	default:
		return nil, fmt.Errorf("%w (installed as %q)", ErrNotSelfUpdating, u.Kind)
	}
	for _, n := range names {
		if _, ok := rel.Sums[n]; !ok {
			return nil, fmt.Errorf("release %s has no package for %s/%s (%s).", rel.Version, u.GOOS, u.GOARCH, n)
		}
	}
	return names, nil
}

func (u *Updater) allCached(rel *Release, names []string) bool {
	for _, n := range names {
		if u.Releases.Cached(rel, n) == "" {
			return false
		}
	}
	return true
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
