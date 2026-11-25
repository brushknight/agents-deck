// Package updatetest serves fake releases over httptest, laid out like
// GitHub's (releases/latest/download/<file> redirecting to
// releases/download/v<version>/<file>), signed with a key made for the test.
package updatetest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/update"
)

type Server struct {
	*httptest.Server
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey

	mu       sync.Mutex
	releases map[string]map[string][]byte
	latest   string
	cut      map[string]int // file name: bytes to send before dropping the connection (once)
	hits     map[string]int // request paths
	ranges   int
}

func New(t testing.TB) *Server {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Priv: priv, Pub: pub, releases: map[string]map[string][]byte{}, cut: map[string]int{}, hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Add publishes a release: its files plus SHA256SUMS and SHA256SUMS.sig.
// The newest added is the latest unless SetLatest says otherwise.
func (s *Server) Add(version string, files map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel := map[string][]byte{}
	var names []string
	for n, b := range files {
		rel[n] = b
		names = append(names, n)
	}
	sort.Strings(names)
	var sums bytes.Buffer
	for _, n := range names {
		h := sha256.Sum256(files[n])
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), n)
	}
	rel[update.SumsFile] = sums.Bytes()
	rel[update.SigFile] = update.Sign(s.Priv, sums.Bytes())
	s.releases[version] = rel
	s.latest = version
}

func (s *Server) SetLatest(version string) { s.mu.Lock(); s.latest = version; s.mu.Unlock() }

// Put replaces a file of a release without touching its checksums (tampering).
func (s *Server) Put(version, name string, b []byte) {
	s.mu.Lock()
	s.releases[version][name] = b
	s.mu.Unlock()
}

// Cut makes the next download of name stop after n bytes.
func (s *Server) Cut(name string, n int) { s.mu.Lock(); s.cut[name] = n; s.mu.Unlock() }

// Hits counts requests whose path ends in suffix.
func (s *Server) Hits(suffix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for p, c := range s.hits {
		if strings.HasSuffix(p, suffix) {
			n += c
		}
	}
	return n
}

// Ranges counts requests that resumed a download.
func (s *Server) Ranges() int { s.mu.Lock(); defer s.mu.Unlock(); return s.ranges }

// Base is the releases root, as update.DefaultBase is for GitHub.
func (s *Server) Base() string { return s.URL + "/releases" }

// Releases reads this server's releases with its key, caching in dir.
func (s *Server) Releases(dir string) *update.Releases {
	return &update.Releases{Base: s.Base(), Key: s.Pub, Dir: dir, HTTP: update.NewHTTPClient()}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.hits[r.URL.Path]++
	if r.Header.Get("Range") != "" {
		s.ranges++
	}
	latest := s.latest
	s.mu.Unlock()
	if name, ok := strings.CutPrefix(r.URL.Path, "/releases/latest/download/"); ok {
		if latest == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/releases/download/v"+latest+"/"+name, http.StatusFound)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/releases/download/v")
	if !ok {
		http.NotFound(w, r)
		return
	}
	version, name, _ := strings.Cut(rest, "/")
	s.mu.Lock()
	b, ok := s.releases[version][name]
	cut, cutting := s.cut[name]
	if cutting {
		delete(s.cut, name)
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if cutting && r.Header.Get("Range") == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusOK)
		w.Write(b[:cut])
		return // the server closes the connection: the client sees a short body
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}
