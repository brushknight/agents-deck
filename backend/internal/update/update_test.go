package update_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/update"
	"github.com/brushknight/agents-deck/backend/internal/update/updatetest"
)

var ctx = context.Background()

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func release(t *testing.T, s *updatetest.Server, version string, extra map[string][]byte) {
	files := map[string][]byte{
		"agentctl_" + version + "_linux_amd64.tar.gz":  tarGz(t, map[string]string{"agentctl": "agentctl " + version}),
		"agentctl_" + version + "_darwin_arm64.tar.gz": tarGz(t, map[string]string{"agentctl": "agentctl " + version}),
		"firmware-manifest.json":                       []byte(`{"version":"` + version + `","boards":[]}`),
		"big.bin":                                      bytes.Repeat([]byte(version), 50000),
	}
	for k, v := range extra {
		files[k] = v
	}
	s.Add(version, files)
}

func TestLatestAndDownload(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.1.0", nil)
	release(t, s, "0.2.0", nil)
	r := s.Releases(t.TempDir())

	rel, err := r.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.2.0" || !rel.Signed {
		t.Fatalf("latest: %s signed=%v", rel.Version, rel.Signed)
	}
	p, err := r.File(ctx, rel, "big.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(r.Dir, "0.2.0") {
		t.Errorf("cached at %s", p)
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, bytes.Repeat([]byte("0.2.0"), 50000)) {
		t.Error("wrong content")
	}
	if _, err := r.File(ctx, rel, "big.bin", 0); err != nil || s.Hits("big.bin") != 1 {
		t.Errorf("second File downloaded again (%d hits): %v", s.Hits("big.bin"), err)
	}
	if _, err := r.File(ctx, rel, "nope.bin", 0); err == nil {
		t.Error("a file outside SHA256SUMS")
	}

	// An older release by version, from the network, then from the cache.
	old, err := r.Release(ctx, "0.1.0")
	if err != nil || old.Version != "0.1.0" {
		t.Fatalf("0.1.0: %v", err)
	}
	before := s.Hits("/v0.1.0/SHA256SUMS")
	if _, err := r.Release(ctx, "v0.1.0"); err != nil || s.Hits("/v0.1.0/SHA256SUMS") != before {
		t.Errorf("cached release fetched again: %v", err)
	}
	r2 := s.Releases(t.TempDir())
	if _, err := r2.File(ctx, rel, "big.bin", 1000); err == nil {
		t.Error("size limit ignored")
	}
}

func TestWrongSignature(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	r := s.Releases(t.TempDir())
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	r.Key = other
	if _, err := r.Latest(ctx); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("other key: %v", err)
	}
	r.Key = s.Pub
	s.Put("0.2.0", update.SigFile, []byte("not base64!\n"))
	if _, err := r.Latest(ctx); err == nil {
		t.Fatal("garbage signature accepted")
	}
	// A changed SHA256SUMS under the original signature.
	release(t, s, "0.2.0", nil)
	s.Put("0.2.0", update.SumsFile, []byte(strings.Repeat("0", 64)+"  agentctl_0.2.0_linux_amd64.tar.gz\n"))
	if _, err := r.Latest(ctx); err == nil {
		t.Fatal("changed SHA256SUMS accepted")
	}
	entries, _ := os.ReadDir(r.Dir)
	if len(entries) != 0 {
		t.Errorf("unverified files cached: %v", entries)
	}
}

func TestTamperedFile(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	r := s.Releases(t.TempDir())
	rel, err := r.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Put("0.2.0", "big.bin", []byte("something else"))
	if _, err := r.File(ctx, rel, "big.bin", 0); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered file: %v", err)
	}
	for _, n := range []string{"big.bin", "big.bin.part"} {
		if _, err := os.Stat(filepath.Join(r.Dir, "0.2.0", n)); err == nil {
			t.Errorf("%s left behind", n)
		}
	}
}

func TestTruncatedDownloadResumes(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	r := s.Releases(t.TempDir())
	rel, err := r.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Cut("big.bin", 100000)
	if _, err := r.File(ctx, rel, "big.bin", 0); err == nil {
		t.Fatal("short download accepted")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "0.2.0", "big.bin")); err == nil {
		t.Fatal("half a file in the cache")
	}
	p, err := r.File(ctx, rel, "big.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Ranges() != 1 {
		t.Errorf("didn't resume: %d range requests", s.Ranges())
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, bytes.Repeat([]byte("0.2.0"), 50000)) {
		t.Error("resumed file differs")
	}
}

func TestBaseURLFromEnvironment(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	t.Setenv("AGENTS_DECK_BASE_URL", s.URL+"/releases/download/v0.2.0/")
	t.Setenv("AGENTSTERM_HOME", t.TempDir())
	r := update.NewReleases()
	if !r.Flat {
		t.Fatal("AGENTS_DECK_BASE_URL not used")
	}
	r.Key = s.Pub
	rel, err := r.Latest(ctx)
	if err != nil || rel.Version != "0.2.0" {
		t.Fatalf("%v", err)
	}
	if _, err := r.File(ctx, rel, "firmware-manifest.json", 0); err != nil {
		t.Fatal(err)
	}
}

// fakeSystem records what Apply runs instead of running it.
type fakeSystem struct {
	mu         sync.Mutex
	teams      map[string]string // bundle base folder name → team; "" ad hoc
	verifyFail bool
	detached   [][]string
}

func (f *fakeSystem) Unzip(_ context.Context, file, dir string) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		p := filepath.Join(dir, zf.Name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		rc, _ := zf.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if err := os.WriteFile(p, b, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSystem) CodesignVerify(_ context.Context, bundle string) error {
	if f.verifyFail {
		return errors.New("invalid signature")
	}
	return nil
}

func (f *fakeSystem) SigningTeam(_ context.Context, bundle string) (string, bool, error) {
	key := "running"
	if strings.Contains(bundle, ".agents-deck-update-") {
		key = "new"
	}
	team := f.teams[key]
	return team, team != "", nil
}

func (f *fakeSystem) Detach(name string, args ...string) error {
	f.mu.Lock()
	f.detached = append(f.detached, append([]string{name}, args...))
	f.mu.Unlock()
	return nil
}

func newUpdater(t *testing.T, s *updatetest.Server, current, kind string) (*update.Updater, *fakeSystem) {
	sys := &fakeSystem{teams: map[string]string{}}
	return &update.Updater{
		Current: current, Kind: kind, GOOS: "linux", GOARCH: "amd64",
		Exe: filepath.Join(t.TempDir(), "agentctl"), Releases: s.Releases(t.TempDir()), Sys: sys,
	}, sys
}

func TestCheckVersions(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0-rc.1", nil)
	release(t, s, "0.2.0", map[string][]byte{update.NotesFile: []byte("Boards update over Wi-Fi.")})
	for _, c := range []struct{ current, want string }{
		{"0.1.0", update.StateAvailable},
		{"0.2.0-rc.1", update.StateAvailable},
		{"0.2.0", update.StateNone},
		{"0.3.0", update.StateNone},
		{"0.3.0-rc.1", update.StateNone},
	} {
		u, _ := newUpdater(t, s, c.current, update.KindArchive)
		st, err := u.Check(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.State != c.want || st.Version != "0.2.0" || st.Current != c.current {
			t.Errorf("current %s: %+v, want %s", c.current, st, c.want)
		}
		if c.want == update.StateAvailable && st.Notes != "Boards update over Wi-Fi." {
			t.Errorf("notes: %q", st.Notes)
		}
	}
	// A pre-release published as the latest (as on GitHub only by hand) is offered to a pre-release.
	s.SetLatest("0.2.0-rc.1")
	u, _ := newUpdater(t, s, "0.2.0-beta.1", update.KindArchive)
	if st, _ := u.Check(ctx); st.State != update.StateAvailable || st.Version != "0.2.0-rc.1" {
		t.Errorf("pre-release: %+v", st)
	}
}

func TestDisabledWithoutKey(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	u, sys := newUpdater(t, s, "0.1.0", update.KindArchive)
	u.Releases.Key = nil
	os.WriteFile(u.Exe, []byte("old"), 0o755)
	st, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != update.StateDisabled || st.Version != "0.2.0" || !strings.Contains(st.Message, "release key") {
		t.Fatalf("%+v", st)
	}
	if _, err := u.Download(ctx); !errors.Is(err, update.ErrNoReleaseKey) {
		t.Fatalf("download: %v", err)
	}
	if err := u.Apply(ctx); !errors.Is(err, update.ErrNoReleaseKey) {
		t.Fatalf("apply: %v", err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "old" || len(sys.detached) != 0 {
		t.Fatal("something was installed without a key")
	}
	if s.Hits("tar.gz") != 0 {
		t.Error("downloaded a package without a key")
	}
}

func TestNotSelfUpdating(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	u, _ := newUpdater(t, s, "0.1.0", update.KindDeb)
	st, err := u.Check(ctx)
	if err != nil || st.State != update.StateAvailable || !strings.Contains(st.Message, ".deb") {
		t.Fatalf("deb: %+v %v", st, err)
	}
	if _, err := u.Download(ctx); !errors.Is(err, update.ErrNotSelfUpdating) {
		t.Fatalf("deb download: %v", err)
	}
	if err := u.Apply(ctx); !errors.Is(err, update.ErrNotSelfUpdating) {
		t.Fatalf("deb apply: %v", err)
	}
	u, _ = newUpdater(t, s, "0.1.0", update.KindZip)
	st, err = u.Check(ctx)
	if err != nil || st.State != update.StateAvailable || !strings.Contains(st.Message, "unzipped by hand") {
		t.Fatalf("zip: %+v %v", st, err)
	}
	if _, err := u.Download(ctx); !errors.Is(err, update.ErrNotSelfUpdating) {
		t.Fatalf("zip download: %v", err)
	}
	if err := u.Apply(ctx); !errors.Is(err, update.ErrNotSelfUpdating) {
		t.Fatalf("zip apply: %v", err)
	}
	u, _ = newUpdater(t, s, "dev", update.KindSource)
	st, _ = u.Check(ctx)
	if st.State != update.StateDisabled || !strings.Contains(st.Message, "development build") {
		t.Fatalf("source: %+v", st)
	}
	if err := u.Apply(ctx); !errors.Is(err, update.ErrNotSelfUpdating) {
		t.Fatalf("source apply: %v", err)
	}
}

func TestApplyArchive(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", map[string][]byte{
		"agents-deck_0.2.0_linux_amd64.tar.gz": tarGz(t, map[string]string{
			"agents-deck/agents-deck": "app 0.2.0", "agents-deck/data/x": "data",
		}),
	})
	u, sys := newUpdater(t, s, "0.1.0", update.KindArchive)
	u.AppDir = filepath.Join(t.TempDir(), "app")
	os.MkdirAll(u.AppDir, 0o755)
	os.WriteFile(filepath.Join(u.AppDir, "agents-deck"), []byte("app 0.1.0"), 0o755)
	os.WriteFile(filepath.Join(u.AppDir, "stale"), []byte("x"), 0o644)
	os.WriteFile(u.Exe, []byte("agentctl 0.1.0"), 0o755)

	if err := u.Apply(ctx); err == nil {
		t.Fatal("applied before downloading")
	}
	if st, err := u.Download(ctx); err != nil || st.State != update.StateReady {
		t.Fatalf("download: %+v %v", st, err)
	}
	if err := u.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "agentctl 0.2.0" {
		t.Errorf("agentctl: %q", b)
	}
	if fi, _ := os.Stat(u.Exe); fi.Mode().Perm()&0o100 == 0 {
		t.Error("agentctl isn't executable")
	}
	if b, _ := os.ReadFile(filepath.Join(u.AppDir, "agents-deck")); string(b) != "app 0.2.0" {
		t.Errorf("app: %q", b)
	}
	if _, err := os.Stat(filepath.Join(u.AppDir, "stale")); err == nil {
		t.Error("old app files left")
	}
	if len(sys.detached) != 1 || !strings.Contains(strings.Join(sys.detached[0], " "), "systemctl --user try-restart") {
		t.Errorf("restart: %v", sys.detached)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(u.Exe), ".agentctl-update-*"))
	if len(leftovers) != 0 {
		t.Errorf("staging left: %v", leftovers)
	}
	if u.State().State != update.StateInstalling {
		t.Errorf("state %s", u.State().State)
	}

	// On macOS the new agentctl registers and restarts its launchd job.
	u, sys = newUpdater(t, s, "0.1.0", update.KindArchive)
	u.GOOS, u.GOARCH = "darwin", "arm64"
	os.WriteFile(u.Exe, []byte("agentctl 0.1.0"), 0o755)
	if _, err := u.Download(ctx); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sys.detached) != 1 || sys.detached[0][0] != u.Exe || sys.detached[0][1] != "install" {
		t.Errorf("restart: %v", sys.detached)
	}
}

func TestApplyTamperedCacheRefused(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	u, sys := newUpdater(t, s, "0.1.0", update.KindArchive)
	os.WriteFile(u.Exe, []byte("agentctl 0.1.0"), 0o755)
	if _, err := u.Download(ctx); err != nil {
		t.Fatal(err)
	}
	// Something changes the downloaded package; the server now serves a bad one too.
	cached := filepath.Join(u.Releases.Dir, "0.2.0", "agentctl_0.2.0_linux_amd64.tar.gz")
	os.WriteFile(cached, tarGz(t, map[string]string{"agentctl": "evil"}), 0o600)
	s.Put("0.2.0", "agentctl_0.2.0_linux_amd64.tar.gz", tarGz(t, map[string]string{"agentctl": "evil"}))
	if err := u.Apply(ctx); err == nil {
		t.Fatal("tampered package installed")
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "agentctl 0.1.0" || len(sys.detached) != 0 {
		t.Fatalf("agentctl changed: %q", b)
	}
	if u.State().State != update.StateReady || u.State().Error == "" {
		t.Errorf("state after a failed apply: %+v", u.State())
	}
}

func macBundle(t *testing.T, root, version string) string {
	app := filepath.Join(root, "agents deck.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "agentctl"), []byte("agentctl "+version), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(
		"<plist><dict><key>CFBundleExecutable</key>\n\t<string>agents deck</string></dict></plist>"), 0o644)
	return app
}

func TestApplyMacApp(t *testing.T) {
	appZip := func(version string) []byte {
		return zipOf(t, map[string]string{
			"agents deck.app/Contents/MacOS/agentctl":    "agentctl " + version,
			"agents deck.app/Contents/MacOS/agents deck": "app " + version,
			"agents deck.app/Contents/Info.plist":        "<plist/>",
		})
	}
	s := updatetest.New(t)
	release(t, s, "0.2.0", map[string][]byte{"agents-deck_0.2.0_macos_arm64.zip": appZip("0.2.0")})

	setup := func(running, fresh string, verifyFail bool) (*update.Updater, *fakeSystem, string) {
		u, sys := newUpdater(t, s, "0.1.0", update.KindApp)
		u.GOOS, u.GOARCH = "darwin", "arm64"
		app := macBundle(t, t.TempDir(), "0.1.0")
		u.Exe = filepath.Join(app, "Contents", "MacOS", "agentctl")
		sys.teams["running"], sys.teams["new"], sys.verifyFail = running, fresh, verifyFail
		if _, err := u.Download(ctx); err != nil {
			t.Fatal(err)
		}
		return u, sys, app
	}
	content := func(app string) string {
		b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "agentctl"))
		return string(b)
	}

	// Same team: swapped, then the service and the app restart from the new bundle.
	u, sys, app := setup("ABCDE12345", "ABCDE12345", false)
	if err := u.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if content(app) != "agentctl 0.2.0" {
		t.Errorf("bundle not replaced: %q", content(app))
	}
	if len(sys.detached) != 1 {
		t.Fatalf("restart: %v", sys.detached)
	}
	d := sys.detached[0]
	if d[0] != "/bin/sh" || d[4] != u.Exe || d[5] != "agents deck" || d[6] != app || !strings.Contains(d[2], `"$1" install`) {
		t.Errorf("restart: %q", d)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(app), ".agents-deck-update-*")); len(left) != 0 {
		t.Errorf("staging left: %v", left)
	}

	// Another team: refused, the old bundle stays.
	u, sys, app = setup("ABCDE12345", "ZZZZZ99999", false)
	if err := u.Apply(ctx); err == nil || !strings.Contains(err.Error(), "team") {
		t.Fatalf("other team: %v", err)
	}
	if content(app) != "agentctl 0.1.0" || len(sys.detached) != 0 {
		t.Error("other team's app installed")
	}

	// Signature doesn't verify: refused.
	u, sys, app = setup("ABCDE12345", "ABCDE12345", true)
	if err := u.Apply(ctx); err == nil {
		t.Fatal("unverified app installed")
	}
	if content(app) != "agentctl 0.1.0" || len(sys.detached) != 0 {
		t.Error("unverified app installed")
	}

	// The running app is ad hoc signed (no Developer ID): only the signature check applies.
	u, _, app = setup("", "", false)
	if err := u.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if content(app) != "agentctl 0.2.0" {
		t.Error("ad hoc: not replaced")
	}
}

func TestApplyWindows(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", map[string][]byte{"agents-deck_0.2.0_windows_amd64_setup.exe": []byte("MZ installer")})
	u, sys := newUpdater(t, s, "0.1.0", update.KindWindows)
	u.GOOS = "windows"
	if _, err := u.Download(ctx); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sys.detached) != 1 || !strings.HasSuffix(sys.detached[0][0], "_setup.exe") || sys.detached[0][1] != "/VERYSILENT" {
		t.Errorf("installer: %v", sys.detached)
	}
}

func TestMissingPackage(t *testing.T) {
	s := updatetest.New(t)
	release(t, s, "0.2.0", nil)
	u, _ := newUpdater(t, s, "0.1.0", update.KindApp) // no macOS zip in this release
	u.GOOS, u.GOARCH = "darwin", "arm64"
	st, err := u.Check(ctx)
	if err != nil || st.State != update.StateAvailable || !strings.Contains(st.Message, "no package") {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := u.Download(ctx); err == nil {
		t.Fatal("downloaded a package that isn't there")
	}
}
