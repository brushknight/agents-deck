package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// DefaultBase is the repository's releases on GitHub. Files are read from
// Base/latest/download/<file> and Base/download/v<version>/<file>: plain
// downloads, not the rate-limited API.
const DefaultBase = "https://github.com/brushknight/agents-deck/releases"

const (
	SumsFile = "SHA256SUMS"
	SigFile  = "SHA256SUMS.sig"

	maxSums  = 1 << 20
	maxSig   = 1 << 10
	MaxFile  = 1 << 30 // no release file is near this; a guard against a runaway download
	smallGet = 60 * time.Second
	idle     = 60 * time.Second // a download that sends nothing for this long is given up
)

// Releases reads releases: the signed checksum file, and any file in it,
// checked and cached under Dir/<version>/.
type Releases struct {
	// Base is the releases root (DefaultBase), or with Flat one release's
	// folder where every file sits at Base/<file> (AGENTS_DECK_BASE_URL, as
	// install.sh uses it).
	Base string
	Flat bool
	Key  ed25519.PublicKey // nil: no release key, nothing is downloaded for use
	Dir  string            // cache root, normally paths.Data()/releases
	HTTP *http.Client

	mu sync.Mutex // one download at a time into the cache
}

// Release is one release's checksum file. Signed means its signature
// verified against the release key; only then are its files used.
type Release struct {
	Version string
	Sums    map[string]string // file name to sha256 (hex)
	Signed  bool
	raw     []byte
	sig     []byte
}

// NewReleases reads releases from GitHub (or AGENTS_DECK_BASE_URL) with the
// key built into this binary, caching under paths.Data()/releases.
func NewReleases() *Releases {
	key, err := ReleaseKey()
	if err != nil {
		key = nil // a malformed release.pub acts as none; TestReleasePub keeps it well formed
	}
	r := &Releases{Base: DefaultBase, Key: key, Dir: filepath.Join(paths.Data(), "releases"), HTTP: NewHTTPClient()}
	if b := os.Getenv("AGENTS_DECK_BASE_URL"); b != "" {
		r.Base, r.Flat = strings.TrimRight(b, "/"), true
	}
	return r
}

// NewHTTPClient: timeouts for connecting and for the server's first answer,
// redirects only to https once on https. Bodies have no overall deadline (a
// slow link may take minutes for an app); a stalled body is cut by File.
func NewHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 15 * time.Second
	t.ResponseHeaderTimeout = 30 * time.Second
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: t, CheckRedirect: checkRedirect}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("refusing a redirect from https to " + req.URL.Scheme)
	}
	return nil
}

func (r *Releases) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return NewHTTPClient()
}

// URL of a file in a release; version "" is the latest release.
func (r *Releases) URL(version, file string) string {
	f := url.PathEscape(file)
	switch {
	case r.Flat:
		return r.Base + "/" + f
	case version == "":
		return r.Base + "/latest/download/" + f
	}
	return r.Base + "/download/v" + url.PathEscape(version) + "/" + f
}

// PageURL is where people can read about and download a release by hand.
func (r *Releases) PageURL(version string) string {
	if r.Flat || r.Base != DefaultBase {
		return r.Base
	}
	if version == "" {
		return r.Base + "/latest"
	}
	return r.Base + "/tag/v" + version
}

// Latest reads the latest release's checksum file. Without a release key it
// still answers (Signed false), so the version can be shown.
func (r *Releases) Latest(ctx context.Context) (*Release, error) {
	// GitHub redirects latest/download/<file> to the latest release's file.
	// Its version comes from the signed file names, not from the redirect.
	rel, err := r.fetchSums(ctx, "")
	if err != nil {
		return nil, err
	}
	if rel.Signed {
		r.store(rel)
	}
	return rel, nil
}

// Release reads one release's checksum file, from the cache when it's there.
func (r *Releases) Release(ctx context.Context, version string) (*Release, error) {
	v, err := ParseVersion(version)
	if err != nil {
		return nil, err
	}
	version = v.String()
	if rel := r.cached(version); rel != nil {
		return rel, nil
	}
	rel, err := r.fetchSums(ctx, version)
	if err != nil {
		return nil, err
	}
	if rel.Version != version {
		return nil, fmt.Errorf("asked for release %s, got the files of %s", version, rel.Version)
	}
	if rel.Signed {
		r.store(rel)
	}
	return rel, nil
}

func (r *Releases) fetchSums(ctx context.Context, version string) (*Release, error) {
	raw, err := r.get(ctx, r.URL(version, SumsFile), maxSums)
	if err != nil {
		return nil, fmt.Errorf("checking for a release: %w", err)
	}
	rel := &Release{raw: raw}
	if rel.Sums, err = ParseSums(raw); err != nil {
		return nil, err
	}
	if rel.Version, err = versionOfSums(rel.Sums); err != nil {
		return nil, err
	}
	if r.Key != nil {
		// The signature from the same release as the checksums, even if a newer
		// release came out between the two downloads. Nothing above counts
		// until it verifies.
		sig, err := r.get(ctx, r.URL(rel.Version, SigFile), maxSig)
		if err != nil {
			return nil, fmt.Errorf("the release's signature: %w", err)
		}
		if err := Verify(r.Key, raw, sig); err != nil {
			return nil, err
		}
		rel.sig, rel.Signed = sig, true
	}
	return rel, nil
}

var assetVersionRe = regexp.MustCompile(`^(?:agentctl|agents-deck)_([^_]+)_`)

// versionOfSums: the version in the release's file names
// (agentctl_<version>_<os>_<arch>.tar.gz, agents-deck_<version>_...), which
// the signature covers. Every such name must agree.
func versionOfSums(sums map[string]string) (string, error) {
	found := ""
	for name := range sums {
		m := assetVersionRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		v, err := ParseVersion(m[1])
		if err != nil {
			continue
		}
		if found != "" && found != v.String() {
			return "", fmt.Errorf("SHA256SUMS mixes versions %s and %s", found, v.String())
		}
		found = v.String()
	}
	if found == "" {
		return "", errors.New("SHA256SUMS names no agentctl_<version>_ or agents-deck_<version>_ file")
	}
	return found, nil
}

func (r *Releases) dir(version string) string { return filepath.Join(r.Dir, version) }

func (r *Releases) cached(version string) *Release {
	if r.Key == nil {
		return nil
	}
	raw, err1 := os.ReadFile(filepath.Join(r.dir(version), SumsFile))
	sig, err2 := os.ReadFile(filepath.Join(r.dir(version), SigFile))
	if err1 != nil || err2 != nil || Verify(r.Key, raw, sig) != nil {
		return nil
	}
	sums, err := ParseSums(raw)
	if err != nil {
		return nil
	}
	if v, err := versionOfSums(sums); err != nil || v != version {
		return nil
	}
	return &Release{Version: version, Sums: sums, Signed: true, raw: raw, sig: sig}
}

func (r *Releases) store(rel *Release) {
	d := r.dir(rel.Version)
	if os.MkdirAll(d, 0o700) != nil {
		return
	}
	_ = writeAtomic(filepath.Join(d, SumsFile), rel.raw)
	_ = writeAtomic(filepath.Join(d, SigFile), rel.sig)
}

// Cached is the path of a release file already downloaded and checked, or "".
func (r *Releases) Cached(rel *Release, name string) string {
	want, ok := rel.Sums[name]
	if !ok || !rel.Signed {
		return ""
	}
	p := filepath.Join(r.dir(rel.Version), name)
	if sum, err := fileSHA256(p); err == nil && sum == want {
		return p
	}
	return ""
}

// File downloads a file of a signed release into the cache (resuming a
// partial download), checks it against SHA256SUMS and returns its path. A
// file already there and intact isn't downloaded again.
func (r *Releases) File(ctx context.Context, rel *Release, name string, max int64) (string, error) {
	if !rel.Signed {
		return "", ErrNoReleaseKey
	}
	want, ok := rel.Sums[name]
	if !ok {
		return "", fmt.Errorf("%s isn't in release %s", name, rel.Version)
	}
	if max <= 0 {
		max = MaxFile
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.Cached(rel, name); p != "" {
		return p, nil
	}
	d := r.dir(rel.Version)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(d, name)
	part := dst + ".part"
	for attempt := 0; ; attempt++ {
		resumed, err := r.download(ctx, r.URL(rel.Version, name), part, max)
		if err != nil {
			return "", fmt.Errorf("downloading %s: %w", name, err)
		}
		sum, err := fileSHA256(part)
		if err != nil {
			return "", err
		}
		if sum == want {
			break
		}
		os.Remove(part)
		if !resumed || attempt > 0 {
			return "", fmt.Errorf("%s doesn't match its checksum in SHA256SUMS: not using it", name)
		}
		// The part kept from before was bad; once more from the start.
	}
	if err := os.Rename(part, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Read is a small release file's content, checked against SHA256SUMS. It works
// without a release key too (against the unsigned checksums), for showing
// what a release holds; nothing read this way may be installed.
func (r *Releases) Read(ctx context.Context, rel *Release, name string, max int64) ([]byte, error) {
	if rel.Signed {
		p, err := r.File(ctx, rel, name, max)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(p)
	}
	want, ok := rel.Sums[name]
	if !ok {
		return nil, fmt.Errorf("%s isn't in release %s", name, rel.Version)
	}
	b, err := r.get(ctx, r.URL(rel.Version, name), max)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("%s doesn't match its checksum in SHA256SUMS", name)
	}
	return b, nil
}

// get fetches a small file whole.
func (r *Releases) get(ctx context.Context, u string, max int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, smallGet)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", path.Base(req.URL.Path), resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: bigger than %d bytes", path.Base(req.URL.Path), max)
	}
	return b, nil
}

// download appends to part from where it stopped (HTTP Range), or starts over
// when the server ignores the range. resumed tells whether old bytes were kept.
func (r *Releases) download(ctx context.Context, u, part string, max int64) (resumed bool, err error) {
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	off, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return false, err
	}
	if off > max {
		off = 0
		if err := f.Truncate(0); err != nil {
			return false, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	if off > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		off = 0
		if err := f.Truncate(0); err != nil {
			return false, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return false, err
		}
	case resp.StatusCode == http.StatusPartialContent && off > 0 &&
		strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", off)):
		resumed = true
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && off > 0:
		return true, nil // already whole (or wrong: the checksum decides)
	default:
		return false, fmt.Errorf("%s", resp.Status)
	}
	if resp.ContentLength > 0 && off+resp.ContentLength > max {
		return false, fmt.Errorf("bigger than %d bytes", max)
	}
	n, err := io.Copy(f, newIdleReader(resp.Body, idle, cancel, max-off+1))
	if err != nil {
		return resumed, err
	}
	if off+n > max {
		return resumed, fmt.Errorf("bigger than %d bytes", max)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return resumed, io.ErrUnexpectedEOF
	}
	return resumed, f.Sync()
}

// idleReader cancels the request when the body sends nothing for d, and stops
// after limit bytes.
type idleReader struct {
	r     io.Reader
	t     *time.Timer
	d     time.Duration
	limit int64
}

func newIdleReader(r io.Reader, d time.Duration, cancel func(), limit int64) *idleReader {
	return &idleReader{r: r, t: time.AfterFunc(d, cancel), d: d, limit: limit}
}

func (ir *idleReader) Read(p []byte) (int, error) {
	if ir.limit <= 0 {
		ir.t.Stop()
		return 0, io.EOF
	}
	if int64(len(p)) > ir.limit {
		p = p[:ir.limit]
	}
	n, err := ir.r.Read(p)
	ir.limit -= int64(n)
	if err != nil {
		ir.t.Stop()
	} else {
		ir.t.Reset(ir.d)
	}
	return n, err
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeAtomic writes through a temporary file and a rename, so a reader never
// sees half a file.
func writeAtomic(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, bytes.NewReader(data))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
